# 实施与验收记录

**首个全自动预发布完成（2026-10-29）：** [v0.0.1-alpha.1](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-alpha.1)已由受保护tag触发[Release 40895007350](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40895007350)，固定源码`f0b5f83dce599c8e0d715bda5e7806a0ef6aaa72`，完整Ubuntu/macOS CI、四目标构建、隔离签名/验签和公开发布全部成功，无人工审批。公开22资产已下载至`dist/releases/v0.0.1-alpha.1/`，以可信仓库公钥独立验证签名、payload/hash/pin、来源回执和远端摘要；24二进制版本/提交/Go平台正确，4个shiki保留的BuiltAt核对通过，另20个被链接器消除，未冒充静态验证。实际macOS arm64与普通用户、断网Linux arm64各12条版本/帮助及共6次首次/重复stage-only通过，无服务激活，输入hash保持且临时资源已清理。92cafaf旧签名包的真实服务新装/升级回滚证据仍单独保留，不追认为新归档全生命周期验收。公开Release及Issues入口HTTP200；工程发行地址与发布材料两条原要求关闭，原42条文字/顺序保持，当前31/42、11项待完成。七类架构复核无新增生产偏差；仅CI和Release活动工作流保留。Homebrew、密钥轮换/撤销、真实账号/网络、原生平台/重启登录、物理多设备及长期资源门槛继续待验；原697长测本批未操作或重新判定。详细证据见[公开alpha验收](15-implementation-status.md#evidence-public-alpha-release-20261029)。

**真实签名包验收与tag自动发布准备（2026-10-29）：** 用户授权首个v0.0.1-alpha.1及后续版本均由tag自动构建/签名/公开Release。92cafaf源实际签名候选已完成本机macOS arm64和Linux arm64容器验收，未重建产品：原生LaunchAgent用目标包自身helper完成开发.5→alpha→.5升级回滚69.72秒，另完成私有新prefix/state的新装、双插件、重复安装和卸载；真实systemd252.39完成13生命周期及4活动升级回滚边界、8MiB文件/终端/TCP与身份/状态保留，另完成签名alpha的全新daemon/user-agent安装、四插件、重复安装及卸载。正常服务/PID/容器清理与认证payload后置hash通过；旧native69.17秒只用A-helper的较窄结果、Linuxfresh重复安装把正确unchanged误期望ready的夹具失败及正常清理均保留，修正后通过。生产Go/proto/module及安装/构建包装输入与92cafaf一致，测试夹具支持预构建包；新tag仍会重建，旧验收不冒充新归档字节或构建元数据。已实现release.yml的tag精确来源/master校验、同源完整CI、无密钥四目标构建、隔离签名/验签、22文件及远端digest核验后从草稿公开；保留CI并删除/禁用旧手动sign-release工作流，历史记录保留。38项工具测试、actionlint、真实verifier四目标临时密钥签名及归档/签名篡改拒绝通过。GitHub环境已读回确认仅v*tag、无人工审批、禁止管理员绕过；规则集26129256禁止版本tag更新/删除，无绕过主体，既有私钥不变。七类复核无生产边界/RPC/状态/生命周期/I/O/安装语义偏差；首发说明和真实Issues反馈入口已准备，原42条/29勾选保持，实际tag工作流及公开下载待验证。既有账号、物理/原生平台、长稳/资源、Homebrew及密钥轮换撤销门槛保留；原697长测未改动。

**预发布签名完成（2026-10-29）：** 最新master当时提交`92cafaf88d834e84678186aa4e083e35e0439d88`的[CI 40872062129](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40872062129)最终六项全部成功；首次macOS任务因托管runner容量不足、未执行步骤而取消，只重新调度该任务后成功。[签名运行40877214329](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40877214329)固定`v0.0.1-alpha.1`及上述源码/签名工作流SHA，来源gate、四目标构建上传全部成功；GitHub审批记录确认DDSRem批准release-signing，签名和独立验签及signed artifact上传成功。22文件已下载至`dist/v0.0.1-alpha.1/`，包含四目标归档/hash/pin/签名/回执、公钥与PROVENANCE。本机从可信仓库编译verifier，用仓库公钥逐包验签，再核对14成员与全部payload hash、pin、版本/OS/arch、22文件精确集合、来源CI/签名run/SHA及远端回执，四目标全部通过；不以包内公钥作为唯一信任来源。七类复核确认本批仅执行既有发布流程，生产目录/依赖、RPC/授权、持久状态、生命周期、I/O、CLI/安装契约无变更；审批与分阶段持钥边界保持。原42条/29勾选不变，尚未创建tag/Release或激活部署；真实签名包安装/运行、公开分发、轮换/撤销演练及既有外部/平台/长期资源门槛仍待，旧`.5`和697长测范围不追认。后续文档提交不改变此固定产物来源。

**凭据轮换验收预算修正与远端通过（2026-10-29）：** 仅文档提交`9fe8e22`的[CI 40867762758](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40867762758)中macOS与四目标打包成功，Ubuntu的`TestRealHostCredentialRotation/client`在旧worker回收、端口恢复成功后，显式解决冲突RPC报DeadlineExceeded；共享探测与端口扫描均通过。独立源码复核发现hostrotation把启动探测用的显式1秒Timeout保留到整场业务客户端，覆盖了文档13的短RPC默认5秒、启停15秒；恢复默认Options，启动await原2秒单次/20秒整体、100秒场景与30秒凭据生命周期均不变。增加既有客户端deadline表对ResolveConflict默认/显式预算的验证，保留父截止优先；无生产逻辑、重试或业务断言修改。旧配置本机单次未复现失败，不把远端日志未提供的精确耗时或磁盘争用当作已证实原因。修后定向race组通过：客户端deadline测试1.530秒，真实hostrotation双角色78.115秒，client/server分别36.01/39.42秒；冲突代际保留后显式解决、双向hash、TCP无重放/半关闭尾部、暂停/停止意图、旧PID回收和socket替换全部通过。七类复核确认目录/依赖、RPC/授权、持久状态、进程生命周期、I/O语义和CLI/安装契约未改，验收夹具恢复默认契约。默认12小时夹具的1秒客户端仅用于临时探测、业务客户端本已5秒，现有实验保持原样；原42条/29勾选、外部与长期资源/签名发布门槛保持。CI 40865903512的历史成功保留，但不抵消本轮新失败。远端复验已完成：提交`8f4e4fac83fac5ad73fcafe2ccc53a3f3ff6bb4c`的[CI 40870240722](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40870240722)六项job全部success；Ubuntu/macOS的hostrotation整包race分别217.851/225.350秒通过，agent/client、生成漂移、依赖扫描、构建/vet/race/E2E与独立终端语义均通过，四目标未签名归档/hash/pin校验通过。此记录只属于该精确提交及CI实际配置，条件跳过不计验收；后续文档提交需按自身CI结果判断。

**远端CI修复闭环（2026-10-29）：** 提交`68f93b0bfb6f09743bb17f3e43f57561276fc785`的[CI 40865903512](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40865903512)已完成且六项job全部success：Ubuntu/macOS的生成漂移、依赖扫描、构建/vet/race/E2E及独立终端屏幕语义检查通过，Linux/macOS × amd64/arm64四目标未签名打包及摘要/pin校验通过。两平台工具测试各26项通过；agent包race分别31.933/32.122秒、port server 3.289/3.410秒，架构测试通过，前两轮暴露的IPv4接口后缀与共享探测取消竞态已在真实远端整套CI中复验。七类独立源码/文档复核无新增确定性偏差；未放宽预算、断言或增加失败重试。08/15/16同步，原42条要求/顺序及29项勾选保持。此证据仅覆盖该精确提交和CI实际配置，条件跳过不算验收，既有外部账号/物理平台/原生部署/长期资源与签名发布门槛仍待；旧失败、旧`.5`归档和697长测范围均保留。后续仅更新本次记录的文档提交，其CI须按各自运行结果判断。

**共享探测取消竞态修复（2026-10-29）：** [CI 40862770945](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40862770945)的四目标打包及macOS测试通过，Ubuntu仅agent共享终端探测用例失败；上一批端口server/真实port集成已在该Ubuntu运行通过。独立源码核对确认：probe到期时AfterFunc可先取消transport子context，Connect unary Send直接返回本地Canceled并绕过Receive归一，旧分类误将其作为共享连接故障。仅当父调用有效、错误非远端、probe确已DeadlineExceeded时，把本地Canceled归入probe超时；父取消/远端取消及超时/身份、认证、协议、证书拒绝仍保持原优先级。真实HTTP2共享终端的受控取消顺序用例修前精确红（original_bad=true），修后8个原/新增子场景、错误优先级及两项路由认证/回退测试race通过（24.648秒），agent vet和架构测试通过；400ms/5s预算、重试上限和断言未放宽。七类复核确认目录/插件依赖、协议/能力/授权、持久状态/幂等和CLI/安装布局未变，调用/共享连接生命周期与错误语义按原契约修复。未启动新长测或重复Linux编译；推送后由真实Ubuntu/macOS CI验证。原42条/29勾选和平台/资源/发行门槛保留，旧失败不改记通过。

**首轮远端CI与IPv4扫描修复（2026-10-29）：** master提交`a8774d4`的[CI 40859588149](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40859588149)已结束：四目标打包和macOS测试成功，Ubuntu测试失败；不是CI整体通过。Ubuntu真实ss输出`127.0.0.53%lo`触发IPv4解析失败，连带端口所有权、真实监听发现和Watch准入三个测试失败。仅在port服务parseSS IPv4分支验证并去除单个非空接口限定，保留原IP/端口/未知owner；IPv6 zone、lsof、授权及限额不变，畸形记录仍整次失败。同测修前race精确复现，修后native server/真实port integration/architecture三整包race（4.257/15.101/1.380秒）与三包vet通过；未追加Linux容器，Ubuntu实际链路待修复后远端CI复验。七类复核确认插件业务仍在plugins/port、SDK/宿主边界与RPC/持久状态/生命周期/CLI安装契约保持；此为明确生产修复，旧.5及697长测不包含它，不追认旧归档或旧源长稳。与Actions签名配置一起提交master，签名gate继续拒绝失败的旧CI；原42条/29勾选保持，完整CI及平台/资源/发布门槛仍待。

扫描修复证据：`/var/folders/1n/q373cln10fqb0gl9z1ds5d8m0000gn/T/shiki-scanner-ipv4-device-20261029-1e5si8x0/summary.json`；原Ubuntu失败日志：`/var/folders/1n/q373cln10fqb0gl9z1ds5d8m0000gn/T/shiki-ci-40859588149-3chdt4cn/ubuntu.log`。

**Actions自动签名配置（2026-10-29）：** 用户选择Actions签名并授权新建专用Ed25519密钥与本机私有备份。公钥`configs/release-signing.pub`及SPKI SHA-256指纹`e21f6dd671ea5042c43c492b759b933dda73985f89a823b43c651ff7c787c17d`已确定；GitHub `release-signing`环境已实际配置并读回复核：DDSRem必需审批、仅master、禁管理员绕过，私钥Secret与公钥Variable均已设置。备份目录0700/私钥0600，密钥对已本机验证。手动工作流严格选择同仓库成功CI push/master的精确commit，四目标无密钥构建，可信workflow提交预编译verifier，审批后才单步持钥签名、公钥匹配和逐包验签；仅上传Actions artifact，不创建tag/Release。实际CI运行40859588149在未完整成功时被新gate拒绝且未写来源输出；5项Python行为测试、actionlint、实际签名shell的四目标微型归档/临时测试密钥/真实Go verifier通过，产出22文件；公钥及私钥不匹配两负例均失败；另只读验证旧.5两真实归档通过，不追改旧源码范围。七类独立架构复核无新增确定性偏差；新增逻辑均属发布工具，产品边界/RPC/授权/状态/生命周期/I/O/安装契约未变。远端签名流程尚未执行，审批/发布和原平台/资源门槛保持；原42条及29勾选不变。操作、备份、信任及轮换/撤销限制见[发布准备](./18-release-readiness.md#github-actions-自动构建签名与发布)。

本批本地签名编排证据：`/var/folders/1n/q373cln10fqb0gl9z1ds5d8m0000gn/T/shiki-sign-actions-smoke-vp5exv28/summary.json`；环境配置与公钥身份记录：`/tmp/shiki-release-signing-setup.json`（不含私钥内容）。

**预发布版本与提交审阅（2026-10-29）：** 用户选择`v0.0.1-alpha.1`。已整理首次实现提交范围：CLI/agent/daemon、sync/port独立插件与SDK、协议、状态/认证/安装、测试工具、CI及docs；Git候选共652文件，bin/dist/工具缓存/临时证据不纳入。按用户要求，CI中四处Action引用改为GitHub官方latest稳定Release标签：checkout v7.0.1、setup-go v7.0.0（2026-10-29查询）；已核对两者action.yml的Node24运行时及当前使用参数，工作流仍为push/pull_request和只读权限，不使用提交哈希引用。现有CI配置经actionlint检查通过，涵盖macOS/Linux检查与四目标未签名打包；CI仍使用独立`v0.0.0-ci`，不冒充预发布产物。完整首次提交检查另修正7份Python脚本的无效尾部空白（AST一致），以.gitattributes将两份终端raw样本标为binary，样本字节/hash保持；本批仅版本记录和提交准备，用户已授权将审阅后的改动直接commit到`master`并推送触发CI；不创建发布分支、tag或Release。提交与执行状态以master实际提交历史和GitHub CI运行记录为准，授权或本地检查不算远端验收通过。七类复核确认生产结构、RPC/授权、状态/幂等、生命周期、I/O语义、安装契约均未变，版本命名不增加能力或平台声明；原42条/29勾选及资源/外部门槛保持。

**首发兼容基线确认（2026-10-29）：** 用户确认项目从未通过任何渠道对外发布可用版本；上一轮GitHub只读检查亦显示Release和tag均为空。历史正式版本迁移在本次首发没有验收对象，记为“不适用：无历史发行”，不是测试通过；旧批次中的“等待历史正式产物”由本说明更新。开发A/B版本的schema迁移、未来schema拒绝、协议/插件版本边界、升级失败恢复、回滚及数据/认证保留要求继续适用；首次对外发布后应保存固定源码、产物和状态格式，作为后续真实升级基线。七类一致性复核确认本批仅澄清验收对象，目录依赖、RPC/授权、状态、生命周期、文件/终端/网络、CLI/安装和能力声明无变更；不重跑行为测试。原42条及29勾选保持，物理设备、账号、原生部署、资源与发布门槛继续待验。用户已确定首个预发布版本为`v0.0.1-alpha.1`；本批未提交、推送、创建tag或发布。

## 历史验收摘要与原始证据

按用户要求，源码目录中的独立 Markdown 验收报告已删除，未归档或迁移正文。
原有验收摘要、冻结来源、失败/skip 与待验门槛仍保留在本页及 08/16/17/18；
原报告链接现指向已有摘要段落，不表示这些摘要等同完整原始日志。
原始 JSON、日志、冻结源码和已交付归档仍在各批次记录的位置；临时目录不是永久证据仓库，
若原始材料不可取得，必须重新核验对应结论，不得只凭旧勾选宣称当前版本通过。
既有 dist 候选是固定交付快照，其中的源码清单和说明可能保留当时路径，不能据此访问已删除报告。

更新：2026-10-29。目标保持为路线图中的完整 Phase 0–5；本文件记录当前证据，不缩减首版范围。逐项完成状态及复合条目剩余门槛已同步 [08 路线图](./08-roadmap.md)，架构复核详见 [16 一致性检查](./16-architecture-conformance.md)；后续每批必须同步三份文件。实现从仅含设计文档的干净工作树开始，实际 module 确认为 `github.com/DDSRem-Dev/Shikigami`。

<!-- active-parallel-work:start -->
**当前工作与验收边界（2026-10-29）：**

| 工作 | 当前状态 | 后续条件 |
| --- | --- | --- |
| 首个自动预发布 | f0b5f83 tag全流程成功；22公开资产认证、24二进制元数据及双平台暂存通过 | 后续推送新版本tag自动构建、签名与发布；旧tag不可移动 |
| 真实签名包服务安装 | 92cafaf候选的原生LaunchAgent及容器systemd新装/运行/升级回滚通过 | 与新tag归档字节和有限暂存验收分开；外部登录及原生部署仍待 |
| 原697源Linux默认12小时 | 保留原冻结运行和历史证据，本批未操作或复核终态 | 独立核验四角色自然更换、恢复尾部及最终清理；不得跨源码追认 |
| 完整交付门槛 | 当前31/42、11项待完成；当前批次已收尾 | 外部账号/物理平台、长期资源、Homebrew和密钥轮换撤销等依路线图推进 |

31/42是原始复合条目的验收数，不是代码完成率；历史批次中的旧计数保持当时范围。等待类验收不阻塞独立工作，不重复已通过检查。
<!-- active-parallel-work:end -->

**本地收尾与目录清理（2026-10-29）：** 当前候选541份Go/proto/全部module执行输入逐hash仍与`.5`一致；已知syncack整包及真实systemd组合遗漏已经补验，不重复运行。完成[当前候选验收矩阵与后续条件](./19-local-acceptance-closure.md)，明确原697源终态先核四角色轮换、30分钟尾部、资源和清理，再依据具体结果补当前候选证据；未发现新的确定资源保留根，不把短测或旧源通过当无泄漏。发布准备中旧容量与撤销Token待办已按后续结果更正。普通/内嵌构建共用严格元数据入口，显式`SHIKI_COMMIT`/`SHIKI_BUILT_AT`注入、缺省unknown；8项Python回归通过，涵盖两入口六程序参数一致、真实buildinfo探针链接/输出及非法输入在构建前拒绝，不宣称六产品重打包。独立架构测试通过；七类复核未发现本批新增生产偏差。按用户最终授权共删除96份源码目录Markdown（scripts中89份及其他子目录7份），未归档；源码目录仅保留根README/AGENTS，既有dist交付快照保持。docs原有验收摘要保留，旧报告链接改指已有摘要锚点；465条相对链接及锚点无断链。三个诊断runner移除已删报告复制依赖，保留源码/工具冻结与hash门禁；实际准备代码9个隔离场景及3份完整脚本语法通过，不运行服务/负载。原42条要求/顺序及29勾选不变；资源、原生平台、外部账号和正式发布门槛仍未完成，原697长测独立继续。18:17检查点2657轮/7小时35分31秒，未轮换，无OOM/重启；本批不改旧`.5`归档、签名状态或unknown元数据。

<a id="evidence-default-rotation-linux-current"></a>


**长跑收尾准备（2026-10-29）：** 既有native最终核验器含macOS路径/PID假设，已准备独立Linux只读入口`/tmp/shiki-default-linux-final-review-20261029/verify.py`；固定697源manifest、16份原契约源码及精确CID，核四角色身份/自然到期/两分钟恢复、TCP不重放、30分钟尾部、11个受管PID的fixture清理和精确容器缺席。PID只要求更换，不能要求数值递增；并发事件按完整记录多重集对照，关键阶段单独核时序。语法和当前真实pending路径已验，23工具/契约文件hash复核；没有运行新负载、探测宿主同号Linux PID或读取私有失败runtime。697终态分支尚未执行，准备不算通过；17:46精确容器仍运行，完整检查点17:45:47为2474轮/7小时4分2秒，无OOM/重启。三文档同步、原42条/29勾选及资源/外部门槛保持；完整入口与范围见原运行报告。

<a id="evidence-long-run-source-coverage"></a>

**后续资源所有权核对（2026-10-29）：** 限定检查630源所有权审计之后新增的startup查询、route attempts、status-only NodeControl与sync Stop：查询许可/子进程/连接均有界并有退出清理，最多两项路由尝试替换旧值，Stop使用单清理owner；既有取消/准入/代际/阻塞依赖及真实syncack/systemd证据已覆盖这些具体边界。当前541执行输入仍与.5一致；未发现新的已确认保留根或足以支持追加用例的具体缺口，不对历史增长归因、不追加短内存实验或新12小时。原697源17:23实查仍存活、无OOM/重启；最后完整检查点17:22:27为2338轮/6小时40分42秒，尚未自然到期。此轮为定向源码核对和已确认活跃进程的必要等待，资源/外部门槛及29/42保持；不重复上一批全套测试。见覆盖与所有权边界。

<a id="evidence-install-systemd-recovery-validation"></a>

**最近完成批次：当前安装恢复与真实systemd组合补验（2026-10-29）。** 针对旧服务验收未包含三处安装修复及新sync Stop的实际覆盖缺口，复用完整既有场景首轮通过：744冻结源、541执行输入与.5一致，13生命周期边界、4活动升级/回滚及真实poweroff，外层165.634秒、race活动探针52.88秒/零skip。每次切换均观测256KiB部分传输、公开目标旧完整hash保持、旧host/worker回收、同pane/原Token保留、文件冲突显式解决与新TCP恢复。产品是普通build，仅活动探针race；只读挂载源复制到容器可写/work构建，未宣称/work后置hash。独立44项复核无未解差异，正常退出0及精确CID/私有镜像删除已核验；systemd日志保留tmp.mount卸载FAILED，随后正常poweroff，不宣称日志全零失败。同期七类架构审查无新增确定性生产偏差，修正15/16旧长跑时态及08两处已完成却仍写待办的记录。三文档当前并行表、部分进展和结论同步，原42条/29勾选保持。正常服务场景不替代受控失败回归、掉电、历史发行、长稳/资源或外部门槛；697原长跑继续。见服务补验。

<a id="evidence-sync-stop-ack-integration-validation"></a>

**最近完成批次：当前sync Stop与丢ACK恢复补验（2026-10-29）。** 补齐上批六包遗漏的既有syncack整包；743同源、541执行输入与.5完全一致，macOS/Linux各一次race+vet通过，各4顶层/7子用例、0skip，包42.802/28.722秒。四种已持久化未知提交后的真实宿主RESTART均保留日志/源/旧基线，旧client回收、server不变、先读恢复且仅一次上传；32-watch准入、映射归档及未来schema拒绝也通过。无生产/测试修改、预算放宽或重复尝试。清理证据限fixture明确join/Reaped/runtime空断言；原生私有tmux目录与精确Linux容器正常回收，非完整PID清单。独立29项核验、七类复核及08/15/16同步，原42条及29/42保持；不作为重复worker Stop、超过5秒排空、提交途中强杀、长稳或资源验收。见专项结果。

**最近完成批次：长跑与候选源码覆盖核对（2026-10-29）。** 697源521份执行输入与当前.5的541份逐项比对：487相同、34修改、20新增、无删除；54差异为25生产Go、3协议/生成、23测试及3独立诊断输入。14份轮换观察/runner/collector字节保持，但新sync Stop改变实际资源排空，路由attempts含运行字段持久化剔除，不能把旧运行追认为当前组合长稳；安装器不在默认长跑执行范围。两路独立只读复核和七类文档审查完成，已有专项逐组对应，未新增测试或重启实验。08/15/16同步，原42条及29/42保持；当前候选默认时长/资源及外部门槛仍待，文件数量变化不单独构成再跑12小时的理由。见覆盖边界。

<a id="evidence-release-ci-validation"></a>

**最近完成批次：四目标CI打包入口（2026-10-29）。** 原compile矩阵改为既有make release，build.sh仍执行原go build ./...；新增精确归档SHA及包内SHA256SUMS pin校验，固定CI版本且禁签名。独立741冻结源的Linux arm64普通用户容器串行四目标首轮通过，29命令/4归档/56成员/24份二进制静态目标信息均核验；源/工具后置hash、正常退出与精确容器清理通过，独立复核包含ELF/Mach-O头。七类架构复核完成，541执行输入与.5不变；仅工作流/文档变化，不运行目标程序、不冒充远端CI或原生平台验收，本地umask077权限结果不外推。08/15/16同步，原42条及29/42保持；签名/发布/外部与资源门槛仍待，697长跑原样继续。见CI打包验证。

<a id="evidence-default-rotation-linux-resource-observation"></a>

**最近完成批次：Linux长跑中期资源只读分析（2026-10-29）。** 原697源日志已保存293个完整检查点/2344条八进程样本；截至15:37:57为1731轮、4小时56分12秒，原容器/StartedAt保持，应用退出未产生。Agent前四完整小时RSS中位数57.45→59.82MiB，末段回落不作为无泄漏证明；全部角色/峰值及不完整小时保留。原FD+4与测试goroutine14+6门禁在各检查点前完成，不声称无瞬时越界或生产进程goroutine已测量。独立32项复算、源双树697hash核对及七类架构复核完成；插件目录/依赖符合文档，当前541执行输入与.5一致。本批无新负载或产品修改，原42条及29/42保持；自然轮换、尾部、清理、资源稳定及外部门槛仍待。见资源观测。

<a id="evidence-local-candidate-20261029-5"></a>

**最近完成批次：恢复修复候选包 .5（2026-10-29）。** 同一739文件冻结源码的macOS/Linux arm64未签名归档已首轮构建、验收并交付至dist/local-20261029.5；541份Go/proto/全部module与上批738已验输入逐项一致，包含四份恢复修复。两平台各52实际命令、14成员/六程序/双插件及全部既有诊断/确认/配置保护检查通过；实际归档helper首次与重复stage-only各校验14成员和私有review，无current/receipt/config/state/安装links，标准服务文件前后保持。两个agent均正常退出/reap，原生PID/runtime及精确Linux容器清理独立核验；源/工具/交付哈希与七类复核完成。08/15/16同步，原42条及29/42保持；未签名、Commit/BuiltAt unknown，暂存不是激活，.4历史及697长跑范围独立。见候选验收与[交付说明](../dist/local-20261029.5/README.md)。

<a id="evidence-recovery-conformance-validation"></a>

**最近完成批次：同步停止重试与安装失败恢复（2026-10-29）。** sync client Stop改为单次清理owner与各调用者独立截止，资源未排空不再重试假成功，关闭错误及未知提交源/日志保留；安装Stage补metadata/父目录同步及已发布目标重试屏障，Rollback早期失败仅恢复可信已提交选择，随机服务临时文件允许中断重试且保留旧现场。停止7条与安装恢复13条父/子同测红绿通过；Linux实际syscall证明原14/0→17/17同步，9精确EIO及新进程恢复通过。738同源双平台六相关包race/vet及真实sync子进程通过，各152父/子pass，1/2 skip不计通过。原生应用0但采集器因缺PID清单退出1，保留失败并限定清理证据为fixture Close/Wait；Linux应用/采集器0、精确CID及副本已清理。七类复核与独立审查完成，08/15/16同步，恢复Phase2项至29/42、13项待验，原42要求保持。.4归档和697长跑不追认本批修改。见合并验收。

<a id="evidence-terminal-analyzer-remediation"></a>

<a id="evidence-local-candidate-20261029-4"></a>

<a id="evidence-node-status-validation"></a>

**最近完成批次：分析器依赖、节点状态诊断与 .4 交付（2026-10-29）。** 独立analyzer的x/sys已最小升级0.44.0，同库两目标复扫零finding，725同源双平台各16终端捕获及清理通过。修复只读状态误要求node.login.v1及doctor被不可用清单阻断：实际运行端NODE_DISABLED/NODE_STATE_UNAVAILABLE、未知清单null和正确Dashboard提示均保留，登录能力及UID/实例/截止校验仍严格。有效同测红绿与729源双平台四包race/vet通过，各196父/子pass、无skip；同537份Go/proto/全部module输入的729源.4实际归档两平台首次通过，各50命令、两agent正常退出及精确清理，12文件已交付。七类复核及独立核验完成，08/15/16同步，恢复诊断与Dashboard两项，原42条文字/顺序不变、当前29/42、13项待验。未签名、Commit/BuiltAt为unknown；.3历史错误、725/729不同证据和697独立长跑范围分别保留。见依赖修复、节点诊断、实际归档验收及[交付说明](../dist/local-20261029.4/README.md)。

<a id="evidence-security-validation"></a>

<a id="evidence-terminal-analyzer-security-validation"></a>

<a id="evidence-port-handshake-branch-validation"></a>

**最近完成批次：端口握手分支与独立分析器依赖（2026-10-29）。** 已补8个Send-EOF/非配额ResourceExhausted握手及已建立流对照分支，以受控公开Connect transport驱动真实Worker/TCP，验证既有payload/同监听/槽回收/无重放和正常清理；724同源双平台完整client包race/vet均首次通过，各7顶层+8分支、无skip，原5秒预算保持。嵌套VT analyzer已新增独立CI扫描，固定v1.1.4两arm64静态目标均无受影响已导入包，但保留GO-2026-5024 Windows模块公告；原生精确CI命令亦退出0，不称零漏洞或远端CI已跑。源后置/精确Linux清理及七类复核完成，08/15/16同步、原42条及29/42保持。没有生产/协议/依赖升级或产物重建，697长跑独立继续。见端口分支与独立依赖扫描。

<a id="evidence-terminal-ci-validation"></a>

**最近完成批次：CI终端语义回归入口（2026-10-29）。** 已将原有test-terminal-state.sh接入macOS/Linux默认CI独立步骤，修复缺analyzer时终端语义专项一直跳过的入口遗漏；只增加调度，不改生产、测试断言、模块或原90秒/60秒预算。同一719基础源加唯一环境wrapper的720源，逐字CI run block在macOS/Linux各执行一次：实际专项2.18/0.82秒通过，各16捕获、无skip；原生六PID/runtime独立缺席，Linux应用/collector退出0、精确CID缺席且源码副本正常删除。原/派生源码后置hash与七类复核通过，08/15/16同步；远端GitHub Actions尚未实际执行，原42条及29/42保持，不重建.3或改变697长跑。见CI终端验收。

<a id="evidence-local-candidate-20261029-3"></a>

**最近完成批次：本地候选包 .3 交付（2026-10-29）。** 同一717文件冻结源码已通过macOS/Linux arm64既有make release和实际归档验证，交付至独占dist/local-20261029.3；530份Go/proto/module与已验719完全一致，包含诊断、操作确认和配置恢复修复。两侧14成员、六程序、双插件清单/hash/pin、startup/list/doctor、缺--yes零启动和坏配置原件/备份保留均通过；正常Shutdown/reap、源事后hash和精确Linux清理已核验，无失败重跑或强制清理。七类架构复核完成，08/15/16同步，原42条及29/42不变。未签名、Commit/BuiltAt为unknown，不追认697长跑、外部连接或正式发行；空服务器诊断不证明实际路由。见候选报告及[交付说明](../dist/local-20261029.3/README.md)。

<a id="evidence-diagnostic-confirmation-validation"></a>

**最近完成批次：诊断与操作确认（2026-10-29）。** 已补06要求的实际路由尝试列表和损坏配置恢复说明，以及05要求的stop/restart非交互--yes确认；运行字段有界、按代际发布且不持久化，原回退授权/停止实现/保存语义保持。三项有效同测红绿及独立审查完成；718同源双平台受影响race/vet各179顶层通过，安装专用skip和无测试包单列。现有4处E2E调用仅追加--yes，719源仅两测试文件和环境脚本不同；双平台两条真实CLI生命周期/端口恢复E2E及tagged vet均通过，无skip。首轮旧Go环境前置失败保留，实际验收仅修PATH；源后置/正常Linux清理和七类复核通过，08/15/16同步。恢复三项，原42条文字/顺序不变、当前29/42；707归档与697长跑不追认这些后续变更。见合并验收。

**进行中的验收：新Linux默认12小时（2026-10-29）。** 已验宿主/观察修复和最终collector组成独立697源，于10:40:43启动；collector33067、精确容器232b672a…65f3，四初始worker/UID1000/只读源码已核验。17:46只读检查截至17:45:47已2474轮、7小时4分2秒且原容器仍运行；默认凭据12小时，最早22:41自然到期，恢复尾部下界23:11、外层截止23:55（Asia/Shanghai）。整轮恢复、至少30分钟尾部、资源及最终清理仍待验；原Linux失败保留。后续启动策略、诊断/确认、节点状态、同步停止与安装恢复修复及.5候选不在该冻结源中。见独立运行。

<a id="evidence-default-tools-validation"></a>

**最近完成批次：默认入口补齐采集器回归（2026-10-29）。** 发现已有13项Python采集器关键回归未进入make test及CI，已新增test-tools并由test依赖；普通用户/Python3.10+前置避免root跳过生命周期测试却返回成功。同一最终四文件在macOS/Linux各13项通过、无skip；Linux实际root调用退出2且未运行suite，make -n test确认原三组Go入口保持。只改Makefile及文档，采集器/测试断言/生产输入不变；七类架构复核完成，08/15/16同步。未声称远端CI已运行，不重建707候选或变更697长跑；原42条及29/42保持。见默认工具检查。

<a id="evidence-local-candidate-20261029"></a>

**最近完成批次：本地开发候选交付（2026-10-29）。** 同一707文件冻结源码已通过既有make release构建macOS/Linux arm64的v0.0.0-local.20261029.2归档，并交付至独占dist/local-20261029.2。两侧14个归档成员、六二进制版本、双插件清单/hash及校验pin均已核验；实际解包CLI/agent的隔离startup JSON、新文本说明、停止后不自启、正常Shutdown/reap与清理通过，Linux为UID1000且精确容器已删除。相对已验启动策略产品输入仅一句CLI说明变化，本批已实际覆盖。完整源清单随包提供，未签名，Commit/BuiltAt为unknown，不作为正式发行或发布者认证。七类架构复核及08/15/16同步完成；原42条及29/42不变。697源12小时运行独立保持，签名/分发、资源及外部平台门槛仍待。见候选包报告与[交付说明](../dist/local-20261029.2/README.md)。

<a id="evidence-startup-policy-validation"></a>

<a id="evidence-systemd-validation"></a>

<a id="evidence-startup-policy-systemd-validation"></a>

**最近完成批次：启动策略配置展示（2026-10-29）。** 补齐11要求的本地展示：新增只读shiki config startup / GetStartupPolicy及可协商agent.startup.v1，分别报告agent加载的CLI按需配置、受管登录/开机策略与unknown。核对配置绑定、持久启用及Linger/launchd覆盖；共享单查询准入、3秒总预算、64KiB输出限制和取消回收已验证，不修改服务或持久偏好。macOS/Linux相同产品/Go测试/协议输入各四完整包race、vet通过，无skip；真实systemd四态与同agent身份、正常stop/poweroff/精确清理通过。两次Linux夹具失败保留，最终两个诊断脚本差异及后置一行CLI说明/12文档更新单列。七类架构复核与08/15/16同步完成；12/18过期状态已纠正，原42条及29/42不变。该本地缺口关闭，真实重启/登录、默认12小时、资源与外部发布门槛仍待。见实现验收与真实systemd。

<a id="evidence-cleanup-validation"></a>

<a id="evidence-host-cleanup-affected-validation"></a>

<a id="evidence-host-handler-ownership"></a>

<a id="evidence-acceptance-cleanup-validation"></a>

<a id="evidence-rotation-merged-preflight-validation"></a>

**最近完成批次：宿主清理所有权与验收采集（2026-10-29）。** 修复CoreAPI回调尚未结束即释放writer lease、并发Stop阻塞调用者截止，以及首次Kill早于Client启动的进程回收竞态；Supervisor等待实际清理完成才重试或关闭状态库，停止准入拒绝保留no-store。有效同测红绿与最终692基础源的原生host/plugin两包race/vet通过；增加一个诊断脚本的693同源原生11项链路/架构检查及Linux七组race、vet全部通过。Linux应用/collector退出0、精确容器及快照清理已核验；原生补验由夹具断言回收，不冒充完整PID清单。独立637源双平台预检和最终collector13项测试分别通过；637/693使用初版正常清理collector，不追认后续异常保护覆盖。七类架构复核与08/15/16同步完成，恢复两项，原42条要求不变、当前29/42。资源循环显式排除，不归因历史heap差值，原Linux12小时失败及外部/发布门槛保留。见宿主修复、受影响验证、预检与采集器修复。

<a id="evidence-rotation-validation"></a>

<a id="evidence-rotation-observer-validation"></a>

<a id="evidence-default-rotation-validation"></a>

**最近完成批次：长跑进程观察修复（2026-10-29）。** 原Linux12小时运行在替换身份读取时失败，未完成四角色恢复/30分钟尾部，原失败保留。已补有限截止内PID/出生/UID/父进程/路径/hash与唯一候选核验；只有独立确认进程消失才重试，权限/未知/换代错误明确失败，退休采样错误不冒充退出。清理确认后将有界私有原始证据保留到外层目录；不称脱敏或原子快照。635诊断源同源双平台helper及7项命名观察/时序/证据race、vet均通过，无skip；Linux应用/collector退出0、精确容器与快照回收，七类复核完成。未改生产/module，未重跑12小时，不把夹具检查追认为实际轮换验收。中间Darwin错误/共享调用签名编译失败均保留；当前29/42。见观察修复与原运行结果。

<a id="evidence-load-capacity-validation"></a>

<a id="evidence-port-limit-isolation-validation"></a>

**最近完成批次：端口准入隔离与普通混合负载（2026-10-29）。** 修复单连接握手ResourceExhausted取消整个转发任务的问题，按14使用保留错误码而非被脱敏文本；已建立流及认证/权限/路由错误处理保持。633冻结源同测有效红绿、双平台相关五包race/vet与七类审查完成；macOS/Linux普通正式负载各32轮、232流、每向14.5GiB及128MiB文件、双40空载样本、双默认128准入和12个存活/单槽恢复检查点均通过。原始数据独立复算，进程/session/runtime及精确容器清理核验；Linux专项collector只读快照清理错误保留并有补充清理，正式两轮均退出0。恢复Phase3条目，当前29/42；完整资源稳定、原Linux12小时恢复/尾部与外部发布门槛保持。见修复验收与容量测量。

<a id="evidence-resource-ownership-audit"></a>

**最近完成批次：成功上传流资源释放（2026-10-29）。** host与SDK纯client stream成功后保留两项调用方取消注册，已由相同测试的冻结修前/修后和真实generated UDS/HTTP2上传确认并修复；仅两生产文件变化，server/bidi消息及半关闭保持。共同632输入双平台两完整包race/vet、六包命名链路与架构race、真实终端16项捕获各平台均通过，无skip；原生6PID/runtime及Linux正常退出/精确CID清理独立复核。七类架构审查未发现其他确认生产偏差，08/15/16同步，原42条及29勾选不变。仅证明该取消注册保留根修复，不解释历史heap斜率、不关闭资源稳定/容量或外部门槛。见所有权报告。

<a id="evidence-schema-macos-validation"></a>

<a id="evidence-tailscale-coexist-validation"></a>

**此前批次：系统Tailscale非干扰与原生跨schema恢复（2026-10-29）。** Linux容器真实tailscaled在21个安装边界保持进程/unit/配置/socket/状态一致；独立UID1001内嵌tsnet实际报告needs_login/NODE_AUTH_REQUIRED与退出3，私有state和系统资源EACCES验证通过。系统state仅为2字节空JSON，不证明已有账户身份保持。macOS原生LaunchAgent完成A529→B630→旧A拒写→仅sync受控恢复→A→B及双向同步，30次RPC为18成功/12远端拒绝；最终B代际及清理已补齐。源/工具/上游模块、正常退出/精确清理与七类复核通过，无生产修改；失败和不完整轮保留，08/15/16同步，原42条不变、当前29/42。原Linux12小时、资源及外部/发布门槛继续待验。见系统Tailscale共存与原生schema。

<a id="evidence-service-boot-validation"></a>

<a id="evidence-upgrade-validation"></a>

<a id="evidence-schema-service-upgrade-validation"></a>

**此前批次：服务二次启动与恢复后撤权（2026-10-29）。** 同630文件冻结源码的Linux容器持久卷二次真实PID1启动通过：daemon/user-agent自动启动、四worker代际、身份/凭据/映射/基线/冲突/暂停/转发保持、双向同步与仅配置快照恢复均已验证；前两次夹具失败保留，修正依据为文档10。跨schema A529/B630场景5阶段30次TLS/HTTP2 RPC通过（18次成功、12次远端Unauthenticated），恢复未复活撤销凭据。两场景正常退出、精确资源清理及七类架构独立复核通过，无生产修改；08/15/16同步，原42条不变、当前29/42。容器PID1重启不等于物理重启，受控sync恢复不等于产品自动恢复；原Linux12小时、资源及外部/发布门槛继续待验。见二次启动与跨schema报告。

**此前批次：固定版本下载与Agent生命周期（2026-10-29）。** 新增显式HTTPS固定版本下载，独立公钥先验签再取归档；真实94MB既有发行归档经当前Fetch认证后由真实helper暂存通过。修复映射迟到事件/配置响应回填、宿主全局锁等待网络及映射/转发旧预检跨代提交，保留持久重放与共享连接生命周期。确定性红绿和独立复核完成；同630文件源码双平台各8个完整包、命名CLI与两条E2E及相关vet通过，无失败/跳过，Linux正常退出/精确清理已核验。540份执行输入一致，17个Go文件变化（10生产/7测试），协议/模块不变。七类架构复核及08/15/16同步完成，恢复映射项，原42条不变、当前29/42。原macOS12小时轮换/30分钟尾部/清理另已通过，仅覆盖原旧源码；Linux原长跑、资源及正式发布/外部门槛保持待验。

**此前批次：节点授权生命周期与端口准入（2026-10-29）。** 修复节点授权lease保留已结束/取消请求回调、端口首请求前缺少宿主并发门、worker长Watch挤占短查询及Stop重试累加等待者。共享metadata32/watch32和首帧5秒已由真实HTTP2/独立插件链验证；节点取消、client终态与bidi半关闭有确定性红绿。原生节点组合夹具拆成各自45秒的独立目标，原断言及历史失败保留；同620文件源码双平台各11个有测试包race及vet通过，2无测试包和1opt-in跳过单列，进程/容器清理核验通过。七类架构复核及08/15/16同步完成，恢复Phase0条目，原42条不变、当前29/42。未将本修复当作旧TLS内存增长归因；正式发布、资源与外部门槛仍待。

**上一批：同源完整签名构建与诊断对象回收（2026-10-29）。** 同一608文件冻结源码在macOS/Linux实际完成`make release`、一次性测试密钥签名、独立公钥验签、认证私有副本解包、六二进制版本核验及真实helper暂存；macOS client与Linux client/daemon通过，私钥删除、Linux正常退出和精确容器清理已核验。诊断绑定`Close`持有已关闭daemon取得有效红绿，仅释放诊断回调，原生普通/race/vet通过，无生产改动或新增内存负载。七类架构复核未发现新增确定性偏差，08/15/16同步；原42条要求不变，当前29/42。两轮原12小时运行保持原样，运行内存增长、平台/外部及正式发布门槛仍待。

**此前批次：文件RPC并发准入与首帧预算（2026-10-29）。** 已补齐单OS用户跨连接/Token共享的metadata32、transfer16、watch32限额，授权先行且类别独立；上传首帧先登记再接收、默认5秒且保留更早父截止，正常长Watch不受此5秒限制。SDK可取消单reader与Stop超时重试假成功均已修复。有效红绿、真实独立插件链、macOS/Linux各10包race、vet和精确容器清理通过；2无测试包和1项opt-in资源skip不计通过。原42条要求不变，恢复Phase0传输条目，当前29/42、13项待完成；全架构复核及08/15/16同步完成。已有内存profile离线归因取得具体所有权线索，但残余增长、两轮真实12小时和外部/正式发布门槛仍待。

**此前批次：离线签名与后台同步截止时间（2026-10-29）。** 两轮12小时原运行持续期间并行完成两条工作。后台CoreAPI短unary默认5秒、按文件大小预算及未知提交恢复已完成修前红/修后绿、macOS与Linux受影响race检查，恢复Phase1含deadline条目，当前29/42、13项待完成。离线Ed25519签名/验签、严格PEM及打包sidecar不覆盖修复通过双平台检查；真实开发归档经当前verifier认证、私有复制并stage-only安装通过，独立102项断言核验。Linux补验9包及两个命名工具测试通过、2包无测试不计通过、无测试skip，vet和精确容器清理通过。原42条要求不变，全面架构复核及08/15/16同步完成。正式签名身份/分发、长稳与资源门槛仍待；旧developer归档不冒充当前源码构建，两轮原长跑不覆盖本批生产修改。

**默认12小时验收新进展：2026-10-29 02:37:12（Asia/Shanghai）。** 原macOS运行已通过：4216轮、实际工作12h30m1.03382625s，四worker各自然替换一次，恢复尾部1802.031秒；应用/collector退出0、owned_cleanup无失败，13个记录PID已不存在，原runtime/source已删除，防休眠进程正常退出。独立复核`/tmp/shiki-default-native-final-review-20261029`通过；server-port瞬态不可用未被采样，保留false、不声称同时退役。仅覆盖原495文件/411份Go-proto-module，Linux原运行仍在到期前阶段，新修复另行验证。见轮换记录。

**原Linux默认12小时已失败（2026-10-29 09:13:50）。** 原578文件源码运行43,219.82秒后，在server-sync旧代跨12小时退役后的替换进程身份读取阶段报`readlink /proc/274459/exe: no such file or directory`；4,191轮最后检查点仍recovered=false，无四角色恢复或30分钟尾部。该PID角色未保存，不能断定是无关短命child或候选worker退出。原七PID清理无报错，精确容器已不存在；新代未登记，不声称逐PID新代清理。失败证据保留，观察夹具已完成独立双平台确定性修复验证，不自动重启12小时，不覆盖当前633产品修复。见实际运行记录。

<a id="evidence-default-rotation-observation"></a>

**只读中期观测：2026-10-28 21:51。** 从两轮现有日志保存完整前缀：macOS459个检查点、7小时44分/2610轮；Linux38个检查点、37分33秒/227轮，均recovered=false。旧版macOS Agent前七个完整小时RSS中位数从63.77升至75.95 MiB，当前80.06 MiB；Linux当前54.49 MiB，但运行不足一小时且源码不同，不能比较修复效果或认定稳定。全部角色/高点保留，RSS不等于保留堆或泄漏证明；不追加负载、不重启，29/42保持。见只读观测。

<a id="evidence-memory-observer-validation"></a>

**此前批次：残余资源增长的观测开销调查（2026-10-28）。** 当前29/42不变。本批仅8个诊断文件变化，生产输入不变；默认/延后导出的同二进制短预检均通过。唯一一组普通Linux负载/空载各317.709/317.672秒完成，136/128份原始归档、各68份采样JSON、6个关闭后profile及精确进程/容器清理独立核验通过。Agent末段load−control为−260,788 B/min、+9.98对象/min；heap负值受idle3高点回落主导，不表示稳定。关闭后仍有+24,240 B/+228对象差值；旧参照为+79,456 B/+302对象。保留全部样本，不由一组干预推定因果或无泄漏。按预声明停止追加短窗口实验，资源门槛仍未关闭。全面架构复核与08/15/16同步完成，见观测开销报告。

<a id="evidence-pool-request-lifetime-validation"></a>

**上一批：请求生命周期与跨schema服务验收（2026-10-28）。** 当前29/42、13项待完成。Agent共享pool请求回调保留已确定性复现并修复；专项与Linux修前/修后/同二进制空载三组完成，修后后段load−control仍+15,597 B/min、+92.35 objects/min，不认定无泄漏。macOS与Linux完整race/E2E各46包通过；两次完整运行的生产输入相同，Linux最终快照仅多一处syncack夹具预算修正，macOS对该修正另有四场景race补验；不称两侧最终测试源码逐字相同。6/10项skip不计通过。真实Linux systemd开发schema升级、旧版拒写、有限恢复及再次同步已通过独立复核，不代表产品自动恢复。历史失败均保留，见生命周期与回归、跨schema服务。本批全面复核与08/15/16同步完成；原12小时运行和外部门槛保持待验。

## 本批完成：成功上传流资源释放（2026-10-29）

host和SDK的取消包装器在成功client stream的唯一响应/最终状态之后仍由长寿命caller保留两项取消注册。生成的WriteFile/RemoteWriteFile只有CloseAndReceive，不会额外调用Close；现仅对精确StreamTypeClient成功终态调用既有幂等Close。caller、共享pool、server/bidi多条响应及CloseSend半关闭保持，响应header/trailer与远端错误不丢失。

最终red3/green各632输入，新测试逐字相同，仅两生产文件不同；每包成功wrapper与真实generated UDS/h2均修前残留2项、修后归零，调用方在断言后才清理。最初CallInfo包装隐藏计数hook的夹具盲区及red2保留，不将错误的零计数追认为通过。native两完整包race2.499/6.521秒、Linux2.159/6.135秒，双方scoped vet与六包命名链路/架构race通过；真实终端语义16捕获各平台通过（2.16/0.85秒），全部无skip。

共同green manifest为`dbc92d410523fcff70523eeff0a52b093030ccfd904b0215dc45a93d2d8c0454`；Linux仅额外一份独立shell工具，原632输入一致。两份独立复核确认输入、日志与清理；native6记录PID/runtime/source消失，Linux UID1000应用/collector退出0、专属CID消失，无强制清理。完整证据与七类复核见所有权报告。仅该具体保留根修复，不证明即时GC或历史heap斜率因果；sync既有逐次cancel保持。普通负载工具另行推进，资源稳定/完整容量及外部门槛仍待；原42要求/29勾选与08/16一致。

## 本批完成：系统Tailscale非干扰与原生schema（2026-10-29）

Linux容器第二轮用630文件生产源、10份工具及2536份tailscale.com v1.98.10上游输入完成161.236秒真实systemd场景。先启动真实未登录系统tailscaled，再对Shiki daemon/client执行安装、升级、回滚、卸载、重装；21个边界的LocalAPI均HTTP200/NeedsLogin，无登录URL/节点地址，进程出生标识、unit InvocationID、配置/二进制/socket/状态记录一致。运行版本如实为1.98.10-ERR-BuildInfo，Go buildinfo确认固定模块，不冒充官方发行。

另一UID1001真实启动内嵌tsnet，NodeControl为needs_login/NODE_AUTH_REQUIRED，安装器退出3/node_needs_login/external pending；独立state.json为252B、0600、UID1001，访问系统socket与state实际均EACCES13。系统tailscaled state自身为2字节空JSON对象，只证明未登录实例和该文件未受干扰，不证明已有账户/profile/key身份保持。真实poweroff退出0/PID0、无OOM/强制清理，精确CID独立确认已删除；证据`/tmp/shiki-tailscale-coexist-pzqvkja5`。首轮完整TLS生命周期已到达断言，新增探针漏填Capabilities导致协商错误，失败与清理保留；只修工具，不放宽状态或超时。详见共存报告。

原生macOS第四轮使用A529/B630及8份工具，在真实HOME、UID501和gui/501中运行LaunchAgent，独立A daemon与自有tmux session保持。实际完成schema1→2→旧A拒写2/应用pending→停写后仅恢复13文件/10349字节sync子树→A ready→B ready及既有基线双向同步；host/auth/凭据/项目、映射/revision/冲突/暂停/谱系/回执保持。5阶段30次RPC为18成功、12远端Unauthenticated/HTTP401，TLS/h2、原证书/daemon/project一致。最终B host/worker的birth/exe/UID/产物哈希进入所有权记录；12个记录PID独立确认消失，daemon退出0、无fallback、标准job/plist/staging和自有session/runtime已清理。证据`/private/tmp/shiki-schema-native-20261029-attempt4`，详见原生报告。

原生首轮只在工具链选择失败、未启动服务；第二轮初始shell身份断言失败，独立复现/bin/sh由实际/bin/bash执行，但原轮未导出actual字段，不能追认唯一原因；第三轮业务/RPC通过但最后B进程代际未记录，不算完整进程验收。第四轮补齐后通过，前三轮原样保留。两场景均无生产/go.mod变化，源码和工具独立冻结，原Linux12小时未重启。七类架构复核及08/15/16同步完成；29/42保持，已认证共存、真实账号、原生重启登录、历史正式产物、资源稳定与发布门槛仍待。

## 本批完成：服务二次启动与恢复后撤权（2026-10-29）

跨schema授权子项已完成。真实Linux systemd使用原A529和当前B630冻结源码完成1→2→旧A拒写2→停写后仅恢复sync子树1→A ready→再次升级2。新候选Token在升级备份后签发，先证明可用再撤销；5阶段各两凭据×三RPC，共18次成功、12次远端Unauthenticated/HTTP401。每次都验证TLS、HTTP/2、同一证书，正对照保持初始daemon与project身份；实际请求不被客户端协商失败短路。原mapping/revision、共同基线、冲突、KEEP_BOTH谱系与完成回执断言保持。

源码1159份及7份实际工具输入逐hash匹配，两次导出的18份证据一致；systemd正常poweroff退出0/PID0、无OOM/强制清理，精确CID独立核验已不存在。根复核在`/tmp/shiki-schema-auth-rpc-20261029-attempt1/root-independent-review.json`，另一独立核验在`/tmp/shiki-service-recovery-independent-20261029/schema-review.json`。仅用固定A daemon测试client开发版本恢复，原私有状态未导出，删除容器后不能再次rehash；不代表自动恢复、任意新业务后的旧备份回滚或历史正式版本迁移。完整范围见跨schema报告。

服务二次启动第三轮已通过。同630文件源、8份独立工具输入完成同容器两次真实systemd PID1启动，持久home卷与rootfs保留，/run和/tmp重新建立；第二次daemon/user-agent启用状态与进程先于任何CLI/probe观察，auto_start关闭。原身份/Token/凭据、映射/revision/基线/冲突/暂停意图/转发保持，已有共同基线路径自动上传/下载，新路径实际观察INITIAL_SYNC_CHOICE_REQUIRED后经CLI显式选向，fresh TCP与新会话的配置快照恢复通过，项目文件不回滚。

双host及各自sync/port四worker均为UID1000、匹配选中release哈希；PID1启动tick与unit InvocationID确实改变，真实内核boot ID保持相同，不冒充物理重启。两次poweroff退出0/PID0、无OOM或强制清理；精确容器与独有home卷已删除。根复核`/tmp/shiki-service-boot-qyngyd0m/root-independent-review.json`与独立`/tmp/shiki-service-recovery-independent-20261029/serviceboot-review.json`通过，详见二次启动报告。

前两次失败保留：首次fixture home权限0755被产品正确拒绝；第二次错误要求无基线新路径自动上传，30秒超时。修正遵守文档10，不改变生产、不延长超时；旧失败未导出具体Initial状态，不能追认当时已观察该结果。第三轮独立明确验证首次选向。七类架构复核和08/15/16同步完成，当前29/42不变；原Linux12小时保持运行，物理重启/原生登录、外部账号、历史正式产物及资源门槛仍待。

## 本批完成：固定版本下载与Agent生命周期（2026-10-29）

显式HTTPS Fetch实现固定版本/平台、公钥独立输入、先验签后取归档、签署size/hash及私有receipt事务；CLI新增fetch而保留sign/verify。当前工具对此前608源94,476,199字节native归档实际下载、独立OpenSSL认证及真实helper client stage-only通过，私钥删除、无激活/残留helper；测试TLS根只在独立测试进程，不代表正式来源/身份或本批应用产物重新构建。

已确认并修复：迟到事件/Configure回复重建已删除映射缓存或覆盖新修订；PutMapping/Resolve/维护探测持宿主全局锁等待网络；成功转发预检在服务器/凭据变化或父请求取消后仍提交。事件/缓存短锁重验，网络与worker在全局状态/存储锁外，按server串行probe且等待可取消，结果核验pool指针和完整server record；mapping提交复核修订/身份/根重叠，历史请求结果不重新应用已过期配置。转发reconcile在锁内取得client快照，修复依据为静态所有权检查和受影响race，未伪称取得旧竞态red。单port插件forwardMu操作串行不等于全局状态锁，未做无依据重构。

共同源码`/tmp/shiki-fetch-mapping-final-20261029-wt1r8jvv/source`共630文件，manifest SHA256 `ba0e8bb7398541d6e0a8b225d14bafa0e5c0ddb4c4873bbb31abee444310862a`；540份执行输入与工作树一致，17个Go变化（10生产/7测试），proto/module不变。双平台各8完整包race、命名CLI、两条选定E2E与相关vet通过，无失败/skip/无测试包；native E2E63.110秒/Linux71.747秒，hostrotation218.494/209.369秒。E2E本轮子进程普通构建、parent race；hostrotation独立采集worker race日志，不能扩称全部子进程race。Linux普通UID1000无遗留进程，正常退出0、无OOM/强制清理，精确CID移除由root再inspect确认。原有效red与共享工作树中间态故障、TLS夹具/检查器错误分别保留，后者不充当产品red。连接初步green两例因维护探测准入失败，最终原子准入修复后通过；该初步raw路径曾被覆盖，只保留工具转录复原，明确不充当原始日志或通过证据，最终冻结日志完整。

<a id="evidence-fetch-mapping-validation"></a>

<a id="evidence-release-fetch-validation"></a>

<a id="evidence-forward-preflight-validation"></a>

<a id="evidence-mapping-event-lifetime-validation"></a>

七类全面复核覆盖目录/依赖、RPC/授权/能力、运行/持久状态与原子性/去重、进程/取消/恢复、文件终端网络语义、CLI配置/安装产物、行为证据/支持声明。已确认偏差同批修复；无新增协议/capability/schema，不放宽文档要求。独立审查与逐条命令/结果见合并验收、下载、映射、转发。08/15/16同步，原42要求逐字/顺序保持，恢复mapping项后29/42（8/11、7/7、5/6、6/6、3/6、0/6）。Linux原12小时、资源/正式发布及外部平台/账号门槛仍待，不将旧macOS长跑通过追认本批修复。

## 本批完成：节点授权生命周期与端口准入（2026-10-29）

确定性红证据确认节点在线授权lease保留client流成功、调用者取消及建流中取消的回调；修复仅解绑结束请求，不撤销共享授权，server/bidi每条响应和半关闭仍受epoch控制。真实HTTP2 generated upload与同连接活动terminal验证成功/错误/取消和隔离。旧TLS-only内存实验不执行该路径，不宣称历史斜率已解释。

端口List/Check/Watch已授权静默body在生成handler首Receive前无宿主门的缺口，经真实TLS/io.Pipe跨Token/连接红测复现。共享文件首帧工具提取后保留全部FILE错误/默认，新增port metadata32/watch32及5秒首请求预算，成功接收后解除短预算。worker原scans32把长Watch和短查询混用，也已由32个真实Watch挤掉List的独立插件链复现；现metadata/watch/ForwardPort三类独立，重复超时Stop共享一个实际完成信号，不累加waiter或报告假成功。ForwardPort配置上限/CONNECT/半关闭不改。私有AutoMTLS gRPC解码前预算不是本批新增保证，远端慢body在host先被限制，不把受信host故障窗口说成已解决。

节点整包最初共用45秒的多个验证目标耗尽预算失败；原生产授权代码同overlay/45秒也在后续重新授权阶段超时。单独原用例曾在39.08秒通过，原失败不追认为成功。现将四RPC形态、peer地址、授权epoch和移除peer拆成各自独立真实双节点fixture，每例仍45秒、三个原assert helper逐字不变，普通地址拒绝和EOF断言保留。最终node native race159.457秒/16顶层无skip，Linux101.468秒通过，vet通过；不更改任何生产deadline。mapping晚事件重新插入已删除缓存项只列静态候选，尚未复现或修改。

同620文件快照（manifest7b0fc491304c32a9fb1036a5baf135cf612ed209e117103712ea04bedb5e40a0）双平台各11个有测试包race及受影响vet通过。2无测试包和1opt-in资源skip单列；native文件Watch真实链5.668秒/Linux3.629秒，实际port子进程链均确认32条初始扫描事件后再断言限额/隔离/取消复用，并核验child race日志与PID回收。Linux正常退出0、无OOM/强制回收、UID1000无遗留子进程，精确CID移除已独立inspect。533份执行输入与工作树一致；本批17执行文件变化（8生产/9测试），不把上批诊断两文件重复计入，proto/module不变。

<a id="evidence-node-authorization-lifetime-validation"></a>

<a id="evidence-port-node-lifetime-validation"></a>

七类全架构复核覆盖目录/依赖与SDK-host-plugin所有权；RPC/并发/版本能力/授权；运行与持久状态/原子性/revision/去重；进程/取消/恢复；文件终端网络语义与限制；CLI/TUI配置/安装产物；测试证据与声明。计数/回调均运行态，不新增协议/capability或迁移；插件业务仍在plugins，跨进程用例位于test。未发现新增已确认设计偏差；新端口默认值作为实现策略写入14，不改规范为旧实现免责。独立复核报告`/tmp/shiki-node-port-independent-review-20261029/REPORT.md`，详细红绿/失败/共同源码/命令见端口与节点验收和节点授权记录。08/15/16同步，原42条保持，恢复Phase0勾选后29/42。

等待期间同时取得原macOS12小时自然恢复进展：02:07:08四角色已恢复，每worker仅替换一次，旧4PID/CoreAPI socket已消失，新PID路径经规范化核验。server-port的不可用采样为false，保留真实值、不要求所有角色同时观察到瞬态。恢复尾部至少运行到02:37:08，最终退出/清理仍待；Linux原长跑尚未自然到期。本次只读证据在`/tmp/shiki-default-native-recovery-observation-20261029-_79traj8`，第一版观察器把/tmp与/private/tmp原始字符串直接比较导致失败，规范化后复核通过，不是长跑失败或改变测试断言。两侧旧源码不覆盖本批修复。

## 本批完成：同源签名构建与诊断回收（2026-10-29）

<a id="evidence-current-release-validation"></a>

同一608文件快照分别在原生macOS arm64和普通UID1000 Linux arm64容器执行完整`SHIKI_VERSION=v0.0.0-local.20261029 SHIKI_SIGNING_KEY=... make release`。六个可执行文件与双插件清单来自当前冻结源码；可信源码verifier使用独立公钥、固定版本/目标认证，解包仅用认证私有副本。macOS客户端与Linux客户端/daemon真实helper仅暂存通过，无服务激活或ready声明。Linux构建108.485秒、21命令全0、236项断言通过；native1320项断言包含两份608源清单校验，不冒充1320个行为测试。独立OpenSSL与产物复核native101项/Linux138项通过，私钥已删除；Linux普通用户无遗留子进程，容器正常退出0、无OOM/强制回收，精确CID已移除。归档/清单/命令证据见当前源码发布构建。

<a id="evidence-memory-closed-ownership"></a>

另一条工作确认诊断持有`r.Close`会保留已关闭Runtime：真实daemon正对照保留绑定方法并GC，退出helper后weak清空与AddCleanup通知成功；诊断旧回调保留在同一测试下失败，成功关闭后释放回调则通过。原生红3.621秒、绿0.693秒、race1.726秒及tagged vet通过，无skip；初次port0夹具错误另列，不计行为红。失败关闭仍保留回调供defer重试。仅修改test/diagnostics，生产Runtime/协议/schema/capability不变；没有新负载或配对实验，也不把旧残余差值和运行中的增长认定为已解决。新诊断回归未声称Linux已运行。见所有权验证。

七类架构独立复核覆盖：目录/SDK-host-plugin依赖；RPC准入、版本能力与授权；持久/运行态、原子性、revision与去重；进程/启动关闭/取消恢复；文件终端TCP语义与限制；CLI/TUI配置及安装产物；行为证据与文档声明。全类别结合关键实现抽查未发现新增确定性偏差，不声称逐行证明整个仓库。独立报告位于`/tmp/shiki-release-independent-review-20261029/REPORT.md`。原插件目录与进程边界保持；release工具不成为新宿主服务，验签不自动执行或建立信任根，暂存不修改活动安装。三个构建源副本608文件逐份匹配，262份生产/构建输入与当前工作树一致；并行诊断修改另行记录，未混入已冻结发布源码。

08/15/16同步，原42条要求与顺序保留，当前29项已勾选/13项待验；正式身份/公钥分发撤销/真实安装源、外部平台与资源门槛保持。两条原12小时实验继续使用各自旧快照；本批签名构建或诊断结果不能外推到原长跑，也不能由原长跑追认覆盖本批改动。

## 本批完成：文件RPC准入

<a id="evidence-file-admission-validation"></a>

宿主和worker默认metadata32/transfer16/watch32三类共享限额、首帧5秒与取消登记已实现；SDK独立FileWorkerService可取消传输reader、Stop超时可重试完成。真实红绿及首帧/类别隔离/回收、最终同522执行输入macOS/Linux各10包race和vet通过，Linux正常退出/精确清理及608冻结文件哈希核验通过；2无测试包、1opt-in资源skip单列。未增加配置/能力或改持久schema；相关Phase0条目恢复，三份当前状态统一为29/42。见文件准入报告。

已有profile离线分析完成，18项产物哈希核验：HTTP2样本可对应runtime等待节点缓存，诊断Close方法值仍持有关闭的daemon对象图。没有新增资源短实验或生产清空字段“修复”；残余对象和运行期趋势仍未全部归因，见离线报告。

## 上一批交付与证据：离线签名及调用deadline（2026-10-29）

<a id="evidence-release-trust-validation"></a>

<a id="evidence-file-deadline-validation"></a>

- 后台同步期限：SDK unary默认5秒；文件为30秒+每开始64KiB一秒，保留更早父截止，覆盖首帧/中途/最终ACK；超时后未知提交保持日志并重观测。原生与Linux相同7份输入的受影响race、vet及架构检查通过，见截止时间报告。
- 离线发布认证：新internal/releaseauth和可信源码CLI，显式外置私钥签名、独立公钥验证固定版本/平台，同流哈希复制到私有目录。修复严格PEM与打包sidecar覆盖；核心/命名工具双平台race及vet通过。94,433,024字节现有开发归档实际认证后stage-only，通过独立102项断言；不冒充当前源码全产品构建或正式身份，见签名报告。
- Linux联合补验：595文件冻结源、9包及两个命名工具race通过，2包无测试不计通过，无测试skip/fail，三组vet通过；精确容器退出/删除和全部冻结输入哈希核验通过。原12小时两运行持续，未重启或借用其结果覆盖本次修改。
- 全架构七类复核和文档对齐完成；07/11/14/18及脚本说明同步，原42条要求文字/顺序保留。仅恢复Phase1缺口条目的勾选；Phase5签名/分发复合条目仍待外部发布条件。

## 当前进度

当前 **31/42项已勾选、11项待完成**；Phase 0–5分别为 **9/11、7/7、5/6、6/6、3/6、1/6**。首个全自动alpha及公开下载认证完成，新增关闭工程/实际发行地址和发布材料/支持入口两项。sync client停止超时重试清理缺口已修复，同测红绿及同源双平台相关包/实际插件链通过，插件生命周期项恢复。节点只读状态诊断/UX偏差已修复，有效同测红绿、双平台相关四包race/vet及.4实际包内验收通过，相关两项恢复勾选。宿主回调清理与晚启动回收修复已完成同源双平台相关验证和七类复核，两项恢复勾选。转发限额隔离缺陷已完成修复、双平台受影响与普通完整负载验证，恢复勾选。原42条要求不变。上一批sync日志、迁移栅栏、pending删除准入与SDK状态目录修复已完成同源双平台验收和独立证据核验，恢复共同基线条目勾选；终端语义项保持已勾选。首个公开预发布已完成；完整Phase 0–5、无泄漏、外部部署和稳定版交付仍未完成。

剩余11项已在[08路线图](./08-roadmap.md#剩余11项本地工作与外部门槛)逐项区分可继续本地推进的工作与外部门槛。已通过的本地回归不替代外部验收；已停止的短窗口内存实验不再追加，独立实现、已确认缺口的验收和文档审查继续并行推进。以下旧批次证据保留当时的源码与时间范围，当前工作以文首并行表和最新批次为准。

- **本批观测开销调查已完成并停止。** 只有8份诊断输入变化。两次短预检默认60/延后6个profiles均符合策略；唯一Linux正式配对的同源码/二进制/计划、136/128原始文件、各68份JSON及6关闭后profiles、工作量和清理独立核验通过。Agent末段heap差值−260,788 B/min受idle3高点回落支配，对象差值+9.98/min；关闭后+24,240 B/+228对象。所有数据保留，不能推定无泄漏；不追加该短窗口实验，见报告。

- **上一批请求生命周期修复与回归收尾。** 确定性红绿、真实TLS多流、并发取消和半关闭专项通过；Linux三组普通构建原始归档334/334/326份及清理已独立核验。macOS完整46包通过（E2E218.155秒、hostrotation225.822秒）；Linux串行完整46包通过（E2E217.938秒、hostrotation226.082秒）。两侧各15包无测试，6/10项skip不是通过。最终Linux测试源只比macOS完整源多一处syncack启动探测预算修正，原生四场景race23.050秒补验；生产输入相同，make lint通过。两侧32份终端捕获独立重放及限定夹具/容器清理通过。并发Linux原运行的插件构建signal:killed、冲突恢复超时及先前lint失败完整保留；未证明构建被杀或原超时的唯一原因。见报告。
- **跨schema服务验收完成。** A529/B569实际开发产物在Linux systemd完成1→2→旧程序拒写2→停写后仅恢复sync子树1→再次升级2并同步新文件。第三次正常poweroff退出0且精确CID消失；前两次夹具失败保留。root和独立review重核1098份A/B源码、6份工具及导出回执。保留撤销记录，不声称撤销Token实际RPC拒绝；敏感原状态未导出，删除容器后只复核留存摘要/回执与冻结断言。只覆盖B无新业务修改的受控恢复，不是自动产品恢复或历史正式发行兼容，见报告。
- **上一批资源与时长观测（历史）。** 修后Agent晚期load−control仍+15,597 B/min、+92.35 objects/min；保留端口缓冲峰及空载daemon峰。原12小时实验九PID仍存活，20:21:02 checkpoint为2103轮、6小时13分52.778秒、recovered=false；原495文件版本不覆盖新修复。Linux实际12小时尚未开始。

### 上一批已完成证据（历史版本）

<a id="evidence-journal-conformance-validation"></a>

<a id="evidence-terminal-state-validation"></a>

<a id="evidence-state-directory-validation"></a>

<a id="evidence-sync-journal-validation"></a>

<a id="evidence-memory-idle-control-diagnostic"></a>

- **终端语义与接管尺寸已完成。** 549文件冻结快照在macOS/Linux通过（2.27/0.84秒），各16份真实前端字节捕获独立重放一致，验证宽字单元、颜色、光标、alternate/main屏恢复、重连与接管后的当前尺寸；进程/容器清理通过。见终端语义报告。真实Claude账号及完整物理终端体验仍是独立门槛。
- **历史545文件内存对照已完成，当时增长未归因。** 545文件冻结快照的双平台四组串行运行完成，同平台使用同一二进制/源码；Agent后段load−control heap斜率分别为62,281/65,347 B/min。单对实验不能判断因果或认定无泄漏，且该快照早于本批sync日志修复。见配对诊断。
- **sync与SDK修复已通过最终双平台验收。** prepared/committed、末块前提交可能性、未知结果冲突保留、schema1迁移持久栅栏、固定staging/SDK状态目录FD及未来schema字段拒绝已实现。pending状态按冲突/操作/迁移路径并集计数；host删除准入核对真实paused、revision与pending，致命持久化失败后的状态调用在锁后复查lifetime。SDK原生state/storage/auth race（2.396/1.332/4.283秒）及Linux五个fsync点/新进程恢复、两个portable与两个ENOSPC分支通过（18.94秒，无跳过），root已独立核验。见sync日志和状态目录。
- **v3最终合并已通过。** 568文件/457份Go/proto/module使用同一源码manifest（SHA-256 `4499d601da33214dfbefc7f89c5566a6788f1a3ab0f445d805423da0c702bbbf`）；macOS/Linux均46包通过、15包无测试，显式skip分别6/10项，跳过不算通过。E2E204.984/192.045秒、hostrotation220.483/215.485秒、syncack45.395/34.394秒。两侧application/collector均退出0；root独立重放32份终端捕获一致，核验原生终端夹具六PID/runtime无残留及精确Linux容器消失，未将该清点扩大为全仓库进程独立清点。make build lint通过，最后夹具修改后的make lint及E2E vet亦通过；上一批root核验当时491份执行源（Go/proto/module/Python/shell）与v3逐份hash一致，凭据为final3下的`root-current-source-verification.json`。详见合并验收。
- **历史合并失败完整保留。** v1路由夹具失败及pycache导致collector失败分别记录；v2核心E2E只等磁盘删除就提前pause/unmap，正确触发PENDING_SYNC拒绝。仅修夹具等待真实active、pending=0、conflict=0，focused37.033秒通过，未绕过归档保护；随后v3整套成功不追改v1/v2为通过。
- **2026-10-28默认12小时中期观测（历史，非当前运行状态）。** 当时macOS原runner82454/句柄11096未重启；root于19:18:15（Asia/Shanghai）核验原runner/test82622/两host/四worker/pane共九PID存活。最新checkpoint为19:17:18、1744轮、5小时10分9.0735秒、recovered=false；最早10月29日02:07:04到期后还需至少30分钟尾部。495文件/411份源码早于本批修改，不覆盖新修复；Linux实际12小时尚未开始。该中途观测不计自然到期通过；后续原macOS已完成、原Linux失败及新697源Linux运行另见文首最新进展。见中途观测。

本批请求生命周期与跨schema服务验收及文档同步已收尾；下一批只推进明确剩余条件，实际长时运行和残余资源增长仍待完成。外部账号、历史发行、物理多设备、原生平台、重启/登录持久化和正式发布门槛分别保留。每批须全面复核并同步08/15/16，跳过不算通过，未验能力不得宣称支持。

## 上一批最终同源回归证据

源码：`/tmp/shiki-sync-journal-final3-20261028-uvp0mlm8/source`，568文件/457份Go、protobuf、module输入；具体证据目录、manifest、日志摘要及root核验见合并报告。以下均为包运行秒数，不是完整构建或验收墙钟耗时。

| 包/场景 | macOS arm64 | Linux arm64普通用户 |
|---|---:|---:|
| E2E | 204.984 | 192.045 |
| hostrotation | 220.483 | 215.485 |
| syncack | 45.395 | 34.394 |
| terminalstate | 6.559 | 3.855 |
| pkg/plugin/state | 4.666 | 1.807 |
| plugins/sync/client | 28.766 | 10.049 |
| architecture | 1.357 | 1.200 |

本表不将15个无测试包或6/10项skip视为通过，也不代替原12小时自然到期、资源增长归因、外部账号和发布验收。549终端专项和545内存配对保留各自冻结版本；最终终端语义在v3中已明确启用并独立重放。

## 历史批次：安装、在途轮换与五窗口诊断

以下保留安装529/在途530/内存527批次的原记录；其“本批”“待验”和计数仅表示当时状态，不覆盖上方当前结论。

上一批 **28/42项已勾选、14项待完成**；Phase 0–5分别为 **8/11、6/7、5/6、6/6、3/6、0/6**。原42条要求保持不变。此次同步补齐本批已完成工作，复合条目仍按全部条件验收；终端项因03规定的语义屏幕验收缺证暂撤勾选，不能仅凭marker/任意ANSI输出认定光标、颜色、alternate-screen和resize重连状态正确。

<a id="evidence-memory-windows-diagnostic"></a>

<a id="evidence-install-space-validation"></a>

<a id="evidence-host-rotation-inflight-validation"></a>

- **安装磁盘预检与备份修复完成。** 停旧服务前按真实目标文件系统估算staging/备份，停止后重新取有界清单和空间检查；固定目录FD，防备份包含自身，并补齐tar→backups目录→prefix的fsync。双平台install/helper整包race通过，Linux普通用户16MiB tmpfs九项无跳过，strace确认真实fsync顺序。529文件快照实际LaunchAgent上传活动升级/回滚90.41秒及真实systemd的13次生命周期边界通过。见安装验收。
- **在途文件自然轮换矩阵完成。** 530文件快照的client/server×upload/download在macOS/Linux八格均通过，原两场景也重跑通过；hostrotation整包220.396/211.039秒，架构检查通过，无失败/跳过。8MiB传输在真实部分内容阶段到期，验证旧公开目标、临时文件清理、基线、恢复及原stats流；内部30秒TTL、测试内宿主和真实worker子进程，不代替默认12小时或提交后最终ACK丢失验收。20ms采样是离散观察；最后仅修改一处代理header说明注释，未改执行语句。见在途轮换报告。
- **五窗口普通构建内存诊断完成，泄漏门槛未通过。** 同一527文件快照双平台319.58/320.33秒，各80次文件更新、160次TCP及等时空闲、334份原始归档核验和清理通过。Agent最后三个空闲点的post-GC heap仍约增加76,326/68,164 B/min；采样本身和维护流量影响尚未分离，不能宣布稳定或无泄漏。当时待做的相同11个采样边界空载对照现已完成，见545文件配对报告，归因仍未完成。见多窗口诊断。

<a id="evidence-conformance-regression-validation"></a>

本批完成全架构类别复核，插件入口/业务仍在plugins/sync、plugins/port，宿主internal/plugin与公共pkg/plugin依赖隔离；未增加协议、capability、默认TTL或发行承诺。最终make build lint通过。上述为专项、真实服务和诊断证据；上一批507文件完整race/E2E各44包通过（226.898/203.361秒）仅适用于该历史快照，不算本批全仓库复验。原502文件WatchStats失败因果仍未确认，505文件Linux失败及所有跳过均保留，见历史合并验收。

原macOS默认12小时实验继续原runner82454/句柄11096，未重启：2026-10-28 17:18:47（Asia/Shanghai）检查点为3小时11分38秒、1080轮、recovered=false，原宿主/四worker/pane仍存活。最早10月29日02:07:04到期后还需至少30分钟恢复尾部；原495文件/411份源码早于本批修改，结果只适用于原版本。Linux实际12小时待原实验退出清理后串行执行。先前中途观测不计自然到期或内存验收。

剩余本地验收包括终端语义状态、服务端提交后丢失最终ACK时真实worker重观察及远端再次编辑保护、内存增长归因和实际长时运行。外部账号、历史发行、物理多设备、原生平台、重启/登录持久化及正式发布仍分别待验；每批须全面对照文档并同步08/15/16后才能收尾。

以下其余章节继续保留历史实现与批次过程；“当前”“运行中”及计数仅适用于各记录，最新状态以本文顶部和08为准。

## 架构实施顺序

已经按 00–14 阅读工程、RPC、IPC、状态/资源归属、异常恢复、插件和安装约束。实施依赖顺序为：协议和传输 → 身份/持久化 → agent/daemon 生命周期 → 项目/会话/终端 → tsnet/插件 → 文件同步 → TCP 转发/诊断 → TUI/安装升级 → 跨平台/发布。

四份 proto 保持文档字段号、service、流方向及 go_package 分包，仅将占位 module 替换为真实仓库路径。协议可生成不等于相应方法已实现；当前只为实际实现的能力进行协商。

## 历史实现证据（各批快照，不等于整阶段验收）

| 要求 | 实现与可重复验证 | 仍需完成 |
|---|---|---|
| 工程、module、许可证、构建/CI | go.mod/go.sum、MIT、Makefile、固定版本工具脚本、双平台 CI 配置；本机协议编译与生成漂移检查 | 远端 CI 尚未运行；实际版本发行地址尚未产生 |
| 四份 schema 与真实 stub | proto/{types,shiki,plugin,agent}.proto；8 个生成文件；只有 plugin_grpc.pb.go | 方法的业务实现按阶段推进 |
| 本地 IPC/UID/代理 | internal/agent、internal/connection；真实 UDS 握手/白名单/实例及前端归属测试；单实例与 stale socket测试 | 跨版本迁移、长期运行及重启/登录持久化；当前登录域LaunchAgent和容器systemd已验收 |
| Connect v2 四种 RPC | internal/connection/transport_test.go：UDS/TLS unary/server/client/bidi、未 CloseSend 先收到响应、半关闭、最终错误、截断、取消、准入、压缩/大小拒绝 | 外部真实网络路径及长稳；本地 testcontrol/DERP 和双端插件代理矩阵已通过 |
| 状态、Token、request_id、配置 | storage/auth/config：原子提交/备份/锁、参数去重、拒绝未来 schema、条件配置更新、HS256/权限/跨进程撤销；已接入项目/会话/快照关联校验、64 MiB 读写上限与迁移前原始备份 | 长稳和安装升级场景继续验证 |
| tmux/PTY | tmux/session 测试及 server/rpc_test.go：真实普通 attach、多观察者重绘、原始字节、resize、控制权竞争、慢客户端、宿主 SIGKILL 后 pane 保活；真实 agent UDS→TLS daemon→tmux，观察者写拒绝、Token 撤销关闭流且保留进程 | 完整终端视觉验收及原生VPS；容器真实systemd保活已验收 |
| 插件双协议 | pkg/plugin、internal/plugin、test/integration/plugin：真实 AutoMTLS 子进程生命周期/CoreAPI 回调、凭据 TTL/scope、宿主 SIGKILL 清理；真实 client plugin→CoreAPI→TLS daemon→gRPC worker 四种 RPC、半关闭/终态/超限/错误/取消；生产 sync/port 双端集成及转发恢复已验证 | 最终发行产物加载/升级验收 |
| tsnet/NodeControl | internal/node：固定 SDK v1.98.10，官方本地 testcontrol/DERP 下两个真实 tsnet 节点；未登录 UDS 管理、交互登录/取消、身份保持、错误 key、四流/半关闭；隔离 Sys netstack 适配，禁止系统 TCP 回退 | 官方控制面的 key 过期/一次性复用/审批/撤销、外部 P2P/DERP 与长期运行仍未验收 |
| 四条版本边界、多设备 | 四层独立进程合成版本矩阵、同地址重连能力收缩、未来schema拒绝/测试迁移备份，双平台通过；本地双agent终端接管 | 历史正式产物组合、物理多设备与长期运行验收 |

用户已确认暂时没有 VPS 或外部 Tailscale 测试环境；继续本地和 Linux 容器开发，不把外部登录策略、网络路径或真实部署门槛标为通过。

当前阶段没有整体完成。真实systemd容器保活和原生LaunchAgent生命周期已通过；外部控制面认证策略、原生VPS、重启/登录持久化仍未验收。本地测试控制面和容器证据只支持其实际覆盖范围。

## 产品闭环与本批一致性检查

- `cmd/shiki`、`cmd/shiki-agent`、`cmd/shiki-daemon` 可构建运行。CLI 自动启动单实例 agent，管理服务器、项目/会话/映射、Token 和插件；所有前端终端经 agent IPC，通知不写入终端原始字节流。
- `test/e2e/core_test.go` 的真实进程 TLS 测试验证四 CLI 并发启动、controller/observer、Ctrl-Q、daemon/agent SIGKILL 后身份与 pane 保活、Token 撤销后断流及重新认证。Claude 使用可控 shell fixture，不能视为真实账号登录验收。
- 同一 E2E 已接入真实双端 sync 子进程，覆盖首次显式冲突、默认排除秘密文件、无前台后台上传/下载、双方编辑冲突、agent 重启保留冲突、解决冲突、条件删除、暂停/解绑/停用。可见文件替换与最终 ACK/基线持久提交分别等待，避免测试掩盖实际并发冲突。
- 插件入口及业务按 07 迁至 `plugins/sync` 和 `plugins/port`，宿主与插件生产依赖双向隔离；SDK 不依赖宿主 `internal`。双端 capability、manifest、默认安装目录按 09/04 修正。`test/architecture` 防止回退。
- 同步使用三方共同基线、条件分块提交、受根限制访问、持久恢复日志/历史、递归 fsnotify 与周期重扫、任务归档及有界重试；不声称能对不合作的外部写入提供文件系统 CAS。
- port 插件真实双进程与 CLI E2E 已验证 loopback、默认拒绝与显式端口授权、TCP 半关闭和最终 RPC 状态、限额/取消/离线清理。宿主持久注册/重启恢复、无拨号权限预检、双端 allowlist 自动策略及显式停止抑制已完成并通过 macOS/Linux 验证；未知归属不自动授权，扫描失败关闭自动运行态但保留期望及手动活动流。最终自动策略 E2E 分别为 104.48/110.74 秒。
- 配置命令 `get/set/edit` 通过 agent 类型接口读写偏好/服务器记录，使用 revision 防止覆盖并发修改，真实 UDS 测试通过；凭据不导出，bootstrap 文件不被前端修改。
- 配置快照/模板、系统/会话监控、可选有界 Claude 自动重启已有真实 tmux 测试。自动重启默认关闭，停止抑制及次数预算持久化，未知退出码报告 unknown，不因无输出杀进程。
- TUI Dashboard、创建/删除/重命名/编辑、attach/observe/接管、当前屏幕和终端交接已有行为测试；受限 Token 真实集成验证无需 system.info 也可使用已授权接口，TUI 根据实际权限隐藏变更动作。stats/doctor/脱敏 logs 已实现，实际网络路径展示仍随当前连接策略批次验收。
- 连接批次已修复并专项验证 tsnet 优先、仅网络失败显式 TLS 回退、有界后台重连及耗尽后恢复；连接/安装合并版本亦通过 macOS 全套 race 与 Linux 全套含 E2E。后续已修复空闲/过期 peer 的伪线路展示，以及CLI connect/doctor额外system.info依赖并专项验证。安装事务审计修复和真实tar/helper/wrapper隔离staging/重复/篡改拒绝均已通过，安装本地应用就绪/异步登录/显式初始Token闭环已补齐并通过真实helper与TLS专项，该历史专项未激活服务；本批已另行完成真实systemd/LaunchAgent生命周期验收，见下文。
- 后续按 05/11 逐步复核：smart 活动菜单/独立 verbose、受限 Token connect/doctor 和 TUI 切换时旧scope清理已补齐并通过真实 UDS/TLS/PTY 测试。首次同步向导、项目目录选择和退出恢复菜单已有真实 PTY 专项证据，未知退出不会伪造退出码；目录/保留日志后端及真实 agent 代理授权、实时退出与 signal 有界重启已通过 macOS/Linux race；未知退出持续观察且不自动重启。安装器应用就绪/交互初始 Token/异步登录等待也已通过本地专项和最终产物验收；最终冻结Linux首次合并发现的问题已修复，macOS/Linux整套race含E2E复验通过。
- CLI文档与帮助最终复核修正必填参数、快照命令路径、插件side/server、日志范围和配置编辑语义；69个应用帮助入口全部成功，CLI/架构race通过。协议、代理、权限与capability对照一致；路线图对应文档一致性条目已勾选，未扩大外部/发布验收范围。
- 新增同机两个独立 agent 的 macOS/Linux 真实 E2E，验证各自状态/身份/映射、同revision控制权竞争、只读拒绝、停止一端不取消另一端原有流/后台任务，以及离线编辑冲突保留/过期解决拒绝/最终收敛；分别通过 13.671/27.587 秒。它是本地双agent验收，不替代物理多设备或外部网络验收。

本批偏差、修复和测试记录见 [16 架构与文档一致性检查](./16-architecture-conformance.md)。后续每批仍需复核所有已实现模块，不能用旧通过结果覆盖新变更。

## 版本和验证环境

<a id="evidence-benchmarks"></a>

<a id="evidence-resource-regression"></a>

<a id="evidence-platform-validation"></a>

- 本机：macOS 27.0.1 / Darwin 27.0.0，arm64，初始 Go 1.26.3、升级后 Go 1.26.10，tmux 3.7b。
- Connect 运行库及生成器 v2.0.2；protoc 31.1；protoc-gen-go v1.36.11；protoc-gen-go-grpc v1.6.2；grpc-go v1.85.0；go-plugin v1.8.0。
- grpc-go 的传递约束要求 protobuf **运行库** v1.36.12；消息**生成器**仍为 v1.36.11。两者区别已记录在工具说明。
- 系统 Homebrew protoc 存在动态库问题且版本不匹配，采用校验过的隔离工具，不修改系统安装。
- 不能直接使用尚未核验的最新版 tsnet：查询到 v1.104.2 要求 Go 1.27.3、v1.98.10 要求 Go 1.26.5。当前选择固定 v1.98.10，最低 Go 更新为 1.26.5、工具链/CI 固定 1.26.10，保持 1.26 系列；SDK 已接入并使用官方测试控制面验证；没有使用真实外部账号或公开登录链接。非稳定的公开 Sys/netstack 依赖隔离在 sdk_adapter.go，SDK 升级必须重验。
- Linux arm64 / Debian bookworm 普通用户容器已完成 Phase 0 基础整套 race 回归；修复并复验 tmux pane 格式、locale、空服务器探测和插件 socket 路径问题，tmux 包额外连续运行 10 次通过。随后连接策略/安装事务合并版本通过 macOS 全套 race、Linux 全套含 E2E（151.445 秒），macOS 独立 E2E 通过（146.604 秒）。八份协议文件隔离重新生成逐字节一致，模块校验通过；构建成功，lint 检出的 import 排序已修正。后续节点路径、CLI 权限/智能菜单及安装就绪闭环按各自新源码继续验收，历史结果不覆盖新改动。
- Linux amd64 与 Darwin amd64 交叉编译通过；Darwin amd64 新增 Rosetta 实际版本、临时状态 IPC、停启/新 instance smoke 已通过。固定 Go 镜像的 amd64 层随后成功拉取，Go1.26.10 linux/amd64 已实际运行，普通用户八项 IPC/TLS/真实插件/半关闭/tmux race 回归全部通过，无跳过。模拟/翻译结果与原生硬件区分，原生 Intel 与 WSL 完整验收仍待完成。该翻译测试容器不运行systemd；独立arm64 systemd PID1/cgroup验收已完成，见下文，不能外推至amd64。
- 初始govulncheck v1.1.4仅CLI导入模块扫描未命中，不能代表全部宿主。现已改全仓库package扫描并升级compress1.18.5→1.18.7修复GO-2026-5841；复扫无受影响的已导入包，保留未导入openpgp的GO-2026-5932模块公告。远端CI未执行。Linux 测试曾在官方 Debian 源下载阶段失败，该次未运行测试；后续使用显式 HTTPS 镜像并保留官方包签名校验的冻结源码复验通过，详情见 16。
- 内嵌产物的 Linux arm64/amd64 普通用户真实 sync 子进程/CoreAPI race 分别通过（1.322/1.613 秒），四宿主目标版本与 staging 清理通过；amd64 仍属翻译执行。Node.Active 路径复核的最后 Linux 专项已通过，最终脚本退出 0。可复现平台命令见 [相关验收摘要](#evidence-platform-validation)。
- 新增本地传输性能基线 `BenchmarkRPCTransport`：真实 UDS/loopback TLS 上的 warm unary、1 MiB client stream、bidi 打开至半关闭终态，macOS arm64 和 Linux arm64 普通用户各三轮短样本通过并记录 allocations。详见 [相关验收摘要](#evidence-benchmarks)；不包含业务认证、磁盘、tsnet/WAN 或产品完整负载，不能作为生产容量结论。
- macOS/Linux 各24轮终端 attach/detach/cancel、真实插件停启/父取消、生产TCP半关闭/idle停止资源回收全部通过；每轮真实socket/attachment/进程回收，FD与测试宿主goroutine不增长。最后终端整条E2E分别281.96/277.17秒；仅opt-in分支给实际子进程启用race及相应有界总时间。复现与资源阈值见 [相关验收摘要](#evidence-resource-regression)，不称为生产长稳。

- 子进程输出上限复核修复 tmux capture 与 port scanner 匿名嵌入 bytes.Buffer 导致 io.Copy/exec 绕过 Write 限额；真实超大 stdout/stderr 与复制快路径回归在 macOS/Linux arm64 通过，Linux 包分别 2.028/2.058 秒。scanner 加入有界 WaitDelay；诊断命令同类问题由安装就绪批次一并验收。

- 最新目录/保留日志/实时退出/有界重启批次：macOS与Linux arm64普通用户的project/session/tmux/server/agent/auth/architecture七包race全部通过；真实权限代理、锁等待截止、输出上限、未知退出不重启及已知信号持久预算均验证。macOS全普通套件race通过，完整E2E180.243秒通过；安装实例一致性与真实helper专项已通过，最终macOS/Linux整套race含E2E均通过（190.197/173.475秒）。

- 最终darwin/arm64发行包smoke：LICENSE与仓库、release.json、SHA256SUMS一致；真实解包helper两次stage-only及install-client.sh wrapper通过，大小变化/同大小LICENSE篡改均拒绝，未激活服务或写入实际配置/状态。真实helper/daemon/sync bootstrap E2E26.60秒通过，覆盖Token输出、普通probe不增发、TLS认证与依赖异常pending。

测试命令：`make test`、`make lint`、`make build`；专项可运行 `go test -race ./internal/connection ./internal/auth ./internal/server/... ./internal/agent ./pkg/config`。测试输出中的 skip 不是验收成功。插件跨进程集成使用 `make test-integration`，Linux 普通用户容器使用 `make test-linux`；端到端套件使用 `make test-e2e`，已覆盖下列本地闭环；发行构建和服务模型使用各自独立验收；普通全套中的opt-in skip不算对应场景通过。

上一批冻结合并复验通过：TUI输入关闭竞态、状态读取省略字段残留、安装socket创建竞态均已修复；对应macOS/Linux完整race含E2E全部通过。本批服务生产修复和compress1.18.7的新全套回归正在执行，尚不沿用旧通过结论。路线图TUI/退出监控项已恢复勾选，首次失败及修复证据保留在16。

## 真实服务与安全批次进度（2026-10-28）

<a id="evidence-soak-validation"></a>

<a id="evidence-launchd-validation"></a>

- 原生macOS27.0.1 arm64、UID501 LaunchAgent生命周期67.41秒通过：真实安装、sync/port进程、SIGKILL重启/新instance、旧前端拒绝、显式停止15秒不重启、A→B升级/B→A回滚、卸载保留数据；标准job/plist最终不存在。独立SIGTERM取消清理已验证，不算第二次生命周期通过。见 [相关验收摘要](#evidence-launchd-validation)。
- Linux arm64真实systemd252.39作为容器PID1，daemon与user-agent均以UID1000运行；42.86秒完整流程通过，同一tmux pane跨11次停启/崩溃/升级/回滚/重装保活，旧插件回收，身份/原Token/配置/状态保留，systemctl poweroff退出0。独立取消场景确认容器清除。仅私有cgroup追加SYS_ADMIN，无privileged或宿主cgroup挂载；见 [相关验收摘要](#evidence-systemd-validation)。
- 实测发现并修复三处偏差：daemon禁用节点状态统一为13规定的offline/NODE_DISABLED；daemon初始化不再把受管不可变插件产物当私有状态目录；systemd WorkingDirectory使用有效的~用户home语义。已有专项和真实服务证据；新全套回归正在运行。
- 审计65处生产Snapshot.Get调用，新增实际TLS重放时重新检查当前Token/权限/撤销，以及持久状态namespace/请求intent/主文件rename失败行为验证；双平台专项通过。全仓库package依赖扫描替代原CLI-only扫描，compress已升级1.18.7，四发布目标静态扫描无受影响已导入包；未导入openpgp仍有模块公告，不称所有依赖无漏洞。见 [相关验收摘要](#evidence-security-validation)。
- 持续负载工具最终1分钟自检macOS/Linux通过：26/27轮，8,585,216/8,650,752字节，平均同步收敛305/225毫秒，固定FD/goroutine阈值通过；独立注入真实子进程race确认报告会使测试失败。早期1小时尝试因发现测试证书有效期不足主动中止，不计完成。修正后新的双平台1小时冻结源码测试正在运行；生产长稳仍未验收。见 [相关验收摘要](#evidence-soak-validation)。

08已勾选服务保活原型；Phase4安装复合条目保留未勾选并列明本地已完成及外部/发布门槛。42条原始要求不变，本次没有将整阶段或外部部署标为完成。

## 后续交付清单

| 阶段 | 未完成的主要交付 |
|---|---|
| Phase 0 | 远端 CI/真实发行地址、外部认证策略、跨版本/物理多设备门槛；原型复盘与条件估算已在17完成，已完成项已同步路线图 |
| Phase 1 | 完整终端视觉体验、真实Claude账号与跨环境验收；新终端背压/最终状态修复已有专项与最终合并证据；CLI/菜单/映射条目已勾选，首次同步/目录/保留日志/恢复菜单及实时事件已有双平台专项和macOS完整E2E |
| Phase 2 | 外部 tsnet 策略/实际 P2P/DERP 与长稳；tsnet 优先/显式 TLS 回退、运行期路径和双端 sync/后台恢复已实现并专项验证 |
| Phase 3 | 转发限额隔离已修复并通过双平台行为/普通负载，恢复条目；其余诊断/退出监控保持历史限定验收；共享状态完整读取已修复，真实部署仍待。退出码或信号已知时允许有界重启，tmux缺失原因时保持unknown、不自动重启；不承诺任意平台都能恢复退出原因 |
| Phase 4 | 本地启动策略展示已通过双平台相关验证及真实systemd四态；物理多设备完整体验、原生VPS/重启登录持久化、外部登录、既有用户迁移和原生平台/历史发行产物等完整在途升级场景；真实Linux容器/macOS后台服务下安装/升级/回滚已验收；安装本地就绪/初始Token/登录等待、事务审计、真实产物暂存与篡改拒绝、TUI、快照/模板、配置命令已验证；文档一致性条目已勾选 |
| Phase 5 | 完整平台运行、长稳/性能/最终安全、正式签名信任链/分发渠道/发布与支持入口；已有故障测试、依赖扫描、本地打包校验和版本匹配证据 |

GUI、WSS、文件级快照和三方自动合并仍为文档约定的后续范围，不计入本次首版。没有对工期、性能或未运行环境给出成功承诺。

本批复核进行中：新macOS全套已出现无效auth key测试等待超时，正在调查；另发现agent诊断节点错误广告node.login.v1，正在按实际能力修复。新全套尚未通过，既有通过结果仅适用于各自冻结源码。

持续负载状态更新：本轮macOS一小时尝试在前置撤权步骤等待CLI退出15秒超时，尚未进入持续计时，已记失败并调查；Linux一小时冻结源码任务仍在运行。另查出06要求的同版本5分钟3次agent崩溃熔断尚未实现，已安排本地修复，不将其归入外部环境门槛。本批仍在进行。

最新专项：诊断node登录能力偏差已修复并通过双平台agent/install/architecture race；node新attempt发布立即退役旧generation，并校验SDK启动/watch失败/迟到worker的实例边界。确定性回归在旧代码失败，最终两平台完整node包各10次race通过（206.603/168.413秒）。首轮合并Linux通过（E2E178.950秒），macOS因node旧回调失败而整体失败（E2E198.318秒单包通过）；不覆盖后续修改。

终端在本地PTY输出堵塞时消费不到远端失败已确定性复现并修复；独立接收、最多64帧/1MiB排队、异常取消、正常EOF排空的首轮双平台10次终端专项通过（53.162/15.283秒）。发送/接收同时结束的状态仲裁补验及新合并待完成，08终端条目暂撤勾选。agent崩溃熔断实现/服务实测进行中。17原型复盘与条件估算已审阅并同步08，仅完成复盘条目，不代表Phase0完成。

本批修复后的最新专项：agent崩溃熔断已实现并通过真实三次SIGKILL、并发CLI、明确停止/显式恢复，以及原生LaunchAgent106.81秒和真实systemd完整生命周期复验。systemd同一pane跨13个操作保活，第三次agent异常后诊断PID稳定15秒且无worker；正常stop退出0且15秒不重生。终端最终实现（含发送/接收终态仲裁）双平台各10次race通过71.096/22.584秒，独立并发终态专项亦有修复前失败/修复后通过对照。build/lint/vet/module校验通过；最终双平台整套正在执行，macOS新一小时实验已进入计时，Linux此前冻结实验仍运行，均未计完成时长。

本批最终同步：macOS/Linux完整race/E2E207.127/177.266秒通过，08终端恢复勾选，服务原型/复盘新增勾选，总29/42；三份状态一致。首次macOS node失败、最初soak前置超时、各确定性红绿对照均保留。普通全套root-only安装和opt-in资源循环跳过，mac另跳过opt-in LaunchAgent；隔离root安装1.058秒与真实服务本批单独通过，24轮资源证据沿用先前专项。build/lint、含e2e vet、go mod verify、diff检查和架构协议检查通过，未变schema的8份协议重生成对照继续一致。完整产品仍待上述本地、外部、平台及发布门槛。

新批次进行中：真实四层跨版本、活动传输升级/回滚、隔离磁盘满及fsync故障验收。已确认并正在修复agent按当前协商结果限制代理方法的缺口；版本拒绝诊断也需提供安全且有界的双方范围/缺失能力说明。helper帮助已修复（真正--help/help/子命令帮助在账号/凭据/目录读取前退出0，version未知参数拒绝），双平台整包race1.508/1.018秒通过。18发布准备草稿补平台证据、升级限制、诊断命令及未完成发布元数据；README清除真实服务未验收的过时状态。新批次完成前不宣称全部代码再次合并通过。

<a id="evidence-fault-validation"></a>

本批持久化故障验收已冻结：新增test/fault及scripts/test-faults.*。macOS portable/state/auth/filetree race1.650/2.743/2.717/4.430秒、Linux相关state/auth/filetree1.287/2.194/2.126秒通过。独立UID1000、无网络/cap-drop=ALL/no-new-privileges/只读rootfs容器18.26秒通过5个fsync EIO点及各自新进程恢复、两种真实rename/auth失败和两个8MiB tmpfs ENOSPC分支；选中场景无skip，容器退出0且移除。写入结果不确定时仍重观察，不回滚撤销或覆盖当前文件；无生产改动。见[相关验收摘要](#evidence-fault-validation)，不宣称物理掉电、持久磁盘耐久或穷举提交点已通过。

最新持续负载结果：Linux普通用户冻结源码的一小时实验已完整通过，计时1h0m0.384s、1,577轮、491,651,072 payload字节，收敛min/mean/max53/267/1007ms，全部固定FD/goroutine检查与尾部恢复/清理通过，总测试3714.54秒退出0且容器消失。353文件源码hash已从实际只读挂载保留；该快照早于后续节点/终端/崩溃预算/capability修复，不能覆盖那些新代码。RSS/CPU仅观察值，不据此宣称无所有类型泄漏或生产容量；详见[相关验收摘要](#evidence-soak-validation)。macOS一小时实验仍在运行。

新版本/能力修复专项已经双平台通过；helper进一步拒绝非install的stage-only和不适用布局/产物参数，帮助按命令展示，uninstall wrapper只在验证helper后剥离bootstrap bundle/pin。实际本地bundle/helper/wrapper证明错误stage返回2、普通缺安装返回1且不创建目录。新完整合并正在执行，macOS认证测试在新Token绑定前过期而失败，正在调查秒级JWT与一秒夹具有效期；该轮macOS不得记整套通过。未完成批次保持进行中。


## 进度同步纠正与最新合并结果（2026-10-28）

此次发现的记录偏差是：专项报告已有完成结果，08的页首/条目却仍写“正在调查”，并在文末追加新进度，使当前状态与历史日志混杂。现将08整理为当前条目状态和剩余门槛，历史失败/修复仍保留在本记录及另一份实施/复核记录；15/16页首同步当前结论。恢复文档/帮助/capability一致性勾选，42条原始要求不改，计29项已勾选、13项待完成。复合条目的部分完成必须直接更新条目说明，不能只写到独立报告或等整个Phase结束。

<a id="evidence-version-validation"></a>

四层版本专项已完成，macOS/Linux独立进程矩阵40.604/43.422秒通过。修复按当前原子协商快照的逐方法capability准入、mapping的major/minor/required agreement验证及forward minor验证；双方版本/缺失能力诊断仅通过严格白名单ErrorInfo重建。旧连接同地址对端替换后重新协商，缺失能力仅阻止受影响方法，不启动进程内业务fallback。未来schema原文保留，测试专用迁移不广告为任意历史格式支持；详见[相关验收摘要](#evidence-version-validation)。

独立审查另发现维护命令忽略--stage-only会继续执行真实操作，已在任何账号/目录访问前拒绝不适用参数（含显式false）；帮助按真实各命令参数生成。uninstall wrapper验证本地bundle/helper后只剥离bootstrap参数。实际wrapper拒绝stage-only返回2，缺安装返回1且目录未创建；helper双平台race1.495/1.025秒通过。18修正交互probe可能启动登录的描述，不再称所有probe只读。

首轮最新macOS全套仅auth的TestRefreshIsAtomicAndExpiryCancels失败：10:37:54.939568开始，到10:37:55.048419绑定时，按生产JWT整秒exp生成的一秒Token已过期；拒绝符合契约。仅将测试建立有效流的fixture调整为三秒，并保留原子refresh/revoke断言、增加JWT/registry expiry一致性、不提前取消、实际到期取消及过期拒验。生产TTL/校验/monitor未改；双平台各20次race60.769/60.655秒、完整auth包4.312/3.710秒通过。失败日志保留，不用一次重跑抹去原因。

最终macOS完整race含E2E退出0，40包通过、E2E196.232秒，日志/tmp/shiki-version-fault-merged-macos-final.json。Linux完整race同范围退出0、40包通过、E2E183.381秒，日志/tmp/shiki-version-fault-merged-linux.json；Linux全套早于上述test-only调整，后续auth专项独立补证。build/lint、go mod verify及diff检查通过。普通全套的opt-in/root-only跳过不当作专项成功；真实故障、服务与持续负载各用独立报告记录。活动升级仍待完整结果；下文补记macOS一小时完成结果，不因限定实验提前关闭Phase4/5复合门槛。


最新持续负载补证：macOS冻结源码一小时实验完整退出0，计时1h0m0.107s、1,540轮、480,378,880 payload字节，收敛min/mean/max135/318/918ms，总测试3700.65秒。固定FD/goroutine检查、尾部冲突/恢复/清理均通过；六个跟踪进程和临时源码目录独立确认消失。日志/tmp/shiki-soak-macos-terminal-final.log及源码清单/结果JSON已保留，资源观测表见[相关验收摘要](#evidence-soak-validation)。该快照包含终端/节点/崩溃守卫修复，早于本批能力/helper修复；不由RSS观测声称内存无泄漏。Linux一小时结果保持先前快照范围，两平台均不替代生产长稳/凭据轮换。

活动升级当前仍未通过完整场景：/tmp/shiki-systemd-pu0im7nr中agent升级/回滚分别18.56/18.30秒通过，daemon升级后文件恢复/冲突显式解决/终端重连完成，随后ListForwards连续deadline exceeded，30秒轮询失败。新冻结运行补充user-agent journal和进程证据定位；尚未确定生产根因，不通过加大超时或空闲升级结果关闭门槛。

本批独立只读架构复核确认插件路径和SDK/宿主依赖方向、逐方法capability准入、mapping/forward协商核验、安全ErrorInfo白名单及helper操作参数边界符合文档。状态/授权/生命周期/终端/文件/TCP结果仅限对应专项与完整回归范围；活动升级新失败继续调查。08/15/16当前状态一致，42条要求逐条与HEAD核对未变（29已勾选/13未勾选），文档本地链接与diff检查通过。


## 活动恢复、清理契约与轮换批次（2026-10-28，历史过程记录）

上一轮是有效进展：三文档纠正、42项要求核对与双平台一小时完成证据已落盘。当前继续完整Phase0–5目标，本地与Linux容器范围不变。

活动升级失败已定位为实际恢复缺口，而非ListForwards RPC死锁：daemon正常停止使活动Port流返回Canceled，worker清理监听后报告FORWARD_STREAM_FAILED；连接池随后认证/协议验证仍ready，但same-route对象gate永久跳过重新绑定。真实agent+port worker/认证TLS回归先失败14.956秒；拟仅对已结束流原因允许同一已验证route重建监听，旧TCP及数据不复活，绑定/授权失败和显式停止抑制保持。修复与真实systemd四边界复验尚未收敛。

全面契约审计新增确证缺口：docs06 auto_cleanup配置（默认关闭、idle_threshold、warn_before）没有schema/调度/预警实现，docs05的清理建议也未实现；Session.last_active仅创建时赋值。它们不是外部门槛或后续豁免。正在补显式Stop资格、真实dead pane且无attachment的持续观测、独立持久预警窗口和条件删除；活跃shell/未知状态不删除，项目文件不受清理影响。last_active以有效附着/用户输入更新，不以Claude输出或静默推断闲置；本批未完成前不广告新capability。

插件12小时默认短期凭据已有到期回收代码，但原测试只证helper退出，未证真实sync/port业务重建；正在用显式测试TTL和真实worker/Supervisor补到期取消回调、旧PID/socket回收、新instance及业务恢复证据，不改变生产TTL或时钟，也不冒充默认12小时全链长稳。监控磁盘生产逻辑已覆盖state目录与无session项目根，本批补缺失路径必须unknown、inventory错误不得伪造空列表的行为验证。


本批已完成独立轮换专项：test/integration/rotation实际运行生产Supervisor及四个sync/port worker，使用既有Config.CredentialLifetime的12秒客户端夹具，生产默认12小时不变。双平台每场景各3轮race通过，macOS102.456秒、Linux普通UID1000容器98.938秒，无选中skip/子进程race报告；vet/架构检查通过。实际到期取消旧CoreAPI回调、回收旧PID/移除socket，新instance恢复双向SHA256文件和原loopback监听；旧TCP终止，新连接327680字节+半关闭尾部正确且无重放。宿主mapping/forward期望及Ready重放是fixture，远端中间链为UDS；不代替真实Resources/服务端自身到期或默认12小时整链。两次夹具失败与边界在[相关验收摘要](#evidence-rotation-validation)保留，无生产修改。

磁盘观测新增行为证据：无session的注册项目仍进入磁盘根集合；指定session仅选择其项目，未知session/cancel不回落全量。任何项目根或state目录消失时disk_free_bytes为unknown，其他内存观测保留；项目枚举失败返回错误，不伪造空列表。macOS monitor/session重点race1.121/1.163秒、Linux1.008/1.070秒通过，无skip；日志/tmp/shiki-disk-observation-linux-20261028.log。该专项补文档06监控边界证据，不宣称容量/性能或所有平台门槛完成。


本批第一轮完整合并与实服务结果：macOS完整race含E2E退出0、42包通过、E2E212.847秒；Linux完整退出1、40包通过，E2E185.827秒自身通过。Linux两项失败分别为test/integration/port的TestTwoProductionProcessesHalfClosePolicyAndStop（process_test.go269，unknown: write envelope: EOF）及session.TestAutomaticRestartBudgetSurvivesManagerRestart（Attempts1、EXIT_REASON_UNKNOWN未达到预算，8.32秒超时）。已分别安排抓取实际流终态与tmux退出观测，未放宽安全断言，未用专项通过覆盖全套失败。对应日志/tmp/shiki-cleanup-upgrade-merged-{macos,linux}-20261028.json；376源码hash与Linux实际只读snapshot全部一致。

真实systemd活动升级完整已通过：/tmp/shiki-systemd-3593x8vs/evidence.json，190.09秒、poweroff退出0且容器移除。agent升级/回滚19.63/18.37秒、daemon升级/回滚12.91/13.15秒；同一pane1864跨13边界，8MiB文件的256–512KiB部分传输实际观察、终端/TCP无重放、新监听恢复、冲突保留再显式解决、临时文件及旧host/worker清理全部通过。它使用本批首次冻结源码；不覆盖下面追加修复，也不代表历史发行版或原生VPS验收。

独立最终架构审查另发现真实cleanup偏差：watchProcesses先执行restartStep，若最初全局tmux.List失败则跳过cleanupStep，仅清稳定观测，仍保留旧Since/Warning/Due；下次成功可能立即按过期计划删除，违反06“观测失败取消旧计划”。正在补失败latch及事务清计划、失败跨Due/清理持久化失败回归。生产freeze暂撤；08文档/capability一致性项暂撤勾选，当前28/42、14项待完成。自动清理其它双平台十包专项、build/lint/module、8份固定生成文件对照与376源hash证据保留，但不能替代追加修复的最终验证。


追加收敛证据：全局tmux.List失败取消计划已修为maintenanceStep统一失效latch；清计划写盘失败时继续保留latch并报告unknown，下次先成功清除旧计划才允许重新观察。cleanup时钟回拨改比较UnixMilli墙钟，避免time.Time单调部分掩盖实际墙钟倒退。Linux20次观察失败/跨Due与写失败恢复专项通过；最终整套待重跑。

端口EOF经确定性测试证明是提前拒绝时发送端先失败、接收端后暴露最终领域状态；原集成用例过早Fatal，没有核验ResourceExhausted。独立代码审查同时发现RemoteCore端口首CONNECT/文件首header及agent终端首Open会丢该终态，已用共享internal/connection.FinalSendError修复：仅EOF取一次最终响应、最多1秒收尾、父取消和硬错误不另读、nil/EOF不能把失败发送升级为成功、不重放请求。agent真实UID检查UDS+合成下游顺序测试先红四分支，修后双平台各3次race通过4.594/4.069秒，日志/tmp/shiki-terminal-admission-{before,macos,linux}-20261028.log；覆盖PermissionDenied/ResourceExhausted安全重建、空响应失败、停滞响应独立上限。插件host及daemon上传同源路径由独立专项验收，完整结果待合并。

Linux自动重启测试进一步捕获两次真实tmux3.3a pane dead=true但退出code/signal均缺失；即使FIFO确认live再放行exit7，20次仍有2次未知，不能称调长超时或FIFO已解决根因。生产不凭预期exit7推断原因、停止自动重启符合06；正在将严格已知退出预算与真实未知退出保守行为分开验收，两者都保留强断言和实际known/unknown计数。原失败保留，根因未证明，不称上游缺失已修复。


最终重跑仍未收敛：macOS TestAgentCrashLoopSharedByCLIAndProcessStarts实际出现并发CLI启动读取agent.sock的mode不是0600（agent_crash_guard_test.go104/126），而不是绕过熔断。ListenUnix在公开路径bind后才Chmod，存在发布前权限窗口；客户端拒绝符合安全契约。正在修为先设置私有socket权限再原子无覆盖发布，不放宽UID/mode验证，不以全局umask影响并发进程。该轮完整测试仍等待尾部，不能记macOS通过。日志/tmp/shiki-cleanup-upgrade-final-macos-20261028.json。

已知退出/未知暂停分开后的严格预算专项两项各20次通过：macOS60.696秒、实际known60/unknown0；Linux53.742秒、实际known49/unknown4。确定性exit7每次仍要求原创建+2次重启=3、跨manager不补预算、Stop抑制；真实unknown要求dead无退出原因、NextAttempt0、不伪造LastExit/Signal、实际启动次数一致及重建manager后不再启动，不计作known预算证据。生产未知不重启契约未改。

端口集成又暴露夹具只等TCP EOF就进入下一连接、而server单连接名额尚未随RPC终态释放的前置条件错误。现先完成完整字节/半关闭，再等真实client worker Connections归零且listener仍有效，才停止或发新请求，不重试payload。test-only最终macOS/Linux各10次race通过40.674/46.133秒；该修改在最终全套快照之后，双平台专项独立补证，hash067d9f117e11a158d1d4c30bf5701bd50e07f598f97bea5cbb75c305822dc110。shared FinalSendError在nil/EOF终态时返回不包裹io.EOF的Unavailable，防后续把失败误识为clean EOF；mac重点connection/plugin/agent通过2.142/1.668/2.554秒。最终systemd正在独立复验，须按实际冻结范围记录。


## 本批收尾复核与进度同步（2026-10-28）

本批路线图补齐自动清理/活动时间、同路由端口恢复、客户端凭据轮换、真实活动升级、磁盘未知观测及UDS原子发布证据；42条原始要求逐字保留，复合项没有仅因部分实现而勾选。08/15/16当前计数一致，最终完整回归尚未通过前保留28/42。

<a id="evidence-uds-publication-validation"></a>

UDS原始bind-public→chmod实现通过编译overlay同一测试确定性失败，修后权限就绪再hardlink无覆盖发布。临时alias清理失败返回错误；Close不删已替换inode，未修改全局umask。最终connection双平台各3次race4.657/4.345秒；原crashguard真实E2E各3次45.789/47.934秒（后续临时unlink报错小修由完整合并覆盖）。SIGKILL窗口可能残留私有随机名，不声称任意时点零残留；不支持socket hardlink的文件系统明确失败。见[相关验收摘要](#evidence-uds-publication-validation)。

最新真实systemd活动升级完整187.62秒、poweroff退出0，容器已移除；agent升级/回滚18.61/18.18秒、daemon升级/回滚12.92/13.11秒，同一pane1600跨13边界。包含清理失效latch与最终RPC错误修复，不含随后UDS原子发布。证据/tmp/shiki-systemd-u1qv9m13/evidence.json，详见SYSTEMD/UPGRADE报告。

独立八类全架构复核未发现新增已确证生产偏差；修准03及清理报告的活动时间持久化说明：五秒是健康维护循环间隔，观察/写盘失败可能延长，崩溃可能丢失自最后成功持久化以来的活动，不能承诺最大五秒丢失。自动清理独立资格计时不依赖该近似值。目录/依赖、RPC与权限、持久/运行状态、生命周期、文件/终端/TCP、CLI/TUI与安装、能力声明和证据范围均复核；外部/平台/发布门槛保持。

最终382份Go/proto/module冻结清单/tmp/shiki-roadmap-final-source-20261028.json，实际Linux只读快照全部逐份匹配，记录/tmp/shiki-roadmap-final-linux-snapshot-20261028.json。build/lint、go mod verify和diff检查通过。全套日志/tmp/shiki-roadmap-final-{macos,linux}-20261028.json；macOS出现端口unknown-owner测试PORT_SCAN_FAILED和节点移除peer后连接仍成功，正在调查；不得记录该轮整体通过。端口错误是保守拒绝而不是已证实越权，初始日志不足以确定退出PID为唯一原因。

本轮macOS完整回归已退出1：40个测试包通过（含E2E209.403秒），port/server归属与internal/node移除peer两个包失败，不能记全套通过。port错误为保守拒绝，真实归属夹具改为从真实SystemScanner结果筛选自身稳定监听端点，再执行真实OS身份复核；unknown仍严格要求PermissionDenied。新增真实退出子进程的过期PID快照，严格要求FailedPrecondition且TCP不发生拨号。生产授权不变，整包race各10次macOS30.501秒/Linux22.252秒通过；原现场底层OS错误未记录，不断言其唯一原因。文件ownership_test.go hash cdc2840ce2073a8c7ef32dcaf36bb135f8f8625e3a789407960f5b91bf2fc34e，晚于382清单，按追加测试证据记录。node移除peer失败仍待调查，Linux整套尾部仍运行。

Linux本轮382源码全套race含E2E已退出0、42包通过；它不覆盖随后端口测试修正。macOS真实节点失败确认额外生产缺口：控制面peer移除后engine peer图先更新，旧userspace数据路径可能仍完成握手；adapter未绑定已解析peer并在握手后核验，不能把这种连接视为已授权。正在增加拨号前后精确peer身份/有效性检查和过时连接关闭，保留原typed userspace-only调用，不退回系统网络。08的Phase0四形态/准入复合项暂撤勾选，当前27/42、15项待完成；文档能力一致性也待整批收敛恢复，不用Linux本轮通过掩盖macOS暴露的缺口。

后续peer准入修复将Node.Dial解析的PublicKey绑定到typed userspace adapter，拨号前后核验engine精确单IP路由、非self、同key且未过期，成功但过时的连接立即关闭。独立只读审查确认无系统fallback；前后观测不是对未来任意peer变化的原子lease。补充DNS/hostname/IPv4/IPv6真实入口时发现测试控制面的旧注册身份产生同名歧义，保留生产InvalidArgument拒绝，仅给现peer通过真实control发布唯一名称；修正后单轮mac专项17.922秒通过。

383份源码再次全套复验仍未收敛：该冻结node测试尚含重复名称fixture，macOS在该处失败；另CLIThreeProcessTLSLifecycle在remote disable sync返回REMOTE_RPC_FAILED（39.04秒），正在调查，不能将整套记成功。独立复核另发现既有Node.Dial Status错误吞掉父context取消/截止分类，按14补修中。日志/tmp/shiki-roadmap-converged-{macos,linux}-20261028.json，383清单及实际Linux逐份匹配记录/tmp/shiki-roadmap-converged-{source,linux-snapshot}-20261028.json。当前27/42保持，待真实收敛后再恢复勾选。

<a id="evidence-node-admission-validation"></a>

最终节点准入专项收敛：internal/node完整包race各3次macOS71.763秒/Linux arm64普通用户61.781秒，退出0且无skip。包含真实peer移除、唯一DNS/hostname/IPv4/IPv6、四种RPC、旧连接回收和真实LocalAPI取消/截止；后握手检查overlay14分支先红0.628秒、Status ctx两分支先红0.727秒。生产仍仅typed userspace；精确peer/key观测不构成未来变化的原子lease。08恢复Phase0传输准入项，当前28/42、14项待完成，能力一致性等待本批remote插件停用失败调查。报告[相关验收摘要](#evidence-node-admission-validation)；这些专项不把原失败全套追认为成功。

383快照全套已结束：macOS40包通过，node DNS fixture与core E2E失败，E2E包213.636秒退出1；Linux41包通过，只有相同DNS fixture失败，E2E214.839秒通过，脚本退出1。最终node专项已修正fixture与追加ctx分支并双平台通过，旧全套依旧记失败。

core E2E调查发现独立可证明的共享连接缺陷：resolveLocked对已有pool的verifyRemote遇调用者取消/截止仍invalidate/failRemote，会取消无关流并进入冷却。sync停止可触发这个路径，但原失败没有内部细因，不把该机制宣称为原事件唯一根因。正在补受控TLS探测先红与共享流保活回归，再修调用者取消隔离；不得重试不确定mutation。08暂撤Phase1 TLS取消/重连复合项，当前27/42、15项待完成。


## 本批最终收尾（2026-10-28）

最终路线图29/42、13项待完成，原42条要求逐字保留；Phase0/1/2/3/4/5分别8/11、7/7、5/6、6/6、3/6、0/6。部分完成写在原条目下，未新增较小的交付范围，原生/外部/正式发布门槛未勾选。

<a id="evidence-shared-connection-validation"></a>

共享连接取消缺陷已修：调用者取消/父截止只结束当前探测，不invalidate既有健康pool或改变其预算；下一独立请求仍重新认证。真实TLS+活动TerminalStream两分支先红0.875秒，修后race3次2.488秒；macOS完整agent7.959秒、agent集成8.542秒、原core E2E38.914秒通过，Linux取消/重连预算/认证专项1.924秒和原core E2E43.326秒通过，命令均退出0。Linux过滤后的integration/agent未匹配测试，不计通过；原39.04秒REMOTE_RPC_FAILED唯一原因没有被证明，不以重跑替代原失败。见[相关验收摘要](#evidence-shared-connection-validation)。

最终源码清单/tmp/shiki-roadmap-delivery-source-20261028.json共384文件；相对383全套快照仅internal/agent/connections.go、connection_retry_test.go、internal/node/transport.go、dial_context_test.go、node_test.go变化。对应最终node完整包双平台race各3次71.763/61.781秒无skip，以及上述agent/真实core E2E补验覆盖这些变更。最终架构race1.356秒、make build lint、go mod verify及git diff --check通过；协议源未在该收尾变化，先前8份固定生成文件逐字节一致证据保持。

证据边界：382快照Linux42包含E2E183.474秒通过，macOS40包通过但两包失败；383快照macOS40包通过但node DNS fixture及core E2E失败（E2E包213.636秒），Linux41包通过但相同DNS fixture失败（E2E214.839秒自身通过）。两轮失败均保留，最终以明确范围补验收敛，未称384源码重新整套全绿。普通全套的root权限、长资源循环、原生LaunchAgent、真实systemd活动升级和注入故障等opt-in跳过不算通过，分别参考其独立专项且遵守快照范围。

最终独立全架构检查覆盖目录/SDK/宿主、RPC与准入授权、持久/运行状态及幂等、生命周期与取消、文件/终端/TCP语义、CLI/TUI/配置、安装产物、能力声明和证据，未发现新增已确证偏差。已修准五秒活动合并的持久化范围，不承诺异常写盘下最大五秒丢失。UDS任意SIGKILL可能残留私有临时名、peer前后检查不等于后续变化原子lease、真实tmux未知退出暂停均明确保留。

下一批继续本地/Linux容器完整凭据轮换、混合负载与性能/资源验收；历史发行组合、物理多设备、原生VPS/重启登录、外部账号、原生平台和签名分发仍独立待验。完整Phase0–5目标保持进行中，不将本批收尾视为成品交付。


## 真实宿主轮换与混合负载批次（2026-10-28，已收尾；以下保留过程记录）

上一批384源码修复/补验和29/42同步属于已完成工作，本轮继续完整Phase0–5，不缩减交付范围。批次开始时既有rotation仅Supervisor+fixture期望/Ready；本轮使用生产agent运行入口/Resources、daemon Runtime、Core/Ready、持久映射与forward及真实TLS，客户端/服务端分别自然到期。采用内部构造的显式短TTL依赖，现有命令仍零值12小时，不新增配置或环境暗门；宿主在测试进程、四worker独立子进程的范围将如实记录，不能当作默认12小时独立宿主binary长稳。

混合负载扩展现有opt-in真实命令测试，默认原soak不变；增加并发TCP与终端/64KiB/1MiB双向文件同时运行、半关闭与无重放、固定进程/FD/goroutine观测和子进程race报告。先双平台一分钟自检，再冻结源码做受控持续运行，结果必须等实际结束。

初审时发现确定状态窗口：CoreAPI grant到期已经取消回调/拒绝Call，但Instance.Stop等待期间Supervisor.List仍可能返回ready和Protocol，daemon依据它显示可用。当时据此补确定性到期/停止未完成回归并派生实时可用状态，保留持久enabled/revision，不能仅用等待隐藏窗口。默认12小时超过稳定预算重置窗口；短TTL<一分钟不能用于推导长期重启预算，测试不能修改或绕过既有预算。

<a id="evidence-plugin-readiness-validation"></a>

readiness修复已冻结并完成专项：List/current统一核验grant有效性、实际deadline、未取消lifetime、未关闭Done及实例ready；退出窗口派生failed/PLUGIN_INSTANCE_ENDED且清Protocol，不改变enabled/revision。原实现对deadline期间真实Stop受阻及Done两分支先红1.580秒；最终internal/plugin完整包race各3次macOS11.732秒/Linux11.013秒，无skip。报告[相关验收摘要](#evidence-plugin-readiness-validation)，vet/diff通过。

开发期间真实hostrotation首轮完成双端场景（mac开发包71.412秒），但后续加强stats持续消费与恢复后新帧断言，当时等待最终双平台复验，旧缓冲帧不作为存活证明。首验测试错误要求manual stop后仍有spec，已按13修成spec删除+成功持久request receipt，原失败保留。混合模式首版两平台一分钟各25轮/100次1MiB TCP自检通过，后续增加同宿主空载终端测前/后基线并改精细观测，须以新快照复验后再跑持续负载，不能混记。

<a id="evidence-host-rotation-validation"></a>

最终新增宿主矩阵：macOS client36.13/server35.98秒（包74.820秒）、Linux client34.36/server34.26秒（包71.325秒）通过，无skip/child race报告；强化非nil配置负TTL、精确错误及无目录/无socket副作用测试单独mac1.622/Linux1.023秒通过。默认宿主回归mac agent8.048/server12.636/architecture1.352秒通过，Linuxarchitecture1.155秒。报告[相关验收摘要](#evidence-host-rotation-validation)。两宿主在测试进程，四worker独立；生产命令无TTL入口，零值12h不变。

本轮冻结390份Go/proto/module清单/tmp/shiki-host-mixed-final-source-20261028.json，与Linux实际只读快照逐份一致（/tmp/shiki-host-mixed-final-linux-snapshot-20261028.json）。完整macOS43包通过、E2E210.593秒；Linux42包通过、E2E186.391秒自身通过，但旧Supervisor rotation/sync在15.36秒返回old callback not canceled: started=1 active=0 canceled=0，整套退出1。该测试只在handler返回的defer即时采样ctx.Err，当时据此补真实Context.Done与handler终态时间观测，不能把返回后请求本身自动cancel冒称到期取消；到期前结束必须仍判失败。原失败保留，未称Linux全套通过。build/lint/go mod verify/diff通过。

混合负载最终一分钟自检双平台均通过，测前/测后各20次空载终端与混合期使用相同1ms观测：mac163秒量级、Linux173秒量级，25轮/100条1MiB TCP、各方向104857600字节、EOF尾2900字节、文件8519680字节；准确时长162.25/172.59秒。这两次运行仅算自检；随后各10分钟受控实验按mac→Linux串行执行，并保留真实snapshot、process/container及清理证据，结果见下文。

旧轮换取消观察器已通过独立只读审查：真实Context.Done观察与handler返回分别记录，拒绝到期前返回、普通成功返回后请求cleanup等假证据。AfterFunc记录的是取消的观察时间，不是精确取消瞬间或原失败的唯一因果证明。修正后旧rotation整包race各3次macOS102.311秒/Linux99.378秒通过，无skip/child race；最终又补等待handler退出与观察完成共同成立的join，保持到期断言不变，最终join源码整包补验macOS35.033秒/Linux34.154秒均退出0、无skip/child race，四份测试hash再次核验一致，vet/diff通过。原390源码Linux全套失败不改写为成功。

最终差异的独立八类架构复核通过，未发现新增已确证偏差：目录/依赖、SDK与宿主归属；Connect/插件gRPC与授权准入；持久期望/修订/请求记录；进程退役/取消/恢复；文件冲突/暂停与TCP半关闭/无重放；CLI/TUI/默认配置；安装与产物边界；能力声明与验收证据。最终join源码清单为/tmp/shiki-host-mixed-delivery-final-source-20261028.json（391文件），相对390全套只改变旧rotation的harness并新增observation测试；生产及混合负载源码未变，受影响包已双平台补验。

默认12小时门槛保持未完成：后续须独立执行实际agent/daemon二进制，所有构造参数零值，在每平台真实运行至少12小时并核验到期后恢复；建议12小时30分负载、13小时外层截止，串行平台观测时间单列。不得把当前30秒测试、最长8小时固定PID的旧soak或未开始的计划记成通过；只有实际观察到两侧退役重叠才能声称同时退役。

macOS十分钟混合实验已完整通过：实测10分0.131秒、247轮、988条TCP，每方向1,035,993,088字节及28,652字节EOF尾，文件77,135,872字节；完整core703.29秒、runner退出0。终端混合min/mean/max1/2/6毫秒，TCP386/414/444毫秒，文件131/298/439毫秒；所有固定资源检查通过，不能推导完全无FD增长或不存在任何泄漏。8个记录PID全部消失，实际及外层快照删除，证据/tmp/shiki-mixed-10m-macos-mxjuj2uh/evidence.json。该快照与最终391源码仅旧rotation两份测试不同，生产和mixed一致；Linux随后串行启动，仍待真实完成。

## 本批最终收尾与证据边界（2026-10-28）

08/15/16已同步：原42条要求逐字保留，29项勾选、13项待完成，Phase0–5依次为8/11、7/7、5/6、6/6、3/6、0/6。新增证据补进原条目下，不将组合要求的部分通过算成全部完成。此前过程中的“正在”描述属于当时记录，以本节及页首为准。

双平台混合实验按macOS→Linux串行完成。macOS实测10分0.131秒、247轮/988条TCP，每方向1,035,993,088字节，EOF尾28,652字节，文件77,135,872字节；Linux实测10分0.154秒、249轮/996条TCP，每方向1,044,381,696字节，EOF尾28,884字节，文件78,249,984字节。完整core场景703.29/709.77秒，两侧应用runner退出0，无选中skip或子进程race报告。混合终端min/mean/max分别1/2/6与1/2/5毫秒，TCP386/414/444与362/393/432毫秒；同一宿主前后各20次空载样本及固定8进程、FD+4、测试goroutine+6边界通过。这是有节奏的race负载样本，不代表饱和容量、SLA或全部内存泄漏验收；RSS增长如实记录，完整资源/性能门槛不勾选。

<a id="evidence-mixed-load-validation"></a>

macOS8个跟踪PID全部消失，实际及外层快照删除。Linux应用runner退出0，但外部取证driver因大小写敏感匹配“No such object”未接受实际“error: no such object”，自身退出1并留下container_absent=false；保留原evidence.json。独立精确CID inspect退出1且明确no such object，docker ps -a按相同CID过滤为空，确认容器确已移除；另存cleanup-recheck.json，root也独立重复核验。Linux实际/外层快照均删除，不以改写原失败或重跑十分钟代替复查。证据目录/tmp/shiki-mixed-10m-macos-mxjuj2uh与/tmp/shiki-mixed-10m-linux-bmpvopwv，完整数据/复现与限制见混合负载报告。

最终391源码清单/tmp/shiki-host-mixed-delivery-final-source-20261028.json已核验未变。Linux混合快照410份源码/工具hash与这391份逐一匹配；macOS409份快照仅旧rotation的harness/observation两份测试不同，生产及mixed源码一致。390源码完整macOS43包通过、E2E210.593秒；Linux42包通过、E2E186.391秒自身通过，旧rotation观察失败使整套退出1，仍记失败。最终rotation补验为双平台各3轮102.311/99.378秒，最后join细化后再整包35.033/34.154秒通过；不声称391源码重新整套全绿。构建/lint/模块校验已通过，后续仅两份测试变化并获对应补验。

最终全架构复核覆盖目录/依赖、传输与准入、持久/运行状态、生命周期/取消、文件/终端/TCP、CLI/TUI/配置、安装/产物及能力/证据，未发现新增已确证文档偏差。插件退役状态修复不改变持久期望与重启预算，内部短TTL无用户入口，默认仍12小时；取消观察器不把提前结束或普通请求cleanup当自然到期。后续继续本地/Linux容器，默认时长实际轮换和完整资源/性能另行验收；外部账号、历史发行/物理多设备、原生平台及签名分发保持待验，完整Phase0–5目标仍进行中。


## 默认时长轮换与内存诊断批次（2026-10-28，已收尾；以下保留过程记录）

新批次进行中：实现默认12小时凭据自然轮换的独立宿主验收工具，并诊断混合负载RSS增长。短预检只证明工具与业务/清理流程；实际12小时到期、完整生产内存/容量验收未通过。长跑保持生产默认TTL，无CLI/YAML/环境覆盖；诊断仅测试夹具私有通道，不新增产品pprof能力。上一批已完成证据和29/42计数保持。

<a id="evidence-sync-skip-log-validation"></a>

本批新增确定偏差并已修复：sync client/server的OnSkipped曾附带mapping_id/project_id字段，但生产RecordLog明确拒绝非空任意Fields，调用错误被忽略，导致符号链接/特殊文件跳过提示未进入真实host日志。仅删除两个worker的Fields，保留reason code和实例归属，不放宽宿主安全白名单。真实worker→CoreAPI→RecordLog→WatchLogs的双端×symlink/FIFO四场景旧实现全部红6.451秒，修后绿7.973秒；macOS scoped race integration9.375/client5.231/filetree4.572/reconcile1.371/plugin5.226/architecture1.350秒，Linux普通用户只读快照integration12.373/client2.553/filetree2.276/reconcile1.049/plugin4.494/architecture1.181秒，无选中skip，vet通过。见[相关验收摘要](#evidence-sync-skip-log-validation)；测试也验证正常文件仍传输/列出、跳过项保留源端且未传输。

默认时长工具已实现并通过独立架构复核：实际普通构建CLI/agent/daemon与四插件，24小时证书/Token、12小时默认凭据不变；短预检macOS76.29秒/Linux82.09秒，各15轮，应用/collector退出0，清理及源码无变化检查通过。预检明确natural_rotation_passed=false，未执行12小时到期。最终退役观察采用窗口内5毫秒采样、保守LastPresent跨最早TTL边界，否则证据不足即失败，并记录最大采样间隙；不声称精确同时退役。到期窗口只持续原终端和held TCP，文件短周期暂停，不能作为在途文件提交轮换证据。

普通/race短诊断表明只读RPC阶段持久状态数量不变，GC后活堆小幅变化；文件/TCP进一步定位port tunnel每次Read分配256KiB的热点，当前正在验证每流读缓冲+每帧独立payload的优化。此为分配压力改进，不把RSS增长直接判为泄漏或声称根因已穷尽。默认预检快照早于该后续优化，补验范围须另记。


## 默认时长工具与分配优化最终收尾（2026-10-28）

本批已收尾：修复双端sync跳过告警，完成默认12小时独立宿主验收工具及双平台短预检、普通/race内存诊断、端口分配优化和双平台混合补验。原42条要求不变，当前29/42、13项待完成。默认12小时实际到期尚未执行，完整生产内存/容量、外部账号、原生平台与正式发布仍待验收；不把预检或局部分配下降当作整个Phase5完成。

生产改动仅本批的三处：sync client/server去除被host拒绝的任意日志Fields；port tunnel将逐Read的256KiB分配改为每流私有读缓冲加每帧实际n字节独立payload。没有增加生产CLI/YAML/env/协议/pprof入口，默认12小时与重启预算不变。既有DATA上限、发送后所有权、半关闭与取消语义保留；不能直接把scratch交给Send后复用，因为运输层trace/stats可能在返回后仍读取消息。真实保留帧测试在危险复用overlay上红、正确实现绿。

默认工具最终短预检macOS76.29秒/Linux82.09秒，各15轮，应用和collector均退出0，natural_rotation_passed=false；两平台实际493文件快照记录、应用输入未变，所有记录native PID与Linux精确CID/快照清理通过。此快照早于随后tunnel优化，不能追认它覆盖优化；最终优化由下述受影响包/真实混合补验覆盖。额外checkpoint后主动TERM两平台应用143、collector1，原始中断保留，全部进程/容器/快照清理且无需强制回收；证据/tmp/shiki-collector-cancel-validation-wthrt10y。

<a id="evidence-memory-profile-diagnostic"></a>

v4普通/race分阶段profile均完整通过31.73/40.40秒：16次256KiB双向文件与32次1MiB有节奏TCP、等时idle、双GC采样，所有SHA256/半关闭/精确target接受数及六子进程退出通过。诊断是fixture入口，不冒充shipped命令长跑。32KiB分块基准每1MiB的8.71–8.76MB/op降为1.319MB/op；同诊断port-client累计分配普通381,427,504→120,065,456字节、race602,955,840→329,874,240字节。RSS并未同比降低，server race分配还存在变动；因此只认定此工作负载的客户端分配压力改善，不宣称生产无泄漏、整体容量提升或原十分钟RSS原因已全部确定。详细前后快照/profile/失败记录见[相关验收摘要](#evidence-memory-profile-diagnostic)。

最终双平台plugins/port/...、真实integration/port及architecture race通过（integration5.154/4.217秒，tunnel1.442/1.092秒）。优化后一分混合实测1m0.149/1m0.148秒，各25轮/100条TCP，完整core167.10/173.64秒、命令退出0，无选中skip或child race；两平台混合terminal均min/mean/max1/2/3ms，TCP379/401/441与363/391/417ms。完整字节、半关闭、固定FD/goroutine和尾部恢复通过。native8PID全消失，Linux精确CID已移除；实际Linux快照在正常清理后已不存在，未取得逐文件后验hash对照，不宣称该证据存在。工作树411份Go/proto/module清单/tmp/shiki-default-memory-final-source-20261028.json全程不变。make build lint、go mod verify及diff检查通过，旧失败全套不改写为成功，未称最终411源码重新全套全绿。

最终按八类边界复核：目录/依赖和SDK/宿主/插件分工由架构测试核验，新增cross-process/diagnostics仅test；Connect与原生plugin gRPC路由/授权无变；日志只传reason且保持安全拒绝；持久期望/修订/去重无改；私有读缓冲和独立帧保障跨发送生命周期，worker取消/停止仍复用原控制；文件跳过提示与TCP语义有真实行为证据；CLI/TUI/安装产物/默认配置没有新增未验证能力；报告区分普通/race、fixture/shipped、preflight/真实12h和历史快照。早前独立审查发现的工具source新增文件漏检、异常退出丢失、精准清理、agent登记窗口、退役模糊区间均已修复并验证。最后tunnel生产差异由root补复核（独立审查代理限流退出），未发现新增已确证文档偏差。

08/15/16同步，42原条目逐字保留，仍29/42。下一步在冻结源码上实际执行默认12小时到期及恢复窗口，期间以活跃进程/句柄为准继续观测；外部、原生平台和正式签名分发门槛独立保留。


## 默认12小时实测（2026-10-28，运行中）

默认时长实测已于2026-10-28 14:06:52（Asia/Shanghai）启动，macOS原生普通构建，当前仍在运行、未验收通过。已核验真实独立宿主/四worker/终端进程存活，冻结495文件中的411份Go/proto/module与最终清单逐份匹配。首个检查点已通过；预计凭据最早于10月29日02:07:04到期，恢复后至少再运行30分钟，最早约02:37结束，13小时外层期限。测试句柄11096、runner PID82454；证据目录`/private/var/folders/1n/q373cln10fqb0gl9z1ds5d8m0000gn/T/shiki-default-rotation-12h-macos-8t9oxxnp`。仅为该runner使用临时caffeinate防空闲睡眠（句柄91172），随runner退出结束，不修改系统电源配置。Linux实际长跑尚未开始，待本轮完整退出及清理后串行执行。


## 长跑期间文档复核（2026-10-28）

本批仅调整当前证据表述，不修改生产、测试或运行快照。17原型复盘将登录/终端/熔断问题、合成版本矩阵、持久化故障、容器活动升级、双平台一小时/十分钟实验以及默认轮换工具按已完成范围重新对照；原10–19工作日保留为当时历史估算，不再误作当前剩余量。18平台表区分历史全套与最新受影响包补验，明确390快照Linux失败及后续观察器修正，不把411源码标为重新全套全绿。

08/15/16计数保持29/42。17/18新增的当前结论逐一对照各专项报告；设计00–14、原42条要求、生产源码与接口/capability无变。沿用上一批已完成的八类架构复核，当前411份源码与实际长跑快照仍一致；文档链接、原条目对照及diff检查完成。正在运行的默认12小时实验只按真实检查点记录，仍未验收。未重启长跑、未将耗时缩短为预检，也未改写任何历史失败。


## 路线图进度同步复核（2026-10-28 14:14，Asia/Shanghai）

本次发现08的Phase 5部分进度仍写“真实12小时待运行”，与实际已启动及三份文档顶部记录矛盾；已改为macOS运行中、Linux尚未开始。路线图新增最近进展表，明确插件目录修正、sync告警、轮换工具、分配优化及安装升级各自已完成范围与剩余条件。此前历史批次中的“进行中/下一步”保留其当时语境，当前状态以文首和最新记录为准。

实测原句柄11096仍在运行；14:14:14检查点为7分5.630秒、48轮，recovered=false，跟踪的宿主、四worker及终端进程仍存活。这只证明当时持续运行，不能作为12小时自然到期或最终通过证据。新LaunchAgent活动升级批次仅实现/审查中，未借用既有生命周期证据宣称完成。

本次仅同步文档；沿用上一收尾批次的八类架构复核及各专项证据，不宣称重新执行全套测试。核对原42条要求、勾选计数、阶段统计、三份文档状态及本地链接；29项已勾选、13项待完成，Phase 0–5依次为8/11、7/7、5/6、6/6、3/6、0/6。未降低验收条件或改写历史失败。活动升级实现与完整审查另行收尾，完成前不得增加相应验收声明。


## 原生LaunchAgent活动升级批次收尾（2026-10-28）

真实当前GUI登录域、UID501和HOME下，生产安装器A→B→A通过87.08秒（升级17.76秒、回滚18.73秒）。每边界均先证实8MiB自动上传已有256KiB匹配临时文件、独立1MiB daemon上传已有256KiB部分内容，原TCP目标仍开放且请求计数1，终端confirmed输入1/uncertain0。替换后旧frontend/终端/TCP退出，新输入与新TCP正确、原pane完整身份及daemon/server worker不变；精确文件hash、冲突显式解决、额外未解决冲突ID/generation、暂停mapping与StopForward持久回执保留。正常卸载保留配置/状态/Token/项目/备份，原Go test与wrapper均退出0，fallback_actions与errors均空。

主跑证据`/tmp/shiki-launchd-activity-main2-rpedflxx`，root核验14个唯一PID不存在、内层root/源码快照已删除，实际清单中415份Go/proto/module与工作树逐份一致。随后相同507文件清单的取消验收在真实ready后向自己的runner发送TERM：test1（46.48秒）、runner143/signals=[15]，driver0仅代表取消断言通过；清理仅owned bootout/unlink-plist、无进程信号或tmux兜底。root独立核验8个PID、专属session $928及标准job/plist不存在，保留的507份快照文件逐份hash匹配，证据`/tmp/shiki-launchd-activity-cancel-_3ag98e3/root-independent-verification.json`。首次33.11秒因夹具遗漏双端sync enable而失败，未进入升级边界；修正真实CLI启用和ready检查后通过，原失败保留，不记为生产缺陷或成功。

架构复核覆盖全部八类边界：生产目录/SDK/宿主/plugin依赖、RPC与授权、持久/运行态和revision/去重、进程归属与取消、文件/终端/TCP语义、CLI/TUI配置、安装产物、能力与验收证据。生产代码、依赖、协议及默认配置无变化；新跨进程夹具位于test/e2e，Linux systemd入口逐字迁到linux限定文件，共享逻辑只增加可选边界回调。独立审查并修正父testing.T错误归属、session登记前失败窗口、job缺失误判、Bash回收后PID复用、单项清理失败阻断其他资源、兜底回收掩盖成功等夹具问题。清理用birth/executable/UID核验及精确session身份，不杀共享tmux server；未知状态记录失败。架构race检查通过（缓存），native编译/清理行为race及shell/Python检查通过；Linux arm64仅交叉编译，不声称重新运行Linux活动升级或完整全套。

<a id="evidence-launchd-activity-validation"></a>

验收限于原生macOS agent升级/回滚及上传方向；daemon是普通TLS子进程，Claude是shell fixture，A/B是当前源码的两个测试版本。race构建不代表已捕获所有worker race报告。完整报告见LAUNCHD-ACTIVITY-VALIDATION。08/15/16同步29/42、13项待完成，原42条要求保持不变；外部登录、历史发行迁移、重启/登录持久化、其他平台和正式发布门槛保留。

默认12小时实验未重启，原句柄11096于14:44仍存活，14:44:39检查点37分30.572秒/219轮、recovered=false。其495文件/411份源码快照保持原样；本批新增测试不改生产输入。实际到期与恢复尾部尚未发生，Linux实际12小时仍待本轮完整退出后串行执行。


## 原生下载方向与共享验收语义收尾（2026-10-28）

原生agent下载中的A→B→A通过87.29秒（17.79/18.54秒），同一最终夹具的上传回归通过85.03秒（17.83/16.08秒）。共享exercise使用具名direction/replacedHost两个独立参数，方向只决定源/公开目标/临时文件位置，被替换宿主只决定独立直连daemon上传是否取消。Linux原四个组合保持agent+upload两次、daemon+download两次及所有原断言；原生download+agent真实运行证明两个轴没有混用。新增原子性观察：真正部分temp/staging出现并暂停传输后，公开目标SHA仍等于改源前完整内容；恢复仍验证两端最终8MiB SHA、冲突显式解决及所有残留临时文件回收。

两正常run的test/wrapper均退出0，fallback_actions/errors为空。root分别核验14个唯一PID已退出、成功root已删除、最终job/plist不存在；下载实际415份Go/proto/module与当前源码一致，两run的507文件清单逐字相同。root还核验两次边界的旧公开目标SHA链：首边界对应初始化内容，回滚边界对应上一边界完整新payload。下载未确认输入次数[1,0]，上传[0,1]；confirmed/new各1，旧TCP请求各1，符合未确认输入可到达一次、禁止重放。samepane、daemon/server worker身份、持久冲突ID/generation、暂停mapping及停止回执保留。

证据：`/tmp/shiki-launchd-download-yo6cl_oq`与`/tmp/shiki-launchd-upload-regression-gja3jybo`，各有root-independent-verification.json；共同source清单SHA256为`ff64afaefa6685c17085332b99e1f2108faf2e2f811a5f5334bbe300767f7d51`。本批没有再次运行取消，先前runner143/test1结果仍限定于当时上传夹具。Linux仅交叉编译，不将其历史活动验收追认为本次新增断言已运行。race构建仍不声称完整捕获所有worker race日志。

已完成全架构基线与本批差异复核：生产代码/依赖/协议/SDK与host/plugin边界不变，持久化/授权/生命周期/文件终端TCP/CLI配置/安装产物/能力声明保持设计；新增行为仅test/e2e和测试脚本。独立审查逐行确认两轴语义、Linux四组合、部分数据与公开目标原子性、新内容及清理断言；native编译及cleanup guard race、Linux arm64交叉编译和shell语法检查通过。完整报告与源码/日志hash见原生活动升级。08/15/16同步，原42要求不变、29项勾选/13项待完成。接下来当前冻结源码完整双平台race回归另行取证，不能将历史通过与专项拼接成整套通过。


## 历史完整回归与轮换流失败取证（2026-10-28，后续结果见顶部）

502文件/415份Go/proto/module冻结源码`/tmp/shiki-full-race-20261028-5h0eo7fo`以count=1、race、e2e tags运行全部包。macOS43包通过、hostrotation包失败，runner238.212秒/退出1，E2E224.579秒自身通过；Linux arm64普通用户容器44包通过，runner278.313秒/退出0，E2E199.408秒通过。15个包无测试；macOS6个、Linux9个场景skip不计对应验收完成。精确skip/失败/包时长和日志SHA见该目录summary.json。

实际Linux容器CID `c35ad017aa5d3ec137a870363490402b17e3e9dea749020e415d39d00f053910`运行时只读挂载已inspect核验，实际502文件逐份与共同冻结源码匹配；完成后结构化容器清单确认精确CID不存在、实际挂载快照已删除。共同冻结源码执行前后502文件完全一致。保留全部原日志，不将后续补验改写该次macOS失败。

失败发生于真实client插件自然到期：旧PID/socket已回收，新文件/TCP/冲突/暂停/停止回执断言已成功，但要求恢复后新鲜Stats帧时无关流已返回Canceled。server场景通过。现仅增强hostrotation失败观测（父/流context状态、时间与服务器快照），不忽略Canceled或重新打开流；独立client三次race109.866秒通过仍不足以解释整套失败。正在隔离源码副本追踪共享pool首个invalidate及probe/parent期限；内部探测超时、远端取消与本地网络错误尚为候选，未认定根因或声明修复。

相关Phase 2生命周期验收暂恢复待验，当前28/42、14项待完成；原42要求不变。本批尚未收尾，生产代码没有因猜测修改，实际12小时原冻结实验继续运行，不与本轮诊断共用进程或状态。

<a id="evidence-signed-alpha-install-20261029"></a>

## 真实签名alpha的本机与容器安装验收（2026-10-29）

对象为手动签名运行40877214329的92cafaf源码候选，版本v0.0.1-alpha.1；先用独立仓库公钥验签，仅从认证私有副本解包。A为v0.0.0-local.20261029.5开发归档，B为该签名候选；没有历史正式发行升级对象。产品二进制未重建，新增的可选验收输入只编译测试驱动。

| 环境 | 已实际通过 | 清理与范围 |
|---|---|---|
| macOS arm64 / 当前GUI LaunchAgent | 目标包自身helper的A→B→A生命周期69.72秒；插件、崩溃守卫/恢复、备份/配置/状态保留；另在私有新prefix/state中B新装、双插件、重复安装unchanged与卸载 | 标准job/plist前后均无用户已有服务；所属PID回收，配置/状态按契约保留。旧69.17秒结果复用A-helper，只保留较窄证据。 |
| Linux arm64 / systemd252.39 PID1 | 双角色13生命周期、4活动升级回滚边界、8MiB双向文件、同一pane PID287、终端/TCP无重放及身份/凭据/冲突/暂停/转发意图保留；另全新B的daemon/user-agent安装、四插件、会话、重复安装及卸载 | 无外网、私有容器/cgroup/PID；正常poweroff退出0，精确容器删除；原543冻结输入及认证payload后置hash不变。 |

原Linuxfresh夹具将合法重复安装的unchanged当ready而失败，产品无故障；只修正重复调用的明确期望后通过，原失败和正常清理证据保留。显式关闭tsnet、Claude为明确fixture，external_peer_acceptance保持pending；不证明真实账号、原生VPS、物理重启/登录、全平台或长期资源。原697长测及其它容器未改动。

本地证据：`/private/tmp/shiki-signed-install-hikyjcls/`，含`artifact-provenance.json`、`native-target-helper-summary.json`、`native-fresh-summary.json`、`linux-summary.json`、`linux-fresh-summary.json`及各组完整日志/关机记录。Mac B归档SHA256为`6d171a029d0f52d6e9100cc67f5487811817c2272db0afbdf258ad851b58676b`，Linux B为`d2a2d1819a0745899de20df336d921495d8330e29dc448fabcac170ce646f026`。新tag将重新构建，必须独立认证其实际资产，不能复用这两个归档hash。

<a id="evidence-tag-release-automation-20261029"></a>

## tag自动发布实现与本地验证（2026-10-29）

新release.yml由版本tag触发，复用同SHA CI，四目标无密钥构建，tag限定环境自动签名并验签，独立contents-write发布job验证22文件、来源和远端digest，从完整草稿公开。版本规则、不可变tag、幂等/冲突拒绝、私钥隔离及下载验签步骤见18。环境和规则配置已读回核验，旧手动工作流400508788已禁用且源码入口删除；运行证据没有删除。

全部工具测试38项通过（10.354秒）；发布17项行为及actionlint通过，补充metadata上限后发布器10项通过。实际签名shell使用四份有效微型归档、临时测试密钥和真实Go verifier，产出22文件并由发布器再次验签；归档与签名篡改均拒绝，临时私钥/持钥目录已清理。证据位于`/var/folders/1n/q373cln10fqb0gl9z1ds5d8m0000gn/T/shiki-tag-release-smoke-j4qo91bb/summary.json`；环境/规则前后快照位于`/tmp/shiki-tag-release-automation-20261029/`。这些本地检查不代替首次真实tag和公开资产验证。


<a id="evidence-public-alpha-release-20261029"></a>
## 首个全自动公开alpha与下载验收（2026-10-29）

[v0.0.1-alpha.1](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-alpha.1)首次公开时间为2026-10-29 15:13:47 UTC（北京时间23:13:47）。受保护annotated tag指向`f0b5f83dce599c8e0d715bda5e7806a0ef6aaa72`；[Release 40895007350](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40895007350)全部13个job成功，包含来源校验、Ubuntu/macOS完整CI、四目标CI打包、四目标正式构建、签名和发布。GitHub记录为`draft=false`、`prerelease=true`，固定版本未作为稳定latest发布。tag签名环境无人工审批，旧手动工作流已替换，当前活动入口只有CI与Release。

实际下载22公开资产至`dist/releases/v0.0.1-alpha.1/`，合计377,701,496字节。可信仓库编译的verifier使用预先固定公钥，四目标Ed25519签名、精确资产集合、各归档14成员及全部payload hash/checksums pin、签名回执、PROVENANCE固定tag/source/run、远端每资产大小/SHA-256全部通过。PROVENANCE仅为补充工作流记录，不单独宣称已签名的源码证明。公开Release和Issues页面通过无登录curl的TLS校验并返回HTTP200。

24个二进制均静态核对`v0.0.1-alpha.1`、上述完整commit、Go1.26.10和目标OS/arch。4个CLI保留的BuiltAt等于该运行创建时间`2026-10-29T14:47:30Z`；另外20个程序未使用该变量，被链接器消除，记录为optimized-out，仅保留工作流注入证据，不声称逐二进制验证了该时间。

本机macOS arm64与Linux arm64容器各执行六程序的version/help，共24条实际命令；原生client首次/重复stage-only两次，Linux client/daemon首次/重复共四次。Linux为UID1000、network none、cap-drop ALL、只读根目录且无systemd。每次核对14成员hash、0600服务review及显式路径；未创建current/config/state/plugins链接或激活服务，现有服务文件保持。源资产后置hash、临时解包目录清理通过。本轮不重复真实服务生命周期；此前92cafaf签名候选的LaunchAgent/systemd新装运行、开发版本升级回滚仍见[独立证据](#evidence-signed-alpha-install-20261029)，不合并成新tag归档的完整服务验收。

证据目录：`/tmp/shiki-tag-release-automation-20261029/`。其中`release-run-success.json`、`release-jobs-success.json`、`published-release.json`、`public-urls.json`保存运行与公开状态；`public-acceptance/summary.json`、`public-verification.json`、`binary-build-metadata.json`、两平台summary及`container.json`保存独立结果。临时证据不是永久存档；如不可取得，须重新核验对应结论。

独立七类架构复核未发现生产边界、RPC/授权、持久状态、生命周期、I/O或CLI/安装语义偏差；本批只执行已提交自动化并同步文档。42条原要求文字/顺序保持，Phase0工程/发行地址与Phase5发布材料/支持入口两项已满足，当前31/42、11项待验。Homebrew、密钥轮换/撤销、真实账号/网络、原生平台/重启登录、物理多设备、长期资源等门槛保留；原697长测未操作或重判。后续文档提交不改变发行tag、产物来源或此运行的成功结论。
