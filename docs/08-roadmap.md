# 开发路线图


<a id="evidence-public-beta1-release-20261124"></a>
## v0.0.1-beta.1 公开发行、四目标独立验签与公开包升级/渠道验收（2026-11-24）

- [x] 候选固定源码 `f234a090d42ebdb57c1f23e9ff317b6d43770f80` 发布前远端 [CI 45404108766](https://github.com/DDSRem-Dev/Shikigami/actions/runs/45404108766) 于 Attempt 3 完成 `57/57` 全部通过（`non_success=[]`）；如实保留 Attempt 1（`unit-plugins (darwin, arm64)` 在仓库测试前的 `actions/checkout@v7.0.1` 5 秒异常退出、`e2e (darwin, arm64)` 未分配 runner 取消）及 Attempt 2（两项 `macos-15` ARM 任务 `steps=0` 未分配 runner 取消）的托管基础设施重试记录，未把取消计为通过。
- [x] 基于 `f234a090d42ebdb57c1f23e9ff317b6d43770f80` 创建并推送不可变标签 `v0.0.1-beta.1`，[Release 45420481374](https://github.com/DDSRem-Dev/Shikigami/actions/runs/45420481374) 于 Attempt 4 完成 `60` 项成功、`1` 项 `homebrew` 按标签策略条件跳过（跳过不计通过）：于 `2026-11-24T17:11:45Z` 公开 [v0.0.1-beta.1](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-beta.1)（`draft=false`、`prerelease=true`，精确 `23` 个签名资产），并由 `github-actions[bot]` 自动更新 `Formula/shikigami.rb`（提交 `9d5c0c1167e7cf45b3bc8389b42f41c8cd344d68`）。如实保留 Attempt 1（WSL2 在产品/E2E/终端测试全通后于 `systemd-modules` `go mod download` 触发 60.8 秒外部网络超时，及 4 项 `macos-15` ARM `steps=0` 取消）、Attempt 2（2 项 `macos-15` ARM `steps=0` 取消）与 Attempt 3（1 项 `terminal (darwin, arm64)` `steps=0` 取消）记录。历史 `v0.0.1-alpha.1` / `v0.0.1-alpha.2` 核验仍保持原 `22` 个资产未变。
- [x] 完成 `v0.0.1-beta.1` 全部 23 个公开资产独立下载、可信公钥（`e21f6dd671ea5042c43c492b759b933dda73985f89a823b43c651ff7c787c17d`）四目标 `release-trust verify` 离线验签、篡改签名拒绝负例、`PROVENANCE.json` / `bootstrap.sh` / `SHA256SUMS` pin / `GPL-3.0-only` 核对、原生 `darwin/arm64` `shiki version` 与公开 `bootstrap.sh` 实网下载验签 `--stage-only` 暂存。
- [x] 完成公开签名包升级回滚与 Homebrew 公开 tap 渠道验证：隔离 `systemd 252.39` PID 1 容器（`scripts/test-systemd.py --bundles`）使用已验签公开 `v0.0.1-alpha.10` 与 `v0.0.1-beta.1` `linux-arm64` 归档通过 13 项服务生命周期、4 项活动传输升级/回滚边界、卸载数据保留与 `systemctl poweroff` 正常退出（保留首轮外层脚本 `umask 077` 导致只读挂载目录缺执行位的修正记录）；从已认证资产重生成的公式与 `Formula/shikigami.rb`（SHA-256 `96956b630e714bc2023e99f025ebb0c4ba6f351802d1b57218a5311b7b863497`）逐字节一致并通过 `ruby -c`，隔离 `homebrew/brew` Linux `arm64` 容器通过真实公开 `brew tap` -> `install` -> `test` -> `reinstall` -> `test` -> `uninstall` -> `untap` 及镜像/容器清理（保留首轮 `brew --version | head -n 1` `SIGPIPE 141` 脚本修正记录与 Linux arm64 Tier 2 提示）。据此勾选 [18 的两项 beta 发布时门槛](18-release-readiness.md#beta-readiness)。
- [ ] 保留外部与现网边界：用户现网 Mac/VPS 当前仍运行已验收的正式签名 `v0.0.1-alpha.10`（尚未切换至 `v0.0.1-beta.1`）；现网偶发 5 秒短 unary `deadline_exceeded` 尖峰、`B10` 全新双机从零首装与 `B11` 物理主机重启仍作为明确未实测/保留边界单列，不因 `beta.1` 发布完成而宣称关闭。


<a id="evidence-default-shell-alpha10-precheck-20261124"></a>
## 默认交互 shell 修复、alpha.10 实机自动化复验与文档同步（2026-11-24）

- [x] 修复 `B02` 默认 `/bin/sh`（`dash`）方向键缺陷与 `B03` 远端 Claude Code 全屏无闪烁 TUI 模式（`CLAUDE_CODE_NO_FLICKER=1`）：新增 `internal/server/tmux/shell.go`（纯 Go 有界读取 `/etc/passwd` 与 `/etc/shells`，兼容 `CGO_ENABLED=0` 发行构建；校验常规可执行文件并拒绝 `nologin`/`false`/非白名单路径；保留用户显式配置的 `zsh`/`fish` 等交互 shell，将最小 `/bin/sh` 或 `/bin/dash` 默认升级为可用的 `/bin/bash`，缺失时回退 `/bin/sh`），同步接入 `tmux.Create`、`tmux.Stop`、`session.launchCommand`（`--no-claude`）及 `StopClaude`（仅读取本地持久化的 `LaunchEnvironment`，不在停止路径调用 `region` worker RPC）；同时针对 `B03` 实测发现 `tmux` 内 Claude Code 默认采用内联渲染（`alt=0`，输入框停在第 23 行未贴底）的问题，在 `tmux.Create`（`new-session -e`）及 `Respawn`/`RespawnDead`（POSIX 位置参数包装器导出 `CLAUDE_CODE_NO_FLICKER` 后 `exec "$@"`，保留管理员显式环境覆盖，无额外 `tmux` 子进程调用）默认注入 `CLAUDE_CODE_NO_FLICKER=1`，VPS 实测确认 `178x57` 窗格进入 `alt=1`、`mouse_any=1` 并将输入框固定至底行（第 55–57 行）。首轮 `Respawn` 使用独立 `set-environment` 调用触发 `session` 预算测试假 `tmux` 观察脚本拒绝的失败已如实保留并改用单次 `respawn-pane` 包装器解决。
- [x] 完成候选源码本地与 Debian 12 容器全量验证：`make fmt && make lint && make build`、`make test-tools`（112 项）、`make test-viewer`（28 项）、macOS `darwin/arm64` 全包 `go test -race`、`scripts/test-terminal-state.sh` 16/16 VT 语义检查点，以及 Debian 12 容器 `./scripts/test-linux.sh`（覆盖默认 `/bin/sh` -> `dash` 环境下真实 PTY 左方向键 `\x1b[D` 编辑及 `CLAUDE_CODE_NO_FLICKER` 默认/覆盖回归）。
- [x] 完成已部署正式签名 `v0.0.1-alpha.10`（Mac `darwin/arm64` + Debian 12 VPS `linux/amd64`）自动化与用户实机验收闭环：验证跨日空闲 `RECONNECT_EXHAUSTED` 后 `shiki connect` `6788.7 ms` 恢复（`tailscale-direct`）；自动化通过 `B02`、`B04`（含 `6213.9 ms` 与 `11924.4 ms` 跨越旧 5 秒限制的多步 `resolve`）、`B05`（远端 `3000` 转发本地 `56102` 后端拒绝不重绑并恢复）、`B06` 文件与状态子集、`B07` 及 `20/20` RPC 采样；用户实测通过 **`B03`（经远程 `work` Chrome 完成官方 `claude auth login`，`loggedIn=true`，受管 `claude-check` 会话创建 `beta-check.txt` 内容 `SHIKI_BETA_OK` 及 detach/reattach 保活核验通过）**、**`B06`（浏览器日常使用体验正常）** 与 **`B08 + B09`（断网/睡眠恢复与持续使用正常，按用户确认登记）**。全部临时测试目录、项目、会话、映射、转发及暂存文件已清理归零，`work` 实例（`014ec66bfbef6a55488b14117be101d7`）与 Claude 登录状态保留。
- [x] 同步公开入口、支持声明与 `docs/releases/v0.0.1-beta.1.md` 发布说明草稿，完成七类架构一致性复核并保持 `08`/`15`/`16` 一致；候选远端 57 项 CI 与 `v0.0.1-beta.1` 签名发布及公开资产验签已于上方批次闭环。
- [ ] 保留外部边界：现网偶发 5 秒短 unary `deadline_exceeded` 尖峰如实保留；全新双机首装（`B10`）与物理主机重启（`B11`）保留为明确未实测边界。


<a id="evidence-viewer-transport-diagnostics-20261123"></a>
## alpha.10 查看器诊断发布与两端升级（2026-11-23）

- [x] 查看器在本地显示屏幕数据速率、二进制传输块速率、最近采样最大到达间隔、最近数据年龄和本地 WebSocket 发送缓冲区；指标文案明确它们不代表远端 RTT、RFB 帧率、解码耗时或实际呈现帧率。
- [x] 更新对应 viewer UI 回归断言并通过 Node 语法检查；本批只改本地观测，不改变 RFB 字节顺序、队列容量、输入控制、权限、RPC、持久状态、浏览器进程或安装布局。
- [x] 受影响的 client/server/runtime race 与 architecture 检查通过；完整受影响命令和 `git diff --check` 作为本批验证记录保留。
- [x] alpha.10 固定标签指向 `c2ac2068df1794e71c555051a9c5d307df7b994f`；发布流水线 `45056120246` 完成完整检查、四目标构建、签名和公开 Release，23 个资产全部发布。
- [x] Mac arm64 与 Debian 12 amd64 VPS 均升级到 alpha.10；VPS 原 daemon 身份、配置、密钥环和 work profile 保持，正式服务 active，Mac/Tailscale 节点重新授权后恢复 online。
- [x] alpha.10 viewer smoke 复验显示 `Screen data`、`binary msg/s`、`max gap`、`last data` 和 `send buffer` 指标，连接为 Connected/Read-only；viewer、URL 和临时文件已关闭清理。
- [ ] 真实滚动体验、长期稳定性、公网链路归因和 beta 门槛仍未完成；诊断指标不等于卡顿修复或实际呈现帧率验收。

alpha.9 旧版基线（2026-11-23）为 `tailscale-relay`、约12.2 KiB/s 接收和0 KiB发送缓冲；alpha.10 smoke 复验已确认新诊断文案实际由两端发布版本提供。两次均为短时只读样本，不代表远端帧率、滚动流畅度、RTT或长期稳定性。

本批架构复核结论：变更仅位于 browser client 的本地 viewer 静态资源及其 UI 测试；没有跨越 plugin/host/SDK 边界，没有新增 RPC 能力或权限，没有修改持久化格式、revision、去重、进程生命周期、网络策略或正式用户数据。


<a id="evidence-tigervnc-cleanup-20261123"></a>
## TigerVNC 对照测试环境清理（2026-11-23）

- [x] 删除本机退出的 `shiki-rfb-framerate-20261122` 临时 Debian 12 容器及其未被其他容器使用的 `debian:12-slim` 镜像；本机无 `shiki-tigervnc`/`shiki-rfb` 容器、镜像、测试脚本、测试进程或临时目录残留。
- [x] 只读扫描 VPS 的 `/tmp`、`/var/tmp`、`/root`、`/home/shiki`、Shikigami 状态目录和 Docker 资源；没有发现命名测试文件或 Shikigami 开发包。清理了 5 组已无进程引用的旧浏览器运行时 JSON 及对应的 `/run/user/1000` 临时目录。
- [x] 保留当前运行的 `work` 浏览器实例、正式 daemon/keyring/region 服务、正式配置/凭据/项目，以及 `agent-remote` 和其他用户 Docker 服务；未执行全局 Docker prune，未删除用户缓存或账号数据。
- [x] 清理后复核：VPS 三项 Shikigami 服务为 `active`，仅当前浏览器运行时文件保留；VPS Docker 仅保留原有 `agent-remote`/RustDesk 服务；本机测试资源扫描为空。




<a id="evidence-tigervnc-congestion-20261122"></a>
## TigerVNC 拥塞窗口核查（2026-11-22）

- [x] 核对 Debian 12 bookworm 的`tigervnc-standalone-server_1.12.0+dfsg-8`源码与当前runtime调用的`/usr/bin/Xtigervnc`；Debian源文件仍保留延迟尖峰后独立拥塞分支。
- [x] 按上游`895a86cff25b3d410af043098740d425cdad2bdc`的精确逻辑复现无符号窗口绕回：`baseRTT=100ms`、`minRTT=201ms`、初始4KiB窗口时，旧逻辑比例缩窗后再减4KiB并绕回，最终被4MiB上限钳制；补丁逻辑保留4KiB下限。原始补丁、脚本、结果和SHA见[隔离证据](evidence/tigervnc-congestion-20261122/README.md)。
- [x] 核对该补丁尚未进入上游`v1.16.2`稳定标签，当前上游`master`已包含；“直接升级最新稳定版”不能作为现成修复。
- [x] 在一次性Debian 12容器中应用原始上游补丁并成功编译`rfb`公共库目标；这只证明补丁可编译，不是完整`Xtigervnc`运行时或帧率验收，结果见隔离证据。
- [x] 在一次性Debian 12容器中完成stock与patched `Xtigervnc`对照：640x360/24-bit、33ms全屏动画、625,000 B/s与双向55ms代理、raw RFB客户端；两组均4次更新，约0.419/0.418 FPS，均值间隔2.387/2.393s，原始字节与错误数一致。完整结果见[隔离证据](evidence/tigervnc-congestion-20261122/README.md#isolated-runtime-ab-comparison-2026-11-22)。这是合成raw传输样本，不代表Tight/JPEG、noVNC、Tailscale或真实滚动体验。

该发现与低帧率/追赶症状有关，但不能单独解释触控板步进、网页滚动条略好或所有冻结。要纳入产品必须有可审计签名runtime artifact或发行版含补丁包，并完成升级/回滚、进程归属、依赖、实际滚动及长期稳定验收。

<a id="evidence-scroll-followup-20261122"></a>
## 滚动体验跟进与已完成 CI（2026-11-22）

- [ ] 正式alpha.9用户补充：使用触控板，按指导resize缩小后没有明显变化，拖动远端网页滚动条稍好；Fn+↓在该机器触发音量，不能计为Page Down测试。尚未独立记录这次实际framebuffer尺寸，不把主观对照扩大为排除带宽或已定位根因。整体操作可用的反馈保留，滚动流畅度、长期冻结与beta体验门槛仍开放；无需用户反复重做相同resize。
- [x] noVNC1.7/Smooth固定源ce85e60的[CI44949686567](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44949686567)于2026-11-21T21:54:33Z完成，attempt1，57/57成功，无失败、取消或跳过。暂缓采用的网络库候选eb4478c的[CI44951709516](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44951709516)于22:18:38Z同样57/57成功；它证明该候选检查通过，不是性能收益或当前恢复代码的完整CI。两轮均等待原运行结束，没有为新修改取消检查。
- [x] 本地提交0bdb51f恢复后的全部非docs文件与ce85e60逐文件相同，工作树当时干净，实验文本哈希全部通过；后续提交若只补文档不能冒称其独立完整CI已通过。正式环境保持alpha.9，未发布或部署alpha.10。
- [x] 一次性本地容器TigerVNC更新上限对照已完成，结果与限制已记录；未降低正式帧率、未改vendor滚轮语义。现有noVNC公开API没有sendWheel或滚轮灵敏度配置；增加合成事件可能提高位移准确性，也可能加重画面负荷。未来输入候选必须独立验证撤权/重连/失焦、方向与余量、突发及修饰键，不伪装成官方API或已提高实际FPS。

本批先同步实际用户反馈与固定源CI证据；目录/依赖边界、RPC/能力/权限、持久化/revision/去重、进程归属/取消恢复、RFB字节顺序及文件网络边界、CLI/安装布局均未改变。验证不跨越源码、平台与实机效果边界，08/15/16保持一致。




**TigerVNC上游核查（2026-11-22）：** 当前Linux runtime调用系统`Xtigervnc`，现场记录为1.12.0。上游提交[`895a86cff25b3d410af043098740d425cdad2bdc`](https://github.com/TigerVNC/tigervnc/commit/895a86cff25b3d410af043098740d425cdad2bdc)（2026-10-22）修复延迟尖峰后无符号拥塞窗口下溢：旧逻辑在按比例缩窗后仍继续减4KiB，极小窗口可能绕回最大值；补丁把第二个分支改为`else if`。GitHub比较确认该提交不在v1.12.0中，但它位于远离v1.12.0的后续开发历史，不能把整条TigerVNC主线或未构建的系统包当作可直接升级方案。该修复与用户的低帧率症状相关，尚未证明现场曾触发，也没有修改用户VPS系统包；需要在受控环境确认发行版可用的包含补丁版本、升级/回滚及实际滚动效果后才考虑纳入安装流程。

<a id="evidence-tsnet104-deferred-20261122"></a>
## 丢包模拟与候选暂缓（2026-11-22）

当前产品源码已恢复Tailscale1.98.10、原gVisor及Go1.26.11，保留ce85e60的noVNC1.7、默认Smooth与Fence测试。此前1.104.2/Go1.27.4在隔离环境通过兼容测试，但没有现场性能收益证据；受控模拟反而出现低带宽丢包下吞吐下降，因此不作为本次卡顿修复发布。研究分支`fix/browser-latency-tsnet104`的固定eb4478c CI继续运行，不取消/改写原检查；它不代表当前恢复后的发布候选，也不表示已部署。正式两端一直是alpha.9。

- [x] 完成11个内存双gVisor实验，均完整传输2MiB、SHA256匹配、exit0；固定Go1.27.4、MTU1280、双向各55ms传播/5Mbps串行链路，单向仅数据包0或1%独立丢弃，seed901/902，ACK不丢。无丢包两版约4.48Mbps；1%旧Reno/RACKoff为3.174/2.614Mbps，新CUBIC/default为0.967/0.681Mbps。同新版seed901控制：Reno/off2.657、CUBIC/off0.941、Reno/default3.005Mbps，不支持单独归因RACK。一个控制样本投递调度迟到20.480ms，其余约2–4ms；恢复到达追踪有未匹配重分段/冗余包，不能当完整TCP恢复状态。
- [x] 原始输入、依赖锁定、源码、11份结果及校验清单保存为只读用途文本[实验材料](evidence/netstack-simulation-20261122/README.md)；无测试二进制进入仓库，原临时目录已清理。模型不包含真实WireGuard/DERP、HTTP/2、浏览器或Tailscale自定义缓冲配置，不能用1%模拟反推原网络丢包率，也不能宣称新SDK现场必定退化。
- [ ] 保留用户最新反馈：正式alpha.9在Smooth并暂停背景视频后整体操作可用，滚动仍低帧率；调整实际远端分辨率的对照尚未回报，长期冻结/断线恢复和beta体验门槛仍开放。默认Smooth及图像资源释放的源码改进尚未部署。

本批只撤回本任务未发布的候选改动，并补充实验记录；原tailnet-only拨号/peer admission、代理边界、RPC权限、持久状态/去重、进程归属、CLI/安装及工具链恢复到已验证基线。生产差异逐文件对照ce85e60，不把未采用SDK的217项测试或研究CI算作当前发布性能验收；08/15/16保持同一结论。


**后续滚动审计（静态/隔离，不是实机结论）：** noVNC1.7 `_handleWheel` 使用50px累积门槛、每事件每轴最多一步且到阈值清零；执行原函数的隔离输入中，`[150]`产生1步、`[50,50,50]`产生3步、`[30,30,30,30,30]`产生2步余30、`[49,-50]`没有步进。这证明事件分批/残量会影响滚动步进，不证明用户卡顿只有这一原因。FBU可在同一JS任务连续flip，不能把flip计数宣传为屏幕实际FPS。默认远端1440×900，CSS缩放不减少远端像素；需要控制者显式勾选resize才请求远端尺寸变化。数据队列16×64KiB是容量上限，不能当作实测积压。下一步保持Smooth和同一静态长页，对照显式resize前后及滚轮/Page Down；尚未收到结果，不改vendor输入、不自动重启或调整用户分辨率。

<a id="evidence-tsnet104-candidate-20261122"></a>
## 网络栈隔离迁移实验（2026-11-22，已暂缓采用）

- [ ] 原网络严重卡顿/冻结与真实公网吞吐门槛继续开放。此处记录未采用候选的历史测试范围；实验从ce85e60创建隔离worktree，不调用用户已安装Shiki、不连接外部Tailscale账号，不把本地回归当作实机改善。正式两端及冻结alpha.9资产保持原版本。
- [x] 官方Tailscale1.104.2与gVisor `v0.0.0-20260915211658-a6f909f08a72`候选接入，最低Go1.27.3、工具链固定官方稳定1.27.4。上游启用CUBIC及新版默认SACK/RACK，注入路径改为三个有界队列；不是一行参数回补。依赖134→145项、62版本变化/13新增/2移除，必须覆盖文件监视、WebSocket、CLI/TUI及跨平台，不能继承旧SDK验收。来源：[发行](https://github.com/tailscale/tailscale/releases/tag/v1.104.2)、[netstack](https://github.com/tailscale/tailscale/blob/v1.104.2/wgengine/netstack/netstack.go)。
- [x] 初轮真实编译发现Engine.PeerForIP删除及cellbuf/ansi不兼容，完整失败保留为迁移原因。SDK适配改用裸IP WhoIs(port0)，只认live节点精确/32或/128地址、预期公钥、非自身、未Expired；握手前后分别查询、≤5秒且更早context优先，失败关闭已握手连接，继续走userspace netstack并绑定原generation/authorization/peer撤销机制。没有Server.Dial/SystemDial回退。旧新检查均是live peer-map观察，不冒充原子WireGuard路由租约；真实移除测试只替换已删除API的观测点为ErrPeerNotFound，不把任意LocalAPI错误当删除成功。
- [x] 修补新版useproxy feature可由环境禁用导致hook缺失的兼容风险：五个实际proxy hooks缺失时节点初始化失败，不能SetProxyFunc表面成功却绕过系统代理。子进程在真实init阶段测试禁用环境；不修改用户环境或打印原环境内容。TUI使用官方cellbuf0.0.15兼容MVS选择的ansi0.11.8，不降低SDK依赖或删除功能。
- [x] Go1.27.4下shiki/agent/daemon构建20.172秒成功；首组安全race26顶层/105含子例、0skip/0fail，包18.600秒（含编译wall78.245秒）。仅非deadline取消测试由30ms父截止改为5秒，真正deadline子例保持30ms，随后专项race重复10次通过1.858秒；没有改已在执行检查的冻结源或放宽结果断言。
- [x] 完整node race63顶层/217含子例通过、0skip/0fail（76.491秒）；Go1.27.4下全部cmd交叉构建CGO0通过：darwin/arm64 7.976秒、darwin/amd64 22.370秒、linux/amd64 23.903秒、linux/arm64 25.615秒。交叉构建不算对应平台原生运行。
- [x] 工具脚本112项复验通过（21.011秒）；首轮16项cache测试因固定假Go版本仍为1.26.11而拒绝，改测试输入为1.27.4后通过，没有放松生产平台版本检查。
- [x] Mac arm64主机相关race通过：internal/client20.605秒、internal/agent70.767秒、internal/connection2.732秒、cmd/shiki53.990秒、test/architecture1.450秒；browser/sync/port各可用平台包race均通过，无测试文件的入口不计测试通过，Linux专属行为仍由后续原生CI验证。独立terminal analyzer仅升级Go基线并保留自身依赖版本，`go test -race ./...`成功编译、没有测试文件，不计行为验收。
- [ ] 新源完整CI、公开包升级/回滚、账号状态兼容、自然切网、长稳和公网性能仍未验证。

**工具链来源与检查范围：** CI/release各setup-go固定1.27.4，platform_support严格匹配，WSL固定官方下载`go1.27.4.linux-amd64.tar.gz` SHA256 `7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840`；Linux runner固定官方多架构bookworm索引`sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea`，来自只读官方Go JSON与Docker manifest核对，不以元数据核对冒充完整镜像运行。上述脚本变更不包含在先前105项Go回归范围内，单独验证。历史原工具链与公开alpha.9证据保留。

**本轮用户反馈：** 正式alpha.9在暂停Claude登录背景视频并选Smooth后，用户明确反馈点击/输入等整体操作已经可用，但滚动仍卡、帧率不足。此改善发生在旧正式版本，不是网络库候选部署结果；滚动与冻结长期验收仍开放。先前给用户的open命令漏写`--read-only=false`，造成只读链接，已纠正并由用户继续操作；只读默认是原安全契约，不因此改为默认取得控制。建议的窗口resize对照仍待用户实测，不默改远端分辨率。

**隔离实验收尾：** 候选源码已提交8c4af99并快进主工作树，原实验worktree及日志共1,093文件、10,297,495字节已清理，Git仅余主工作树。首次常规worktree remove因残留文件拒绝；随后只删除固定的本任务临时目录，并经prune dry-run确认仅本任务失效登记后清理登记，没有清理其它工作树。源码与验证汇总已入库，未安装dev二进制；正式服务/配置不变。

**七类架构复核：** ①变更位于node SDK边界、工具链与tests，不把插件业务搬入host；②仅适配现有身份查询，RPC消息/权限/能力/transport选择不扩张，应用协议没有以SDK版本冒充升级；③原状态持久化、revision及request去重保持，未读取用户凭据；④握手前后预算/失败关conn/lease撤销与SDK有界队列关闭须回归；⑤保留tailnet-only、固定peer及代理策略，不通过猜测窗口或公开VNC端口规避问题；⑥新Go基线同步构建、CI、平台检查和安装说明，预构建用户不需自行安装Go；⑦局部安全测试、交叉构建、全平台CI、正式部署性能严格分列。当前仅候选，不宣称卡顿解决。

<a id="evidence-viewer-memory-latency-batch-20261122"></a>
## 后续查看器改进与延迟归因（2026-11-22，源码未发布、严重卡顿未关闭）

- [x] 固定官方noVNC1.7.0源码，commit `63107bd06d9e1f6136ff21aeda8cd62cbf0d433e`、归档SHA256 `06dac239be3973ed79a00e357a68aab996e6ed2fd496ee2cc6e6a34020344ac9`，61项payload清单与完整来源核对。纳入已绘图Image.src释放、JPEG扫描优化及H264能力探测VideoFrame.close；无私改vendor。上游依据：[图像释放](https://github.com/novnc/noVNC/commit/23b7219a5d39577f752be939ee25db2484d465b6)、[JPEG扫描](https://github.com/novnc/noVNC/commit/356eab4f4d259b6db7d1c7b84d43408b2e7acb09)。本机image decode短测0–7ms，因此不能把本次严重卡顿归因于解码或宣称升级已解决。
- [x] 新查看器默认Smooth（quality3/compression6），用户仍可选Balanced/Clear，重连保留本窗口选择；权限、分辨率与控制权不随画质改变。正式alpha.9仍默认Balanced。无并发下载的同一15fps动画交替Balanced/Smooth各12次：Balanced343–1034ms、中位439.5ms；Smooth146–720ms、中位288.5ms。只说明该页面短样本改善，未证明真实网站或长期体验。早先口头约465ms为汇总误差，以这里完整24样本计算为准。
- [x] 真实vendor行为回归覆盖异步解码乱序/flush/释放后可见像素快照、64帧源释放、错误尺寸拒绝及JPEG18个分片点；旧1.6在两项释放测试失败。Image/canvas为double，不等于浏览器GC、真实图片像素或性能验收。补齐51个Fence解析/转发子用例，保留二进制payload/flags、跨DATA分片、只读与revision变化，旧revision按键仍拒绝；未发现Fence生产缺陷。
- [ ] 用户反馈低帧率、输入延迟及冻结/断开恢复仍开放。远端点击至双rAF短测2–27ms、图像解码0–7ms；双rAF不证明实际合成呈现，跨主机时钟未校准，不推断单向网络延迟。前批不同工具轮询/rAF测量不作为严格对照。

**传输证据：** 用户确认套餐5Mbps（理论625kB/s）。自有随机2MiB通过正式API上传3.796秒、首次下载16.129秒；其后下载15.554/9.780秒与动画重叠，仅为竞争负载证据，不算干净吞吐对照；该重叠画面测试出现4546/9155ms及一次10秒未观察到目标变化。独立分段客户端经正式agent IPC、不读取Token：首metadata219ms，全部32个64KiB DATA及终帧/EOF14076ms，本机write+hash5390μs、fsync3ms，完整hash匹配。首轮探针错误使用err==io.EOF而非errors.Is，全部数据20398ms到达后误报EOF，已修正，不能记首轮终态成功。源码审计无每块应用ACK或fsync，HTTP2客户端默认4MiB流窗口，不能归因为64KiB窗口等待。单次SSH/SCP同文件约1秒、显示1.7MB/s且hash一致，小文件可能受突发带宽影响，不能声称5Mbps套餐突破或定位ISP；后续doctor显示tailscale-direct，之前relay观察保留，路线可变，尚未证明丢包/中继/拥塞算法哪个为主因。

**验证和架构：** `make test-viewer`28/28通过；`go test -race -p 2 ./plugins/browser/client ./plugins/browser/server ./test/architecture`全部通过（1.987/1.341/1.383秒），`git diff --check`通过。源码边界仍为插件内嵌静态资源与插件本地测试，无host业务依赖；RPC、能力、身份授权和RFB字节保序不变；没有修改持久数据、revision、请求去重、进程归属及启动恢复；未扩张文件、网络、剪贴板或终端权限，默认画质仅作用于viewer；安装布局和冻结alpha.9资产不变。新增测试验证真实vendor逻辑/生产Fence分支，但浏览器GC、Linux运行时及长期公网门槛独立保留。此前固定8f7481d的CI44945455780为57/57成功，不覆盖本批未提交源。

**清理：** 自有loopback内存HTTP夹具已停止；删除两个自建远端标签，保留原about:blank及Claude登录页，点击网站自身暂停按钮暂停背景视频，无账号操作。本机临时viewer关闭、TaskSpace31完成，临时instrumentation移除。远端自有2MiB上传文件核验固定路径/无symlink/完整SHA后删除；本机19文件13,231,736字节临时目录及1文件1,622字节探针目录已删除；SSH会话关闭。未安装dev二进制，未删除或重置用户Chrome profile/密钥环，agent-remote保留。

**下一归因候选：** 官方Tailscale1.104.2（2026-10-21发布）netstack已从Reno改CUBIC并更新恢复算法，来源注明修复旧CUBIC整型溢出后才启用；该版本要求Go1.27.3。当前仍固定1.98.10及原Go工具链，未盲改协议栈/窗口、未部署候选。需要独立兼容性、安全边界和真实节点验证，不能凭上游注释宣称现场改善。

<a id="evidence-alpha9-latency-followup-20261122"></a>
## alpha.9 后续严重卡顿反馈（2026-11-22，未解决）

用户在本次人工验证进入Claude登录页后明确反馈：Ego测试窗口和自行打开的日常查看器都仍严重卡顿、难以使用。因此前一批短时点击/输入/重连成功仅为功能证据，不代表可用体验已通过；本次进入登录页的事实保持，账号授权仍暂停。正在继续分段测量输入、画面传输与实际绘制；新修正尚未完成，不改写正式alpha.9资产。

已识别测量干扰：Ego页面visible/focused时requestAnimationFrame仍出现约1000/16毫秒交替间隔，因此基于该回调或外层工具轮询的约1秒结果不能当作纯网络延迟。改为在本地查看器临时包装真实noVNC send、message和display.flip，以pointerdown到目标像素实际变化为界：静态页10次128–366毫秒，15fps自有彩色动画下10次316–808毫秒；不外推其他网站/浏览器长期体验。连续更新与Fence实际启用；一次队列为空的快照不排除其他层积压。VPS短时vmstat未见CPU、swap、磁盘等待或steal饱和；公网5次ping109.675–116.126毫秒、无丢包不代表tsnet链路或长期网络。

现场TigerVNC1.12.0+dfsg-8、FrameRate=60；尝试通过vncconfig在线设置15失败，随后读取仍为60，没有实际改变或重启Chrome。后续代码及验证、测试资源清理尚待完成，原网络长期体验门槛保持开放。


<a id="evidence-alpha9-viewer-20261122"></a>
## alpha.9 查看器短时功能复验（2026-11-22，已进入登录页并完成临时收尾）

用户补充授权新建验证工作区后，在Ego TaskSpace30完成正式Mac/VPS alpha.9的真实viewer复验。work仍为原实例`712be7ec4555dc68c4f91a66d5c9dc76`、running/browse，原网络路由为tailscale-relay；测试使用自有data: HTML页面，没有新增远端测试文件或HTTP服务。没有修改生产源码、正式资产、出口、DNS或代理。

Balanced的5次点击至结果区域canvas变化观测为924、978、1116、890、1006毫秒；Smooth为939、999、1115、988、907毫秒。所有值包含Ego鼠标dispatch与canvas像素轮询工具开销，不是纯RTT。alpha.8前次使用不同夹具，不能严格作版本性能对比，也不能据此推导优化因果或全局流畅。真实截图确认Count5和输入alpha9-test回显；滚动后观察到SCROLL_END_OK。

5次显式Reconnect均成功，新连接均回到只读，再由操作者明确Take control。首次测试错误期待重连后自动成为Controller，导致15秒等待失败；随后实际截图为Connected/Read-only，符合设计，记录为验收假设错误而非连接失败。后4次连接观测为1099、1459、1343、1469毫秒，同样含工具/观察开销。最后截图确认reconnect-5真实输入回显；中间各次输入只取得动作receipt，没有逐次最终截图，不扩大为每次输入均有画面确认。

自有页面的只读原生观察为webdriver=false、ja-JP、Asia/Tokyo，未伪造或修改这些属性。导航claude.ai出现Cloudflare检查后交接用户；用户明确反馈已进入Claude登录页。助手重新接管TaskSpace30后，截图实际观察到claude.ai/login的正常Google、Apple和email登录表单，没有输入账号。本次人工验证后进入登录页通过，不证明以后不再出现挑战，也不等于Claude账号授权成功；助手未代做挑战。交接结束后，CLI close-viewer返回state=closed，TaskSpace30以task.finish({keep:[]})成功收尾一次。远端Chrome登录页有意保留供用户后续使用，没有关闭；最终status仍是同实例712be7ec4555dc68c4f91a66d5c9dc76、running/browse，region revision保持原值。本轮先核对精确名单、普通文件和无symlink后删除9个本机临时文件（8张截图及私有viewer回执，共1,142,073逻辑字节）和精确临时目录，最终确认截图另343,393字节随后删除，合计10文件、1,485,466逻辑字节；这不是实测释放磁盘空间。没有新增VPS文件或服务，未执行账号登录。本轮临时查看器/工作区/截图清理完成，远端登录页属于明确保留的用户使用状态。最后doctor为7插件ready、core HEALTHY、CPU15.9%，terminal/sync/forward均0；这是当次采样，不证明长期稳定。

- [x] 正式alpha.9自有页面点击、输入、滚动，以及5次显式重连回只读后重新接管的有限功能复验。
- [x] 本次用户人工验证后进入claude.ai/login，助手截图复核正常登录表单。
- [ ] Claude账号授权、原网络长期使用体验及自然切网；不承诺以后不再出现挑战。
- [x] 用户handoff结束后关闭本机viewer、完成TaskSpace并删除本轮10个临时文件；远端Chrome登录页有意保留。

本批七类复核与证据范围见[实施记录](15-implementation-status.md#evidence-alpha9-viewer-20261122)和[架构复核](16-architecture-conformance.md#evidence-alpha9-viewer-20261122)。未改生产源码、冻结资产或权限，不用短时功能通过覆盖旧卡顿/挑战失败，beta保持开放。

<a id="evidence-alpha9-observer-fixture-20261122"></a>
## 发行后 CI 采样夹具竞态修复（2026-11-22）

文档提交`6106fec8`的[CI 44937051142](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44937051142)最终52项成功、1项失败、2项下游矩阵跳过。唯一失败是Intel Mac的`TestDefaultRotationProcessObserverOwnsAndJoinsSamples`：异步观察器启动40毫秒后测试就结束子进程，首次探测已看到进程不存在，导致LastPresent为空。日志不能区分调度延迟与系统探测耗时；这不是产品超时的证据。

夹具改为等待真实身份探测返回首次alive，再结束其拥有的子进程；等待有截止、探测失败仍失败，原2秒退出join及非空/有序时间戳、正采样间隔断言保持。仅修改测试，不更改生产观察器、产品、协议、安装或已发布alpha.9资产。Mac arm64的两项相关测试以`-race -count=20 -p=1`通过，共40次顶层执行、1.821秒，覆盖自有进程退出与未知错误/取消负例；未生成持久测试bin，自有sleep进程均Wait回收。后续完整CI以本修复提交的GitHub检查结果为准，不将旧失败或下游skip计为通过。新版本短时查看器实测另列；本次Cloudflare人工进入登录页已另有证据，长期体验、自然切网及账号授权等外部门槛保持开放。


<a id="evidence-alpha9-upgrade-20261122"></a>
## alpha.9 两端升级完成（2026-11-22，后续交互与本次登录页验证另记）

已将Debian12/amd64 VPS与Mac arm64从公开签名alpha.8升级为alpha.9，固定来源`0b4847d879d10bd2a79b9df348857ab27dafa848`。先经CLI显式停止work，再复用两端完整原receipt Plan执行安装；VPS setup复用原dedicated加密凭据并更新root管理的runtime/helper，没有重新签发或输出设备Token。升级与健康检查已完成；其后浏览器短时功能实测见[交互记录](15-implementation-status.md#evidence-alpha9-viewer-20261122)，本次人工验证进入Claude登录页已通过；严格版本性能A/B、长期稳定及Claude账号授权仍未完成，不能宣称卡顿或挑战循环永久解决。

VPS的SHA256SUMS清单19项、Mac清单15项全部匹配已验签bundle（均包含release.json），VPS两项root支持二进制另外核对一致。两端安装首次均退出3、报告node_uninitialized；后续实际节点online，原daemon_id及100.71.151.36保持。随后正式doctor报告alpha.9、core healthy、7个插件ready，插件配置revision全部保持。首次pending没有被计作ready；安装器本地探测与后续真实远端RPC健康分别记录。

work新实例`712be7ec4555dc68c4f91a66d5c9dc76`为running/browse；原身份、凭据、配置revision、profile与地区revision保持。12次真实RPC均成功0.180–0.211秒，升级前0.179–0.241秒；不据此宣称画面卡顿改善。升级完成时Ego TaskSpace消失，当时等待新建许可、尚未做交互复验，未使用viewer已关闭；后续获授权的实际viewer证据见[本轮交互记录](15-implementation-status.md#evidence-alpha9-viewer-20261122)。

VPS旧测试sync历史19文件11,906字节已在原state.lock排他锁及精确无symlink结构校验后清理，本轮VPS安装staging和loopback夹具已移除；projects/sync projects均空、3正式服务active。本机本轮暂存目录已在关闭SSH复用连接后删除，共74个文件、976,054,591逻辑文件字节（不是实测释放磁盘空间）。临时目录复扫12,874条，仅剩保留的正式tmux socket；56个macOS系统保护目录仍不可访问，未绕过权限。正式安装、alpha.5–9、升级备份、凭据、浏览器profile与agent-remote保留。升级前点击基线、完整保留hash范围及七类复核见[两端升级证据](15-implementation-status.md#evidence-alpha9-upgrade-20261122)和[架构复核](16-architecture-conformance.md#evidence-alpha9-upgrade-20261122)；该清理及升级记录不扩大为全盘无残留或长稳完成；本次人工进入Claude登录页另有顶部独立证据。

<a id="evidence-public-alpha9-release-20261122"></a>
## alpha.9 公开发行（2026-11-22，部署与人工验收另记）

固定标签`v0.0.1-alpha.9`指向`0b4847d879d10bd2a79b9df348857ab27dafa848`。[发布前CI 44924752906](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44924752906) attempt1最终57/57成功；[发行流水线44929424103](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44929424103) attempt1最终60项成功、1项条件Homebrew检查跳过，跳过不计通过。[Release](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-alpha.9)于UTC2026-11-21T18:13:58Z公开，共23个资产。两端正式升级已完成，独立部署证据见[alpha.9升级记录](15-implementation-status.md#evidence-alpha9-upgrade-20261122)；本次人工进入登录页已独立验证；原网络长期体验、自然切网、真实账号与物理重启继续开放。

独立验证23资产精确清单、GitHub元数据size/digest、四目标Ed25519签名与完整payload hash、PROVENANCE/tag/run/source、公钥及固定源码bootstrap一致。34个二进制只读核验实际GOOS/GOARCH和Go1.26.11；四目标`shiki`另从ELF/Mach-O真实Go字符串符号读取Version/Commit/BuiltAt，分别一致为alpha.9、上述完整源码和2026-11-21T17:19:42Z。其余30个二进制不宣称均保留这些字符串；未执行下载的跨平台二进制。

Homebrew公式已自动推进，bot提交[`e0888009cc634de8c6e09b0962b44934498f2f7b`](https://github.com/DDSRem-Dev/Shikigami/commit/e0888009cc634de8c6e09b0962b44934498f2f7b)；从认证资产重新渲染与已下载公式逐字节一致，公式SHA-256为`7c57e9bbdc86922edccb677d4a9b2929d703d6e4c19777267847b7202b534ddb`。这不是新版本真实tap安装/重装或受管服务升级验收。

完整四目标hash/pin、审计脚本错误假设与修正、证据摘要见[公开发行证据](15-implementation-status.md#evidence-public-alpha9-release-20261122)。本批仅补充实际发行记录，生产/协议/权限/持久状态/进程/限额均不变，架构范围见[七类复核](16-architecture-conformance.md#evidence-public-alpha9-release-20261122)；原Phase0–5清单不变，正式部署及本次人工进入登录页各有独立证据，不扩大为长期免挑战或账号授权。

## alpha.9 发行、部署、短时交互与本次登录页验证完成（2026-11-22）

固定源码`0b4847d879d10bd2a79b9df348857ab27dafa848`的完整CI、alpha.9签名公开发行和独立资产验证已完成，见[发行证据](15-implementation-status.md#evidence-public-alpha9-release-20261122)。两端正式升级和RPC健康检查已完成，见[升级证据](15-implementation-status.md#evidence-alpha9-upgrade-20261122)；本机临时材料收尾、短时浏览器交互及本次人工进入Claude登录页均已完成，见[交互与网站证据](15-implementation-status.md#evidence-alpha9-viewer-20261122)；长期体验、自然切网和Claude账号授权仍待验，此记录批次不修改冻结生产源码。

- [x] 用户授权alpha.9公开发行、升级两端及重启Chrome，页面已保存。
- [x] 固定提交完整CI通过后创建不可变tag，完成签名发行及独立公开资产验证。
- [x] 只部署正式签名alpha.9，检查原身份、凭据、浏览器profile及日常模式。
- [x] 完成本轮两端有明确范围的元数据扫描；经既有API暂停并移除旧验收映射，删除两个已停止测试会话、测试项目及两端原验收目录。
- [x] 升级worker释放锁后，以原state.lock和精确结构校验清理VPS旧测试sync历史及本轮VPS安装staging/loopback夹具。
- [x] 完成本机`/tmp/shiki-alpha9.gGYgWZ`临时材料收尾及SSH连接关闭；保留agent-remote、正式安装/历史发行/升级备份、正式归档记录和真实用户数据，不宣称全盘无残留。
- [x] 新版本原网络自有页面短时点击/输入/滚动与显式重连复验，范围及计时限制见本轮交互记录。
- [x] 本次用户人工验证后进入Claude登录页，助手截图复核并完成查看器/临时文件收尾。
- [ ] 长期体验、自然切网及Claude账号授权；不承诺以后不再出现挑战。

以下保留升级前清理快照；后续VPS清理完成见上方升级证据。清理顺序和拒绝保护均已核验：未暂停的unmap返回`MAPPING_MUST_BE_PAUSED`且未修改；pause取得revision后unmap成功。删除原三个测试文件组成的两端`beta-accept-20261118-235852`目录，各55字节；Mac空父目录一并移除，VPS projects根保留。首次文件名白名单抄录错误触发拒绝、未删除，重新核对名称和hash后才成功。临时目录递归补扫另确认并删除90个旧测试tmux socket：均为本UID所有、连接ECONNREFUSED且删除前inode未变，0个活动socket。复扫12,881条，56个macOS受保护TemporaryItems访问拒绝如实保留。收尾正式doctor仍为alpha.8、7插件ready，work仍running，身份/凭据未变。扫描覆盖、排除和七类复核见[实施证据](15-implementation-status.md#evidence-alpha9-cleanup-20261122)与[架构复核](16-architecture-conformance.md#conformance-alpha9-cleanup-20261122)；不替代发行、升级和公网使用验收。


补充同步残留核查：本地旧map已由插件正常归档，sync_mappings为空、archive为archived且operations/conflicts均0。持原state.lock排他非阻塞锁成功并核验无symlink/精确tree结构后，仅删除该map的trees（6文件共2,722字节，含3个旧history副本）及空staging；正式ledger归档去重/防复用记录保留。VPS对应已删除project的sync历史仍无法取得排他锁，未删，待升级worker释放后处理。Git仅主worktree、无未追踪开发产物；Docker无shiki测试容器、其他容器保留；VPS systemd仅3个正式Shiki服务，未发现dev服务。

## 浏览器兼容性与响应路径优化（2026-11-21～22，发行前候选验证记录）

以下保留发行前候选源码、隔离复验及失败记录的原始范围；现已随alpha.9公开，发行证据见上，部署与公网人工验收另记。

用户接受“优先日常浏览兼容性”：新启动默认browse，不启用Chrome调试接口；需要原生浏览器诊断时显式切换diagnostic并确认重启。旧实例不静默重启、不伪报browse。此取舍不修改浏览器指纹，不自动处理Cloudflare挑战，不改变出口、代理、DNS或账号登录安排。

已定位的独立代码瓶颈：每个浏览器帧复制整个agent状态；只读RFB请求在慢socket写入期间占用全局控制锁；状态查询在外部runtime/region等待期间占用worker锁；控制通知与约1MiB图像队列共用容量。候选改为单记录即时授权读取、只读写入独立于控制锁、状态检查两阶段释放锁并核对期间变更，以及有界独立控制队列。RFB字节不可丢弃/重排，真正修改桌面的输入仍在权限和控制revision检查下串行发送。当前传输Send不能被控制通知抢占；长期慢连接仍可能取消，不承诺消除网络延迟。

查看器新增Smooth/Balanced/Clear画质选项（noVNC公开quality/compression参数），默认保留原Balanced；显示本机WebSocket接收速率及发送缓冲区，明确不是RTT或远端画面延迟测量。依赖详情折叠以腾出显示空间。Chrome主动正常退出与异常依赖退出区分，状态不得再把用户关闭窗口统一误报为未知依赖崩溃。

局部证据：固定4MiB无关状态的授权基准，原实现21.589–27.827ms，新单记录读取0.762–0.775µs；仅代表该函数/该受控状态规模，不代表真实帧延迟或整机加速比例。只读阻塞回归已在旧实现确定失败；慢状态检查旧实现两个阶段均阻塞Stop，修改后18个Linux并发场景通过。首轮固定源码快照的组合race已通过：Mac cmd/shiki53.765s、client20.244s、agent72.514s、server36.457s、host plugin5.213s、SDK2.435s、state2.595s、browser client1.809s、architecture1.187s；普通UID的Linux arm64隔离容器中runtime3.661s、browser server21.411s、install5.636s、architecture1.134s、daemon跨进程integration10.560s通过。对应Mac go vet通过；Linux运行按脚本冻结源码、只读挂载，未使用用户状态。该快照不覆盖随后发现的持久格式回滚补修，补修须独立验证。真实Chrome模式切换与画面A/B仍另行验证，不能计为实机修复完成。

查看器24项JS回归通过（无跳过），包括画质重连保留、旧状态/控制回调隔离及mode缺失不猜测。Ego隔离合成页在1280×720下折叠画面高362.58px、无横向溢出，检查了Details、只读、失败/unknown展示；这是布局/模拟UI证据，不是真实RFB或网络性能。测试页loopback服务和临时目录已关闭删除。

独立复核发现并修复最初方案的回滚格式回归：Mode不写入profiles.json.Runtime，磁盘维持alpha.8可严格读取的shape；仅浅复制Runtime投影，不增加整库复制。模式权威在新`<instance>.launch.json`，旧`<instance>.json`只接受无mode的legacy。加载前验证ID和固定私有路径，再核对全部Spec/Region后恢复内存mode；新文件缺mode、缺失/损坏/字段不一致均拒绝，不能猜成diagnostic。内存深拷贝、未发布保存失败恢复与PublishedError保留mode。独立审查直接对照alpha.8真实Spec/database及旧manager路径校验，无剩余阻塞问题；本地旧shape读写后再升级不等于正式两端服务回滚验收。

最终补修快照普通UID Linux arm64 race：runtime3.742s、browser server21.323s、architecture1.233s通过；覆盖模式持久兼容、新命名必须有mode、状态/流并发及X11就绪修正。随后仅新增的路径逃逸、非固定路径和旧名携带新mode负向测试在普通UID禁网容器定向通过，无后续生产变更。Linux amd64 runtime交叉编译及Linux相关go vet通过；锁定生成器重复生成17个protocol Go文件逐字节未变。

真实Chrome隔离验收先发现候选X11匹配缺陷：官方Chrome的第一项res_name带profile路径，最初要求两项均为google-chrome导致15秒超时。修为有界quoted res_name解析、不以其作身份、不记录其值，严格核对第二项res_class=Google-chrome与本Chrome PID，并拒绝重复或多余属性行；未延长15秒期限。修补后普通browse实例窗口就绪、原生webdriver=false、/diagnose返回409拒绝、关闭最后窗口退出0且stopped通过。此前失败保留；这不证明Cloudflare放行。

研究依据：[Cloudflare挑战循环排查](https://developers.cloudflare.com/cloudflare-challenges/troubleshooting/challenge-solve-issues/)、[支持浏览器与自动化限制](https://developers.cloudflare.com/cloudflare-challenges/reference/supported-browsers/)、[noVNC公开API](https://novnc.com/noVNC/docs/API.html)、[Tailscale连接类型](https://tailscale.com/kb/1257/connection-types)、[MDN webdriver](https://developer.mozilla.org/en-US/docs/Web/API/Navigator/webdriver)。官方资料支持分别检查浏览器配置、挑战网络稳定性与中继延迟，不能证明本次Cloudflare拒绝的单一原因。已有截图显示重复挑战；页面标识不等于站点侧原因。不得用本机浏览器或自动化通过冒充远端人工登录通过。

最终真实独立runtime两模式复验通过：UTC2026-11-21 16:18:23.324～16:18:41.638（18.314s），官方Google Chrome158.0.8127.36、Linux arm64普通UID、加密测试keyring与Chrome sandbox保留；容器network none、零宿主mount、非privileged、无cap_add。Docker默认namespace门禁曾导致sandbox失败；仅任务容器使用基于Moby default的专用seccomp，增加USER/PID/NET相关clone及仅NEWUSER的unshare许可，其余默认限制和clone3 ENOSYS保持，未改宿主策略。browse实际running/browse、窗口本PID/class匹配、合成loopback页原生webdriver=false、diagnose409、正常关窗exit0/stopped。diagnostic新实例running/diagnostic、原生diagnose200并含observed browser.context、同合成页webdriver=true、SIGTERM后exit0/stopped。两次/proc固定debug flag观察均为空，原因未解释；未把最初精确flag断言失败追认为成功，仅由真实CDP功能证明诊断可用。没有自动处理Cloudflare、修改webdriver或使用真实账号。

该验收binary SHA256为`cf8a6b9de9042d65f6b7d604ace935ae5b50ae90e1932d68117e306b2228ca7a`，33个runtime Go文件清单摘要`6c867ae505ee64cc9ecbe6e6d32aa2801fd75afb4809cce38455039f55211c48`；不是正式发行签名产物。独立runtime测试不代表完整systemd安装、两端Shiki链路、真实原网络画面A/B或Cloudflare放行。真实网络体验、公开发行/升级和用户人工验证继续保留开放。

本批清理完成：合成查看器loopback服务/TaskSpace关闭，专用Chrome测试容器及镜像、私有测试profile/keyring/脚本、临时测试bin/截图及本批新建的.tools目录均已删除；未执行Docker prune或清理用户通用缓存。正式Mac/VPS服务、agent-remote、原项目/会话/profile与凭据未改。用户于2026-11-22明确授权提交推送、CI全通过后发布alpha.9、升级两端并重启已保存页面的Chrome；另授权全面扫描并清理Shikigami开发/临时数据及旧本地测试目录。正式发行与清理执行证据另行登记，不能提前记完成。

七类最终源码复核：业务仍在browser插件、共享单记录读取在SDK状态包、宿主只负责授权/路由，架构检查通过；RPC只增加模式字段及逐层Start能力门槛，既有权限和当前控制revision不放宽。profiles旧shape与launch新代次分离，atomic发布、request_id去重及内存失败恢复均有回归。Chrome独立进程归属、断线不结束浏览器和显式重启保持，正常退出/依赖异常/取消分列。RFB不丢字节不重放、mutating输入保持授权线性化，队列/超时有界；文件与终端行为不扩展。CLI显式诊断及模式切换确认、x11-utils安装依赖与新旧端报错已同步；查看器模式unknown不冒充browse。实现/本地回归、隔离Chrome、正式发行/部署及公网人工验收分别记账，08/15/16/27/30一致，beta未推进。

- [x] 候选组合回归、七类架构复核与隔离Chrome两模式启动验证完成（范围和失败历史见上文）。
- [x] 正式alpha.9两端升级后，本次用户人工验证进入Claude登录页且助手截图复核；仅限本次，不代表账号授权或长期免挑战。
- [ ] 原网络真实使用延迟与稳定性达到可接受水平；beta门槛仍开放。


## alpha.8 用户体验复验反馈（2026-11-21，卡顿与网站验证未通过）

用户反馈仍卡顿，访问claude.ai时勾选Cloudflare后返回验证页。B06实际流畅性与网站可用性未通过，先前短时功能成功不覆盖此失败。账号登录继续暂停，未尝试代做挑战、修改出口/代理/DNS或浏览器指纹。

只读检查：daemon及7插件ready/core ready，现场路由tailscale-relay；work状态为crashed、reason为browser dependency exited。用户随后明确说明已主动关闭远端Chrome窗口，因此该记录不能算意外崩溃，也不能用于解释关闭前卡顿。VPS journal显示23:54:20起依赖清理、23:54:28 runtime退出；源码将某受监控子进程Wait()==nil归为通用dependency exited并标crashed，未保存组件身份。这是退出诊断粒度不足，尚未修改/部署修补。

VPS只读观察：Asia/Tokyo、NTPSynchronized=yes，可用内存约2.5GiB、load约0.18/0.23/0.25；本次时间窗未发现内核OOM记录，不能由事后采样排除使用过程瞬时资源问题。所查Chrome默认内容设置及claude.ai例外未发现JavaScript/Cookie禁止；受管策略仅密码保存/Chrome同步/浏览器账号登录限制，不等于禁止网站登录。VNC日志显示已有Tight/JPEG编码，不能把卡顿简单归为未压缩传输。原生诊断因浏览器已关闭返回failed_precondition，不记成功。

代码固定带remote-debugging-pipe，上一轮原生诊断webdriver=true；仅登记真实运行事实及待核查的兼容性因素，不断言它导致本次Cloudflare拒绝。未取得失败页错误码/Ray ID或站点侧原因，出口IP地理库分歧仍保留；日本时区/语言不能证明网站资格或挑战会通过。未重启用户已关闭的Chrome、未清理Cookie/配置、未执行外部供应商检测。

- [x] 收录用户失败反馈、纠正主动关闭与意外崩溃的区分，核对服务/日志/有限配置。
- [ ] 卡顿端到端归因、浏览器退出诊断改进及候选复验。
- [ ] Cloudflare循环原因与用户手动验证成功；缺少失败页证据，不能宣称已解决。

七类复核：本批只有只读诊断和文档，目录/依赖、RPC权限能力、持久schema/revision、进程策略、文件/终端/网络契约及CLI/安装布局均未修改；区分用户反馈、当前采样、旧版证据与未测试项，08/15/16/30一致，不推进beta。

## alpha.8 两端升级与自动化复验（2026-11-21，功能子项通过，稳定性仍开放）

用户授权升级VPS和本机，再自动完成可执行测试；Claude授权继续暂停、网络专项暂跳。本批只部署公开签名alpha.8（固定源`3c0d9454cf139eb854e046ae74a1579c9045f7b4`），未安装开发bin，未修改生产源码、DNS、出口、代理或路由，未重启物理主机。保留agent-remote、现有映射、两个原会话、work profile及凭据。记录日期采用本轮工作区日期；现场采样原时间戳保留其原值，不据此宣称主机时钟已验收。

- [x] Debian12/amd64与Mac arm64正式alpha.7→alpha.8签名升级；独立信任公钥、归档hash及checksums pin核验通过。
- [x] VPS18个/Mac14个已安装payload、两项root支持程序匹配已验签alpha.8；receipt active/current一致、无pending，原安装Plan保持。
- [x] 实际终端attach/detach保活、配置快照恢复语义，以及sync/port/browser/region下列功能子项复验。
- [x] 清理本批测试会话、快照、同步测试文件、转发、HTTP夹具、浏览器测试页和暂存文件；原文件、原会话及配置/凭据保留核对通过。
- [ ] 默认空shell仍为`/bin/sh`，方向键交互缺陷未修复；B02不得整体勾选。
- [ ] 浏览器状态偶发超时与一般连接稳定性、长用体验继续开放；不以短时通过抵消失败。
- [ ] 真实Claude授权/任务、切网/睡眠、物理重启、新环境首装/本批回滚及其它原生平台保留未测；不发布beta。

升级时沿用完整原Plan和已注册加密凭据。daemon普通用户、原节点地址/daemon身份、Mac配置/plist/凭据及VPS配置/Token/auth/加密credential/keyring摘要均保持；插件配置revision保持，7个实例均alpha.8 ready。支持setup在明确停止Chrome后更新两项root程序并复用密钥环，官方依赖步骤将Chrome157.0.8103.78更新到158.0.8127.36。work profile定义及地区revision不变，随后启动成功。未采集停止前标签页清单，不能宣称原打开页面全部恢复。两端正式升级备份保留。

安装器服务激活后的首次探测均出现`node_uninitialized`/退出码3；后续节点online，未重新登录或换Token。VPS应用授权仍`CLAUDE_LOGIN_REQUIRED`，与core ready分列。升级前同步曾offline/REMOTE_RETRY_EXHAUSTED，升级重启后active；这是该次重启恢复证据，不能替代自然断线恢复。更新支持前首次`browser stop work --yes`返回`unavailable: service unavailable`，只读确认仍running且无pending任务后，新一次显式stop成功；首次失败保留。

### 实机功能与限制

| 项目 | 正式alpha.8实际结果 | 不能据此关闭的边界 |
|---|---|---|
| B01基线 | 客户端/daemon/7插件alpha.8 ready，最终doctor core ready、节点online，实际项目/会话查询成功 | doctor不检查Claude登录；短时健康不是长期可用性 |
| B02终端 | 临时shell普通用户与cwd正确；实际detach/attach后PID和环境标记相同；配置快照恢复为新PID且不带原环境标记。原两个tmux PID在升级/收尾均保持 | 默认`/bin/sh`方向键问题仍在；快照不恢复进程/文件/对话 |
| B04同步 | 隔离子目录双向初始/后续更新、pause/resume、keep-local/remote/both、重命名、删除、删除对修改冲突均核对两端字节；清理后active、pending=0、conflicts=0 | 6次resolve使用默认调用路径，耗时0.594–4.938秒；均小于旧5秒，不能声称本次实机跨越旧截止或验证完整15分钟上限。部分提交/未知结果故障注入仍依赖已有隔离回归 |
| B05端口 | 仅127.0.0.1 HTTP夹具；首次0.695秒返回正确。停止后端listener时新连接失败、已有TCP仍可响应(0.127秒)；同一forward/listener恢复后新请求1.065秒成功，未改3000/40000 allowlist | 后续浏览器阶段向原临时端口请求另有reset；查询发现forward已在另一个随机端口listening。未取得该次重绑定原因，单列一般连接稳定性，不能冒充目标拒绝回归失败或证明无故障 |
| B06浏览器 | 鼠标点击/英文退格、滚动、中日IME实际转换、显式剪贴板发送/接收缓冲区、CLI上传/取回以及远端Chrome原生文件选择/下载均通过，文本/字节匹配。强制只读查看器不能取得控制；第二可控查看器接管后旧页Read-only且撤销旧控制提示 | 未读写用户OS剪贴板、未做账号登录/Cookie续期；不宣称主观流畅或长期低延迟 |
| B06重连 | 5次显式Reconnect均先Connected+Read-only，再确认Take control成功；重连1.243–1.677秒，总含重新接管1.907–2.333秒。关闭所有查看器后Chrome同实例running，重开后测试页内容保留 | 原生文件测试时出现一次状态查询unknown/timeout，Refresh status恢复；此失败仍保留，不把5次重连通过推广为状态/所有竞态闭环 |
| B07地区 | host为Asia/Tokyo、ja_JP.UTF-8；JP只读plan changes为空、changes_dns=false。远端work原生Chrome诊断timezone=Asia/Tokyo、languages=ja-JP/ja且同实例配置一致 | 未调用外部供应商，出口IP地理库分歧未解决；Secure DNS/WebRTC/权威DNS仍unknown。缺fonts-noto-color-emoji警告保留，未为测试临时安装 |

同步keep-both正常冲突在默认路径2.094秒完成，两端两份内容均保留。测试只使用既有验收项目内独立子目录，不修改原三文件。浏览器原生上传28bytes、下载30bytes均经实际文件通道与内容校验；本地查看器只承载VPS画面，没有用本机Chrome冒充远端环境。短采样12次默认projects/sessions/browser-status查询全成功，0.174–0.465秒；与上述超时分别登记，不声称本批所有请求无失败。

收尾通过摘要对比确认：原三文件、本机/VPS配置与凭据未变；原两会话PID不变，临时两会话和一个配置快照已删除；临时forward为空，HTTP夹具监听已关闭，临时目录及浏览器上传/下载文件已清理；owned远端测试标签页关闭，原about:blank保留，work同实例running、无controller、restart_required=false。同步测试文件先经正常同步删除并核对两端为空，再移除空目录。VPS本批升级暂存目录、本机下载包/验签工具/测试脚本/截图与私有临时日志已移除，SSH复用连接已关闭；正式release、安装备份与agent-remote保留。验收摘要保存在本节，未将Token、引导URL或账号秘密写入仓库。

七类文档复核：目录/SDK-host-plugin归属不变，无业务源码修改；RPC、准入、权限能力不变且不扩大负向授权验收；原Plan/凭据/文件保留，测试使用真实revision与request_id但不冒充故障原子性矩阵；tmux/Chrome独立生命周期按实际验证、物理重启另列；文件冲突和端口拒绝语义核对，终端默认shell及一般连接失败仍开放；CLI/安装只用正式包，签名payload及root支持程序复核；实现回归、公开发行、本批功能实测和外部门槛分开，跳过/unknown不计通过。08/15/16及30同步更新，历史alpha.7失败不删除。

## alpha.8 授权发行（2026-11-20，公开发行与独立验签完成）

用户明确要求发布v0.0.1-alpha.8，用于下一轮修复后测试。本次范围为提交当前修补、固定源CI、不可变新tag、四目标签名归档/公开Release及自动Homebrew更新。本发行批次结束时VPS/Mac仍为alpha.7；随后两端升级见本页2026-11-21记录，升级与现场复验不由发布成功替代，账号授权及网络专项维持既有暂停安排。

变更包含会话错误码/未知结果提示、ResolveConflict复合预算和显式timeout、未决keep-both副本保护、端口目标ECONNREFUSED隔离、浏览器attachment控制归属和迟到响应提示。沿用上一批七类审查，发布批不改RPC方法、权限能力、持久schema或安装布局；插件生产边界与SDK封装保持。已知实机失败和默认shell体验复验保留，不将alpha预发布写成beta验收通过。

本次发布前本地复验：10个受影响包/架构的race组合通过（browser/client与architecture复用上一批有效缓存，其余8包新执行）；对应go vet通过，查看器18/18、无skip，git diff --check通过。Darwin上既有sudo PTY平台跳过不计Linux通过；完整四平台检查最终结果见下方固定源CI及发行记录。

首轮完整CI44572726453（源86f4bcc）暴露两类遗漏，失败保留：协议诊断回归误按protobuf map编码字节顺序比较，定向race100次22次失败；改为解码后核对完整ErrorInfo schema及错误码，1000次通过，errors/SDK/version整包race各5轮通过。多主机诊断夹具仍断言ResolveConflict旧5秒默认，并漏传显式timeout标记；本地先复现默认断言，再新增显式5秒用例RED复现遗漏，修后按新15分钟默认、2分钟父截止、显式5/1/90秒和较早取消验证，multihost整包race2.895秒、vet通过，client/architecture有效缓存通过。该批只改测试/诊断helper与文档，生产行为、权限、状态和安装布局未改；不放宽既有字段断言或未知结果保护。docs25注明旧证据时点；随后新固定源CI通过后才创建tag，结果见下文。

固定源CI终态：3c0d945的CI44579854243共57项最终成功；attempt1为56成功/1 macOS终端runner未领取取消，等待其它长项结束后仅补跑该任务，attempt2成功。按同源各job最新成功结果合并，不把取消计通过。首轮86f4bcc的CI44572726453最终45成功/7失败/1 runner未领取取消/2前置跳过；WSL唯一失败为旧multihost超时断言，其后未执行阶段不计通过。新固定源完整WSL已成功。新tag已推送，同源发行结果与独立公开验证如下。

- [x] 取得alpha.8公开发行授权，编写固定版本发布说明。
- [x] 冻结提交3c0d9454cf139eb854e046ae74a1579c9045f7b4的完整CI44579854243通过，创建并推送不可变v0.0.1-alpha.8 tag。
- [x] 同源发行检查、四目标构建/签名、公开23资产和Homebrew更新成功。
- [x] 下载公开资产独立验证签名、来源、内容和公式。
- [x] 两端升级（后续2026-11-21批次已验证，见本页顶部）。
- [ ] 修复后全部实机门槛闭环（本批功能通过项与仍开放项见顶部；不等于全部验收通过）。


公开发行终态：[Release44593959710](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44593959710)成功，60项成功、1项条件跳过（不计通过）。于`2026-11-19T20:20:26Z`公开[v0.0.1-alpha.8](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-alpha.8)，`draft=false`、`prerelease=true`，精确23资产；自动Homebrew更新提交`5cb2a5c3c341127b407cd45feaf7986a765543ee`。标签固定源3c0d9454cf139eb854e046ae74a1579c9045f7b4，旧标签与资产未改。 首轮发行复核50项成功、4项取消、4项跳过（含未执行的构建/签名/发布）；3个ARM取消任务未取得runner，Intel内部包任务运行中取消、日志无测试fail事件，取消具体原因未明。第二轮仅补跑未完成项，3项成功、ARM发行工具再次未获runner取消；第三轮该项成功并完成后续发行。最终按同源最新job结果为60成功/1条件跳过；取消、未执行阶段不计通过，未改源码或放宽检查。

从公开Release重新下载23资产，以仓库独立公钥核验四目标Ed25519签名、归档/逐文件SHA256、checksums pin、精确资产清单与GitHub资产摘要；PROVENANCE绑定固定tag/source/run，bootstrap与冻结源码逐字节一致。四目标shiki内嵌版本、提交和构建时间从ELF/Mach-O的buildinfo字符串符号读取，OS/架构由Go build info核对；初版临时核验脚本误假定go version -m暴露ldflags，断言失败后改为直接读取实际符号，再完整重验通过，公开包未修改；Mac arm64仅在私有临时目录执行version，返回alpha.8及3c0d945完整SHA，未安装或启动服务。由认证资产重新生成Formula，与自动更新的远端公式逐字节一致，Ruby语法通过。公钥SPKI SHA256：`e21f6dd671ea5042c43c492b759b933dda73985f89a823b43c651ff7c787c17d`。

发行收尾七类复核：插件/宿主/SDK依赖边界保持；RPC传输、准入和权限能力未改；持久schema、revision和去重不变；取消、进程归属与服务启动策略未改；文件/终端/网络语义按本批回归记录，未把间歇实机失败改为通过；CLI/安装布局不变，下载入口统一alpha.8，Homebrew公式仅由流水线认证资产生成；实现回归、公开发行、两端部署和外部门槛分列，跳过不计通过。README、08/15/16、18/22/28/30同步。

## 用户授权自动化实机验收（2026-11-19～20，本轮完成，失败待候选复验）

用户明确授权助手接手命令和可自动化验收，只有确需人工的步骤再暂停。正式两端仍为alpha.7；未安装开发bin。只在既有beta验收项目/专用临时资源内操作，保留agent-remote、原两会话、work浏览器、凭据/节点身份。网络专项暂跳，Claude授权继续暂停。

同步批：先以原最新generation明确keep-local解决已保存REMOTE_CONFLICT_V5的副本，再验证VPS该副本hash后明确keep-local解决原件；两端原件为LOCAL_CONFLICT_V5、副本为REMOTE_CONFLICT_V5，映射active、0 pending/0 conflict。独立测试子目录覆盖本地/远端首次显式方向、已有基线双向更新、重命名（旧名删除、新名首次选择）、已有基线删除、暂停后的双端修改冲突、keep-both双版本、删除对远端新编辑冲突及显式保留远端。新keep-both2.497秒完成；所有结果用两端字节/哈希独立核验。测试新增文件经正常同步删除，两端临时目录已清理，用户原文件和原副本保留。

本批resolve均显式2分钟预算以进行alpha.7现场恢复/功能核验，其他查询和pause/resume用正式默认预算；不把这次功能成功当成旧默认超时已修复或新源码已部署。相关通过记录与未发布源码分开，B04新版默认交互复验仍未关闭。

会话/端口实机补充：通过真实shiki attach新建临时无Claude shell，detach/reattach后PID1528619及环境标记保持；配置快照恢复为新PID1545654、标记unset，目录/描述/tags一致，源和恢复会话均无Claude。恢复会话与临时快照已删除；源临时shell曾供浏览器测试HTTP服务使用，最终清理见下文。

B05失败：既有allowlist为3000/40000，无配置修改。新转发绑定127.0.0.1随机端口，3次真实HTTP返回正确（2.951/1.586/1.737秒），lsof确认仅loopback。通过shiki终端Ctrl+C停后端后下一请求失败符合预期；再启动后端并独立从VPS localhost核验响应成功，但本地forward仍offline/REMOTE_UNAVAILABLE、bound_port=0，原socket拒绝连接。原失败转发已显式stop并核对移除。期间独立SSH master曾40秒超时后broken pipe，重新认证后恢复，此诊断通道异常单列，不冒充Shiki转发失败根因。源码已定位目标ECONNREFUSED被当作整个转发Unavailable，修补与隔离验证见下文，不部署开发bin。

B05源码修补完成（未发布）：服务端仅在授权及allowlist检查后、实际loopback Dial明确ECONNREFUSED时生成固定Unavailable/shiki.port/TARGET_CONNECTION_REFUSED零metadata标记；公共错误过滤限前8详情/每项2KiB、拒绝protobuf未知字段并重建规范详情。插件业务经SDK helper使用，客户端只在CONNECTED前结束该新TCP而保留原listener和其它连接，不自动重试/重放；一般Unavailable、权限/认证及已建立流失败仍沿原策略。取消/截止优先，cause/wrapped/joined错误不携带目标拒绝标记。

验证与复核：新目标拒绝用例先RED复现listener关闭；实际root架构检查发现port业务直接导入pkg/errors违反SDK白名单（FAIL0.597秒），未放宽测试，已改SDK封装并复核导入。独立review另发现取消组合标记优先级问题，8例RED0.627秒后修复。修后race全包errors1.329秒、SDK2.662秒、port/client6.440秒、port/server4.211秒、architecture1.409秒均通过，新增场景无skip。最终root组合race：真实双worker/TLS port集成18.728秒、agent72.635秒、server36.596秒通过；go vet相关错误/SDK/port包通过。真实集成验证同一本地临时端口、后端拒绝时既有TCP保留、恢复后新连接成功、半关闭尾数据和worker回收。正式alpha.7的B05失败继续保留，隔离修补通过不能代替发布后实机复验。

七类源码复核：port业务留在插件，固定错误诊断归公共错误包并通过SDK使用；RPC方法/权限/能力及持久schema不变，只新增严格限定详情。未知错误仍脱敏，取消和进程回收保持；无TCP重试/字节重放，无安装、配置、网络策略变化。06/10/14与08/15/16同步，先前漏判依赖边界的review结论已撤回，更正后的架构检查才计通过。

浏览器实机子项：保留原work实例及原网站标签，只新建loopback测试页。真实鼠标点击、英文输入/退格、滚动、Fcitx拼音候选及中文测试、Mozc候选及日本語テスト均通过；测试页回报独立核验已提交文本。显式剪贴板Mac查看器→VPS文本框、远端Copy→查看器文本框通过；未读取/覆盖用户OS剪贴板，系统剪贴板权限路径不计通过。CLI上传并取回hash一致；VPS Chrome原生选择暂存文件、页面读取正确；Chrome实际下载到暂存区并经CLI取回字节一致。最初keyboard.type未在canvas发送文字，逐键事件及GTK显式Open后成功，记录为测试操作修正，不抹去过程。

浏览器失败保留：连接中曾出现状态查询超时，后恢复；计划5轮重连/接管测试在15秒观察期限未确认Controller而中止，后来实际界面亦显示operation timed out。15秒观察期限短于产品30秒预算，不能将观察超时本身认定为底层超时，亦不能将5轮计通过。刷新后一次明确接管恢复；没有增加产品超时或自动重放输入。强制只读查看器Connected且Take control禁用已验；持续低延迟、网络恢复及长稳仍未通过。

浏览器源码修补（未发布）：POST control固定attachment/instance/revision，锁内核对当前连接，转发绑定原attachment取消上下文，拒绝旧请求改绑；仅当前generation、匹配scope流事件授予/撤销控制，HTTP/快照不授权。已确认流结果优先于迟到HTTP超时，撤权同步提示；重连取消旧control/status请求，旧finally不释放新刷新槽。新增Go/JS回归先RED再GREEN；代理race、vet通过，最终root race browser/client2.073秒、architecture1.658秒，viewer18/18通过、无skip。独立复核发现撤权后旧提示残留，同批补第18例；未更改操作超时、远端协议、权限能力、持久schema、生产依赖或安装布局。27契约与08/15/16同步；这些竞态修补不等于已证明所有实机卡顿根因或已在alpha.7生效。

最终实机收尾（2026-11-20）：第二个可写查看器显式接管成功，旧查看器变Read-only、新查看器Controller；旧alpha.7的旧提示仍显示You control已现场复现，权限徽标与提示矛盾单列，不能宣称旧版UI正确。最后一次默认5秒sessions list在5.047秒deadline_exceeded；后续显式30秒查询0.714秒成功。此为间歇失败与随后恢复，不视为延长预算解决问题。网络专项仍暂跳，未擅改路由/代理/服务或进行Claude授权。

清理及最终状态：仅删除本轮两个临时会话、一个配置快照、测试转发、HTTP fixture、上传/下载测试文件和查看器；测试Chrome标签关闭，原两标签保留，原work实例未重启。最终原两会话保留、forwards为空，loopback3000已关闭。同步一度offline/REMOTE_RETRY_EXHAUSTED、0 pending/0 conflict；doctor仍ready/7插件ready。核对当前revision后以独立request_id显式resume，恢复active（末次观察1.091秒），原件LOCAL_CONFLICT_V5、副本REMOTE_CONFLICT_V5及terminal-check三文件两端字节一致，冲突为空。此为一次显式恢复，不计自然自恢复或持续稳定通过。SSH私有控制连接、临时脚本/截图/查看URL随后统一关闭清理；项目、映射、用户原文件、副本、凭据和正式安装保留。

- [x] 自动化恢复本次部分keep-both，双版本两端保留。
- [x] 正式alpha.7同步功能子项及新增测试资源清理（resolve显式预算）。
- [x] 完成本轮port/browser/配置快照自动化执行、记录失败及专用测试资源清理；不表示全部功能通过。
- [ ] B05后端恢复、B06控制/状态超时及同步重试耗尽的候选实机闭环；短时恢复不关闭稳定性门槛。
- [ ] 最终发行候选默认预算实机复验、人工账号/体验及未测环境门槛；beta未发布。

七类复核（实机操作部分；源码修补另见上文）：测试未部署源码、改变RPC/能力/权限或持久格式；使用正式CLI经agent/插件执行同步决策，SSH仅做普通UID测试输入与独立文件核验。最新generation和独立request_id保护每个明确动作，无超时盲重试；不直接编辑ledger。文件变更限验收目录，未启停正式服务或修改网络/安装布局。原失败不被本次成功覆盖，08/15/16同步，外部结果与隔离回归分列。

## keep-both部分完成与恢复修补（2026-11-19，实机未通过）

用户以固定request_id `beta-keep-both-v5-01` 对from-mac.txt选择keep-both后返回deadline_exceeded。只读核对：本机原文件仍为LOCAL_CONFLICT_V5，唯一新副本保存REMOTE_CONFLICT_V5；映射conflicted、pending_files=2、conflict_count=2。原请求持久结果为failed/RESULT_UNKNOWN_RECONCILE_REQUIRED，副本日志为remote原文件→local副本的committed/possible，远端副本尚缺失，原冲突未解决。这是部分完成，不能解释为全部成功仅丢响应；不重复旧请求或换ID碰运气，不删副本、不编辑ledger。

代码审查确认ResolveConflict共用短unary默认5秒，而keep-both需要多次远端观察/读写，外层预算会提前截断已有按大小计算的文件预算。journal在未知结果后保留两端和冲突是必要安全行为：不能凭目标缺失断言从未提交，也不能自动恢复已经被用户删除的副本。修补同时检查操作预算、显式timeout/父取消、未知结果提示以及副本递归冲突保护；不能仅延长期限就宣称实机故障解决。

当前源码修补（未发布）：ResolveConflict采用有界复合文件预算，保留单流大小预算、metadata短预算和较早父截止，显式5秒也不被默认策略提升；CLI在发送前/后分开报错，保留真实request_id和错误码，不承诺已消失冲突能经CLI重放，不自动查询或重试修改。交互等待提供进度，JSON结果仍只在stdout。插件对未决副本或存在未决副本的原路径拒绝再次keep-both，状态区分CONFLICT_COPY_REVIEW_REQUIRED；两端不一致时，只有最新generation的显式方向决定才能解决副本，原始冲突继续保留。已settled历史副本的新冲突仍支持全部正常选择。Copies仅落盘而尚无fileOperation的窗口也纳入两端重观测；两端相等才收敛，绝不由单端缺失自动补写。

- [x] 记录固定请求与部分提交证据，区分保留数据和操作成功。
- [x] 源码修补与隔离行为/竞态回归、七类架构复核（平台跳过及既有限制另列）。
- [ ] 正式候选发行/升级及同场景实机复验；B04与beta门槛仍未通过。

验证：新增副本回归先RED复现旧版误分类及重复/嵌套copy，再GREEN。精确race：sync10.707秒、client1.198秒、CLI7.884秒，新增场景无skip。真实UDS/HTTP2调用延迟5.1秒验证越过旧5秒截止仍成功，并覆盖显式5秒/100毫秒、父deadline/cancel、进度回收、原始ID、JSON与不自动重试。全包`go test -race -p 2 ./internal/client ./cmd/shiki -count=1`通过（20.290/53.263秒）；既有TestConsoleSudoPipe在Darwin跳过，单独-v确认，不能计为Linux sudo PTY通过。root组合`go test -race -p 2 ./plugins/sync/... ./internal/server/session ./test/architecture`通过：sync/client17.299秒、架构1.364秒，其他相关包复用有效缓存，不冒充新实机运行；sync根包无测试文件。`go vet -p 2 ./cmd/shiki ./internal/client ./plugins/sync/client ./internal/server/session`通过。正式alpha.7只读复查仍为原冲突和唯一副本冲突，generation/两份内容hash未变；未代执行恢复。

七类复核：生产目录和依赖边界不变；无RPC/能力/授权扩张；ledger schema、原子条件提交及请求去重保持，未知请求不重放。阶段文件/取消/重启证据保留，进度协程结束前回收；原有task互斥锁等待不支持取消的限制仍在，前端预算不表示worker排队回收上界或status即时返回。文件副本恢复须观察相等或用户明确方向，不自动恢复被删副本，旧generation拒绝；超时规则是复合文件操作策略，不修改网络路由或以预算掩盖丢包。CLI所有Open入口统一传递显式timeout标记，进度不污染JSON，安装布局/正式bin未变。独立复核发现显式5秒入口漏传及未prepare副本被ignore隐藏的问题，已同批修正并回归；10/13/14契约说明与08/15/16一致，本地检查和外部/发布门槛分开。

本轮尚未部署开发bin或改变正式服务、映射及测试文件；网络专项继续暂跳、Claude授权保持暂停。

## 用户终端保活回传与同步准备

用户按已有terminal-check的退出查看/重新附着步骤回传：普通用户shiki、测试项目目录正确，前后PID均3280565，SHIKI_RESUME_CHECK均resume-1795098825-3280565，terminal-check.txt仍为SHIKI_TERMINAL_OK。本子项按用户实测回传记为通过；不是助手独立重放，也不扩大为B02全部场景或默认shell修补通过。中文弯引号导致的Bash续行已由Ctrl+C及正确ASCII引号恢复，不记作终端传输故障。

随后助手只读核对本机对应测试目录存在且为空，同步映射已存在、state=paused。下一步由用户在该测试目录创建from-mac.txt并显式resume，查看status/conflicts；新映射无共同基线时的初始冲突须逐项确认，不自动选择覆盖方向。助手本轮未恢复同步、写测试文件或改变远端会话。网络专项仍按用户要求暂跳，Claude授权继续暂停。

后续用户执行resume后立即见reconciling/空冲突列表；助手复查确认已形成from-mac.txt（仅本地存在）和terminal-check.txt（仅远端存在）两项INITIAL_SYNC_CHOICE_REQUIRED。期间一次状态为offline/REMOTE_UNAVAILABLE，随后真实projects list成功、sync状态恢复conflicted；该短暂失败保留，不扩大为持续离线。下一步仅指导用户分别keep-local和keep-remote，助手未代执行冲突决策，双向字节一致性仍未验。

用户随后对terminal-check.txt连续3次确认keep-remote，前台均deadline_exceeded。助手没有重提resolve或改变冲突选择；只读复查当前映射active、冲突列表空，本地from-mac.txt为from-mac-v1\n，terminal-check.txt为SHIKI_TERMINAL_OK\n。插件持久请求记录有该远端保留冲突的2条failed/RESULT_UNKNOWN_RECONCILE_REQUIRED，本地保留冲突有1条succeeded；不能将三次超时都解释为已成功但响应丢失，也不能从记录数量确定每次请求的具体阶段。当前两文件共同基线已建立、operations为空，说明数据随后经核对收敛；VPS当前文件内容的独立用户核验仍待完成。ResolveConflict当前普通IPC默认5秒且包含多轮远端观察/传输；未取得逐阶段计时，不通过提高预算或新ID重放掩盖故障。数据收敛与前台超时分别登记，B04整体不计通过，sync resolve的超时阶段及未知结果恢复提示仍待修复/复验。

后续用户回传VPS两文件内容正确，并依次确认本地v2→远端、远端v3→本地、暂停期间本地v4不传输、resume后v4传输正常；上述为用户执行反馈，不冒充助手独立重跑。双端冲突步骤中用户立即查询仍为reconciling/空列表，本地保留LOCAL_CONFLICT_V5；助手随后只读查询确认conflicted、1 pending/1 conflict/CONFLICTS_PENDING。共同基线与两侧哈希分别匹配paused-local-v4、LOCAL_CONFLICT_V5、REMOTE_CONFLICT_V5，确认已形成有共同基线的双端修改冲突，两侧差异未被自动抹平。下一步由用户显式keep-both，成功响应及两端保留副本仍待验；原resolve前台超时问题保持开放。

- [x] 同会话进程/临时标记及文件保留的用户回传证据。
- [x] 测试目录和既有paused同步映射的只读核对。
- [ ] 初始文件同步及冲突决策、双向/暂停/删除后续功能验收。

七类复核：本批仅记录用户证据及只读状态，生产依赖边界、RPC权限能力、持久格式/去重、进程与取消恢复、文件/终端/网络限制、CLI和安装布局无变更；通过项限本子项，后续指令不算已执行，08/15/16同步。仅文档变更，差异格式检查通过，不重复软件测试充当新功能实测。

## 切回原网络的分段诊断（2026-11-19，间歇问题未定责）

用户随后明确暂时跳过网络专项排查，先逐项执行其他功能验收；网络项保持未完成，不要求先修网络才做非账号测试。下一步使用已有terminal-check验证detach/attach的同进程与临时环境标记保留，然后再进行sync、port、browser及快照测试。Claude授权仍暂停；此安排是测试顺序调整，不是这些项目已通过。

用户要求先分析原网络供其自行处理，再在原网络继续验收。此时正式alpha.7自动保持ready/tailscale-direct，agent与daemon身份不变，doctor/core及7插件ready；未再执行connect或修改任何网络配置。原网络默认en0网关10.0.0.1；系统HTTP/HTTPS/SOCKS/PAC开关均未启用，这不等于检查了所有应用或路由器代理。路由前段为10.0.0.1→192.168.1.1→100.70.0.1，经运营商网络到VPS共13跳；只能证明有多级路由，不能单凭地址证明每层NAT、将星号判为转发丢包或将两级路由定为根因。

无线统计（只保留信号/协商字段）：5GHz信道44、160MHz、802.11ax、RSSI -35dBm/噪声-96dBm、协商2401Mbps。支持当前信号很强；协商速率不等于互联网速率，不能排除历史干扰。非交互wdutil无管理员认证不可用，改用只读system_profiler取得统计，无权限策略修改或密码请求。初组网关/VPS各30次全部收到；同一序号出现网关73ms与VPS210ms尖峰，采样与无线信息读取可能重叠，不能排除诊断扫描造成抖动。随后不读无线信息、不启动浏览器、不改配置重复60秒采样：

| 测量 | 结果 |
|---|---|
| 网关10.0.0.1 | 60/60，均值2.814ms，最大5.014ms，无超过30ms样本 |
| VPS公网地址 | 60/60，均值106.437ms，最大122.681ms |
| 上级网关192.168.1.1（独立20次） | 20/20，均值4.186ms，最大18.122ms |
| VPS 1200字节payload、DF（10次） | 10/10，均值104.589ms；只排除这一路径/包长/窗口的明显失败，不宣称完整PMTU验收 |
| Shiki sessions list/browser status（12次） | 全部成功，0.254–0.455秒，无延长期限 |
| Mac en0该60秒平均流量 | 下行3.031Mbps、上行0.948Mbps；不是全屋WAN流量，不能单独证明无排队 |

结论：当前原网络短请求和空闲连接恢复正常，先前丢包/重传及热点改善证据仍成立，但不能把历史3/8 ICMP未回复当成持续37.5%丢包率。优先排查间歇性的原宽带路径损耗或路由器/WAN排队；当前不支持持续弱信号、服务未启动或已被迫DERP作为主因。网络类别、运营商、Mesh/中继与其他设备流量已向用户询问，尚不能凭网络段自行补全。建议用户在同一宽带做主路由有线对照、查看全网尤其上传占用；仅在故障时依据网关/终点共同采样区分局域网与外网，必要时请供应商核查去程/回程。没有证据就不要求改桥接、MTU、DNS或开放防火墙。用户处理前不继续账号或声称稳定性通过。

- [x] 当前原网络只读分段测量，扫描干扰后的独立静默复查；保留历史失败及当前恢复两组证据。
- [ ] 用户确认网络拓扑/流量或执行同宽带有线对照；故障时定位具体损耗段。
- [ ] 处理后在原网络重测真实浏览器、会话及后续B02/B06/B08/B09；短时恢复不能直接关闭beta门槛。

七类复核：本批无生产源码/目录/依赖、SDK-host-plugin边界、RPC能力权限或持久schema改变；使用既有只读RPC与有界网络探测，未创建/删改会话或应用数据。身份与进程所有权、取消/恢复、网络限额及CLI安装保持；未访问新诊断供应商、改代理/DNS/路由/防火墙或启停服务。外部样本及未知点如实分列，08/15/16同步，检查差异格式和三处证据一致性；无代码变更，未重复软件回归冒充网络通过。

## 手机热点对照（2026-11-19，明显改善但稳定性仍未闭环）

用户明确告知Mac已切换手机热点，在同一正式alpha.7、同一work Chrome实例、1440×900默认画质及相同无公网请求data测试页下复测。首轮本机agent ready、内嵌节点online，但服务器停在RECONNECT_EXHAUSTED，8个远端列表/状态调用在12–16毫秒内被拒绝；这属于已耗尽的重连状态，不能计为热点上8次实际网络探测失败。按现有doctor恢复指引显式执行一次`shiki connect <saved-server-id>`，0.637秒恢复ready/tailscale-direct，daemon身份与credential_revision保持；未重启服务、清空状态或重新登录节点。有限自动重连后要求显式恢复符合02的最多5次契约；普通命令仅显示service unavailable的恢复提示仍有改进空间。

| 同类测量 | 原网络（上一批） | 手机热点 |
|---|---|---|
| ICMP，8次、间隔0.5秒 | 3次未收到回复；收到部分均值127.1ms | 8/8收到，平均87.879ms、范围84.966–97.542ms |
| 默认画面点击→变色 | 首组5次2372–6843ms，中位4566ms；其他样本最慢7977ms | 8次1466/904/2122/909/1127/926/1006/1023ms，中位1014.5ms |
| 远端列表/状态 | 操作期间约0.25–4.16秒，另一次查看器状态超时 | 重连后首组8次0.211–1.024秒；点击期间10次0.221–1.927秒；18次全部成功 |
| 新建空shell | 用户此前2次deadline，之后助手一次1.033秒成功 | 新建独立临时会话0.485秒成功，ready/Claude stopped，默认预算不变 |

这支持原网络路径对卡顿有显著影响；对照还包含显式重建连接和不同时段，不能归咎具体Wi-Fi设备/运营商或排除软件传输效率问题。ICMP不代表Tailscale丢包率，8次低丢包窗口不等于长稳；热点仍有约2秒迟滞，B06/B09和beta稳定性不因此通过。准备测试页时连续剪贴板粘贴/回车未完成导航，15秒观察超时；检查空地址栏后单独粘贴、核对并回车成功。该准备失败单列，不伪称所有输入/剪贴板路径通过，不混入8个已就绪测试页点击样本。

- [x] 原样热点对照、显式连接恢复、18次远端查询及一次真正创建测试；原始失败与恢复分别保留。
- [x] 临时会话准确删除，自有测试tab/viewer关闭且查看凭据撤销、任务截图清理；最终仅保留原jp-auth-check/terminal-check，work原实例running、attachments=0，连接ready/tailscale-direct。
- [ ] 剩余画面迟滞、剪贴板交互与切网恢复提示优化/复验；长期稳定性及beta发行仍待办，Claude授权继续暂停。

七类复核：本批无新增生产源码/依赖/SDK-host-plugin边界变化；仅现有认证的connect和会话/浏览器RPC，无权限能力或协议扩张。连接与会话request_id沿现有原子/去重契约，测试创建/删除各一次，不重试未知修改；保留正式bin、身份、项目文件、密钥环、agent-remote、独立Chrome所有权及进程。未改变超时、DNS/代理/路由或默认显示设置；有限样本与未测门槛明确分开，08/15/16同步。文档差异/三处证据一致性检查通过，不把上一批本地代码修补或本次网络改善宣称为新发布版本。

## alpha.7会话超时与浏览器卡顿实测（2026-11-19，稳定性未通过）

用户连续两次创建`jp-auth-check --no-claude`返回deadline_exceeded，并报告浏览器几乎不可用。先查询确认该会话不存在，再用固定request_id只发起一次创建：正式alpha.7默认预算下1.033秒成功，未启动Claude；读取该新pane进程允许列出的环境，确认为TZ=Asia/Tokyo、LANG=ja_JP.UTF-8、LANGUAGE=ja_JP:ja；保留该会话供后续使用。这不证明原故障已经修复，也不证明改用资源ID是修复。创建RPC默认15秒，前置项目查询及创建内部region检查各有5秒预算，原始两次错误没有阶段证据，不能断言由哪一段产生。

现场证据：本机agent状态12–56毫秒；关闭查看器时远程列表/浏览器状态约0.6秒。远程region inspect三次0.281–1.710秒；VPS root/helper inspect23–24毫秒，普通shiki经受限sudo33–35毫秒。采样时VPS CPU约95–99% idle，无明显内存/IO压力；这些是有限窗口，不排除其他时段负载。随后真实查看器一次状态查询超时，画面仍连接；并行远端列表/状态最高约4.16秒。路径采样为tailscale-direct，不能据此宣称持续直连或归因relay。

在work原实例中仅新建自有data测试页，不访问账号或新的外部供应商。默认1440×900、quality6/compression2，首组5次点击至canvas变色为3620/4566/2372/4980/6843毫秒；后续对照最慢7977毫秒。直接观察私有X11及共享只读VNC连接上的同一测试像素，VPS画面变色已发生而本机仍可延迟数秒，说明至少存在画面返回链路延迟。3次本地WebSocket/画布采样没有长任务，末批画面消息抵达后很快可见；诊断记录仅保留测试页时序/字节计数，临时截图已清理，不将实际账号页面或原始输入纳入文档/测试fixture。降低quality1/compression9仍有5440毫秒，缩小960×600仍有4766毫秒；两组样本不足以宣称优化有效，均恢复原设置。

到同一VPS的8次ICMP有3次未收到回复（37.5%，有限样本且ICMP可能受限）；独立SSH TCP短测8次均成功、0.232–0.691秒，其内核累计重传段数30→34、重传字节20016→20932。支持链路存在损耗，不能据此把ICMP比例当作Tailscale丢包率，不能定位运营商/主机方向。内核ss不能代表gVisor内层TCP；目前产品未导出其重传计数，端到端根因仍待进一步取证。当前依赖隔离gonet/HTTP2实验未复现秒级Flush/Nagle等待：默认Nagle关闭；6字节和64KiB各100次echo最大分别108.5/936.2微秒；同连接未读16MiB响应下10次短请求208–755微秒。该实验不包含公网/WireGuard，不作为现场网络通过。

已修源码：会话环境检查经插件RPC返回Canceled/DeadlineExceeded时，Manager.Create原先只识别本地context sentinel，会误存为FailedPrecondition；现保留跨RPC码及request_id重放结果。回归先RED确认误分类，再GREEN验证失败清理、名称/单会话配额释放、无tmux残留及不重复调用环境检查。`go test -race ./internal/server/session ./test/architecture`通过（23.683/1.659秒）；不扩大超时、不跳过实际region检查。此修复尚未发布/部署，且不解释用户原始deadline来源或修复公网丢包。

CLI同批修补：`sessions create`前置server/project选择失败明确标注CreateSession尚未发送；创建调用的Canceled/DeadlineExceeded/Unavailable提示结果可能未知、先查询同server/project的sessions list，并显示此次实际request_id。结果未知时仅以原参数/原ID核对原结果；列表缺失不能证明失败，同ID不会重跑已记录失败，确认原请求失败后才能显式使用新ID创建。明确业务拒绝不冒充未知结果；不自动重试、不自动补偿查询、不打印描述/tags/凭据。错误wrap保留原code/detail，控制字符转义，成功JSON和失败stdout契约不变。真实UDS/HTTP2 fixture覆盖显式/自动ID、两阶段失败及相同payload重放；相关CLI race回归通过（5.246秒、无skip），`go vet ./cmd/shiki`及差异检查通过，独立复核指出持久失败会被同ID重放，已修正不能永久禁止新ID的恢复措辞，精确CLI race回归再通过（2.026秒）；正式安装尚不包含此提示。

- [x] 保留失败证据、确认不存在后仅一次创建成功；复现真实画面延迟及有限网络损耗证据。
- [x] 会话跨RPC错误分类修复及失败/去重/清理race回归。
- [x] CLI创建失败阶段与原request_id恢复提示，真实IPC/HTTP2回归。
- [ ] 超时与卡顿的端到端根因闭环、最终候选实机稳定性复验；B02/B06/B09不得整体计通过，beta仍不发布。

七类复核：改动限宿主session错误分类与CLI创建错误提示，无host/plugin业务依赖跨界；RPC能力/权限不变，保留既有安全公开错误及原始取消/截止语义；持久schema不变、去重失败码一致；进程所有权和失败清理保持，不自动重试修改或重启Chrome/tmux；文件/终端/网络限额和默认期限不变；正式两端bin/安装配置不变。测试与外部失败分列，08/15/16同步；未用当前修补覆盖历史发行证据。自有测试页/viewer已关闭、查看凭据撤销，画质/分辨率恢复，任务截图/临时netstack脚本已清理；work原实例、项目、正式版本、agent-remote和账号状态保留，Claude授权继续暂停。

## VPS日本地区检查与显式应用（2026-11-19）

用户要求账号授权只在VPS Chrome内完成，先核对日本环境与出口；明确批准IPinfo和ipapi.co查询，随后明确选择先应用日本时区/语言、账号登录继续暂停。通过正式alpha.7 region/browser诊断分别在主机与work Chrome执行：两方在两家服务均观察到同一出口IPv4；IPinfo声明JP/Tokyo，ipapi.co声明US/Los Angeles。结果是地理库分歧，不是两个实际出口或流量经过美国的证据，也不能推断Claude使用的数据、账号资格或登录保证。实际IPv6出口、权威DNS递归来源和Claude登录页面可用性未验证。

应用前系统为Asia/Shanghai、en_US.UTF-8；work Chrome原生Intl为Asia/Tokyo，navigator为ja-JP/ja，目标平台Linux。按用户审阅的精确region plan/revision执行后，operation为applied；再inspect实际系统为Asia/Tokyo、ja_JP.UTF-8、LANGUAGE ja_JP:ja，状态ready且无待确认DNS操作。既有DNS服务器及chrome_mode=unchanged保持；未修改路由、代理、NTP、账号或重启应用。work原Chrome实例继续运行，复查原生东京时区和日语一致，browser restart_required=false。region操作的restart_required=true提示既有进程环境不会自动更新，不能据系统值宣称所有旧shell/Claude已采用新默认；后续授权前应使用新会话核对。

- [x] 用户授权的两供应商、主机与实际远程Chrome出口对照及地区配置检查。
- [x] 明确批准后应用日本系统时区/语言，实际值和Chrome再次核对；任务临时JSON及目录已清理。
- [ ] IP地理库分歧与服务实际地区识别仍未确定；Claude授权按用户要求暂停。

七类复核：使用现有公开region/browser契约，SDK-host-plugin目录边界、RPC/权限能力和数据schema无代码变更；计划/revision检查及既有事务记录保持。系统配置变更与已启动进程环境分开，普通UID/独立Chrome所有权、文件/终端/网络限额和安装布局不变。外部查询仅显式批准的两供应商，未发账号/Token/Cookie或综合指纹；本机查看器不冒充VPS Chrome证据。08/15/16同步，查询分歧、未测项及账号暂停如实保留。

## 用户终端方向键反馈（2026-11-19，诊断中）

用户提供alpha.7基线输出：版本一致、项目查询成功、本地3个及远端4个插件ready，work浏览器running；这是用户回传的B01证据，不是新独立实测。B02普通用户与项目路径、测试文件读写正常，但空shell中方向键显示`^[[A/B/C/D`，回车后/bin/sh报not found。B02交互编辑尚未通过。

源码核对发现`internal/server/session/environment.go`的非空地区环境包装在argv为空时显式使用`/bin/sh`，与现场shell一致；Debian的sh可能没有交互行编辑。该证据不能单独证明所有按键转发正确。建议当前测试shell临时exec干净交互bash做对照，保留会话和文件；是否恢复待用户实测，未修改生产源码、用户登录shell或服务。

- [x] 记录用户B01基线和B02失败，定位候选shell选择路径。
- [x] 用户确认切换干净交互Bash后方向键正常。
- [ ] 默认交互shell行为修复及相应回归。

七类复核：本批仅记录诊断和有界临时操作建议，目录/SDK-host-plugin边界、RPC/权限能力、持久schema/revision/去重、进程管理与恢复、文件/终端/网络协议、CLI/安装布局均无生产变更；临时shell替换不作为修复已部署证据。08/15/16一致，B02不因文件读写通过而整体勾选。

## beta 用户实机验收计划（2026-11-18，计划完成、执行待办）

用户要求详细可执行测试流程、助手/用户分工，并以完成测试后进入beta为目标。[30 实机测试手册](30-beta-user-acceptance.md)按正式alpha.7帮助及源码核对项目/会话、sync冲突、port转发、browser控制/文件/持久化、region只读计划、网络恢复、采样、首装及可选重启。真实账号授权由用户完成；主机重启、整机DNS/代理变更不因编写计划获得执行授权。本批未运行新实机用例、未新建测试项目、未改变两端服务或创建beta标签。

已交付的alpha.7文档固定源9f55e5a的CI44120621767最终57/57成功；首轮3项ARM Mac因托管runner未领取而取消，同源原条件补跑通过。该证据不覆盖本次尚未执行的用户实测，历史beta发布前门槛与新增实机建议分别记录。

- [x] 对照设计基线、当前支持声明和正式CLI帮助完成可执行手册及分工。
- [ ] B01–B09新一轮用户实测、偶发超时定位及发现的问题修复/复验。
- [ ] B10真实新向导首装补验；B11物理重启另约窗口，不执行则保留未实测。
- [ ] B12最终候选回归、beta签名发布与公开包验收。

七类复核：本批仅文档新增，无生产目录/SDK-host-plugin依赖、RPC准入/授权/能力、持久状态/原子性/revision/去重、进程所有权/取消恢复、文件/终端/网络限额、CLI或安装布局变更。采样不记录项目正文/原始秘密，不重试修改；新映射暂停、逐项冲突决策、显式浏览器控制与持久boot选择保持。旧通过、此次计划和实际外部验证分别标注；08/15/16进度一致。命令帮助与文档校验不冒充真实测试通过。

本批校验：正式alpha.7命令帮助与对应源码核对；手册25个shell代码块语法及嵌入Python语法通过，相对链接和新增内容秘密模式检查通过。独立只读复核未见阻塞项，已将当前窗口PATH设置移到首次短命令之前，并区分强制只读查看器与允许申请控制的查看器。未执行这些实机测试命令。

## alpha.7 发布、两端升级与实测（2026-11-17，公开发行、两端升级、交互实测与清理完成）

用户已明确批准发布v0.0.1-alpha.7并升级VPS/Mac，允许新建ego-browser工作区继续画面、鼠标控制及显式重连实测，测试后清理本批Shikigami开发/临时产物。agent-remote、正式版本与安装备份、节点身份、凭据、配置、项目、profile和密钥环保留。账号登录、主机重启及网络/代理/路由变更不属于本次操作。

两项生产补修的固定源c5c8f96a4c6e96b95c7993a0d81fca7bb86b0b1f已完成[CI44043767221](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44043767221)57/57成功；WSL2完整阶段通过，无未执行阶段，新增身份预算回归2.54s、浏览器本地清理回归0.21s通过。此前浏览器补修6850e77的独立CI44037921171亦57/57通过。发布说明冻结时生产代码保持该已验源码；标签的完整CI、签名公开资产及两端部署结果在下文分别记录。

七类复核：目录及SDK/host/plugin依赖、RPC/鉴权/能力、持久状态与去重、进程所有权与取消、文件/终端/网络语义、CLI与签名安装布局均沿用已复核实现，无新接口或配置策略；冻结说明区分本地SDK清理与远端释放确认、原调用预算与网络故障。CI通过不替代真实画面/鼠标、网络长稳、账号或物理重启证据。

标签固定源为`cbf3cdfcec1c598e7d9ee4279c4054cdc2eb96bb`，分支CI44091466319和发行44091646249分别执行。发行首轮WSL2 job134068085354在13:20:03–14:05:03 UTC失去hosted runner通信，GitHub注释明确记录该原因，日志资产返回404/BlobNotFound；无法确认具体测试或底层失联原因。同源分支WSL2 job134067362834已成功。发行macOS Intel internal job134070372881达到既有20分钟上限被取消；已取得日志含1081个测试pass事件、无fail事件，session包尚未结束，因此整项不计通过。后续同源补跑保持原断言和超时，结果另记；不移动标签或覆盖签名资产。发行macOS ARM unit-pkg job134070372451在13:23:58–14:23:30 UTC排队后取消，steps为空；GitHub明确注释hosted runner多次未能领取任务及ARM容量限制。该项未执行，不计通过，保留到同源补跑。分支CI的macOS ARM unit-cmd job134071095835同样因runner未领取而于14:28:30 UTC取消，steps为空；亦须原条件补跑。

发行等待期间的alpha.6交互基线（不是alpha.7验收）：在用户已授权的新工作区中，实际work画面从只读显式接管，新建仅用于测试的远端标签，通过键盘载入本地data测试页；鼠标按钮计数0→1、输入alpha6-test、滚动至END均由画面确认。一次显式重连恢复Connected/Read-only，再次明确接管后关闭测试标签，原about:blank保留；随后关闭本地viewer，未停止Chrome。状态查询多次短暂unavailable后恢复，不能计公网稳定通过。测试脚本一次误等Read-only session，而实际重连文案为Read-only，属于观察条件错误，不能记成产品重连超时。

固定标签源cbf3cdf的[分支CI44091466319](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44091466319)在attempt2达到57/57成功：首轮56成功、1个ARM容量取消；只补跑该未执行作业后通过，未改源码、断言或超时。当时发行自身CI、签名公开资产、部署及实测分别待验；最终结果见下文，不由分支CI替代。

alpha.7已于2026-11-17T15:25:25Z公开（draft=false、prerelease=true），固定标签源cbf3cdf；发行44091646249 attempt2最终60项成功、1项条件跳过（不计通过）。此前三项异常在原源码/原断言/原超时下补跑通过，WSL2为14:48:53–15:16:23 UTC，Intel internal为14:48:54–15:04:04 UTC。23公开资产、四目标Ed25519签名、归档安全成员、checksum pin、payload哈希/大小/可执行模式、PROVENANCE源码与run、bootstrap来源和远端资产元数据均独立核对；Mac原生二进制报告alpha.7/cbf3cdf。Homebrew由bot提交9525301693a8e260b9ae5cd85c5bd477ddb2116c更新，使用已验资产重新生成Formula后逐字节一致。

两端正式升级已验证：Debian12/amd64的18个payload、Mac arm64的14个payload及VPS两项root支持程序均匹配独立已验签bundle；release.json逐字节、SHA256SUMS签名pin、receipt active/current一致且无pending事务，原Plan保持。daemon与LaunchAgent实际可执行路径指向正式alpha.7；daemon active/running、NRestarts=0、显式region的有效NoNewPrivileges=no，keyring/region guard服务active。两节点原地址及daemon身份保持；Mac配置/plist/凭据、VPS配置/Token/加密凭据/keyring/region策略文件逐字节保留，插件持久bucket及profile定义的canonical hash一致。7个local/remote插件均alpha.7 ready。安装器切换后的首次探测返回node_uninitialized/exit3；稍后Mac为local_application_ready、两节点online，VPS仍application_pending/CLAUDE_LOGIN_REQUIRED，未重登或换Token。支持升级按批准计划执行APT与官方Chrome检查并复用密钥环；Chrome显式停止且无pending jobs后更新runtime，随后work一次启动成功，Chrome157.0.8103.78、原region revision保持；浏览器交互结果另记。

alpha.7实机交互已完成：15:42:14–15:50:21 UTC约487秒观察窗口，真实work屏幕中鼠标计数0→1、键盘输入alpha7-test、滚轮至END；显式Reconnect screen约1806ms恢复Connected/read-only，原计数及文本保留，没有自动接管。再次明确Take control后计数1→2、追加-reconnected成功。该窗口未观察到断屏或状态unavailable；只证明本次短时操作，不外推公网长期稳定。仅本轮测试标签被关闭，原about:blank保留；本地viewer关闭后原Chrome实例仍running且attachments=0。测试工作区已finish关闭。早先工作区在长时间CI等待后消失、列表为空，按用户已授权新建工作区继续，未将工具工作区故障记作产品故障。

随后独立CLI采样（查看器已关闭，15:50:59–15:52:58 UTC）：12次默认5秒预算projects list，11次成功（0.175–1.319秒），第8次在5.020秒返回deadline_exceeded/operation deadline exceeded，之后4次恢复成功。保留原超时且未重放修改，不能据此宣称链路完全稳定；该错误本身不能区分底层网络或服务响应的原因。与升级前12/12成功的短窗口并列，不能据样本推断版本导致网络变差或已消除历史故障。交互结束后再次核对两端签名payload、receipt和全部基线保留项通过。

- [x] alpha.7生产源码完整CI与发布授权。
- [x] alpha.7签名公开发行及四目标独立验证。
- [x] VPS/Mac签名升级、数据保留验证与浏览器实测。
- [x] 本轮测试产物清理与最终交付记录。

本轮清理已完成：核对准确任务目录、所有者、无符号链接及运行引用后，删除VPS升级staging的32个普通文件（逻辑大小427,395,945字节）和Mac私有验签/升级/观察目录的82个普通文件（727,517,002字节）；此前10张alpha.6基线截图已删除。逻辑大小不是实测磁盘释放量。测试标签、viewer和工具工作区已关闭，临时SSH维护连接已关闭；正式Chrome继续运行。清理后VPS仍运行正式alpha.7，daemon active/running、NRestarts=0。agent-remote、正式release/backups、配置、身份、凭据、项目、profile和密钥环保留。

收尾七类复核：本次仅同步发行、部署、清理证据和当前安装指南，生产目录及SDK/host/plugin依赖、RPC准入/授权/能力、持久状态/原子性/revision/去重、进程归属/取消/恢复、文件/终端/网络语义与限额、CLI契约及安装布局均无新增改变。08/15/16状态一致；短时实测、1次CLI超时及未验账号/物理重启分别记录，不将历史稳定性组合项追认通过。

## alpha.6 安装问题统一修复（2026-11-17，公开发行与两端升级完成，实机复验进行中）

用户授权发布alpha.6并升级VPS/Mac后清理Shikigami开发产物，明确保留agent-remote。完整问题表与清理边界见[29 安装问题跟进](29-alpha6-installation-followup.md)。本批覆盖：地址/标签与端口输入校验、原地纠正和保留pin的地址修正；y/yes/n/no与秘密输入说明/PTY取消恢复；控制面离线不重新发起登录且不误换Token；准确的启动失败阶段；受保护shell文件逐个处理与批准后仅修正准确home写权限；显式region支持的受管daemon策略及有条件daemon-only重启；插件修改只读状态恢复；短探测不误杀共享流、仅同一成功在途检查合并；浏览器显式重连与旧代回调隔离、独立启动状态/画面。

本地证据：引导/地址/PTY专项race三轮7.188s，最终3.731s，节点授权/Token区分三项十轮1.617s；连接池agent整包race63.943s、architecture1.626s；浏览器实际JS行为10/10通过，原HEAD首批6/6失败，真实loopback重连20次×20轮race通过、client整包race1.902s；安装器Mac两包race3.742s/10.697s，Linux arm64测试在隔离Debian12容器实际执行通过。相关vet/actionlint/diff检查通过，最终组合复验另记，不外推公网稳定性。

七类复核：目录及SDK/host/plugin依赖保持；RPC、权限和能力未扩展，region仍仅已批准六动作sudo，core NoNewPrivileges=true；证明仅内存，持久identity/revision/去重不变；原截止/取消和进程归属保持，屏幕重连不自动接管；文件/终端/网络限额保持，home不递归修改，秘密不输出；CLI与安装计划明示可操作错误及变更，签名旧版本不覆盖；原红新绿、本地/容器结果与公开CI、部署、外部网络门槛分开。

补充全量回归发现：首次 `make test` 在Unix socket发布与一次性auth key登录两项失败，其余已执行包通过。Mac默认大小写不敏感文件系统上，临时socket名可能与公开名仅大小写不同，现绑定前排除别名并新增确定性回归；connection整包race三轮4.971s通过。tsnet启动会自行注册，原实现晚传auth key可把空key注册拒绝归入新登录（旧实现十轮六次失败）；现于SDK启动前仅内存传入key，复用已有新注册，仅缓存旧身份仍Running时明确请求注册。node整包race73.473s、key注册/持久秘密检查及四种启动状态十轮10.562s通过。未延长deadline或跳过身份验证。查看器原生disconnect亦等待旧attachment有界关闭，最终JS行为10/10通过。最终完整复验与公开CI另记。

完整本地 `make test` 复验通过：106项Python工具测试、10项查看器行为测试、全部Go race包和两组发行工具测试；其中hostrotation218.350s、插件跨进程集成65.296s。随后独立复核补齐auth key恢复中SDK处于NoState/Starting的分支，在原5秒调用预算内等待状态明确，避免接收旧身份为新登录或盲目重复注册；实际SDK身份轮换及状态/截止专项race最终三轮9.204s通过；旧逻辑overlay下三种延迟启动回归失败，真实SDK虽Running但应用15秒后仍authenticating，临时overlay已清理。正式控制面的单次key消费语义仍按SDK契约，未为本批在用户节点上重登或重置身份；最终node整包race复验74.538s通过。`make lint`、actionlint、diff检查通过。

CI首轮固定源 `ccce4fa72fa3fbfd6f0cfa23bdf574219921eeda` 的 [43896776154](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43896776154) 结束为失败：49项成功、WSL2一项失败、3项macOS ARM作业取消且无执行步骤、2项后置矩阵跳过；取消/跳过不计通过。WSL2产品、E2E、embedded、terminal及发行工具全部通过，systemd-lifecycle在crash-guard阶段扫描 `/proc/*/stat` 失败（test-systemd.py原353行；error_kind=other），原异常类型无法由公开证据确认。

2026-11-17补修仅涉及验收检查器：先筛选准确插件可执行名，再按字节解析stat的最终右括号；仅将ENOENT/ESRCH作为进程退出，权限错误及损坏worker数据仍失败。固定异常分类新增ProcessLookupError/IndexError/UnicodeDecodeError，不导出异常正文或私密路径。旧代码的上述三种脆弱情况均确定性复现，但不冒称已确认原WSL具体异常。工具测试112/112（24.391s）、平台工具30/30（1.234s）、最终专项6/6通过，diff检查通过。七类复核确认生产目录依赖、RPC/授权、状态/事务、进程策略、网络/文件边界、CLI/安装布局不变；仅提高进程快照检查的正确性和失败分类，崩溃恢复断言及时间限制保持。新固定源完整CI结果见下段。

固定源 `377551af19b2f4a51e1057b782148c14a915797a` 的 [CI43906750506](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43906750506) 最终57/57成功。WSL2复验完整通过：incomplete_stages为空，systemd 13项操作、agent/daemon升级与回滚、清理均通过。期间macOS ARM预检查/终端作业因hosted runner多次未能接收而取消；Intel CLI作业一次达到15分钟上限且日志API返回404，不能确定具体耗时原因。同源补跑后该CLI作业8分50秒成功；没有改变断言或超时，没有移动标签，取消/跳过均待补跑成功后才计通过。不可变alpha.6标签指向该提交；[发行44017118492](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44017118492) 已成功（60项成功、1项条件跳过；跳过的复用Homebrew分支不计通过）。2026-11-17T05:29:09Z公开23资产，draft=false、prerelease=true；四目标独立验签、归档成员/payload/平台/固定源码核对通过。Homebrew由bot提交25740ba5cb9baeb258bf988f9e32d429bf22d37a自动更新，本机用已验公开资产重新生成Formula并逐字节比对通过。



2026-11-17实机部署：VPS Debian12/amd64与Mac arm64已通过签名alpha.6安装器升级；全部安装payload、VPS两个root支持程序哈希匹配公开manifest，运行daemon及LaunchAgent均指向正式release。首次安装器探测返回node_uninitialized，随后两节点online，Mac local_application_ready，VPS application_pending/CLAUDE_LOGIN_REQUIRED。原daemon ID、tailnet地址、Mac配置/凭据、VPS配置/设备Token/加密凭据/密钥环字节保持；插件持久bucket与升级备份一致，事务记录变化不冒称整个状态文件未改。local sync/port/browser及remote sync/port/region/browser全部alpha.6 ready。旧90-region-helper.conf仅在内容精确匹配且新50受管策略生效后移除。Chrome显式停止后更新runtime；首次恢复请求service unavailable，确认stopped且无systemd待执行任务后再次启动成功，work profile保留。当时浏览器界面复验因原工具工作区不存在而未完成；现已获alpha.7新工作区授权，结果见本页alpha.7记录。项目请求短时采样另记，不宣称公网长期稳定。

开发产物清理：确认正式服务接管、路径所有者/inode及运行引用后，删除38个Mac开发/诊断/测试路径（逻辑大小5,273,780,157字节，不等同实际磁盘释放量）；Docker残留共享句柄仅在所有容器均无对应挂载后排除，未停止/删除其他容器。VPS已删除哈希匹配的alpha.4旧脚本、精确旧策略及本批升级staging；Mac另清理26个已完成的alpha.6测试/CI helper与日志，本批私有验签/观察临时目录也已在证据入文档后删除。agent-remote、正式release/backups、身份、配置、项目、profile和密钥环保留。七类复核确认此次仅按既有安装/服务归属和授权完成部署，无接口、权限白名单、持久身份或网络配置新增变更；指南补齐实际CLI诊断和显式重连步骤，跳过/账号/物理重启仍不计通过。


### 发布后交付记录 CI 的进程观测补修（2026-11-17）

交付文档提交f0a06bde34124d8881f3d52cea2a884079028af1的[CI44026669305](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44026669305)结束为52项成功、1项失败、2项后置作业跳过，跳过不计通过。失败job133850471668为macOS Intel重型集成：TestRealHostRotationRecoversInflightFiles/server/download在inflight_test.go:95调用resources.Children，直接子进程的Exe查询返回`unknown error: proc_pidpath returned 0`。此错误本身不能证明当次进程退出或权限问题，不能仅凭字符串忽略。

补修仅涉及测试夹具test/fixtures/resources：正常候选仍只查询可执行路径；明确缺失或上述Darwin模糊结果触发现有processidentity.Inspector独立确认，只有ErrDisappeared才能跳过。活进程、权限/未知错误仍失败，expected数量、直接子进程范围、原截止、回收断言不变，不增加重试/超时。真实私有子进程在枚举后、Exe查询时回收的四分支回归，在旧错误处理overlay下全红；修后resources/processidentity专项race三轮通过（2.565s/4.974s），vet与diff检查通过，overlay与子进程均已清理。固定测试源码的受影响整包race复验全部通过：hostrotation219.045s、rotation35.447s、syncack42.019s、port14.586s、architecture1.405s。首次调用误设GOTOOLCHAIN=local而命中本机Go1.26.3，未运行测试；改用项目固定Go1.26.11后获得上述结果，不将前次工具链拒绝计通过。随后be7fada固定源CI的进程夹具检查通过，整轮的独立浏览器失败见下段。

七类复核：改动仅为test夹具复用已有test诊断观察器，生产目录/SDK与插件边界不变；RPC、权限、能力、持久状态/revision/事务、生产进程与网络/终端行为、CLI和安装布局均无改动。无法确认消失仍失败，不以重跑或放宽断言掩盖未知结果。已发布alpha.6标签、签名资产和两端生产二进制保持不变；这批不是新的产品版本。


### alpha.6 发布后发现的本地 attachment 清理竞态（2026-11-17）

固定源be7fada的[CI44031253619](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44031253619)已结束（56成功、1失败）；WSL2 job133863472571的唯一产品失败为plugins/browser/client/TestViewerExplicitReconnectReleasesPreviousAttachment（stream_test.go:220，新的WebSocket Dial失败，耗时0.01s）；公共证据不含原始错误正文，不能补造HTTP状态。该轮systemd维护13项操作、升级/回滚与清理通过；产品阶段失败导致E2E/embedded/terminal/release阶段未执行，不能计通过。macOS进程观察夹具的补修在本轮原平台及WSL已通过。

源码检查与独立确定性复现确认：WebSocket关闭ACK早于旧SDK stream清理及双向pump退出。原实现只看context取消，既可能在取消前拒绝新Dial，也可能在已取消但清理未完成时提前创建第二条远端stream。旧代码在阻塞SDK Close的回归中opened=2；不是把这条复现冒充原WSL未公开的错误正文。

修补限定browser/client：attachment.done在本地SDK Close返回、socket清理及双向pump退出后关闭；这不是远端释放确认；最多一个新请求等待旧done，额外竞争者409，等待不取消既有连接。等待和远端READY共享原operationTimeout截止，取消、scope、Origin/CSRF、单attachment、只读和显式控制权保持，不重放输入。真实loopback测试在旧Close阻塞期间要求新Dial在原预算内504且opened仍1，释放后只创建一次只读新连接；单等待者、额外竞争者及取消隔离有确定性覆盖。专项race十轮、client整包race（2.060s）、前端10/10、vet和diff检查通过；独立只读复核未发现阻塞问题，架构整包race通过（1.659s）。固定源6850e77899f6a8f3424bb9ad393b01ce478c8ab4的[CI44037921171](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44037921171)最终57/57成功；WSL2公开证据无未执行阶段，产品/E2E/embedded/terminal/release及systemd生命周期全部通过，Linux/macOS维护验收通过。该固定源尚不含下述身份复核预算修补。

七类复核：生产业务仍归plugins/browser/client，未引入host依赖或新远端RPC/权限；新增仅内存完成信号与单等待标志，不改持久配置/修订/去重。本地清理完成前不释放连接所有权，所有等待受原预算和生命周期约束；网络/输入限额及安装布局不变，CLI/公开能力不新增。此修补尚未进入已发布且不可覆盖的alpha.6资产；该次记录时两端仍为已验签alpha.6；用户随后已授权alpha.7发布及升级，部署与实测结果见本页alpha.7记录，不能由源码回归直接声称该竞态已在用户环境消除。

### 已有连接身份复核预算跟进（2026-11-17）

下列采样时两端运行公开alpha.6。07:21:40–07:31:06 UTC的11次默认5秒项目查询，8次成功（0.180–2.315s），3次失败（2.552/5.057/2.553s）；这三次未保留原始错误正文，不能补造分类。随后独立普通查询捕获2.517s的`shiki: unavailable: service unavailable`。另12次结构化查询均成功（0.181–2.548s）；后半段诊断与原采样交叠，可能共享在途身份检查，不能据此认定输出格式影响可靠性。doctor另次快照为online/direct、7插件ready；这些成功不否定失败或证明公网长期稳定。

只读审查确认一条与2.5s吻合但尚未在现场独立证明的代码路径：已有池的verifyRemote固定将调用剩余预算分成两份；默认5s调用的探测约2.5s到期，callerLimited分支直接返回而不使用为route替换预留的后半预算。探测错误经既有脱敏成为Unavailable/service unavailable；该文案也可能来自其他故障，不能反推唯一根因。

修补只对已有池的身份复核使用原调用剩余预算、最多5s；首次建立多路线的公平分摊保持，CLI/短RPC总预算不变，不增加业务重试或跳过身份验证。父预算≤5s到期仍仅失败该调用，不计池失败；剩余预算>5s且<10s时现在可以完成一次5s探测并进入已有持续失败观察，恢复仍受父截止约束；≥10s或无父截止的5s上限保持。确认的可用性策略缺陷与现场根因分开记录，不能宣称此修补消除公网故障。确定性延迟回归中，旧代码在4s父预算约2.08s返回Unavailable，父仍剩约2s；修后2.5s释放探测，完成真实HTTP/2认证和终端READY，业务计数为1且仍在原父预算内。短400ms调用要求父截止错误、原共享终端仍传输且池/重试/失败观察不变；6s调用验证完整5s失败可计入观察。专项race60.455s通过，最后仅增强测试释放发送的取消保护后专项4.151s通过。独立最终复核未发现阻塞问题；vet、架构race（1.402s）和diff检查通过，最终固定测试源码的agent/connection/architecture整包race分别72.285s/2.776s/1.198s通过；该生产源码c5c8f96的CI44043767221已57/57通过；alpha.7标签cbf3cdf的发行门槛另记。不可变alpha.6资产不包含它。

七类复核范围：仅host agent连接策略及真实HTTP/2测试，SDK/插件依赖边界不变；无新RPC、权限或能力，pin/凭据代际和每次独立认证保持；只内存探测预算变化，持久状态、revision、原子写和去重不变；父截止/取消、进程所有权和共享流隔离保持；终端输入、文件、端口及网络配置不变；CLI总超时、签名资产和安装布局不变；本地源码验证、固定源CI与当时尚未部署的生产修补分开登记。

- [x] 已确认问题汇总与源码回归覆盖（29记录）。
- [x] alpha.6固定源完整CI、签名公开发行与资产验证。
- [ ] 两端签名升级、稳定性复验与开发产物清理。


## alpha.5 实机连接波动跟进（2026-11-16，本地源码验证及 Mac 热修复试用，公网故障仍复现）

用户已完成 VPS 密钥环登记/解锁、Tailscale 授权、Mac 配对及 sync/port/region/browser 向导，真实远程 Chrome `work` 画面已出现；Claude 账号登录和物理重启没有新增验收证据。Mac 主目录原0775导致插件目录祖先检查失败，现场仅移除主目录组写权限后本地插件ready；VPS daemon 的 `NoNewPrivileges=true` 与region固定sudo helper冲突，用户应用独立systemd覆盖后region/browser ready。这两项现场修复不等于alpha.5安装器已自动处理，后续安装诊断/权限策略仍需单列修复，禁止绕过插件目录或sudo白名单校验。

连接故障现场：正式alpha.5查看器状态曾200 → 约5002ms的504 → 200 → 503并断屏；用户确认未启用代理/VPN。临时诊断agent仅在私有临时目录构建，记录故障分类而不记录Token、URL或原始错误；2026-11-16T10:31:26.644Z及10:31:31.646Z连续本地身份探测超时，紧接10:31:31.646Z的共享流取消，bad=true、pool_canceled=true，随后新路线探测也超时。证明两次5秒探测会放大链路慢响应为共享流终止，不证明底层网络为何慢，也不能归因于中继或VPS资源不足。该轮临时agent已退出并恢复正式LaunchAgent；再次通过RPC确认原`work`实例仍running。随后本机切换热修复试用，见下段；正式发布包和签名未改动。

本批修复目标：保留每次新RPC的身份复核、原请求deadline和信任失败立即撤销；完整探测超时需至少两次、且从首次观察到完整超时开始的持续失败窗口达到30秒，才替换共享池。成功身份协商清除窗口；在后续失败检查中观察到已认证Receive进展会重置窗口，静默流不因两次短慢探测立即断开，真正持续无进展仍有界恢复。CLI仅在插件修改已接受之后，对临时Unavailable/DeadlineExceeded继续查询同一revision，保持原等待截止，不重放修改、输入或控制权接管。

验证状态：CLI真实UDS回归确认两次临时查询失败后成功且配置修改只提交一次，覆盖永久错误、revision变化和取消/截止；专项race三次通过。连接池真实HTTP/2专项race通过（49.734s）；首轮整包race的CLI（46.675s）和architecture（1.365s）通过，agent的runtime夹具因误用真实用户plugins_dir而失败，现将其隔离到夹具私有目录，保留首次失败并重跑agent整包；go vet和diff检查通过。agent整包race复验通过（62.594s），七类架构复核完成；修复尚未公开发布；已在用户 Mac 以独立路径运行本地 agent 热修复，但后续实测仍复现故障，不宣称公网稳定通过。

- [x] 本批源码修复与真实HTTP/2回归、CLI只读查询重试及七类复核闭环；agent/CLI/architecture整包race与定向vet通过，测试夹具首次失败及隔离修正保留。
- [ ] 修复版公开发行、用户双机更新及公网长时间使用验收。

实机热修复试用：2026-11-16T10:48Z 构建 `local-hotfix-7ad65ab3765e`（基于4462f1f及本批未提交补丁；Version仍为alpha.5以匹配内置插件，Commit明确标为本地构建）。二进制、构建记录、原LaunchAgent备份与回退脚本保存在用户私有hotfix目录；只原子替换LaunchAgent程序路径并保留安装器managed标记，没有覆盖签名release/current或用户CLI链接。新agent及local sync/port/browser均ready，真实VPS Chrome保持原实例。CLI修复二进制也已构建，但用户默认CLI链接仍是正式alpha.5。

真实查看器失败保留：首轮观察中状态查询失败但画面暂留Connected，约62秒状态恢复、95秒控制权由远端流确认，随后点击空白/滚动未观察到画面响应；约146秒仍断开。Connected标签不证明帧仍在到达，不能将本轮计为鼠标或稳定性验收通过。局部修复没有消除全部故障；继续用不含Token/URL/请求头的时序诊断区分探测排队、传输与服务端响应。现场原始观察记录保存在私有 `live-observation-1.json`。

后续隔离诊断：同一补丁加临时HTTP trace的agent记录请求写出通常0ms、首响应约2–4秒或5秒探测超时，path查询0ms；一轮54次探测43次成功、11次未完成，该区间没有池主动失效。诊断v4额外区分浏览器方法和TSMP，专项race50.011s通过；但重启后节点成为 `offline/NODE_CONTROL_UNAVAILABLE`，未获得有效TSMP对照，不能计通过。Mac无代理环境变量，系统HTTP/HTTPS/SOCKS/PAC开关均0；直接HTTPS访问controlplane.tailscale.com与github.com均在TLS握手报SSL_ERROR_SYSCALL（约7.03s/6.30s），显式--noproxy的控制面复查也失败（5.13s）。这是当前Mac至外部服务的连接失败证据，不证明具体ISP/NAT/中继或VPS故障，也不把它等同于先前全部断线的根因。

临时诊断已停止，LaunchAgent恢复为独立路径的无诊断热修复；原签名发行物、配置、Token及远程Chrome未被重置。当时建议用户切换Mac网络做同条件对照；后续用户要求保留网络复测，见下段。不能宣称安装重跑或加长超时能解决问题。

同网复测（2026-11-16T12:28–12:34Z，用户暂不切换网络）：无重装/凭据重置时，原热修复agent节点已online；显式无代理HTTPS检查恢复，控制面302/1.238s、GitHub200/0.726s。初次新查看器仍断屏，状态200/503交替。随后临时v4诊断复测中，TSMP成功12次（120–852ms）、超时1次、取消2次（取消不计通过）；同一时期身份探测及GetBrowserStatus仍出现deadline，不能用节点可达排除TCP丢包/队头阻塞，也不能据此断言服务端处理阻塞。

第二个查看器在181秒DOM观察中保持Connected，期间23次状态/控制HTTP请求有5次非200；控制请求200且流确认Controller，鼠标滚动后约11.1秒观察到实际画面变化。该窗口未出现主动池撤销引发的屏幕断开，最终BrowserStream取消来自明确关闭查看器；不把这次局部改善覆盖初次失败或声称长期稳定。VPS单次采样CPU18.7%、已用内存1715.7MiB、最小磁盘空闲35340.2MiB；不能据此推断全部资源或I/O健康。隔离诊断停止并恢复无诊断热修复，证据为私有 `retest-trace.json` 与 `retest-browser-observation.json`。本轮仅补充实测与文档，七类复核确认生产源码/协议/权限/持久身份未新增变更，已有签名发行物不变；精确根因及完整稳定性仍待验证，换网不是继续排查的前置要求。

### 并发探测与启动诊断补修（2026-11-16）

同一服务器代际的并发调用现在仅共享其实际等待的成功身份探测；复用要求相同池、相同完成门、完整持久记录及凭据revision一致，任何后续探测均清除旧证明，失败不形成缓存。后续独立调用仍探测，每个业务RPC仍鉴权，原deadline、信任失败立即停止及禁止重放保持。CLI启动读取失败增加本地服务器/映射、远端项目/会话阶段说明，用%w保留错误码、取消及结构化诊断。

真实HTTP/2并发/后续独立探测、旧成功被新失败替代、凭据更新和等待者取消专项race三轮通过（5.057s）；agent整包race63.943s、architecture1.626s通过；CLI真实PTY/UDS启动错误及现有首次运行/会话专项race2.110s通过；agent/CLI vet通过。七类复核：依赖和SDK/host/plugin归属未改变；RPC/权限/能力未扩展；复用证明仅内存态，持久身份/原子性/revision/去重保持；不新增后台任务或延长取消；文件/终端/网络限额保持；CLI仅增强错误上下文，签名发行布局不改；本地测试不替代外部稳定性或公开CI，原现场失败继续保留。

用户授权SSH诊断后实际登录VPS：daemon active/running，NRestarts=0，NoNewPrivileges=no；三次vmstat采样无blocked进程、无swap、wa/st均0，采样CPU idle96–99%。这些短采样不能排除间歇性I/O/网络问题。SSH首次握手超时、两次认证阶段连接关闭，随后只读命令成功；不将连接关闭误判为密码错误或已经确定的Shikigami根因。真实交互稳定性仍待复验。


本批后续实机证据：新本地构建 `local-hotfix-ec0873d4a3e0` 已在 `~/.local/share/shiki/hotfixes/coalescing-20261116T135216Z/` 保存源码补丁、构建摘要、上一版LaunchAgent/CLI链接及回退脚本；已切换Mac agent与默认CLI链接，未改签名release/current目录。CLI整包race47.204s通过。重启后节点短暂offline随后online；五次默认期限项目查询仍全部Unavailable（4.972/2.523/2.519/2.525/2.524s），之后真实PTY裸命令成功进入“Create a new remote project / Project:”提示并主动Ctrl+C退出，未创建项目/映射。浏览器open仍返回Unavailable，未获得可验的查看器，不计浏览器通过。

独立SSH对照：三个已有SSH连接上的true命令分别超过8s、超过8s和3.925s成功；后续只读状态命令8.004s完成。用SSH_CONNECTION确认所测TCP连接，服务器ss记录其累计重传34次、bytes_retrans9368、rtt847.39ms、rto14916ms（并非丢包率或逐包根因）。20秒有界strace仅统计flock/fsync/fdatasync：34次flock、无syscall错误、未观察到fsync；汇总CPU计时不是锁等待时长。daemon持续active、I/O压力avg10/60/300为0。证明额外SSH路径也出现慢响应与重传，不确定具体ISP/NAT/主机网络段，不将这些短采样推导为Shikigami已修复或所有延迟都来自同一根因。当时新热修复尚未提交/推送/发布，当前公网稳定性仍PENDING。


## alpha.4 实机反馈修复（2026-11-16，本地与公开发行验证完成）

固定源 `21fcb5dc1406c7e47d8d12584110433aeaa773d2` 的 [CI43814011028](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43814011028) 已57/57通过，无失败或跳过job。不可变 `v0.0.1-alpha.5` 的 [Release43819036138](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43819036138) 首次运行成功：60项成功，Homebrew复用分支按契约跳过1项（不计通过；源CI四目标Homebrew均通过）。2026-11-16T07:41:05Z公开，draft=false、prerelease=true；23资产、四目标签名/摘要/来源/成员、公开macOS arm64提示/版本/帮助/首次和重复暂存均验证通过。自动Formula提交 `62df599ad717541e4050f921347597d097567a07` 与从已验签资产重新生成的公式一致。

后续文档/Formula同步提交 `2a0e1883ec6097dac49bfda1d65584ef25cb741c` 的CI43826191775最终56项成功、1项失败；macOS Intel `unit-test-support`失败：`TestFrontendExplicitTimeoutIsScopedAndPreservesEarlierCallerDeadline` 于 frontend_timeout_test.go:124 报caller deadline被延长，测试耗时0.09s。运行时源码和公开alpha.5未改变。Connect通过相对毫秒预算传输超时、在服务端分发时重建绝对deadline；仅加入25ms分发延迟即复现旧断言失败（5s误测5.026919625s）。现只修正测试：HTTP入口检查实际预算并计入序列化耗时/毫秒截断，保留可控延迟，增加真实RPC取消；本地interceptor要求较早父deadline精确相等。原生相关race连续50次通过（multihost10.887s、client1.811s）；两整包race3.132s/20.846s及定向go vet通过；隔离无网络Debian12 arm64相关race连续25次通过（5.574s/1.018s），不修改生产超时或放宽契约。七类复核确认仅测试观测与文档变化，SDK/host/plugin归属、RPC/授权/能力、持久化/身份、生产进程/网络/安装布局均不变；测试资源仍有界回收。原失败保留；本段记录本地修复证据，新提交完整CI结果以关联Actions为准。

补充验收：固定源21fcb5d的Debian12/systemd252实际普通UID密钥环provision与停止后unlock通过，凭据保持私密；隔离容器挂载传播修正和验证范围见15，不冒称用户VPS完整安装。

用户实际 Debian 12 VPS 反馈：管道经 sudo 启动时确认输入阻塞；依赖安装无进度；密钥环登记服务以 `203/EXEC Permission denied` 失败。源码发现 bootstrap 的 `umask 077` 与公共目录创建权限不匹配。真实反馈只证明这些失败与已到达的安装步骤，不代表完整首装或账号授权通过。

- [x] 修复 console 首读/sudo PTY 与 macOS /dev/tty、非交互步骤10秒进度与耗时、严格 umask 下公共目录/服务文件权限及失败后的原地恢复；共享祖先不扩权，私密凭据不递归改权限。原生4包race/lint、bootstrap15项、发行工具33项、Linux整包与跨UID/sudo专项及七类架构复核通过，具体失败与修正见15/16。
- [x] 固定源码完整CI57/57、不可变alpha.5发行、23公开资产及四目标签名验证；公开macOS确认提示和离线暂存通过，真实完整用户安装仍在下一项。
- [ ] 用户 VPS 重新执行修复版向导；真实账号与物理重启仍按外部门槛登记。

**alpha.4 已发布（2026-11-16）：** 固定源 `12664aac1d5b2ae6f1d5bb81e4eb59033009f9b0` 的 [CI43749533031](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43749533031) 57/57 全部通过；不可变标签 `v0.0.1-alpha.4` 的 [Release43754919496](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43754919496) 第二次尝试成功，最终 60 项成功、Homebrew 复用分支按契约跳过 1 项（同源发布前四平台 Homebrew 均成功，跳过不计通过）。23 个公开资产、四目标签名/摘要/成员/来源、自动 Formula 及 macOS arm64 版本/帮助/首次和重复离线暂存已独立核验。新向导真实 VPS/Mac 首装、账号授权与物理重启仍未实测。

首次 WSL2 失败证据：`TestCLIThreeProcessTLSLifecycle` 在 `core_test.go:217` 首次 PTY 初始化调用处失败（8.45 秒），187/444 是清理日志位置，不是额外故障；尚未进入终端就绪输出读取。公开证据不含原始 errno，原因未确定，不宣称已证实基础设施故障。产品测试、发行工具和 systemd 生命周期通过；embedded-plugins 与独立 terminal 阶段未执行，不能计通过。同一源码发布前 WSL2 成功；相关 helper、PTY 依赖与平台脚本自 alpha.3 未修改。保留首次失败并在新 runner 上重试同源完整 WSL2，不改变断言或移动 tag。

第二次尝试在新 runner 重跑相同标签源码，WSL2 全部必需阶段及清理通过；`TestCLIThreeProcessTLSLifecycle` 为 32.79 秒成功，`incomplete_stages=[]`。未修改代码、断言或超时，也未移动标签；首次 PTY 初始化失败未复现，原始系统错误仍未知。最终成功不抹去首次失败和未运行阶段。

**原清单进度（2026-11-12）：42/42，原清单最后一项已关闭。** 功能、平台、验收依据及未支持范围统一见[支持声明](18-release-readiness.md#current-support)。Phase 0–5各阶段为11/11、7/7、6/6、6/6、6/6、6/6；认证生命周期和长稳/资源/性能沿用2026-11-01用户确认，历史实测/失败及源码范围保留。生产heap/object归因、真实Claude登录/推理及物理主机重启仍未实测。2026-11-15已发布alpha.3，新增地区/浏览器及发行证据单列如下，不扩大原42项或宣称稳定版。

## 新增体验：一键安装与首次使用（2026-11-16）

用户要求将手工部署收敛为 VPS/Mac 各一条命令，契约见 [28](28-quickstart.md)。此增量不属于原 42 项，也不追认为已发布 alpha.3 的能力。

- [x] 本地实现与隔离回归：`bootstrap --quickstart`、受信 installer 编排、普通 UID 依赖安装路径、连续登录引导、双端插件及浏览器配置、重复执行与失败恢复。bootstrap 15 项、客户端 13 项专项 race、安装器/支持检查 13 项专项 race、相关整包 race、Linux arm64 容器 20 项（零 skip）、Linux amd64 入口交叉构建及发行工具 28 项通过；系统支持复用、凭据续期、部分密钥环恢复、PATH 和已有登录衔接均已复核修正。此勾选是源码和隔离回归，不是实际 APT/账号/原生完整安装通过。
- [x] 发行门槛：alpha.4 固定源 CI 57/57、自动签名发行、23 公开资产及四目标独立验证、自动 Formula 和 macOS arm64 离线暂存完成；首次 WSL2 失败及同源重试记录见上方及 15。
- [ ] 真实环境门槛：新向导真实 VPS/Mac 从零安装、真实 Claude 授权和物理重启恢复仍未执行，不以本地夹具或 CI 替代。

08/15/16 同步记录本批范围、失败及后续修复。alpha.4 使用新一键入口；旧公开版仍使用原有安装入口，新增入口检测到不支持的旧 helper 会明确停止。

## alpha.3 发布执行记录（2026-11-15）

用户指定的`v0.0.1-alpha.3`已公开，不是beta或稳定版。固定源`d9056c3ffed86b2bbac9cd5e89c433eaa3093cb2`的[CI43723367228](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43723367228)57/57通过；同源[Release43727419792](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43727419792)成功，60项通过、复用检查中的Homebrew分支按工作流跳过1项，不计通过。23项公开资产、四目标签名/摘要/成员、bootstrap来源、自动Formula及Linux amd64版本/帮助/首次和重复暂存已独立核验，精确证据见15。新增原生体验、真实账号、公网和物理重启未验范围仍见[发行说明](releases/v0.0.1-alpha.3.md)；公开包实际升级/回滚未在本轮重复。

发布后清理（2026-11-16）已完成：删除本项目16个测试容器、7个镜像、2个缓存卷及按登记ID筛选的9.702 GB构建缓存；移除本机`.ci-artifacts/`、`.tools/`、临时下载/构建/测试目录和会话验收脚本。Docker清单及构建缓存复核为空，本机指定测试进程与临时目录为0。保留源码、本文验收记录、公开Release及共享工具缓存，不执行全局主机缓存清空；主动容器终止产生的143/137不作为新增验收失败。

首轮候选`dea8f77`的[CI43712715128](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43712715128)失败：四平台preflight均在gofmt门禁失败，WSL公开阶段证据也停在build-lint；四目标归档及四平台Homebrew通过。修正region journal/测试与host权限map三文件排版，本地三文件gofmt比对通过；无生产语义变化，完整CI待下一提交，不提前创建tag。

第二轮`d7be3ad`的[CI43714541188](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43714541188)为56/57；唯一失败是Linux插件测试的pidfd等待被信号中断（EINTR），并非已证明子进程残留。测试改为在同一2秒截止内继续等待，仍要求POLLIN证明退出；不修改生产取消/进程组逻辑。隔离Linux普通UID定向race通过（4.249s），完整CI随修复提交重新执行，通过前不创建tag。

第三轮`9575ec0`的[CI43718718959](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43718718959)仍为56/57；插件取消回归已通过，唯一失败为Linux维护准入`runtime_identity_changed`，未执行原生维护。维护job复用了另一runner的preflight身份，不能保证内核等字段一致。现改为在维护runner本机重新冻结源码/身份并执行build-lint、tool-tests和platform-tool-tests；保留26规定的全部身份、源码和取消门禁，不重写旧证据。30项平台工具回归通过，真实维护及完整矩阵由下一CI验证。

## 新增扩展：地区环境与远程浏览器（2026-11-15）

设计基线：[27](27-region-browser-plugins.md)。用户已授权完整实现；原Phase0–5的42/42及此前beta候选证据不覆盖以下新增项。

- [x] 固化用户确认的地区、权限、DNS、Chrome、profile/生命周期、输入/文件及诊断边界。
- [x] 类型化协议、SDK、宿主鉴权/路由/准入、CLI全链路；本地/容器证据见15。
- [x] region普通用户插件、root所有受限helper、版本化计划/应用/回滚及独立DNS恢复守卫；本地实现与隔离amd64验收完成，两架构原生平台门槛仍在下方单列。
- [x] browser持久profile、独立systemd用户运行实例、单运行/单开机目标、Chrome沙箱与人工崩溃恢复；物理开机门槛另列。
- [x] 本机受保护noVNC、服务端单控制者、中文/日文输入、剪贴板和授权文件通道；含真实查看器上传/下载。
- [x] 主机/目标Chrome/外部观察分层诊断，外部默认关闭、显式供应商与未知状态；公网观察另列。
- [x] 显式依赖安装/维护、受信产物打包、卸载保留敏感档案与回滚证据；私有签名与最终客户端增量分别绑定，不等于公开发行。
- [x] 本地/隔离Linux行为与真实界面验证；复核目录/协议/状态/进程/文件网络/CLI安装/证据七类契约。
- [ ] 用户介入与外部门槛：真实Claude登录、原生客户端/两架构平台、物理重启及公开发行按实际可用环境登记，未验不计通过。

当前：新增源码实现及授权范围内的本地/隔离amd64容器验收已完成。类型化SDK、宿主/CLI、region、browser、诊断、安装维护均有分层证据；最终schema2原生resolver、真实跨进程权限/取消、签名安装增量和查看器下载修复已完成。下列段落按发现先后保留历史证据，精确冻结源码与产物见15；不得拼接成一次全平台/同包全矩阵通过。用户账号、原生平台、物理重启、公网及公开发行仍在17项与27外部清单中保持未验。

增量证据：已安装helper拒绝未授权guard/activate、额外argv、任意JSON路径和普通用户直接执行；region诊断差异/未知分类回归通过。Ubuntu24.04完整镜像后续构建成功，隔离systemd容器已启动；这仅是环境就绪，不计浏览器或平台验收通过。

后续原生DNS证据已补齐：resolvconf/openresolv与systemd-resolved分别通过实际DNS/TLS检查、确认截止独立恢复、确认与显式回滚；两个resolvconf实现还验证竞争源/独占源的应用拒绝。均为容器私有DNS/TLS夹具，不是公共出口验证。宿主CLI/agent的darwin两架构及Linux arm64宿主/region交叉编译通过，不能替代原生运行。

浏览器增量：普通UID真实CLI经agent/直连TLS/独立browser插件完成profile创建重放、revision竞争拒绝、303360字节上传与重放、下载校验及0600权限、拒绝未授权覆盖和profile删除。实测发现上传已提交但代理额外Receive导致canceled；修正Connect client-stream单响应终态处理后，定向race及真实链路通过。真实Chrome画面、输入法、独立生命周期和安装仍未关闭。

密钥环增量：Debian12实际systemd host-key加密凭据、普通UID专用加密keyring创建、停止创建服务后由独立解锁服务重新解锁通过；不是安装器或物理开机验收。Docker私有`/run`需shared传播，否则凭据挂载为空；只调整可丢弃容器。真实启动还发现插件环境清理与runtime依赖XDG变量冲突，改由当前UID定位固定用户bus，相关runtime/server race通过；继续验证Chrome表面。

真实界面增量：Debian12普通UID的官方Chrome157已通过受保护noVNC显示、显式接管、英文导航、Unicode剪贴板和原生下载→CLI校验取回。noVNC使用未终结的zlib flush块，原解析器误拒绝并断线；修复保留解压上限和完整格式校验，RFB定向race通过。XIM快速输入丢字及分离总线上的IBus服务发现问题已定位，最终采用同一选定密钥环总线上的原生Fcitx GTK3前端；真实无人工延迟的Pinyin/Mozc输入、候选显示及提交通过。发行manifest/release Go检查及48项Python工具检查通过。

完整架构复核发现的事务代际/部分恢复、子进程取消、DNS备用服务器、浏览器发布崩溃窗/落盘后错误/编码上限、启动重认领、诊断CORS、上传超时和敏感目录同步准入缺口已修复并通过冻结专项回归。真实已安装sync插件证明敏感状态目录保护；不以模拟属性/私有socket证据替代真实重启或物理断电。

最终闭环：schema2在resolved/resolvconf/openresolv新隔离容器通过六动作、ABA拒绝/逆序回滚、独立30秒恢复及外部修改冲突；保留原生resolved速率限制及失败恢复证据。最终20文件签名包在Ubuntu完成真实setup、加密密钥环、普通UID Chrome/Fcitx与维护保活。该包不含随后发现的查看器包装EOF修复；该两文件增量另通过定向race、独立插件构建及真实网页上传→下载45字节/完整SHA256校验。未创建新tag/Release，也不声称有一份已发布且包含全部增量的新包。


**首个 beta 候选修复与验收（2026-11-14，发布前验收通过）：** 本批安装、签名 marketplace、插件运行时及发行工具修复已完成新增本地验收和固定候选 `a5032c1` 的完整远端 CI（57/57）。原42项保持原证据范围，不新增勾选；只关闭本批发布前门槛，实际 tag/Release、发行签名和公开包验收仍待执行，见[18](18-release-readiness.md#beta-readiness)。

本地证据已完成：bootstrap 真实签名、发布/公式工具、actionlint，SDK/host/sync-client、真实第三方生命周期及 sync/port CLI JSON race；真实 HTTPS 签名 marketplace 进程验收包含接受前 schema 拒绝、丢失响应恢复和后续有效更新。已独立验签并执行真实 alpha.1/alpha.2 helper 的迁移/回滚/卸载/再升级，保留第三方数据、原目录与锁 inode。真实 systemd 用户服务完成 bootstrap 安装、就绪、卸载与配置/状态保留；节点显式禁用，外部 peer 仍 pending。详见[15 本批记录](15-implementation-status.md#evidence-first-beta-candidate)。

候选 `058ea5d` 的 [CI 43464849797](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43464849797) 首轮失败：macOS 两架构均在 bootstrap 下载取消测试中超时，后续依赖矩阵未展开；两 Linux preflight、四平台编译/Homebrew 与 WSL2 通过。随后下载监督器改用有界 readiness 等待，并在退出前显式终止/回收生产子进程；Linux bootstrap 10项及强化的 HUP/INT/TERM 取消回归通过，macOS 结果由后续 CI 补齐。

下载取消修复 `87cfefb` 的 [CI 43470576055](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43470576055) 前两次为55/57：两项 macOS arm64 作业未获 runner 即取消。第三次为56/57，仅真实 launchd-lifecycle 失败，公开证据未包含私有日志；其测试仍要求卸载删除整个 plugins_dir，与保留用户插件目录的契约冲突。`a5032c1` 对齐 `.builtin` 移除、0700 私有目录及用户数据保留断言，不修改生产卸载语义。

最终 [CI 43484685340](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43484685340) 对固定提交 `a5032c112ffdace9b9ab6455f7280480223a8482` 首次执行即 completed/success，57/57，无失败、取消或跳过；覆盖四平台单元/集成/E2E/发行工具/Homebrew、WSL2、终端及原生维护。macOS 生命周期、活动上传升级、活动下载升级均通过。此前失败/取消保留历史记录，不以成功覆盖；本轮未创建 tag/Release。

最终记录提交 `96961ab` 的 [CI 43495414864](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43495414864) 首轮在 Intel macOS 第三方生命周期测试的 RESTART RPC 超时，下游维护/终端未执行。测试启动连接设置的 `Options.Timeout=1s` 同时作用于后续 RPC，覆盖产品默认生命周期15秒预算；现改用默认客户端选项，启动探测仍由单次1秒 context 限制，生命周期/去重/失败关闭断言不变。此项为测试预算修正，生产代码不变；修正后验证另行记录，不将本轮失败记为通过。

预算修正后的 Linux amd64 真实第三方完整生命周期定向 race 已通过（35.255秒）；新提交的完整远端 CI 待执行。原临时构建容器重启时入口脚本退出、验证进程被终止（137），未记为测试失败或通过；独立容器复用已冻结文件后正常完成上述验收。

**插件 marketplace 安装批次（2026-11-12）：** 新增 `internal/pluginmarket` 签名 envelope、Ed25519 trust store、严格 JSON/HTTPS/平台/归档身份校验和流式 SHA-256 验证；CLI 新增 `plugins trust`、`plugins market`、签名 marketplace 安装与更新路径。手工 URL/path 安装保持显式责任边界，不进入自动更新。Linux amd64 目标 CLI 编译和内部包测试编译通过；Windows 执行受 Unix-only 源码与 Application Control 限制，外部 marketplace/发布验证仍待，未新增路线图勾选或发布声明。


**CI 重跑与验证（2026-11-12）：** 提交 `91fe5c7` 的 [CI 43093710222](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43093710222) 首次执行的 57 项矩阵中，WSL2 的 `actions/upload-artifact@v7.0.2` 因 artifact 服务返回 HTML（`Unexpected token '<'`）在五次重试后失败；WSL 产品与维护测试本身已完成，首轮最终 53/57，另有四个下游 terminal/maintenance 作业未调度，不将该基础设施失败计为源码回归。完整重跑 attempt 2 已 completed/success，57/57 作业通过，无失败、取消或跳过，包含 WSL2、四平台插件/E2E/集成、terminal、maintenance、embedded、release 与 Homebrew；本轮无源码变更，首次失败与重跑均按实际结果保留。
**CI 修复批次（2026-11-12）：** 提交 `e4d660f` 的 [CI 43068229844](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43068229844) 因插件 manifest 扩展后未提交 protoc 生成绑定而失败：四个 native preflight 在 `git diff --exit-code -- pkg/protocol` 发现 `PluginInfo`、`InvokeAction` 及相关生成代码漂移；WSL2 job 同轮失败，公开产物未暴露其私有 Linux 细节。已按锁定工具版本补交生成文件，并将已被生成代码占用的手工重复 handler/PluginActions 服务迁移为生成 `Plugin.InvokeAction`，删除过时文件。协议包定向测试与固定工具重复生成通过；新远端 CI 结果待验证，本轮不计全绿。
**Go 安全补丁批次（2026-11-12）：** 为修复 Go 标准库 `net/http` 的 GO-2026-6859（1.26.10→1.26.11），已同步 `go.mod`、CI/release setup、WSL 固定下载摘要、Linux 测试镜像 pin、缓存键测试和当前开发/诊断文档。Windows 本机插件测试不能代表 Linux 构建，因 Unix-only 传输符号不可用；本地 Go 版本已切换到 1.26.11，远端复验待新提交结果。
**格式化修复批次（2026-11-12）：** Go 1.26.11 的 `gofmt` 在 native preflight 中发现四个既有源码文件的实际格式漂移（`plugins/port/client/worker.go`、`pkg/plugin/manifest.go`、`internal/client/client.go`、`internal/agent/ipc.go`）；已按工具输出修正，仅涉及导入缩进、注释格式、字段对齐和注释间空行。上一轮四平台 build/vet 均通过，格式门因该项失败；本轮相关 Linux 定向测试通过，远端复验待提交结果。
**CI 失败复现与修复（2026-11-12）：** 提交 `e4d660f` 的 [CI 43082895990](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43082895990) 完成 56/57 个任务，仅 `unit-internal (darwin, amd64)` 失败；日志明确失败用例为 `TestSDKInteractiveRecoveryWaitsForRegisteredMap`，在 45 秒内未通过“缓存 self map 仍可被 SDK 观察”的测试屏障。当前工作树已将测试门槛改为只要求 `Running`、self 存在且无认证 URL，同时继续校验旧注册 key 和未授权状态，避免把 `Self.Online` 的平台时序差异当作协议完成。Windows 本机该包因 Unix-only 符号无法编译，Linux 容器启动因 Docker 返回 125 未执行；待远端复验。
**本批CI修复与验证（2026-11-05）：** 补充提交`17ed77f`的[CI42017919620](https://github.com/DDSRem-Dev/Shikigami/actions/runs/42017919620)已completed/success，57/57任务通过，无失败、取消或跳过任务。修复首次tsnet注册被重复启动、过早公开URL及发送期间deadline被清理取消覆盖；host与独立SDK保持相同取消语义。macOS arm64两项原失败用例及四分支注册回归已直接读取日志确认pass；全部原生平台、WSL2、插件/E2E、终端和服务维护通过。旧CI41888706987的失败/容量取消及首轮CI42012596520的56/57结果保留。文档提交`d49310d`的稳定性复验[CI42023610286](https://github.com/DDSRem-Dev/Shikigami/actions/runs/42023610286)已结束，56/57通过，唯一失败为Intel macOS显式断网恢复登录等待URL超时；后续`ff338f9`的[CI42028867098](https://github.com/DDSRem-Dev/Shikigami/actions/runs/42028867098)已结束56/57，所有节点认证回归及Intel内部包通过，唯一失败为arm64快速退出pane的空PID解析。有效授权SDK复用及失败退役已通过本地与该轮节点验证；本批继续修复tmux 3.8关闭PTY但waitpid状态未到的合法过渡，并明确禁止据此自动重启或开始自动清理，最终tmux/session/架构race与vet通过；冻结`5254892`的原生macOS arm64隔离tmux 3.8两整包race亦通过（71顶层/104含子项，零fail/skip），该提交的[CI42034435896](https://github.com/DDSRem-Dev/Shikigami/actions/runs/42034435896)已completed/success，57/57全通过，macOS arm64原失败及新增回归已核对日志；单轮成功按其来源记录，稳定性继续用完整矩阵复验。原42项验收登记、平台矩阵、测试预算和断言未削弱；本轮未发布新包。

**追加多轮稳定性验证（2026-11-05起）：** 用户要求再次触发完整CI。冻结生产/测试源码`886de184ad11f35a7b6879c2ca1b9d2dbab27a53`，计划追加3轮完整矩阵，逐attempt要求57个job全success，并核对四平台各7个必需E2E/重型集成用例实际pass。首轮CI42040152398 attempt2已完成56/57，唯一WSL2产品测试的端口较早截止用例失败，不把本轮计入成功；发现并修复共享准入错误分类两次读取ctx.Err导致DeadlineExceeded误判Canceled的竞态，修后FILE/PORT较早截止真实wire各50轮race、server/架构整包race及vet通过，修复提交`e95ff1c`的CI42063751769 attempt1与attempt2连续两轮均57/57通过（含WSL原失败项及新回归），每轮四平台28条加WSL7条必需记录与下游产物ID均核验；随后文档提交`d71a235`触发的第3轮CI42079305988 attempt1最终56/57，唯一macOS Intel CLI测试超时及失败清理竞态，本轮不计成功；已修正PTY测试输出消费与cancel/join顺序并通过最终200次定向race、CLI/架构整包race及vet，生产代码及原8秒期限未改；`c96abf5`的CI42085722699已57/57通过，四平台原CLI回归共16条、必需集成/E2E35条及产物来源均核验；随后统一补齐相邻CLI智能首次运行/目录/同步及UI取消/交接测试的同类夹具缺口，本地定向与整包race/vet通过，保持各自5/6/8秒期限。最终基线`bfdefc4`的CI42092895871 attempt1为56项成功、1项macOS arm64维护因GitHub runner容量不足而未启动取消，明确不计完整成功；同源码attempt2与attempt3已连续两轮57/57完整通过，每轮CLI16条、必需集成/E2E35条、WSL各阶段及四下游产物来源均核验；文档提交`6a86188`触发的第3轮CI42126955938最终56/57，唯一新的macOS Intel CLI登录等待父截止边界失败（原PTY回归仍通过），本轮不计成功；已统一状态RPC与定时等待的截止处理并保留最后已接受状态，原350ms/15秒期限及实际RPC错误边界不变，18分支各20轮race共360次通过；独立复核还修正终端附加日志期间取消覆盖既有鉴权拒绝，真实IPC/PTY六场景各10轮race共60次通过。最终CLI/客户端/架构整包race及vet通过，新源码完整连续轮次另记。补修提交0642f71的CI42132144143随后被新漏洞公告GO-2026-6865阻断（compress1.18.7，修复版1.20.2），已最小升级至1.20.2，根module/analyzer同标准扫描及上游溢出/解码限额race通过；该轮8成功、5失败、12组跳过不计完整通过，WSL仅preflight完成且具体失败原因未公开，升级后全仓build/vet、节点/客户端/CLI/架构race及模块校验通过，远端矩阵重新核验。依赖修复87fa715的CI42133128357又遇模块代理HTTP503、WSL准备阶段失败及macOS Intel预检15分钟总预算耗尽；已新增三次有界模块下载并将预检任务总预算调整为30分钟，原测试/扫描/warm阶段期限与断言保持；四条脚本路径、actionlint、83工具及30平台测试通过，本轮失败仍保留，最终CI基线重新累计完整轮次。最终CI基线fd78178的CI42135628202 attempt1与attempt2已连续两轮57/57完整通过，每轮四平台144条关键CLI、35条必需集成/E2E、WSL13阶段/cleanup及四下游产物来源均核验；文档提交b7c51bd触发第3轮CI42174388098，macOS Intel真实节点控制面恢复测试出现机器身份一致性断言失败（10.96秒、未超过原45秒），CLI144条回归仍通过；本轮最终56/57、不计成功；真实SDK屏障确认旧Self在URL清空后提前完成并授权的竞态，已收紧为已知旧Self须新注册映射到位，保留原机器身份/取消/期限及真实待登录URL复用。九项各10轮race、取消双场景各10轮、最终node整包178条及客户端/CLI/架构race与vet通过，原CI合并断言具体分支仍未确证；前两轮保留为历史证据，新基线连续完整轮次另记。节点修复09adb4a的CI42181018164 attempt1与attempt2已连续两轮57/57完整通过，每轮CLI144条、node40条、必需集成/E2E35条、WSL13阶段/cleanup及四下游产物来源五组均核验；本次仅同步文档，触发相同生产/测试/依赖/CI配置的第3轮完整复验，结果待实际完成后另报。三项原不稳定真实节点用例各50轮race已通过（150 pass、0 fail/skip，883.157秒），590个源码输入哈希保持，内部期限未改；完整CI结果单独记录。本轮不扩大原42项验收或发行声明。

**扩大范围复查（2026-10-31）：** 继续检查Mac全HOME及可读系统/临时目录、VPS完整可持久文件系统，额外删除Mac377项测试残留（98,262,156文件字节，含358个失效tmux测试socket）和VPS旧测试服务drop-in目录/配置（2项154字节）。VPS复扫项目名/已知测试标识无命中、无遍历错误，进程/账户/服务/容器挂载及cron无项目残留；本轮SSH连接和socket已关闭移除。Mac部分TCC、废纸篓及root/系统私有区域无权限，不能宣称全盘零残留；源码、发布签名备份和其他项目保持。39/42不变，详见[全机复查](24-public-package-compatibility.md#whole-host-cleanup-audit-20261031)。

**历史CI状态（2026-10-31，后续成功记录见下）：** 最终源码8fdfe25的[CI41197195478](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41197195478)尚未全绿：Linux arm64在原生安装/服务生命周期/升级维护阶段运行60秒后失败，内部具体步骤因安全artifact读取TLS超时尚未核实；不能套用上一轮13bcd0a的useradd超时结论。其他仍在运行的任务不计通过，最终支持门槛继续保留。本机相关race与实际Mac/VPS安装矩阵各自范围的通过不等同整轮远端CI成功。


**本批 CI 修复与耗时收敛（2026-11-01，已核验）：** GitHub Actions run [41269137757](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41269137757) 对提交 `b948664` 已完成并以 `success` 结束，57/57 个任务通过，覆盖四平台 preflight、单元/集成/E2E、四目标 Homebrew、WSL2、terminal 与 maintenance。原生 preflight 的失败根因是 workflow 调用了不存在的 `scripts/test-platform-tools.py`，现改用仓库实际入口 `scripts/test-platform-ci.py`；集成与 E2E 在 preflight 后直接并行，移除不消费的中间 artifact 和 compile 阶段无写入来源的冗余缓存恢复，运行时依赖仅在缺失时安装并启用 apt 重试，同分支并发取消避免旧提交占用 runner。terminal job 也加入 tmux/unzip/ip/openssl/dbus-user-session 的缺失检查、macOS Homebrew 安装及 Linux apt 有界重试，实际 Linux/macOS terminal 均通过。WSL2 保留完整产品/系统维护验收，本轮约 20 分 50 秒；整轮从 03:25:47 到 04:03:25 UTC 约 37 分 38 秒，受 WSL2 与托管 runner 排队影响，不能保证整轮低于 20 分钟，未削弱覆盖范围。另将 WSL 上偶发的 120ms Connect 超时夹具放宽为 500ms（仍早于服务端 5s 预算），修复 `internal/server` 的跨平台时序脆弱性。扫描后删除未被 workflow、Makefile、文档或测试引用的 `scripts/compare-memory-windows.py`、`scripts/test-install-space.py` 和 `scripts/test-schema-upgrade.sh`。按架构基线复核目录依赖、RPC/授权、状态原子性、进程生命周期、文件/终端语义及 CLI 合约，未发现生产边界偏差；本地 `actionlint`、平台工具 30 项、脚本工具 83 项、race 专项及 `git diff --check` 通过。

**最近已核验CI（本批修复前）：** 文档提交3b2d360的[CI41131301002](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41131301002)已completed/success、13/13通过，实际32分43秒。代码提交045b463的[CI41125907702](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41125907702)也已13/13通过，五平台各730文件源码摘要、必需阶段及清理已独立核验；其整轮46分51秒→36分09秒的性能比较仍按冷暖缓存/并行/排队边界记录于[26](26-native-platform-acceptance.md#ci-045-final-20261031)。下述既有CI不代表本批新修改已通过远端CI；本批相关本地race已通过，未重跑12小时。

以下是按各批源码范围保留的历史记录，当前状态以本摘要和原条目的最新部分进展为准。历史批次中要求真实Claude账号的待办仅表示当时计划；当前范围以[11验收范围](11-automated-installation.md#claude-acceptance-scope-20261031)为准，不再要求用户提供Claude登录。

**上批收尾：36/42，剩余6项（2026-10-30）。** 四目标原生Homebrew已验；Mac/VPS最终10阶段矩阵157.31秒、两类8MiB真实强杀恢复、条件竞争、后台冲突/TUI和撤A保B通过，操作系统账户/目录/转发清理已独立核验。结合公开包版本/既有ACK专项与[CI41047781448](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41047781448)十任务全成功，新增关闭Phase0恢复/版本/多设备、Phase4多设备体验、Phase5审查/扫描/关键分支三条；连同本批Homebrew共减少四条门槛。原42条文字/顺序保留，详情及失败记录见[25](25-multihost-acceptance.md)。完整认证、主机生命周期/外部身份升级、其余原生平台、资源性能长稳及最终支持声明六项继续待验。以下较早批次计数是当时记录，不代表当前进度。

**环境清理收尾（2026-10-30）：** 在此前专项清理之外，补充删除811项已确权历史临时对象；本轮closeout、归属审计目录及一次性清理工具也已删除，原始日志/报告未归档进docs。124项来源不足的历史对象及1项未确权且有VM缓存引用的脚本保留，不声称所有旧临时文件均已清空。8个受保护进程身份、运行中的长测容器、expiry输入hash/预约和代理连接复核保持；正式签名备份保留。当前源码提交cc0bb7e的[CI41051011635](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41051011635)十任务全成功。仍为36/42；两项12h与10月31日15:56的独立到期测试尚待实际结果，详见[清理边界](25-multihost-acceptance.md#历史临时材料最终清理)。

**此前阶段记录：双主机业务矩阵通过、OS清理尚未完成（2026-10-30）：** 第12轮10阶段/157.31秒全部通过，两helper及controller清理通过；源码/部署hash和737条事件独立核验完成。最终OS账户、目录、转发回收正在按精确所有权执行；完成前暂保持33/42，不提前勾选。范围与失败记录见[25](25-multihost-acceptance.md)。

**有限故障矩阵继续与清理预检（2026-10-30，33/42）：** 前八阶段已有通过步骤，整轮仍待；第九轮单次只读恢复状态查询超时，已在原60秒总限内先看journal再重观察；第十轮碰撞人工慢桥仍超过默认5秒且A映射归档失败，helper退出通过，最终清理预检因此正确拒绝，未删除本轮runtime。v6仅碰撞专项设显式90秒诊断前端预算，真实IPC测试确认默认5秒、专用90秒、原前端仍5秒、显式1秒及更早200ms父截止；整包race2.778秒和两平台构建通过。普通CLI/产品预算不改，不将此专项外推为默认CLI慢链路性能。当前表和历史进度歧义已修正；七类独立静态复核无新增确定生产偏差。

**双主机矩阵后半部进度（2026-10-30）：** 两种SIGKILL、条件竞争、离线冲突、旧generation拒绝、KEEP_BOTH及真实Sync界面已有实际通过步骤；整轮仍未通过，33/42不变。第七轮纠正agent旧附件所有权拒绝的错误预期，第八轮碰撞夹具的8MiB人工慢下载超过既有5秒父截止；仅碰撞数据改1MiB并在注入同名文件后解除限速，两类硬崩溃仍8MiB，失败立即呈报、无写重放。完整记录见25。另将07中过时的发行状态明确为已有公开alpha.2预发布、无稳定版。

**双主机恢复与夹具收敛（2026-10-30，33/42保持）：** 第六轮普通上传SIGKILL后完整恢复及B终端/TCP连续性通过；冲突解决强杀后过早再次Resolve返回Canceled，失败保留。夹具现等新worker同mapping revision的active/conflicted及恢复journal条件，再判断已恢复；不重试Canceled写请求。独立review另发现上传Send EOF可能遮蔽条件拒绝，helper v5复用既有1秒有界终态读取，四个旧行为反证失败、整包race2.775秒及两平台构建通过。67项Python工具测试通过。清理历史dist候选说明导致的13处链接已改为明确历史清理说明；没有将旧文档归档回docs。整轮与最终CI仍待。

**取消错误修复与条件提交工具补齐（2026-10-30）：** CI41039024257捕获请求已取消时发送层偶发返回EOF；共享连接层现保留Canceled/DeadlineExceeded，正常显式关闭仍EOF。确定性12分支先红后绿，connection整包race2.482秒、启动IPC专项race1.532秒及vet通过。双主机前五轮失败保留；条件文件原语改用独立认证TLS/H2且核对同一daemon，普通同步仍走agent和插件，前端raw文件代理限制不变。新两平台产品构建固定为local.multihost.3，完整矩阵和最终CI仍待，不重启旧c4长测。33/42保持，详见[25](25-multihost-acceptance.md)。

**双主机验收发现并修复终端接管提示（2026-10-30，整轮仍待）：** Mac＋VPS独立普通用户/状态/TLS测试已启动。真实CLI暴露接管后仍显示observer，现等待有序CONTROL_CHANGED确认后报告controller，保留输入代次与提前输入丢弃；超时回调join及缓存释放同步补齐。客户端、CLI、UI和架构race/vet已通过；修复后真实观察/接管提示与正常detach均通过。重绘节流和Dashboard过早按键两处夹具问题单独修正，失败不改记成功；其余并发/冲突/崩溃矩阵仍待，保持33/42。详见[25](25-multihost-acceptance.md)。

**Homebrew 四目标原生渠道验收完成（2026-10-30）：** CI [41037444120](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41037444120) 的 macOS/Linux × arm64/amd64 四个真实渠道任务全部成功，公开alpha.2的 tap/install/test/reinstall/test/uninstall/untap、完整bundle及清理前后环境一致已独立核验。关闭Phase 5签名/分发原条目，当前33/42、9项待完成；完整产品平台、主机重启、Claude与长期资源条件仍独立保留。用户Mac未运行brew或更改Xcode/CLT；具体范围见[22](22-homebrew-distribution.md#四目标原生-ci验收)。

**四目标原生 Homebrew 验收已接入 CI（2026-10-30，待实际运行）：** 在现有 CI 增加 macOS/Linux × arm64/amd64 原生 runner，读取当前可信 Formula 固定其公开来源，执行真实 tap/install/test/reinstall/test/uninstall，并核验完整签名 bundle 及清理前后环境一致。仅一次性 GitHub 托管环境可运行，不在用户 Mac 安装或更新 Homebrew/Xcode。11项隔离工具测试通过，实际四目标结果尚待，分发条目不预先勾选，仍32/42。上一候选 c4c5a56 和文档提交 a8112d9 的 CI 均已成功；12h 与未使用key自然到期任务继续独立运行。详见[22](22-homebrew-distribution.md#四目标原生-ci验收)。

**当前候选双平台长稳已启动（2026-10-30）：** 上一批修复与公开包验收已提交master c4c5a56，CI41029690521四目标构建及Linux全套测试成功、macOS全套测试仍运行。为补齐旧697未覆盖的新Stop自然轮换/停机排空，固定c4c5a56启动一次macOS原生和一次Linux arm64容器默认12h凭据轮换，均已先通过预检和正常清理，再进入正式观察首checkpoint。四份快照同为690文件，558项Go/proto/module输入与提交一致；TTL、2分钟恢复预算、实际恢复后30分钟尾部与原资源阈值未改。北京时间10月31日初始最早结束分别为Linux05:32:35、Mac05:35:08，最终仍需实际恢复尾部/退出/清理核验；现在只记运行中。官方未使用key自然到期仍独立计划15:56执行。七类复核通过，原42条/32勾选保持；详见[当前长稳启动](15-implementation-status.md#evidence-current-default-rotation-start-20261030)。

**公开包升级、兼容与取消回收完成（2026-10-30）：** 原始公开alpha.1→alpha.2→alpha.1在macOS原生LaunchAgent、Linux容器真实systemd下的活动升级/回滚通过，两平台alpha.2新装/重复/卸载通过。实际混合CLI/agent/daemon、原插件匹配/拒绝和同schema双向重开矩阵双平台各92命令通过，来源/身份/状态/清理有证据，见[24](24-public-package-compatibility.md)。新增慢订阅回归发现并修复ReleaseFrontend的阻塞Send、最终envelope及取消回调竞争；统一1秒仅取消收尾预算，保持原Canceled与同连接健康流。最终native四包race/vet、Linux最终agent增量race/vet及架构检查通过，较早源码范围分开记录，见[15专项](15-implementation-status.md#evidence-frontend-cancellation-20261030)。上一提交67fb0f3的CI41024681916六任务成功；本批提交CI另行记录。自然到期任务仍待10月31日15:56，原42条/32勾选保持，物理设备、主机、真实Claude、其余认证/平台及当前候选长期门槛不外推。

**未使用key撤销与审批实测、自然到期已调度（2026-10-30）：** 精确99bee22的CI41005895032六job成功，未开启opt-in仍不算通过。独立未使用single-use/1天key撤销后，新隔离节点1.932秒明确INVALID_AUTH_KEY，正常退出0并删key。审批临时开启后，新节点NODE_APPROVAL_REQUIRED且有效Token仍不能访问；管理台仅提供Remove，移除待批节点后仍禁入，但旧reason不证明明确管理员拒绝分类。保留状态显式重新申请并批准后自动online，Token/原daemon ID/能力及原session重验通过；专用节点与进程已清理，审批恢复原关闭，主Mac/VPS正常。另补CLI对有效待审批attempt的有界等待，状态仍denied，明确key拒绝仍终止。独立未使用key自然到期任务已注册，最早北京时间10月31日15:56执行，结果仍待。原42条及32/42保持，完整认证矩阵/平台/主机/长期门槛继续保留，详见[23](23-tsnet-external-acceptance.md)。

**最终agent撤销恢复与CI/CLI补修（2026-10-30）：** 92fe17a的CI40998046799整体失败：Linux测试及四目标构建成功，macOS凭据检查在SDK活动时扫描缓存遭遇ENOENT，后续E2E未执行、终端专项跳过。已修正为SDK完全关闭后重开最终持久状态并严格扫描，未忽略文件错误；定向race三次通过。相同生产源码的candidate4再次由官方移除后，旧附件2→1、专用终端退出，主Mac与tmux保持；自身正确offline/NODE_CONTROL_UNAVAILABLE。最终agent经同一显式attempt的Chrome认证恢复online，Token/原daemon ID/能力重验及P2P连接通过，无诊断overlay。现场另发现CLI在active attempt的控制离线阶段提前退出，已补有界等待，CLI全包race40项/22.312秒通过，详见23。本轮仅补外部子项，不由控制失联断言明确撤销；保持32/42、10项部分完成与原42条要求，独立key撤销/到期、审批及其余门槛继续推进。

**官方撤销回收、认证恢复与控制面断网验证（2026-10-30）：** 63924ce的六job CI已成功；官方移除专用节点后附件2→1、专用终端退出，主Mac与tmux保持正常，观察上界13/27秒包含Chrome操作与采样。用户完成Touch ID后，诊断构建重新认证online并重连原daemon；随后candidate4 clean构建保留同身份恢复。已分离控制面诊断与既有peer授权，修复登录完成/取消竞争、daemon误关健康流及CLI隐式登录；本地node按310.580/82.302/11.242秒三个源码范围分层race通过，server/architecture/CLI检查通过。官方控制面单进程阻断11.807秒期间，既有只读终端存活、新RPC成功，恢复online同地址；显式connect在独立断网场景ready且未触发登录。完整自身撤销分类、独立key过期/撤销、审批及其余认证/平台/长稳门槛保留，不把断联猜为denied。保持32/42、10项部分完成及原42条文字/顺序；本批新CI结果另行核验，详见[23 外部tsnet验收](23-tsnet-external-acceptance.md)。

**上一批官方 tsnet 验收与 peer 撤销修复（2026-10-30，第二次移除前）：** Mac/VPS官方登录、进程身份保持、真实P2P/受控DERP业务流和恢复P2P已有证据；仅关闭Phase 2首条，原42条文字/顺序保持，当前32/42、10项部分完成。官方节点移除暴露旧流未及时回收，现已补入站/出站peer身份代际、状态校准与连接关闭；本地真实SDK和生命周期回归通过。完整node race226.285秒属于最后5秒观察预算修正前的冻结源；最终预算修正先红后绿，相关专项race106.481秒、vet通过，不追认为最终源码全包重跑。官方撤销修复复验尚未通过；前两次重新认证未完成，第三次fresh URL经Chrome提交后，诊断overlay的SDK Running、URL清除且NodeControl online，随后切换不含诊断代码的最终构建，重启后同地址online，Token/daemon ID/能力重验及P2P应用连接通过。Auth Key独立过期/撤销、审批及其余认证/平台/长稳门槛保留，详见[23 外部tsnet验收](23-tsnet-external-acceptance.md)。

**历史计划盘点（2026-10-30，官方tsnet验收前）：** 已按原42项核对完成31项、部分完成11项，逐项剩余条件及自主/用户协助分工见[08](08-roadmap.md#剩余工作分工2026-10-30)和[19](19-local-acceptance-closure.md#plan-inventory-20261030)。修正汇总中仍等待alpha.2分发、原697尾部和永久沿用首发无历史版本的过期描述；公开alpha.1→alpha.2可开展真实包组合验收。原12小时、资源报告、密钥演练与自动Homebrew已完成；本批仅整理计划，没有启动新负载/部署或扩大支持声明，原条目文字/顺序及31/42保持。

**资源收敛、签名演练与alpha.2自动分发完成（2026-10-30）：** 已完成原697源全部740组/5920条资源样本及12进程代际复算，保留全部峰值/末半小时；697到公开alpha.1执行输入482相同/39修改/21新增，明确新Stop/probe/诊断等覆盖差异，未确认新生产保留根，不由RSS宣称无泄漏或追加盲目长测。临时测试密钥的真实verifier12步轮换/撤信正负演练和3项回归通过，私钥清理，真实发行身份未操作；旧钥显式传入仍可验签，在线撤销未提供。用户指定当前项目GPL-3.0-only及Homebrew首发v0.0.1-alpha.2；完整GNU正文已置LICENSE，README/构建说明同步，旧alpha.1冻结MIT保持。Homebrew从已认证四目标归档自动生成版本/URL/hash/签名/许可证，发布前Ruby校验，公开后master单文件CAS更新，同版幂等/冲突拒绝、旧版防降级与稳定渠道保护；仅保留CI/Release工作流。整套55工具测试、最终生成器7项定向复验及actionlint通过；Linux arm64普通用户Homebrew实际安装/test、包revision升级/test、重装/test及卸载7步全部退出0，三次test共6次helper暂存；Homebrew4/7暂存附加文件和LICENSE搬移两处适配已修正，完整认证包位于libexec/bundle，早期失败保留。最终native公式方法/重复暂存/签名篡改拒绝通过；实际环境细节见22，macOS27因Xcode/CLT不足被前置拒绝，不能记为brew全流程通过。alpha.2受保护tag固定fa35debd9c74d0ad44704e3a7897e0e0c1fcc3f2，Release40912019720完整成功并于2026-10-29T17:30:47Z公开22资产；Ubuntu/macOS完整CI、四目标构建、签名验签、发布前公式检查与公开后自动Formula提交85003591044fca15f82c9c220b2051766f1e47aa全部通过，双平台各56工具测试通过。macOS前两次容量取消均未执行步骤，第三次通过；历史失败保留。22公开资产重新验签、四目标GPL全文、24二进制及双平台6次暂存通过，公开产物重新生成Formula与bot提交字节一致。真实远端tap固定8500359，在Linux arm64普通用户环境完成tap/安装/test/重装/test/卸载6步及4次helper暂存，精确容器清理通过；本机完整brew的Xcode/CLT门槛保持。七类复核无生产模块/RPC/状态/生命周期偏差；原42条文字/顺序及31/42保持，外部/平台/当前候选长期资源门槛保留。详见[资源审查](20-long-run-resource-review.md)、[密钥演练](21-release-key-rotation.md)与[Homebrew分发](22-homebrew-distribution.md)。

**Linux默认12小时终态核验完成（2026-10-29）：** 原697文件冻结源运行已于北京时间23:11:50正常结束，应用/collector均退出0；实际工作12h30m3.603s、4368轮、740检查点，四角色各自然替换一次且在原两分钟预算内恢复，恢复尾部1803.013s、30个恢复后检查点，原持有TCP结束且无重放。11个受管PID的fixture清理、collector源码后置hash/快照移除与精确容器缺席已核验；预先准备的只读终态verifier实际返回verified，旧Linux578源失败保留。此证据仅属于原697源，不追认为当前alpha整版本长稳或无内存泄漏；FD+4/测试goroutine+6门禁不等于生产goroutine或全时RSS限制。当前31/42、11项待完成不变。本批没有启动新测试或操作服务/容器；七类架构复核保持原契约，后续先分析完整已有资源记录与当前版本覆盖差异，再决定必要补验。详见[Linux最终核验](15-implementation-status.md#evidence-default-linux-final-20261029)。

**首个全自动预发布完成（2026-10-29）：** [v0.0.1-alpha.1](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-alpha.1)已由受保护tag触发[Release 40895007350](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40895007350)，固定源码`f0b5f83dce599c8e0d715bda5e7806a0ef6aaa72`，完整Ubuntu/macOS CI、四目标构建、隔离签名/验签和公开发布全部成功，无人工审批。公开22资产已下载至`dist/releases/v0.0.1-alpha.1/`，以可信仓库公钥独立验证签名、payload/hash/pin、来源回执和远端摘要；24二进制版本/提交/Go平台正确，4个shiki保留的BuiltAt核对通过，另20个被链接器消除，未冒充静态验证。实际macOS arm64与普通用户、断网Linux arm64各12条版本/帮助及共6次首次/重复stage-only通过，无服务激活，输入hash保持且临时资源已清理。92cafaf旧签名包的真实服务新装/升级回滚证据仍单独保留，不追认为新归档全生命周期验收。公开Release及Issues入口HTTP200；工程发行地址与发布材料两条原要求关闭，原42条文字/顺序保持，当前31/42、11项待完成。七类架构复核无新增生产偏差；仅CI和Release活动工作流保留。Homebrew、密钥轮换/撤销、真实账号/网络、原生平台/重启登录、物理多设备及长期资源门槛继续待验；原697长测本批未操作或重新判定。详细证据见[公开alpha验收](15-implementation-status.md#evidence-public-alpha-release-20261029)。

**真实签名包验收与tag自动发布准备（2026-10-29）：** 用户授权首个v0.0.1-alpha.1及后续版本均由tag自动构建/签名/公开Release。92cafaf源实际签名候选已完成本机macOS arm64和Linux arm64容器验收，未重建产品：原生LaunchAgent用目标包自身helper完成开发.5→alpha→.5升级回滚69.72秒，另完成私有新prefix/state的新装、双插件、重复安装和卸载；真实systemd252.39完成13生命周期及4活动升级回滚边界、8MiB文件/终端/TCP与身份/状态保留，另完成签名alpha的全新daemon/user-agent安装、四插件、重复安装及卸载。正常服务/PID/容器清理与认证payload后置hash通过；旧native69.17秒只用A-helper的较窄结果、Linuxfresh重复安装把正确unchanged误期望ready的夹具失败及正常清理均保留，修正后通过。生产Go/proto/module及安装/构建包装输入与92cafaf一致，测试夹具支持预构建包；新tag仍会重建，旧验收不冒充新归档字节或构建元数据。已实现release.yml的tag精确来源/master校验、同源完整CI、无密钥四目标构建、隔离签名/验签、22文件及远端digest核验后从草稿公开；保留CI并删除/禁用旧手动sign-release工作流，历史记录保留。38项工具测试、actionlint、真实verifier四目标临时密钥签名及归档/签名篡改拒绝通过。GitHub环境已读回确认仅v*tag、无人工审批、禁止管理员绕过；规则集26129256禁止版本tag更新/删除，无绕过主体，既有私钥不变。七类复核无生产边界/RPC/状态/生命周期/I/O/安装语义偏差；首发说明和真实Issues反馈入口已准备，原42条/29勾选保持，实际tag工作流及公开下载待验证。既有账号、物理/原生平台、长稳/资源、Homebrew及密钥轮换撤销门槛保留；原697长测未改动。

**预发布签名完成（2026-10-29）：** 最新master当时提交`92cafaf88d834e84678186aa4e083e35e0439d88`的[CI 40872062129](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40872062129)最终六项全部成功；首次macOS任务因托管runner容量不足、未执行步骤而取消，只重新调度该任务后成功。[签名运行40877214329](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40877214329)固定`v0.0.1-alpha.1`及上述源码/签名工作流SHA，来源gate、四目标构建上传全部成功；GitHub审批记录确认DDSRem批准release-signing，签名和独立验签及signed artifact上传成功。22文件已下载至`dist/v0.0.1-alpha.1/`，包含四目标归档/hash/pin/签名/回执、公钥与PROVENANCE。本机从可信仓库编译verifier，用仓库公钥逐包验签，再核对14成员与全部payload hash、pin、版本/OS/arch、22文件精确集合、来源CI/签名run/SHA及远端回执，四目标全部通过；不以包内公钥作为唯一信任来源。七类复核确认本批仅执行既有发布流程，生产目录/依赖、RPC/授权、持久状态、生命周期、I/O、CLI/安装契约无变更；审批与分阶段持钥边界保持。原42条/29勾选不变，尚未创建tag/Release或激活部署；真实签名包安装/运行、公开分发、轮换/撤销演练及既有外部/平台/长期资源门槛仍待，旧`.5`和697长测范围不追认。后续文档提交不改变此固定产物来源。

**凭据轮换验收预算修正与远端通过（2026-10-29）：** 仅文档提交`9fe8e22`的[CI 40867762758](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40867762758)中macOS与四目标打包成功，Ubuntu的`TestRealHostCredentialRotation/client`在旧worker回收、端口恢复成功后，显式解决冲突RPC报DeadlineExceeded；共享探测与端口扫描均通过。独立源码复核发现hostrotation把启动探测用的显式1秒Timeout保留到整场业务客户端，覆盖了文档13的短RPC默认5秒、启停15秒；恢复默认Options，启动await原2秒单次/20秒整体、100秒场景与30秒凭据生命周期均不变。增加既有客户端deadline表对ResolveConflict默认/显式预算的验证，保留父截止优先；无生产逻辑、重试或业务断言修改。旧配置本机单次未复现失败，不把远端日志未提供的精确耗时或磁盘争用当作已证实原因。修后定向race组通过：客户端deadline测试1.530秒，真实hostrotation双角色78.115秒，client/server分别36.01/39.42秒；冲突代际保留后显式解决、双向hash、TCP无重放/半关闭尾部、暂停/停止意图、旧PID回收和socket替换全部通过。七类复核确认目录/依赖、RPC/授权、持久状态、进程生命周期、I/O语义和CLI/安装契约未改，验收夹具恢复默认契约。默认12小时夹具的1秒客户端仅用于临时探测、业务客户端本已5秒，现有实验保持原样；原42条/29勾选、外部与长期资源/签名发布门槛保持。CI 40865903512的历史成功保留，但不抵消本轮新失败。远端复验已完成：提交`8f4e4fac83fac5ad73fcafe2ccc53a3f3ff6bb4c`的[CI 40870240722](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40870240722)六项job全部success；Ubuntu/macOS的hostrotation整包race分别217.851/225.350秒通过，agent/client、生成漂移、依赖扫描、构建/vet/race/E2E与独立终端语义均通过，四目标未签名归档/hash/pin校验通过。此记录只属于该精确提交及CI实际配置，条件跳过不计验收；后续文档提交需按自身CI结果判断。

**远端CI修复闭环（2026-10-29）：** 提交`68f93b0bfb6f09743bb17f3e43f57561276fc785`的[CI 40865903512](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40865903512)已完成且六项job全部success：Ubuntu/macOS的生成漂移、依赖扫描、构建/vet/race/E2E及独立终端屏幕语义检查通过，Linux/macOS × amd64/arm64四目标未签名打包及摘要/pin校验通过。两平台工具测试各26项通过；agent包race分别31.933/32.122秒、port server 3.289/3.410秒，架构测试通过，前两轮暴露的IPv4接口后缀与共享探测取消竞态已在真实远端整套CI中复验。七类独立源码/文档复核无新增确定性偏差；未放宽预算、断言或增加失败重试。08/15/16同步，原42条要求/顺序及29项勾选保持。此证据仅覆盖该精确提交和CI实际配置，条件跳过不算验收，既有外部账号/物理平台/原生部署/长期资源与签名发布门槛仍待；旧失败、旧`.5`归档和697长测范围均保留。后续仅更新本次记录的文档提交，其CI须按各自运行结果判断。

**共享探测取消竞态修复（2026-10-29）：** [CI 40862770945](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40862770945)的四目标打包及macOS测试通过，Ubuntu仅agent共享终端探测用例失败；上一批端口server/真实port集成已在该Ubuntu运行通过。独立源码核对确认：probe到期时AfterFunc可先取消transport子context，Connect unary Send直接返回本地Canceled并绕过Receive归一，旧分类误将其作为共享连接故障。仅当父调用有效、错误非远端、probe确已DeadlineExceeded时，把本地Canceled归入probe超时；父取消/远端取消及超时/身份、认证、协议、证书拒绝仍保持原优先级。真实HTTP2共享终端的受控取消顺序用例修前精确红（original_bad=true），修后8个原/新增子场景、错误优先级及两项路由认证/回退测试race通过（24.648秒），agent vet和架构测试通过；400ms/5s预算、重试上限和断言未放宽。七类复核确认目录/插件依赖、协议/能力/授权、持久状态/幂等和CLI/安装布局未变，调用/共享连接生命周期与错误语义按原契约修复。未启动新长测或重复Linux编译；推送后由真实Ubuntu/macOS CI验证。原42条/29勾选和平台/资源/发行门槛保留，旧失败不改记通过。

**首轮远端CI与IPv4扫描修复（2026-10-29）：** master提交`a8774d4`的[CI 40859588149](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40859588149)已结束：四目标打包和macOS测试成功，Ubuntu测试失败；不是CI整体通过。Ubuntu真实ss输出`127.0.0.53%lo`触发IPv4解析失败，连带端口所有权、真实监听发现和Watch准入三个测试失败。仅在port服务parseSS IPv4分支验证并去除单个非空接口限定，保留原IP/端口/未知owner；IPv6 zone、lsof、授权及限额不变，畸形记录仍整次失败。同测修前race精确复现，修后native server/真实port integration/architecture三整包race（4.257/15.101/1.380秒）与三包vet通过；未追加Linux容器，Ubuntu实际链路待修复后远端CI复验。七类复核确认插件业务仍在plugins/port、SDK/宿主边界与RPC/持久状态/生命周期/CLI安装契约保持；此为明确生产修复，旧.5及697长测不包含它，不追认旧归档或旧源长稳。与Actions签名配置一起提交master，签名gate继续拒绝失败的旧CI；原42条/29勾选保持，完整CI及平台/资源/发布门槛仍待。

**Actions自动签名配置（2026-10-29）：** 用户选择Actions签名并授权新建专用Ed25519密钥与本机私有备份。公钥`configs/release-signing.pub`及SPKI SHA-256指纹`e21f6dd671ea5042c43c492b759b933dda73985f89a823b43c651ff7c787c17d`已确定；GitHub `release-signing`环境已实际配置并读回复核：DDSRem必需审批、仅master、禁管理员绕过，私钥Secret与公钥Variable均已设置。备份目录0700/私钥0600，密钥对已本机验证。手动工作流严格选择同仓库成功CI push/master的精确commit，四目标无密钥构建，可信workflow提交预编译verifier，审批后才单步持钥签名、公钥匹配和逐包验签；仅上传Actions artifact，不创建tag/Release。实际CI运行40859588149在未完整成功时被新gate拒绝且未写来源输出；5项Python行为测试、actionlint、实际签名shell的四目标微型归档/临时测试密钥/真实Go verifier通过，产出22文件；公钥及私钥不匹配两负例均失败；另只读验证旧.5两真实归档通过，不追改旧源码范围。七类独立架构复核无新增确定性偏差；新增逻辑均属发布工具，产品边界/RPC/授权/状态/生命周期/I/O/安装契约未变。远端签名流程尚未执行，审批/发布和原平台/资源门槛保持；原42条及29勾选不变。操作、备份、信任及轮换/撤销限制见[发布准备](./18-release-readiness.md#github-actions-自动构建签名与发布)。

**预发布版本与提交审阅（2026-10-29）：** 用户选择`v0.0.1-alpha.1`。已整理首次实现提交范围：CLI/agent/daemon、sync/port独立插件与SDK、协议、状态/认证/安装、测试工具、CI及docs；Git候选共652文件，bin/dist/工具缓存/临时证据不纳入。按用户要求，CI中四处Action引用改为GitHub官方latest稳定Release标签：checkout v7.0.1、setup-go v7.0.0（2026-10-29查询）；已核对两者action.yml的Node24运行时及当前使用参数，工作流仍为push/pull_request和只读权限，不使用提交哈希引用。现有CI配置经actionlint检查通过，涵盖macOS/Linux检查与四目标未签名打包；CI仍使用独立`v0.0.0-ci`，不冒充预发布产物。完整首次提交检查另修正7份Python脚本的无效尾部空白（AST一致），以.gitattributes将两份终端raw样本标为binary，样本字节/hash保持；本批仅版本记录和提交准备，用户已授权将审阅后的改动直接commit到`master`并推送触发CI；不创建发布分支、tag或Release。提交与执行状态以master实际提交历史和GitHub CI运行记录为准，授权或本地检查不算远端验收通过。七类复核确认生产结构、RPC/授权、状态/幂等、生命周期、I/O语义、安装契约均未变，版本命名不增加能力或平台声明；原42条/29勾选及资源/外部门槛保持。

**首发兼容基线确认（2026-10-29）：** 用户确认项目从未通过任何渠道对外发布可用版本；上一轮GitHub只读检查亦显示Release和tag均为空。历史正式版本迁移在本次首发没有验收对象，记为“不适用：无历史发行”，不是测试通过；旧批次中的“等待历史正式产物”由本说明更新。开发A/B版本的schema迁移、未来schema拒绝、协议/插件版本边界、升级失败恢复、回滚及数据/认证保留要求继续适用；首次对外发布后应保存固定源码、产物和状态格式，作为后续真实升级基线。七类一致性复核确认本批仅澄清验收对象，目录依赖、RPC/授权、状态、生命周期、文件/终端/网络、CLI/安装和能力声明无变更；不重跑行为测试。原42条及29勾选保持，物理设备、账号、原生部署、资源与发布门槛继续待验。用户已确定首个预发布版本为`v0.0.1-alpha.1`；本批未提交、推送、创建tag或发布。

**状态：Phase 0–5 原清单42/42项已关闭；新版本发布另行记录。** 当前证据见 [15 实施记录](./15-implementation-status.md)。本文是排期与阶段范围的唯一来源。原 14 周是未经验证的粗估，新增本地 agent 后不能保留为交付承诺；以验收门槛推进，完成 Phase 0 的原型后再估算工期。05/12 的目标体验不能替代逐项验收；已有实现与缺口见 15/16，GUI 仍为后续范围。

<!-- active-parallel-work:start -->
**当前工作与验收边界（2026-11-05）：** 原42项已关闭。当前源码支持范围及证据统一见[18支持声明](18-release-readiness.md#current-support)。用户确认与独立实测分别记录；未实测平台/账号/重启、资源归因和后续功能边界保留。下一发行版的tag、构建签名、实际包验收与渠道更新尚未在本轮执行；不能将当前源码结论追认为alpha.2已包含所有修复。
<!-- active-parallel-work:end -->

**本地收尾与目录清理（2026-10-29）：** 当前候选541份Go/proto/全部module执行输入逐hash仍与`.5`一致；已知syncack整包及真实systemd组合遗漏已经补验，不重复运行。完成[当前候选验收矩阵与后续条件](./19-local-acceptance-closure.md)，明确原697源终态先核四角色轮换、30分钟尾部、资源和清理，再依据具体结果补当前候选证据；未发现新的确定资源保留根，不把短测或旧源通过当无泄漏。发布准备中旧容量与撤销Token待办已按后续结果更正。普通/内嵌构建共用严格元数据入口，显式`SHIKI_COMMIT`/`SHIKI_BUILT_AT`注入、缺省unknown；8项Python回归通过，涵盖两入口六程序参数一致、真实buildinfo探针链接/输出及非法输入在构建前拒绝，不宣称六产品重打包。独立架构测试通过；七类复核未发现本批新增生产偏差。按用户最终授权共删除96份源码目录Markdown（scripts中89份及其他子目录7份），未归档；源码目录仅保留根README/AGENTS，既有dist交付快照保持。docs原有验收摘要保留，旧报告链接改指已有摘要锚点；465条相对链接及锚点无断链。三个诊断runner移除已删报告复制依赖，保留源码/工具冻结与hash门禁；实际准备代码9个隔离场景及3份完整脚本语法通过，不运行服务/负载。原42条要求/顺序及29勾选不变；资源、原生平台、外部账号和正式发布门槛仍未完成，原697长测独立继续。18:17检查点2657轮/7小时35分31秒，未轮换，无OOM/重启；本批不改旧`.5`归档、签名状态或unknown元数据。


**长跑收尾准备（2026-10-29）：** 既有native最终核验器含macOS路径/PID假设，已准备独立Linux只读入口`/tmp/shiki-default-linux-final-review-20261029/verify.py`；固定697源manifest、16份原契约源码及精确CID，核四角色身份/自然到期/两分钟恢复、TCP不重放、30分钟尾部、11个受管PID的fixture清理和精确容器缺席。PID只要求更换，不能要求数值递增；并发事件按完整记录多重集对照，关键阶段单独核时序。语法和当前真实pending路径已验，23工具/契约文件hash复核；没有运行新负载、探测宿主同号Linux PID或读取私有失败runtime。697终态分支尚未执行，准备不算通过；17:46精确容器仍运行，完整检查点17:45:47为2474轮/7小时4分2秒，无OOM/重启。三文档同步、原42条/29勾选及资源/外部门槛保持；完整入口与范围见[原运行报告](15-implementation-status.md#evidence-default-rotation-linux-current)。

**后续资源所有权核对（2026-10-29）：** 限定检查630源所有权审计之后新增的startup查询、route attempts、status-only NodeControl与sync Stop：查询许可/子进程/连接均有界并有退出清理，最多两项路由尝试替换旧值，Stop使用单清理owner；既有取消/准入/代际/阻塞依赖及真实syncack/systemd证据已覆盖这些具体边界。当前541执行输入仍与.5一致；未发现新的已确认保留根或足以支持追加用例的具体缺口，不对历史增长归因、不追加短内存实验或新12小时。原697源17:23实查仍存活、无OOM/重启；最后完整检查点17:22:27为2338轮/6小时40分42秒，尚未自然到期。此轮为定向源码核对和已确认活跃进程的必要等待，资源/外部门槛及29/42保持；不重复上一批全套测试。见[覆盖与所有权边界](15-implementation-status.md#evidence-long-run-source-coverage)。

**最近完成批次：当前安装恢复与真实systemd组合补验（2026-10-29）。** 针对旧服务验收未包含三处安装修复及新sync Stop的实际覆盖缺口，复用完整既有场景首轮通过：744冻结源、541执行输入与.5一致，13生命周期边界、4活动升级/回滚及真实poweroff，外层165.634秒、race活动探针52.88秒/零skip。每次切换均观测256KiB部分传输、公开目标旧完整hash保持、旧host/worker回收、同pane/原Token保留、文件冲突显式解决与新TCP恢复。产品是普通build，仅活动探针race；只读挂载源复制到容器可写/work构建，未宣称/work后置hash。独立44项复核无未解差异，正常退出0及精确CID/私有镜像删除已核验；systemd日志保留tmp.mount卸载FAILED，随后正常poweroff，不宣称日志全零失败。同期七类架构审查无新增确定性生产偏差，修正15/16旧长跑时态及08两处已完成却仍写待办的记录。三文档当前并行表、部分进展和结论同步，原42条/29勾选保持。正常服务场景不替代受控失败回归、掉电、历史发行、长稳/资源或外部门槛；697原长跑继续。见[服务补验](15-implementation-status.md#evidence-install-systemd-recovery-validation)。

**最近完成批次：当前sync Stop与丢ACK恢复补验（2026-10-29）。** 补齐上批六包遗漏的既有syncack整包；743同源、541执行输入与.5完全一致，macOS/Linux各一次race+vet通过，各4顶层/7子用例、0skip，包42.802/28.722秒。四种已持久化未知提交后的真实宿主RESTART均保留日志/源/旧基线，旧client回收、server不变、先读恢复且仅一次上传；32-watch准入、映射归档及未来schema拒绝也通过。无生产/测试修改、预算放宽或重复尝试。清理证据限fixture明确join/Reaped/runtime空断言；原生私有tmux目录与精确Linux容器正常回收，非完整PID清单。独立29项核验、七类复核及08/15/16同步，原42条及29/42保持；不作为重复worker Stop、超过5秒排空、提交途中强杀、长稳或资源验收。见[专项结果](15-implementation-status.md#evidence-sync-stop-ack-integration-validation)。

**最近完成批次：长跑与候选源码覆盖核对（2026-10-29）。** 697源521份执行输入与当前.5的541份逐项比对：487相同、34修改、20新增、无删除；54差异为25生产Go、3协议/生成、23测试及3独立诊断输入。14份轮换观察/runner/collector字节保持，但新sync Stop改变实际资源排空，路由attempts含运行字段持久化剔除，不能把旧运行追认为当前组合长稳；安装器不在默认长跑执行范围。两路独立只读复核和七类文档审查完成，已有专项逐组对应，未新增测试或重启实验。08/15/16同步，原42条及29/42保持；当前候选默认时长/资源及外部门槛仍待，文件数量变化不单独构成再跑12小时的理由。见[覆盖边界](15-implementation-status.md#evidence-long-run-source-coverage)。

**最近完成批次：四目标CI打包入口（2026-10-29）。** 原compile矩阵改为既有make release，build.sh仍执行原go build ./...；新增精确归档SHA及包内SHA256SUMS pin校验，固定CI版本且禁签名。独立741冻结源的Linux arm64普通用户容器串行四目标首轮通过，29命令/4归档/56成员/24份二进制静态目标信息均核验；源/工具后置hash、正常退出与精确容器清理通过，独立复核包含ELF/Mach-O头。七类架构复核完成，541执行输入与.5不变；仅工作流/文档变化，不运行目标程序、不冒充远端CI或原生平台验收，本地umask077权限结果不外推。08/15/16同步，原42条及29/42保持；签名/发布/外部与资源门槛仍待，697长跑原样继续。见[CI打包验证](15-implementation-status.md#evidence-release-ci-validation)。

**最近完成批次：Linux长跑中期资源只读分析（2026-10-29）。** 原697源日志已保存293个完整检查点/2344条八进程样本；截至15:37:57为1731轮、4小时56分12秒，原容器/StartedAt保持，应用退出未产生。Agent前四完整小时RSS中位数57.45→59.82MiB，末段回落不作为无泄漏证明；全部角色/峰值及不完整小时保留。原FD+4与测试goroutine14+6门禁在各检查点前完成，不声称无瞬时越界或生产进程goroutine已测量。独立32项复算、源双树697hash核对及七类架构复核完成；插件目录/依赖符合文档，当前541执行输入与.5一致。本批无新负载或产品修改，原42条及29/42保持；自然轮换、尾部、清理、资源稳定及外部门槛仍待。见[资源观测](15-implementation-status.md#evidence-default-rotation-linux-resource-observation)。

**最近完成批次：恢复修复候选包 .5（2026-10-29）。** 同一739文件冻结源码的macOS/Linux arm64未签名归档已首轮构建、验收并交付至dist/local-20261029.5；541份Go/proto/全部module与上批738已验输入逐项一致，包含四份恢复修复。两平台各52实际命令、14成员/六程序/双插件及全部既有诊断/确认/配置保护检查通过；实际归档helper首次与重复stage-only各校验14成员和私有review，无current/receipt/config/state/安装links，标准服务文件前后保持。两个agent均正常退出/reap，原生PID/runtime及精确Linux容器清理独立核验；源/工具/交付哈希与七类复核完成。08/15/16同步，原42条及29/42保持；未签名、Commit/BuiltAt unknown，暂存不是激活，.4历史及697长跑范围独立。见[候选验收](15-implementation-status.md#evidence-local-candidate-20261029-5)与历史交付说明（临时候选包与说明已于2026-10-30清理；验收摘要保留于本文档集）。

**最近完成批次：同步停止重试与安装失败恢复（2026-10-29）。** sync client Stop改为单次清理owner与各调用者独立截止，资源未排空不再重试假成功，关闭错误及未知提交源/日志保留；安装Stage补metadata/父目录同步及已发布目标重试屏障，Rollback早期失败仅恢复可信已提交选择，随机服务临时文件允许中断重试且保留旧现场。停止7条与安装恢复13条父/子同测红绿通过；Linux实际syscall证明原14/0→17/17同步，9精确EIO及新进程恢复通过。738同源双平台六相关包race/vet及真实sync子进程通过，各152父/子pass，1/2 skip不计通过。原生应用0但采集器因缺PID清单退出1，保留失败并限定清理证据为fixture Close/Wait；Linux应用/采集器0、精确CID及副本已清理。七类复核与独立审查完成，08/15/16同步，恢复Phase2项至29/42、13项待验，原42要求保持。.4归档和697长跑不追认本批修改。见[合并验收](15-implementation-status.md#evidence-recovery-conformance-validation)。

**最近完成批次：分析器依赖、节点状态诊断与 .4 交付（2026-10-29）。** 独立analyzer的x/sys已最小升级0.44.0，同库两目标复扫零finding，725同源双平台各16终端捕获及清理通过。修复只读状态误要求node.login.v1及doctor被不可用清单阻断：实际运行端NODE_DISABLED/NODE_STATE_UNAVAILABLE、未知清单null和正确Dashboard提示均保留，登录能力及UID/实例/截止校验仍严格。有效同测红绿与729源双平台四包race/vet通过，各196父/子pass、无skip；同537份Go/proto/全部module输入的729源.4实际归档两平台首次通过，各50命令、两agent正常退出及精确清理，12文件已交付。七类复核及独立核验完成，08/15/16同步，恢复诊断与Dashboard两项，原42条文字/顺序不变、当前29/42、13项待验。未签名、Commit/BuiltAt为unknown；.3历史错误、725/729不同证据和697独立长跑范围分别保留。见[依赖修复](15-implementation-status.md#evidence-terminal-analyzer-remediation)、[节点诊断](15-implementation-status.md#evidence-node-status-validation)、[实际归档验收](15-implementation-status.md#evidence-local-candidate-20261029-4)及历史交付说明（临时候选包与说明已于2026-10-30清理；验收摘要保留于本文档集）。

**最近完成批次：端口握手分支与独立分析器依赖（2026-10-29）。** 已补8个Send-EOF/非配额ResourceExhausted握手及已建立流对照分支，以受控公开Connect transport驱动真实Worker/TCP，验证既有payload/同监听/槽回收/无重放和正常清理；724同源双平台完整client包race/vet均首次通过，各7顶层+8分支、无skip，原5秒预算保持。嵌套VT analyzer已新增独立CI扫描，固定v1.1.4两arm64静态目标均无受影响已导入包，但保留GO-2026-5024 Windows模块公告；原生精确CI命令亦退出0，不称零漏洞或远端CI已跑。源后置/精确Linux清理及七类复核完成，08/15/16同步、原42条及29/42保持。没有生产/协议/依赖升级或产物重建，697长跑独立继续。见[端口分支](15-implementation-status.md#evidence-port-handshake-branch-validation)与[独立依赖扫描](15-implementation-status.md#evidence-terminal-analyzer-security-validation)。

**最近完成批次：CI终端语义回归入口（2026-10-29）。** 已将原有test-terminal-state.sh接入macOS/Linux默认CI独立步骤，修复缺analyzer时终端语义专项一直跳过的入口遗漏；只增加调度，不改生产、测试断言、模块或原90秒/60秒预算。同一719基础源加唯一环境wrapper的720源，逐字CI run block在macOS/Linux各执行一次：实际专项2.18/0.82秒通过，各16捕获、无skip；原生六PID/runtime独立缺席，Linux应用/collector退出0、精确CID缺席且源码副本正常删除。原/派生源码后置hash与七类复核通过，08/15/16同步；远端GitHub Actions尚未实际执行，原42条及29/42保持，不重建.3或改变697长跑。见[CI终端验收](15-implementation-status.md#evidence-terminal-ci-validation)。

**最近完成批次：本地候选包 .3 交付（2026-10-29）。** 同一717文件冻结源码已通过macOS/Linux arm64既有make release和实际归档验证，交付至独占dist/local-20261029.3；530份Go/proto/module与已验719完全一致，包含诊断、操作确认和配置恢复修复。两侧14成员、六程序、双插件清单/hash/pin、startup/list/doctor、缺--yes零启动和坏配置原件/备份保留均通过；正常Shutdown/reap、源事后hash和精确Linux清理已核验，无失败重跑或强制清理。七类架构复核完成，08/15/16同步，原42条及29/42不变。未签名、Commit/BuiltAt为unknown，不追认697长跑、外部连接或正式发行；空服务器诊断不证明实际路由。见[候选报告](15-implementation-status.md#evidence-local-candidate-20261029-3)及历史交付说明（临时候选包与说明已于2026-10-30清理；验收摘要保留于本文档集）。

**最近完成批次：诊断与操作确认（2026-10-29）。** 已补06要求的实际路由尝试列表和损坏配置恢复说明，以及05要求的stop/restart非交互--yes确认；运行字段有界、按代际发布且不持久化，原回退授权/停止实现/保存语义保持。三项有效同测红绿及独立审查完成；718同源双平台受影响race/vet各179顶层通过，安装专用skip和无测试包单列。现有4处E2E调用仅追加--yes，719源仅两测试文件和环境脚本不同；双平台两条真实CLI生命周期/端口恢复E2E及tagged vet均通过，无skip。首轮旧Go环境前置失败保留，实际验收仅修PATH；源后置/正常Linux清理和七类复核通过，08/15/16同步。恢复三项，原42条文字/顺序不变、当前29/42；707归档与697长跑不追认这些后续变更。见[合并验收](15-implementation-status.md#evidence-diagnostic-confirmation-validation)。

**历史启动与中期观测：新Linux默认12小时（2026-10-29，最终结果见文首）。** 已验宿主/观察修复和最终collector组成独立697源，于10:40:43启动；collector33067、精确容器232b672a…65f3，四初始worker/UID1000/只读源码已核验。17:46只读检查截至17:45:47已2474轮、7小时4分2秒且原容器仍运行；默认凭据12小时，最早22:41自然到期，恢复尾部下界23:11、外层截止23:55（Asia/Shanghai）。整轮恢复、至少30分钟尾部、资源及最终清理仍待验；原Linux失败保留。后续启动策略、诊断/确认、节点状态、同步停止与安装恢复修复及.5候选不在该冻结源中。见[独立运行](15-implementation-status.md#evidence-default-rotation-linux-current)。

**最近完成批次：默认入口补齐采集器回归（2026-10-29）。** 发现已有13项Python采集器关键回归未进入make test及CI，已新增test-tools并由test依赖；普通用户/Python3.10+前置避免root跳过生命周期测试却返回成功。同一最终四文件在macOS/Linux各13项通过、无skip；Linux实际root调用退出2且未运行suite，make -n test确认原三组Go入口保持。只改Makefile及文档，采集器/测试断言/生产输入不变；七类架构复核完成，08/15/16同步。未声称远端CI已运行，不重建707候选或变更697长跑；原42条及29/42保持。见[默认工具检查](15-implementation-status.md#evidence-default-tools-validation)。

**最近完成批次：本地开发候选交付（2026-10-29）。** 同一707文件冻结源码已通过既有make release构建macOS/Linux arm64的v0.0.0-local.20261029.2归档，并交付至独占dist/local-20261029.2。两侧14个归档成员、六二进制版本、双插件清单/hash及校验pin均已核验；实际解包CLI/agent的隔离startup JSON、新文本说明、停止后不自启、正常Shutdown/reap与清理通过，Linux为UID1000且精确容器已删除。相对已验启动策略产品输入仅一句CLI说明变化，本批已实际覆盖。完整源清单随包提供，未签名，Commit/BuiltAt为unknown，不作为正式发行或发布者认证。七类架构复核及08/15/16同步完成；原42条及29/42不变。697源12小时运行独立保持，签名/分发、资源及外部平台门槛仍待。见[候选包报告](15-implementation-status.md#evidence-local-candidate-20261029)与历史交付说明（临时候选包与说明已于2026-10-30清理；验收摘要保留于本文档集）。

**最近完成批次：启动策略配置展示（2026-10-29）。** 补齐11要求的本地展示：新增只读shiki config startup / GetStartupPolicy及可协商agent.startup.v1，分别报告agent加载的CLI按需配置、受管登录/开机策略与unknown。核对配置绑定、持久启用及Linger/launchd覆盖；共享单查询准入、3秒总预算、64KiB输出限制和取消回收已验证，不修改服务或持久偏好。macOS/Linux相同产品/Go测试/协议输入各四完整包race、vet通过，无skip；真实systemd四态与同agent身份、正常stop/poweroff/精确清理通过。两次Linux夹具失败保留，最终两个诊断脚本差异及后置一行CLI说明/12文档更新单列。七类架构复核与08/15/16同步完成；12/18过期状态已纠正，原42条及29/42不变。该本地缺口关闭，真实重启/登录、默认12小时、资源与外部发布门槛仍待。见[实现验收](15-implementation-status.md#evidence-startup-policy-validation)与[真实systemd](15-implementation-status.md#evidence-startup-policy-systemd-validation)。

**最近完成批次：宿主清理所有权与验收采集（2026-10-29）。** 修复CoreAPI回调尚未结束即释放writer lease、并发Stop阻塞调用者截止，以及首次Kill早于Client启动的进程回收竞态；Supervisor等待实际清理完成才重试或关闭状态库，停止准入拒绝保留no-store。有效同测红绿与最终692基础源的原生host/plugin两包race/vet通过；增加一个诊断脚本的693同源原生11项链路/架构检查及Linux七组race、vet全部通过。Linux应用/collector退出0、精确容器及快照清理已核验；原生补验由夹具断言回收，不冒充完整PID清单。独立637源双平台预检和最终collector13项测试分别通过；637/693使用初版正常清理collector，不追认后续异常保护覆盖。七类架构复核与08/15/16同步完成，恢复两项，原42条要求不变、当前29/42。资源循环显式排除，不归因历史heap差值，原Linux12小时失败及外部/发布门槛保留。见[宿主修复](15-implementation-status.md#evidence-host-handler-ownership)、[受影响验证](15-implementation-status.md#evidence-host-cleanup-affected-validation)、[预检](15-implementation-status.md#evidence-rotation-merged-preflight-validation)与[采集器修复](15-implementation-status.md#evidence-acceptance-cleanup-validation)。

**最近完成批次：长跑进程观察修复（2026-10-29）。** 原Linux12小时运行在替换身份读取时失败，未完成四角色恢复/30分钟尾部，原失败保留。已补有限截止内PID/出生/UID/父进程/路径/hash与唯一候选核验；只有独立确认进程消失才重试，权限/未知/换代错误明确失败，退休采样错误不冒充退出。清理确认后将有界私有原始证据保留到外层目录；不称脱敏或原子快照。635诊断源同源双平台helper及7项命名观察/时序/证据race、vet均通过，无skip；Linux应用/collector退出0、精确容器与快照回收，七类复核完成。未改生产/module，未重跑12小时，不把夹具检查追认为实际轮换验收。中间Darwin错误/共享调用签名编译失败均保留；当前29/42。见[观察修复](15-implementation-status.md#evidence-rotation-observer-validation)与[原运行结果](15-implementation-status.md#evidence-default-rotation-validation)。

**最近完成批次：端口准入隔离与普通混合负载（2026-10-29）。** 修复单连接握手ResourceExhausted取消整个转发任务的问题，按14使用保留错误码而非被脱敏文本；已建立流及认证/权限/路由错误处理保持。633冻结源同测有效红绿、双平台相关五包race/vet与七类审查完成；macOS/Linux普通正式负载各32轮、232流、每向14.5GiB及128MiB文件、双40空载样本、双默认128准入和12个存活/单槽恢复检查点均通过。原始数据独立复算，进程/session/runtime及精确容器清理核验；Linux专项collector只读快照清理错误保留并有补充清理，正式两轮均退出0。恢复Phase3条目，当前29/42；完整资源稳定、原Linux12小时恢复/尾部与外部发布门槛保持。见[修复验收](15-implementation-status.md#evidence-port-limit-isolation-validation)与[容量测量](15-implementation-status.md#evidence-load-capacity-validation)。

**最近完成批次：成功上传流资源释放（2026-10-29）。** host与SDK纯client stream成功后保留两项调用方取消注册，已由相同测试的冻结修前/修后和真实generated UDS/HTTP2上传确认并修复；仅两生产文件变化，server/bidi消息及半关闭保持。共同632输入双平台两完整包race/vet、六包命名链路与架构race、真实终端16项捕获各平台均通过，无skip；原生6PID/runtime及Linux正常退出/精确CID清理独立复核。七类架构审查未发现其他确认生产偏差，08/15/16同步，原42条及29勾选不变。仅证明该取消注册保留根修复，不解释历史heap斜率、不关闭资源稳定/容量或外部门槛。见[所有权报告](15-implementation-status.md#evidence-resource-ownership-audit)。

**此前批次：系统Tailscale非干扰与原生跨schema恢复（2026-10-29）。** Linux容器真实tailscaled在21个安装边界保持进程/unit/配置/socket/状态一致；独立UID1001内嵌tsnet实际报告needs_login/NODE_AUTH_REQUIRED与退出3，私有state和系统资源EACCES验证通过。系统state仅为2字节空JSON，不证明已有账户身份保持。macOS原生LaunchAgent完成A529→B630→旧A拒写→仅sync受控恢复→A→B及双向同步，30次RPC为18成功/12远端拒绝；最终B代际及清理已补齐。源/工具/上游模块、正常退出/精确清理与七类复核通过，无生产修改；失败和不完整轮保留，08/15/16同步，原42条不变、当前29/42。原Linux12小时、资源及外部/发布门槛继续待验。见[系统Tailscale共存](15-implementation-status.md#evidence-tailscale-coexist-validation)与[原生schema](15-implementation-status.md#evidence-schema-macos-validation)。

**此前批次：服务二次启动与恢复后撤权（2026-10-29）。** 同630文件冻结源码的Linux容器持久卷二次真实PID1启动通过：daemon/user-agent自动启动、四worker代际、身份/凭据/映射/基线/冲突/暂停/转发保持、双向同步与仅配置快照恢复均已验证；前两次夹具失败保留，修正依据为文档10。跨schema A529/B630场景5阶段30次TLS/HTTP2 RPC通过（18次成功、12次远端Unauthenticated），恢复未复活撤销凭据。两场景正常退出、精确资源清理及七类架构独立复核通过，无生产修改；08/15/16同步，原42条不变、当前29/42。容器PID1重启不等于物理重启，受控sync恢复不等于产品自动恢复；原Linux12小时、资源及外部/发布门槛继续待验。见[二次启动](15-implementation-status.md#evidence-service-boot-validation)与[跨schema报告](15-implementation-status.md#evidence-schema-service-upgrade-validation)。

**此前批次：固定版本下载与Agent生命周期（2026-10-29）。** 新增显式HTTPS固定版本下载，独立公钥先验签再取归档；真实94MB既有发行归档经当前Fetch认证后由真实helper暂存通过。修复映射迟到事件/配置响应回填、宿主全局锁等待网络及映射/转发旧预检跨代提交，保留持久重放与共享连接生命周期。确定性红绿和独立复核完成；同630文件源码双平台各8个完整包、命名CLI与两条E2E及相关vet通过，无失败/跳过，Linux正常退出/精确清理已核验。540份执行输入一致，17个Go文件变化（10生产/7测试），协议/模块不变。七类架构复核及08/15/16同步完成，恢复映射项，原42条不变、当前29/42。原macOS12小时轮换/30分钟尾部/清理另已通过，仅覆盖原旧源码；Linux原长跑、资源及正式发布/外部门槛保持待验。

**此前批次：节点授权生命周期与端口准入（2026-10-29）。** 修复节点授权lease保留已结束/取消请求回调、端口首请求前缺少宿主并发门、worker长Watch挤占短查询及Stop重试累加等待者。共享metadata32/watch32和首帧5秒已由真实HTTP2/独立插件链验证；节点取消、client终态与bidi半关闭有确定性红绿。原生节点组合夹具拆成各自45秒的独立目标，原断言及历史失败保留；同620文件源码双平台各11个有测试包race及vet通过，2无测试包和1opt-in跳过单列，进程/容器清理核验通过。七类架构复核及08/15/16同步完成，恢复Phase0条目，原42条不变、当前29/42。未将本修复当作旧TLS内存增长归因；正式发布、资源与外部门槛仍待。

**上一批：同源完整签名构建与诊断对象回收（2026-10-29）。** 同一608文件冻结源码在macOS/Linux实际完成`make release`、一次性测试密钥签名、独立公钥验签、认证私有副本解包、六二进制版本核验及真实helper暂存；macOS client与Linux client/daemon通过，私钥删除、Linux正常退出和精确容器清理已核验。诊断绑定`Close`持有已关闭daemon取得有效红绿，仅释放诊断回调，原生普通/race/vet通过，无生产改动或新增内存负载。七类架构复核未发现新增确定性偏差，08/15/16同步；原42条要求不变，当前29/42。两轮原12小时运行保持原样，运行内存增长、平台/外部及正式发布门槛仍待。

**此前批次：文件RPC并发准入与首帧预算（2026-10-29）。** 已补齐单OS用户跨连接/Token共享的metadata32、transfer16、watch32限额，授权先行且类别独立；上传首帧先登记再接收、默认5秒且保留更早父截止，正常长Watch不受此5秒限制。SDK可取消单reader与Stop超时重试假成功均已修复。有效红绿、真实独立插件链、macOS/Linux各10包race、vet和精确容器清理通过；2无测试包和1项opt-in资源skip不计通过。原42条要求不变，恢复Phase0传输条目，当前29/42、13项待完成；全架构复核及08/15/16同步完成。已有内存profile离线归因取得具体所有权线索，但残余增长、两轮真实12小时和外部/正式发布门槛仍待。

**此前批次：离线签名与后台同步截止时间（2026-10-29）。** 两轮12小时原运行持续期间并行完成两条工作。后台CoreAPI短unary默认5秒、按文件大小预算及未知提交恢复已完成修前红/修后绿、macOS与Linux受影响race检查，恢复Phase1含deadline条目，当前29/42、13项待完成。离线Ed25519签名/验签、严格PEM及打包sidecar不覆盖修复通过双平台检查；真实开发归档经当前verifier认证、私有复制并stage-only安装通过，独立102项断言核验。Linux补验9包及两个命名工具测试通过、2包无测试不计通过、无测试skip，vet和精确容器清理通过。原42条要求不变，全面架构复核及08/15/16同步完成。正式签名身份/分发、长稳与资源门槛仍待；旧developer归档不冒充当前源码构建，两轮原长跑不覆盖本批生产修改。

**进度同步：2026-10-29。当前 42 项中 31 项已勾选、11 项待完成。** 勾选表示该条目已有实现与对应验收证据，不表示整阶段、真实部署或正式发布完成。复合条目只完成一部分时保留未勾选，并在条目下记录已完成工作与剩余门槛；测试跳过不算通过。每批任务必须同时更新本路线图与 [15 实施记录](./15-implementation-status.md)、[16 一致性检查](./16-architecture-conformance.md)，三份一致后才算收敛。

**默认12小时验收新进展：2026-10-29 02:37:12（Asia/Shanghai）。** 原macOS运行已通过：4216轮、实际工作12h30m1.03382625s，四worker各自然替换一次，恢复尾部1802.031秒；应用/collector退出0、owned_cleanup无失败，13个记录PID已不存在，原runtime/source已删除，防休眠进程正常退出。独立复核`/tmp/shiki-default-native-final-review-20261029`通过；server-port瞬态不可用未被采样，保留false、不声称同时退役。仅覆盖原495文件/411份Go-proto-module，Linux原运行仍在到期前阶段，新修复另行验证。见[轮换记录](15-implementation-status.md#evidence-default-rotation-validation)。

**原Linux默认12小时已失败（2026-10-29 09:13:50）。** 原578文件源码运行43,219.82秒后，在server-sync旧代跨12小时退役后的替换进程身份读取阶段报`readlink /proc/274459/exe: no such file or directory`；4,191轮最后检查点仍recovered=false，无四角色恢复或30分钟尾部。该PID角色未保存，不能断定是无关短命child或候选worker退出。原七PID清理无报错，精确容器已不存在；新代未登记，不声称逐PID新代清理。失败证据保留，观察夹具已完成独立双平台确定性修复验证，不自动重启12小时，不覆盖当前633产品修复。见[实际运行记录](15-implementation-status.md#evidence-default-rotation-validation)。

**只读中期观测：2026-10-28 21:51。** 从两轮现有日志保存完整前缀：macOS459个检查点、7小时44分/2610轮；Linux38个检查点、37分33秒/227轮，均recovered=false。旧版macOS Agent前七个完整小时RSS中位数从63.77升至75.95 MiB，当前80.06 MiB；Linux当前54.49 MiB，但运行不足一小时且源码不同，不能比较修复效果或认定稳定。全部角色/高点保留，RSS不等于保留堆或泄漏证明；不追加负载、不重启，29/42保持。见[只读观测](15-implementation-status.md#evidence-default-rotation-observation)。

**此前批次：残余资源增长的观测开销调查（2026-10-28）。** 当前29/42不变。本批仅8个诊断文件变化，生产输入不变；默认/延后导出的同二进制短预检均通过。唯一一组普通Linux负载/空载各317.709/317.672秒完成，136/128份原始归档、各68份采样JSON、6个关闭后profile及精确进程/容器清理独立核验通过。Agent末段load−control为−260,788 B/min、+9.98对象/min；heap负值受idle3高点回落主导，不表示稳定。关闭后仍有+24,240 B/+228对象差值；旧参照为+79,456 B/+302对象。保留全部样本，不由一组干预推定因果或无泄漏。按预声明停止追加短窗口实验，资源门槛仍未关闭。全面架构复核与08/15/16同步完成，见[观测开销报告](15-implementation-status.md#evidence-memory-observer-validation)。

**上一批：请求生命周期与跨schema服务验收（2026-10-28）。** 当前29/42、13项待完成。Agent共享pool请求回调保留已确定性复现并修复；专项与Linux修前/修后/同二进制空载三组完成，修后后段load−control仍+15,597 B/min、+92.35 objects/min，不认定无泄漏。macOS与Linux完整race/E2E各46包通过；两次完整运行的生产输入相同，Linux最终快照仅多一处syncack夹具预算修正，macOS对该修正另有四场景race补验；不称两侧最终测试源码逐字相同。6/10项skip不计通过。真实Linux systemd开发schema升级、旧版拒写、有限恢复及再次同步已通过独立复核，不代表产品自动恢复。历史失败均保留，见[生命周期与回归](15-implementation-status.md#evidence-pool-request-lifetime-validation)、[跨schema服务](15-implementation-status.md#evidence-schema-service-upgrade-validation)。本批全面复核与08/15/16同步完成；原12小时运行和外部门槛保持待验。

<a id="最近进展与剩余条件"></a>
## 历史进展与当时剩余条件

**2026-10-31两步推进：** 完整原生四目标与实际WSL2产品/维护矩阵已接入CI，17项工具检查及静态检查通过，首轮CI41090262712与42eb93e轮已有真实失败，完整平台尚未通过，见[26](26-native-platform-acceptance.md)。节点显式Self.Expired跨BackendState分类及AuthURL出现时旧授权退役已修复，原生node整包race40项无skip和vet通过；官方干净候选Mac首轮登录超时后第二轮两端已online，失败及后续验证见[23](23-tsnet-external-acceptance.md#独立认证环境重建与状态修复2026-10-31)。重启预检发现本地节点未入网被永久标成REMOTE_INCOMPATIBLE，已修为本地auth_required等待并重验远端身份，定向race/vet、agent整包及干净m7官方节点同身份自动恢复通过。新专用认证环境保留到相关认证/安装升级验收结束；不清除仍有依赖的节点。VPS进程更新后另一处连接停在REMOTE_TRUST_OR_PROTOCOL_REJECTED，当前保留失败并调查。原42条、36/42及六个复合门槛保持。

以下保存各批次进度与当时条件；当前42/42及支持边界以文首和18支持声明为准。

| 工作 | 已完成并有证据的范围 | 当前剩余条件 |
|---|---|---|
| 启动策略配置展示 | 新增只读config startup，区分按需/登录/开机/未知；双平台四包race/vet与真实systemd四态/清理通过 | 本地展示缺口关闭；实际重启/登录持久化仍独立待验 |
| 宿主回调与启动清理 | handler/进程实际join、Stop/Launch截止与Supervisor交接已修复；双平台相关race/vet及七类复核通过 | 此缺口关闭；资源稳定、默认12小时和外部门槛独立保留 |
| 轮换夹具预检与采集器清理 | 同637源双平台各15轮启动/工作/正常清理通过；collector最终13项测试与独立审查通过 | 不等于12小时轮换；host修复由692/693源独立验收 |
| 系统Tailscale非干扰与原生schema | 未登录真实系统tailscaled的21边界不干扰、内嵌tsnet启用未登录状态隔离、原生LaunchAgent跨schema恢复与实际撤权验证通过 | 与已认证系统tailscaled共存、原生重启登录及其余外部/平台门槛仍待；独立tsnet双节点实际结果见23 |
| 服务二次启动与恢复后撤权 | 同容器持久卷第二次PID1自动启动、状态/双向同步/TCP/配置快照恢复；跨schema恢复前后撤销Token真实RPC拒绝通过 | 原生物理重启/登录、外部账号及自动恢复未由本批验证；历史正式迁移首发无对象 |
| 下载与映射/连接/转发代际 | 固定版本HTTPS先验签后下载；缓存所有权、锁外预检与提交重验完成；同源双平台受影响验证及真实native下载→暂存通过 | 正式公钥/Release下载认证已完成，直接fetch仍拒绝GitHub重定向；资源/外部门槛保留 |
| 节点授权与端口准入 | lease请求回调解绑、宿主首帧/分类门、worker短查询与Watch隔离及Stop共享完成；同源双平台验证通过 | 不解释旧TLS内存增长；资源/外部门槛保持 |
| 文件RPC资源准入 | 宿主/worker共享分类限额、首帧预算、SDK取消与Stop重试；双平台受影响race/vet与架构复核通过 | 此缺口关闭；不替代总体负载容量、资源泄漏或长稳验收 |
| 后台同步截止时间 | 默认5秒unary、文件大小预算、未知提交恢复；双平台受影响race/vet及架构检查通过 | 此缺口关闭，Phase1条目恢复勾选；不替代长稳/外部验收 |
| 离线发布认证 | 同608文件源码双平台实际完整签名构建、验签/解包/真实helper暂存通过，见[构建验收](15-implementation-status.md#evidence-current-release-validation) | 专用身份、公开下载认证、隔离密钥演练和alpha.2自动Homebrew/Linux真实tap均完成；原生macOS工具与其他平台门槛保留，生产身份未轮换或撤销 |
| 插件目录与依赖边界 | 入口/业务位于 `plugins/sync/`、`plugins/port/`；宿主与SDK分别为 `internal/plugin/`、`pkg/plugin/`，架构检查通过 | 后续每批继续检查，不允许宿主内嵌插件业务回退 |
| sync跳过告警 | 双端真实子进程回归与macOS/Linux专项通过 | 已收尾；证据见Phase 2 |
| 共享探测与转发隔离 | 短调用/远端取消不撤共享pool，完整5秒探测连续两次失败才进入恢复；原生四包race与build/lint通过 | 修复后507文件快照双平台完整race/E2E均通过；原Stats事件因果仍未确认，历史失败保留 |
| 终端与自动重启回归 | 查询竞态/稳定窗口有507文件回归；549文件已完成双平台终端语义与接管尺寸验收，终端项已勾选 | 真实账号及完整物理终端体验仍待；历史失败保留 |
| 同步事务恢复 | 日志、提交可能性、迁移栅栏、固定FD及pending准入已验；新Stop已取得红绿/双平台回归，743源真实syncack各11父/子用例通过 | 未知提交正常重启保留源/日志且无重复上传；不代替长稳或强杀边界，见[Stop与ACK补验](15-implementation-status.md#evidence-sync-stop-ack-integration-validation) |
| SDK状态目录与schema | 固定目录FD、未来schema字段拒绝已修；原生受影响race及Linux故障矩阵通过 | 最终v3同源双平台通过；仍不等于硬件掉电或全部文件系统验收，见[状态目录](15-implementation-status.md#evidence-state-directory-validation) |
| 在途文件轮换 | 内部30秒TTL的两侧×双向传输，双平台八格及原场景均通过 | 默认12小时和提交后最终ACK丢失不在该矩阵覆盖内 |
| 请求生命周期与内存诊断 | 共享pool修复及完整回归/补验通过；新增延后profile导出的唯一Linux配对完成，保留全部高点与清理证据 | 已确认并修复诊断Close回调持有；历史差值尚不能定量归因，短窗口不作为无泄漏证明；不追加此实验，见[观测开销](15-implementation-status.md#evidence-memory-observer-validation) |
| 默认凭据轮换 | macOS原12小时通过；旧Linux失败保留，697源Linux12h30m/4368轮、四角色轮换/恢复尾部及清理已独立通过；c4双平台终态已通过 | 完整已有记录与当前alpha覆盖差异分析已完成；生产heap/object归因保留为技术边界，不从RSS推断无限容量或零泄漏 |
| 端口分配与混合负载 | 单连接限流隔离修复、双平台五包race/vet、普通32轮完整混合负载及默认128准入/单槽恢复/清理通过 | 仅限定源码/环境/负载证据；对应长稳/资源/性能门槛已按用户确认测试通过关闭，外部网络支持范围另行声明 |
| 服务安装与升级 | 原生LaunchAgent、真实systemd生命周期及安装529补验通过；同容器二次PID1启动与开发schema升级/拒写/有限恢复后撤销Token实际RPC拒绝通过 | 自动状态恢复并未提供；外部身份跨升级、其他原生平台及重启/登录持久化待验收；两公开版本同schema升级/回滚已通过24，不能称历史schema迁移 |

## 阶段概览

| 阶段 | 交付范围 | 已勾选/总项 | 依赖 |
|---|---|---|---|
| Phase 0 | 工程、协议、本地 IPC/agent 设计和高风险原型 | 11/11 | 已确认范围 |
| Phase 1 | CLI + agent + daemon、TLS 直连、Project/Session、tmux 终端 | 7/7 | Phase 0 |
| Phase 2 | 内嵌 tsnet、双端插件、基础双向文件同步 | 6/6 | 核心生命周期稳定 |
| Phase 3 | TCP 转发、监控诊断、后台运行体验 | 6/6 | 插件路由和身份明确 |
| Phase 4 | TUI、配置快照、模板、多设备打磨、安装升级 | 6/6 | 关键故障场景验证 |
| Phase 5 | 首个正式版本验证与发布 | 6/6 | 全部首版门槛 |
| 后续 | GUI、WSS、文件快照等独立迭代 | 不计入42项 | 首版稳定及再次评审 |

条目全部勾选也不单独构成整阶段或发布验收；外部环境与跨阶段交付门槛仍按各条记录。

<a id="剩余11项本地工作与外部门槛"></a>
<a id="剩余5项本地工作与外部门槛"></a>
<a id="剩余1项本地工作与外部门槛"></a>
<a id="剩余工作分工2026-10-30"></a>
<a id="剩余工作分工2026-11-01"></a>
## 验收闭环与后续发布（2026-11-05）

原42项全部完成登记。最后一项通过支持范围与证据/代码契约对照关闭，详见[18支持声明](18-release-readiness.md#current-support)、[15本批记录](15-implementation-status.md#support-closeout-20261105)和[16复核](16-architecture-conformance.md#support-closeout-20261105)。原条目文字与顺序保持；此前未闭环的认证和资源项沿用用户确认，不伪造新增试验。

当前源码与公开alpha.2分开声明。未实测的真实Claude、物理重启/重新登录、其他OS及生产heap归因继续披露；GUI/WSS等未实现功能仍在后续迭代。已清理的测试身份、进程和资料不当作可用环境或可重读证据，不再要求重复已关闭矩阵。

后续交付新包时，固定新的版本/tag来源，执行已有自动CI/签名/发布流程，再核验实际归档、安装/升级与Homebrew指向。未完成的发布步骤保持待执行，不由42/42替代。遇到新的失败或影响既有行为的代码变更，再补受影响验收；本轮不创建tag或操作外部服务。

## Phase 0：初始化和可行性验证

- [x] 建立 go.mod、源码目录、LICENSE、Makefile、CI，确认实际 module/release 地址。
  - 已完成工程、LICENSE（当前GPL-3.0-only，首发alpha.1为MIT）、固定构建工具和 CI 配置，module 为 `github.com/DDSRem-Dev/Shikigami`；远端完整CI已实际通过，首个tag自动发布成功；实际[发行地址](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-alpha.1)与公开下载认证已验证，见[公开alpha验收](15-implementation-status.md#evidence-public-alpha-release-20261029)。
- [x] 将 09 的三个 protobuf 块和 13 的 agent.proto 落成文件，锁定 protoc/生成器版本，生成后 go build。
- [x] 按 09/13 已定义的 IPC、CoreAPI/worker、路由和能力协商生成真实 stub，验证 UID、代理白名单、旧 instance 拒绝和取消；不能停留在 Go interface。
- [x] 验证 14 的 Connect-Go v2 生成、注册/客户端、HTTP/2 over UDS/tsnet/TLS 四种 RPC 形态、HTTP/1 拒绝、下游错误重建、超限/取消/半关闭/最终状态及协议准入；不以 unary 成功代替双向流验证。
  - 新增端口metadata32/watch32宿主首请求前限额、5秒预算及worker分类隔离，授权/跨连接/取消/长Watch及真实插件链双平台通过；节点授权lease请求取消/成功终态释放同时修复。原文件策略保留，详见[端口与节点验收](15-implementation-status.md#evidence-port-node-lifetime-validation)。
  - 本批曾因文件链路并发资源与上传首帧等待缺少完整限制撤回勾选。现已落实同用户跨连接/Token共享32/16/32三类限额、授权先行、首帧5秒/父截止、取消回收和Stop重试闭环；真实红绿、独立插件链及同522执行输入双平台各10包race/vet通过，恢复勾选。2无测试包和1opt-in资源skip不计通过，既有传输/节点证据保留，见[文件准入报告](15-implementation-status.md#evidence-file-admission-validation)。
  - 四种RPC、半关闭/终态及准入已验收。最新真实节点测试暴露peer移除后旧数据路径仍可握手，已补绑定已解析公钥、拨号前后身份/有效性核验及过时连接关闭，取消/截止保留安全分类；最终node完整包双平台race各3次通过（71.763/61.781秒），恢复此项。旧全套失败保留，不声称未来任意peer变化的原子lease或外部撤权通过，见[节点准入报告](15-implementation-status.md#evidence-node-admission-validation)。
- [x] 定义状态 schema、Token 撤销表、request_id 幂等与配置迁移。
- [x] 终端原型：普通 tmux attach/PTY、多观察者屏幕初始化、resize、退出回收。
- [x] 服务保活原型：daemon crash/restart 不杀 tmux，插件不留孤儿。
  - 真实systemd252.39作为Linux arm64容器PID1，普通UID1000 daemon/user-agent的停启、SIGKILL自动恢复、升级/回滚/重装均已验收；同一tmux pane跨11次边界操作保活，旧插件回收，实际poweroff退出0。macOS原生LaunchAgent生命周期亦通过。见 [相关验收摘要](15-implementation-status.md#evidence-systemd-validation) 与 [相关验收摘要](15-implementation-status.md#evidence-launchd-validation)；不替代原生VPS、重启/登录持久化或外部账号验收。
- [x] 插件原型：真实 go-plugin gRPC 子进程 Init/Start/Stop + 独立 Connect CoreAPI 回调；验证 AutoMTLS、实例凭据、context 生命周期、跨协议错误/半关闭适配和失败回收。
  - 宿主handler实际join、独立Stop截止、失败Launch锁转交、晚启动进程最终回收及Supervisor等待已修复；有效红绿、最终692基础源/693诊断源双平台相关race/vet及架构复核通过。默认12小时/资源稳定单独待验，见[清理所有权](15-implementation-status.md#evidence-host-handler-ownership)与[受影响链路](15-implementation-status.md#evidence-host-cleanup-affected-validation)。
- [x] 完成 11 的 tsnet 登录/一次性 key/身份持久化/认证失败/NodeControl 原型，记录 SDK/OS 版本与脱敏结果。
  - 2026-11-01：用户确认完整 tsnet 认证生命周期测试已通过，本项关闭。既有官方登录、一次性 key 首用/复用/无效/撤销/到期、审批、身份保持、NodeControl、peer 撤销、重新认证、业务恢复和失败回退证据继续保留；历史失败记录不删除，真实 Claude 与物理主机重启仍按独立范围记录。
  - 2026-10-31新增：官方节点key单次短期到期、旧终端/TCP回收、CLI9/agent8/VPS7干净产品重新认证后同IP/daemon/凭据自动恢复及新终端/双向文件/TCP已验；SDK组合到期分类与有效attempt等待修复有本地race证据，干净重启后SDK缺旧期限的显示边界独立保留。独立未使用Auth Key于15:56只调用一次登录并明确denied/INVALID_AUTH_KEY，key/进程/job及私有状态已清理，见[自然到期终态](23-tsnet-external-acceptance.md#unused-auth-key-expiry-final-20261031)；完整认证矩阵不提前关闭。
  - 固定SDK、本地testcontrol/DERP和NodeControl已有证据；官方双端登录、一次性key首用/复用及无效key拒绝、进程身份保持与交互取消已验。官方节点移除暴露旧流回收缺口；入站/出站peer代际、每秒本地校准和共享5秒观察预算已修复，本地SDK/生命周期回归通过，精确源码与测试范围见[23](23-tsnet-external-acceptance.md#peer撤销修复与复验边界)。前次重新认证及clean最终构建重启/应用重连已通过；再次官方移除后的附件回收与主节点保持正常已验证，本次认证与clean恢复、控制面断网保留通道和独立connect已验证；独立未使用key撤销及审批等待/移除/批准已通过；独立自然到期已完成；官方URL有效期、审批拒绝结果和原因未知回退的完整契约对照仍待。
- [x] 验证 06 的 agent 崩溃恢复、四层版本边界及多设备条件提交/控制权竞争。
  - 2026-10-30：Mac/VPS独立普通用户与最终候选的完整10阶段矩阵157.31秒通过，操作系统资源回收独立核验；结合24公开包四层版本、既有06恢复/ACK专项及最终CI41047781448，关闭本项。两类8MiB真实SIGKILL、条件竞争、后台冲突/TUI与撤A保B范围详见[25](25-multihost-acceptance.md)；不外推完整tsnet认证、Claude、主机重启、其他平台或长稳。
  - 本地真实 agent 崩溃恢复、文件条件提交、多客户端控制权竞争已验证；四层真实独立进程的合成版本矩阵已通过（macOS/Linux 40.604/43.422秒），覆盖major/minor/required拒绝、同地址进程替换后能力收缩、AutoMTLS插件不兼容、未来schema拒绝及显式测试迁移备份，见 [版本验收报告](15-implementation-status.md#evidence-version-validation)。历史正式产物组合已由24补齐，Mac/VPS双设备由25补齐；合成对端不冒充历史发行版，其他完整产品平台另列。
- [x] 根据原型结果重估工期，登记不能满足的假设。
  - 已形成17原型复盘：核对限定环境证据、失效/未满足假设、本地工作包退出条件和条件工作量；外部账号/硬件/发布与观测等待单列，不沿用原14周或承诺总交付日期。近期新缺陷已纳入复盘。

证据：`internal/connection`、`internal/agent`、`internal/node`、`internal/auth`、`internal/server/storage`、`test/integration/plugin` 和 `test/e2e`。tsnet四种RPC原有本地控制面证据；官方不同网络的业务流新增证据见23，不外推任意外部网络策略。

验收：协议可编译，关键进程生命周期实测，接口没有未指定的跨进程指针传递。

## Phase 1：CLI 核心闭环

<a id="evidence-tmux-inventory-validation"></a>

- [x] 本地 agent 单实例、受限 IPC、状态/凭据管理，CLI 自动发现/启动。
- [x] daemon 普通用户运行、完整 unary/stream 认证、Token 创建/撤销、显式权限表。
- [x] 先建立 Project 模型及持久化，再实现依赖它的 Session 创建，避免先实现无项目会话再返工。
- [x] Connect/protobuf over HTTP/2 TLS 直连测试通道，证书验证、取消、deadline、重连；公网不可明文。
  - 本批发现后台sync→CoreAPI缺少短unary默认5秒及按文件大小deadline，曾撤回勾选。现已补齐完整响应计时器、首帧/中途/最终ACK预算和更早父截止；真实socket及虚拟时钟红绿、提交后超时日志恢复、同输入macOS/Linux受影响race与架构检查均通过，恢复勾选。既有TLS、证书、重连和取消证据保留，见[截止时间验收](15-implementation-status.md#evidence-file-deadline-validation)。
  - 已修复单次调用取消误撤共享pool并影响无关流：真实TLS取消/截止先红后绿，旧终端继续、新调用重新认证且不进入冷却；macOS完整agent/集成与原core E2E、Linux专项与原core E2E通过，见[共享连接报告](15-implementation-status.md#evidence-shared-connection-validation)。不重试mutation，原偶发E2E错误的唯一根因未被证明。
  - 全局共享的有界后台重连、耗尽后停止拨号与显式更新恢复已通过专项测试及 Linux 合并回归；取消和认证错误不触发路线回退。
- [x] 终端 TerminalStream 首帧/控制权、PTY、屏幕重绘、只读观察者、慢消费者处理。
  - 本批补齐真实client.Terminal→UDS agent→TLS daemon→tmux语义验收：独立VT核验宽字单元/前景背景/光标/alternate主屏恢复、尺寸边角和双方重连；修复接管后未主动发送当前尺寸，37×121无额外SIGWINCH实际RPC验证通过。549文件快照双平台2.27/0.84秒、各16捕获独立重放和六PID清理通过，恢复勾选，见[语义报告](15-implementation-status.md#evidence-terminal-state-validation)。
  - 此前按03复核发现验收缺口：真实多观察者测试已有alternate-screen/颜色输出，但只断言marker和任意ANSI序列，尚未语义核验光标、颜色、alternate-screen与resize后重连状态。实现和历史回归保留，当时暂撤勾选；现由上述独立语义证据补齐，不把普通全套绿色当该要求证明。
  - 此前505文件冻结Linux全套暴露最后会话删除后库存确认的退出竞态；现将既有一次可取消只读重查覆盖完整库存查询，仍只在确认后返回不存在。双平台tmux整包及507文件完整race/E2E通过，当时恢复勾选；其后因独立语义缺证曾暂撤，现由549文件专项补齐；原失败保留，见tmux库存报告。
  - 历史PTY回归先复现输出堵塞无法及时消费远端错误，修为独立接收、有界输出队列、错误取消与正常EOF排空；另修复并发Send的EOF抢占最终认证错误。双平台各10次终端race、真实UDS末帧竞争红绿对照及该批完整race/E2E通过，当时恢复勾选；后续语义缺口现由549文件专项补齐，未隐藏首次失败。
- [x] 创建/删除幂等、停止/重启 Claude、断线保活、状态恢复和错误提示。
- [x] CLI connect/attach/@/sessions/agent、目录映射、基本菜单。
  - 2026-10-29曾因stop-claude/restart-claude缺非交互确认撤勾；现已增加--yes/交互确认与拒绝时零mutation，双平台受影响race/vet及真实CLI E2E通过，恢复勾选，见[合并验收](15-implementation-status.md#evidence-diagnostic-confirmation-validation)。
  - 本地IPC新增权限就绪后原子无覆盖发布，修复并发CLI读到尚未chmod0600 socket的启动竞态；双平台connection各3次与原crashguard各3次通过；随后507文件快照完整双平台回归已通过，见[UDS报告](15-implementation-status.md#evidence-uds-publication-validation)。
  - 命令、映射及核心 E2E 已通过；受限 Token connect、smart 活动会话的观察/新建/管理/退出菜单和独立 verbose 日志已补齐并通过真实 UDS/TLS/PTY 测试。首次同步向导、受限项目目录选择及退出恢复菜单已有真实 PTY 证据；目录/保留日志后端、代理授权和实时退出事件已通过 macOS/Linux race，完整 macOS E2E 180.243 秒通过。

证据：`test/e2e/core_test.go` 的真实 CLI→agent→TLS daemon→tmux，以及 `test/integration/admission` 的受限 Token/观察者/撤销测试。macOS/Linux 已有本地闭环结果；Claude 进程承载路径使用可控 shell fixture；真实Claude登录/推理按[11验收范围](11-automated-installation.md#claude-acceptance-scope-20261031)不纳入当前交付验收且未实测。完整终端视觉行为以具名专项证据为准，不能用fixture宣称真实Claude推理通过。

验收：两窗口通过一个 agent 使用同一项目；断网重连恢复当前屏幕，无输入重放；删除会话不改文件；观察者控制操作被拒绝；Token 撤销关闭流。

该阶段的验收使用开发产物和受控 TLS 测试环境；一键 tsnet 安装、自动同步和端口转发分别在后续阶段跟踪，不把未发布 Homebrew/download URL 作为验收命令。

## Phase 2：tsnet 与文件同步

- [x] agent/daemon 内嵌 tsnet 的首次登录、状态持久化、节点名和 P2P/DERP 状态展示。
  - 官方Mac/VPS首次登录、节点名、双端进程重启身份记录/地址保持，以及实际tailscale-direct/relay展示已通过；两路径均有终端/文件/TCP内容证据，强制DERP后恢复P2P。登录等待/拒绝修复有回归和官方复验，见23；不外推主机重启、自然网络切换或完整认证生命周期。
- [x] 直连 TLS 显式回退，认证失败不换路线绕过；同一用户多个 CLI 不重复创建节点。
  - 已修复同时配置时优先 TLS 的偏差；tsnet 优先、网络回退、认证/证书/身份/协议失败停止、父截止预算及失败候选关闭已有真实 TLS 专项证据，共享 agent/节点已验证。
- [x] 插件 manifest/version/side、CoreAPI 能力 scope、加载/失败清理/重启、两端管理命令。
  - 2026-10-29曾因sync client Stop超时重试跳过资源清理撤勾；现单清理owner、各调用者截止与关闭错误保留已修复，5种等待障碍同测红绿及738同源双平台相关包/真实插件链通过，恢复勾选；见[合并验收](15-implementation-status.md#evidence-recovery-conformance-validation)。
  - 宿主handler实际join、独立Stop截止、失败Launch锁转交、晚启动进程最终回收及Supervisor等待已修复；有效红绿、最终692基础源/693诊断源双平台相关race/vet及架构复核通过。默认12小时/资源稳定单独待验，见[清理所有权](15-implementation-status.md#evidence-host-handler-ownership)与[受影响链路](15-implementation-status.md#evidence-host-cleanup-affected-validation)。
  - 此前502文件冻结macOS全套发生无关WatchStats提前Canceled，原事件因果仍未知；旧源码单轮诊断未复现，不能追改失败。独立共享探测/转发隔离缺陷已修并有先红后绿证据；最终507文件快照双平台完整race/E2E均通过，hostrotation包81.689/75.782秒，原存活断言未放宽，按当前版本验收恢复勾选，见[合并验收报告](15-implementation-status.md#evidence-conformance-regression-validation)。
  - 插件入口与业务位于 `plugins/sync`、`plugins/port`；宿主 `internal/plugin`、公共SDK `pkg/plugin`，生产依赖双向隔离由 `test/architecture` 检查。已补生产 Agent Resources/daemon Runtime、持久期望和真实 Core/Ready 的两侧自然到期恢复，双平台客户端/服务端各场景均通过（包74.820/71.325秒）；保留暂停/冲突/停止凭据，旧PID/socket回收，恢复文件与新TCP且不重放。两宿主在测试进程、四worker为独立子进程，使用内部30秒TTL；命令默认12小时不变，独立宿主二进制的默认时长验收仍待完成，见[真实宿主轮换](15-implementation-status.md#evidence-host-rotation-validation)。到期退役期间假ready已修复，插件整包双平台race各3轮通过（11.732/11.013秒），见[状态修复](15-implementation-status.md#evidence-plugin-readiness-validation)。
  - 本批补齐真实部分传输阶段client/server×upload/download自然轮换，macOS/Linux八格及原场景均通过；整包220.396/211.039秒，无跳过。内部30秒TTL和20ms离散采样，不覆盖提交后最终ACK丢失或默认12小时，见[在途轮换](15-implementation-status.md#evidence-host-rotation-inflight-validation)。
- [x] 项目映射级 sync，共同基线/hash/revision、首次同步计划、冲突积压、删除冲突。
  - 本批删除/修订后迟到事件与Configure响应重填缓存已确定性复现并修复；网络预检移出宿主/存储全局锁，提交复核完整服务器/凭据及mapping修订，幂等重放保留。630文件同源双平台各8包、CLI、两条E2E与vet通过，无跳过，恢复勾选；见[合并验收](15-implementation-status.md#evidence-fetch-mapping-validation)。
  - 本批已实现06要求的prepared/committed日志、末块发出前的提交可能性持久化、未知结果重观察/冲突保留及服务独立schema；固定staging目录FD与致命持久化失败后的UNHEALTHY已接入。开发冻结通过四种提交后ACK丢失场景、真实worker未来schema拒绝和原短TTL/在途轮换回归；旧schema1历史未知提交已由持久迁移恢复栅栏保护；另补host删除核对真实paused/revision及pending路径并集、锁后fatal lifetime复查。v3最终568文件同源双平台全套race/E2E通过，syncack45.395/34.394秒、hostrotation220.483/215.485秒；root独立核验后恢复勾选。见[sync日志](15-implementation-status.md#evidence-sync-journal-validation)和[合并验收](15-implementation-status.md#evidence-journal-conformance-validation)，历史失败保留。
- [x] 分块上传下载、清单分页、受根限制文件访问、原子替换、冲突副本/恢复。
- [x] watcher 递归/溢出/重扫、ignore、后台运行、pause/resume、无需前台也保留冲突。
  - 本轮审计修复双端跳过告警被宿主拒绝的问题：worker只发送安全reason code，保留host字段白名单；客户端/服务端×符号链接/FIFO四案真实子进程先红后绿，双平台scoped race通过，见[跳过日志验收](15-implementation-status.md#evidence-sync-skip-log-validation)。

证据：`plugins/sync`、`test/integration/sync`、核心 CLI E2E；macOS/Linux 普通用户 race、真实双端插件、后台冲突与重启恢复、递归 watcher 专项已通过。目录/生产依赖由 `test/architecture` 检查。

验收：双方修改、修改与删除、断线补齐、权限错误、symlink 逃逸、大文件限制、多窗口共享 watcher 均验证。首版先完整文件分块传输；增量 diff/三方自动合并后续优化，不能与安全冲突检测混为一件事。

## Phase 3：端口转发与诊断

<a id="evidence-stable-window-validation"></a>

- [x] 服务端发现 TCP endpoint、进程归属、保护端口及允许规则。
- [x] 本地 agent 持有 loopback listener，冲突重新绑定与实际地址展示。
  - 修复daemon平滑停止后同一已验证路由无法恢复监听；真实worker回归及systemd活动升级通过。旧TCP终止，新连接由应用建立，不重放字节；显式停止不被自动恢复。
- [x] ForwardPort 握手、半关闭、双向取消、限额、统计和孤儿连接清理。
  - 2026-10-29真实准入诊断发现server拒绝新增连接后client误取消整个转发与既有流，已修握手错误隔离；同测修前红/修后绿、633同源双平台五包race/vet及普通32轮混合负载/默认128完整准入均通过，七类复核后恢复勾选。见[修复与验证](15-implementation-status.md#evidence-port-limit-isolation-validation)，历史失败保留。
- [x] 明确手动/自动转发策略；关闭所有 CLI 后用户已启用的监听继续。
- [x] 系统/会话资源 RPC、插件状态、网络诊断与 stats/doctor/logs。
  - 2026-10-29曾因只读节点状态误要求登录能力撤勾；现状态专用协商、doctor独立诊断/未知清单和Dashboard提示已修复。同测红绿、双平台四包race/vet及.4实际包内两类节点状态验收通过，恢复勾选；见[节点诊断](15-implementation-status.md#evidence-node-status-validation)与[归档验收](15-implementation-status.md#evidence-local-candidate-20261029-4)。
  - 2026-10-29曾因06§2实际路由尝试列表缺失撤勾；现已补有界运行记录、旧代/持久隔离及CLI/TUI/连接失败展示，原策略授权不变，同测红绿和双平台完整受影响检查通过，恢复勾选，见[合并验收](15-implementation-status.md#evidence-diagnostic-confirmation-validation)。
  - 磁盘观测已补双平台行为验收：无会话项目仍计入、缺失路径报告unknown、枚举错误不伪造空列表。
  - 资源 RPC、插件状态、stats/doctor/脱敏 logs 和实际路径已实现并测试；已修复额外 system.info 依赖，受限 Token 独立诊断与耗尽后的明确恢复提示已通过，未知 RTT 不伪造。
- [x] Claude 进程退出检测、可选有界自动重启；不靠“无输出”自动杀进程。
  - 最新505文件冻结Linux全套的TestRestartStableWindowQuietProcessAndExplicitStop未得到退出码，保持EXIT_REASON_UNKNOWN且未重启，验收未收敛；生产unknown暂停符合文档；稳定窗口夹具改为真实静默进程与显式初始重试计数，双平台session整包race通过20.492/11.305秒，真实退出预算两侧均known=3/unknown=0，见稳定窗口报告。最终507文件双平台完整回归通过，恢复勾选；该次Linux真实退出测试走unknown安全暂停分支，不算known重启证据，known证据来自上述独立双平台整包。
  - 已知非零退出码/信号的顺序事件、持久重启预算、主动Stop抑制与alive pane拒绝已通过 macOS/Linux；unknown→known持续观察和通知去重已修复。Linux tmux偶发缺失退出原因时诚实报告unknown且不自动重启，不猜测退出码、不凭静默杀进程；详细分支证据见16。最后冻结Linux全套暴露Snapshot.Get对复用目标保留省略字段；公共读取边界已修为完整值替换，原安全断言不变，双平台专项与最终整套race含E2E均通过。

监听由 agent 管理的 client port worker 实际持有，host 持久化期望与恢复，不在宿主内嵌插件业务。证据：`test/e2e/port_test.go`、`port_automatic_test.go`、`test/integration/port` 和 session/tmux 测试。最终自动策略 E2E 在 macOS 104.48 秒、Linux arm64 普通用户容器 110.74 秒通过，含无拨号预检、双端 allowlist、停止抑制持久化、扫描失败保留手动活动流。

验收：TCP EOF 后仍收到完整响应，断线不泄漏连接，默认无 LAN 暴露，未知进程端口不自动转发，扫描失败不清空所有映射。

## Phase 4：首版体验和运维

- [x] TUI Dashboard、非交互模式、友好错误、通知不污染终端字节流。
  - 2026-10-29曾因只读节点状态误要求登录能力撤勾；现状态专用协商、doctor独立诊断/未知清单和Dashboard提示已修复。同测红绿、双平台四包race/vet及.4实际包内两类节点状态验收通过，恢复勾选；见[节点诊断](15-implementation-status.md#evidence-node-status-validation)与[归档验收](15-implementation-status.md#evidence-local-candidate-20261029-4)。
  - 2026-10-29曾因损坏配置缺06§7恢复指引撤勾；现已补保留原件/验证后显式恢复/重启说明及future独立提示，不自动恢复旧配置，错误身份与首次缺文件语义保持，同测红绿及双平台受影响检查通过，恢复勾选。
  - 新增默认关闭的显式stopped会话自动清理、独立持久预警与doctor/TUI展示；仅同一dead pane且无attachment，观察失败/时钟回退取消旧计划，重启延后窗口，项目文件不删除。有效附着/输入更新last_active，输出不算用户活动。双平台专项及观察失败20轮通过，见[清理报告](15-implementation-status.md#evidence-cleanup-validation)。
  - 已有本地功能与PTY证据；最终冻结Linux全套发现取消时Bubble Tea/cancelreader输入关闭竞态；已修复并通过双平台各20次PTY回归、前端广包race及最终整套race含E2E。
- [x] 会话配置快照与模板；恢复新会话且不回滚文件或对话。
- [x] 多设备路径映射、显式终端控制权接管、后台同步冲突展示。
  - 2026-10-30：Mac/VPS独立普通用户与最终候选的完整10阶段矩阵157.31秒通过，操作系统资源回收独立核验；结合24公开包四层版本、既有06恢复/ACK专项及最终CI41047781448，关闭本项。两类8MiB真实SIGKILL、条件竞争、后台冲突/TUI与撤A保B范围详见[25](25-multihost-acceptance.md)；不外推完整tsnet认证、Claude、主机重启、其他平台或长稳。
  - 新增真实两个独立 agent/身份/映射连接同一 TLS daemon 的 macOS/Linux E2E：同revision接管仅一方成功、只读拒绝、停止A不影响B流/后台同步、离线编辑冲突保留、过期解决拒绝和最终收敛均通过；Mac/VPS双设备体验随后由25补齐，其他完整平台范围另列。
- [x] 按 11 实现安装器、Linux systemd 服务、macOS LaunchAgent/Linux 用户服务。
  - 2026-10-31终态：Mac同官方身份的实际首装/重复/升级/回滚、SIGKILL自动恢复、管理器重注册和卸载保持全部通过；结合VPS真实systemd及历史公开发行/平台/schema证据，关闭本项。原身份/凭据、映射/基线/暂停/冲突/转发和六组新业务保持，四次旧TCP EOF无重放、18旧进程身份回收；来源与边界见[24终态](24-public-package-compatibility.md#installer-final-20261031)。本地同源版本切换不冒充历史发行迁移，硬件重启/真实Claude不追认；以下旧待验描述是历史进展。
  - 2026-10-31本轮：原生VPS保留官方tsnet身份，真实systemd安装、stop→daemon-reload→start及卸载保留数据通过，管理器启动先于业务CLI；未重启宿主。Mac公开A与候选B的原生LaunchAgent均未在原30秒内节点就绪，栈定位SDK控制面公钥请求的TCP拨号等待，具体原因未定；本项仍未完成。恢复与清理范围见[24本轮结果](24-public-package-compatibility.md#external-installer-20261031)。
  - 11要求的启动策略展示已补齐：shiki config startup只读区分CLI按需与受管登录/开机策略，未知不猜测启用；双平台受影响race/vet及真实systemd启用/禁用运行中/错配置/恢复绑定四态通过。仅配置观察，不替代实际登录/重启，见[启动策略验收](15-implementation-status.md#evidence-startup-policy-validation)。
  - 2026-10-29实际锁定tailscale.com v1.98.10源码构建的系统tailscaled在21边界保持原进程/unit/config/socket/state，Shiki双角色安装/升级/回滚/卸载/重装通过；另一个UID1001内嵌tsnet启用未登录返回node_needs_login/退出3，独立252B state与访问系统socket/state实际EACCES通过。正常poweroff及精确容器清理独立复核。系统state是2字节空JSON，不能证明已有认证身份保持；不是官方二进制或已认证双节点共存。见[共存报告](15-implementation-status.md#evidence-tailscale-coexist-validation)。
  - 2026-10-29第三轮持久卷二次PID1自动启动验收通过：同容器正常poweroff后重新start，daemon与user-agent无需CLI拉起；身份/Token/映射/revision/基线/冲突/paused/forward保留，既有路径自动双向、新路径首次显式方向、fresh TCP及配置快照创建新会话通过。双host/四worker当前产物与UID/代际、两次正常退出与精确CID/home卷清理独立核验；前两次夹具失败保留。仅容器PID1重启，不代表物理VPS或原生登录；见[二次启动报告](15-implementation-status.md#evidence-service-boot-validation)。
  - 安装器、服务渲染、NodeControl/bootstrap、内嵌产物及隔离smoke已实现；macOS27 arm64原生LaunchAgent与Linux arm64容器真实systemd/systemd --user均通过安装、实际应用就绪、停启、崩溃自动恢复、升级/回滚和卸载保留数据。保留未勾选：11的完整安装要求还涉及外部登录/真实账号、可信发行来源、原生VPS和重启/登录持久化。
  - 本批按11补齐停止旧服务前的磁盘预检、停止后新清单复核和备份父目录fsync；双平台整包race、真实16MiB tmpfs及fsync跟踪通过。529文件快照原生上传升级/回滚90.41秒、真实systemd 13次边界补验通过，见[安装空间验收](15-implementation-status.md#evidence-install-space-validation)；不把历史下载/取消结果标成新源码验证。
- [x] 重复安装/升级/回滚、自定义 config、非交互登录、依赖检查和卸载保留数据。
  - 2026-10-31终态：Mac同官方身份的实际首装/重复/升级/回滚、SIGKILL自动恢复、管理器重注册和卸载保持全部通过；结合VPS真实systemd及历史公开发行/平台/schema证据，关闭本项。原身份/凭据、映射/基线/暂停/冲突/转发和六组新业务保持，四次旧TCP EOF无重放、18旧进程身份回收；来源与边界见[24终态](24-public-package-compatibility.md#installer-final-20261031)。本地同源版本切换不冒充历史发行迁移，硬件重启/真实Claude不追认；以下旧待验描述是历史进展。
  - 2026-10-31本轮：VPS公开alpha.2→冻结b30c585候选→回滚alpha.2保留同tsnet身份、daemon/凭据、pane、基线、暂停及冲突；新终端/双向文件/TCP和旧TCP结束不重放通过。Mac首装未就绪，尚未进入该身份下的升级/回滚，不能由VPS结果代替；首次迁移显式connect及原失败观察保留，详见[24](24-public-package-compatibility.md#external-installer-20261031)。
  - 2026-10-30公开alpha.1/alpha.2双平台真实服务升级/回滚、重复安装和各92命令兼容矩阵已通过；同批慢流取消三类缺口修复及分源双平台检查通过，见[24](24-public-package-compatibility.md)与[取消回收专项](15-implementation-status.md#evidence-frontend-cancellation-20261030)。外部身份、物理主机/平台和当前候选长稳仍独立待验。
  - 2026-10-29确认的Stage落盘遗漏、中断回滚早期失败启动未提交版本及遗留服务临时文件阻塞已修复；真实syscall前后对照、9个EIO新进程恢复、13条父/子同测红绿及同源双平台相关包通过。服务管理器/物理持久化/正式发行仍保留原门槛，见[合并验收](15-implementation-status.md#evidence-recovery-conformance-validation)。
  - 2026-10-29原生macOS27 arm64/真实UID501 HOME与GUI domain完成开发A529/B630的1→2→旧A拒写→停写仅sync恢复→A→B，并完成既有基线双向同步及30次RPC撤权检查。第四轮包含最终B host/worker身份/哈希与全记录代际清理，daemon退出0、无fallback、job/plist/staging/自有session/runtime无残留，独立复核通过；前三轮失败/缺证据范围保留。不代表daemon schema迁移、任意B新写后恢复旧备份、重启登录或历史正式发行。见[原生schema报告](15-implementation-status.md#evidence-schema-macos-validation)。
  - 新增真实Linux systemd用户服务的开发schema1→2→旧程序拒写2→显式恢复sync子树1→再次升级2及新文件同步验收，第三次尝试通过并独立复核；前两次夹具失败保留。恢复不触碰host/auth/项目文件，撤销记录保留，但未以撤销Token另发RPC；这是停写且B无新业务修改条件下的测试操作，不是产品自动恢复能力。见[跨schema服务报告](15-implementation-status.md#evidence-schema-service-upgrade-validation)。
  - 2026-10-29补验：原A529与当前B630的真实systemd矩阵、5阶段30次RPC通过；撤销前候选Token实际可用，撤销后及受控恢复前后均由同一daemon经验证TLS/HTTP2返回远端Unauthenticated，有效Token始终成功。正常poweroff退出0、精确CID清理与两份独立复核通过；历史第三次试验仍只证明当时撤销记录，不追补其RPC范围。
  - 版本化产物、事务日志/恢复、回滚、保留数据卸载及独立审计修复已通过 macOS/Linux race；真实 tar/helper/wrapper 的隔离 staging/重复/篡改拒绝通过。完整本地应用就绪、异步登录等待、显式初始 Token 交付和探测版本/实例一致性现已补齐，真实helper/daemon/sync及TLS认证E2E通过；含LICENSE的真实发行包暂存/重复/篡改拒绝通过。最终Linux合并曾暴露socket尚未创建时probe提前失败；已纳入原20秒等待期限，Linux连续3次真实bootstrap、双平台专项及最终整套race含E2E通过；本批真实LaunchAgent/systemd下升级、回滚、重复安装、卸载保留配置/身份/凭据/状态已通过；真实systemd活动终端/TCP/8MiB文件中升级与回滚已通过（187.62秒、13次生命周期边界，同一pane保活）；外部非交互登录、历史发行版迁移、原生VPS/其他平台等完整场景仍待验收，见[活动升级报告](15-implementation-status.md#evidence-upgrade-validation)。
  - 新增macOS原生LaunchAgent活动升级/回滚87.08秒通过：每次先观察8MiB同步上传的256KiB临时内容及真实终端/TCP在途，再执行A→B→A；同一pane、独立daemon上传和持久冲突/暂停/停止回执保持，旧流终止且新流无重放，正常退出无兜底清理。取消测试runner143/test1符合预期，8个PID与专属session/job/plist均回收；首次33.11秒夹具初始化失败未算通过。仅agent升级与上传方向，不外推为macOS daemon服务、历史发行迁移或全平台验收，见[原生活动升级报告](15-implementation-status.md#evidence-launchd-activity-validation)。
  - 下载方向已补齐：最新夹具将方向与替换宿主分开；原生agent下载A→B→A87.29秒、同版上传85.03秒通过，客户端staging/服务端temp均有真实部分内容，切换前公开目标旧完整SHA保持，切换后精确新内容收敛。独立daemon上传不被agent替换取消；未确认输入分别[1,0]/[0,1]，符合至多一次而非强称从未到达。两个正常run均无兜底清理；不追认旧Linux运行已覆盖新增原子性断言，不重复宣称新取消结果，详见同一[原生活动升级报告](15-implementation-status.md#evidence-launchd-activity-validation)。
- [x] 文档和命令帮助与 capability 保持一致。
  - 上一批已修复agent按当前协商能力准入方法、mapping返回agreement核验及安全版本诊断；双平台真实进程矩阵与完整回归通过。本批新增session.cleanup.v1，观察失败取消计划、墙钟回退、socket发布竞态、节点准入和共享pool取消隔离均已修复并取得相关双平台补验；独立八类架构复核、协议/依赖检查通过，08/15/16同步后恢复勾选。先前失败全套仍保留，不以补验改写历史结果。helper帮助在账号/凭据/目录访问前返回，非install拒绝stage-only及不适用产物/布局参数，实际wrapper验证拒绝且不创建目录；双平台helper race 1.495/1.025秒通过。
  - 协议文档词法对照、插件依赖及能力/权限测试通过；05/12/13命令示例与范围已复核修正，实际69个应用帮助入口全部成功，CLI/架构race通过。08/15/16同批同步，未来GUI和未发布渠道明确保留为目标。

证据：`internal/client/ui`、`cmd/shiki`、session/server 快照模板测试及核心 E2E。`config get/set/edit` 已通过真实 agent IPC 验证 revision 并发保护，前端不修改 bootstrap 或导出凭据。

## Phase 5：正式发布

<a id="evidence-acceptance-runner"></a>

- [x] macOS Intel/ARM、Linux、WSL 客户端与 Linux daemon 兼容性验证。
  - 2026-10-31关闭：c1e5ece的CI41119194452全13任务成功；同源原生Mac双架构、Linux双架构、真实WSL2完整race/E2E/终端及服务维护、活动升级回滚、来源一致性和清理均通过，主代理已独立核验。证据见[平台验收闭环](15-implementation-status.md#evidence-native-platform-c1e-20261031)与[26](26-native-platform-acceptance.md)。仅覆盖记录的runner/内核与本地产品/服务范围；外部账号、真实Claude、物理重启登录/自然切网及长稳仍在其他条目。后三核调度045b463仅改CI/测试、保持生产Go/依赖，实际3CPU/7GiB已由CI41125907702完整通过。
  - 关闭前历史：macOS ARM与Linux ARM容器既有证据保持。原生Intel的bf2e645（树同c83c95a）与58806be均已通过完整产品、终端及LaunchAgent生命周期/活动升级下载上传；实际tmux3.7c，不覆盖3.8。58806be的Linux amd64/arm64与真实WSL2产品/E2E/embedded/terminal通过，systemd维护仍失败。c10852f的真实WSL2完整产品/systemd维护已通过，两Linux产品链通过但首次daemon激活仍失败；已实际复现/opt0777祖先权限拒绝，继续改用/var/lib及前置校验。c108的Mac双架构全部阶段通过，tmux3.8实际运行已验；e2f的WSL2在/var/lib新布局完整通过，两Linux产品测试新失败后未进入维护。退出事件/真实HTTP拒绝夹具已修并通过专项race，独立维护准入的27项工具检查通过，当时下一轮完整平台仍待；保留arm64旧useradd超时未知原因。896b的两Linux及WSL完整矩阵现已通过，包括/var/lib正向维护；Mac Intel登录准备出现单次超时，未复现，Mac ARM运行中。已确认并修复gVisor的ARM64 race CAS错误，固定上游可构建修复快照，本地44顶层node race、CAS回归、全包build/vet与漏洞扫描通过。新依赖尚需完整平台验证；上述为关闭前历史过程；最终c1e同源完整矩阵已通过，详见[26](26-native-platform-acceptance.md)。
- [x] 长时间后台运行、资源泄漏、故障注入、安全边界与性能回归。
  - 2026-11-01关闭：按用户“测试通过即闭环”的口径，c4双平台默认12小时四角色自然轮换、30分钟恢复尾部及进程/容器清理已独立通过；Linux4371轮、macOS4192轮，全部11,848资源样本保留。故障注入、安全边界、持续终端/双向文件/四路TCP负载、性能基线、取消回收及相关 race/vet/CI 回归均有通过证据；插件业务、internal/plugin宿主及pkg/plugin公共SDK生产代码，以及13份观察输入与当前相同，认证等未调用路径和两处共享叶子修复分别核对。生产存活heap/object归因仍是技术观测限制，不作为本项阻塞，不据此宣称无限容量或零泄漏，见[终态](15-implementation-status.md#evidence-current-default-rotation-final-20261031)与[20](20-long-run-resource-review.md)。
  - 2026-10-29原697源Linux12h30m3.603s/4368轮、四角色自然凭据轮换、1803.013s恢复尾部及正常清理已独立核验通过。仅覆盖该冻结源；完整已有资源记录分析、当前alpha覆盖差异与长期资源稳定未关闭，见[最终核验](15-implementation-status.md#evidence-default-linux-final-20261029)。
  - 2026-10-29逐项核对697运行源与当前候选：521→541执行输入，34改/20新增/487不变，14观察工具相同；新sync Stop直接改变轮换/停机清理，路由attempts也包含持久字段隔离，旧长跑不能追认为当前组合长稳。安装器/独立analyzer不在此场景；已有专项保持各自范围，不因文件数变化盲目追加12小时，见[源码覆盖核对](15-implementation-status.md#evidence-long-run-source-coverage)。
  - 2026-10-29新697源Linux已只读分析4小时56分/1731轮、293完整检查点与八角色2344资源样本；保留RSS前段上升及末段回落，固定FD/测试goroutine门禁完成不等于全时无越界或无泄漏。原容器及源双树保持，自然轮换/尾部/终态清理仍待，见[中期资源分析](15-implementation-status.md#evidence-default-rotation-linux-resource-observation)。
  - 2026-10-29普通构建混合负载已完成：633同源macOS/Linux各32轮/232流、每方向14.5GiB、文件128MiB、前后各40空载终端样本；默认128两端准入、既有连接保持、仅释放一槽补位与清理通过，原始样本独立复算。修复单连接限流取消整组转发，Phase3恢复；不以有限负载证明无泄漏。原Linux12小时在替换身份采样阶段失败，观察夹具已通过双平台确定性修复检查；完整恢复/尾部仍未通过。见[容量结果](15-implementation-status.md#evidence-load-capacity-validation)和[隔离修复](15-implementation-status.md#evidence-port-limit-isolation-validation)。
  - 2026-10-29已确认并修复host/SDK纯client stream成功终态保留调用方取消注册：同测试修前红/修后绿、真实generated UDS/h2、双平台两整包race/vet、六包命名集成/架构和真实终端语义均通过；源/工具/退出/清理及七类复核完成。仅两生产文件变化，builtin sync原已逐次cancel，不能将此归因为历史heap斜率。当时待运行的普通构建并发负载现已完成，有限负载结果见[容量报告](15-implementation-status.md#evidence-load-capacity-validation)，仍不关闭资源稳定门槛。见[所有权报告](15-implementation-status.md#evidence-resource-ownership-audit)与[负载协议](16-architecture-conformance.md#evidence-load-capacity-protocol)。
  - 真实daemon确定性weak/AddCleanup正对照确认诊断绑定Close方法保留已关闭对象，诊断关闭成功后释放回调；同测红绿、原生race/vet通过，失败关闭仍保留重试。仅修诊断，不改生产、不新增负载，不把历史残余内存或运行增长声明为已修复。见[所有权验证](15-implementation-status.md#evidence-memory-closed-ownership)。
  - 新增已有关闭后profile的离线所有权分析：HTTP2样本映射runtime等待节点缓存，诊断Close方法值仍持有关闭daemon对象图。18项分析产物哈希复核，无新短负载实验；稀疏pprof差值不能分摊全部精确残余；当时待做的确定性释放测试后已完成同测红绿，不能由此将全部残余归因，见[所有权分析](15-implementation-status.md#evidence-memory-closed-ownership)。
  - 本批完成唯一一组延后profile导出的普通Linux对照：保持五窗口/全部工作量/64KiB采样率/双GC/状态遍历/JSON，仅在host关闭后测量之后导出profile。负载/空载317.709/317.672秒、136/128份原始归档及清理独立核验通过；Agent末段对象load−control为+9.98/min，关闭后仍+24,240 B/+228对象；heap负斜率受高点回落支配。结果不足以证明因果或无泄漏，已按预声明停止追加短窗口运行。见[观测开销报告](15-implementation-status.md#evidence-memory-observer-validation)。
  - 当前已修复Agent共享pool保留已结束请求取消回调的问题：成功客户端流响应与调用者取消均释放请求资源，双向/服务端流半关闭不误断共享池。确定性红绿、真实TLS多流和四包race通过；Linux普通构建修前负载/修后负载/同二进制空载三组各约320秒完成，334/334/326份归档和清理已独立核验。修后Agent后段load−control仍+15,597 B/min、+92.35 objects/min；只确认这一保留原因已修复，完整回归与预算夹具补验已通过，无泄漏验收仍待，见[请求生命周期报告](15-implementation-status.md#evidence-pool-request-lifetime-validation)。
  - 已有 SIGKILL/断线/撤权/并发/特殊文件与越界访问等故障测试及 race 回归；本批新增5个精确fsync EIO点及各自新进程恢复、2个真实ENOSPC分支和实际rename/撤销失败测试，见 [相关验收摘要](15-implementation-status.md#evidence-fault-validation)，不等同硬件掉电或穷举故障。新增可复现 UDS/TLS unary、1 MiB 上传和 bidi 半关闭性能/分配基线，见 [相关验收摘要](15-implementation-status.md#evidence-benchmarks)。macOS/Linux 各24轮真实终端/插件/TCP资源回收全部通过，见 [相关验收摘要](15-implementation-status.md#evidence-resource-regression)；新增持续终端/双向64KiB与1MiB同步工具，两平台最终1分钟自检通过（26/27轮）；Linux一小时冻结源码实验已完整通过（1,577轮、491,651,072 payload字节，尾部恢复与清理通过），该快照早于后续终端/节点/崩溃守卫/能力修复；macOS独立实验亦完整通过（1,540轮、480,378,880 payload字节），其快照包含终端/节点/崩溃守卫修复，但早于最新能力/helper修复。最初macOS前置撤权失败及后续终端修复记录保留。生产长稳、完整负载性能与资源泄漏验收未完成，见 [相关验收摘要](15-implementation-status.md#evidence-soak-validation)。
  - 本轮新增终端/双向文件/四路TCP同时运行的真实进程混合场景，双平台一分钟自检通过（25轮、100次TCP，每方向104,857,600字节）；固定FD/goroutine边界及同宿主空载前后基线均有记录。双平台十分钟实验及完整core尾部已通过：macOS10分0.131秒、247轮/988次TCP，Linux10分0.154秒、249轮/996次TCP，完整core703.29/709.77秒；各方向TCP载荷约1.04GB，固定FD/goroutine阈值通过，进程/快照/容器清理有证据。Linux外部取证脚本大小写误判导致自身退出1，原记录保留，精确CID独立复查确认容器不存在；30秒真实宿主轮换已通过，仍不代替默认12小时与生产长稳。见[混合负载](15-implementation-status.md#evidence-mixed-load-validation)及[真实宿主轮换](15-implementation-status.md#evidence-host-rotation-validation)。
  - 默认时长验收工具已通过普通独立宿主双平台短预检（76.29/82.09秒），明确natural_rotation_passed=false；macOS原真实12小时运行已于2026-10-29 02:37:12完成自然到期、30分钟恢复尾部及清理验收（4216轮/应用与collector退出0），独立复核通过；Linux原长跑已于替换身份观测阶段失败、完整恢复/尾部待验。两侧分别使用原冻结源码，不覆盖随后修复。普通/race诊断定位tunnel逐Read分配热点，已改每流读缓冲+每DATA独立payload，保留发送所有权；32KiB分块基准约减少85%分配字节，RSS未同比下降。双平台真实port集成及优化后一分钟混合完整尾部167.10/173.64秒通过，不代替长稳/容量验收。见[默认轮换](15-implementation-status.md#evidence-default-rotation-validation)、[内存诊断](15-implementation-status.md#evidence-memory-profile-diagnostic)和取证工具。
  - 本批同527文件快照普通构建五窗口实验双平台319.58/320.33秒完成，各80次文件更新/160次TCP及等时空闲、归档和清理通过；Agent后段post-GC heap仍增长约76,326/68,164 B/min，内存增长未归因；当时待做的同采样空载对照现已完成，见[545文件配对报告](15-implementation-status.md#evidence-memory-idle-control-diagnostic)，仍不算无泄漏。见[多窗口内存诊断](15-implementation-status.md#evidence-memory-windows-diagnostic)。
- [x] 代码审查、依赖扫描、关键恢复/认证/数据操作分支测试；覆盖率数字不能替代行为验收。
  - 2026-10-30：Mac/VPS独立普通用户与最终候选的完整10阶段矩阵157.31秒通过，操作系统资源回收独立核验；结合24公开包四层版本、既有06恢复/ACK专项及最终CI41047781448，关闭本项。两类8MiB真实SIGKILL、条件竞争、后台冲突/TUI与撤A保B范围详见[25](25-multihost-acceptance.md)；不外推完整tsnet认证、Claude、主机重启、其他平台或长稳。
  - 2026-10-30公开alpha.1/alpha.2双平台真实服务升级/回滚、重复安装和各92命令兼容矩阵已通过；同批慢流取消三类缺口修复及分源双平台检查通过，见[24](24-public-package-compatibility.md)与[取消回收专项](15-implementation-status.md#evidence-frontend-cancellation-20261030)。外部身份、物理主机/平台和当前候选长稳仍独立待验。
  - 2026-10-29当前安装恢复/新Stop的真实systemd组合遗漏已补：744源首轮13生命周期与4活动升级回滚通过，正常poweroff/精确清理及源工具核对完成；原断言/预算保持，无产品修改。并行架构审查修正旧状态/待办，故障、长稳及外部证明范围分别保留，见[服务补验](15-implementation-status.md#evidence-install-systemd-recovery-validation)。
  - 2026-10-29补齐新Stop修复后遗漏的真实syncack整包：743同源双平台race/vet各4顶层+7子用例通过，无skip；四种ACK丢失后的正常宿主重启保留未知journal/stage及旧baseline，无二次上传。使用既有测试、生产5秒Stop与原夹具预算，不把该组合补验当作重复Stop或长稳/资源通过，见[Stop与ACK补验](15-implementation-status.md#evidence-sync-stop-ack-integration-validation)。
  - 2026-10-29补齐CI四目标真实打包入口：独立741源容器中精确run block串行首轮通过，4归档/56成员/24静态目标及正常清理独立复核；保留既有全包编译和release单文件测试路径。无产品修改，远端CI、目标原生执行和签名分发未由本批验证，见[打包入口](15-implementation-status.md#evidence-release-ci-validation)。
  - 2026-10-29恢复审查四处偏差已修：Stop独立截止/实际排空、Stage持久化、未提交选择拒绝恢复启动、遗留临时文件重试。两组同测红绿、九个实际EIO/新进程恢复与738同源双平台六包race/vet通过；native采集器无PID清单退出1及1/2 skip保留，不称完整采集/系统验收通过，见[恢复合并](15-implementation-status.md#evidence-recovery-conformance-validation)。
  - 2026-10-29独立analyzer x/sys升级0.44.0并同库复扫两目标零finding，双平台各16终端捕获通过；节点只读诊断偏差有效同测红绿、双平台四包race/vet各196父/子pass及.4实际归档验证通过，无skip。不把局部修复或静态扫描当作全部平台/资源门槛完成；见[依赖修复](15-implementation-status.md#evidence-terminal-analyzer-remediation)与[节点修复](15-implementation-status.md#evidence-node-status-validation)。
  - 2026-10-29补齐默认make test/CI入口遗漏的13项Python采集器回归，新增test-tools及普通用户/Python3.10+前置。双平台同四输入13项无skip通过；实际root拒绝与原Go入口保持已核验。生产及验收断言不变，远端CI仍待实际运行；见[默认工具检查](15-implementation-status.md#evidence-default-tools-validation)。
  - 本批按文档补齐终端549文件双平台语义验收；安装529文件空间预检及备份fsync已有双平台专项/真实服务证据。sync提交后ACK丢失/两端回退保护的开发专项已通过，旧状态迁移栅栏、SDK固定目录/未来schema字段拒绝代码均已完成，SDK原生受影响race及Linux故障矩阵已通过，最终v3同源双平台合并已通过，恢复sync条目；完整长稳、资源归因及外部门槛仍待。已有syscall故障、marker或历史全套通过不能替代这些恢复边界验收。
  - 每批架构复核、关键行为测试和固定 govulncheck v1.1.4 扫描已执行；后续审计修正原CLI-only范围，改全仓库package扫描并将compress升级至修复GO-2026-5841的1.18.7，复扫未发现受影响的已导入包（仍记录未导入openpgp的模块公告）；连接/安装合并 macOS 全套 race 与 Linux 全套含 E2E 已通过。后续节点路径、CLI 权限/交互和24轮资源循环已有专项验收；本次修复 tmux/端口扫描子进程输出经 io.Copy 绕过上限的问题，macOS/Linux 回归通过。新增后端 API/退出恢复已通过双平台专项；安装就绪和产物专项通过，最终macOS与冻结Linux全套race含E2E均通过（E2E190.197/173.475秒），三项合并回归发现的问题已修复并保留记录。本批另完成65处Snapshot.Get调用审计、真实TLS跨Token重放授权与持久状态故障分支验证、四目标全仓库依赖静态扫描，详见 [相关验收摘要](15-implementation-status.md#evidence-security-validation)；服务修复/compress1.18.7及本批节点/终端/崩溃守卫的最终完整双平台race/E2E已通过（207.127/177.266秒），历史失败与修复均保留。此前版本/能力与helper批次完整macOS/Linux race亦通过（E2E196.232/183.381秒）；原macOS一秒认证夹具跨JWT整秒边界过期，修为三秒并增加实际expiry断言，仅改测试，双平台各20次race通过。
- [x] 产物签名/校验、版本固定、插件匹配、真实安装源和 Homebrew 等分发渠道。
  - 2026-10-30：四目标原生CI41037444120实际公开tap完整安装/重装/测试/卸载及所有权清理全部通过，见22。结合固定正式身份、自动tag发布、公开资产认证、插件匹配、真实服务包验收与密钥演练，关闭本项；本机macOS27工具前置限制及完整产品平台另记，不外推Apple公证。
  - 2026-10-30：新增自动Homebrew生成/发布与隔离12步密钥演练，当前LICENSE切换GPL-3.0-only，首个Homebrew alpha.2已完成全自动发布及公式提交，公开22资产认证和双平台暂存通过；实际渠道安装见22，原生macOS brew受Xcode/CLT门槛阻塞，复合项保持未勾选。
  - 已按用户要求替换为全自动tag发布，首个alpha也使用新流程；同源CI、隔离签名和完整草稿发布已有本地验证，GitHub已限定tag环境并取消人工审批。92cafaf真实签名包双平台新装/运行/升级回滚通过；新tag公开22资产下载认证与双平台暂存已通过，隔离密钥轮换/撤信演练已完成，生产身份未操作；Homebrew完整平台验收仍待。详见[发布准备](18-release-readiness.md#github-actions-自动构建签名与发布)。
  - 2026-10-29已完成专用公钥身份的Actions四目标签名：v0.0.1-alpha.1固定92cafaf源、成功CI40872062129、DDSRem审批后签名运行40877214329成功；22文件已本机再次验签/完整payload hash及来源核验。此真实签名包的安装/运行验收、公开Release/安装源/Homebrew及密钥轮换撤销演练仍待，复合条目保持未勾选。
  - 2026-10-29同739源最新未签名候选v0.0.0-local.20261029.5已交付macOS/Linux arm64归档，541执行输入与已验738一致；14成员、六程序/双插件版本与hash、来源清单及每侧52实际命令通过，含两agent诊断/正常清理与归档helper首次/重复stage-only。暂存不激活服务、不生成current/receipt/config/state，标准服务前后保持。Commit/BuiltAt如实unknown；旧.2/.3/.4、608测试签名与697长跑范围独立保留。见[本地候选](15-implementation-status.md#evidence-local-candidate-20261029-5)。
  - 新增显式HTTPS固定版本Fetch，先验签再下载且拒绝重定向/凭据URL，限量/取消/私有输出及CLI双平台通过；此前608源94MB native产物经当前Fetch取回、独立OpenSSL核验及真实helper stage-only通过。正式来源、公钥分发/撤销及渠道仍待，不以测试TLS/密钥建立信任根。见[下载验收](15-implementation-status.md#evidence-release-fetch-validation)。
  - 同608文件冻结源码双平台实际`make release`与测试密钥签名通过；可信源码验签后仅从认证私有副本解包，六二进制版本和双插件清单核对，macOS client及Linux client/daemon实际stage-only通过，私钥和精确容器已清理。此为该608冻结源码的本地构建验收，不替代正式签名身份/下载源/渠道。见[完整签名构建](15-implementation-status.md#evidence-current-release-validation)。
  - 本批新增离线 Ed25519 sign/verify：独立公钥、固定版本/平台、同流hash与私有副本、认证pin；修复严格PEM及打包sidecar覆盖，双平台race、vet和真实归档验签后stage-only通过，独立证据与架构复核完成。正式身份、公钥分发/撤销政策、真实安装源与渠道未建立，整项继续未勾选。见[离线验收](15-implementation-status.md#evidence-release-trust-validation)。
  - 固定版本打包、校验和、插件匹配及内嵌提取已实现；macOS arm64、Linux arm64 和 Linux amd64 翻译环境均通过真实内嵌 sync 子进程/CoreAPI 测试，四宿主版本及 staging 清理通过。专用身份的归档签名及验签现已完成；公开公钥分发/撤销、真实下载源/渠道和发布未完成。
- [x] 发布说明、升级限制、故障排除、许可证、真实支持入口。
  - 首个公开alpha包含冻结源码的[发布说明](releases/v0.0.1-alpha.1.md)、升级/回滚限制、故障排除链接、包内MIT与真实[Issues入口](https://github.com/DDSRem-Dev/Shikigami/issues)。tag/Release、公开资产认证与入口HTTP200已验证，关闭本条；其余复合平台/资源/分发条件不因此关闭，见[公开alpha验收](15-implementation-status.md#evidence-public-alpha-release-20261029)。
- [x] 经验证后才能把“设计目标”改成“已支持”，不得预先勾选完成。
  - 2026-11-05关闭：完成[当前支持矩阵](18-release-readiness.md#current-support)、源码能力/CLI帮助对照及版本来源核对；已支持、限定验收、用户确认和未支持范围分别说明。原42项全勾选不代表后续修复已随alpha.2交付或稳定版已发布；检查结果见15/16本批记录。

## 后续迭代

1. GUI：Wails + React/TypeScript + xterm.js，通过本地 agent；版本、平台打包、托盘和通知需专门验证。见 12，不计入首版工期。
2. 企业代理/WSS 隧道：专用双端适配和代理测试，不能以 Connect-Web/gRPC-Web wrapper 冒充原生双向流。
3. 同步增量优化、三方合并、独立目录/worktree 文件快照与恢复。
4. 终端历史录制/回放、Claude 官方对话恢复集成。
5. 团队多租户、访客沙箱、插件市场、云配置等另行设计安全边界。

后续功能无既定发布日期，不因 UI 草图或协议预留就承诺已可用。

## 最近完成批次与当前工作

以下保留前序批次的历史说明；当前42/42及支持范围以文首和18为准：官方网络、Homebrew渠道及本批恢复/版本/多设备与有限审查原项均按各自证据关闭。官方节点移除暴露旧流回收问题，peer生命周期修复及最终5秒状态观察预算已取得本地回归证据，前次重新认证及clean最终构建重启/应用重连已通过，随后官方移除复验已确认旧附件回收及主节点不受影响；控制失联分类、认证恢复与clean控制面断网复验已完成，完整认证矩阵已按2026-11-01用户确认关闭，见23；真实主机重启/重新登录仍属未实测支持边界。原macOS12小时和697源Linux12h30m及终态核验已完成，旧Linux失败保留，不能追认为包含本批修复的当前版本长稳。原有插件清理、系统共存、跨schema恢复和自动发布证据保留各自源码范围。

此前启动的c4c5a56两正式长稳任务与真实未使用key到期任务均已完成并通过独立核验；终态结果和精确清理见文首，三项不再列为等待。耗时验证与独立实现、七类审查、证据分析和文档同步并行；冻结每组输入、隔离状态并限制构建并发。原Linux578失败保留，后续697源恢复/尾部已通过；旧源码结果不外推为当前源码通过。

2026-10-31官方节点key单次短期到期已实际发生：官方回读期限后SDK进入NeedsLogin但Self.Expired=false，旧终端和TCP已关闭。已修复组合证据分类（仅NeedsLogin、已知self公钥及非零期限且超过期限1秒，或显式Self.Expired），11项定向race与vet通过；CLI对有效expired/uninitialized尝试继续原有界等待，最终整包race42顶层/183含子项、28.607秒、零skip通过。干净m8重启后SDK不再提供旧期限，实际为needs_login；CLI9仅替换前端，01:34 Chrome重新认证后同IP/daemon/凭据自动ready，终端新回显、文件双向收敛及新TCP精确回显通过；不把该恢复追认干净候选到期分类已现场观测，详见[23](23-tsnet-external-acceptance.md)。CI优化已实现四原生并发、按资源选择p1/p2、隔离用户复用私有缓存副本及安全失败行号；17项工具检查和独立七类审查通过，实际收益待新CI，旧失败保留，见[26](26-native-platform-acceptance.md)。原42条及36/42保持。


2026-10-31追加平台修复：已适配tmux3.8对已退出pane返回空PID的实际格式变化，仅dead缺PID记0，live缺PID仍拒绝；退出证据与精确tmux身份保持，PID0不进入信号/活跃归属。确定性修前红，本机3.7b两包race65顶层零skip/fail及vet、独立七类复核通过；真实3.8平台须待新CI，不用本机旧版本覆盖替代。Mac排队实测已降至Intel5秒/arm64 2分27秒，整轮耗时仍待，详见[26](26-native-platform-acceptance.md)。原42条及36/42保持。


**2026-10-31 CI修复与性能收敛：** 首轮固定源原生Intel（tmux3.7c）完整产品及LaunchAgent验收通过；58806be轮Linux双架构与实际WSL全产品/E2E/embedded/terminal通过，两Linux全包race约17–19分钟降至9分钟。该轮Mac的tmux3.8空PID与GOMAX1期限失败已分别取得红绿和修复；systemd专属根曾移至/opt，0755隔离容器的生产路径对照通过；后续发现托管镜像可将/opt设为0777，修复并未覆盖这一环境，现补/var/lib及逐层前置校验。arm64 useradd超时仍待原因，新固定阶段诊断保持60秒预算。构建参数对齐实测185.545→152.838秒且六二进制/双manifest完全一致。新增按角色/平台/来源隔离的partial/warm缓存，16缓存、18平台、8构建元数据工具检查及独立七类审查通过，远端新缓存和修复后整轮结果待CI。详情及失败/源码范围见[26](26-native-platform-acceptance.md)；同身份官方到期恢复见[23](23-tsnet-external-acceptance.md)。原42条及36/42保持，受保护长测和预约key到期未修改。

**2026-11-01 CI长任务缓存修复：** 远端运行`41226652766`的Linux、macOS ARM及多数测试通过；macOS Intel的E2E在冷构建中出现子进程被系统终止，heavy integration在测试自身45秒构建预算内超时，embedded测试已通过但setup-go缓存归档耗尽10分钟job预算。根因是拆分job未复用preflight的Go module/build cache及Intel冷构建过慢。当前修复让E2E、heavy integration、embedded关闭setup-go自动缓存，恢复preflight私有缓存并预取模块；embedded job预算调整为15分钟。该修复已通过本地actionlint、平台/脚本测试与架构race，尚未由新远端运行验证；不得据此宣称CI全绿，WSL长任务仍单独记录实际耗时。

本次新运行`41230062520`验证了恢复步骤，但确认preflight因未向`ci_go_cache.py plan`传递成功状态而没有保存缓存；Intel heavy/E2E仍分别在生产子进程编译时被`signal: killed`并超时，其他矩阵及embedded通过。已补充显式`SHIKI_CACHE_BUILD_OUTCOME=success`与`SHIKI_CACHE_ACCEPTANCE_OUTCOME=success`，只有成功到达缓存计划步骤才会保存来源hash对应的native缓存；该补丁待下一次远端运行验证。

运行`41233288832`已证明Linux preflight成功保存缓存，但macOS Intel首次冷构建在原10分钟preflight预算内被取消，导致依赖矩阵跳过；已将preflight预算调整为15分钟，让首次Intel构建有机会完成并保存缓存。该预算变更不删减任何阶段，仍待新远端运行验证。

`41235197414`中Intel preflight与native cache保存成功，heavy integration通过；E2E仍因其`go build -race`子进程缓存未被非race预热而出现一次`signal: killed`。已在preflight增加cmd、sync、port等E2E/集成目标的race缓存预热，预算增至20分钟以覆盖首次生成；下一轮需验证Intel E2E和完整矩阵。

同一运行的Linux arm64 E2E还记录一次daemon重启后本地agent尚未恢复的瞬时`service unavailable`；远端连接已重新协商成功，失败发生在有界恢复窗口边界。测试现在仅在该明确瞬态内重试最多10秒，其他CLI错误立即失败；这是测试稳定性修复，不改变产品重连、期限或业务断言，待新远端运行验证。

`41239995440`验证race预热和10秒恢复重试后，主矩阵、Intel E2E/heavy、Linux arm64 E2E及embedded均通过；terminal/maintenance四个job在启动阶段因`actions/download-artifact@v7.0.2`不存在而失败。已统一到release workflow已验证的`v8.0.2`，下一轮只需确认artifact下载后的terminal/maintenance与最终workflow结论。

`41242641679`因CI workflow变更使cache source key重新冷启动，Intel preflight在20分钟预算内完成非race build/lint后仍未完成race预热，导致下游跳过；其余平台无失败。已让缓存来源hash忽略workflow编排文件和CI helper脚本、仅保留实际Go工具版本变化，并通过16项缓存专项、83项脚本、actionlint和diff检查；下一轮应复用现有native/race缓存并验证完整矩阵。

`41247430410`验证artifact下载、主矩阵、Intel E2E/heavy、WSL2及macOS/Linux大部分收尾任务；Linux amd64 systemd在旧实现的`useradd --create-home`阶段60秒超时，macOS arm64 terminal语义阶段在托管runner上约30秒失败。失败证据没有扩大到产品断言：前者已改为预创建隔离home、`--no-create-home`并对短暂passwd锁做三次有界重试；后者为每次终端专项使用独立0700 `TMUX_TMPDIR`，避免中断运行遗留socket污染。两项本地平台工具29项、脚本83项、actionlint、终端专项重复运行及diff检查通过；旧run重跑中Linux maintenance已成功，新的源码提交仍需完整远端CI确认。

新提交`2d6980e`的完整矩阵确认Linux systemd、WSL2及其余已完成任务通过，但macOS arm64 terminal仍失败；仅隔离`TMUX_TMPDIR`不足以定位该托管环境差异。现增加只输出固定checkpoint文件名的脱敏失败摘要，不上传终端原始日志或屏幕内容，下一轮据此继续修复；当前不宣称CI全绿。

`2421ba3`运行的公开摘要显示terminal失败发生在首个屏幕checkpoint之前；已进一步为analyzer构建、测试二进制编译和测试启动分别加入私有running/done阶段标记，仍只公开固定文件名。Linux terminal与两项systemd维护本地回归保持通过，下一轮远端结果待定。

2026-11-01终端托管稳定性补强：最新`41255473767`已证明macOS arm64失败发生在测试进程启动后的首轮语义执行（analyzer构建与测试编译均通过），Linux terminal通过。终端脚本现在对完整隔离fixture执行一次有界重试；两次均失败才报告失败，断言、测试超时和私有原始日志策略不变。失败摘要继续只公开固定阶段文件名/数量。该修复待新提交的完整远端CI验证，不提前宣称全绿。

`41257594777`验证了原重试分支未实际进入第二次运行（shell errexit与共享tmux目录使其仍约27秒失败）；现已改为显式条件分支，并为每次尝试创建独立0700 `TMUX_TMPDIR`，保留两次失败才报告失败的门槛。该修复待下一轮完整CI验证。

该轮两次隔离尝试均在首个checkpoint前失败，阶段总时长约25秒，符合托管runner上agent UDS启动窗口过短的表现；已将terminal fixture的有界agent就绪等待从5秒增至15秒，保留上下文总截止、所有断言和清理要求。新提交待远端验证。

`41262112652`另暴露Darwin amd64 preflight在`go install protoc-gen-connect-go@v2.0.2`访问proxy.golang.org时的65秒TCP超时；不是源码或协议生成漂移。工具安装脚本现对下载和三个固定工具安装各做最多3次有界重试，并保留失败即失败语义，待下一轮远端验证。

该轮工具重试已使Darwin amd64 preflight继续通过；新的macOS arm64 terminal仍需进一步分类首个checkpoint前的测试失败。失败摘要现在增加固定`failure_kinds`枚举（不公开原始日志），用于下一轮收敛夹具问题。

新运行还捕获到Darwin arm64内部client测试在取消竞态中收到原始`context.Canceled`而不是等价Connect Canceled码；该路径由`terminalCause`有意保留调用方取消原因。测试现接受这两种等价取消表现，其他错误码断言不变；本地20次race通过，远端待验证。

进一步复核发现部分HTTP/2路径会把取消编码为无cause的Unknown Connect错误（消息仍为标准context cancellation），测试无法可靠识别。`terminalCause`现将本地/传输层取消统一为Connect Canceled/DeadlineExceeded并保留`errors.Is` cause；其他错误原样返回。该生产边界修复已本地20次race通过，待远端验证。

同轮还发现Darwin arm64的端口准入测试在32个静默请求尚未全部进入 admission counter 时就探测第33个请求，偶发得到插件不可用而非预算耗尽。测试现先等待精确计数后再探测；本地10次race通过，未改变生产限额或错误语义。

<a id="plugin-manifest-phase-extension"></a>
## 插件 Manifest 与 UI 注入（Phase 5 后扩展，2026-11-11）

本批为 42/42 原清单关闭后的后续工程扩展，不修改原路线图条目数量或计数，不替代既有验收门槛。

**已完成：**

- manifest.json schema 及 `PluginManifest`、`ValidateManifest` 类型（`pkg/plugin/manifest.go`）
- `ConfigField` schema 及 `ValidateConfig` 宿主预校验（`pkg/plugin/schema.go`）
- `UIContribution`、`StatusPayload`、`NotifyPayload` 类型（`pkg/plugin/ui_contribution.go`）
- `ActionRequest/Result`、`ActionImplementation` 接口（`pkg/plugin/action.go`）
- `Plugin.InvokeAction` 生成 gRPC 绑定（`proto/plugin.proto`、`pkg/protocol/plugin/plugin_grpc.pb.go`）
- `AgentActions` Connect 非生成扩展（`pkg/protocol/agent/agentconnect/agent_actions.go`）
- `Lifecycle.InvokeAction` 及运行时实现（`pkg/plugin/lifecycle.go`、`pkg/plugin/grpc.go`）
- `PluginRegistry` 第三方插件目录扫描（`internal/plugin/registry.go`）
- `SettingsRegistry` 活跃 UI 注入记录（`internal/plugin/settings.go`）
- `ActionRouter` 路由 InvokePluginAction 到 Supervisor（`internal/plugin/action_router.go`）
- agent `InvokePluginAction` IPC 接入，`internal/agent/ipc.go` 拦截器扩展（AgentActionsName OR 条件）
- 动态 CLI 命令注册（`cmd/shiki/dynamic_commands.go`，路径 `shiki plugins <id> <command>`）
- `plugins/sync/manifest.json`、`plugins/port/manifest.json`

**下一阶段门槛（TUI/GUI 渲染，当前 PENDING）：**

- TUI 状态栏状态槽渲染：订阅 `plugin.status.<id>.v1` 事件，从 `StatusPayload.text`/`state` 更新状态栏
- TUI/GUI dashboard 面板区渲染（`UIDashboardPanel` 声明）
- 设置界面从 `ConfigSchema` 自动生成表单

TUI/GUI 渲染层实现前，`shiki plugins <id> <command>` CLI 路径和第三方插件注册能力已完全可用。
