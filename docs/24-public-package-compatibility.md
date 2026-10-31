# 公开发行包升级、回滚与兼容验收

本批使用已经公开的 `v0.0.1-alpha.1`（`f0b5f83dce599c8e0d715bda5e7806a0ef6aaa72`）与 `v0.0.1-alpha.2`（`fa35debd9c74d0ad44704e3a7897e0e0c1fcc3f2`）原始归档。两版均先以可信仓库 `configs/release-signing.pub` 独立认证，再从认证副本安全解包，核对完整 SHA256SUMS。没有重新构建、修改版本字符串或修改插件 manifest 来模拟历史版本。

测试夹具来自当前工作树，正式产品仍为各发行版本；夹具的 race 构建不表示公开产品本身启用了 race。两版状态 schema 相同，本批是实际包升级和双向重开兼容，不是 schema 迁移。历史开发 schema 的迁移/拒写/有限恢复证据继续单独保留。

## macOS arm64 原生活动升级与回滚

公开 alpha.1 初装 → alpha.2 升级 → alpha.1 回滚 → 卸载已通过。测试只在标准 `dev.shikigami.agent` job 与 plist 均不存在时开始，激活前再次检查，使用独立配置、prefix 和状态。真实 LaunchAgent 负责 agent；daemon 是隔离的 TLS 进程夹具，不构成 macOS daemon 服务验收。

本批修正活动夹具：接入已有认证 prebuilt 参数，将原包复制到受精确清理保护的私有 runtime 并复验 pin，每次激活使用目标发行包自己的 installer。回滚由升级版 installer 执行。未修改产品。

总计18.26秒，升级边界6.22秒、回滚边界4.05秒。两边界均实际验证上传、终端和TCP：8MiB文件在256KiB暂存时跨越边界；旧终端/TCP关闭，新连接与新输入可用且不重放，远端原pane保持，独立直连上传不被agent替换误取消。冲突两侧内容、显式解决、暂停状态、转发意图及停止请求保留。正常清理记录18个进程身份；runner退出0，fallback与errors均空；临时fixture、标准job/plist已再次确认不存在。

证据根：`/private/tmp/shiki-public-launchd-abrjie6t/evidence/`。`activity.log` SHA256：`a111943a71c95e9fe3e271d7d3e36654fd34b369d607138feb4742e729280a11`；696项全源输入manifest：`7efd5d30637528d5a273ee865eb67c9b9df13908c900c06ae27e45623bfc5ecd`。进程记录的未填UID/hash字段不是运行用户或产品hash证据；产品来源以独立认证和payload后置校验为准。两个darwin-arm64归档后置hash保持：alpha.1 `6229882b3b730dfc4ab98147c782b34e0820ee98ac2416f1e0062dc2ae79ca53`，alpha.2 `8a63cba6b19aa42fd3bd41992e09099cff7ef52fede464dec47e1685281e459d`。

## Linux arm64 真实 systemd 生命周期

既有 `scripts/test-systemd.sh --bundles` 原样运行，两版产品均未重建。独立容器以systemd为PID1，私有cgroup/PID空间、network=none、只加SYS_ADMIN而非privileged；daemon/agent/插件/终端均以UID1000运行，源与发行包只读挂载。

13个生命周期记录通过：daemon停止/启动、restart、SIGKILL自动恢复；agent停止/启动、SIGKILL、三次崩溃守卫与显式恢复后交还服务、正常退出15秒不重启；agent升级/回滚/卸载后重装保留原凭据；daemon升级/回滚/卸载后重装。四个活动边界分别为agent升级、agent回滚、daemon升级、daemon回滚，均核验旧宿主/worker回收及实际文件、终端、TCP恢复。原daemon ID、原Token、项目文件、配置和tmux pane按契约保留；卸载后重新激活daemon明确签发一个新Token，同时原凭据仍可访问。

最终正常 `systemctl poweroff`，容器退出0、非OOM；精确容器及镜像已不存在。553项冻结源hash与认证payload后置hash保持。证据根：`/tmp/shiki-public-upgrade-linux-20261030-o5e0V9/systemd/`；`source-sha256.json` SHA256：`8ae34fbd1f82c881ac9350e617b19b9508f23bac230782884895b180598042fd`；`evidence.json` SHA256：`dc3e48a90667d51779eea20d5c001f36ad86da125064c305b1692a31d459c3fe`。独立复核见同目录 `independent-verification.json`。

