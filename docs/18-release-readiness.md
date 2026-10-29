# 首版发布说明草稿与交付门槛

更新：2026-10-29。本文件是发布准备材料，不是发行公告。首个预发布版本已确定为 `v0.0.1-alpha.1`；发布 tag 尚未创建，最终提交和签名身份尚未确定；当前没有可供用户下载的正式签名发行版。范围仍由 [08 路线图](./08-roadmap.md) 决定，本文件不另设较小的“完成”标准。

## 首发兼容基线

2026-10-29用户确认从未对外发布可用版本，GitHub Release和tag列表的只读检查也为空。
因此首发没有“从历史正式发行升级”的对象，此项适用性为N/A，不记为测试通过，
也不等待用户提供不存在的旧版本。历史段落中的相关待办由此说明更新。
开发版本间的schema迁移、未知未来格式拒绝、协议与插件兼容协商、重复安装、
升级失败恢复、回滚和数据/Token撤销记录保留仍需按原契约验证。
首次对外发布须保存固定源码、构建元数据、产物摘要及状态格式，供下一版真实升级回归使用。

用户已确定首个对外预发布版本为 `v0.0.1-alpha.1`。它符合当前构建、安装与签名工具的版本格式，
表达正在验证的预发布状态；该版本名称不代表全部Phase 0–5门槛已通过，也不等于已发布。

## 首次提交与远端 CI 审阅

准备提交的范围为当前首次实现：`cmd/`、`internal/`、`pkg/`、`plugins/`、`proto/`、
`configs/`、`scripts/`、`test/`、`.github/`、根构建/许可证/入口文件及 `docs/`，共652文件。
`bin/`、`dist/`、工具缓存与临时运行证据被排除；现有开发归档没有改名或重打包。
用户已授权直接提交到 `master` 并推送触发 CI，提交标题为 `feat: prepare v0.0.1-alpha.1 preview`。
不创建发布分支、tag或Release；以master实际提交历史和GitHub运行记录确认结果，授权不等于CI通过。

按用户要求，CI中四处Action引用改为GitHub官方latest稳定Release标签：checkout v7.0.1、setup-go v7.0.0（2026-10-29查询）；已核对两者action.yml的Node24运行时及当前使用参数，工作流仍为push/pull_request和只读权限，不使用提交哈希引用。

`.github/workflows/ci.yml` 已通过本地 actionlint 检查。push将触发macOS/Linux测试、
生成漂移、依赖扫描、终端语义和Linux/macOS amd64/arm64打包检查；工作流不会创建Release或签名。
CI采用 `v0.0.0-ci`，后续预发布应从通过验收的固定提交显式构建 `v0.0.1-alpha.1`。
工作流静态检查不等于远端运行通过；真实结果与剩余验收门槛分别记录。

## 当前候选与验收边界

本地可独立收尾的证据矩阵和条件驱动方案已完成，见 [19 本地验收收敛](./19-local-acceptance-closure.md)。已知 syncack 和当前 systemd 组合遗漏已由对应补验关闭；当前候选长期资源门槛仍待有效证据，不再把这两项写成未启动任务。以下带冻结源编号的段落是历史验收范围，不能叠加成一次当前全套验收。

构建入口现支持显式 `SHIKI_COMMIT=<40或64位十六进制提交号>`、`SHIKI_BUILT_AT=<YYYY-MM-DDTHH:MM:SSZ>`，普通、内嵌及其调用方 release 使用同一校验规则；省略时仍为 `unknown`。8项回归验证两入口六程序的相同链接参数、真实 buildinfo 探针的默认/显式输出及非法输入在写输出前拒绝；没有重新打包现有候选。提交号应来自实际冻结源码的可信记录，脚本不自动取脏工作树 HEAD，也不证明调用方提供值的真实性。正式发行仍需真实提交/tag、可信构建及签名；`.5` 已有 `unknown` 元数据保持原样。

