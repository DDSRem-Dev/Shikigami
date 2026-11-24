# alpha.10 自动化复验与 beta.1：实机测试执行手册

**2026-11-24（alpha.10 自动化复验、空闲恢复与候选修复进展）：**
- **当前正式部署基线：** Mac（`darwin/arm64`）与 Debian 12 VPS（`linux/amd64`）均运行正式签名 `v0.0.1-alpha.10`（固定源 `c2ac2068df1794e71c555051a9c5d307df7b994f`，[Release 45056120246](https://github.com/DDSRem-Dev/Shikigami/actions/runs/45056120246)），本机 3 插件与远端 4 插件全部 `ready`，`work` Chrome `158.0.8127.36` 保持 `running/browse`（`instance_id: 014ec66bfbef6a55488b14117be101d7`），`region inspect` 保持 `Asia/Tokyo` / `ja_JP.UTF-8`，Mac LaunchAgent `present/enabled`。
- **长空闲 `RECONNECT_EXHAUSTED` 恢复实测通过：** 跨日空闲/睡眠后连接处于有界 `offline`（`RECONNECT_EXHAUSTED`），执行 `shiki connect srv_5953306eb779a11a396fea38e2893685` 在 `6788.7 ms` 内恢复 `ready`（`tailscale-direct`），`daemon_id` 与 `credential_revision` 不变，无需重签 Token 或重新配对。
- **alpha.10 非破坏性自动化功能复验与零残留收尾通过：** 独立临时项目完成 `B02`（PTY detach/reattach 同 PID 与文件内容保持、配置快照恢复新 PID、`stats`）、`B04`（初始冲突 `show-diff`/`keep-local`/`keep-remote`、已有基线双向自动上传/下载、`pause` 隔离、双端并发修改 `keep-both` 双版本核验、本地重命名收敛与删除传播；完整轮 5 次默认预算 `sync resolve` 全部一次成功，耗时 `2160.0–11924.4 ms`，其中 `6213.9 ms` 与 `11924.4 ms` 实际跨越旧 5 秒上限证明 15 分钟多步文件预算生效）、`B05`（远端 `3000` loopback HTTP 转发成功、后端停止拒绝期间保持同一本地端口 `56102` 与 `listening` 状态不重绑、后端重启后原端口恢复、`ports stop` 回收）、`B06` 自动化子集（`browser status work` 连续 10 次 `0` 失败，中位 `627.5 ms` / 最大 `1188.5 ms`；暂存区文件上传 `939.8 ms` / 列表 / 下载 `2532.5 ms` SHA-256 一致并精确清理）、`B07`（日本配置 `region plan` `0` 变更）及 `20/20` 只读 `projects list` 采样（`0` 失败，最小 `275.8 ms` / 中位 `605.5 ms` / 最大 `2857.2 ms`）。全部临时项目、会话、快照、同步映射、端口转发、远端测试目录及浏览器暂存文件已清理并复核归零。
- **保留真实瞬时超时记录（不掩盖）：** 多轮调试与复验期间，现网 Tailscale 链路仍观察到偶发 5 秒短 unary `deadline_exceeded` 尖峰（含前序轮一次 `sync resolve` 内部元数据探测 `7659.9 ms` 超时、一次约 10 秒链路停顿导致 `projects create`/`projects list` 连续触发 5 秒截止且复查确认未在服务端半创建、一次 `attach` 前置查询 5 秒超时，以及完整通过轮中一次只读 `sync conflicts` 轮询 `5034.9 ms` 超时后下一拍恢复）。未发生数据损坏、越权或身份变化，但长期稳定性（`B08/B09`）仍待用户日常与切网/睡眠检验。
- **默认 `/bin/sh`（`dash`）方向键缺陷与 Claude 全屏 TUI 模式（`CLAUDE_CODE_NO_FLICKER=1`）已在当前候选源码修复：** `internal/server/tmux/shell.go` 及 `session` 启动/停止路径已将普通运行用户配置的 `/bin/sh` 或 `/bin/dash` 自动升级为可用的 `/bin/bash`（保留用户显式配置的 `zsh`/`fish` 等交互 shell）；同时针对 `B03` 实测发现远端 Claude Code 在 `tmux` 下默认采用非全屏内联渲染（`alt=0`，输入框悬浮在消息下方且流式重绘易闪烁）的问题，`internal/server/tmux/tmux.go` 在 `Create`、`Respawn` 与 `RespawnDead` 中默认注入 `CLAUDE_CODE_NO_FLICKER=1`（保留管理员显式环境变量覆盖），已在 VPS 实测确认开启后 `178x57` 窗格进入 `alt=1` 全屏模式并将输入框固定至底行（第 55–57 行）。两项修复均已通过本地与 Debian 12 容器回归，随 `v0.0.1-beta.1` 签名包发布。
- **用户实机验收与 `v0.0.1-beta.1` 公开发行闭环（2026-11-24）：** 用户已在正式 `alpha.10` 环境确认 **`B06` 远程浏览器日常使用体验正常**、**`B08` + `B09` 断网/睡眠恢复与持续使用正常**（按用户确认记录，未另存本地 CSV），并通过远程 `work` Chrome 完成 **`B03` 官方 `claude auth login`（`loggedIn=true`）与受管 `claude-check` 会话实际文件创建任务（`beta-check.txt` 内容 `SHIKI_BETA_OK` 核验一致、detach/reattach 保活通过）**；`b03-claude-check` 临时测试目录、会话与项目已清理归零，Claude 登录状态与 `work` 浏览器完整保留。随后候选固定源码 `f234a090d42ebdb57c1f23e9ff317b6d43770f80` 通过远端 [CI 45404108766](https://github.com/DDSRem-Dev/Shikigami/actions/runs/45404108766)（57/57），不可变标签 [v0.0.1-beta.1](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-beta.1) 经 [Release 45420481374](https://github.com/DDSRem-Dev/Shikigami/actions/runs/45420481374) 完成 23 个签名资产公开发布与 Homebrew 公式自动更新（`9d5c0c1167e7cf45b3bc8389b42f41c8cd344d68`），并通过四目标离线验签、公开 `bootstrap.sh` 暂存、隔离 `systemd` `alpha.10 -> beta.1 -> rollback -> uninstall` 与隔离 Linux 远端 tap 核验（见[15](15-implementation-status.md#evidence-public-beta1-release-20261124)）。用户现网两端当前仍运行已验收的 `alpha.10`，可随时按 [28](28-quickstart.md) 升级至 `v0.0.1-beta.1`；`B10` 全新双机首装与 `B11` 物理主机重启按约定保留为明确未实测边界。

2026-11-22历史反馈（alpha.9）：正式alpha.9选择Smooth且暂停Claude登录背景视频后，用户报告整体操作可用，但滚动低帧率仍明显。现两端已升级正式 `alpha.10`（默认 `Smooth`、noVNC 1.7.0、新增底部本地传输观测指标），请按下方第 `1a` 节直接体验 `alpha.10` 浏览器滚动并反馈。可控制查看器须显式 `browser open work --read-only=false --launch` 并在窗口 Take control，默认只读不变。

本轮对照已回报：触控板；按指导resize缩小后无明显改善；拖动远端网页滚动条稍好。Fn+↓在该机器触发音量，不计为Page Down测试，不再要求用户重复。实际framebuffer尺寸未独立记录；结果只用于继续区分输入步进与画面更新，不证明带宽正常或根因已定。助手继续隔离测试，暂不需要用户额外操作或登录账号。


**历史严重卡顿反馈（后续改善以上方记录为准）：alpha.9的Ego和日常查看器曾都难以使用；滚动及长期体验验收仍未通过。** 本次已进入Claude登录页仍成立，账号授权暂停；继续定位见[后续记录](15-implementation-status.md#evidence-alpha9-latency-followup-20261122)。

**当前两端：正式签名alpha.9（2026-11-22），短时浏览器交互与本次人工进入Claude登录页已通过，账号授权待验。** 固定源`0b4847d`的[发布前CI](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44924752906)57/57通过，[Release44929424103](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44929424103)成功并公开23资产；[28](28-quickstart.md)安装入口已切换alpha.9。两端7插件ready、work为running/browse，原配置、Token/auth、密钥环、profile及daemon身份保持；Chrome158.0.8127.36和x11-utils7.7+5已核对。12次CLI成功（0.180–0.211秒）只证明短时健康，见[升级证据](15-implementation-status.md#evidence-alpha9-upgrade-20261122)。本轮正式alpha.9已另行完成自有data:页面点击/输入/滚动和5次显式重连回只读后接管；Balanced/Smooth各5次计时含工具开销，不能严格比较前版不同夹具或证明全局流畅。用户人工验证后已进入claude.ai/login，助手截图确认Google/Apple/email正常登录表单，未输入账号；本轮viewer/TaskSpace/10个临时文件已清理，远端Chrome登录页明确保留，见[alpha.9查看器证据](15-implementation-status.md#evidence-alpha9-viewer-20261122)。历史alpha.8记录保持原版本，不自动扩大为本版其它功能通过。

当前执行方式：用户已授权助手自动执行命令与可自动化项目；账号登录和主观体验单独交接。历史alpha.7已完成冲突恢复、同步功能、终端保活/配置快照、端口与浏览器自动化实测，当时端口恢复及间歇超时失败且后续修补尚未部署；resolve使用显式2分钟预算，原默认超时门槛不因此关闭。当前alpha.9已部署，这些历史功能记录仍不能替代新版复验。

历史alpha.8已完成当时两端升级、版本/连接/插件基线、同步默认调用路径功能、端口后端拒绝恢复、浏览器控制/5次重连/IME/剪贴板/文件，以及终端保活和配置快照回归。当时测试产物已清理、验收项目/映射/terminal-check/jp-auth-check保留；这些旧验收资源后来已按2026-11-22新授权删除，不应继续复用。默认shell、偶发状态超时和长期稳定性仍开放；同步部分提交故障注入没有在实机重做。网络专项暂跳，Claude账号授权继续暂停。

热点复测补充（2026-11-19）：一次显式connect恢复后18次远端查询全部成功，新空shell创建0.485秒；相同画面8次点击更新0.904–2.122秒，较原网络改善但仍有迟滞。有限测试不替代B06/B09长稳，Claude登录继续暂停；切网耗尽重连后可使用`shiki connect "$SHIKI_SERVER"`显式恢复，无须重新配对。

历史阻塞（2026-11-19）：用户两次创建会话超时，真实浏览器点击至画面更新复现约1.3–8秒；现场还观察到有限网络损耗证据，尚未完成端到端归因。`jp-auth-check`已由一次有固定request_id的后续请求创建成功，勿重复创建；Claude授权继续暂停。B02/B06/B09当前不能计为整体通过，先处理稳定性再推进账号测试或beta发行。详见[本轮诊断与源码修补](15-implementation-status.md#alpha7会话超时与浏览器卡顿实测2026-11-19稳定性未通过)。

编制日期：2026-11-18；本轮验收更新：2026-11-22。两端正式alpha.9部署、实际版本及browse模式已核对，新版浏览器交互和Cloudflare验证未验；下列已完成的alpha.7/alpha.8记录保留原版本。本文同时保存历史记录与待执行步骤，并非整套测试通过报告。现有安装、配置、凭据、正式备份、浏览器work profile和agent-remote保留。

## 1. 当前结论、分工与完成条件

alpha.7 已完成公开签名验证和 VPS Debian12/amd64、Mac arm64 升级，7 个插件实例 ready；487 秒浏览器实测覆盖鼠标、输入、滚动、显式重连。随后 12 次 CLI 请求中有 1 次超时、之后恢复。它仍需排查，不能由 CI 全绿宣称消失。交付文档固定源 `9f55e5a` 的 [CI44120621767](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44120621767) 最终57/57成功；首轮3项ARM Mac因托管runner未领取而取消，原条件补跑成功。

[18 的原 beta 发布前清单](18-release-readiness.md#beta-readiness)已有通过证据，但部分是较早候选；beta 发布时必须绑定最终源码。真实 Claude 登录和物理重启在既有验收中明确保留为未实测，并非早已存在但被漏掉的硬阻塞。为本次“实际可用后再发 beta”的目标，建议补齐以下用户路径；新增建议不追改历史门槛或宣称旧记录已覆盖新插件。

| 编号 | 项目与建议时长 | 谁执行 | 当前 alpha.10 状态及下一轮待办 |
|---|---|---|---|
| B01 | 安装/连接基线，5分钟 | 我已完成，你可随时复查 | alpha.10 两端正式签名升级、7插件 ready、work running/browse、跨日 `RECONNECT_EXHAUSTED` 显式 `connect` 6.8秒恢复已验 |
| B02 | 测试项目、终端、detach/attach，10分钟 | 我已完成自动化，候选修复随 beta.1 发布 | alpha.10 实际 detach/attach 同 PID、配置快照新 PID 及 stats 已复验通过；默认 `/bin/sh` 方向键与 Claude 全屏 TUI 修复已通过容器 PTY 测试，随 `beta.1` 签名包上线 |
| B03 | Claude Code真实授权与一次任务，10–20分钟 | **已完成实机通过** | 用户经远程 `work` Chrome 完成官方 `claude auth login`（`loggedIn=true`），受管 `claude-check` 创建 `beta-check.txt`（`SHIKI_BETA_OK`）及 detach/reattach 保活核验通过；临时项目已清理 |
| B04 | sync双向同步、暂停、冲突、删除，15–20分钟 | 我已完成自动化 | alpha.10 初始冲突、双向自动传输、暂停隔离、`keep-both` 副本、重命名与删除通过，含 >5秒多步 resolve 成功；偶发 5秒短查询超时如实保留 |
| B05 | port真实HTTP转发、断流和重连，10分钟 | 我已完成自动化 | alpha.10 远端 3000 HTTP 转发、后端停止拒绝期间保持同端口 `56102` `listening`、后端重启原端口恢复及停止清理全部通过 |
| B06 | browser控制/中日输入/剪贴板/文件/持久化，20–30分钟 | **已完成通过** | 连续 10次状态查询与暂存文件往返清理通过；用户确认 `alpha.10` 默认 Smooth 日常使用体验正常 |
| B07 | region实际值和只读计划，5分钟 | 我已完成 | alpha.10 `region inspect`（`Asia/Tokyo` / `ja_JP.UTF-8`）与 JP `region plan` 零变更已复验通过 |
| B08 | 自然断线/切网/睡眠恢复，10–15分钟 | **已完成通过** | 跨日空闲 `RECONNECT_EXHAUSTED` -> `shiki connect` 6.8秒恢复已验；用户确认断网/睡眠恢复正常 |
| B09 | 日常工作2小时＋可选隔夜8–12小时 | **按用户确认通过** | 20次短采样 0 失败；用户确认 `B08 + B09` 正常（无独立 CSV 落盘，按用户确认登记） |
| B10 | 干净环境一键首装、取消后续装 | 独立机器可选门禁 | 现有升级与隔离测试已覆盖签名与向导逻辑；无独立新双机时不擦除现有环境，在发行说明保留首装实机未验边界 |
| B11 | 实际重启、启动解锁/登录恢复 | 你可选维护窗口，我前后检查 | 可选补验（见第11节）；若暂不安排物理重启窗口，继续按 [11](11-automated-installation.md) 明确保留未实测边界 |
| B12 | 候选回归、故障注入、升级/回滚、签名发行 | **已完成通过** | 候选 `f234a09` 远端 CI `45404108766` 57/57 通过，`v0.0.1-beta.1` Release `45420481374` 公开 23 签名资产并通过四目标离线验签、`systemd` 升级/回滚与公开 tap 验证 |

你不需要手工运行 Go 测试、构建插件、检查签名或学习 CI。你必须亲自完成的是账号授权、主观交互体验，以及你选择进行的断网/睡眠/重启操作。需要额外机器时先明确用途和可用环境；本手册不会要求擦除现有Mac/VPS来模拟首次安装。

建议用一个晚上完成B01–B08，随后正常使用B09。耗时是人工安排估计，不是软件性能承诺。GUI、原生Windows、对话历史恢复、三方自动合并、代理/地区“防封保证”不是本次验收对象。

### 助手自动化结果补充（2026-11-19～20）

以下为2026-11-20之前的历史alpha.7自动化记录，当时正式两端保持alpha.7。该批已实际使用shiki完成同步恢复与功能子项、真实attach/保活、配置快照恢复、loopback HTTP转发、浏览器输入/IME/显式剪贴板/暂存文件及双查看器接管。每项实现和现场失败详见08/15/16顶部同名记录。B04的显式2分钟resolve预算仅用于功能核验；B05后端重启后转发未恢复、B06状态/接管超时、收尾会话列表一次5秒超时均保留。同步最终还出现REMOTE_RETRY_EXHAUSTED，显式恢复和数据核验结果见同记录。源码隔离回归通过不能代替候选部署后的默认体验。

上述历史功能命令无需整套重跑。Claude真实账号授权继续暂停，主观长用体验及切网/睡眠/物理重启仍需用户参与。当前alpha.9已完成下述有限浏览器交互、本次人工进入登录页及临时收尾；长期体验、自然切网与Claude账号授权继续待验；首装环境/新节点登录仍有独立前置条件，不以现有安装上的功能测试代替。beta尚不具备本次实机稳定性闭环证据。

## 1a. 本轮之后你需要做什么

**2026-11-22：两端alpha.9升级完成，work为普通browse；真实viewer短时功能和本次人工进入Claude登录页已通过，临时收尾完成。** 用户已授权新建Ego TaskSpace30。自有页面截图确认Count5/alpha9-test输入及SCROLL_END_OK；5次显式重连均回只读后明确接管，最后截图确认reconnect-5输入，中间输入仅有动作receipt。首次误等自动Controller的15秒等待失败属于验收假设错误，实际Connected/Read-only符合设计。原生只读观察webdriver=false、ja-JP、Asia/Tokyo，未伪造。用户反馈并由助手截图确认claude.ai/login正常登录表单，未登录账号。close-viewer已closed，TaskSpace30已完成，10个临时文件共1,485,466逻辑字节已删除；远端Chrome同实例running/browse、地区revision保持，登录页有意保留。下面旧版失败记录保留为背景，本次成功不保证以后不再挑战。

alpha.9当前状态与后续执行顺序：

1. **已完成：** 两端签名alpha.9、7插件ready、原凭据/work profile保留，Chrome158.0.8127.36和x11-utils7.7+5已核对。旧验收目录、项目、映射、两个已停止测试会话及sync副本已按新授权清理，不能再复用旧ID。
2. **已完成：** 在用户确认页面已保存后，work已显式启动为普通浏览，status实际返回running/browse，保持原profile、出口和地区配置。已在新授权工作区打开viewer并明确Take control；无需仅为重复核对模式再次重启。
3. **本轮已完成的范围：** 自有data:页面点击/输入/滚动和5次显式重连后重新接管；Balanced为924/978/1116/890/1006毫秒，Smooth为939/999/1115/988/907毫秒。观测含Ego dispatch和像素轮询开销，非纯RTT，不能严格对比旧版不同夹具。此次没有补记IME或暂存文件通过；查看器接收KiB/s与本地发送buffer也不等于帧呈现延迟。
4. **本次已通过：** 用户人工验证后已进入claude.ai/login，助手截图复核正常登录表单；未输入账号或代做挑战。远端登录页保留供后续用户使用，Claude账号授权须另外决定。若仍循环，停止重复点击，反馈时间、Ray ID或页面错误码及截图；不要发送Cookie、Token、完整HAR或登录资料。不要求此时完成Claude账号登录。是否允许后续账号授权由用户另行决定。
5. 若仍失败，先结合新模式结果与原网络证据再决定下一项诊断；可能需要站点/Cloudflare支持或机房处理出口信誉，不能保证源码可强制放行。原生浏览器诊断需显式`--diagnostics --replace-running --yes`重启，采样后再显式回browse；此操作不会在用户手动挑战过程中偷偷进行。


**历史alpha.8用户反馈（本轮alpha.9已人工进入登录页，不覆盖历史失败或长期边界）：仍卡顿，claude.ai勾选Cloudflare后循环。浏览器体验/网站可用性未通过，暂停反复验证及Claude授权。用户主动关闭远端Chrome导致后续crashed记录，不能算意外崩溃。已只读核查服务、NTP及有限JS/Cookie设置，根因未定；先前功能通过不覆盖本次失败，详见15顶部。**

已通过的功能命令不用整套重跑。旧验收目录、项目、同步映射、两个已停止测试会话及sync副本已于2026-11-22按用户授权清理，不能再复用其ID；确需新的终端/同步测试时创建独立临时项目，保留日常work profile与原Token。当前剩余项目：

1. **先体验浏览器是否仍卡顿。** 在Mac执行下列命令打开work（若没有设置环境变量，先复制export）。使用30分钟，尝试点击、滚动和普通文字输入；出现卡顿时记录本机时间、动作、等待秒数和页面提示。遇到状态unknown先点Refresh status；出现断屏再点Reconnect screen，并重新Take control。不要为测试登录Claude。
2. **正常工作2小时，条件允许再做隔夜测试。** 按第9节采样并保留失败行；“短期恢复”不能替代无丢数据、可恢复和延迟体验评估。默认shell问题与状态超时需要后续修补复验，不需要你反复执行同一失败命令。
3. **网络/睡眠测试继续保留。** 你准备好窗口后再做B08；当前没有替你切网、睡眠或修改网络。既有IP地理库分歧仍未解决，地区语言一致不是出口国家证明。
4. **Claude真实授权继续暂停。** 等你确认出口与账号使用条件后再执行第4节；授权只能由你在VPS远端Chrome完成，本机浏览器只显示远程画面。
5. **物理重启、干净首装和回滚另选窗口。** 不为补测清空当前环境。默认shell、间歇超时、真实账号与长期体验等开放项应先逐项处理，再决定beta发行。

```bash
export PATH="$HOME/.local/bin:$PATH"
export SHIKI_SERVER='srv_5953306eb779a11a396fea38e2893685'
export SHIKI_EVIDENCE="$HOME/Shikigami-Acceptance/alpha10-user-evidence"
mkdir -p -m 700 "$SHIKI_EVIDENCE"
shiki --server "$SHIKI_SERVER" doctor
shiki --server "$SHIKI_SERVER" browser open work --launch --read-only=false
```

只向我反馈现象、时间、错误码和测试结果，不发送Token、密钥环口令或带引导秘密的完整查看器URL。同步最终active/零待处理/零冲突；两个原会话、原三文件和work仍保留，本轮两个临时会话、快照、转发、夹具及浏览器测试文件已清理。

## 2. B01：固定环境并建立最小记录

以下未特别说明的命令在 **Mac终端** 执行。先查看已有服务器标签：

```sh
~/.local/bin/shiki version
~/.local/bin/shiki servers list
export PATH="$HOME/.local/bin:$PATH"
```

当前用户继续使用既有验收项目，以下变量固定到此前创建的服务器和目录，不再生成新的时间戳项目。其他环境须改为自己已核对的ID/名称/路径；如果目录不存在，先查已有映射，不能默认mkdir并重新创建项目：

```sh
export SHIKI_SERVER='srv_5953306eb779a11a396fea38e2893685'
export SHIKI_TEST_NAME='beta-accept-20261118-235852'
export SHIKI_TEST_DIR="$HOME/Shikigami-Acceptance/$SHIKI_TEST_NAME"
export SHIKI_TEST_REMOTE="/home/shiki/projects/$SHIKI_TEST_NAME"
export SHIKI_EVIDENCE="$HOME/Shikigami-Acceptance/${SHIKI_TEST_NAME}-alpha8-evidence"
test -d "$SHIKI_TEST_DIR"
mkdir -p "$SHIKI_EVIDENCE"
chmod 700 "$SHIKI_EVIDENCE"
shiki --server "$SHIKI_SERVER" doctor
shiki --server "$SHIKI_SERVER" projects list
shiki plugins --side local list
shiki --server "$SHIKI_SERVER" plugins --side remote list
shiki --server "$SHIKI_SERVER" browser status work
```

上面的PATH只对本窗口生效，不修改受保护的 `.zshrc`。

2026-11-22已核对客户端、daemon及7插件为正式alpha.9、work running/browse；12次CLI成功只证明短时健康，见[升级证据](15-implementation-status.md#evidence-alpha9-upgrade-20261122)。以下历史2026-11-21的alpha.8功能记录仍保留原版本：实际项目请求成功；本机sync/port/browser和远端sync/port/region/browser ready。`doctor` 即使退出码为0也可能包含degraded/errors，必须阅读字段。Claude尚未登录允许记录pending，授权步骤仍暂停。`tailscale-relay`本身不是失败，不要求为了测试强制direct或改路由。

`SHIKI_TEST_REMOTE`使用默认专用账号的项目根目录；若安装配置使用不同 `projects_dir`，由我确认后替换，不去放宽服务权限。每个新Mac终端需重新设定变量；优先保持此窗口用于管理命令，另开终端用于交互会话。

只记录版本、时间、测试编号、是否通过及错误码。不要把设备Token、SSH密码、授权URL、查看器带`#`的完整URL、Cookie或整个配置目录贴进反馈。

## 3. B02：建立独立项目并验证终端保活

alpha.7历史回传与助手自动化已验证detach/attach前后PID及临时环境标记保留；使用现有项目/terminal-check，无须重跑下方创建命令。同步映射已完成恢复，最后记录为active且无冲突；下一轮先读取实际状态，不按旧paused状态执行初始同步。

2026-11-19用户反馈：B01基线正常；B02的用户/路径/文件读写通过，但方向键在`/bin/sh`显示转义序列，交互编辑未通过。源码的地区环境包装存在空argv显式选择`/bin/sh`路径。临时对照：在当前远端shell先Ctrl+C清除输入，再执行`exec /bin/bash --noprofile --norc -i`，检查上下历史及左右编辑。用户随后确认切换后方向键均正常；此操作只替换当前测试shell，不能把绕过后的通过记作默认shell缺陷已修复。

仅首次、尚无验收项目/会话的独立环境执行以下创建命令；当前用户跳过这两条，直接进入下面的attach：

```sh
shiki --server "$SHIKI_SERVER" projects create "$SHIKI_TEST_NAME" \
  --path "$SHIKI_TEST_REMOTE" --map "$SHIKI_TEST_DIR"
shiki --server "$SHIKI_SERVER" --project "$SHIKI_TEST_NAME" sessions create terminal-check --no-claude
```

在Mac管理窗口附着既有会话：

```sh
shiki --server "$SHIKI_SERVER" --project "$SHIKI_TEST_NAME" attach terminal-check
```

最后一条进入 **VPS普通用户shell**。当前用户只读取已保留的验收文件；首次独立环境应先创建自己的测试文件：

```sh
id -un
pwd
cat terminal-check.txt
```

当前用户预期为`shiki`、既有测试项目路径，文件内容`SHIKI_TERMINAL_OK`；不覆盖该历史验收文件。输入中文/英文、移动光标、扩大/缩小Mac终端窗口，确认不乱码或重复按键。按 **Ctrl+Q** 回到Mac；再次运行上面的attach命令，远程执行：

```sh
cat terminal-check.txt
```

预期文件内容保持相同，仍是同一个session。不要用`exit`代替detach测试。再Ctrl+Q回Mac：

```sh
shiki --server "$SHIKI_SERVER" --project "$SHIKI_TEST_NAME" sessions list
cd "$SHIKI_TEST_DIR"
shiki
```

智能入口应识别映射/已有项目并允许选择会话，不创建第二个项目。仅首次独立环境的同步提示先保持暂停；当前用户保持已有映射状态，B04另行安排隔离测试文件。若命令超时，先`projects list`或`sessions list`查是否已经创建，禁止连续重复创建来“碰运气”。

可选双窗口控制：另一Mac终端复制实际服务器/项目名，运行`shiki --server 标签 --project 项目名 attach terminal-check --read-only`；只能看不能输入。使用显式`--take-control`的新attachment验证接管，旧窗口应失去写权限。只测试这个临时会话。

配置快照和诊断的补充冒烟可在Mac执行：

```sh
shiki --server "$SHIKI_SERVER" --project "$SHIKI_TEST_NAME" sessions snapshot terminal-check --description beta-test
shiki --server "$SHIKI_SERVER" --project "$SHIKI_TEST_NAME" sessions snapshots
shiki --server "$SHIKI_SERVER" --project "$SHIKI_TEST_NAME" sessions restore '刚返回的snapshot-id' --name snapshot-check
shiki --server "$SHIKI_SERVER" stats
shiki --server "$SHIKI_SERVER" dashboard
```

确认restore提示后只创建新的测试会话；它恢复启动配置，不恢复文件或Claude对话。stats应展示实际值或明确unavailable，dashboard不能把失败插件显示为ready。模板、清理策略、权限和其余管理命令的完整矩阵由B12自动化回归覆盖，无需你逐条重复CI。

## 4. B03：由你完成 Claude Code 授权和真实工作

继续attach到`terminal-check`。在远程shell确认：

```sh
command -v claude
claude --version
claude auth status --json
```

如果PATH没有找到，但使用默认quickstart安装，可用`/home/shiki/.local/bin/claude`替代下列`claude`。不要在root用户下登录。

未登录时执行：

```sh
claude auth login
```

按官方CLI提示，由你在浏览器授权。需要远程Chrome时，先Ctrl+Q脱离会话，在Mac运行第7节的`browser open`；在远程Chrome中打开CLI显示的官方授权链接。若流程要求回贴授权码，只贴回你自己的远程终端。不要发给我。网页Claude账号登录并不自动证明Claude Code已获授权。

再次attach到同一shell：

```sh
claude auth status --json
```

应报告已登录。Ctrl+Q回Mac，再创建真实Claude会话：

```sh
shiki --server "$SHIKI_SERVER" --project "$SHIKI_TEST_NAME" sessions create claude-check
shiki --server "$SHIKI_SERVER" --project "$SHIKI_TEST_NAME" attach claude-check
```

在Claude输入：

> 只在当前测试目录中新建 beta-check.txt，内容为 SHIKI_BETA_OK。不要读取或修改其他目录，不提交Git。完成后说明文件路径。

按正常提示审阅并允许这项测试文件写入。随后Ctrl+Q、重新attach，确认当前进程的会话仍可继续。让Claude读取刚才文件，内容应一致。真实请求可能消耗你的账号额度；账单/账号资格问题单独记录，不归为Shikigami通过或失败。

回Mac执行`shiki --server "$SHIKI_SERVER" doctor`。向我反馈“已登录/未登录、任务成功/失败、detach后恢复情况”即可，不发送完整账号JSON。若网页登录成功但CLI仍未登录，记录为B03未完成。

## 5. B04：sync双向、冲突与删除

2026-11-19恢复前现场（历史）：固定请求beta-keep-both-v5-01超时后仅本地副本已落盘，原冲突和副本差异均保留，B04当时不通过。助手随后已对确认保存REMOTE_CONFLICT_V5的本地副本显式keep-local，核对两端后再以最新generation处理原文件；两端双版本均保留，冲突已清空，见本页自动化结果与15的记录。无需用户重复恢复步骤。alpha.7现场恢复使用显式预算。2026-11-21另行完成alpha.8默认调用路径6次成功及字节核对；这些耗时均小于5秒，不扩大为跨旧截止/长传输或部分提交故障注入通过。网络专项仍暂跳，Claude授权继续暂停。

本轮反馈：首次keep-remote连续三次前台超时；助手随后只读确认active、无冲突且本地两文件正确，后台共同基线已建立，持久记录保留结果未知失败。不要重复旧resolve；先核验VPS两文件，再继续已有基线更新。数据收敛不等于resolve交互通过，详见15的用户终端保活/同步记录。

先Ctrl+Q回Mac管理窗口：

```sh
cd "$SHIKI_TEST_DIR"
printf 'from-mac-v1\n' > from-mac.txt
shiki --server "$SHIKI_SERVER" sync status
shiki --server "$SHIKI_SERVER" sync resume
shiki --server "$SHIKI_SERVER" sync conflicts
```

新映射没有共同基线，初始差异显示冲突是预期行为。对每个测试文件复制其`id`与`generation`，逐项审阅：

```sh
shiki --server "$SHIKI_SERVER" sync show-diff '实际conflict-id' --generation '实际generation'
shiki --server "$SHIKI_SERVER" sync resolve '实际conflict-id' \
  --generation '实际generation' --choice keep-local
```

`from-mac.txt`选keep-local；远程创建的`terminal-check.txt`、`beta-check.txt`选keep-remote。按提示确认。每次使用最新generation，不能套用同一个ID/版本批量覆盖。`show-diff`显示差异时可能返回非零，不能单凭该退出码判同步失败。

检查`sync status`与`sync conflicts`直到无未处理项。预期两端有相同文件：Mac用`cat`，远程attach后用`cat`；需要摘要时Mac用`shasum -a 256 文件`、VPS用`sha256sum 文件`。

按以下顺序测试，所有文件只放本次测试目录：

| 子项 | 动作 | 预期 |
|---|---|---|
| 已有基线的本地修改 | Mac `printf 'from-mac-v2\n' > from-mac.txt` | 等实际同步完成，VPS内容相同 |
| 远程新增 | 在远程测试shell `printf 'from-vps\n' > from-vps.txt` | Mac收到同名同内容；如产生需确认项，按状态处理 |
| 暂停 | Mac `shiki --server "$SHIKI_SERVER" sync pause`，修改from-mac.txt | 暂停期间远端不跟随；resume后协调，按实际冲突处理 |
| 双端冲突 | pause后，Mac把from-mac.txt写为LOCAL；VPS同文件写为REMOTE；再resume | 两边不同内容不被静默覆盖，出现冲突 |
| 保留两份 | 对上述最新冲突执行resolve，`--choice keep-both` | 两份内容都能找回，不要求人为猜测备份文件名 |
| 重命名 | 基线稳定后，Mac把from-vps.txt改名为renamed.txt | 等状态稳定后两端名称/内容一致；不宣称协议具有原子rename语义 |
| 删除 | 新建delete-me.txt，等两端均有基线后，只在Mac删除它 | 远端按已建立基线删除；若远端被修改，则应冲突而非覆盖 |

报告冲突ID、generation、选择和结果，不把整份项目内容发给我。B04未通过时先暂停本测试映射，保留现场，勿将真实agent-remote加入测试。

## 6. B05：port真实服务转发

本例使用远端3000。先由我核对已有allowlist和端口占用；如果未允许，需保留完整既有配置，仅审阅后增加测试端口。`plugins config`替换完整配置，**不能用只有allow_ports的JSON覆盖不明现有设置**。ready不是已允许端口的证明。

在Mac新建专用远程shell：

```sh
shiki --server "$SHIKI_SERVER" --project "$SHIKI_TEST_NAME" sessions create port-check --no-claude
shiki --server "$SHIKI_SERVER" --project "$SHIKI_TEST_NAME" attach port-check
```

在该远程shell运行（若3000占用则停止本项并换已审阅的空闲端口，不结束其他进程）：

```sh
mkdir -p port-web
printf 'SHIKI_PORT_OK\n' > port-web/index.html
python3 -m http.server 3000 --bind 127.0.0.1 --directory port-web
```

Ctrl+Q回Mac，服务应继续运行：

```sh
shiki --server "$SHIKI_SERVER" ports forward 3000 --local-port 0 --bind-address 127.0.0.1
shiki --server "$SHIKI_SERVER" ports list
```

从输出复制本次forward ID与实际本地端口；显式0由系统选空闲端口，避免占用现有本地3000。Mac执行：

```sh
curl --fail --max-time 10 http://127.0.0.1:实际本地端口/
```

应返回`SHIKI_PORT_OK`。用本机浏览器打开同一URL也应成功。若返回`not allowlisted`，记录配置前提未满足，交由我处理，不关鉴权、不公开VPS端口。

再次attach到port-check，按Ctrl+C结束本次Python HTTP服务；Mac请求应失败，不能仍显示旧响应作为成功。远端重启同一HTTP命令后，新的Mac HTTP请求应恢复；既有TCP连接不保证保活。最后Mac运行`shiki ports stop '本次forward-id'`，确认本次转发停止；再在port-check按Ctrl+C结束本次服务。

## 7. B06：浏览器、输入法、剪贴板与文件

先看`browser status work`；如果确实stopped且你准备使用，再执行`browser start work --yes`。不要替换另一个正在运行的profile。Mac打开可申请控制的查看器：

```sh
shiki --server "$SHIKI_SERVER" browser open work --read-only=false --launch
```

打开后仍需 **Take control → 明确确认 → Controller**。记录viewer ID，但不分享带凭据的URL。用新测试标签，保留原有标签。

| 次序 | 操作 | 通过标准 |
|---|---|---|
| 1 | 鼠标点击链接、选中文字、滚轮上下滚动、英文输入和退格 | 反馈正常，没有重复/丢失到无法使用的按键 |
| 2 | 本机键盘切英文布局，使用远程Fcitx切换中文/日文，在文本框输入“中文测试”“日本語テスト” | 远程候选框可见、能选择并上屏；本机输入法粘贴不算此项通过 |
| 3 | 查看器Explicit clipboard中手动填测试文本，点击Send；在远程文本框粘贴 | 只有明确Send才传输，内容相同 |
| 4 | 在远端复制测试文本，在面板明确Copy remote text to local clipboard | 本机粘贴内容相同；浏览器权限拒绝时单独记，不假装成功 |
| 5 | 连续5次Reconnect screen；每次Connected后确认read-only，再Take control | 每次能恢复，输入不重放、不会自动成为Controller |
| 6 | 第二个查看器先用默认只读打开；关闭它后，再用`--read-only=false`新开第二个查看器并Take control | 强制只读查看器不能申请控制；允许控制的新查看器明确接管后，第一个失去控制，只能有一个Controller |
| 7 | Close viewer；重新执行open | Chrome实例及已有页面仍在，不因关闭本地查看器结束 |
| 8 | 连续使用30分钟，至少10次导航/滚动/输入 | 记录每次冻结、断线、恢复方式及用时，不以最后一次成功覆盖失败 |

alpha.7受管默认快捷键：**Ctrl+Space**开关服务端输入法，**Ctrl+Shift+Space**在中文拼音/日文Mozc之间切换。先点击远程文本框，再用英文键盘输入拼音或罗马字，观察远端候选框。若Mac拦截快捷键或实际受管配置不同，记录现象由我核对，不随意改本机设置。网站本身打不开与远程画面断开分别记录。

浏览器文件通道先做不涉及网站的往返：

```sh
printf 'browser-file-roundtrip\n' > "$SHIKI_EVIDENCE/browser-upload.txt"
shiki --server "$SHIKI_SERVER" browser files upload work "$SHIKI_EVIDENCE/browser-upload.txt" --yes
shiki --server "$SHIKI_SERVER" browser files list work --area uploads
shiki --server "$SHIKI_SERVER" browser files download work browser-upload.txt \
  --area uploads --output "$SHIKI_EVIDENCE/browser-returned.txt" --yes
cmp "$SHIKI_EVIDENCE/browser-upload.txt" "$SHIKI_EVIDENCE/browser-returned.txt"
```

`cmp`无输出且退出0才通过。若同名已存在，选新的测试文件名，勿直接覆盖。此项证明上传暂存与取回，不等于网页上传/下载完整通过。另在你选择的网站手动选取本测试文件上传，下载一个非敏感小文件，再用`browser files list work --area downloads`和download取回并对比；不要以网站上传为名传输真实项目或凭据。

Cookie持久化安排在你已保存网页内容后：由你手动登录一个允许测试的网站，记录登录状态；Close viewer再打开应仍保持。随后显式stop/start work，再open检查。若网站要求重新认证，记录网站/时间与Chrome状态，由我区分站点策略、Cookie或密钥环问题；不承诺所有网站永不掉登录。停止Chrome会丢失网页未提交内容，先保存。

## 8. B07：region检查，不改整机网络

2026-11-19额外实测/授权：用户明确批准IPinfo/ipapi.co出口核对及日本系统时区/语言应用。主机和实际work Chrome观察到同一出口IPv4，但两库分别标JP与US；分歧未定。系统已复查为Asia/Tokyo、ja_JP.UTF-8，原Chrome仍是东京/日语；DNS、路由和运行应用保持。旧shell环境不自动刷新，账号授权继续暂停。下面保留一般测试流程，不据这次配置成功宣称Claude登录或IP地区判断已通过。

Mac执行：

```sh
shiki --server "$SHIKI_SERVER" region inspect
shiki --server "$SHIKI_SERVER" browser profile list
shiki --server "$SHIKI_SERVER" browser diagnose work
cat > "$SHIKI_EVIDENCE/jp-plan.json" <<'JSON'
{"country":"JP","timezone":"Asia/Tokyo","locale":"ja_JP.UTF-8","languages":["ja_JP","ja"]}
JSON
shiki --server "$SHIKI_SERVER" region plan --file "$SHIKI_EVIDENCE/jp-plan.json"
```

检查helper可用、当前值、revision和pending_operation_id；计划应列出变化，但不应用。work的JP浏览器设置与整机设置可以不同，不能因浏览器日文就推定VPS整机已切JP。诊断默认不访问外部供应商，缺失观察可为unknown，不全部强求pass。

整机apply/confirm/rollback、DNS超时恢复与冲突保护由我在隔离机补验。若要在当前VPS实测，另外约定目标配置、带外恢复和窗口；本流程不执行apply、DNS/代理改动或外部检测授权。

## 9. B08–B09：断线恢复与持续使用

先运行B02测试shell和浏览器。仅在没有重要传输/未保存操作时，由你选择做以下测试；它们会短暂影响Mac上的其他联网应用。

1. 关闭本地查看器再打开：远程Chrome持续运行。
2. Mac手动断网约30秒后恢复：记录终端自动恢复结果；浏览器必要时点Reconnect screen，再明确接管。
3. Mac睡眠5分钟后唤醒：检查原会话、同一Chrome实例、文件同步状态和转发新连接。
4. 有手机热点时再测试一次自然切网；没有则记“未执行”，不用安装VPN或改服务器网络来制造路线。

每次恢复后，在Mac管理终端运行：

```sh
shiki --server "$SHIKI_SERVER" projects list
shiki --server "$SHIKI_SERVER" --project "$SHIKI_TEST_NAME" sessions list
shiki --server "$SHIKI_SERVER" browser status work
shiki --server "$SHIKI_SERVER" ports list
cd "$SHIKI_TEST_DIR"
shiki --server "$SHIKI_SERVER" sync status
```

若明确`RECONNECT_EXHAUSTED`，网络恢复后执行`shiki connect "$SHIKI_SERVER"`复用已保存连接。身份拒绝时停下来核对；不要签发新Token或重登节点来绕过。连接恢复后不得出现重复终端输入、自动接管、文件静默覆盖或新daemon身份。

下面是一段**只读采样**，Mac复制整段执行。项目正文直接丢弃，只记录时间/耗时/退出码及有限错误分类，不自动connect、不改超时、不重试原请求。环境中的`SHIKI_SERVER`与`SHIKI_EVIDENCE`须已设置，Mac需要python3。

```sh
export SHIKI_SAMPLES=60
export SHIKI_INTERVAL=10
python3 - <<'PY'
import csv, datetime, os, pathlib, subprocess, time, statistics
server = os.environ['SHIKI_SERVER']
folder = pathlib.Path(os.environ['SHIKI_EVIDENCE'])
count = int(os.environ.get('SHIKI_SAMPLES', '60'))
interval = float(os.environ.get('SHIKI_INTERVAL', '10'))
assert 1 <= count <= 10000 and 1 <= interval <= 3600
path = folder / ('requests-' + datetime.datetime.now().strftime('%Y%m%d-%H%M%S-%f') + '.csv')
os.umask(0o077)
durations, failures = [], 0
with path.open('x', newline='') as f:
    writer = csv.writer(f)
    writer.writerow(['utc', 'sample', 'milliseconds', 'exit_code', 'result'])
    try:
        for number in range(1, count + 1):
            stamp = datetime.datetime.now(datetime.timezone.utc).isoformat()
            begin = time.monotonic()
            try:
                result = subprocess.run(
                    [str(pathlib.Path.home()/'.local/bin/shiki'), '--server', server,
                     '--json', 'projects', 'list'], stdout=subprocess.DEVNULL,
                    stderr=subprocess.PIPE, text=True, timeout=30)
                code = result.returncode
                reason = 'ok' if code == 0 else 'other_error'
                if code:
                    for token in ('IDENTITY_VERIFICATION_DENIED', 'RECONNECT_EXHAUSTED',
                                  'deadline_exceeded', 'unavailable', 'unauthenticated',
                                  'permission_denied'):
                        if token.lower() in result.stderr.lower():
                            reason = token
                            break
            except subprocess.TimeoutExpired:
                code, reason = 124, 'sampler_watchdog_30s'
            elapsed = round((time.monotonic() - begin) * 1000)
            durations.append(elapsed)
            failures += code != 0
            writer.writerow([stamp, number, elapsed, code, reason])
            f.flush()
            print(f'{number}/{count} {elapsed}ms {reason}', flush=True)
            if number < count:
                time.sleep(interval)
    except KeyboardInterrupt:
        print('Interrupted; partial samples retained, not a completed run.')
print('CSV:', path)
if durations:
    print('completed_samples=', len(durations), 'failures=', failures,
          'median_ms=', statistics.median(durations), 'max_ms=', max(durations))
PY
```

默认约10–15分钟，实际取决于请求耗时；隔夜如需约12小时，设`SHIKI_SAMPLES=720`、`SHIKI_INTERVAL=60`后重跑同一段Python。不要同时开多个采样器。CLI默认5秒是每个短IPC调用预算，整条命令可含多次调用；脚本30秒只是外层卡死保护，`sampler_watchdog_30s`必须另计，不冒充产品deadline错误。采样通过也不证明浏览器流或同步通过。

先保持原网络完成一轮；切网/睡眠测试另起一轮并记录事件时间，预期人为断网期间失败不能混成正常在线失败率。日常工作2小时期间体验Claude、同步、浏览器与端口；隔夜主要看空闲恢复及后台存活，不伪称12小时持续负载。

本轮目标是没有未解释的产品错误、数据损失、权限绕过或不可恢复故障。不是自创延迟SLA。任何超时都保留；我需结合当时agent/daemon/plugin状态、CPU/内存和SSH/网络对照归因。再次出现alpha.7的偶发超时则优先调查，不能仅把5秒调大后宣布修复。若确认多步文件操作误用短查询预算，应同时验证新预算、较早父截止、部分提交恢复和真实交互；延长预算不替代这些验收。

## 10. B10：干净首装、重复向导与中断恢复

我在隔离Linux系统和独立Mac测试环境执行，不卸载当前两端。Mac另一个用户可覆盖新用户安装，但不能称为整机全新环境；整机首装需要独立VM/设备。测试范围：

1. 使用已公开验签的[28](28-quickstart.md)官方 `v0.0.1-beta.1` 两条命令，在独立无Shikigami状态环境安装；正确签名通过、错误签名拒绝在隔离测试完成。现有用户两端不为此清空重装。
2. 记录y/yes/n/no、隐藏输入与Enter/Ctrl+C、每10秒等待提示、纯端口误填原地纠正。
3. 无Claude授权也能交付连接并打开浏览器，最终应用状态仍pending。
4. 授权阶段正常取消后重跑；已完成步骤复用，既有keyring、地址与Token不重建。
5. 再执行同版向导，端口留空保留、已有profile/配置保留；取消不是卸载。
6. 测试结束只卸载隔离环境本次安装，记录正式安装和数据保留边界。

需要你做的只有新测试节点的Tailscale人工授权，以及可选的全新Mac向导体验。测试使用独立身份，不撤销当前正常节点。生产APT过程中强杀、损坏密钥环、撤销真实Token等故障注入由我在隔离夹具做。

## 11. B11：可选实际重启，独立维护窗口

这不是让你现在重启。先保存工作、结束测试HTTP进程、暂停本测试同步，确认VPS提供商控制台可以登录；Mac本机重启会中断当前协作。先VPS、后Mac，不能同时重启后再排障。没有安排窗口则保持未实测，不伪装通过。

VPS重启前，如果你要验证Chrome自动启动，在Mac明确选择work：

```sh
shiki --server "$SHIKI_SERVER" browser profile list
shiki --server "$SHIKI_SERVER" browser profile boot work --yes
```

如果已有其他boot目标，先停下核对，不直接追加`--replace-boot`。记录原boot选择以便恢复；本次改变是持久设置。未选择boot目标时，重启后Chrome不自动运行是预期，不应判失败。

在VPS root终端记录状态，然后由你确认窗口后执行最后一条：

```sh
systemctl show shiki-daemon.service -p ActiveState -p SubState -p NRestarts
systemctl is-active shikigami-browser-keyring-1000.service shikigami-region-guard.service
systemctl reboot
```

恢复SSH后再次检查这些服务，Mac检查projects list、profile list、browser status和doctor。预期服务普通UID、节点身份与配置保持，keyring自动解锁；已选择boot且插件有效时只启动一个受管Chrome。不要求VPS重启后旧tmux/Claude进程仍活着：主机重启会结束进程，不能把服务重启保活承诺扩大到跨主机重启或对话恢复。

Chrome自动启动验证后显式stop，观察同一次开机不自动复活；再由你显式start恢复使用。若原来未设boot目标，测试后用`browser profile boot work --enabled=false --yes`恢复；原有其他目标按原记录恢复，不猜测。

Mac由你通过苹果菜单重启并登录，再运行`shiki agent status`、doctor与projects list。验证的是登录后的LaunchAgent恢复，不是未登录时后台已启动。失败时我取日志定位，不清空身份或重装来掩盖问题。

## 12. 我负责的B12与beta发布检查

| 工作 | 执行方法/必须留下的证据 |
|---|---|
| 连接故障分析 | 对齐B09时间戳，记录原超时、route、服务状态和相邻恢复；复现后先红后绿回归，保留默认调用预算 |
| 四插件及主功能回归 | 单元/race、真实CLI→agent→daemon→worker、协议/权限/能力、取消、冲突/重复请求、终端控制与浏览器重连 |
| 高风险故障注入 | 隔离夹具覆盖认证拒绝/撤销、插件崩溃、磁盘满/中断、同步恢复、region DNS guard；不污染用户身份和项目 |
| 签名marketplace | 受信/不受信包、更新/失败回滚、重启恢复、卸载；公开第三方目录/发行者互操作没有环境则单独列未验 |
| 原生平台矩阵 | 最终源码CI57项、Linux/macOS/WSL2已有必需验收；新增浏览器ARM/Linux/WSL2完整交互需真实环境，交叉编译不代替 |
| 公开包迁移 | alpha.7→alpha.8签名升级和配置/凭据/profile保留已验；本批回滚未执行。以后进入beta再固定来源版本及回滚范围 |
| 支持声明与文档 | 08/15/16同步，首装/升级/账号/物理重启/实际平台分别登记，历史42/42不冒充新插件全验 |
| 发布 | 冻结最终提交，CI通过后不可变beta.1 tag；同源构建/签名/23资产/公开Release/Homebrew均成功；四目标独立验签与公开包验收 |

本手册记录已执行测试和后续计划；alpha.10已公开发行，不因“争取发布beta”直接创建beta tag。真实账号、重启及新增平台边界可保留为明确未实测，但不能把选择暂不验证写成通过。是否进入发布，应依据本轮发现、明确支持范围与最终验收记录，不是简单凑够测试数量。数据损坏、越权、错误身份接受、明文凭据泄露和正常流程不可恢复是停止发布的缺陷。

## 13. 给我的反馈与测试后的清理

复制这个模板即可，不必写长报告：

```text
版本：填写本轮实际客户端/daemon/插件版本（当前两端已为alpha.10；反馈时仍以实际查询为准）
时间与时区：
B01 基线：通过/失败/未测
B02 终端与detach：
B03 Claude：CLI已登录？任务完成？重新attach正常？
B04 sync：双向/暂停/冲突/保留两份/删除分别如何？
B05 port：实际HTTP内容/停止/重新请求是否正确？
B06 browser：中文/日文/剪贴板/文件/5次重连/30分钟使用结果
B07 region：helper状态、是否有pending（不贴秘密）
B08 恢复：断网/睡眠/切网动作与时间、恢复方式和秒数
B09 采样：总数/失败数、CSV路径
B10/B11：未执行或实际环境及结果
失败操作：
错误码/错误原文（先移除Token、授权URL、账号信息）：
是否影响其他网站/SSH：
```

我最需要的是**第一次失败的时间、精确操作、错误码、恢复动作和CSV**。只发“又断了”不足以区分查看器、插件、agent、daemon或链路；也不要为了重现反复粘贴Token。日志优先由我按失败时间截取必要片段，完整doctor/配置/系统日志不作为默认公开附件。

清理先保留失败现场，等结论记录后再执行。仅针对本次名称/ID：关闭测试viewer、停止本次HTTP与forward；暂停并解除测试映射；删除本次session和project记录（项目文件仍保留）；核对后删除测试目录/暂存文件。work profile、密钥环、真实项目和agent-remote不删除。测试期间设定的boot/端口配置按记录恢复；不使用`rm -rf ~/.shiki`、全局`pkill`或批量清空浏览器数据。

```sh
# Mac：确认位于本测试目录，且冲突/传输已处理完再解除映射
cd "$SHIKI_TEST_DIR"
shiki --server "$SHIKI_SERVER" sync pause
shiki --server "$SHIKI_SERVER" sync unmap
# session、forward、snapshot等按实际创建的ID逐个清理，由我核对清单。
```

若unmap报告pending，先处理或保存证据，不默认使用`--archive-pending`绕过。最终报告分别列已通过、失败已修复、未执行和不适用；正式版本与安装备份继续保留。
