# Cloud Clip Lite - Rust 桌面客户端

跨平台剪切板同步客户端，基于 [egui](https://github.com/emilk/egui) 构建，支持 Windows、macOS 和 Linux。

## 功能特性

- **文本同步**：一键发送文本到云端剪切板，支持 Ctrl/⌘ + Enter 快捷发送
- **文件上传**：支持任意类型文件上传（图片、文档、压缩包等）
- **实时同步**：WebSocket 长连接，新条目实时推送，30 秒心跳保活
- **断线重连**：指数退避重连策略（1s → 2s → 4s → ... → 30s 上限）
- **增量同步**：连接恢复后自动请求增量同步，不遗漏离线期间的更新
- **历史记录**：游标分页加载，支持按类型过滤（文本/图片/文件）和关键词搜索
- **系统剪贴板**：文本一键复制到系统剪贴板，图片自动解码为位图复制
- **Token 自动刷新**：JWT 过期时自动使用 Refresh Token 刷新，无缝续期
- **会话持久化**：登录状态保存到本地文件，重启自动恢复
- **深色主题**：内置深色主题 UI，自动检测并加载系统中文字体

## 技术栈

| 组件 | 库 | 说明 |
|------|-----|------|
| GUI 框架 | `eframe` / `egui` | 纯 Rust 即时模式 GUI |
| HTTP 客户端 | `reqwest` | rustls TLS，无 OpenSSL 依赖 |
| WebSocket | `tokio-tungstenite` | 异步 WebSocket 客户端 |
| 异步运行时 | `tokio` | 多线程运行时 |
| 系统剪贴板 | `arboard` | 跨平台剪贴板操作 |
| 文件对话框 | `rfd` | 原生文件选择对话框 |
| 序列化 | `serde` / `serde_json` | JSON 序列化/反序列化 |

## 项目结构

```
clients/rust/
├── Cargo.toml              # 依赖配置
├── src/
│   ├── main.rs             # 入口：窗口创建、日志初始化
│   ├── app.rs              # 主应用：状态管理、视图路由、异步任务调度
│   ├── api.rs              # HTTP API 客户端：REST 端点封装、JWT 自动刷新
│   ├── ws.rs               # WebSocket 客户端：心跳、重连、增量同步
│   ├── config.rs           # 配置与 Token 持久化
│   ├── models.rs           # 数据模型（与后端 API 对应）
│   └── views/
│       ├── mod.rs          # 视图模块导出
│       ├── login.rs        # 登录视图
│       ├── clipboard.rs    # 剪切板主视图（发送/上传/最新条目）
│       ├── history.rs      # 历史记录视图（分页/过滤/搜索）
│       └── settings.rs     # 设置视图（账号信息/修改密码）
└── README.md
```

## 构建与运行

### 前置要求

- Rust 工具链（rustup 推荐，stable 通道）
- 各平台原生构建工具：
  - **Windows**：MSVC 构建工具（Visual Studio Build Tools）
  - **macOS**：Xcode Command Line Tools
  - **Linux**：`libgtk-3-dev`、`libxcb-shape0-dev`、`libxcb-xfixes0-dev`

### 开发模式

```bash
cd clients/rust
cargo run
```

### Release 构建

```bash
cd clients/rust
cargo build --release
```

构建产物位于 `clients/rust/target/release/cloud-clip-client`（或 `.exe`）。

### 连接到后端

1. 启动 Cloud Clip Lite 后端服务（默认 `http://localhost:8080`）
2. 启动客户端，在登录页面确认服务器地址正确
3. 输入用户名和密码登录（需先在 Web 界面注册账号）

## 架构设计

### 异步任务通信

egui 是同步即时模式 GUI，无法直接调用 async 函数。本客户端采用 channel 通信模式：

```
UI 线程 (egui)                    Tokio 运行时
    │                                  │
    ├── spawn(async { ... }) ──────────►
    │                                  │
    ◄──── TaskResult (std::mpsc) ──────┤
    │                                  │
    ├── 处理结果，更新 UI 状态           │
```

- `task_tx` / `task_rx`：标准库 mpsc channel，传递异步任务结果
- `ws_rx`：tokio mpsc channel，传递 WebSocket 事件
- UI 每帧轮询 channel，100ms 请求重绘保证及时性

### Token 自动刷新

```
请求 → 401 Unauthorized → refresh_token() → 重新发送请求
                         ↓ 失败
                    清除 token → 回到登录页
```

### WebSocket 连接生命周期

```
连接 → 鉴权(token查询参数) → connected
  ├── 30s 心跳 ping/pong
  ├── 接收 clip.created → 插入列表顶部
  ├── 接收 clip.deleted → 移除条目
  ├── 接收 sync.result → 批量插入
  └── 断开 → 指数退避重连 → 连接后请求增量同步
```

## 与 Web 端的对应关系

| Web 前端 | Rust 客户端 | 说明 |
|----------|-------------|------|
| Login 页面 | `views/login.rs` | 用户名密码登录 |
| Clipboard 页面 | `views/clipboard.rs` | 文本发送、文件上传、最新条目 |
| History 页面 | `views/history.rs` | 分页列表、类型过滤、搜索 |
| Settings 页面 | `views/settings.rs` | 账号信息、修改密码 |
| WebSocket 连接 | `ws.rs` | 实时同步、心跳、重连 |
| API 请求封装 | `api.rs` | 所有 REST 端点 |

## 配置文件

- 配置文件路径：`~/.config/cloud-clip-lite/config.json`（Linux/macOS）
  或 `%APPDATA%\cloud-clip-lite\config.json`（Windows）
- Token 存储路径：同目录下 `tokens.json`

```json
{
  "server_url": "http://localhost:8080"
}
```
