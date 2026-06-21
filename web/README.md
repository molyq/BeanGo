# 拼豆管理

拼豆店铺的桌台计时和预约管理 Web 应用。

## 功能

- 桌台计时：空闲 → 选豆中 → 使用中 ↔ 暂停中
- 客户预约、计划时长、超时提醒
- 自动计时（选豆超时自动开始）
- 区域管理、历史记录
- 数据 JSON 文件持久化

## 开发

```bash
cd web
npm install
npm run dev          # Vite dev server，监听 22700 端口，API 中间件内置
```

浏览器访问 `http://127.0.0.1:22700`，iPad 可访问 `http://<本机IP>:22700`。

## 生产构建

```bash
cd web
npm run build:renderer     # 构建前端到 dist/renderer/
build.bat                  # 构建前端 + Go 服务器，输出 ../jgdz-server.exe
```

Go 服务器会嵌入 `dist/renderer/` 静态资源，打包为单一 exe。运行后自动打开浏览器。

## 数据存储

- 开发模式：项目根目录 `data/db.json`
- 生产模式：exe 同级 `data/db.json`

## 技术栈

- **Go 1.23** 后端（嵌入式静态资源）
- **Vue 3.5** + **Element Plus 2.13**
- **Vite 8**（开发服务器 + API 中间件）
