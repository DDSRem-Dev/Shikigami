# 用户体验设计

**本文档展示目标用户体验，具体功能交付以 [08-roadmap.md](./08-roadmap.md) 为准；当前尚无实现。**

安装地址、Homebrew 包、版本、耗时、资源数据均为示例，不是发布或性能承诺。阶段范围以 [08 路线图](./08-roadmap.md) 为准；Phase 1 不含同步/端口插件。

CLI 通过本地 shiki-agent 工作；关闭窗口只 detach，已启用的同步和转发继续。显式 `shiki agent stop` 才停止本地后台；远端会话仍保留。首次登录 tsnet 需要浏览器 URL 或受限 auth key，不需要系统 Tailscale。

术语：Active/Idle 表示有无终端连接，Running/Stopped 表示 Claude 进程状态，两者不是一回事。所有会话选择应显示项目/服务器；名称有歧义时要求选择。

## 首次使用流程

### 服务端安装

```bash
# 用户在 VPS 上执行
sudo bash scripts/install.sh --user=rem  # 拟议命令，脚本尚未实现

# 输出：
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
  shiki sessions info      - Show detailed info
  shiki sessions delete    - Delete session
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

# 使用模板初始化
shiki init --template fullstack-dev

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
shiki sessions create test

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

### 连接管理
```bash
shiki                      # 智能连接（基于当前目录）
shiki connect             # 首次配置服务器
shiki @ <session>         # 快速连接到会话
shiki attach <session>    # 连接到会话（交互式）
```

### 会话管理
```bash
shiki sessions list       # 列出所有会话
shiki sessions create     # 创建新会话
shiki sessions delete     # 删除会话
shiki sessions info       # 查看会话详情
shiki sessions snapshot   # 创建会话配置快照
shiki sessions restore    # 按配置创建新会话，不回滚文件
```

### 服务器管理
```bash
shiki servers list        # 列出服务器
shiki servers add         # 添加服务器
shiki servers remove      # 删除服务器
shiki servers switch      # 切换默认服务器
```

### 插件管理
```bash
shiki plugins list        # 列出插件
shiki plugins enable      # 启用插件
shiki plugins disable     # 禁用插件
shiki plugins status      # 查看插件状态
```

### 监控和调试
```bash
shiki stats              # 性能统计
shiki doctor             # 健康检查
shiki logs               # 查看日志
shiki ports              # 端口转发状态
```

### 配置
```bash
shiki config get         # 查看配置
shiki config set         # 修改配置
shiki config edit        # 编辑配置文件
```


### 本地后台与控制权

```bash
shiki agent status        # 查看本地后台和节点状态
shiki agent stop          # 停止本地同步/监听，远端会话保留
shiki sync status         # 查看映射、积压和冲突
shiki sync pause          # 暂停当前映射
shiki sync resume         # 重新对账后继续
shiki sync resolve        # 处理积压冲突
shiki ports stop <id>     # 停止本地转发监听
```

观察者可查看输出；若具备权限，界面提供“接管输入”操作并通知旧控制者。只读 Token 不显示该操作，服务端也必须拒绝。控制者断开不自动提升只读观察者。

删除会话提示结束哪些进程/连接，不询问是否删除项目文件；文件清理由独立操作完成。用户进入无窗口后台运行模式后，可通过上述命令查看和暂停任务。
