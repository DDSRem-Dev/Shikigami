# 用户体验设计

**本文档展示目标用户体验，具体功能交付以 [08-roadmap.md](./08-roadmap.md) 为准；CLI/TUI 已有开发实现，但完整体验和发行验收尚未完成，当前证据见 [15](./15-implementation-status.md)。**

安装地址、Homebrew 包、版本、耗时、资源数据均为示例，不是发布或性能承诺。阶段范围以 [08 路线图](./08-roadmap.md) 为准；Phase 1 不含同步/端口插件。

CLI 通过本地 shiki-agent 工作；关闭窗口只 detach，已启用的同步和转发继续。显式 `shiki agent stop` 才停止本地后台；远端会话仍保留。首次登录 tsnet 需要浏览器 URL 或受限 auth key，不需要系统 Tailscale。

术语：Active/Idle 表示有无终端连接，Running/Stopped 表示 Claude 进程状态，两者不是一回事。所有会话选择应显示项目/服务器；名称有歧义时要求选择。

## 首次使用流程

### 服务端安装

```bash
# 用户在 VPS 上执行
# 已实现的本地受信 bundle 安装入口；摘要须来自独立可信渠道。
sudo bash scripts/install.sh --user=rem \
  --bundle=/absolute/path/to/verified-bundle --version=VERSION \
  --checksums-sha256=TRUSTED_64_HEX_DIGEST \
  --token-output=/home/rem/shiki-initial-token

# 以下为目标交互示例；当前安装器分别报告节点与应用验收状态，
# 不以 systemd 进程存活或节点 online 代替 application ready。
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Shiki Daemon Installer
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

✓ Detecting system... Ubuntu 22.04 LTS (x86_64)
✓ Downloading shiki-daemon v0.1.0...
✓ Installing to /usr/local/bin/shiki-daemon...
✓ Checking tmux and server-side Claude installation/login...
✓ Creating configuration directory...
✓ Setting up systemd service...
✓ Starting shiki-daemon...
⠋ Complete the displayed embedded tsnet login URL...
✓ Embedded node authenticated and application ready

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Installation complete!
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Your connection token:

  shiki_eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...

⚠ Keep this token secure! You'll need it to connect.

Next steps:

1. Use the authenticated embedded node:
   Shiki node: shiki-vps (100.64.1.2)
   No system Tailscale installation is required.

2. On your local machine, install the client and its background agent.
   Run shiki connect and complete the local agent's tsnet login.

3. Enter the server's Shiki node address and connection token.
   The daemon runs as your selected non-root user.

For help: https://docs.shiki.dev
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

### 客户端安装

```bash
# macOS
brew install shikigami

# Linux（拟议脚本，安装 CLI、agent 和匹配插件）
bash scripts/install-client.sh

# 源码开发也需构建 CLI、agent、两端插件，不能只 go install CLI。
```

### 首次连接

当前智能模式先选择/创建远端项目，明确显示本地目录、服务器与项目，再确认保存暂停的映射。随后可保持暂停，或选择审查远端→本地/本地→远端；方向只是逐项审查偏好，不是批量覆盖授权。只有有效 `file.read`/`file.write`/`file.watch` 与 `sync.revision.v1` 都具备时才提供同步入口，且本地 sync 插件须已就绪，不替用户自动启插件。用户再次确认后启用后台扫描：无共同基线的差异保留为 initial conflict，逐项展示两端存在状态与 generation，可查看 diff、明确确认 keep-local/keep-remote/keep-both、暂不处理或暂停后台。只展示真实 SyncState，不把保存 `sync_enabled=true` 或未知总量当成传输完成。

```bash
cd ~/projects/myapp
shiki

# 输出：
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Shiki - Remote Claude Code Manager
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

✓ Local shiki-agent is ready.
No servers configured. Let's set up your first server!

? Server name (for easy reference): my-vps
? Server address (Tailscale IP or hostname): 100.64.1.2
? Connection token: shiki_eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...

⠋ Connecting to server...
✓ Connected successfully!

Server information:
  OS: Ubuntu 22.04 LTS
  CPU: 4 cores
  Memory: 8 GB
  Daemon version: v0.1.0

? Set as default server? Yes

✓ Configuration saved to ~/.shiki/config.yaml

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Setup complete!
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

