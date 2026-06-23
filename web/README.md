# 拼豆管理

拼豆店铺的桌台计时和预约管理 Web 应用。

## 功能

- 桌台计时：空闲 → 选豆中 → 使用中 ↔ 暂停中
- 客户预约、计划时长、超时提醒
- 自动计时（选豆超时自动开始）
- 区域管理、历史记录
- SQLite 数据持久化（WAL 模式，ACID 事务）

## 开发

```bash
cd web
npm install

# 终端 1：启动 Go API 服务器
go run . -dev

# 终端 2：启动 Vite 前端开发服务器
npm run dev
```

- Vite 开发服务器监听 22700 端口，代理 `/api` 请求到 Go 后端（22701 端口）
- Go 后端在 dev 模式不嵌入静态资源、不打开浏览器
- 浏览器访问 `http://127.0.0.1:22700`

## 生产构建

```bash
cd web
npm run build:renderer     # 构建前端到 dist/renderer/
go build -ldflags="-s -w" -o ../BeanGo.exe .
```

## 数据存储

- SQLite 数据库：`data/BeanGo.db`（WAL 模式）
- 首次启动自动从 `db.json` 迁移数据（成功后重命名为 `.bak`）
- `BeanGo.exe --export-json` 可将 SQLite 数据导出为 `db.json`

## 技术栈

- **Go** 后端（嵌入式静态资源，SQLite 持久化）
- **Vue 3.5** + **Element Plus 2.13**
- **Vite 8**（开发 HMR，API 代理到 Go）
- **modernc.org/sqlite**（纯 Go，无 CGO，单 exe 分发）
