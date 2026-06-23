package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
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
	"syscall"

	_ "modernc.org/sqlite"
)

//go:embed dist/renderer/*
var staticFS embed.FS

const schema = `
CREATE TABLE IF NOT EXISTS areas (
    id TEXT PRIMARY KEY, name TEXT NOT NULL, color TEXT NOT NULL,
    x1 INTEGER, y1 INTEGER, x2 INTEGER, y2 INTEGER
);
CREATE TABLE IF NOT EXISTS tables (
    id TEXT PRIMARY KEY, name TEXT NOT NULL, codePrefix TEXT NOT NULL,
    number INTEGER NOT NULL, tag TEXT DEFAULT '', areaId TEXT,
    status TEXT NOT NULL DEFAULT 'idle', sessionId TEXT,
    timerStart INTEGER, timerPausedTime INTEGER DEFAULT 0,
    totalPausedDuration INTEGER DEFAULT 0, scheduledDuration INTEGER,
    selectingAt INTEGER, selectingDuration INTEGER,
    packageEndTime INTEGER, packageDuration INTEGER,
    remark TEXT DEFAULT '', x INTEGER, y INTEGER, createdAt INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS records (
    id TEXT PRIMARY KEY, tableId TEXT NOT NULL, tableName TEXT NOT NULL,
    areaName TEXT DEFAULT '', sessionId TEXT,
    startTime INTEGER, endTime INTEGER, duration INTEGER DEFAULT 0,
    type TEXT NOT NULL DEFAULT 'timing', createdAt INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS histories (
    id TEXT PRIMARY KEY, tableId TEXT NOT NULL, tableCode TEXT,
    createdAt INTEGER NOT NULL, duration INTEGER DEFAULT 0,
    type TEXT NOT NULL, tableSnapshot TEXT,
    recordId TEXT, restoredAt INTEGER
);
CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY DEFAULT 'main',
    hourlyRate REAL NOT NULL DEFAULT 30,
    currency TEXT NOT NULL DEFAULT '¥',
    autoStartDelay INTEGER NOT NULL DEFAULT 0
);
`

type Database struct {
	db *sql.DB
}

func openDatabase(dbPath string) (*Database, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}

	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema init: %w", err)
	}

	return &Database{db: db}, nil
}

func (d *Database) close() error {
	return d.db.Close()
}