No remote sessions found. Create your first session?

Current directory: /Users/rem/projects/myapp

? Project name (default: myapp): myapp
? Remote working directory: /home/rem/projects/myapp
? Enable background sync for this project? Yes
? Review initial sync plan (existing differences will be preserved as conflicts): Confirm

⠋ Creating project...
⠋ Syncing files (15 files, 2.3 MB)...
  [████████████████████████████████] 100%

✓ Files synced successfully

? Create first session
? Session name (default: main): main
? Description (optional): Main development session

⠋ Creating session...
⠋ Creating tmux session...
⠋ Starting Claude Code...

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✓ Connected to remote Claude Code!
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Project: myapp
Session: main
Remote: /home/rem/projects/myapp
Server: my-vps (100.64.1.2)

Plugins:
  • sync: ✓ Active (project mapping, watching 15 files)
  • port: ✓ Active (only explicitly allowed ports auto-forward)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Tips:
  • Press Ctrl+Q to detach (session continues)
  • Files sync automatically between local and remote
  • Allowed remote ports forward to localhost
  • Background sync and forwards continue after detach
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

# 此时进入 Claude Code 界面
Claude Code >
```

## 日常使用流程

### 场景 1：快速重连（智能模式）

```bash
# 第二天，用户打开终端
cd ~/projects/myapp
shiki

# 输出：
⠋ Connecting to my-vps...
✓ Connected

Found existing session for this directory:
  Session: main
  Status: ● Running
  Created: 1 day ago
  Last active: 16 hours ago

⠋ Checking file changes...
✓ No local changes to sync

⠋ Attaching to session...
✓ Attached!

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Project: myapp | Session: main | Server: my-vps
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

# 直接进入之前的 Claude Code 会话
Claude Code > (显示之前的对话历史)
```

### 场景 2：创建新会话（并行开发）

当前 CLI 对已有 attachment 的目标会话显示编号菜单：只读观察、同项目新建、进入 dashboard 管理或退出。菜单按认证协商返回的有效权限显示；未知旧协议权限只允许尝试只读观察，不推断创建或写入权。新建收集名称、项目内工作目录和描述；具备 `file.read` 与 `project.directories.v1` 时，以每页 50 项浏览项目内子目录，支持根目录、父目录、下一页、自定义路径及取消。缺少目录枚举权限/能力时仍可手动输入工作目录，最终均由 daemon 验证包含关系。现有目录映射的默认会话不被这次选择覆盖；没有 attachment 的默认会话仍走快捷连接。

```bash
# 在另一个终端窗口
cd ~/projects/myapp
shiki

# 输出：
⠋ Connecting to my-vps...
✓ Connected

Found project: myapp

Active sessions (1):
  1. main
     ● Running (connected in another terminal)
     Working on: Main development session
     Created: 1 day ago

? What would you like to do?
  Connect to "main" (will be read-only)
❯ Create new session
  Manage sessions
  Exit

# 用户选择 "Create new session"

? Session name: frontend
? Working directory:
❯ /home/rem/projects/myapp (project root)
  /home/rem/projects/myapp/frontend
  /home/rem/projects/myapp/backend
  Custom path within this project...

# 用户选择 frontend 子目录

? Description (optional): Working on React UI

⠋ Creating session "frontend"...
✓ Session created

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Project: myapp | Session: frontend (2/2) | Server: my-vps
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Claude Code >
```

现在用户有两个终端：
- 终端 1：连接到 `main` 会话，处理后端
- 终端 2：连接到 `frontend` 会话，处理前端

### 场景 3：管理多个会话

```bash
shiki sessions

# 输出：
Project: myapp (/home/rem/projects/myapp)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Session      Status       Working Dir           Description          Created   Active
────────────────────────────────────────────────────────────────────────────────────
main         ● Active     .../myapp             Main development     1d ago    5m ago
             👤 MacBook-Pro

frontend     ● Active     .../myapp/frontend    Working on React UI  30m ago   1m ago
             👤 MacBook-Pro

backend      ○ Idle       .../myapp/backend     API development      2d ago    1d ago

experiment   ○ Idle       .../myapp             Testing new idea     3d ago    2d ago
             🏷️  experimental

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
4 sessions | 2 active | 2 idle