当前源码的同步停止、Stage落盘及中断回滚/服务临时文件恢复修复已进入 `.5` 候选。同测红绿、Linux九个真实EIO和738同源双平台六包race/vet的证据保持；原生通用采集器缺PID清单退出1与skip仍单列，不因本批归档检查通过而追改历史结果。路线图29/42，见[恢复验收](15-implementation-status.md#evidence-recovery-conformance-validation)。

`.3` 在关闭/不可用节点的只读诊断上误报协议不兼容的偏差已修复，并完成有效同测红绿、双平台相关四包race/vet及 `.4` 实际包内验证。路线图恢复29/42；旧归档和原始错误记录保留，不追认后续源码。

最新739源本地开发候选 `v0.0.0-local.20261029.5` 已完成macOS/Linux arm64首轮构建及实际归档验收，交付目录 `dist/local-20261029.5/`。541份Go/proto/根及嵌套module与恢复修复已验738源完全一致；此前 `.2/.3/.4` 保留为历史产物。每平台52实际命令、六程序/双插件/14成员hash与pin、两私有agent状态诊断和正常清理通过；新增归档helper首次/重复stage-only校验同一14成员及review，无激活current/receipt、无配置/状态/安装links生成，标准服务文件保持。Plan.Home来自真实账户查询，仅服务review写入私有prefix，不声称HOME环境重定向标准服务路径。未签名，Commit/BuiltAt unknown；暂存不等于激活，本地摘要不是发布者认证，不追认697长跑。见[交付说明](../dist/local-20261029.5/README.md)与[候选验收](15-implementation-status.md#evidence-local-candidate-20261029-5)。

2026-10-29宿主清理修复已完成限定范围的双平台验收：692基础源中仅host/supervisor两份生产文件变化，补齐CoreAPI处理器实际join、并发Stop独立截止、失败Launch锁归属及晚启动进程回收；增加一个诊断脚本后的693源验证真实同步、转发、worker/host轮换、在途文件恢复与架构边界。原生及Linux相关race/vet通过，Linux应用/collector退出0、精确容器/快照清理完成。详见[修复](15-implementation-status.md#evidence-host-handler-ownership)与[受影响验收](15-implementation-status.md#evidence-host-cleanup-affected-validation)。

这不是当前全部平台的完整发行验收。原macOS默认12小时成功仅覆盖原495文件，原Linux578文件运行已失败；独立观察夹具修复与637文件预检不能替代默认时长验收。资源稳定、真实账号/物理平台、正式信任根/发行源和支持入口仍待完成，路线图保持29/42。新697源Linux默认12小时运行已于10:40启动，仍未验收，见[独立运行](15-implementation-status.md#evidence-default-rotation-linux-current)。11要求的登录/开机启动策略配置展示已完成双平台相关验证和真实systemd四态查询；仅报告受管配置，不是实际重启/登录验收，见[启动策略](15-implementation-status.md#evidence-startup-policy-validation)。该后续实现不进入已运行697快照。

## 发布说明草稿

Shikigami 通过本地 CLI/TUI 和常驻 agent 使用远端普通用户的 tmux/Claude 会话。前端共用 agent 的连接、节点、文件同步与 TCP 转发；关闭前端不停止远端会话。文件同步与转发由双端独立插件执行。

已有实现与限定环境证据如下；sync及SDK恢复修复的568文件历史快照已通过同源双平台race/E2E，后续修改各有明确范围的补验，不能把旧快照当成当前源码的完整验收：

- CLI→agent IPC→TLS daemon→真实 tmux，原始终端字节、观察者/控制权、屏幕重绘实现、Token 撤销及崩溃恢复；光标/颜色/alternate-screen/resize重连已补549文件双平台独立VT语义验收。
- 项目级双向同步、共同基线与条件提交、首次同步计划、冲突保留/显式解决、后台watcher与重扫已有历史证据。本批日志、ACK丢失保护、旧状态迁移栅栏及pending删除准入代码完成且有开发专项结果，最终同源双平台验收已通过，共同基线项恢复勾选。没有文件级快照或三方自动合并。
- 默认 loopback 的 TCP 转发、显式授权和自动策略、半关闭、限额、状态与恢复。已有 TCP 连接中断后由应用重连，不回放旧数据。
- TUI、配置命令、资源/日志诊断、会话配置快照与模板。恢复配置不会回滚项目文件、Claude 进程或对话。
- 受信本地 bundle 的固定版本安装、状态备份、原子切换、回滚及保留数据卸载。真实 systemd 容器和原生 macOS LaunchAgent 已验收，不能外推为所有部署环境通过。
- agent 同版本五分钟内连续三次异常结束后保留诊断 IPC、停止业务自动恢复；修复原因后显式 `shiki agent start --recover-crash-loop`。损坏记录和时间回拨不由该命令绕过；配置/锁获取之前的明确错误不属于已取得实例身份的崩溃计数。

这些是开发源码的实现/验收范围，不是正式版本的平台支持承诺。真实 Claude 账号、官方 Tailscale 政策、物理多设备、原生 VPS、重启/登录持久化、生产长稳和最终签名分发仍有待验收项。GUI、WSS、增量同步、文件快照、终端历史回放和多租户仍在首版之后。

## 平台与证据矩阵

当前更正：默认连接准入发现client单连接拒绝取消整组转发，已按文档修复并恢复Phase3条目，当前29/42。633源码v2真实双端回归有效红绿、双平台受影响race/vet、普通完整混合负载及default128保持/释放一槽均通过；见[隔离修复](15-implementation-status.md#evidence-port-limit-isolation-validation)。原Linux12小时运行已于09:13:50失败于替换进程身份读取ENOENT，只有server-sync旧代到期退役得到导出证据，无完整恢复或30分钟尾部；见[轮换结果](15-implementation-status.md#evidence-default-rotation-validation)。

2026-10-29新增632文件源码的限定补验：host/SDK纯client-stream成功终态释放已由确定性红绿、真实UDS/HTTP2、双平台两整包race/vet及命名集成/终端语义验证。原普通构建混合负载的32轮macOS数据已独立复算，第二组准入补充RPC当时因测试路由与13冲突而失败。后续633源已完成双平台32轮/232流、默认128准入及释放一槽恢复；先前失败仍保留，容量结论仅适用于该负载和冻结源，不证明当前候选长期无泄漏。见[所有权报告](15-implementation-status.md#evidence-resource-ownership-audit)、[负载协议与勘误](16-architecture-conformance.md#evidence-load-capacity-protocol)、[实际结果](15-implementation-status.md#evidence-load-capacity-validation)。下列各历史快照保持实际范围。

新增630文件同源验收：固定版本HTTPS下载、映射迟到事件/响应、连接锁外探测及转发提交代际修复已通过双平台各8包、CLI、两条选定E2E及vet，无失败/跳过；完整范围见[本批验收](15-implementation-status.md#evidence-fetch-mapping-validation)。这是受影响范围补验，不能把下列历史全套结果外推到当前源码。原macOS默认12小时轮换/30分钟尾部/清理已通过，Linux原运行已在替换身份观测阶段失败，未完成四角色恢复及30分钟尾部；资源与正式发布门槛保留。

最新观测开销调查仅改8个诊断文件，生产不变；默认/延后短预检与唯一Linux五窗口配对已完成并停止。Agent末段对象差值+9.98/min，关闭后仍+24,240 B/+228对象；heap负值受高点回落影响，不能将小样本下降作为因果或无泄漏证明。实际长时运行与平台/发布门槛仍保留，见[观测开销报告](15-implementation-status.md#evidence-memory-observer-validation)。

| 环境 | 当前证据 | 仍缺的发布证据 |
|---|---|---|
| macOS 27.0.1 arm64 | 历史冻结源码完整race/E2E及后续明确范围补验；真实LaunchAgent生命周期及活动终端/TCP/文件升级回滚（此前下载87.29秒/同版上传85.03秒；529文件新安装快照上传90.41秒）、此前独立取消清理；崩溃预算；实际tmux 3.7b | 目标支持版本范围、重新登录/重启、现有用户迁移、真实账号与完整视觉体验 |
| Linux arm64 / Debian bookworm 容器 | 普通UID1000历史冻结源码完整race/E2E及后续明确范围补验；真实systemd 252.39 PID1/cgroup；tmux 3.3a；卸载/重装和poweroff | 原生 VPS/目标发行版、重启/登录、真实网络与账号 |
| macOS amd64 / Rosetta | 翻译执行版本、私有 IPC 停启 smoke | 原生 Intel 完整场景，不以 Rosetta 代替 |
| Linux amd64 / Docker 翻译执行 | IPC/TLS/真实插件/tmux 专项，内嵌插件运行 | 原生 amd64 完整客户端/daemon 场景与服务模型 |
| WSL | 尚无运行证据 | 目标 WSL/发行版、systemd --user 或明确手动启动路径、完整终端/插件/恢复验收 |

上一批507文件/418份源码冻结版本的macOS与Linux完整race/E2E均通过（各44包，E2E226.898/203.361秒），build/lint及架构复核通过，该批收尾为29/42；随后按03发现语义屏幕验收缺口，曾降至28/42；549文件双平台专项现已补齐该项，但新发现sync操作日志缺失及ACK未知结果覆盖风险，相关日志/旧状态迁移栅栏、pending删除准入、SDK固定目录FD及未来schema字段拒绝代码修复已完成，v3最终同源双平台验收已通过，当前29/42。此前502文件macOS的WatchStats取消及505文件Linux的tmux/session失败仍保留；原Stats因果未确认，不能把新快照通过追改为旧运行成功。该507文件历史快照的15个无测试包、6/9个跳过测试不算通过，详见[历史合并验收报告](15-implementation-status.md#evidence-conformance-regression-validation)。macOS原495文件/411份源码的实际12小时运行已于2026-10-29 02:37:12通过自然轮换、30分钟恢复尾部和清理；不覆盖随后生产修改。Linux原长跑已失败，原始结果见[默认轮换报告](15-implementation-status.md#evidence-default-rotation-validation)；短预检不是自然到期验收。完整平台、外部及发行门槛继续待验。

2026-10-29新增限定恢复证据：同630文件生产源码的持久卷第二次systemd PID1自动启动及数据面恢复通过；原A529/当前B630跨schema场景实际30次RPC验证恢复不复活已撤销Token。正常退出、精确清理与七类复核通过，无生产修改；前两次二次启动夹具失败保留。证据见[二次启动](15-implementation-status.md#evidence-service-boot-validation)与[跨schema恢复](15-implementation-status.md#evidence-schema-service-upgrade-validation)。不代表物理重启、产品自动恢复、历史正式产物或外部账号已通过，29/42保持。

同日另完成未登录真实系统tailscaled的21边界安装非干扰及内嵌tsnet独立pending状态验证，原生macOS LaunchAgent实际开发schema恢复/30RPC撤权/最终进程代际清理通过。系统state为2字节{}，已有账户身份、已认证共存、重启登录和正式历史发行仍待；详见[共存报告](15-implementation-status.md#evidence-tailscale-coexist-validation)与[原生schema报告](15-implementation-status.md#evidence-schema-macos-validation)。

版本工具链为 Go 1.26.10（module 最低 1.26.5）、tsnet v1.98.10、Connect-Go v2.0.2。平台实测详情见 [平台报告](15-implementation-status.md#evidence-platform-validation)；服务见 [systemd](15-implementation-status.md#evidence-systemd-validation) 和 [LaunchAgent](15-implementation-status.md#evidence-launchd-validation)。构建成功、翻译执行与原生完整验收是不同证据。

以下为历史批次的三个证据快照：529文件的安装预检/备份修复已通过双平台race、原生LaunchAgent上传90.41秒和真实systemd 13次边界；530文件的短TTL两侧×双向在途文件轮换八格通过；527文件的普通构建五窗口内存诊断完成，但Agent后段heap增长未归因。它们不构成当前仓库完整回归、默认12小时、提交后最终ACK丢失、无泄漏或发布验收。详见[安装](15-implementation-status.md#evidence-install-space-validation)、[在途轮换](15-implementation-status.md#evidence-host-rotation-inflight-validation)、[内存诊断](15-implementation-status.md#evidence-memory-windows-diagnostic)。

549文件历史终端语义双平台验收见[终端报告](15-implementation-status.md#evidence-terminal-state-validation)。545文件空载/负载配对四组已完成，Agent后段load−control heap斜率约62–65KB/分钟，仍未归因且快照早于sync修复，不能认定无泄漏；见[配对报告](15-implementation-status.md#evidence-memory-idle-control-diagnostic)。最终候选须重新对应实际源码和验收范围。 本批修复见[sync日志](15-implementation-status.md#evidence-sync-journal-validation)与[状态目录](15-implementation-status.md#evidence-state-directory-validation)；v3冻结568文件/457份Go/proto/module双平台各46包通过、15包无测试，6/10项skip不计通过，E2E204.984/192.045秒；v1路由/collector失败及v2删除完成时序夹具失败保留，见[合并验收](15-implementation-status.md#evidence-journal-conformance-validation)。

历史请求生命周期批次已完成Agent请求回调保留修复、专项race和Linux三组内存对照；完整回归及受影响夹具补验通过，生产输入相同、最终测试快照仅一处预算夹具差异，原失败保留。修后仍有残余增长，不宣称无泄漏。真实Linux systemd开发schema升级/旧版拒写/有限sync恢复/再次同步已验收；只覆盖停写且B无新业务变更的恢复，该历史批次尚未额外测撤销Token RPC；后续30次真实RPC已验证受控恢复不复活撤销Token（18次成功、12次Unauthenticated）。恢复仍是停写后的显式操作，不是产品自动恢复。见[请求生命周期](15-implementation-status.md#evidence-pool-request-lifetime-validation)、[跨schema服务](15-implementation-status.md#evidence-schema-service-upgrade-validation)。

## 安装、升级和回滚限制

目前使用可信源码构建或已经独立验证的本地 bundle。`SHIKI_VERSION=<固定版本> make release` 产生本机目标 tarball、release.json、SHA256SUMS、archive digest 和 checksums pin；默认不签名；显式配置 `SHIKI_SIGNING_KEY` 时追加本地 Ed25519 签名。它不会上传、创建 tag 或建立下载渠道。不同目标必须独立构建，不在同一工作树并行生成内嵌产物。

安装前验证整个归档和来源，之后再解包/运行脚本或 helper。对刚下载文件自行计算摘要只能发现后续变化，不能证明发布者身份。调用 helper 时仍必须传入独立可信的 `--checksums-sha256`；不能从未认证的同一归档读出一个值就称完成发布者验证。可信源码 verifier 配合独立公钥、固定 version/OS/arch 认证归档，再生成新私有副本与认证 pin；新增显式HTTPS下载入口先验签再取归档，安装仍使用本地认证副本，当前验收范围见15/16。操作说明见[固定版本发布认证](11-automated-installation.md#本地离线签名与验签)。正式签名密钥/身份、信任根分发、轮换和渠道尚未确立。

此前608文件冻结源码的实际完整`make release`已在macOS arm64和普通用户Linux arm64容器通过：一次性测试密钥签名、可信源码verifier配合独立公钥验签、认证私有副本解包、六二进制版本核对、真实helper暂存（macOS client；Linux client/daemon）。仅生成可审查服务文件，未激活或发布；详见[完整签名构建](15-implementation-status.md#evidence-current-release-validation)。此证据补齐先前旧developer归档的构建来源范围，不建立正式发布者身份。

- 安装器需要现存普通运行用户、明确服务 PATH、tmux 和可运行且已授权的 Claude。缺依赖会报告，不会静默代用户安装或登录。
- 现有配置/身份保持。全新非交互配置需要私有 key 文件；首次非交互 daemon 激活需要新的私有 Token 输出路径。凭据不放命令行参数值或 journal。
- 停止对应 host/worker 后再备份和切换；tmux 会话保留，但网络流需要重建。真实systemd容器活动传输中agent/daemon升级与回滚已专项通过（187.62秒、13次边界，同一pane保活），覆盖终端/TCP无重放、文件冲突保留及新监听恢复；证据快照不含随后Unix socket原子发布修改。原生macOS LaunchAgent的活动上传/下载/终端/TCP中A→B→A均已通过，此前下载87.29秒/同版上传85.03秒，含旧公开目标完整SHA断言；本批新安装源码另补上传90.41秒及systemd 13次边界验证；此前独立取消保留runner143/test1并验证清理，未重复追认到新夹具。daemon为普通TLS fixture。历史正式产物、重启/登录持久化和外部部署仍待验收，见[Linux升级报告](15-implementation-status.md#evidence-upgrade-validation)及[原生活动升级报告](15-implementation-status.md#evidence-launchd-activity-validation)。
- 本批sync ledger迁至服务独立schema2的修复已通过限定双平台验收，旧写入方须拒绝新ledger；迁移保留原始备份，旧schema1历史未知操作不能因初始化空日志就视为安全。迁移恢复栅栏及SDK未来schema字段拒绝已实现，SDK原生受影响race和Linux故障矩阵已通过；最终同源双平台合并亦已通过；这些是当前开发格式的迁移验收，不替代历史正式产物/所有既有用户组合验收。核心安装schema保持其自身版本，不能将所有状态统一升至2。
- `rollback` 切换旧产物，保留当前配置/认证状态。不会自动还原旧 Token 撤销表、项目文件或对话。未知未来 schema 拒绝使用；不能把“保留旧二进制”理解为任意状态格式可向下降级。
- 第一次安装没有上个产物可回滚。中断事务需要显式恢复；不要手工改 current 或删除 receipt 绕过检查。
- 卸载移除受管启动项/命令链接，保留配置、状态、项目、tmux、旧版本及备份。删除账号、节点、项目或全部状态是另一个有明确后果的操作。
- 退出码 0 仅表示报告的本地操作完成，3 表示登录或本地应用条件仍 pending，1 表示操作失败，2 表示参数错误。`local_application_ready` 不会把 `external_peer_acceptance=pending` 变为通过。

准确命令与参数见 [11 安装契约](./11-automated-installation.md)、脚本说明 及 `shiki-install help <command>`。helper 的帮助在读取配置、key、账号或安装目录之前返回。`--stage-only` 和布局/bundle/version 参数只适用于 install，维护命令只选 role/user/prefix，不能用忽略的参数模拟 dry-run。交互 probe 可能启动节点登录；不发行 Token 不等于所有 probe 都只读。

## 故障排除与诊断材料

| 现象 | 首先检查 | 恢复边界 |
|---|---|---|
| 配置解析/权限失败或未来schema | 保留原配置及任何 `.previous` 备份，按具体错误修复；需要恢复时先用兼容版本验证备份，再显式恢复并重启相应服务 | 加载器不自动覆盖原件或恢复旧配置；缺文件的首次初始化语义保留；未来schema应使用兼容程序，不能手改版本号向下读取 |
| 本地后台不可用 | `shiki agent status`，指定 `--config` 时与安装/服务一致；检查该配置声明的私有 agent 日志 | 不推断远端会话已停止；不要删除状态来强制新建节点 |
| `AGENT_CRASH_LOOP` | 查看 agent 日志并修复崩溃原因 | 再执行 `shiki agent start --recover-crash-loop`；普通 start 或停掉诊断进程不会清除熔断 |
| `AGENT_CRASH_STATE_UNAVAILABLE` / 时钟回拨 | 检查私有启动记录的权限/完整性、真实系统时钟 | 保留异常文件；不要盲目删除记录或用恢复开关绕过 |
| 节点离线/登录待完成 | `shiki doctor`，daemon 在对应安装 prefix 使用 `shiki-install probe --non-interactive`（不带 key/Token 发行选项） | 区分 node/login、daemon Token、TLS 和网络失败；认证拒绝不换路线绕过 |
| 重连预算耗尽 | 检查地址、证书、身份和凭据；按实际问题显式更新服务器记录 | 不无限拨号；重新连接不会重放旧终端输入或 TCP 缓冲 |
| 同步冲突 | `shiki sync status`，按当前冲突 generation 显式解决 | 重新观察两端；未知提交结果不能解释为已回滚；历史保留有上限，不替代备份 |
| 插件异常 | `shiki plugins status <id> --side local` 或明确远端作用域；`shiki logs <id> --side local --tail 100` | 维护匹配 manifest/产物；篡改或不兼容不会切换到宿主内置业务 |
| 升级中断或就绪 pending | `shiki-install status --role <role> --prefix <受管目录>`，读取 receipt；probe 检查当前状态 | 状态记录不等于应用 ready；有 pending 事务先按错误提示显式 rollback，不重新发行多余 Token |
| 撤销泄露的 daemon Token | 在正确配置下运行 `shiki-daemon token revoke <token-id>` | 只传 Token ID；撤销会关闭相关流但保留 tmux。不要用旧认证备份撤销这次安全操作 |

反馈材料应包括准确 build/OS/arch、操作与退出码、脱敏 reason_code、受影响范围、可复现步骤，以及对应的测试/运行日志位置。不要附 Token、auth key、登录 URL、账号原始输出、私有配置全文或项目内容。`shiki logs` 是宿主脱敏的插件日志，不能代替不存在的 daemon 文件日志；Linux daemon 默认查看相应 systemd journal。

## 发布前仍必须取得的结果

正式发行时，应以一个冻结源码候选重新填写版本、不可变提交/tag、实际通过的 CI 运行、平台/服务/外部账号矩阵、所有归档及其摘要、签名身份与验证方式、升级限制和真实支持入口。当前这些元数据未确定，不能用本地测试版本名或占位 URL 冒充。

必须补齐 [08](./08-roadmap.md) 的剩余门槛，再从本草稿删去不适用的草稿描述；不能仅因文档已写完就把发布条目勾选。持续负载要记录真实完成时长、尾部恢复与清理，性能结论需给工作负载和资源预算。隔离磁盘满、精确fsync错误及新进程恢复已有[故障专项证据](15-implementation-status.md#evidence-fault-validation)，不等同真实硬件掉电或全部提交点覆盖。当前依赖扫描没有受影响的已导入包，仍有未导入 OpenPGP 的模块级公告；见 [安全报告](15-implementation-status.md#evidence-security-validation)，不能写成“没有任何漏洞”。

仓库路径已确定为 `github.com/DDSRem-Dev/Shikigami`，但 Issues/安全报告渠道及维护承诺尚未作为正式支持入口验收。MIT 许可证位于 [LICENSE](../LICENSE)，本地发行包已包含并纳入摘要；尚无 Homebrew formula 或已发布安装源。不得建议用户执行占位域名的 curl 管道，或将本地产物/自签测试密钥当作官方发行。

历史502文件/415份源码冻结整套回归：macOS43包通过、真实client凭据轮换的WatchStats提前Canceled导致整套退出1（E2E224.579秒自身通过）；Linux44包通过、整套退出0（E2E199.408秒）。当时相关生命周期条目暂撤勾选；之后507文件快照双平台完整回归通过，按该版本证据恢复，原Stats事件因果仍未确认。终端项已由独立语义验收恢复；当前sync基线项已凭v3同源双平台最终验收恢复勾选。568文件版本整套race/E2E已通过，但其15个无测试包及6/10项skip不计通过，也不替代资源或外部验收，详见15/16及`/tmp/shiki-full-race-20261028-5h0eo7fo/summary.json`。