Linux归档SHA256：alpha.1 `6157f56907519d0df3911bb5dc6697f02e0e5811df4f107cfc373f81e203c9f8`；alpha.2 `592fd4ba986c0bcfa00a3b830fdf2f58b9b73ad2c081fe8d3f39b9768ef5a86d`。测试通过不等于物理主机关机/重启、正式账号、tsnet或真实Claude使用通过；本批网络为loopback TLS，Claude为显式shell夹具。

## Linux alpha.2 新装、重复安装与卸载

另起隔离容器，将认证alpha.2放在manifest首项，执行既有 `--fresh-only` 路径。新装agent/daemon、四个插件ready、同版重复安装返回unchanged、原daemon ID/Token/配置/项目/tmux保持，以及卸载后宿主/worker回收全部通过。正常poweroff退出0，精确容器与镜像已清理；554项冻结源hash保持。未编译或重建产品。

证据根：`/tmp/shiki-public-upgrade-linux-20261030-o5e0V9/systemd-alpha2-fresh/`。`evidence.json` SHA256：`aafb5ab291f30814b9ab715da26ad788bae72cbec761907db49b3ed0f1d2f2ab`；源manifest SHA256：`e96915118b9af6ec3bf0c063347dd71ee5a81b2dee4be3c9beaf95d8d5e33843`。运行时源快照独立于前一组；此模式没有编译activity夹具，不把后来新增测试文件算作产品变化。

## macOS alpha.2 新装、重复安装与卸载

独立临时Python夹具直接执行认证alpha.2 installer，不编译产品或Go测试。新装、两插件ready、同参数重复安装、卸载及正常清理共3.654秒通过。重复安装保持同agent及两个worker的PID、birth、UID、偏好revision；卸载保留配置、resources和独立项目文件。标准job/plist前后均不存在，fixture已删除，无fallback或残留。

证据在前述Mac目录的 `fresh-repeat/`；`events.json` SHA256：`ec42e6a84440951aa00d5994998d8b08bbdd7db13d68d6fea6a81a7dafbe794c`；`fresh-repeat.py` SHA256：`5f5920f6438dfe3027bab9296b1244fc24b11873cd6132e21fb6e5ccad21cefd`。首轮临时脚本只匹配install路径而遗漏runtime插件路径，在重复安装之前失败；已精确清理，原失败保存在 `fresh-repeat-preflight-failed/assessment.json`。修正为本次runtime白名单路径后，新隔离目录通过，不把首轮失败追认为成功。

## 实际混合版本矩阵

macOS arm64原生第五轮通过，测试12.38秒（外层12.788秒），证据 `/tmp/shiki-rc-57soczen/`。独立 `releasecompat` 夹具运行92条实际命令，没有安装服务；两包再次由仓库公钥认证，运行前后38项payload hash不变。

| 场景 | 实际结果 |
|---|---|
| CLI alpha.2 → agent alpha.1 → daemon alpha.2 | 原CLI通过IPC/TLS/Token执行项目创建/读取；实际握手及远端协商记录版本与协议1.0。 |
| CLI alpha.1 → agent alpha.2 → daemon alpha.1，再回切 | 旧项目可读，再创建新项目；回切后两者仍存在并可正常删除，原凭据继续有效。 |
| 两端宿主使用各自同版sync/port | 共8次ready正控，记录实际协议及8个worker身份。 |
| 两端宿主换为另一版原始manifest及二进制 | 共8次incompatible拒绝，无可用协议能力；核心状态和项目查询继续成功。不改manifest伪造版本。 |
| 真实状态的隔离future-schema副本 | 两发行版均拒绝schema99：agent degraded/AGENT_STATE_UNAVAILABLE、daemon明确新schema错误；state及previous字节不变。此项为合成负向副本，不是历史schema升级。 |

8个宿主进程正常退出；包含worker共16个已观察身份消失，私有路径扫描无残留。`evidence/events.json` SHA256：`34ffc9e0fdc389f5dc77ec67aa3608f06aa2553caad1a4770c7d4e5a291bcc2a`。默认CI不运行该opt-in真实发行矩阵；可使用 `python3 test/diagnostics/releasecompat/runner.py --go "$(command -v go)"`，前提是本机对应平台公开归档及签名已下载到dist/releases，详见脚本参数。

