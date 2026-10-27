# 客户端界面设计

**GUI 设计供后续参考，首版交付 CLI/TUI。**

本文描述目标界面和接入约束，尚无 UI 实现；排期统一引用 [08](./08-roadmap.md)，不再独立承诺 GUI 与首版同期发布。

## 共享架构

CLI/TUI/GUI → internal/client → 本地 agent IPC → 远端 daemon。前端不独立创建 tsnet、插件、文件 watcher 或端口 listener。所有本地状态和多窗口资源由 agent 统一管理。

单个界面操作必须包含 server_id/project_id/session_id；终端再绑定 attachment_id。用户切换服务器取消旧查询，丢弃旧响应，不让后返回的数据覆盖新选择。

## CLI/TUI

采用 Cobra（命令）、Bubble Tea（交互）、Lipgloss（样式），配置 schema 由共享模块管理；是否使用 Viper 属实现选择。完整命令树以 [07](./07-project-structure.md) 为准。

- 无参数：按目录映射智能连接；明确命令不依赖隐藏当前目录状态。
- attach 快捷形式是 `shiki @ <session>`，不是未实现的 `shiki @session`。
- 模板语法是 `shiki init --template <name>`。
- 支持 --server/--project、--no-color、非交互错误；列表命令后续可提供结构化输出。
- 空菜单、取消、无服务器、无会话都必须有独立状态，不能索引空数组。
- Bubble Tea model 的接收者/构造返回类型一致；`Program.Run()` 的最终 model 类型不能在无按键退出时断言成另一种值/指针类型。
- 控制终端时停止 TUI 对同一 stdin/stdout 的竞争。插件通知进入独立面板/积压，不在 Claude ANSI 字节流中插入日志和菜单。
- raw 模式退出、SIGWINCH、Ctrl+Q 与粘贴行为遵循 10。进度只反映真实传输；未知总量使用不定进度，不用定时器模拟完成。

### 状态展示

会话连接状态与 Claude 进程状态分别显示；观察者明确标注只读。同步按项目映射展示，端口按真实 listener 展示，不伪装成当前终端私有资源。

Dashboard 查询 agent 合并后的数据：远端资源、网络路径、后台同步、监听和冲突。前端退出后已启用的后台任务继续；关闭 agent 的动作必须与“关闭窗口/Detach”分开。

配色使用语义令牌、兼容浅色/深色终端、支持 NO_COLOR，不假设终端总是黑底。窄屏下减少列，不以负宽度渲染面板。

## 后续 GUI

GUI 的 Go 后端通过 Connect-Go v2 调用 agent UDS，JS 前端使用 Wails 桥接。浏览器 fetch/Connect-Web 不能直接访问 Unix socket，也不能承载此设计的客户端流/双向终端流；不为迁移 ConnectRPC 增设浏览器可访问的本地 TCP/CORS 端点。独立 Web 客户端需另行设计传输和认证。

规划 Wails + React/TypeScript + xterm.js；框架版本、体积、原生平台适配需要原型与实测，不承诺固定 10 MB 或自动拥有系统托盘。

GUI 放在同仓 `apps/shiki-gui/`，沿用根 Go module 才能合法导入 internal/client。React 前端只能调用本地后端绑定，由后端接入 agent；不把 daemon Token 暴露到浏览器状态。

以下是概念布局。需补充项目选择、离线/加载/错误/空状态、观察者状态及同步冲突入口后再实现。

### GUI 界面设计

#### 主界面布局

```
┌────────────────────────────────────────────────────────────┐
│ Shiki                                    [_] [□] [×]        │
├────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌─────────────┐  ┌─────────────────────────────────────┐ │
│  │  Servers    │  │  Sessions                           │ │
│  ├─────────────┤  ├─────────────────────────────────────┤ │
│  │ ❯ my-vps    │  │                                     │ │
│  │   ● Online  │  │  ┌─────────────┐ ┌─────────────┐  │ │
│  │   3 sessions│  │  │   main      │ │  frontend   │  │ │
│  │             │  │  │   ● Active  │ │  ● Active   │  │ │
│  │   work      │  │  │   2h ago    │ │  30m ago    │  │ │
│  │   ○ Offline │  │  │             │ │             │  │ │
│  │   0 sessions│  │  │  [Connect]  │ │  [Connect]  │  │ │
│  │             │  │  └─────────────┘ └─────────────┘  │ │
│  │ [+ Add]     │  │                                     │ │
│  └─────────────┘  │  ┌─────────────┐ ┌─────────────┐  │ │
│                    │  │  backend    │ │ experiment  │  │ │
│                    │  │  ○ Idle     │ │  ○ Idle     │  │ │
│                    │  │  1d ago     │ │  2d ago     │  │ │
│                    │  │             │ │  🏷️ test    │  │ │
│                    │  │  [Connect]  │ │  [Delete]   │  │ │
│                    │  └─────────────┘ └─────────────┘  │ │
│                    │                                     │ │
│                    │                   [+ New Session]  │ │
│                    └─────────────────────────────────────┘ │
│                                                              │
└────────────────────────────────────────────────────────────┘
```

#### 连接界面（终端视图）

