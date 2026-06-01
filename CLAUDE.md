# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

拼豆管理平台 — 基于 Electron + Vue 3 的桌面应用，用于拼豆店铺的桌台计时、预约和结算管理。Windows/macOS 双平台支持。

## 开发命令

```bash
cd electron
npm install              # 安装依赖
npm run dev              # 开发运行（Vite dev server + Electron 并行启动）
npm run build:renderer   # 仅构建渲染器
npm start                # 构建后用 Electron 启动（生产模式预览）
npm run dist             # 打包 Windows + macOS
```

## 技术栈

- **Electron 33** + **electron-builder 25**
- **Vue 3.5** (Composition API, SFC) + **Element Plus 2.13**
- **Vite 8** (渲染器打包，开发热更新)
- **无 TypeScript**，全部使用原生 JS
- 数据持久化：主进程内存缓存 + JSON 文件（120ms 防抖异步写入）

## 架构分层

### 主进程 (`src/main.js`)
- 窗口管理（单窗口，900×600 最小尺寸）
- 系统托盘（点击切换显示/隐藏）
- 应用菜单（文件导入/导出、打开数据目录）
- IPC 处理器：`db:getAll`, `db:put`, `db:del`, `db:putBatch`, `db:putFull`
- 数据存储：启动时从 `{userData}/data/db.json` 加载到 `dbCache`，写入通过 `dbSave()`（120ms 去抖），退出时同步刷盘
- 开发模式下加载 `VITE_DEV_SERVER_URL`，生产模式加载 `dist/renderer/index.html`

### 预加载 (`src/preload.js`)
- 通过 `contextBridge.exposeInMainWorld` 暴露 `window.electronAPI`
- 封装所有 IPC 调用（`dbGetAll`, `dbPut`, `dbDel`, `dbPutFull`, `dbPutBatch`）
- 暴露 `onDataImported` / `onShowToast` 事件监听

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

- 项目根目录下的 `data/db.json`（已加入 .gitignore）
