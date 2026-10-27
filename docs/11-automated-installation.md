# 自动化安装与运维契约

**本文档是安装流程设计，安装脚本尚未实现和测试；下列命令与 service unit 均不是已验证的发布产物。**

当前没有可执行安装器、发布产物或正式下载地址。本文件是安装器的实现和验收规范，替代原先会安装两套 Tailscale、以 root 运行 daemon、覆盖配置并暴露明文端口的示例脚本。

## 部署模型

- 服务端首版：Linux + systemd，amd64/arm64；macOS/Linux/WSL 是客户端平台。原生 Windows 客户端/GUI 后续验收。
- 内嵌 tsnet：daemon 和本地 agent 自行注册/维护节点，不安装/启停系统 tailscaled，不修改机器原有 Tailscale 配置。
- 安装文件和 system service 可需 sudo；daemon、插件、tmux、Claude 用指定普通用户运行。禁止默认 User=root。
- 默认仅 tsnet listener :9527（Connect/protobuf over h2c），不监听普通网卡 `0.0.0.0:9527`。显式启用公网直连必须同时提供有效 TLS 配置并协商 HTTP/2；不为 Connect 安装额外 gRPC-Web 代理。
- 首次加入 tailnet 仍需登录 URL 或预先提供的 auth key；“自动化安装”不等于免认证。Claude 登录也使用其官方流程，不生成或代管用户账号凭据。

## Phase 0 必须验证的原型

锁定 tsnet SDK 版本后，以最小宿主验证下表；完成前不把 API 名称或预期状态当成实际能力。完整发行版安装器仍在 Phase 4 实现。

| 原型 | 要验证的事实 | 未通过时的门槛 |
|---|---|---|
| 节点启动与登录 URL | 未入网时能先启动本地 NodeControl，再通过 SDK 的实际登录/状态 API取得有效 URL；不因 Listen/Dial 等待登录而阻塞管理通道 | 禁止承诺无人值守首次安装 |
| 一次性 key | 无效/过期/复用 key、审批拒绝的真实错误与状态；key 不写日志、状态导出或命令行 | 保留手动认证流程并阻止显示 ready |
| 节点身份持久化 | 正常重启复用同一身份/地址；目录锁、权限错、状态损坏不制造新节点 | 禁止自动恢复后台同步/转发 |
| 认证中断 | URL 过期、取消、节点撤销和控制面离线可区分；不能区分时报告 unknown，不猜测 | 无法确认授权时暂停任务，不绕过认证回退 |
| 本地引导接口 | [13](./13-local-ipc-protocol.md) 的 NodeControl 在 agent/daemon 各自私有 socket 上工作，UID 校验、协商、取消正确 | 安装器不能依赖不存在的管理接口 |
| Connect 传输 | 本地 NodeControl 使用 UDS/h2c；tsnet 上实际全双工；可选 TLS 协商 h2、证书拒绝及流式终态正确，见 14 | 禁止以 TCP 端口可达或普通 HTTP 200 显示安装成功 |
| 服务进程保活 | systemd crash/restart 保留 tmux，同时插件/PTY attach 可回收；普通用户和真实 HOME/PATH 可用 | 保活承诺不得进入安装成功提示 |

当前以上运行结果均未验证。原型应记录 SDK/OS 版本、步骤和输出，不记录 auth key 或登录 URL；只有通过后才将对应流程接入自动化安装。

运行预构建产物不要求用户安装 Go/protoc；源码构建要求 Go ≥ 1.26.0，并锁定 Connect-Go 运行库与生成器 v2.0.2。安装器检查发行清单中的核心/插件应用协议能力；不能把 Connect 库版本当作 Shiki 协议 major。

## 安装流程

1. 解析并验证全部参数。未知参数失败；非交互缺少运行用户、必要配置或认证材料时立即失败，不尝试 read 或进入无限等待。
2. 检查 OS/arch/systemd、目标用户及 home、磁盘/权限、tmux、受支持的 Claude 可执行文件和登录可用性。只安装缺少且用户选择的依赖；Claude 安装/授权由官方流程完成并在结果中区分“已安装”和“已登录”。
3. 下载固定版本 manifest、daemon 与匹配服务端插件到临时目录，验证受信发布来源和校验信息后原子安装。单一未认证 checksum 不是签名信任链；发布前明确签名/校验分发方式。
4. 初始化配置与目录：`/etc/shiki/daemon.yaml`、`/var/lib/shiki/`、插件和 tsnet 状态目录。由指定用户所有，敏感文件 0600、状态目录 0700。已有配置/密钥只验证和迁移，不覆盖或重新生成。
5. 以运行用户身份一次性初始化 daemon ID、签名密钥、状态 schema。管理命令和 service 必须显式传同一 `--config`，否则可能生成另一把密钥导致 Token 全失效。
6. 安装服务并启动 daemon；先让本地 NodeControl 可用；在有界时间内检查进程存活、管理通道 ready 与依赖状态，若 needs_login 则执行下一步，不能在等待 tsnet online 时阻塞登录引导。`systemctl is-active` 不是应用 ready 的充分条件。
7. tsnet 未登录时通过 NodeControl.BeginLogin/GetStatus 获取登录 URL，显示给用户；无浏览器的 VPS 由用户在另一台设备打开。auth key 从 0600 文件/受限环境注入启动流程，完成后清除临时材料，不留在常规配置/日志/命令行参数。
8. 登录成功后再检查 tsnet online、已启用插件和应用能力；获取内嵌 tsnet 节点的真实地址，生成单独设备 Token，向当前 TTY 显示一次，或非交互写入用户指定的 0600 输出文件。不要把 Token 放进 journal。
9. 失败时区分依赖缺失、登录待完成、应用未 ready；保留诊断与原版本，不宣称安装成功。新安装的临时资源可清理，不能误删已有项目或账号状态。

