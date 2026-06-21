# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

拼豆管理平台 — 基于 Vue 3 的桌面应用，用于拼豆店铺的桌台计时、预约和结算管理。后端使用 Go 嵌入式服务器，打包为单一 exe 分发。

## 开发命令

```bash
cd web
npm install              # 安装前端依赖
npm run dev              # 开发运行（Vite dev server，含 API 中间件）
npm run build:renderer   # 构建前端到 dist/renderer/
build.bat                # 构建前端 + Go 服务器，输出单一 exe
```

Go 后端：
```bash
cd web
go build -ldflags="-s -w" -o ../jgdz-server.exe .   # 构建（需先 npm run build:renderer）
../jgdz-server.exe                                    # 运行（自动打开浏览器）
```

## 技术栈

- **Go 1.23** 后端服务器（嵌入式静态资源，单一 exe 分发）
- **Vue 3.5** (Composition API, SFC) + **Element Plus 2.13**
- **Vite 8**（渲染器打包，开发热更新，开发时 API 中间件）
- **无 TypeScript**，全部使用原生 JS
- 数据持久化：JSON 文件（原子写入：先写 .tmp 再 rename）

## 架构分层

### 后端 (`main.go`)
- 监听 127.0.0.1:22700，启动时自动打开浏览器
- 嵌入 `dist/renderer/` 全部静态资源，SPA 回退
- RESTful API：`GET /api/db`、`POST /api/db/:ns`、`POST /api/db/:ns/batch`、`DELETE /api/db/:ns/:id`
- 数据存储在可执行文件同级 `data/db.json`（可通过 `-data` 参数指定）
- 写入采用原子操作（写 .tmp 后 rename），保证数据安全
- 优雅关闭（SIGINT/SIGTERM）

### 开发服务器 (Vite `apiPlugin`)
- 开发模式下 Vite 中间件处理 `/api/db` 请求
- 读写项目根目录 `../data/db.json`
- 与 Go 后端的 API 完全一致

### 渲染进程 (`src/renderer/`)

**入口** (`main.js`): 创建 Vue app → 安装 Element Plus → 挂载到 `#app`

**`App.vue`**: 根组件，组合所有弹窗逻辑（新增桌台、设置、区域管理、更换桌台、结束计时、营收统计、历史记录、删除桌台、编辑桌台），通过 `useAppStore()` 获取所有状态和方法，向下分发到子组件。

**组件**:
- `TopToolbar.vue` — 搜索栏 + 操作按钮（历史记录/区域管理/营收/设置）
- `StatusSidebar.vue` — 左侧状态筛选、标签筛选、桌台管理按钮（新建/编辑/删除）
- `TableCard.vue` — 单个桌台卡片，根据 `table.status` 渲染不同操作按钮（空闲→开台/预约，已预约→开台/取消，选豆中→开始计时，使用中/暂停中→暂停-继续/结束/更换）

**状态管理** (`composables/useAppStore.js`):
- 核心 composable，无外部状态库，使用 Vue `reactive` + `computed`
- 包含所有业务逻辑：`openTable`, `reserveTable`, `cancelReserve`, `startTable`, `pauseTable`, `resumeTable`, `endTiming`, `restoreFromHistory`, `changeTable`, `addTables`, `deleteTable`, `editTable`, `addArea`, `deleteArea`, `saveSettings`, `clearRecords`
- 每秒 `setInterval` 更新 `now` ref，驱动计时器重新渲染
- 每次业务操作后调用 `db.put/db.putBatch/db.del` 持久化
- `init()` 时自动创建默认区域（大厅/单人间/包间），并迁移旧桌台数据

**数据服务** (`services/db.js`):
- 渲染进程侧缓存层（`cache` 对象），与 IPC 保持同步
- `loadAll()` 从主进程获取全量数据并填充缓存
- `put/putBatch/del` 先更新本地缓存再通过 IPC 写入主进程
- `replaceAll` 用于导入数据时整体替换缓存

## 数据模型

桌台状态流转（固定，不可手动选择状态）：
```
空闲 → 选豆中 → 使用中 ↔ 暂停中
空闲 → 已预约（开始计时）
           ↓ 取消预约 → 空闲（生成取消记录和历史）
已预约 → 选豆中（清空预约计时）
```

桌台编号格式：`{字母}-{数字}`（如 A-1），字母按区域创建顺序分配（A=第一个区域，B=第二个...）。

会话 ID：6 位随机数字字符串，开台/预约时生成，结算后清除。

## 数据存储路径

- 开发模式：项目根目录下的 `data/db.json`
- 生产模式：exe 同级的 `data/db.json`