func (d *Database) migrateFromJSON(jsonPath string) error {
	// Skip if SQLite already has data
	var count int
	if err := d.db.QueryRow("SELECT COUNT(*) FROM areas").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取 db.json: %w", err)
	}

	var data map[string]json.RawMessage
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("解析 db.json: %w", err)
	}

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("开始事务: %w", err)
	}
	defer tx.Rollback()

	// settings (object)
	if raw, ok := data["settings"]; ok {
		var s map[string]any
		json.Unmarshal(raw, &s)
		tx.Exec(`INSERT OR REPLACE INTO settings (key, hourlyRate, currency, autoStartDelay) VALUES (?, ?, ?, ?)`,
			"main", toFloat64(s["hourlyRate"], 30), toString(s["currency"], "¥"), int64(toFloat64(s["autoStartDelay"], 0)))
	}

	// areas (array)
	if raw, ok := data["areas"]; ok {
		var items []map[string]any
		json.Unmarshal(raw, &items)
		for _, item := range items {
			tx.Exec(`INSERT OR REPLACE INTO areas (id, name, color, x1, y1, x2, y2) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				item["id"], item["name"], item["color"], item["x1"], item["y1"], item["x2"], item["y2"])
		}
	}

	// tables (array)
	if raw, ok := data["tables"]; ok {
		var items []map[string]any
		json.Unmarshal(raw, &items)
		for _, item := range items {
			tx.Exec(`INSERT OR REPLACE INTO tables (id, name, codePrefix, number, tag, areaId, status, sessionId, timerStart, timerPausedTime, totalPausedDuration, scheduledDuration, selectingAt, selectingDuration, packageEndTime, packageDuration, remark, x, y, createdAt) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				item["id"], item["name"], item["codePrefix"], int64(toFloat64(item["number"], 0)), item["tag"], item["areaId"],
				item["status"], item["sessionId"], item["timerStart"], item["timerPausedTime"],
				item["totalPausedDuration"], item["scheduledDuration"], item["selectingAt"],
				item["selectingDuration"], item["packageEndTime"], item["packageDuration"],
				item["remark"], item["x"], item["y"], int64(toFloat64(item["createdAt"], 0)))
		}
	}

	// records (array)
	if raw, ok := data["records"]; ok {
		var items []map[string]any
		json.Unmarshal(raw, &items)
		for _, item := range items {
			tx.Exec(`INSERT OR REPLACE INTO records (id, tableId, tableName, areaName, sessionId, startTime, endTime, duration, type, createdAt) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				item["id"], item["tableId"], item["tableName"], item["areaName"], item["sessionId"],
				item["startTime"], item["endTime"], item["duration"], item["type"], int64(toFloat64(item["createdAt"], 0)))
		}
	}

	// histories (array)
	if raw, ok := data["histories"]; ok {
		var items []map[string]any
		json.Unmarshal(raw, &items)
		for _, item := range items {
			snapshot := ""
			if s, ok := item["tableSnapshot"]; ok && s != nil {
				if str, ok := s.(string); ok {
					snapshot = str
				} else {
					b, _ := json.Marshal(s)
					snapshot = string(b)
				}
			}
			tx.Exec(`INSERT OR REPLACE INTO histories (id, tableId, tableCode, createdAt, duration, type, tableSnapshot, recordId, restoredAt) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				item["id"], item["tableId"], item["tableCode"], item["createdAt"],
				item["duration"], item["type"], snapshot, item["recordId"], item["restoredAt"])
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交迁移: %w", err)
	}

	os.Rename(jsonPath, jsonPath+".bak")
	log.Printf("数据已从 %s 迁移到 SQLite，原文件重命名为 .bak", jsonPath)
	return nil
}

func toFloat64(v any, def float64) float64 {
	switch x := v.(type) {
	case float64:
		return x
	default:
		return def
	}
}

func toString(v any, def string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return def
}