## 参数契约（尚待实现）

| 参数 | 行为 |
|---|---|
| `--user=rem` | 指定已存在的非 root 运行用户；无法可靠推断时必填 |
| `--version=<version>` | 固定发布版本，与 manifest 匹配 |
| `--config=/etc/shiki/daemon.yaml` | 初始化和所有管理/服务命令使用同一路径 |
| `--port=9527` | tsnet 内 ConnectRPC 端口，范围 1–65535 |
| `--tailscale-key-file=<path>` | 从受限文件读取一次性 key，非交互登录用 |
| `--non-interactive` | 绝不读取 stdin/TTY，必要参数缺失返回非零 |
| `--token-output=<path>` | 非交互时写入初始 Token 的私有文件，不输出到公共日志 |
| `--direct-tls-address/--tls-cert/--tls-key` | 三项一起启用可选直连；缺少任一项拒绝 |

没有 `--skip-tailscale` 自动明文降级，也不使用 `--tailscale-key=<secret>` 暴露凭据。自定义配置目录必须安全创建并保持所有权。

## 交互方式

最终发布后建议先下载/验证安装器，再从文件执行 Bash；不要把需要 read 的脚本通过 stdin 管道执行，避免把后续脚本正文读成回答。

```bash
# 仅说明拟议调用形式；scripts/install.sh 当前不存在。
sudo bash scripts/install.sh --user=rem --config=/etc/shiki/daemon.yaml
```

确实需要管道安装时，所有交互从 `/dev/tty` 读取（不存在则失败），secret 输入不回显。脚本使用 Bash 时文档不能写 `| sh`。实现使用 `set -euo pipefail` 之外仍需显式检查预期失败、trap 清理、shell 安全引用；不能把参数解析代码独立列出却不用于真实配置和服务命令。

## systemd 与会话保活

服务模型是“停止/重启 daemon 保留 tmux 会话”；主机重启不保证进程存活。systemd 默认 KillMode=control-group 会杀死同 cgroup 的 tmux/Claude，必须显式处理。

```ini
# 安装器按已验证的用户/home/config 渲染；此处 rem 只是示例。
[Unit]
Description=Shiki Daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=rem
Group=rem
Environment=HOME=/home/rem
WorkingDirectory=/home/rem
ExecStart=/usr/local/bin/shiki-daemon serve --config=/etc/shiki/daemon.yaml
Restart=on-failure
RestartSec=5s
TimeoutStopSec=15s
# 有意保留独立 tmux；必须实现下述子进程清理和验收。
KillMode=process
PrivateTmp=false
NoNewPrivileges=true
UMask=0077
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

daemon 收到停止信号关闭 listener、网络流、PTY attach 与插件，保留 tmux。插件在 Linux 配置父进程死亡信号或等价监管机制，防止 daemon 崩溃后孤儿插件继续写盘；tmux server 不继承该死亡策略。重启前核对受管遗留进程，不能启动重复 watcher。

KillMode=process 是为保活的有意取舍，systemd 不再自动回收所有子进程；必须实测 crash/restart/stop、插件孤儿清理及主机 shutdown。若运行环境无法可靠满足，改用独立受管 tmux service/scope 后再交付保活承诺。

PrivateTmp=false 使手动 tmux 与 daemon 使用同一用户的稳定 socket；socket 目录权限必须保护。若使用完整私有路径则统一 `tmux -S`，不能同时使用 `-L /tmp/path`。显式销毁会话用应用命令，不等同于停止 daemon。

## 本地 agent 安装

本地以当前普通用户安装 CLI、agent、匹配插件。macOS 使用 LaunchAgent，Linux/WSL 使用 systemd --user（不具备该条件时提供显式 agent start/stop）。后台安装和启动是本地客户端安装流程的一部分；是否开机/登录自动启动在配置中显示。

首次 CLI 自动启动/发现 agent；agent 独占状态目录锁并暴露受限 IPC。agent 注册自身 tsnet 节点，UI 显示登录链接，不额外安装 Tailscale app。后台运行意味着用户关闭 CLI 后同步/转发继续；`shiki agent stop` 显式停止。

agent 必须使用与交互 shell 一致且明确的配置路径；launchd/systemd 环境 PATH 不等于用户 shell，安装器不能假定能找到用户手工安装的二进制。

## 升级、卸载与恢复

- 升级备份配置/状态、验证版本兼容性、停止相应宿主和插件，原子切换产物，保留旧版本便于回滚；远端 tmux 会话保留，网络流重建。
- 日志默认 journal，不同时声称写入未创建的 `/var/log/shiki/daemon.log`。
- 默认卸载只停止宿主并移除程序/服务，不删除项目目录、配置、Token 状态或 tmux 会话。可另行显式选择清理状态/结束会话，列出后果。
- 撤销或删除 Tailscale 节点需明确选择；不触碰系统 tailscaled。
- 不通过额外第三方 IP 查询服务推断可用直连地址；公网连接信息来自明确配置和 TLS 检查。

## 安装验收

干净 Linux 用户安装、已有系统 Tailscale、重复安装、自定义 config/port、非交互缺参数、无 TTY、下载中断、校验失败、已有会话升级、Token 重启仍有效、tsnet 重新登录、daemon crash 保活及插件回收，均需自动化验证。认证失败与服务安装成功分开报告。
