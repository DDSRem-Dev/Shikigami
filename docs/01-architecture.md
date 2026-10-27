# 系统架构设计

本文规定进程和资源归属；当前是设计，尚无实现。

## 组件与边界

```text
本地设备（同一 OS 用户）
  shiki CLI / TUI ─┐
  未来 GUI ────────┴─ 本地 IPC → shiki-agent
                                 ├─ tsnet 节点 + daemon 连接池
                                 ├─ 本地配置与路径映射
                                 ├─ 终端流路由（每个前端一次 attachment）
                                 └─ 插件宿主 → sync / port 子进程
                                                ↕ 本地 CoreAPI
                                 ↕ gRPC over tsnet（P2P / DERP）
远端 VPS（指定普通 OS 用户）
  shiki-daemon
    ├─ tsnet listener + 认证 + RPC 路由
    ├─ 项目/会话管理与持久化
    ├─ 每会话 PTY 桥接与 attachment 管理
    ├─ 资源监控
    └─ 插件宿主 → sync / port 子进程
           ↕                        ↕
    项目目录 / TCP 服务         独立 tmux server
                                 └─ 每 Session 一个 pane → claude
```

文件监听属于 sync 插件，端口扫描属于 port 插件；核心不再重复实现。go-plugin 仅解决同机宿主与插件间通信，不自动提供跨设备插件路由。

## 本地 agent

- 每个 OS 用户一个 agent，以状态目录锁防止重复启动。CLI 自动发现或启动，等待 IPC ready 后使用。
- Unix domain socket 放在 0700 的私有目录中、socket 为 0600，并验证对端 OS 身份；WSL 使用 Linux 路径。原生 Windows IPC 属后续 GUI 平台工作。
- agent 持有每个 tailnet 配置的 tsnet 节点、每个服务器的 gRPC 连接。多个 CLI 不单独打开相同 tsnet 状态目录。
- 前端与远端 attachment 一一对应，身份为 `(server_id, session_id, attachment_id)`。关闭一个窗口仅取消该窗口终端流。
- 同步按 `(server_id, project_id, canonical_local_root)` 唯一化；同一远端项目的不同本地根必须显式创建独立映射，不自动互相覆盖。
- 转发按 `(server_id, remote_endpoint, local_bind)` 唯一化。同机多个窗口复用同一转发；跨设备各有自己的 agent。
- 同步和转发可在全部前端关闭后继续；显式 pause/stop、agent 退出或插件故障才停止。新建映射是否后台启用要在设置中显示。
- `shiki agent status/start/stop` 管理后台；`shiki sync status/pause/resume` 管理映射。`shiki ports` 查询 agent 的真实监听状态，不向远端查询本地端口。
- agent 重启恢复已启用映射，先重新认证、扫描差异，再继续同步；不恢复已断开的 TCP 连接或重放终端输入。

本地 IPC 必须在 Phase 0 定义独立 schema：服务器/项目选择、终端双向流、agent 生命周期、同步与转发状态。远端 09 中的 RPC 不是可直接暴露给本地前端的完整 API；尚未定义的 IPC 是实现前置任务。

## 远端 daemon 与 tmux

daemon、插件、tmux 和 Claude 均以指定普通用户运行，不通过 JWT 字段切换 Unix UID。安装器可用 root 安装文件和服务，但运行服务不使用 root。

每个会话启动独立 tmux session，统一 socket `shiki-<uid>`，session 名 `shiki-<session_id>`。用户可通过相同 UID/socket 手动查看。PTY 连接的是普通 `tmux attach-session`，不能把 `-CC` 控制模式协议作为终端字节显示。

远端 Session 生命周期独立于客户端连接。daemon 故障后的存活不仅依赖 tmux，还依赖 systemd 不杀死其子进程，见 [安装契约](./11-automated-installation.md)。主机重启会结束进程；只能恢复配置并按显式策略重新启动，不能恢复内存或对话状态。

## 数据流

| 功能 | 数据路径与所有者 |
|---|---|
| 终端 | 前端 ⇄ agent IPC ⇄ TerminalStream ⇄ daemon 会话桥接 ⇄ PTY ⇄ tmux |
| 本地文件变化 | sync 插件扫描/监听 → CoreAPI 代理 → daemon 鉴权路由 → 远端 sync 插件 → 版本检查与原子写入 |
| 远端文件变化 | 远端 sync 插件 → WatchFiles → agent → 本地 sync 插件 → 差异协调 |
| 转发 | 本地 port 插件持有 loopback listener → 每条 TCP 连接一个 ForwardPort 流 → daemon 鉴权 → 远端 port 插件拨号 |
| 状态 | daemon 提供远端项目/会话/系统状态；agent 合成本地网络、同步、监听状态后供前端展示 |

## 状态模型

三个维度分别记录，禁止混用：

- 生命周期：`creating → ready → deleting → deleted`；创建失败为 `failed`。
- Claude 进程：`starting / running / stopped / crashed / unknown`。
- attachment：0 个显示 Idle，1 个及以上显示 Active；不代表 Claude 是否正在推理。

UI 的 Idle 不能作为自动杀进程条件。没有可靠 Claude 活动协议时，只能展示连接、输出和进程观测值。

## 安全与故障边界

- Tailscale 控制网络准入，应用 Token 控制 RPC；连接回退仍需同一 daemon 身份，禁止跳到不可信明文端点。
- 多设备 Token 可单独撤销。只读终端禁止输入、resize、信号及接管；终端写权限等价于该 OS 用户的代码执行能力，不提供文件级沙箱。
- 插件进程崩溃可以隔离，但插件仍有宿主 OS 用户权限；go-plugin 不是恶意代码沙箱。
- 插件未就绪时，相关 RPC 返回 `FailedPrecondition`，不偷偷启用备用实现。
- 不丢弃终端字节来应对慢客户端；有界队列溢出断开该 attachment，重连重新建立屏幕状态。
- 文件/端口与终端分队列、限制并发，避免大文件占满交互链路。gRPC 会序列化和复制，不承诺“零拷贝”。

## 设计依据

[02 连接](./02-connection-model.md)、[03 会话](./03-session-management.md)、[04 插件](./04-plugin-system.md)、[09 协议](./09-protocol-definition.md)、[10 实现约束](./10-implementation-details.md) 必须共同满足。扩展多租户、GUI 和 WebSocket 时单独评审，不能沿用单用户信任边界。