Commands:
  shiki @ <session>        - Connect to session
  shiki sessions info <session>    - Show detailed info
  shiki sessions delete <session>  - Delete session
```

### 场景 4：快速切换会话

```bash
# 当前在 frontend 会话
# 按 Ctrl+Q 断开（不关闭会话）

[Detached from session "frontend"]

# 快速连接到另一个会话
shiki @ main

# 或使用交互式选择
shiki attach

? Select session:
❯ main          (● active, 2 clients)
  frontend      (● active, 1 client)
  backend       (idle)
  experiment    (idle)
  [Create new]

# 选择后立即进入
⠋ Attaching to "main"...
✓ Attached

Claude Code >
```

### 场景 5：文件同步实时演示

`--verbose` 已提供独立的诊断展示：dashboard 的 Plugins 面板读取两端宿主脱敏日志，每个插件最多 10 条；终端 detach 并恢复终端模式后读取本地 sync/port 各最多 20 条。raw attachment 期间由宿主保留有界历史，CLI 不向终端字节流插入日志；尾部明确标注不是完整回放。诊断只呈现真实记录，不生成下方示例中的文件进度、字节数或延迟。`--json` 管理命令不会因该标志混入日志。

```bash
# 用户在本地编辑器修改文件
# 本地终端（开启 verbose 模式）
shiki --verbose

# 独立日志面板输出（不插入 raw 终端字节流）：
[sync] Detected change: src/utils.go (modified)
[sync] Uploading to remote... ✓ (1.2 KB in 45ms)

# 同时，远端 Claude Code 可以看到变化
Claude Code > can you help me with this function?

# Claude 读取的是最新的代码

# 反向同步：Claude 创建新文件
Claude Code > I'll create a new file called helper.go

[sync] Remote change detected: src/helper.go (created)
[sync] Downloading to local... ✓ (850 B in 32ms)

# 本地编辑器自动出现新文件
```

### 场景 6：端口转发自动化

先分别配置两端允许规则：client port 配置 `auto_forward: true` 与显式 `auto_allow_ports: [8080]`；server port 配置 `allow_ports: [8080]`。可用 `shiki plugins config port --side local --file client-port.json` 与 `shiki plugins config port --side remote --server my-vps --file server-port.json` 提交 JSON。默认不开启自动模式；需要会话进程归属已被验证，未知来源只能手动操作。`ports` 列表显示 MODE（manual/auto）与实际本地端口；显式停止 auto 记录后，同一进程启动身份在 agent 重启后仍被抑制，新进程可再次参与规则。

```bash
# Claude Code 启动测试服务器
Claude Code > I'll start a test server to verify the API

# 远端执行
$ go run server.go
Server listening on :8080

# 用户已为该项目允许 8080；agent 检测并转发到 loopback
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔌 Port forwarding activated
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Remote port 8080 is now available at:
  http://localhost:8080

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

# 用户在本地浏览器打开
open http://localhost:8080

# 端口管理
shiki ports

Port Forwarding Status
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Session: main
  Remote 8080 → Local 8080  ● Active
    Service: HTTP (Go server)
    Connections: 3
    Data: 1.2 MB

Session: frontend
  Remote 3000 → Local 3000  ● Active
    Service: HTTP (React dev server)
    Connections: 15
    Data: 8.5 MB
```

## 高级使用场景

### 场景 7：会话配置快照和恢复

```bash
# 保存工作目录、描述、标签和启动选项
shiki sessions snapshot main --description "前端开发配置"
✓ Configuration snapshot created: snap_abc123

shiki sessions snapshots
# 显示创建时间、原项目/会话、配置说明；不显示文件变更或备份容量。

shiki sessions restore snap_abc123 --name main-copy
This creates a new session using the saved configuration.
Project files and Claude conversation history will NOT be restored.
? Continue? Yes
✓ Session "main-copy" created in the current project directory
```

两个会话看到同一份当前项目文件；此功能不能撤销重构。文件版本恢复使用用户自己的版本管理工具。

### 场景 8：使用模板快速启动

```bash
# 新项目
mkdir ~/projects/newapp
cd ~/projects/newapp