// getAll returns all data in the original JSON format
func (d *Database) getAll() map[string]any {
	result := map[string]any{
		"areas":     []any{},
		"tables":    []any{},
		"records":   []any{},
		"histories": []any{},
		"settings":  map[string]any{"key": "main", "hourlyRate": float64(30), "currency": "¥"},
	}

	// settings
	var hourlyRate float64
	var currency string
	var autoStartDelay int64
	err := d.db.QueryRow("SELECT hourlyRate, currency, autoStartDelay FROM settings WHERE key='main'").Scan(&hourlyRate, &currency, &autoStartDelay)
	if err == nil {
		result["settings"] = map[string]any{
			"key":            "main",
			"hourlyRate":     hourlyRate,
			"currency":       currency,
			"autoStartDelay": autoStartDelay,
		}
	}

	// areas
	result["areas"] = d.querySlice(`SELECT id, name, color, x1, y1, x2, y2 FROM areas`, func(rows *sql.Rows) any {
		var id, name, color string
		var x1, y1, x2, y2 *int64
		rows.Scan(&id, &name, &color, &x1, &y1, &x2, &y2)
		m := map[string]any{"id": id, "name": name, "color": color}
		if x1 != nil {
			m["x1"] = *x1
		}
		if y1 != nil {
			m["y1"] = *y1
		}
		if x2 != nil {
			m["x2"] = *x2
		}
		if y2 != nil {
			m["y2"] = *y2
		}
		return m
	})

	// tables
	result["tables"] = d.querySlice(`SELECT id, name, codePrefix, number, tag, areaId, status,
		sessionId, timerStart, timerPausedTime, totalPausedDuration, scheduledDuration,
		selectingAt, selectingDuration, packageEndTime, packageDuration,
		remark, x, y, createdAt FROM tables`, func(rows *sql.Rows) any {
		var id, name, codePrefix, tag, areaId, status, remark string
		var number int64
		var sessionId *string
		var timerStart, timerPausedTime, totalPausedDuration, scheduledDuration *int64
		var selectingAt, selectingDuration, packageEndTime, packageDuration *int64
		var x, y, createdAt *int64
		rows.Scan(&id, &name, &codePrefix, &number, &tag, &areaId, &status,
			&sessionId, &timerStart, &timerPausedTime, &totalPausedDuration, &scheduledDuration,
			&selectingAt, &selectingDuration, &packageEndTime, &packageDuration,
			&remark, &x, &y, &createdAt)
		m := map[string]any{
			"id": id, "name": name, "codePrefix": codePrefix, "number": number,
			"tag": tag, "areaId": areaId, "status": status, "remark": remark,
			"createdAt": *createdAt,
		}
		if sessionId != nil {
			m["sessionId"] = *sessionId
		}
		if timerStart != nil {
			m["timerStart"] = *timerStart
		}
		if timerPausedTime != nil {
			m["timerPausedTime"] = *timerPausedTime
		}
		if totalPausedDuration != nil {
			m["totalPausedDuration"] = *totalPausedDuration
		}
		if scheduledDuration != nil {
			m["scheduledDuration"] = *scheduledDuration
		}
		if selectingAt != nil {
			m["selectingAt"] = *selectingAt
		}
		if selectingDuration != nil {
			m["selectingDuration"] = *selectingDuration
		}
		if packageEndTime != nil {
			m["packageEndTime"] = *packageEndTime
		}
		if packageDuration != nil {
			m["packageDuration"] = *packageDuration
		}
		if x != nil {
			m["x"] = *x
		}
		if y != nil {
			m["y"] = *y
		}
		return m
	})

	// records
	result["records"] = d.querySlice(`SELECT id, tableId, tableName, areaName, sessionId,
		startTime, endTime, duration, type, createdAt FROM records`, func(rows *sql.Rows) any {
		var id, tableId, tableName, areaName, rtype string
		var createdAt, startTime, endTime, duration int64
		var sessionId *string
		rows.Scan(&id, &tableId, &tableName, &areaName, &sessionId,
			&startTime, &endTime, &duration, &rtype, &createdAt)
		m := map[string]any{
			"id": id, "tableId": tableId, "tableName": tableName, "areaName": areaName,
			"startTime": startTime, "endTime": endTime, "duration": duration,
			"type": rtype, "createdAt": createdAt,
		}
		if sessionId != nil {
			m["sessionId"] = *sessionId
		}
		return m
	})

	// histories
	result["histories"] = d.querySlice(`SELECT id, tableId, tableCode, createdAt, duration,
		type, tableSnapshot, recordId, restoredAt FROM histories`, func(rows *sql.Rows) any {
		var id, tableId, tableCode, htype string
		var createdAt, duration int64
		var tableSnapshot, recordId *string
		var restoredAt *int64
		rows.Scan(&id, &tableId, &tableCode, &createdAt, &duration,
			&htype, &tableSnapshot, &recordId, &restoredAt)
		m := map[string]any{
			"id": id, "tableId": tableId, "tableCode": tableCode, "createdAt": createdAt,
			"duration": duration, "type": htype,
		}
		if tableSnapshot != nil {
			m["tableSnapshot"] = *tableSnapshot
		}
		if recordId != nil {
			m["recordId"] = *recordId
		}
		if restoredAt != nil {
			m["restoredAt"] = *restoredAt
		}
		return m
	})

	return result
}

func (d *Database) querySlice(query string, scan func(rows *sql.Rows) any) []any {
	rows, err := d.db.Query(query)
	if err != nil {
		return []any{}
	}
	defer rows.Close()
	var items []any
	for rows.Next() {
		items = append(items, scan(rows))
	}
	if items == nil {
		return []any{}
	}
	return items
}

