package main

import (
	"embed"
	"encoding/json"
	"flag"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
)

//go:embed dist/renderer/*
var staticFS embed.FS

var defaultSettings = map[string]any{
	"key": "main", "hourlyRate": float64(30), "currency": "¥",
}

func defaultData() map[string]any {
	return map[string]any{
		"areas":     []any{},
		"tables":    []any{},
		"records":   []any{},
		"histories": []any{},
		"settings":  defaultSettings,
	}
}

type Database struct {
	mu   sync.RWMutex
	path string
}

func (db *Database) load() (map[string]any, error) {
	raw, err := os.ReadFile(db.path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultData(), nil
		}
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	data := make(map[string]any, len(m))
	for k, v := range m {
		var val any
		json.Unmarshal(v, &val)
		data[k] = val
	}
	if data["settings"] == nil {
		data["settings"] = defaultSettings
	}
	return data, nil
}

func (db *Database) save(data map[string]any) error {
	dir := filepath.Dir(db.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp := db.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	return os.Rename(tmp, db.path)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start() // best-effort, fire and forget
}

type Server struct {
	db   *Database
	port string
}

func (s *Server) handleGetAll(w http.ResponseWriter, r *http.Request) {
	s.db.mu.RLock()
	defer s.db.mu.RUnlock()
	data, err := s.db.load()
	if err != nil {
		http.Error(w, `{"error":"failed to read database"}`, 500)
		log.Printf("[db] load error: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/db/"), "/")
	if len(parts) < 1 {
		http.Error(w, `{"error":"bad request"}`, 400)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body failed"}`, 400)
		return
	}
	ns := parts[0]

	data, err := s.db.load()
	if err != nil {
		http.Error(w, `{"error":"failed to read database"}`, 500)
		return
	}

	if ns == "settings" {
		var settings map[string]any
		if err := json.Unmarshal(body, &settings); err != nil {
			http.Error(w, `{"error":"invalid json"}`, 400)
			return
		}
		data["settings"] = settings
	} else {
		var item map[string]any
		if err := json.Unmarshal(body, &item); err != nil {
			http.Error(w, `{"error":"invalid json"}`, 400)
			return
		}
		data[ns] = upsert(data[ns], item)
	}

	if err := s.db.save(data); err != nil {
		http.Error(w, `{"error":"save failed"}`, 500)
		log.Printf("[db] save error: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

func (s *Server) handlePostBatch(w http.ResponseWriter, r *http.Request) {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/db/"), "/")
	if len(parts) < 2 || parts[1] != "batch" {
		http.Error(w, `{"error":"bad request"}`, 400)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body failed"}`, 400)
		return
	}
	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		http.Error(w, `{"error":"invalid json"}`, 400)
		return
	}

	ns := parts[0]
	data, err := s.db.load()
	if err != nil {
		http.Error(w, `{"error":"failed to read database"}`, 500)
		return
	}

	arr := toSlice(data[ns])
	for _, item := range items {
		arr = upsert(arr, item)
	}
	data[ns] = arr

	if err := s.db.save(data); err != nil {
		http.Error(w, `{"error":"save failed"}`, 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/db/"), "/")
	if len(parts) < 2 {
		http.Error(w, `{"error":"bad request"}`, 400)
		return
	}
	ns, id := parts[0], parts[1]

	data, err := s.db.load()
	if err != nil {
		http.Error(w, `{"error":"failed to read database"}`, 500)
		return
	}

	if ns == "settings" {
		data["settings"] = defaultSettings
	} else {
		data[ns] = removeById(data[ns], id)
	}

	if err := s.db.save(data); err != nil {
		http.Error(w, `{"error":"save failed"}`, 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

func (s *Server) apiHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}

	cleanPath := strings.Split(r.URL.Path, "?")[0]

	switch {
	case r.Method == http.MethodGet && cleanPath == "/api/db":
		s.handleGetAll(w, r)
	case r.Method == http.MethodPost && strings.Count(cleanPath, "/") == 3 && strings.HasSuffix(cleanPath, "/batch"):
		s.handlePostBatch(w, r)
	case r.Method == http.MethodPost && strings.Count(cleanPath, "/") == 3:
		s.handlePost(w, r)
	case r.Method == http.MethodDelete && strings.Count(cleanPath, "/") == 4:
		s.handleDelete(w, r)
	default:
		http.Error(w, `{"error":"not found"}`, 404)
	}
}

type spaHandler struct {
	fs  http.Handler
	sub fs.FS
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	f, err := h.sub.Open(path)
	if err != nil {
		r.URL.Path = "/"
	} else {
		f.Close()
	}
	h.fs.ServeHTTP(w, r)
}

func toSlice(v any) []any {
	if v == nil {
		return []any{}
	}
	arr, ok := v.([]any)
	if !ok {
		return []any{}
	}
	return arr
}

func upsert(arr any, item map[string]any) []any {
	slice := toSlice(arr)
	id, _ := item["id"].(string)
	for i, x := range slice {
		if m, ok := x.(map[string]any); ok && m["id"] == id {
			slice[i] = item
			return slice
		}
	}
	return append(slice, item)
}

func removeById(v any, id string) []any {
	slice := toSlice(v)
	out := make([]any, 0, len(slice))
	for _, x := range slice {
		if m, ok := x.(map[string]any); ok && m["id"] == id {
			continue
		}
		out = append(out, x)
	}
	return out
}

func main() {
	port := flag.String("port", "22700", "服务端口")
	dataDir := flag.String("data", "", "数据存储目录（默认为可执行文件同级的 data 目录）")
	flag.Parse()

	dbPath := filepath.Join(*dataDir, "db.json")
	if *dataDir == "" {
		exe, _ := os.Executable()
		dbPath = filepath.Join(filepath.Dir(exe), "data", "db.json")
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("数据目录: %s", filepath.Dir(dbPath))

	s := &Server{
		db:   &Database{path: dbPath},
		port: *port,
	}

	// static files (embedded)
	sub, err := fs.Sub(staticFS, "dist/renderer")
	if err != nil {
		log.Fatalf("无法加载内嵌静态资源: %v", err)
	}
	static := &spaHandler{fs: http.FileServer(http.FS(sub)), sub: sub}

	mux := http.NewServeMux()
	mux.Handle("/api/db", http.HandlerFunc(s.apiHandler))
	mux.Handle("/api/db/", http.HandlerFunc(s.apiHandler))
	mux.Handle("/", static)

	addr := "0.0.0.0:" + *port
	server := &http.Server{Addr: addr, Handler: mux}

	// graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("正在关闭...")
		server.Close()
	}()

	localURL := "http://127.0.0.1:" + *port
	log.Printf("拼豆管理平台 %s", localURL)
	go openBrowser(localURL)

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("服务启动失败: %v", err)
	}
	log.Println("已关闭")
}