# 使用已配置的模板，在显式远端目录创建项目和会话。
# 模板引用的远端子目录须已存在；本地 mkdir 不会创建远端目录。
shiki init --template fullstack-dev --name newapp --path /home/rem/projects/newapp

? Template: fullstack-dev
  Description: Frontend + Backend development setup
  Sessions: frontend, backend, database

? Proceed? Yes

⠋ Creating project...
⠋ Creating session "frontend"...
⠋ Creating session "backend"...
⠋ Creating session "database"...
✓ Done!

Project initialized with 3 sessions:
  • frontend - /home/rem/projects/newapp/frontend
  • backend  - /home/rem/projects/newapp/backend
  • database - /home/rem/projects/newapp

Connect to them:
  shiki @ frontend
  shiki @ backend
  shiki @ database
```

### 场景 9：多设备使用

```bash
# 在公司 MacBook 上配置好项目
cd ~/work/myapp
shiki
# ... 工作一天

# 回家，在台式机上
cd ~/projects/myapp  # 不同的本地路径
shiki

? Local directory not mapped to remote project.

Found remote project: myapp (/home/rem/projects/myapp)
Local directory: /home/desktop/projects/myapp

? What would you like to do:
❯ Preview remote → local sync (review existing differences)
  Preview local → remote sync (review existing differences)
  Continue without sync (manual)
  Cancel

# 审查差异并确认后下载；已有不同内容不静默覆盖
⠋ Syncing files...
  [████████████████████████████] 100%
  42 files, 8.5 MB downloaded

✓ Synced successfully
✓ Mapping saved

# 继续之前的会话
? Connect to existing session:
❯ main (last active: 2 hours ago on MacBook-Pro)
  frontend (last active: 3 hours ago on MacBook-Pro)

# 无缝继续工作
```

### 场景 10：健康检查和故障排除

```bash
# 连接出现问题
shiki doctor

Running diagnostics...
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

✓ Local client version: v0.1.0
✓ Config file: ~/.shiki/config.yaml (valid)
✓ Local agent: running
✓ Embedded tsnet: authenticated and online

Server: my-vps (100.64.1.2)
  ✓ Reachable via Tailscale P2P (latency: 45ms)
  ✓ Daemon running (v0.1.0)
  ✓ Token valid (expires in 23 days)
  ✓ Disk space: 45 GB free / 100 GB
  ⚠ Memory: 7.2/8.0 GB used (90%)
  ✓ tmux: v3.3a
  ✓ Claude Code: v1.5.0

Sessions (3):
  ✓ main: healthy
  ✓ frontend: healthy
  ⚠ backend: idle for 3 days (consider cleanup)

Plugins:
  ✓ sync: active (23 files watched)
  ✓ port: active (2 forwards)

Network:
  Connection: Tailscale P2P
  Latency: 45ms (good)
  Bandwidth: 50 Mbps ↓ / 10 Mbps ↑
  Packet loss: 0%

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Overall: Healthy (2 warnings)

Recommendations:
  • Clean up idle session "backend" to free memory
    shiki sessions info backend  # 检查任务后再选择停止/删除
```

## 错误处理示例

### 网络中断

```bash
# 正在使用 Claude Code
Claude Code > help me optimize this function

# WiFi 突然断开

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠ Connection lost to my-vps
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Your session is still running on the server.
  Session: main
  Tmux: shiki-sess_a1b2c3

⠋ Attempting to reconnect...
⠋ Retry 1/5... (next retry in 1s)
⠋ Retry 2/5... (next retry in 2s)
✓ Reconnected!

⠋ Syncing state...
✓ Back online

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

