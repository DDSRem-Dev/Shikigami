# 原型复盘、假设登记与条件工作量重估

复盘日期：2026-10-28。范围仍是 [08 路线图](./08-roadmap.md) 的完整 Phase 0–5；本文件为路线图提供估算依据，不另建排期、不改变设计契约，也不表示 Phase 0 或产品已完成。当前可执行环境是原生 macOS arm64 与本机 Linux 容器，未取得 VPS、外部 Tailscale/Claude 账号或其他物理测试设备。

原 14 周估计不能继续作为交付承诺。原型已经证明核心方案能够在限定环境中运行，但外部认证、历史发行组合、物理平台、生产容量/长稳和正式发布仍缺证据。复盘初期发现的登录事件生命周期、终端背压和agent崩溃熔断问题已经修复并取得对应验证；后续批次又补齐合成版本矩阵、持久化故障、容器活动升级、混合负载及真实宿主短TTL轮换。不能继续把这些已完成工作当作剩余工程量，也不能用局部证据关闭其外部或完整生产门槛。下方原工作量区间保留为历史规划，不是当前剩余工期或交付承诺。

## 当前结果与原工作包的关系

当前更正：默认连接准入发现client单连接拒绝取消整组转发，已按文档修复并恢复Phase3条目，当前29/42。633源码v2真实双端回归有效红绿、双平台受影响race/vet、普通完整混合负载及default128保持/释放一槽均通过；见[隔离修复](15-implementation-status.md#evidence-port-limit-isolation-validation)。原Linux12小时运行已于09:13:50失败于替换进程身份读取ENOENT，只有server-sync旧代到期退役得到导出证据，无完整恢复或30分钟尾部；见[轮换结果](15-implementation-status.md#evidence-default-rotation-validation)。

2026-10-29当前资源工作：632文件源码已完成成功client-stream终态取消注册释放修复及双平台受影响验证，不能据此解释旧heap斜率。普通构建混合负载已取得完整32轮的macOS原始测量，但第二组准入的测试路径误用了13禁止的前端代理，整轮仍失败；正在按设计修正测试并做独立复核。Linux原12小时已在替换进程身份采样阶段失败，无完整恢复/尾部；不自动重跑。工作安排为运行、失败定位、七类审查和证据同步并行；只有依赖结果的验收结论等待，详见[负载记录](15-implementation-status.md#evidence-load-capacity-validation)。

新增安装与恢复验收：同630产品源的容器真实未登录系统tailscaled在21边界不受Shiki影响，独立内嵌tsnet启用未登录的私有状态与EACCES检查通过；原生LaunchAgent A529/B630跨schema恢复、30次实际RPC撤权和最终代际清理通过。系统state为{}，不证明已有账户身份保持；两项不改变外部/物理重启/历史正式发行或资源门槛，完整范围见[共存](15-implementation-status.md#evidence-tailscale-coexist-validation)与[原生schema](15-implementation-status.md#evidence-schema-macos-validation)。

新增630文件同源验收：固定版本HTTPS下载、映射迟到事件/响应、连接锁外探测及转发提交代际修复已通过双平台各8包、CLI、两条选定E2E及vet，无失败/跳过；完整范围见[本批验收](15-implementation-status.md#evidence-fetch-mapping-validation)。这是受影响范围补验，不能把下列历史全套结果外推到当前源码。原macOS默认12小时轮换/30分钟尾部/清理已通过，Linux原运行继续；资源与正式发布门槛保留。

最新观测开销调查仅改8个诊断文件，生产不变；默认/延后短预检与唯一Linux五窗口配对已完成并停止。Agent末段对象差值+9.98/min，关闭后仍+24,240 B/+228对象；heap负值受高点回落影响，不能将小样本下降作为因果或无泄漏证明。实际长时运行与平台/发布门槛仍保留，见[观测开销报告](15-implementation-status.md#evidence-memory-observer-validation)。

当前资源批次已确定并修复Agent共享连接池请求回调保留，专项race及Linux修前/修后/同二进制空载对照完成。修后后段load−control仍+15,597 B/min、+92.35 objects/min；不能把修复单一原因视为资源稳定。完整回归及受影响夹具补验已通过，生产输入相同、测试快照差异明确记录；真实Linux跨schema升级/拒写/有限恢复已验收，历史同源码A/B结果不代替格式升级验收。证据见[请求生命周期](15-implementation-status.md#evidence-pool-request-lifetime-validation)。

当前勾选数量与缺口以[08路线图](./08-roadmap.md)顶部为准，不在估算文档重复维护动态计数；新回归失败及补验会改变验收状态。2026-10-28开始的macOS默认12小时独立宿主实验已完成自然到期、恢复尾部和清理验收；Linux原长跑继续，两侧不同源码分别验收，状态与句柄见[默认轮换报告](15-implementation-status.md#evidence-default-rotation-validation)。

| 原工作包 | 当前已有结果 | 仍不能视为完成的部分 |
|---|---|---|
| L1 失败与生命周期收敛 | 登录generation、终端背压/终态、agent熔断、节点/共享连接修复已有证据；终端549语义补齐；sync日志/迁移栅栏/pending删除及SDK修复在v3同源双平台race/E2E通过 | 历史失败与因果限制保留；功能回归不外推为长稳、资源稳定或发布验收 |
| L2 版本与活动升级 | 四层独立进程合成版本矩阵；真实systemd容器活动升级回滚；原生macOS LaunchAgent上传/下载活动升级回滚及独立取消清理通过；新529文件安装修复快照上传90.41秒/真实systemd 13次边界补验通过 | 历史正式产物、重启/登录持久化、外部部署与物理多设备 |
| L3 持久化故障 | 五个fsync EIO点及新进程恢复、两个ENOSPC分支、rename/撤销失败；sync日志/ACK丢失/legacy栅栏与SDK状态修复完成，Linux故障矩阵及v3同源双平台通过 | 任意提交窗口、硬件掉电和全部文件系统组合 |
| L4 混合与轮换工具 | 双平台十分钟混合、两侧30秒TTL轮换及在途八格、默认12小时工具/预检、端口分配优化、五窗口内存诊断与545空载/负载配对已完成；sync修复后恢复回归通过 | macOS原默认到期/尾部/清理通过，Linux原运行仍待；Agent后段heap增长未归因；完整容量/资源预算与生产长稳 |
| L5 发布准备 | 18草稿、平台矩阵、升级限制、校验与支持入口缺口已有材料 | 最终候选、远端CI、签名身份、下载/分发和真实支持入口 |

证据分别见[版本](15-implementation-status.md#evidence-version-validation)、[活动升级](15-implementation-status.md#evidence-upgrade-validation)、[持久化故障](15-implementation-status.md#evidence-fault-validation)、[混合负载](15-implementation-status.md#evidence-mixed-load-validation)、[真实宿主轮换](15-implementation-status.md#evidence-host-rotation-validation)和[内存诊断](15-implementation-status.md#evidence-memory-profile-diagnostic)。终端549与内存545配对证据见[终端语义](15-implementation-status.md#evidence-terminal-state-validation)和[空载/负载对照](15-implementation-status.md#evidence-memory-idle-control-diagnostic)，后者早于sync日志修复、不能用于修复后的资源结论。新增证据见[安装空间](15-implementation-status.md#evidence-install-space-validation)、[在途轮换](15-implementation-status.md#evidence-host-rotation-inflight-validation)及[多窗口内存](15-implementation-status.md#evidence-memory-windows-diagnostic)。这些进展不改变00–14的设计要求或08的42项范围。 本批代码修复和最终验收分别见[sync日志](15-implementation-status.md#evidence-sync-journal-validation)、[状态目录](15-implementation-status.md#evidence-state-directory-validation)及[合并验收](15-implementation-status.md#evidence-journal-conformance-validation)；v1/v2失败保留；v3同源568文件/457份Go/proto/module双平台各46包通过、6/10项skip不算通过，当前29/42以08为准。

## 已验证的事实与适用边界

| 原型或交付部分 | 已有证据 | 不能由此推出的结论 |
|---|---|---|
| 三宿主、项目/会话和终端闭环 | CLI→agent→TLS daemon→真实 tmux 的本地 E2E；控制者/观察者、重连、撤权、SIGKILL 恢复；上一批冻结源码两平台完整 race/E2E | 真实 Claude 账号可用、所有终端视觉行为、外部网络稳定；历史通过不能覆盖新源码 |
| 协议与传输 | 09/13 四份 schema 与生成代码对照；UDS/TLS/本地 testcontrol 下 tsnet 四种 RPC、取消/半关闭/最终错误/准入测试 | 官方控制面和任意网络路径通过；HTTP 200 或 unary 成功不能替代完整流语义 |
| 插件边界 | 入口/业务在 `plugins/sync`、`plugins/port`；host 在 `internal/plugin`，SDK 在 `pkg/plugin`；真实 go-plugin + Connect 回调及 `test/architecture` | 插件业务可回移宿主作进程内 fallback；匹配的源码不等于签名发行产物已可信 |
| 持久状态与数据边界 | 原子状态、revision/request_id、撤销、条件提交、恢复日志、受根文件操作；跨 Token 重放重新授权与状态失败测试 | 任意外部写者上的文件系统强 CAS、掉电后所有文件系统都无不确定结果、所有故障点已注入 |
| systemd 保活 | 真实 systemd 252.39 为 Linux arm64 容器 PID1；普通 UID1000 daemon/user-agent；同一 pane 跨 11 项停启/崩溃/升级/回滚/重装；旧插件回收，实际 poweroff 退出 0 | 原生 VPS、其他发行版/CPU、主机重启后的进程存活、账号/外网验收 |
| 原生 LaunchAgent | macOS 27.0.1 arm64 当前 GUI 登录域，真实安装/自动恢复/显式停止/升级/回滚/卸载保留数据；完整流程 67.41 秒，取消清理另验 | 重启/重新登录持久化、既有用户安装迁移、其他 macOS/CPU |
| 平台范围 | macOS arm64 与 Linux arm64 容器实际运行；Darwin amd64/Rosetta smoke、Linux amd64 翻译执行专项与内嵌插件 | 原生 Intel、WSL 或完整 amd64 验收；交叉编译不能充当运行证据 |
| 资源与性能 | 双平台各24轮资源回收、一小时终端/双向同步、十分钟混合实验各有对应快照；普通/race诊断和端口分配优化有独立前后证据 | 无所有类型泄漏、生产容量、WAN 性能或正式长稳；RSS/CPU 观察值不是通过阈值 |
| 安全扫描 | 全仓库 package 扫描，四发布目标静态分析；compress 已升级 1.18.7；65 处状态读取调用审计 | 所有依赖/源代码无漏洞；未导入 OpenPGP 的模块公告仍需保留；静态目标扫描不是目标硬件执行 |

证据入口：[15 实施记录](./15-implementation-status.md)、[16 一致性检查](./16-architecture-conformance.md)、[systemd](15-implementation-status.md#evidence-systemd-validation)、[LaunchAgent](15-implementation-status.md#evidence-launchd-validation)、[平台](15-implementation-status.md#evidence-platform-validation)、[资源循环](15-implementation-status.md#evidence-resource-regression)、[持续负载](15-implementation-status.md#evidence-soak-validation)、[传输基准](15-implementation-status.md#evidence-benchmarks)、[安全](15-implementation-status.md#evidence-security-validation)。测试运行时间只描述对应场景，不是实现工作量。

## 已被否定、尚未满足或必须限定的假设

| 假设 | 原型结论 | 对交付和估算的影响 |
|---|---|---|
| 首版只是 CLI 和远端 daemon，原 14 周估计可沿用 | 已否定。本地常驻 agent、双端插件、四层协商、安装事务及恢复均属于既定首版范围 | 必须按完整三宿主/双端边界估算，不能删减这些工作使估计成立 |
| Go 1.26.3 足以运行选定 tsnet，升级 SDK 不影响基线 | 已否定。固定 tsnet v1.98.10 要求 Go ≥1.26.5，当前工具链 1.26.10；更高 SDK 有不同 Go 要求 | 锁定并记录当前组合；SDK 升级需重新验证隔离的 Sys/netstack 适配和完整节点行为 |
| 手工启动通过即可推定 systemd/launchd 安装成功 | 已否定。真实服务测试发现 cwd 引号、插件产物/状态归属和 disabled-node 状态偏差 | 服务渲染、服务激活、本地应用就绪、外部 peer 可用性必须分别验证 |
| 所有安装器管理目录都应是运行用户的 0700 私有目录 | 已否定。私有状态与 root-owned 不可变插件 release 是不同对象 | 保留 manifest/owner/mode/hash 检查，不能把产物当状态改权限；升级/回滚必须验证二者边界 |
| 本地 testcontrol 能证明一次性 key、审批/撤销与外部 P2P/DERP | 尚未满足。测试控制面并不完整模拟官方政策、真实网络和账号状态 | 保留官方账号/网络门槛，无法用更多同机测试换成完成 |
| CancelLogin 能保证服务端登录 URL 立即失效 | 当前 SDK 不提供该承诺。已验证本地 attempt 取消和迟到结果不应重新激活它 | 明确本地取消范围，不对外声称服务端 URL 撤销；外部生命周期另验 |
| 任何 tmux 平台都能可靠恢复退出原因 | 尚未满足。Linux tmux 3.3a 偶发无可信退出原因 | 继续报告 unknown 且不自动重启，不伪造 exit code；完整平台验证仍需覆盖 |
| 文件替换原子性意味着未知结果可安全重放或一定回滚 | 已否定。交换后fsync/历史保存/ACK失败可能已改变目标；内容revision可随用户回退重复，旧schema1没有操作日志也不能证明没有未知提交 | 新日志区分确定未发完整输入与可能提交；迁移已保留旧基线并加入持久恢复栅栏，最终双平台验收已通过；硬件掉电/全部文件系统仍需另验 |
| 一次全套通过、CLI-only 依赖扫描或交叉编译可以代表最终质量 | 已否定。后续合并暴露真实竞态；CLI-only 扫描漏掉其他入口图；翻译执行不是原生 | 每次生产修复重新冻结并运行相关检查，所有入口扫描；保持证据与源码/平台对应 |
| 持续测试只要保持全部 PID 不变并延长时长即可证明长期稳定 | 已否定。worker 凭据默认 12 小时生命周期会合法替换进程，原测试证书也曾不足以覆盖运行尾部 | 当前保持 PID 的工具限定 1 分钟至 8 小时；跨轮换负载需要单独断言和测试，不能取消安全轮换来让测试通过 |
| 本地 checksum、占位下载 URL 和帮助示例等于发布渠道已建立 | 尚未满足。只有本地产物、固定摘要、版本匹配和安装工具证据 | 正式签名信任链、实际下载/分发/支持入口、远端 CI 必须有独立可核查结果 |

这些限制不降低 00–14 的行为要求。GUI、WSS、文件级快照和三方自动合并仍按 08 列为后续范围；已有的会话配置快照不承诺回滚文件或 Claude 对话。

## 历史工作包与当时条件工作量（不作为当前剩余量）

估算单位为一名熟悉本项目的维护者的有效工程工作日，含实现、审查、相关测试和 08/15/16 同步；不是代理运行的墙钟时间。假设当前协议/产品范围不新增、现有 macOS/Linux 构建环境可用、可复用已有夹具、缺陷为局部修改、没有数据模型重构。尚无经过测量的历史交付速度，所以区间不能解释为统计置信区间；出现新架构问题须重估。

| 本地工作包 | 具体退出条件 | 条件工作量 |
|---|---|---:|
| L1 失败与生命周期收敛 | 登录generation、终端背压/终态、agent熔断、节点/共享连接修复已有证据；终端549语义补齐；sync日志/迁移栅栏/pending删除及SDK修复在v3同源双平台race/E2E通过 | 历史失败与因果限制保留；功能回归不外推为长稳、资源稳定或发布验收 |
| L2 跨版本和活动升级恢复 | 使用明确版本差异的实际进程覆盖四层拒绝/允许边界；双端有活动传输/终端时升级、回滚、重新协商/恢复；不重放输入、不静默覆盖文件、不遗留 worker；与状态 schema 升降级边界对应 | 2–4 日 |
| L3 关键持久化故障补证 | 列出已覆盖/未覆盖的提交点，在隔离环境补磁盘满、写入/rename/fsync 失败和重启恢复；验证未知结果重观察、旧内容保留、认证状态不回滚；不能模拟的真实掉电条件仍标未验 | 2–4 日 |
| L4 完整负载与轮换验证工具 | 从10的限制形成可复现终端+同步+TCP混合负载、并发/大小组合和观察指标；独立覆盖 worker 凭据到期替换和原有任务恢复；明确 RSS/CPU/延迟判据及依据后再冻结执行 | 3–5 日 |
| L5 发布准备材料 | 基于最终候选源码生成发布说明、升级/回滚限制、支持平台证据矩阵、签名/校验操作方案、剩余外部门槛；所有真实入口尚无证据时明确留空，禁止伪造 URL/签名/发布状态 | 1–2 日 |

在复盘初期上述假设下，L1–L5 当时的已知主动工作合计约 **10–19 个工程工作日**，是新增终端背压和崩溃熔断缺口后的规划重估（L1由1–2日调整为2–4日），**不是完整 Phase 0–5 剩余交付工期**。各工作包仍可能发现未计入的生产缺陷。并行只能用于文件和状态互不干扰的实现/审查；最终冻结验证、串行生命周期及观测时长不能用增加代理数量等比缩短。不可将这一区间作为“这些天之后可以正式发布”的承诺。

实测可确定的运行下限单独记录：上一批完整 E2E 包约 3 分钟，24 轮资源场景约 5 分钟，均不含所有构建和其他包耗时；一次一小时持续负载必须实际运行满该负载时段并通过尾部恢复/清理。完整跨凭据轮换试验必须覆盖选定生命周期边界；当前 1–8 小时固定 PID 工具不能承担它。生产长稳与容量的工作负载、持续时间、资源预算和环境尚需明确，不能先给一个任意时长再将其通过当作整个 Phase 5 完成。

以下保留复盘初期过程，后续结果以本节末尾和当前结果表为准：当时一小时 macOS 运行在前置撤权阶段失败：CLI在15秒等待内未退出，尚未进入一小时持续负载，因此不是时长验收通过，也不能解释为运行满一小时后失败。Linux一小时运行仍在执行，完成前不记通过；此前主动中止的尝试也不计时长。后续发现的终端背压缺陷已由旧生产代码上的确定性PTY回归复现，尚不能证明它是首次15秒超时的唯一原因，独立接收/有界输出队列修复及EOF/溢出验证正在进行；另发现Send关闭会抢占Receive最终状态，已由真实PTY/UDS延迟末帧测试在缺少仲裁的隔离源码复现（干净EOF输出丢失、认证状态被发送EOF覆盖），加入有界仲裁后该专项10次race在macOS/冻结Linux分别19.022/6.864秒通过。诊断节点不可用登录能力的专项已通过；node旧generation登录覆盖的确定性回归先在旧实现失败，修复后完整node包10次race在macOS/冻结Linux arm64分别206.603/168.413秒通过，未放宽时间或断言。06规定的同版本5分钟内连续3次崩溃熔断已补齐，真实CLI/并发启动及原生LaunchAgent/容器systemd三次SIGKILL复验通过；明确配置/锁前错误不属于已取得实例身份的崩溃计数。本批L1修复已通过新冻结双平台完整race/E2E（207.127/177.266秒），失败和红绿对照仍在15/16保留；该结果不关闭L2–L5或外部门槛。最新macOS一小时实验已重新进入持续计时，两平台实验完成前仍不计通过。

后续一小时实测已完成：Linux 1h0m0.384s、1,577轮、491,651,072字节；macOS 1h0m0.107s、1,540轮、480,378,880字节，均通过各自尾部与清理。它们早于若干后续修复，不能当作后续修复源码的长稳证据。双平台十分钟混合与后续端口优化的一分钟混合补验也已完成，默认12小时macOS原运行已通过，Linux原运行继续。原10–19日是已含后来完成工作和当时未知缺陷的历史估算，不能直接用作当前剩余量；当前可确认的是实际观测时间下限，不能凭测试耗时推算完整交付日期。

## 依赖外部条件的工作，不计入上述本地合计

| 外部门槛 | 执行前必须具备 | 最终应取得的证据 |
|---|---|---|
| 官方 Tailscale 认证与路径 | 有权限的测试 tailnet、可处置的测试节点/key、至少两个实际网络端点及网络路径控制 | 一次性复用/过期/审批/拒绝/撤销、重新登录/身份保留、P2P/DERP 切换、失网/恢复；脱敏事件与实际状态，不能保留 key/公开登录 URL |
| 原生 Linux VPS 和已有 Tailscale 共存 | 可隔离的普通用户部署环境、实际服务管理器及受控重启窗口 | 安装/重复/自定义配置/无TTY、保活/清理、重启与登录服务持久化、系统 tailscaled 未被修改、TLS/防火墙行为 |
| 真实 Claude 与物理多设备 | 已授权的 Claude 账号和使用安排、至少两个实际客户端/网络环境 | 实际终端/账号闭环、各自映射、控制权竞争、离线冲突、后台同步与通知体验；fixture 结果不得充当账号验收 |
| 完整平台 | 原生 macOS Intel/ARM、目标 Linux、WSL 环境及其服务能力 | 目标平台完整核心/插件/终端/安装/恢复验证；记录 OS、CPU、工具链及任何 skip，不用翻译运行替代原生 |
| 正式发布和远端 CI | 经确认的仓库权限/版本、可信签名身份与分发源、真实支持入口 | 远端 CI、固定产物/签名/验证链、下载中断/篡改拒绝、渠道安装/升级及公开资料一致性 |
| 生产长稳/容量 | 代表性的持续负载、机器/网络/磁盘预算、明确成功判据及足够观测时间 | 完整负载下终端延迟、同步正确性、TCP半关闭/容量、资源增长、轮换/恢复与故障观察；单独微基准不能替代 |

这些门槛的环境取得时间、账号审批时间和发现缺陷后的修复量都未知，当前不能给出可信总历时或固定上界。环境齐备后先做最小可用预检，再按实际平台/网络组合估算执行和复验时间；不能仅把它们标成“等待用户”而停止仍可进行的本地工作，也不能擅自假设外部环境已经可用。

## 后续重估与完成判定

每个批次结束时，核对生产源码/测试快照、实际输出及剩余门槛，先修复文档偏差，再同步 08/15/16。出现 SDK/Go 升级、协议或状态迁移、服务/文件系统语义变化、新平台失败、负载增长趋势或外部认证与本地模型不一致时，重新估算受影响工作包。

本复盘完成的是“已有原型事实、失效假设、剩余工作和条件估算”的记录，不是外部门槛的替代证据。是否勾选 08 中原型复盘条目由同批审查和三份进度文档一致性决定；其他条目仍逐项要求完整证据。完整 Phase 0–5 交付须在原 42 条要求及对应设计验收范围全部有证据后才能声明。

最新配对诊断见[空载/负载报告](15-implementation-status.md#evidence-memory-idle-control-diagnostic)：545文件同平台同二进制四组完成，负载后段Agent heap斜率高于空载约62–65KB/分钟，仍未归因，且早于本批sync日志修复。终端549文件语义验收见[终端报告](15-implementation-status.md#evidence-terminal-state-validation)。
