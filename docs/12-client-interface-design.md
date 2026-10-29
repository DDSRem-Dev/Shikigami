# 客户端界面设计

**GUI 设计供后续参考，首版交付 CLI/TUI。**

本文描述目标界面和接入约束；CLI/TUI 开发实现与验收证据见 [15](./15-implementation-status.md)，GUI 尚未交付；排期统一引用 [08](./08-roadmap.md)，不再独立承诺 GUI 与首版同期发布。

## 共享架构

CLI/TUI/GUI → internal/client → 本地 agent IPC → 远端 daemon。前端不独立创建 tsnet、插件、文件 watcher 或端口 listener。所有本地状态和多窗口资源由 agent 统一管理。

单个界面操作必须包含 server_id/project_id/session_id；终端再绑定 attachment_id。用户切换服务器取消旧查询，丢弃旧响应，不让后返回的数据覆盖新选择。

## CLI/TUI

采用 Cobra（命令）、Bubble Tea（交互）、Lipgloss（样式），配置 schema 由共享模块管理；是否使用 Viper 属实现选择。完整命令树以 [07](./07-project-structure.md) 为准。

- 无参数：按目录映射智能连接。显式项目/会话命令不从当前目录推断项目，创建会话须给出 `--project`；其他会话操作可用该参数消除同名歧义。`sync` 明确支持当前目录映射：省略映射 ID/`--mapping` 时选择包含当前目录的最具体映射，自动化建议显式传入映射 ID。
- attach 快捷形式是 `shiki @ <session>`，不是未实现的 `shiki @session`。
- 模板语法是 `shiki init --template <name> --project <project>`，或以 `--name <name> --path <remote-path>` 新建项目；交互时可提示输入新项目名称/远端路径，不从本地目录推断。非交互必须给全项目范围并加 `--yes`。
- `sessions stop-claude/restart-claude <session> [--yes]` 会替换当前 pane 任务，保留 session 记录和项目文件；非 TTY 无 `--yes` 在连接 agent 前拒绝，TTY 使用明确输入 `yes` 确认。
- 支持 --server/--project、--no-color、非交互错误与管理命令 --json；--verbose 不向 JSON 或 raw 终端流插入日志。
- `shiki config startup` 通过本地 agent 只读展示启动策略：区分 agent 加载的 CLI 按需配置、受管服务登录启动和开机启动，并显示配置来源。无法核验服务绑定、覆盖配置或管理器状态时显示未知；只检查标准 Shiki 服务，不据此推断其他登录项或自定义自动化。实际启动/重启验收仍见 08/15。
- 空菜单、取消、无服务器、无会话都必须有独立状态，不能索引空数组。
- Bubble Tea model 的接收者/构造返回类型一致；`Program.Run()` 的最终 model 类型不能在无按键退出时断言成另一种值/指针类型。
- 控制终端时停止 TUI 对同一 stdin/stdout 的竞争。插件通知进入独立面板/积压，不在 Claude ANSI 字节流中插入日志和菜单。
- Dashboard 拥有可取消输入 reader；上下文取消或异常退出也须先唤醒并等待正在进行的读取完成，再关闭取消管道/epoll/kqueue。返回调用者前恢复终端并释放输入所有权，确保下一次 attachment 不被旧 reader 抢读。Bubble Tea 的 kill 路径不能代替该生命周期保证。
- raw 模式退出、SIGWINCH、Ctrl+Q 与粘贴行为遵循 10。进度只反映真实传输；未知总量使用不定进度，不用定时器模拟完成。

### 状态展示

会话连接状态与 Claude 进程状态分别显示；观察者明确标注只读。同步按项目映射展示，端口按真实 listener 展示，不伪装成当前终端私有资源。

Dashboard 查询 agent 合并后的数据：远端资源、网络路径、后台同步、监听和冲突。诊断面板与 CLI doctor/servers 同时展示最近一次已发布路由选择的实际策略顺序（tsnet/direct-tls），它不是当前实时路径或完整重试历史，不区分 tsnet 内部 P2P/DERP。未报告或不符合有限有序词汇时明确未报告，不从地址/路径推断。前端退出后已启用的后台任务继续；关闭 agent 的动作必须与“关闭窗口/Detach”分开。节点状态由运行中的 NodeControl 独立只读查询，缺少登录能力不等于协议不兼容；offline/NODE_DISABLED 应说明配置与重启步骤，NODE_STATE_UNAVAILABLE 应说明保留身份、检查路径/权限，不能统一提示登录。只有实际 needs_login/expired/denied 状态提示登录。

agent 崩溃熔断时展示 `degraded / AGENT_CRASH_LOOP`，不把诊断 IPC 可达视为后台任务已就绪。提示查看本地/服务日志、修复原因后明确运行 `shiki agent start --recover-crash-loop`；普通启动或停止不解除熔断。启动记录损坏与时钟回拨使用各自的修复提示，不提供无效的一键覆盖。

智能首次使用先确认项目/目录关联并保存 paused 映射，启用后台扫描须再次确认。初始差异由宿主/worker 保存为版本化冲突，前端通过已有 PutMapping、GetSyncState、ListConflicts、ReadConflictFile、ResolveConflict 展示和提交选择，不自行读写同步树、不选择隐含覆盖方向、不自动启插件。每项决定使用观测 generation；取消、跳过、暂停及权限不足都有显式状态。

目录选择通过 agent 代理的 ListProjectDirectories 获取项目内目录分页，须有 `file.read` 与 `project.directories.v1`；不开放插件专用 ListFiles。客户端每次请求绑定 server/project/path，检查响应范围，防止旧页/错误项目进入选择。手动路径仍由 daemon 做最终包含关系检查。

Claude 崩溃事件先结束当前 raw attachment；CLI 和 dashboard 的终端返回路径共用恢复菜单。菜单前重取会话状态，避免在已自动恢复后再次重启；重启动作需 `session.control` 与确认，观察者不显示该项。保留输出由 GetSessionLogs 在 `session.attach`/`terminal.logs.v1` 下读取，显示真实保留范围、截断与不可用原因，不当作完整持久日志。所有诊断/菜单在恢复终端之后显示；网络重连仍使用独立的有界策略。

已确认 pane 结束但原因不可用的 `CLAUDE_STOPPED/EXIT_REASON_UNKNOWN` 也退出 raw 后进入显式恢复，文案区分“原因未知”和已证实崩溃。其他 STOPPED 不被客户端推断成异常；未知状态不触发自动重启。

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

表内描述目标范围，不表示验收已经通过。CLI/TUI 已有开发实现，逐项完成情况和证据以 08/15 为准；GUI 仍为后续实施范围。GUI 接入前先完成本地 IPC 版本兼容与终端订阅/销毁原型，再实现页面；不另建与 08 冲突的 Week/Phase 时间线。