# Claude Code 继续显示响应
Claude Code > Here's how to optimize the function...
```

### Claude Code 崩溃

收到带 attachment/sequence 校验的 `CLAUDE_CRASHED` 后，CLI 先停止该流并恢复终端，再读取会话当前状态；若有界自动重启已使状态回到 running/starting，则连接当前进程，不再发送重启。否则提供权限允许的重启、保留输出/当前屏幕、手动 tmux 命令和退出。重启需要 `session.control` 与再次明确确认；读取输出需要 `session.attach` 和对应能力。退出码无法确认时显示 unknown，不由普通断线推断崩溃。

tmux 已确认受管 pane 结束、但退出状态/信号均不可用时，宿主发送 `CLAUDE_STOPPED` 与 `reason_code=EXIT_REASON_UNKNOWN`。CLI 同样先恢复终端，再显示“进程结束、原因未知”及显式恢复选择，不将其标成已证实崩溃，也不据此自动重启。普通停止/正常退出仍保持已有 attachment 行为。

下方“View full logs”指查看 tmux **实际仍保留的** scrollback 与当前屏幕：当前请求最多 2,000 行、宿主最多 128 KiB，明确标记采样时间、截断和 available/not_retained/unavailable。它不是永久完整历史、Claude 对话恢复或原始终端字节回放；保留输出不足时不会虚构缺失内容。诊断在恢复终端之后展示，控制字符被清理。

```bash
# Claude Code 意外退出

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✗ Claude Code process terminated unexpectedly
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Exit code: 1
Last output:
  Error: Cannot read property 'map' of undefined
  at processFiles (/usr/local/lib/claude/index.js:245)

Session "main" is still active but Claude Code stopped.

? What would you like to do:
❯ Restart Claude Code (new process; conversation recovery not guaranteed)
  View full logs
  Attach to session manually (tmux)
  Exit

# 用户选择重启

⠋ Restarting Claude Code...
✓ Claude Code restarted

⚠ Shiki did not restore conversation history.
  A new Claude process has started; use supported Claude recovery separately if needed.

Claude Code >
```

### 服务器资源不足

```bash
shiki sessions create test --project myapp

✗ Failed to create session

Server resources:
  Memory: 7.8/8.0 GB used (98%)
  CPU: 3.9/4.0 cores used (98%)

Active sessions: 12
  • 8 running Claude Code instances
  • 4 idle sessions

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Recommendations:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

1. Delete idle sessions (free ~500 MB each):
   shiki sessions delete experiment
   shiki sessions delete old-test
   shiki sessions delete prototype

2. Stop Claude Code in idle sessions (keep session):
   shiki sessions stop-claude experiment

3. Check resource usage:
   shiki stats

? Would you like to view resource usage? Yes

# 显示资源使用详情
Session Resource Usage
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Session      CPU    Memory   Last Active
──────────────────────────────────────────
main         25%    1.2 GB   5m ago
frontend     18%    980 MB   1m ago
backend      5%     650 MB   3h ago  ← idle
experiment   2%     520 MB   2d ago  ← idle
old-test     1%     480 MB   5d ago  ← idle

Listed sessions: 51% CPU, 3.8 GB Memory
Other sessions/system processes account for the remaining server usage.
```

## TUI Dashboard（高级功能）

```bash
shiki dashboard