```
┌────────────────────────────────────────────────────────────┐
│ Shiki - my-vps / main                        [_] [□] [×]   │
├────────────────────────────────────────────────────────────┤
│  Session: main  │  Status: ● Connected  │  Latency: 45ms  │
├────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌─────────────────────────────────────────────────────┐  │
│  │ Claude Code >                                        │  │
│  │                                                       │  │
│  │ User: help me create a login component              │  │
│  │                                                       │  │
│  │ Claude: I'll help you create a login component.     │  │
│  │ Let's start with the React component structure...   │  │
│  │                                                       │  │
│  │ [Code block showing component...]                   │  │
│  │                                                       │  │
│  │ Claude Code >█                                       │  │
│  │                                                       │  │
│  └─────────────────────────────────────────────────────┘  │
│                                                              │
├────────────────────────────────────────────────────────────┤
│  Plugins: ● sync (23 files)  ● port (2 ports)│
│  [Detach] [Settings] [Dashboard]                           │
└────────────────────────────────────────────────────────────┘
```

#### 设置界面

```
┌────────────────────────────────────────────────────────────┐
│ Settings                                     [_] [□] [×]    │
├────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌─────────────┐  ┌─────────────────────────────────────┐ │
│  │  General    │  │  General Settings                   │ │
│  │  Servers    │  ├─────────────────────────────────────┤ │
│  │  Plugins    │  │                                     │ │
│  │  Appearance │  │  Default Server                     │ │
│  │  Advanced   │  │  [my-vps            ▼]             │ │
│  └─────────────┘  │                                     │ │
│                    │  Auto Connect on Startup            │ │
│                    │  [✓] Enable                         │ │
│                    │                                     │ │
│                    │  Remember Window Size               │ │
│                    │  [✓] Enable                         │ │
│                    │                                     │ │
│                    │  Notifications                      │ │
│                    │  [✓] Show connection status         │ │
│                    │  [✓] Show file sync events          │ │
│                    │  [ ] Show port forward events       │ │
│                    │                                     │ │
│                    │  [Save]  [Cancel]                   │ │
│                    └─────────────────────────────────────┘ │
│                                                              │
└────────────────────────────────────────────────────────────┘
```


## GUI API 和终端约束

以下是本地应用 API 设计要求，不是远端 RPC 或可直接运行的 Wails 绑定：

| 操作 | 输入/输出约定 |
|---|---|
| ListServers | 返回脱敏 ServerSummary，不返回 config 中的 Token |
| ListProjects / ListSessions | 显式 server_id/project_id，支持取消/请求代次 |
| OpenTerminal | server_id/session_id/read_only/rows/cols → attachment_id |
| SendInput | attachment_id + base64 bytes；后端有界队列统一发送 |
| ResizeTerminal | attachment_id + rows/cols，仅控制者有效 |
| CloseTerminal | attachment_id，恰好一次取消该流 |
| TerminalEvent | attachment_id + sequence + type + base64 data，按 attachment 过滤 |
| TakeControl | attachment_id，服务端检查权限并原子切换 |

Wails/JSON 的 Go []byte 通常表现为 base64 字符串，不能直接假定为 Uint8Array。桥接明确采用 base64 表示原始字节，前端解码成 Uint8Array 交给 xterm.write；输入 TextEncoder 编码后 base64 交给后端。不得把任意终端输出先作为 UTF-8 字符串解码。

终端组件生命周期：

1. 创建 xterm/fit addon，注册带 attachment 过滤的事件接收器，再打开远端流；握手前输出须缓冲或保证订阅就绪，不丢首帧。
2. ResizeObserver 监听容器尺寸，fit 后发送 resize；窗口被隐藏/尺寸为零时不发送非法大小。
3. onData → 有界异步发送队列；输入错误/断线可见，不吞 Promise rejection。
4. READY/RESET 事件更新模式并重置终端，数据按序应用；xterm.write 的完成回调用于背压，不能用无限 Wails 事件淹没前端。
5. 卸载/切换会话取消 EventsOn 返回的订阅、onData、resize observer 和关联 attachment，再 dispose 终端。异步 OpenTerminal 在组件卸载后才完成时仍需关闭所得 attachment。
6. React StrictMode 重复挂载不得创建双 attachment；老终端事件不能渲染到新终端。

所有快捷键必须避免与终端输入/操作系统冲突，在已处理时 preventDefault。Cmd+Q 保留平台退出语义；Detach 提供独立按钮或可配置快捷键，不覆盖系统退出。退出 GUI 不停止 agent。

## 托盘与通知

Wails 标题栏/About/单实例配置不等于系统托盘；需要选定版本支持的独立托盘实现并跨平台验收。向前端 EventsEmit 一个 notification 事件只产生应用内事件，不自动成为 OS 通知；桌面通知需要权限、平台桥接与后台行为设计。

GUI 托盘可以展示后台 agent 状态并提供显式停止入口；不能把窗口隐藏误当作断开全部会话或停止同步。

## 能力和排期

| 能力 | 首版 CLI/TUI | 后续 GUI |
|---|---|---|
| 项目/会话/服务器管理 | 计划支持 | 复用 agent |
| 终端与观察者/接管 | 计划支持 | 增加 xterm 渲染 |
| 同步冲突和端口状态 | 计划支持 | 图形展示 |
| Dashboard | TUI | 图形面板 |
| OS 托盘/桌面通知 | 不在首版范围 | 单独验证 |
| 拖放文件 | 不在首版范围 | 后续提案 |

表内是计划，所有功能尚待实现。GUI 接入前先完成本地 IPC 版本兼容与终端订阅/销毁原型，再实现页面；不另建与 08 冲突的 Week/Phase 时间线。