func (d *Database) upsert(ns string, item map[string]any) error {
	switch ns {
	case "settings":
		_, err := d.db.Exec(`INSERT OR REPLACE INTO settings (key, hourlyRate, currency, autoStartDelay) VALUES (?, ?, ?, ?)`,
			"main", toFloat64(item["hourlyRate"], 30), toString(item["currency"], "¥"), int64(toFloat64(item["autoStartDelay"], 0)))
		return err
	case "areas":
		_, err := d.db.Exec(`INSERT OR REPLACE INTO areas (id, name, color, x1, y1, x2, y2) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			item["id"], item["name"], item["color"], item["x1"], item["y1"], item["x2"], item["y2"])
		return err
	case "tables":
		_, err := d.db.Exec(`INSERT OR REPLACE INTO tables (id, name, codePrefix, number, tag, areaId, status, sessionId, timerStart, timerPausedTime, totalPausedDuration, scheduledDuration, selectingAt, selectingDuration, packageEndTime, packageDuration, remark, x, y, createdAt) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item["id"], item["name"], item["codePrefix"], int64(toFloat64(item["number"], 0)), item["tag"], item["areaId"],
			item["status"], item["sessionId"], item["timerStart"], item["timerPausedTime"],
			item["totalPausedDuration"], item["scheduledDuration"], item["selectingAt"],
			item["selectingDuration"], item["packageEndTime"], item["packageDuration"],
			item["remark"], item["x"], item["y"], int64(toFloat64(item["createdAt"], 0)))
		return err
	case "records":
		_, err := d.db.Exec(`INSERT OR REPLACE INTO records (id, tableId, tableName, areaName, sessionId, startTime, endTime, duration, type, createdAt) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item["id"], item["tableId"], item["tableName"], item["areaName"], item["sessionId"],
			item["startTime"], item["endTime"], item["duration"], item["type"], int64(toFloat64(item["createdAt"], 0)))
		return err
	case "histories":
		snapshot := ""
		if s, ok := item["tableSnapshot"]; ok && s != nil {
			if str, ok := s.(string); ok {
				snapshot = str
			} else {
				b, _ := json.Marshal(s)
				snapshot = string(b)
			}
		}
		_, err := d.db.Exec(`INSERT OR REPLACE INTO histories (id, tableId, tableCode, createdAt, duration, type, tableSnapshot, recordId, restoredAt) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item["id"], item["tableId"], item["tableCode"], item["createdAt"],
			item["duration"], item["type"], snapshot, item["recordId"], item["restoredAt"])
		return err
	default:
		return fmt.Errorf("未知 namespace: %s", ns)
	}
}

func (d *Database) upsertBatch(ns string, items []map[string]any) error {
	for _, item := range items {
		if err := d.upsert(ns, item); err != nil {
			return err
		}
	}
	return nil
}

func (d *Database) delete(ns, id string) error {
	switch ns {
	case "settings":
		_, err := d.db.Exec(`INSERT OR REPLACE INTO settings (key, hourlyRate, currency, autoStartDelay) VALUES ('main', 30, '¥', 0)`)
		return err
	default:
		if ns == "areas" || ns == "tables" || ns == "records" || ns == "histories" {
			_, err := d.db.Exec(fmt.Sprintf("DELETE FROM %s WHERE id = ?", ns), id)
			return err
		}
		return fmt.Errorf("未知 namespace: %s", ns)
	}
}