# 显示交互式界面
┌─────────────────────────────────────────────────────────────┐
│ Shiki Dashboard - Project: myapp                      my-vps │
├──────────────────────┬──────────────────────────────────────┤
│ Sessions (4)         │ Session: frontend                    │
│                      │ Status: ● Running                    │
│ ❯ main          ●    │ Working Dir: .../myapp/frontend     │
│   frontend      ●    │ Connected: Yes (this terminal)      │
│   backend       ○    │ Created: 30m ago                    │
│   experiment    ○    │ Last Active: 1m ago                 │
│                      │                                      │
│ [N] New Session      │ Description:                         │
│ [D] Delete           │   Working on React components        │
│ [R] Rename           │                                      │
│ [E] Edit             │ Tags: frontend, react               │
│                      │                                      │
│                      │ Resources:                           │
│                      │   CPU: 18%                           │
│                      │   Memory: 980 MB                     │
│                      │                                      │
│                      │ Recent Output:                       │
│                      │ > Claude Code                        │
│                      │ > User: create login component       │
│                      │ > Claude: I'll create a login...     │
│                      │                                      │
├──────────────────────┴──────────────────────────────────────┤
│ Server Status                                                │
│   Connection: ● Tailscale P2P (45ms)                       │
│   CPU: 51% | Memory: 3.8/8.0 GB | Disk: 45 GB free        │
├──────────────────────────────────────────────────────────────┤
│ [↑↓] Navigate [Enter] Attach [Tab] Switch Pane [Q] Quit     │
└──────────────────────────────────────────────────────────────┘
```

## 命令速查表

`<…>` 为必填值，`[…]` 为可选值。远端命令使用 `--server` 或已配置的默认服务器；会话名称须无歧义，可加 `--project` 限定项目，显式会话命令不会从本地当前目录推断项目。破坏性操作及恢复/初始化/冲突决定在非交互模式需 `--yes`；终端连接须有交互 TTY，不支持 `--json`。

### 连接管理
```bash
shiki                      # 智能连接（基于当前目录）
shiki connect [name]      # 配置或重新验证服务器；缺失参数在交互终端询问
shiki @ <session>         # 快速连接到会话
shiki attach [session]    # 连接到会话；省略会话时交互选择
```

### 会话管理
```bash
shiki sessions list [--project <project>]                 # 列出所选服务器/项目的会话
shiki sessions create <name> --project <project>           # 创建新会话
shiki sessions delete <session>                           # 删除会话
shiki sessions info <session>                             # 查看会话详情
shiki sessions stop-claude <session> [--yes]               # 替换当前pane任务为shell
shiki sessions restart-claude <session> [--yes]            # 替换当前pane任务并重启Claude
shiki sessions snapshot <session>                         # 创建会话配置快照
shiki sessions snapshots                                 # 列出配置快照
shiki sessions restore <snapshot-id> --name <new-name>     # 按配置创建新会话，不回滚文件
shiki sessions delete-snapshot <snapshot-id>              # 删除配置快照记录
```

停止/重启 Claude 会实际替换目标 pane 的当前任务，保留 session 记录与项目文件。非 TTY 必须显式传 `--yes`，缺少时在连接 agent 前拒绝；TTY 使用输入 `yes` 的明确确认，不把选中会话当作操作授权。

### 服务器管理
```bash
shiki servers list        # 列出服务器
shiki servers add [name]                # 添加服务器；交互询问缺失参数
shiki servers update <name-or-id>       # 更新服务器的显式参数
shiki servers remove <name-or-id>       # 删除服务器
shiki servers switch <name-or-id>       # 切换默认服务器
```

### 插件管理
```bash
shiki plugins list --side local                          # 列出本地插件
shiki plugins list --side remote --server <server>        # 列出指定远端插件
shiki plugins enable <plugin-id> --side local             # 启用本地插件
shiki plugins disable <plugin-id> --side local            # 禁用本地插件
shiki plugins status <plugin-id> --side local             # 查看本地插件状态
shiki plugins config <plugin-id> --side local --file <json-file> # 提交完整插件配置
```

所有插件操作必须明确 `--side local|remote`；远端还必须给出 `--server`，本地不接受 `--server`。enable/disable/restart/config 返回实际 worker 状态；保存期望配置不代表 worker 已就绪。

### 监控和调试
```bash
shiki stats              # 性能统计
shiki doctor             # 健康检查
shiki logs <plugin-id> --side local  # 查看宿主脱敏后的插件日志；等同 plugins logs
shiki ports              # 端口转发状态
```

### 配置
```bash
shiki config get                     # 查看有类型偏好快照；--server 指定服务器快照
shiki config set <field> <value>     # 通过 agent 修改一个字段并校验 revision
shiki config edit                    # 用 VISUAL/EDITOR 编辑私有 JSON 快照
shiki config edit --file <json-file> # 应用当前用户所有、0600 的 JSON 快照；保留 revision
```


### 本地后台与控制权

```bash
shiki agent status        # 查看本地后台状态
shiki node status         # 查看内嵌节点状态
shiki agent stop          # 停止本地同步/监听，远端会话保留
shiki agent start --recover-crash-loop  # 检查日志并修复原因后，明确解除崩溃熔断
shiki sync status [mapping-id]    # 查看映射、积压和冲突
shiki sync pause [mapping-id]     # 暂停映射
shiki sync resume [mapping-id]    # 重新对账后继续
shiki sync conflicts [mapping-id] # 获取冲突 ID 和当前 generation
shiki sync resolve <conflict-id> --mapping <mapping-id> --generation <generation> --choice <keep-local|keep-remote|keep-both>
shiki ports stop <id>     # 停止本地转发监听
```

`sync` 省略映射 ID/`--mapping` 时采用包含当前目录的最具体映射；脚本应显式指定映射。冲突决定只应用到观测到的 generation，过期决定会被拒绝，不自动覆盖另一版本。配置读/编辑接口不导出凭据；`logs` 只读所选宿主的插件日志，不表示完整 Claude 会话历史。

观察者可查看输出；若具备权限，界面提供“接管输入”操作并通知旧控制者。只读 Token 不显示该操作，服务端也必须拒绝。控制者断开不自动提升只读观察者。

同一版本在五分钟内连续异常结束三次后，agent 保留诊断 IPC，状态为 `degraded / AGENT_CRASH_LOOP`，不启动节点和后台 worker；普通自动启动不能显示 ready 或解除熔断。用户先查看 `agent status`、私有 `agent.log` 或服务日志并修复原因，再显式执行上述恢复命令。正常停止诊断进程不清除熔断；有效的显式恢复或切换构建版本才重新建立启动预算。若启动记录损坏或系统时间回拨，则保留证据并提示修复，不能通过恢复参数覆盖无效记录。通过 CLI 恢复服务安装后，先停止手动启动的 agent，再由原用户服务/LaunchAgent 启动，以恢复服务管理归属。

删除会话提示结束哪些进程/连接，不询问是否删除项目文件；文件清理由独立操作完成。用户进入无窗口后台运行模式后，可通过上述命令查看和暂停任务。

### 停止会话的清理提醒

默认不会自动删除会话。启用 daemon `auto_cleanup` 后，只有明确 stop、真实 pane 已死亡且无 attachment 的会话才进入观察；StopClaude 留下的 shell 仍运行时，doctor/TUI 明确显示“pane 仍运行，未安排自动清理”，用户退出 shell 后才可能进入预警。

`shiki doctor --server <server-id>` 在拥有 `session.list` 权限时独立读取清理提醒，不依赖 `system.info`；JSON 的 `cleanup` 映射按 session ID 提供提醒。TUI 每五秒刷新会话列表，在选中会话详情优先显示最早清理时间；不足视口仍以明确截止时间开头，完整说明可用 doctor 查看。预警不写入正在附着的终端字节流；离线用户下次查询可见当前持久计划，不承诺离线推送。重新附着或重启 Claude 可取消计划；重启 daemon 会延后观察期限，不等同于重启 Claude。

默认关闭时，对超过七天未有已记录用户活动的 stopped 且未连接会话仅给出人工复核建议，明确要求检查活任务，不能由“无人连接”推断 Claude 空闲或直接删除。实际清理仅删除会话记录和已确认死亡的 tmux 资源，保留项目文件。

### 路由尝试诊断

`shiki servers list`、`shiki doctor` 与 Dashboard 诊断面板均显示最近一次路由选择的实际尝试顺序；doctor 和 Dashboard 另显示当前网络路径，servers list 的 JSON 也保留独立路径字段。列表只可能包含 `tsnet`、`direct-tls`，不带地址、Token 或原始错误；空值显示 `Latest route selection: not reported`。它不代表已连通，不累计全部重试，也不把 tsnet 内部 P2P/DERP 当两个候选。旧 agent 未报告该字段时不根据已配置地址猜测尝试；认证或信任拒绝后未执行的回退不会显示。

`shiki connect` 等待验证时，若观察到鉴权失败、协议不兼容或重连耗尽，终态错误同时显示该次观测中的尝试列表与恢复建议。调用者取消或等待超时立即结束，不为了补充诊断追加查询或延长连接预算。


## 本地节点诊断与不可用清单

`shiki node status`（包括不带子命令的 `shiki node`）、doctor 和 Dashboard 从运行中的 NodeControl 读取状态，不用前端当前磁盘配置推断节点是否启用。关闭 tsnet 时显示 `offline / NODE_DISABLED`；身份状态不可用时显示实际 `NODE_STATE_UNAVAILABLE`，保留原状态供修复。只读查询不要求登录能力，也不触发登录；登录与取消仍需对应能力。

agent 业务资源不可用时，doctor 继续报告独立节点诊断。服务器清单失败在 JSON `errors.servers` 保留错误且 `servers` 为 null，文本明确显示 `Registered servers: unavailable`，不把未知清单写成零台。成功输出诊断报告不代表 agent 或远端已 ready；真实协议不兼容、实例变化和权限错误仍须如实呈现。