前四轮夹具失败均保留，未冒充通过：agent版本命令应使用--version；首次握手可能starting需等ready；过长私有根触发既有UDS长度限制；复制带Revision配置到新路径需清空Revision。分别修正夹具并在新隔离目录运行，没有修改产品或扩大路径/版本限制。第四轮已经通过混合业务和插件矩阵，但未完成future副本测试；最终第五轮才是完整结论。

Linux arm64普通用户、network=none的独立容器也已通过同一矩阵：测试7.43秒，92命令、8个同版ready、8个跨版incompatible、两版future拒写、8宿主+8worker清理及38payload复核全部通过。证据 `/tmp/shiki-subscription-linux-final-20261030-DoRHUa/compat-evidence/rc/`；events SHA256 `68bd1d07a5971768bb0bef18948b216ec013c68d0bcfb70baf5f8d32d2da7a35`。发行包只读挂载，状态在容器原生/tmp，正常退出后打包取证；精确容器与镜像已清理。

首个Linux矩阵在业务之前因Mac bind目录映射所有者不匹配、私有路径扫描误命中docker-init参数而失败；改为容器原生状态与通过环境传参后通过，不改产品或Go夹具。原失败与同期成功的四包检查保留在 `/tmp/shiki-subscription-linux-20261030-cMX2jX/`，不将整个首轮记为通过。本批当时Phase 0–5为32/42（最新总数见08），当时剩余10项复合门槛不因局部通过自动关闭。公开alpha.2不包含其后master上的tsnet登录或取消修复，不将本批公开包结果追认为最新生产源码全量验收。

## 七类架构复核

| 检查范围 | 本批结论与边界 |
|---|---|
| 目录、依赖、SDK与host | 活动夹具位于test/e2e，跨版本诊断位于test/diagnostics；未将插件业务移入宿主或SDK。test/architecture的目录/依赖与协议文档检查通过。 |
| RPC、准入、版本和授权 | 实际发行组合使用原握手、能力与TLS/Token；不以版本不同直接判不兼容，不篡改插件manifest获取伪成功。 |
| 持久与运行状态 | 公开包schema相同，升级保留配置/身份/凭据/项目及活动意图；不把同schema重开叫作迁移。 |
| 进程、取消、崩溃和恢复 | 原生LaunchAgent及容器systemd按目标helper切换，旧worker与owned资源回收有证据；主机重启登录仍独立待验。 |
| 文件、终端、网络 | 实际上传/冲突/终端/TCP验收；旧TCP结束后重建，不重放输入；loopback TLS和shell夹具不代表tsnet/Claude。 |
| CLI、配置、安装和产物 | 公开签名归档独立验证并后置hash，原helper执行；重复安装/卸载保留范围明确；没有发布新tag。 |
| 行为证据与支持声明 | 保留夹具失败及各冻结输入；本批通过项只减少对应子门槛，本批当时32/42和剩余外部/平台/长期条件保持（最新总数见08）。 |

同批慢流取消修复另见15/16的专项记录，不属于上述历史公开发行包；其最终双平台结果不得由公开包验收替代。临时目录不是永久证据存储；原始证据丢失时不能仅凭摘要扩大支持范围。


<a id="external-installer-20261031"></a>
## 官方身份下的实际安装器进展（2026-10-31，尚未收尾）

A为重新验签的公开v0.0.1-alpha.2，B为冻结提交b30c58563651271b044796f5a8a52065ec155f57的本地v0.0.1-alpha.2-local.install.1候选，B未签名、未发布。732个源码输入在构建前后保持；不能将B结果称为新正式发行验收。证据根为`/private/tmp/shiki-installer-tsnet-20261031-deho3ofr/evidence/`，准备包来源另保留在`/private/tmp/shiki-alpha2-installer-a-a3af0oz0/`和`/private/tmp/shiki-installer-b-1jq9a7eo/`的manifest/收据。

