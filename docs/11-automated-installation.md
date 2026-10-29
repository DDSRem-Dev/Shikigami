# 自动化安装与运维契约

**本文保留安装流程契约。仓库已实现本地固定版本 bundle 校验、事务安装/回滚、服务渲染、异步登录引导与本地就绪诊断；真实 systemd 容器与原生 macOS LaunchAgent 已有下述限定环境生命周期证据；首个公开预发布 v0.0.1-alpha.1 已由 tag 自动构建、签名和发布。原生 VPS/重启登录持久化、外部连接、完整平台与 Homebrew 分发仍需单独验收。**

`scripts/install.sh`、`install-client.sh`、`uninstall.sh` 与短时运维 helper `shiki-install` 已存在；已完成专用公钥的四目标签名及验签。[首个公开 Release](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-alpha.1) 来自提交 `f0b5f83dce599c8e0d715bda5e7806a0ef6aaa72`、成功自动发布运行 `40895007350`，公开资产的下载验证结果见[发行证据](./15-implementation-status.md#evidence-public-alpha-release-20261029)。安装只接受本地 bundle、固定版本及调用者从独立可信渠道取得的 checksum pin。可信源码工具 `scripts/release-trust.go fetch` 提供显式HTTPS归档/签名地址、独立可信公钥和固定版本/平台的下载验签入口，先认证签名再下载归档，成功后生成私有副本/pin/receipt；解包与安装仍是显式后续步骤，工具不指定正式来源或建立公钥信任。GitHub下载地址会重定向，现有 `fetch` 拒绝重定向；应使用 `gh release download v0.0.1-alpha.1 --repo DDSRem-Dev/Shikigami` 取回资产，再用可信源码 verifier 和独立可信公钥离线验签。命令与限制见发布认证，本批验收进度见16。下列完整就绪和运维要求不因预发布成功而降低。

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

本地 NodeControl、固定 SDK 的测试控制面登录/身份保持、真实 tsnet/UDS/TLS 传输和普通用户宿主崩溃恢复已有运行证据，详见 15/16。真实 systemd PID1 容器的服务保活和普通用户 user-agent、原生 macOS 当前 GUI 登录域的 LaunchAgent 生命周期已有下述证据；官方控制面的 key 生命周期/审批/撤销、原生 VPS/重启登录持久化和完整外部就绪仍未验证。原型必须记录 SDK/OS 版本、步骤和输出，不记录 auth key 或登录 URL；只有相应门槛通过后才可宣称该安装流程已验收。

运行预构建产物不要求用户安装 Go/protoc；源码构建要求 Go ≥ 1.26.5，并锁定 Connect-Go 运行库与生成器 v2.0.2。安装器检查发行清单中的核心/插件应用协议能力；不能把 Connect 库版本当作 Shiki 协议 major。

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

## 参数契约

| 参数 | 行为 |
|---|---|
| `--user=rem` | 指定已存在的非 root 运行用户；无法可靠推断时必填 |
| `--version=<version>` | 固定发布版本，与 manifest 匹配 |
| `--config=/etc/shiki/daemon.yaml` | 初始化和所有管理/服务命令使用同一路径 |
| `--port=9527` | tsnet 内 ConnectRPC 端口，范围 1–65535 |
| `--tailscale-key-file=<path>` | 从受限文件读取一次性 key，非交互登录用 |
| `--non-interactive` | 绝不读取 stdin/TTY，必要参数缺失返回非零 |
| `--token-output=<path>` | 非交互时写入初始 Token 的私有文件，不输出到公共日志 |
| `--issue-token` | 显式要求就绪后发行新的 daemon 设备 Token；交互输出到控制 TTY，非交互必须同时指定 token-output。普通 probe 不发行 Token |
| `--direct-tls-address/--tls-cert/--tls-key` | 三项一起启用可选直连；缺少任一项拒绝 |

没有 `--skip-tailscale` 自动明文降级，也不使用 `--tailscale-key=<secret>` 暴露凭据。自定义配置目录必须安全创建并保持所有权。

## 交互方式

先下载并验证安装器，再从文件执行 Bash；不要把需要 read 的脚本通过 stdin 管道执行，避免把后续脚本正文读成回答。

```bash
# 在目标 Linux 主机上明确部署；bundle 与 pin 必须已验证。
sudo bash scripts/install.sh --user=rem --config=/etc/shiki/daemon.yaml \
  --version=<固定版本> --bundle=/absolute/verified-bundle \
  --checksums-sha256=<独立可信的SHA256SUMS摘要>
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

`shiki config startup` 通过已运行 agent 的只读 `GetStartupPolicy` 显示启动策略，不隐式拉起 agent、不修改服务。`cli_auto_start` 是该 agent **启动时加载**的按需启动配置，`config_path` 标识来源；外部随后修改磁盘配置时，它不是磁盘的新值。`config get/set/edit` 继续只处理可往返的 Preferences，不混入这份只读报告。agent 离线时先由用户明确 `shiki agent start`；旧 agent 未协商 `agent.startup.v1` 时明确不支持。

服务观察限于当前配置绑定的标准 Shiki 受管服务，不枚举或推断用户自定义自动化。Linux 只读核对 user unit 的已加载来源、无 drop-in/待 reload、受限配置语法、当前 config 和持久 default.target 启用链接；`enabled-runtime` 等其他状态为 unknown。登录启动与开机启动分别报告，只有持久启用且 `loginctl` 实际返回 Linger=yes 才显示 boot enabled；Linger 无法读取时仅 boot unknown。macOS 以当前普通用户的标准 LaunchAgent plist、RunAtLoad 和 `launchctl print-disabled` 覆盖共同判断，LaunchAgent 本身不是开机级服务，boot 为 not_applicable。复杂或未知配置、读取/管理器失败均不猜测为 enabled。标准受管服务 absent/disabled 只说明这一受管策略未启用，显式手动 agent start 仍可用，不排除其它自动化。

报告为有限时间内的配置观察，不是跨文件/服务管理器的原子快照，也不是实际重新登录或重启验收。每个 agent 同时最多一项查询，忙时立即返回 ResourceExhausted/STARTUP_OBSERVATION_BUSY，不建立后台等待队列；每次总查询预算 3 秒，文件/命令输出各限 64 KiB，不返回原始命令、环境、stderr 或凭据。持续变化或无法确认配置绑定时为 unknown；不会因进程存在、父 PID 或环境变量猜测启用。


## 升级、卸载与恢复

- 升级备份配置/状态、验证版本兼容性、停止相应宿主和插件，原子切换产物，保留旧版本便于回滚；远端 tmux 会话保留，网络流重建。
- 日志默认 journal，不同时声称写入未创建的 `/var/log/shiki/daemon.log`。
- 默认卸载只停止宿主并移除程序/服务，不删除项目目录、配置、Token 状态或 tmux 会话。可另行显式选择清理状态/结束会话，列出后果。
- 撤销或删除 Tailscale 节点需明确选择；不触碰系统 tailscaled。
- 不通过额外第三方 IP 查询服务推断可用直连地址；公网连接信息来自明确配置和 TLS 检查。

## 安装验收

干净 Linux 用户安装、已有系统 Tailscale、重复安装、自定义 config/port、非交互缺参数、无 TTY、下载中断、校验失败、已有会话升级、Token 重启仍有效、tsnet 重新登录、daemon crash 保活及插件回收，均需自动化验证。认证失败与服务安装成功分开报告。


## 本地实现入口与仍待验收的部署边界

`make release` 打包当前固定版本的本地产物、MIT `LICENSE` 及校验清单；清单内同时约束 OS/arch、协议 major、状态/配置 schema 和双端插件能力。许可证本身也在 payload 摘要清单中。`make build-embedded` 生成按目标平台嵌入的插件，开发构建不依赖生成目录。`shiki-install` 是一次性的安装/运维 helper，不是第四个常驻宿主。

安装新增 `--bundle`、`--checksums-sha256`、`--prefix`、`--state-dir`、`--plugins-dir`、`--bin-dir`、`--path`、`--claude-executable` 与 `--stage-only`。stage 只复制校验通过的不可变版本并生成可审查 service 文件；不写实际配置、不注册服务、不登录、不显示 ready。checksum pin 必须由操作者独立确认，不能将对未认证下载自行计算的 hash 冒充发布者签名。

实际激活以普通运行用户初始化配置/身份。NodeControl 可达后，BeginLogin 的异步结果会通过只读 GetStatus 轮询，直到取得 URL、online、明确失败或有界截止；已有登录 attempt 不会重复 BeginLogin。登录 URL 只显示到控制 TTY，不进入 JSON、日志或 receipt。JSON 保留节点真实地址和 reason_code。全新配置的非交互激活需要 key；已存在的有效私有配置保留原节点身份，也允许已有显式 TLS-only 配置，缺少登录时返回 pending，不凭空要求一个不会使用的 key。

显式关闭 tsnet 时，agent 与 daemon 的 NodeControl 均保留协议约定的 `offline` 状态并报告 `NODE_DISABLED`，不扩展状态集合为 `disabled`。只有安装配置明确关闭 tsnet（daemon 还必须启用显式 TLS listener）且精确收到这个状态/原因组合，安装器才继续独立应用就绪检查；其他 offline 原因始终 pending，最终节点复查使用同一规则。

daemon 的 admin.sock 另注册独立只读 DaemonReadiness（见 13），不扩大 NodeControl 或远端业务权限。安装器绑定选中 release/helper build、两套本地 instance 与 daemon_id，检查真实 state store、listener、已启用插件当前实例 Health 和实际能力。client 使用独立 Agent 握手验证选中 build，再确认状态/已启用插件，最后重查原 Node 实例，不能把中途重启的另一个宿主当作原版本成功。pending receipt 或 current/receipt 不一致时，probe 必须先拒绝，要求显式 rollback。

Claude 版本通过受限 `--version` 查询；登录使用运行用户 HOME/PATH 下固定 `auth status --json`，3 秒、4 KiB 上限，只保留严格布尔 loggedIn 对应的 reported_logged_in/reported_logged_out。未知、不支持或无法可靠读取仍是 unknown，应用条件不满足返回具体 pending 原因。这个 CLI 报告不等于远端 Claude 账户端到端已验证。

节点与本地条件满足时返回 `local_application_ready`；`external_peer_acceptance=pending` 始终独立保留，本地结果不能改称“整套安装已验收”。节点登录或应用条件未满足返回退出码 3（node_* / application_pending），不再无条件把 online 变成同一个 application_pending。退出码 0 只表示所报告的本地产物/本地条件操作完成。

首次 daemon 激活在本地条件满足后发行初始设备 Token：交互仅写控制 TTY，非交互要求新建 0600 token-output 文件。若首次登录尚未完成，之后显式执行 `probe --issue-token` 或 `probe --token-output=/private/new-device.token` 继续发行；普通 probe、升级或重复安装不会隐式发行。重复同一 bundle 时携带发行选项会拒绝并提示使用 probe，避免默默忽略意图。已有输出文件不覆盖，输出失败由 Token 命令撤销本次已发行凭据。

配置/状态备份为 0600、有总量和条目边界，活跃 writer lock 阻止备份；回滚切换旧产物，不自动恢复可能撤销过 Token 的旧认证状态。服务管理器的限定环境验收不替代外部 tailnet/VPS、官方控制面生命周期和完整就绪要求。

2026-10-29安装非干扰补验：独立Linux systemd容器中的真实tailscaled在Shiki双角色安装/升级/回滚/卸载/重装等21边界保持原进程、unit/config/socket与状态文件；另一个普通用户的内嵌tsnet启用后真实返回needs_login/NODE_AUTH_REQUIRED，安装器退出3，独立私有state且访问系统socket/state均EACCES。系统tailscaled从锁定v1.98.10源码构建，版本后缀如实保留；其状态是2字节空JSON，不能外推已登录账户身份保持或已认证双节点共存。实际poweroff及精确清理通过，见[共存报告](15-implementation-status.md#evidence-tailscale-coexist-validation)。

原生macOS当前GUI登录域另完成开发A529/B630客户端的实际schema升级、旧A拒写、停写后仅恢复sync子树、再次升级及双向同步；30次真实RPC验证恢复未复活已撤销Token。最终宿主/worker代际与正常清理已独立复核。独立daemon为普通TLS测试进程，不是macOS daemon服务；不代表任意新写后恢复旧备份、历史正式发行或重新登录/物理重启。见[原生schema报告](15-implementation-status.md#evidence-schema-macos-validation)。

2026-10-29另完成同容器持久卷下第二次真实systemd PID1启动：正常poweroff后重新start，daemon与启用linger的user-agent自动启动，CLI自动启动关闭；身份、凭据、映射/基线/冲突/暂停与转发保持，既有基线双向自动同步、新路径显式首次选向、新TCP及仅配置快照恢复通过。两host/四worker的UID/产物/代际、两轮正常关机与精确容器/卷清理独立核验。Docker内核未重启，不能替代物理VPS或原生登录持久化；见[二次启动报告](15-implementation-status.md#evidence-service-boot-validation)。

2026-10-29限定补验已实际验证撤权保持：Linux systemd的A529/B630开发client跨schema恢复矩阵中，原有效Token在5阶段持续成功；备份后签发的候选Token先证明可用，撤销后、受控恢复前后及最终升级均由同一daemon通过已验证TLS/HTTP2拒绝。共30次实际RPC，18次成功、12次远端Unauthenticated，正常退出与清理已独立核验。该操作只恢复停写的sync子树，未新增产品自动状态恢复；完整边界见[跨schema服务报告](15-implementation-status.md#evidence-schema-service-upgrade-validation)。


## 已验证的真实服务管理器边界

`scripts/test-systemd.sh` 在 Docker Desktop 的独立 Linux arm64 容器内运行真实 systemd 252.39 PID1/cgroup v2，而不是 mock systemctl 或直接拉起进程。运行时无外网、私有 PID/cgroup namespace，只额外增加 `SYS_ADMIN` 以重挂容器自己的 cgroup 子树；不使用 privileged、宿主 cgroup/Docker socket 挂载或可写源码。只读源码快照在容器内独立构建，实际安装器注册生成 unit，daemon 和 Linux user-agent、tmux、双端 sync worker 均以 UID1000 运行。真实 stop/start/restart、daemon SIGKILL 自动恢复、升级/回滚保留原 Token/daemon_id/同一 pane PID、卸载保留配置/状态/项目/Token/tmux、重装和最终 systemd shutdown 已通过。user-agent 同样验证了 SIGKILL 自动恢复、升级/回滚、卸载/重装以及原远端凭据和配置/状态保留；另有激活后受控 SIGTERM 取消时真实 systemd 正常退出、所属容器已删除的清理证据。完整命令、证据和限制见 [SYSTEMD-VALIDATION](15-implementation-status.md#evidence-systemd-validation)。

systemd 的 `WorkingDirectory` 使用规范 `~`（按 `User=` 或用户 manager 解析真实 home），不能套用 ExecStart 的 shell 风格引号。`HOME` 与 `PATH` 仍显式设置。插件目录属于安装器管理的可执行产物，允许指向 root-owned 不可变 release；daemon 初始化不再把它创建/强制改成 0700 本用户状态目录。manifest/二进制 owner、mode、摘要与协议校验保持，插件可写状态和 socket 仍在独立私有目录。

`scripts/test-launchd.sh` 已在 macOS 27.0.1 arm64、真实 UID501/HOME 和当前 GUI domain 验证原生 LaunchAgent bootstrap、SIGKILL 自动重启/新实例与旧 frontend 拒绝/旧插件回收、正常 agent stop 后 15 秒不重生、升级/回滚与卸载数据保留；另有受控 SIGTERM 取消后的 job/plist 清理证据。使用独立临时配置/产物且预检拒绝已有标准 job/plist。完整命令与证据见 [LAUNCHD-VALIDATION](15-implementation-status.md#evidence-launchd-validation)。这些结果不证明 reboot/login 持久化、已有用户安装迁移、其他 OS/arch、外部认证或生产长稳。两类流程均显式关闭 tsnet；systemd 场景的 Claude 是明确测试替身，`external_peer_acceptance` 仍为 pending。

运维helper提供 `shiki-install --help`、`help <command>` 及各命令 `--help`；帮助在账号、凭据、配置、安装目录和服务访问之前返回0。参数错误仍返回2，`version` 不忽略多余参数。帮助不执行依赖安装或登录。公开预发布、问题反馈入口与升级限制见 [18 发布准备](./18-release-readiness.md)；预发布不代表全部平台与交付门槛已完成。

`--stage-only` 只适用于 install；rollback/uninstall/status/probe 不接受它或安装布局/产物参数，避免忽略参数后执行与预期不同的操作。维护操作从 role/user/prefix 选中的受管 receipt 读取原配置/状态路径；probe 可另指定登录key或显式Token发行选项。uninstall wrapper验证本地bundle/helper后，仅去除用于bootstrap的bundle/pin参数再调用helper，不把它们作为维护路径覆盖。

原生LaunchAgent后续活动验收已补充真实终端/TCP与8MiB上传、下载中A→B→A（当前下载87.29秒、同版上传85.03秒）：公开目标在部分传输阶段仍保持旧完整内容，原pane/独立daemon上传及持久冲突/暂停/停止回执保留，旧流终止且新流不重放；此前另有取消清理证据。此处daemon为普通TLS fixture、版本为当前源码的测试bundle，不外推为历史发行迁移或重启/登录持久化。详见[LAUNCHD-ACTIVITY-VALIDATION](15-implementation-status.md#evidence-launchd-activity-validation)。

### 本地离线签名与验签

可信源码中的 `scripts/release-trust.go` 提供显式 `sign` / `verify`，核心位于 `internal/releaseauth`；不属于宿主常驻服务或公共插件 SDK。`SHIKI_SIGNING_KEY=<外置0600私钥> SHIKI_VERSION=<固定版本> make release` 在本地打包后追加 Ed25519 签名；未配置密钥时仍为未签名本地产物。已有归档、校验 sidecar 或签名不覆盖。专用签名公钥及受限tag的Actions环境已配置；首个公开alpha已通过该自动工作流签名、验签和发布，22个公开资产的独立下载、签名/payload/pin/远端摘要核对及双平台直接运行/重复离线暂存已通过；未激活服务，不替代首发前候选的服务验收，详见[发行证据](./15-implementation-status.md#evidence-public-alpha-release-20261029)。

安装前从独立可信渠道取得 SPKI Ed25519 公钥，从可信源码运行 verifier，并显式指定期望 version/OS/arch。签名绑定归档名、大小、SHA-256 和 SHA256SUMS pin；先验证元数据，再对同一输入流复制并哈希到新建0700目录，归档/pin/最终 receipt 为0600。仅成功的私有副本可进入解包和显式安装；verifier 不解包、不执行、不联网。对同一未认证归档附带的公钥或hash不自动赋予信任。签名者负责确认归档与pin配对，安装器继续检查清单、平台、schema和插件契约。

固定tag构建来源、公钥指纹及轮换/撤销处置边界见[自动发布流程](./18-release-readiness.md#github-actions-自动构建签名与发布)；轮换/撤销演练仍待。指纹仅标识实际使用的公钥，工具不提供在线撤销或时间戳服务。输出使用可信私有父目录；异常路径替换时宁可保留不完整证据，也不递归删除未知对象。参数、格式、清理边界及先验签后stage命令见离线签名说明。

### 受保护的 Actions 签名

按用户要求，首个及后续版本均以推送固定版本tag触发[release.yml](../.github/workflows/release.yml)：校验tag精确来源/master祖先、同源完整CI、四目标无密钥构建，tag限定的`release-signing`环境自动签名并验签，再由独立发布job完整上传草稿资产并公开Release。环境不再要求人工审批；推送tag是发布授权，受保护版本tag不能更新或删除。公钥、指纹、密钥备份、重试/冲突和轮换/撤销边界见[发布准备](./18-release-readiness.md#github-actions-自动构建签名与发布)。稳定版及alpha/beta/rc分别处理；安装前独立信任与校验要求保持不变。
