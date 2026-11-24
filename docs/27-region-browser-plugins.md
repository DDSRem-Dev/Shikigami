# 地区环境与远程浏览器插件

2026-11-24更新：用户在已部署正式签名 `v0.0.1-alpha.10` 环境确认默认 `Smooth` 远程浏览器日常使用体验正常（`B06`）、断网/睡眠恢复与持续使用正常（`B08/B09`），并通过远程 `work` Chrome（`browse` 模式）人工完成官方 `claude auth login`（`loggedIn=true`）与受管会话实际文件任务（`B03`）；首个公开 Beta [v0.0.1-beta.1](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-beta.1) 随后已发布并通过 23 资产四目标验签与升级/渠道验证，见[发行记录](15-implementation-status.md#evidence-public-beta1-release-20261124)。物理主机重启（`B11`）与全新双机首装（`B10`）仍保留未实测边界。

2026-11-21历史更新：用户VPS与Mac已正式升级alpha.8，浏览器输入、5次显式重连、接管与暂存文件功能复验通过；一次状态查询超时经手动Refresh恢复，临时转发后续端口重绑定原因未定。长稳/网络专项、Claude账号及物理重启继续开放，见[两端升级与实测](15-implementation-status.md#evidence-alpha8-upgrade-20261121)。以下早期验收保留原日期与源码范围。

2026-11-16 后续一键体验：管理员 setup 与客户端插件配置由 [28 首次使用向导](28-quickstart.md)组合，保留本文系统授权、密钥环、实例归属和显式应用边界。该入口已随 alpha.4 发布；真实新向导双机首装、账号和物理重启仍未实测，见[发行证据](15-implementation-status.md#evidence-public-alpha4-release-20261116)。下方细分命令继续用于高级操作，alpha.3 历史证据保持原范围。

历史状态：2026-11-15用户批准的实现与授权范围内本地/隔离amd64容器验收已完成，并随v0.0.1-alpha.3公开发行；精确源码、产物及公开验签证据见08/15/16。本文仍是行为基线，不代表全平台通过。原Phase0–5的42项不包含本批；新增原生体验、真实账号、物理重启、公网及公开包完整升级/回滚仍按下方边界保留。

## 已确认产品契约

- 单用户 VPS：Ubuntu24.04/Debian12，amd64/arm64，systemd。客户端沿用macOS/Linux/WSL2；不承诺原生Windows/移动端或无agent客户端。
- 新插件ID为region、browser。独立可选启用，不改变sync/port的既有功能。生产业务在plugins/region、plugins/browser；SDK在pkg/plugin；宿主在internal/plugin，不建立host到插件业务的import。
- region修改整机IANA时区、UTF-8 locale、语言和区域格式。首批JP/US；JP建议Asia/Tokyo/ja_JP.UTF-8；US必须明确时区/语言。选择国家不是身份、居住地或服务资格证明。
- 系统真实时钟/NTP、VPN/代理出口、路由不由本批修改。用户自行准备出口。DNS是独立显式授权的扩展，用户指定系统解析器和Chrome安全DNS；不得按国家暗选供应商。
- 普通用户daemon/插件/Chrome保持不变。管理员一次安装并预授权root拥有的受限helper，普通用户不可替换；不接受任意命令、路径、下载源或可执行文件。授权意味着同UID程序也可能使用该白名单权限，不是插件沙箱。
- 设置先检查/计划，再显式确认应用。版本竞争拒绝。保存旧配置，报告实际生效和待重启；不自动重启现有Claude、tmux、Chrome或第三方服务。
- DNS由脱离客户端连接的恢复守卫保护；应用后新解析与连接检查，通过后用户确认；超时尝试恢复，恢复失败真实报告。不能保证所有失联都能自动救回。包安装不能承诺完整事务回滚。
- Chrome官方稳定版，有界面、保留sandbox。虚拟显示+VNC+noVNC，本机浏览器仅承载画面与输入；不提供完整桌面、音视频、摄像头或安全密钥转发。
- 多持久profile，同机同时只允许一个运行、最多一个显式开机目标；切换要确认停止旧实例。新profile默认不开机启动。禁用插件停止所有受管实例但保留数据。
- 独立systemd用户服务持有运行实例；客户端断线、agent/daemon/plugin重启不结束Chrome。主机开机只有已校验并启用的插件可以启动指定profile。Chrome正常退出（exit 0）标stopped，非零/信号退出及必要运行依赖异常退出标crashed；需人工重启，不自动循环拉起。
- 一个控制者，其余只读；显式版本化接管，断线不自动提升其他人。输入/resize/剪贴板的权限在服务端实施。
- 服务端中文/日文输入法，远程显示候选框，本机英文键盘传递按键。显式剪贴板；独立授权的上传和下载取回。文件通道只访问profile暂存区，但Chrome同UID文件选择框不是整机文件沙箱。
- 保留Cookie，不提供密码保存/Chrome同步。用户追加确认采用整机Chrome托管策略：管理员安装`/etc/opt/chrome/policies/managed/shikigami-browser.json`，设置`PasswordManagerEnabled=false`、`SyncDisabled=true`、`BrowserSignin=0`；安装计划必须说明影响同机所有官方Chrome，已有冲突策略不得覆盖。网站账号登录不受此浏览器账号登录策略禁止。profile偏好只是补充，不能冒充不可修改的限制。密钥环问题真实报告，不静默使用明文密码存储。profile不进入项目同步、日志、普通诊断和配置快照。持久profile不保证登录永久有效或恢复未保存内容。
- 用户追加确认无人值守开机解锁：管理员显式配置systemd加密凭据与解锁服务，运行时仍以普通用户使用加密密钥环；解锁口令仅经stdin传递，不进入命令参数、日志、配置快照或诊断。不得重置已有密钥环。无TPM时允许本机root密钥保护的凭据，必须披露无法抵御整盘与root密钥一并失窃。解锁失败明确阻止Chrome启动，不静默退回明文存储。
- Claude Code由用户通过远程Chrome人工完成官方授权，不复制Cookie、不抓取密码、不承诺自动回调集成。
- Chrome/系统依赖首次安装与维护更新展示清单和来源，管理员确认。不静默重启、不关闭系统既有更新策略。

alpha.9把新启动分为普通浏览 `browse`（默认，不建立原生Chrome诊断通道）和诊断 `diagnostic`（显式 `browser start --diagnostics`，专用私有调试管道，不开放公网调试端口）。诊断模式可能影响网站兼容性，普通浏览不承诺通过任何网站风控。模式属于本次runtime，不写入profile偏好；新开机目标默认普通浏览。相同profile且相同模式复用现有实例；切换模式或profile必须 `--replace-running` 并显式确认停止旧实例。旧 `<instance>.json` launch record 无mode字段时按其真实历史行为解释为diagnostic；新 `<instance>.launch.json` 必须显式包含有效mode，字段丢失不能回退为旧行为。不静默改为普通浏览或重启。状态返回实际模式；普通浏览实例调用原生诊断明确返回FailedPrecondition。该变化已随alpha.9发布，alpha.8不含此功能；两端已升级正式签名alpha.9，7插件ready、work实际为running/browse，Chrome158.0.8127.36与x11-utils7.7+5已核对，原profile/密钥环保留，见[升级证据](15-implementation-status.md#evidence-alpha9-upgrade-20261122)。新版浏览器交互与公网兼容性仍需单独验收。

模式权威保存在私有、不可变的launch record中。`profiles.json` 的Runtime磁盘副本保留alpha.8字段形状，不写入新增mode；内存实例、保存失败回滚及去重结果保留实际模式。插件初始化先验证profile/instance ID与固定launch路径，再安全读取文件，完整校验其余Spec字段及Region一致后恢复mode。缺失、损坏、权限异常或身份不匹配均拒绝，即使实例已停止也不能猜测legacy。旧字段形状严格解码、改写后再次升级的本地测试不等于已经执行正式alpha.8服务回滚；公开包/用户环境升级回滚仍需独立验收。

## 诊断与证据边界

官方披露IP/地区、设备、OS/浏览器、网络/ISP、时区、Cookie及使用信息，但未公开完整风控项、权重或阈值。产品验收目标是可观测一致性，不是保证规避风控。不得伪造Canvas/WebGL/TLS/设备身份，不修补Claude，不改变用户既有遥测选择。

主机、指定远程Chrome profile、外部观察点是三个不同vantage。不得用本机noVNC页面或daemon出口冒充远程Chrome。结果包含check/state/value/expected/source/vantage/observed_at/limitation；unknown不等于pass。外部检测默认关闭，可逐项显式授权自建或公开HTTPS服务，披露供应商和外发字段，不暗中回退，不提交账号/Cookie/Token，不上传综合指纹或分享报告。差异只警告，不终止现有进程。

DNS配置、浏览器Secure DNS、权威DNS观察到的递归出口、HTTPS出口分别报告；公共DNS anycast/注册国不能作为固定出口国家。IP地理库、信誉数据库、众包封禁记录和社区启发式分数不是Claude认可或封禁概率。不提供自创防封分数。

原生Chrome的诊断保持当前profile和网络路径，不为测量临时设置代理。仅访问显式授权的HTTPS供应商，禁止凭据、重定向和未授权URL；公开DNS预检及连接后IP观察不能保证阻止连接前DNS重绑定。域名供应商必须受用户信任，此残余风险在报告中披露，缺少远端IP证据记unknown，不宣称具备主机探测的IP固定保障。

原始资料：
- https://www.anthropic.com/legal/privacy
- https://privacy.claude.com/en/articles/11186740-does-claude-use-my-location
- https://developer.mozilla.org/en-US/docs/Web/API/Navigator/languages
- https://www.rfc-editor.org/rfc/rfc7871
- https://developers.google.com/speed/public-dns/faq
- https://github.com/stormzhang/ipcheck/tree/50018243a5699df10c6b46617f3fe4bd73b35dab
- https://github.com/TZZ520/claude-environment-check/tree/39fe2f3d19eb2b9b474a348c6d5e6ee1ab2b757f
- https://github.com/Nooobhh/claude-analysis/tree/a245653756f8d7c41bae8f81f67b35ffc8fd22cd
- https://github.com/misakaikato/exitlens/tree/05eccb39e11c9faf71537fed15b55006493a033c

## 协议与所有权

新增proto/extensions.proto定义RegionService、BrowserService和仅本地的BrowserLocalService。远端与IPC使用Connect HTTP/2；宿主到插件仍用go-plugin gRPC；插件回调在私有CoreAPI socket注册受实例鉴权的专用服务。禁止用通用action或任意字节的执行接口绕过类型化服务。

agent继续持有连接和scope；CLI不得启动插件或拥有loopback listener。browser client插件持有loopback Web查看器。daemon只路由，region/browser server插件持有业务；browser runtime由systemd用户服务持有，插件重连认领时核对实例身份，不凭旧PID杀进程。

新权限使用显式method映射：region.read/plan/apply/dns/rollback、browser.list/manage/view/write/control/files.read/files.write/diagnose。调用者不得自行填写owner/grant；宿主覆盖认证派生字段，未知方法默认拒绝。远端流撤销/过期关闭attachment，但独立Chrome实例继续运行。插件未ready不广告能力。

本地查看入口只loopback，短期单次引导凭据，Host/Origin/CSRF/WebSocket校验，不暴露daemon Token、完整agent API、VNC/CDP公网端口。静态资源本地分发。画面协议仅固定受管实例，不是通用WSS回退或任意TCP隧道。

查看器控制请求必须绑定用户确认时的attachment、instance和control revision；本地插件在锁内核对当前连接，转发固定旧scope并随该attachment取消，不能将迟到请求改绑到重连后的连接。前端只接受当前generation且匹配scope的流事件授予/撤销控制，HTTP成功和状态快照不能授权。已由流确认的控制结果不被迟到HTTP超时提示覆盖，撤权同步更新只读提示；显式重连取消旧控制/状态请求，旧请求完成不能占用或释放新一代刷新槽。这些修补已随alpha.8发布并于2026-11-21部署到用户两端；本轮5次显式重连和接管功能通过，但另有一次状态查询超时经手动Refresh恢复，不能宣称所有超时或长稳问题已解决，见[两端升级与实测](15-implementation-status.md#evidence-alpha8-upgrade-20261121)。alpha.7历史失败记录保留。

alpha.9响应路径：每帧仍实时核查本机服务器记录及撤销上下文，只复制单条记录而非整个状态；只读RFB协商/更新请求仅写自身socket，不占用全局控制锁。修改桌面的按键/鼠标/剪贴板/resize仍在控制revision及权限检查后、同锁内发送，不能让旧控制者输入越过接管边界。READY始终先于控制通知和图像；CONTROL独立有界队列在发送边界优先，DATA保持全部字节顺序，不能丢帧/重排RFB块。已阻塞的transport Send不能被抢占，控制队列耗尽只取消该慢attachment。状态查询释放worker锁等待runtime/region，返回前验证完整状态/配置/实例版本，发生漂移返回Aborted供显式刷新。

公开 alpha.10 及当前源码的新查看器默认 Smooth（历史公开 alpha.9 默认 Balanced），采用quality=3/compression=6，Balanced保持原quality=6/compression=2，Clear采用quality=8/compression=2；只使用noVNC公开API。更强压缩可减少流量但增加服务端CPU成本，必须实际比较，不自动改变分辨率或取得控制权。屏幕接收速率、二进制块速率、到达间隔、最近数据年龄和本地WebSocket发送buffer不等于RTT、远端队列或最终显示延迟。模式显示仅由status报告的browse/diagnostic映射，缺失/未知值显示unknown；状态查询失败时历史模式仅留在明确标记为旧快照的Details中。

状态持久化原子写与revision/request_id去重；runtime连接、PID、listener不当作持久真相。配置生效版本和进程启动版本分开。stop/disable与正常宿主shutdown须区分，避免重启误杀浏览器。

## 实现与验证取舍

优先系统发行版TigerVNC、Fcitx5及中文/日文引擎、官方Chrome、随包固定版本noVNC；安装前按平台检查来源和依赖。具体兼容版本以产物及验收记录为准。未测性能不宣传，配额/确认窗口必须可配置且有有界安全默认，记录实测依据后才宣称容量。

Linux Chrome固定使用GTK3及发行版`fcitx5-frontend-gtk3`，不使用XIM或另起IBus daemon。受管Fcitx与Chrome使用选定的加密密钥环总线，同时保留受管私有显示与输入法配置；GTK前端需要在会话总线上发现输入法服务。若该总线上已有`org.fcitx.Fcitx5`则启动失败并要求用户显式处理冲突，不替换现有输入法。此总线仍属于同一OS用户，不构成同UID安全沙箱。

本批必须完成真实CLI→agent→daemon→插件调用、真实浏览器画面、输入/控制权/文件/取消行为，并验证helper白名单、计划竞争与DNS回滚。Ubuntu/Debian两架构、macOS/Linux/WSL2实际表面、真实账号授权及物理重启分别登记；无法在授权环境完成的外部门槛列出具体用户操作，不伪装成功。

## 当前源码命令流程

以下命令自公开alpha.3提供，当前安装入口为v0.0.1-beta.1，不适用于alpha.2；系统支持安装及平台验收状态以08/15/16为准。先按[11](11-automated-installation.md)完成可信bundle安装、管理员依赖/策略/密钥环配置，再通过既有连接命令登记服务器。

```sh
shiki --server my-vps plugins --side remote enable region
shiki --server my-vps region inspect
shiki --server my-vps region plan --file jp.json
# 核对返回的changes、warnings、id及revision后，再显式提交：
shiki --server my-vps region apply <plan-id> --expected-revision <revision> --yes
shiki --server my-vps region operation <operation-id>
```

`jp.json`可使用完整配置`{"country":"JP","timezone":"Asia/Tokyo","locale":"ja_JP.UTF-8","languages":["ja_JP","ja"]}`。US须明确选择时区和语言，例如`America/New_York`、`en_US.UTF-8`、`["en_US","en"]`，不是整个美国的唯一正确值。未提供系统DNS变更时保持原解析器；不要把国家选择当作DNS供应商选择。

应用超时或响应丢失先运行`region inspect`，读取`pending_operation_id`及`confirm_before`，再查询操作。系统DNS变更必须在新的解析/连接检查成功后于截止前显式`region confirm <operation-id> --yes`；否则独立root守卫尝试恢复。`region rollback <operation-id> --yes`用于显式回滚；冲突不覆盖外部修改，需管理员复核后恢复，不可盲目重试或删除守卫。

```sh
shiki --server my-vps plugins --side remote enable browser
shiki plugins --side local enable browser
shiki --server my-vps browser profile create jp-work --name "JP work" --file jp.json --yes
shiki --server my-vps browser start jp-work --yes
shiki --server my-vps browser open jp-work --read-only=false --launch
```

alpha.9进行原生诊断须先显式切换模式；以下启动模式选项需要新版CLI、agent、daemon及浏览器worker协商 `browser.launch-mode.v1`，alpha.8不支持：

```sh
shiki --server my-vps browser start jp-work --diagnostics --replace-running --yes
shiki --server my-vps browser diagnose jp-work
# 诊断完成后显式重启回普通浏览：
shiki --server my-vps browser start jp-work --replace-running --yes
```

查看URL是短时单次凭据，仅保留在私有stdout，不复制到日志或分享。默认只读；允许写入的查看器仍须在页面点击Take control显式接管。关闭CLI/查看页面不停止远端Chrome；`browser close-viewer <viewer-id>`只关闭本机查看会话，`browser stop <profile-id> --yes`才停止Chrome。

`browser profile boot <profile-id> --yes`选择唯一开机目标；已有目标时须显式`--replace-boot`，`--enabled=false`清除。仅切换保存的配置不重启现有Chrome。`browser profile delete`会删除Cookie及暂存数据，不能与停止实例混为一谈。

上传使用`browser files upload <profile-id> <local-file> --yes`；取回使用`browser files download <profile-id> <name> --output <local-path> --yes`，覆盖还需显式`--overwrite`。网页也提供独立文件面板及显式剪贴板操作；实际服务端权限仍单独检查。

诊断默认不访问外部服务。只有显式`--allow-external --providers <providers.json>`才授权指定供应商；供应商协议见`pkg/plugin/diagnostics/doc.go`。WebRTC还需独立`--allow-webrtc`，没有隐式STUN/TURN。诊断不会把配置值、主机结果或本机查看器环境冒充指定远程Chrome的实测值。

## 尚需真实环境验证的项目

以下不计入本地容器通过，也不是要求提供账号或密钥给开发工具：

| 项目 | 用户环境中的验证步骤 | 当前边界 |
|---|---|---|
| Claude人工授权 | 在远程Chrome打开官方授权页面，手动完成登录；回到远程终端核对Claude Code授权状态与一次实际调用 | 未使用真实账号；Cookie持久化夹具不等于授权闭环 |
| 物理重启与无人值守恢复 | 配置加密凭据并选择唯一boot profile；在具备带外恢复手段的主机自行重启，检查普通UID服务、唯一实例、密钥环解锁及profile保留；显式stop后同次开机不得自动复活 | 容器服务重启与模拟boot ID不是断电/物理重启 |
| 原生平台 | 在Ubuntu24.04/Debian12的amd64/arm64与macOS/Linux/WSL2实际客户端运行安装、查看器、输入法、文件、断线/重连；记录精确包版本与架构 | 2026-11-21已有Debian12/amd64 VPS与Mac arm64的alpha.8功能实测；不扩大为其余平台全矩阵或长稳通过 |
| 真实网络观察 | 用户自行确认出口及供应商授权后，分别查看主机、目标Chrome与权威DNS/HTTPS观察结果；不把解析器注册国当作出口国 | 隔离DNS/TLS夹具不代表公网地区、信誉或服务资格 |
| 公开包完整部署 | 先独立验签，再在可恢复环境执行服务安装/升级/回滚 | 2026-11-21已完成签名alpha.7→alpha.8的VPS/Mac升级；本轮未执行回滚、从零首装或物理重启。历史alpha.3四目标资产及Linux amd64首次/重复暂存已验；这些与其他版本验收不代替本版实测 |

### alpha.6 region daemon运行策略补修

显式region系统支持计划包含受管daemon专用`50-shikigami-region.conf`（NoNewPrivileges=false），仅匹配已安装受管服务的普通UID，保留六动作sudoers白名单。core默认服务仍NoNewPrivileges=true。daemon-reload不能改变已有进程继承标志，故若/proc仍报告NoNewPrivs=1，批准setup有条件仅重启daemon；不重启独立Chrome/tmux/Claude。固定drop-in被修改或其他管理员策略冲突时保留并明确失败；卸载只删除内容精确匹配的受管文件。当前发行/部署证据见08/15/16及29。