| 范围 | 已完成与剩余条件 |
|---|---|
| 原生Linux amd64 VPS安装/升级 | 真实systemd下A安装→B升级→回滚A通过，PID/InvocationID变化，未执行主机重启；同tsnet身份/IP、daemon ID和凭据revision保持，无新Token签发。 |
| VPS业务及服务恢复 | 新终端、双向文件、共同基线自动同步、新TCP通过；原pane、原baseline、暂停mapping及未解冲突保持。第二轮升级/回滚旧TCP EOF且无重放。stop→daemon-reload→start后先观察管理器再调用CLI，9.645秒内旧TCP结束及服务转换完成；不代表主机重启。 |
| VPS卸载与恢复 | 真正执行uninstall，标准unit/链接移除，配置/state/tsnet保留；恢复原配置与原测试daemon后业务通过。全UID/PPid扫描核验当时仅当前daemon及两个匹配worker，没有前代或孤儿worker。 |
| Mac原生LaunchAgent | A首装及单独B首装均未在原30秒预算内完成节点就绪，A共86次观察（85份节点未初始化结果、1次探测超时），B共85份节点未初始化结果。两轮均止于首装，不将未执行的重复安装、升级、回滚或服务恢复记通过。 |
| Mac定向诊断 | 单独诊断plist关闭KeepAlive、增加私有stderr并获取Go栈；Server.Start已返回，watch/poll均运行。authRoutine→Direct.TryLogin→loadServerPubKeys→http.Transport.getConn等待，另有tsdial.SystemDial→netFD.connect→WaitWrite。证据指向控制面公钥请求的TCP拨号等待，未确定目标地址或具体网络原因；curl直连/既有代理均能得到302，不能宣布代理是唯一根因。诊断变体不算安装通过。 |
| Claude | 官方CLI未登录报告和安装器application_pending/退出3、CLAUDE_LOGIN_REQUIRED映射有证据。真实登录/推理按[11当前范围](11-automated-installation.md#claude-acceptance-scope-20261031)排除，未获取凭据或推理。 |

保留失败边界：VPS最初将系统命令目录放在普通用户HOME，安装器正确拒绝，恢复后使用新的root所有目录；首次迁移超过客户端重试窗口，曾显式connect，不能证明该次自动恢复。初始node_uninitialized/REMOTE_LISTENER_PENDING是合法中间态，先前夹具仅接受application_pending的失败保留。首次回滚held TCP的5秒读超时保留；独立第二轮按已有升级测试使用转换完成后的15秒读期限，不声称整个升级在15秒内恢复。第二轮回滚曾过早使用旧ready观察，等待目标版本与live session后业务成功，不抹去首次失败。

主要收据：`vps-transition-B2.json`、`vps-transition-rollback2.json`、`vps-manager-reload-transition.json`、`vps-all-worker-generation-audit.json`、`vps-uninstall-retention-and-restore.json`、`vps-restored-business.json`；Mac为`mac-install-summary.json`、`mac-bcontrol-summary.json`、`mac-sdkdiag-go-stack-summary.json`、`mac-sdkdiag-independent-recovery-check.json`。

清理已完成的范围：VPS本轮11个安装/官方Claude临时目标、113项约1.54GB已清理；Mac三轮runtime及标准job/plist均不存在，原始栈/sample已删除，仅保留函数名/等待态摘要。Mac最终恢复专用agent PID93282，原配置hash、身份、终端/文件/TCP、暂停/冲突和旧基线核验通过。两端未重启、未注销，未改全局launchctl代理环境。专用Tailscale身份继续保留；业务测试文件、两个任务mapping/project及本机A/B预备包仍待精确收尾，不能宣称环境已全部清干净。

本轮结果减少VPS子门槛，但Mac失败仍阻塞两项完整安装条目，37/42保持。下一步需定位SDK实际拨号与受管进程网络环境的差异，再基于证据修复并完成Mac矩阵；不能靠延长等待、重复登录或重复无假设测试取得勾选。


<a id="macos-route-fix-20261031"></a>
### macOS 网络根因与修复（2026-10-31，安装复验待执行）

独立CGO_ENABLED=0、无代理环境、每项5秒的拨号对照确认：控制域名被本机既有VPN解析为198.18范围虚拟地址，普通OS拨号TLS及公开/key请求成功；SDK默认绑定物理接口超时。SDK按目的路由选接口也因隧道网关解析`no valid address found`回落默认接口并超时；只启用该开关不是修复。诊断进程禁用netns后两域均HTTP200，约0.98/0.89秒。证据为同根`mac-network-dial-differential.json`、`mac-network-dial-detail.json`、`mac-network-userspace-differential.json`，未创建节点或请求登录。

修复限定Darwin SDK适配：节点启动前`netns.SetEnabled(false)`，匹配固定SDK `cmd/tailscaled/tailscaled.go`的userspace-networking策略；其他平台no-op。Shiki使用fake TUN/router、不安装内核路由，OS现有隧道是外层承载，不会回入同一Shiki节点。没有改变系统Tailscale/代理/路由；业务仍按peer身份和授权直入gVisor，不允许系统TCP回退。单独SetDisableBindConnToInterface会被netmap能力覆盖，故未采用；进程级策略不随单个Node退出恢复。HTTP(S)环境代理仍按SDK实现，ALL_PROXY隐式SOCKS包装不在支持范围，见[02](02-connection-model.md#代理与后续-websocket)。

真实socket回归验证SDK不再替换普通拨号器Control，并模拟netmap接口开关变化，策略保持；node整包race48.272秒、架构race1.378秒通过。完整现场矩阵仍须独立执行。将从修复后同一冻结源码正常构建两个明确本地版本；它们证明安装器版本切换及外部身份恢复，历史实现兼容性仍沿用本文件公开alpha.1/alpha.2的独立真实包证据。不得篡改或追认旧公开alpha.2在当前fake-IP网络下的失败；本修复尚未发布。


<a id="macos-route-counterexample-20261031"></a>
### 全socket解绑方案被现场对照否定（2026-10-31）

13bcd0a的Darwin `netns.SetEnabled(false)`虽使控制面/key和节点online通过，但随后的实际远端查询及终端失败。route2/route3及独立flow/proxy诊断均保留；后者证明普通sessions查询在terminal开始前也可能超时，不能写成“attach导致锁死”。Go栈没有确证的锁死，ready/direct是最近观察、不是每次业务链路可达性保证。

严格单变量对照最终保持13bcd0a源码、构建Version/Commit/BuiltAt、新CLI/plugin及原HTTP(S)代理相同，仅在独立诊断副本将Darwin网络适配置为空，真实新终端回显与正常detach通过。不同诊断版本名的早期尝试触发plugin同构建检查而未执行业务，不能用于网络因果判断。最终对照来源为`mac-policy3-sdk-default-with-proxy-outcome.json`等收据。故全socket解绑不是最终修复，后续源码须保留SDK默认UDP策略，改为显式读取现有macOS静态HTTP(S)系统代理，边界以[02](02-connection-model.md#代理与后续-websocket)为准。上述HTTP200和node race不追认为完整外部业务通过，13bcd0a尚未公开发行。

另外，验收辅助函数原先只等目标文件字节出现就立即开始下一轮停机/修改，未等共同基线持久化；专用common测试文件因此出现真实双边变更冲突。已只对该测试文件显式解决并留据，辅助函数增加每方向durable baseline与最终内容一致的等待，未修改产品冲突语义、原baseline或预先保留的暂停/冲突。之后各轮恢复原配置/身份和完整业务均核验，原失败不追认通过。当前安装复验和最终清理尚未完成。


<a id="macos-system-proxy-20261031"></a>
### macOS静态系统代理适配（2026-10-31，完整矩阵待执行）

替代实现保留SDK默认UDP/接口及其原有ALL_PROXY机制，只在Darwin首个Node启动前以进程级sync.Once注册SDK HTTP(S)代理解析器。绝对路径scutil只读一次系统快照，执行2秒、共享输出64KiB、溢出取消及等待边界；系统设置变化须重启Shiki。显式同scheme环境代理优先，无效/含userinfo/非HTTP(S)URL固定错误拒绝；NO_PROXY明确绕过不落入系统代理，独立*条目不读取系统配置。缺失scheme才采用对应静态系统配置及例外，已由env覆盖的scheme不被系统fallback错误阻塞。全局配置与嵌套scoped配置隔离，PAC/自动发现/SOCKS-only和代理凭据不新增支持；SDK DERP可忽略解析错误后尝试原直连的边界明确保留。

注册失败和无可用代理配置时不输出原始值，Node.Open失败释放状态锁；多Node共用不可变快照且关闭不重置。没有修改plist、系统代理、Tailscale或路由，也没有修改业务用户态TCP/授权。真实CONNECT测试验证目标TLS与hostname错误仍拒绝，loopback与各种绕过/优先、命令取消/输出上限有回归；Darwin真实socket测试确认SDK默认接口策略保留。整包node/architecture race51.199/1.373秒通过，最后新增scheme/PAC/readerror/无关坏字段/NO_PROXY=*回归另行race1.548秒通过。接下来以明确local.proxy.1/.2同源本地bundle实际安装，不能将上述测试替代现场矩阵。