func (d *Database) exportJSON(w io.Writer) error {
	data := d.getAll()
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

type Server struct {
	db   *Database
	port string
	dev  bool
}

func (s *Server) handleGetAll(w http.ResponseWriter, r *http.Request) {
	data := s.db.getAll()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
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
	var item map[string]any
	if err := json.Unmarshal(body, &item); err != nil {
		http.Error(w, `{"error":"invalid json"}`, 400)
		return
	}

	if err := s.db.upsert(parts[0], item); err != nil {
		http.Error(w, `{"error":"save failed"}`, 500)
		log.Printf("[db] upsert error: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

func (s *Server) handlePostBatch(w http.ResponseWriter, r *http.Request) {
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

	if err := s.db.upsertBatch(parts[0], items); err != nil {
		http.Error(w, `{"error":"save failed"}`, 500)
		log.Printf("[db] batch upsert error: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/db/"), "/")
	if len(parts) < 2 {
		http.Error(w, `{"error":"bad request"}`, 400)
		return
	}

	if err := s.db.delete(parts[0], parts[1]); err != nil {
		http.Error(w, `{"error":"delete failed"}`, 500)
		log.Printf("[db] delete error: %v", err)
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
	cmd.Start()
}

func main() {
	port := flag.String("port", "", "服务端口（默认 22700，dev 模式默认 22701）")
	dataDir := flag.String("data", "", "SQLite 数据存储目录（默认 D:\\BeanGo_data，dev 模式默认 ../data）")
	devMode := flag.Bool("dev", false, "开发模式（不嵌入静态资源、不打开浏览器）")
	exportJSON := flag.Bool("export-json", false, "将 SQLite 数据导出为 db.json 并退出")
	flag.Parse()

	defaultPort := "22700"
	if *devMode {
		defaultPort = "22701"
	}
	if *port == "" {
		*port = defaultPort
	}

	if *dataDir == "" {
		if *devMode {
			abs, _ := filepath.Abs(filepath.Join("..", "data"))
			*dataDir = abs
		} else {
			*dataDir = `D:\BeanGo_data`
		}
	}

	dbPath := filepath.Join(*dataDir, "BeanGo.db")

	// jsonPath 用于迁移：始终从 exe 同级 data/ 目录（或 dev 模式 ../data）查找 db.json
	jsonPath := ""
	if *devMode {
		abs, _ := filepath.Abs(filepath.Join("..", "data", "db.json"))
		jsonPath = abs
	} else {
		exe, _ := os.Executable()
		jsonPath = filepath.Join(filepath.Dir(exe), "data", "db.json")
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("数据目录: %s", *dataDir)

	database, err := openDatabase(dbPath)
	if err != nil {
		log.Fatalf("无法打开数据库: %v", err)
	}
	defer database.close()

	if err := database.migrateFromJSON(jsonPath); err != nil {
		log.Printf("数据迁移失败: %v", err)
	}

	if *exportJSON {
		exportPath := filepath.Join(*dataDir, "db.json")
		f, err := os.Create(exportPath)
		if err != nil {
			log.Fatalf("无法创建导出文件: %v", err)
		}
		defer f.Close()
		if err := database.exportJSON(f); err != nil {
			os.Remove(exportPath)
			log.Fatalf("导出失败: %v", err)
		}
		log.Printf("数据已导出到 %s", exportPath)
		return
	}

	s := &Server{db: database, port: *port, dev: *devMode}

	mux := http.NewServeMux()
	mux.Handle("/api/db", http.HandlerFunc(s.apiHandler))
	mux.Handle("/api/db/", http.HandlerFunc(s.apiHandler))

	if !*devMode {
		sub, err := fs.Sub(staticFS, "dist/renderer")
		if err != nil {
			log.Fatalf("无法加载内嵌静态资源: %v", err)
		}
		static := &spaHandler{fs: http.FileServer(http.FS(sub)), sub: sub}
		mux.Handle("/", static)
	}

	addr := "0.0.0.0:" + *port
	server := &http.Server{Addr: addr, Handler: mux}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("正在关闭...")
		server.Close()
	}()

	localURL := "http://127.0.0.1:" + *port
	log.Printf("拼豆管理平台 %s", localURL)
	if !*devMode {
		go openBrowser(localURL)
	}

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("服务启动失败: %v", err)
	}
	log.Println("已关闭")
}
