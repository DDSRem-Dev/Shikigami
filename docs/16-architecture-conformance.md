# 架构与文档一致性检查

**资源收敛、签名演练与alpha.2分发准备（2026-10-30）：** 已完成原697源全部740组/5920条资源样本及12进程代际复算，保留全部峰值/末半小时；697到公开alpha.1执行输入482相同/39修改/21新增，明确新Stop/probe/诊断等覆盖差异，未确认新生产保留根，不由RSS宣称无泄漏或追加盲目长测。临时测试密钥的真实verifier12步轮换/撤信正负演练和3项回归通过，私钥清理，真实发行身份未操作；旧钥显式传入仍可验签，在线撤销未提供。用户指定当前项目GPL-3.0-only及Homebrew首发v0.0.1-alpha.2；完整GNU正文已置LICENSE，README/构建说明同步，旧alpha.1冻结MIT保持。Homebrew从已认证四目标归档自动生成版本/URL/hash/签名/许可证，发布前Ruby校验，公开后master单文件CAS更新，同版幂等/冲突拒绝、旧版防降级与稳定渠道保护；仅保留CI/Release工作流。整套55工具测试、最终生成器7项定向复验及actionlint通过；Linux arm64普通用户Homebrew实际安装/test、包revision升级/test、重装/test及卸载7步全部退出0，三次test共6次helper暂存；Homebrew4/7暂存附加文件和LICENSE搬移两处适配已修正，完整认证包位于libexec/bundle，早期失败保留。最终native公式方法/重复暂存/签名篡改拒绝通过；实际环境细节见22，macOS27因Xcode/CLT不足被前置拒绝，不能记为brew全流程通过。alpha.2实际tag/公开产物及自动Formula提交仍待执行。七类复核无生产模块/RPC/状态/生命周期偏差；原42条文字/顺序及31/42保持，外部/平台/当前候选长期资源门槛保留。详见[资源审查](20-long-run-resource-review.md)、[密钥演练](21-release-key-rotation.md)与[Homebrew分发](22-homebrew-distribution.md)。

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

更新：2026-10-29。规范基准为文档 00–14；本记录只描述实现、发现的偏差和验证，不修改规范来迁就实现。每批任务结束后必须检查全部已实现边界，再开始下一批；该要求同时保存在根目录 AGENTS.md。

<!-- active-parallel-work:start -->
**当前工作与验收边界（2026-10-30）：**

| 工作 | 当前状态 | 后续条件 |
| --- | --- | --- |
| 原697源Linux默认12小时 | 终态通过；740组5920资源样本/12代际及到公开alpha源码差异已完成分析 | 生产heap/object归因、当前候选长期覆盖仍待，不从RSS推断无泄漏 |
| 发行密钥轮换/撤信 | 临时测试密钥12步演练及异常清理通过，真实签名身份保持 | 显式旧钥仍可认证旧钥签名；在线撤销未提供，生产身份操作需独立记录 |
| GPL与Homebrew alpha.2 | GPL-3.0-only正文与自动生成/验签/CAS更新实现完成；本地/容器分发验收见22 | 实际新tag完整流水线、公开alpha.2与自动Formula提交仍待执行；macOS工具门槛保留 |
| 完整交付门槛 | 当前31/42、11项待完成 | 外部账号/物理平台、长期资源与完整渠道平台验收依路线图推进 |

31/42是原始复合条目的验收数，不是代码完成率；历史批次中的旧计数保持当时范围。独立工作并行推进，不重复已通过检查。
<!-- active-parallel-work:end -->

**本地收尾与目录清理（2026-10-29）：** 当前候选541份Go/proto/全部module执行输入逐hash仍与`.5`一致；已知syncack整包及真实systemd组合遗漏已经补验，不重复运行。完成[当前候选验收矩阵与后续条件](./19-local-acceptance-closure.md)，明确原697源终态先核四角色轮换、30分钟尾部、资源和清理，再依据具体结果补当前候选证据；未发现新的确定资源保留根，不把短测或旧源通过当无泄漏。发布准备中旧容量与撤销Token待办已按后续结果更正。普通/内嵌构建共用严格元数据入口，显式`SHIKI_COMMIT`/`SHIKI_BUILT_AT`注入、缺省unknown；8项Python回归通过，涵盖两入口六程序参数一致、真实buildinfo探针链接/输出及非法输入在构建前拒绝，不宣称六产品重打包。独立架构测试通过；七类复核未发现本批新增生产偏差。按用户最终授权共删除96份源码目录Markdown（scripts中89份及其他子目录7份），未归档；源码目录仅保留根README/AGENTS，既有dist交付快照保持。docs原有验收摘要保留，旧报告链接改指已有摘要锚点；465条相对链接及锚点无断链。三个诊断runner移除已删报告复制依赖，保留源码/工具冻结与hash门禁；实际准备代码9个隔离场景及3份完整脚本语法通过，不运行服务/负载。原42条要求/顺序及29勾选不变；资源、原生平台、外部账号和正式发布门槛仍未完成，原697长测独立继续。18:17检查点2657轮/7小时35分31秒，未轮换，无OOM/重启；本批不改旧`.5`归档、签名状态或unknown元数据。


**长跑收尾准备（2026-10-29）：** 既有native最终核验器含macOS路径/PID假设，已准备独立Linux只读入口`/tmp/shiki-default-linux-final-review-20261029/verify.py`；固定697源manifest、16份原契约源码及精确CID，核四角色身份/自然到期/两分钟恢复、TCP不重放、30分钟尾部、11个受管PID的fixture清理和精确容器缺席。PID只要求更换，不能要求数值递增；并发事件按完整记录多重集对照，关键阶段单独核时序。语法和当前真实pending路径已验，23工具/契约文件hash复核；没有运行新负载、探测宿主同号Linux PID或读取私有失败runtime。697终态分支尚未执行，准备不算通过；17:46精确容器仍运行，完整检查点17:45:47为2474轮/7小时4分2秒，无OOM/重启。三文档同步、原42条/29勾选及资源/外部门槛保持；完整入口与范围见[原运行报告](15-implementation-status.md#evidence-default-rotation-linux-current)。

**后续资源所有权核对（2026-10-29）：** 限定检查630源所有权审计之后新增的startup查询、route attempts、status-only NodeControl与sync Stop：查询许可/子进程/连接均有界并有退出清理，最多两项路由尝试替换旧值，Stop使用单清理owner；既有取消/准入/代际/阻塞依赖及真实syncack/systemd证据已覆盖这些具体边界。当前541执行输入仍与.5一致；未发现新的已确认保留根或足以支持追加用例的具体缺口，不对历史增长归因、不追加短内存实验或新12小时。原697源17:23实查仍存活、无OOM/重启；最后完整检查点17:22:27为2338轮/6小时40分42秒，尚未自然到期。此轮为定向源码核对和已确认活跃进程的必要等待，资源/外部门槛及29/42保持；不重复上一批全套测试。见[覆盖与所有权边界](15-implementation-status.md#evidence-long-run-source-coverage)。

**最近完成批次：当前安装恢复与真实systemd组合补验（2026-10-29）。** 针对旧服务验收未包含三处安装修复及新sync Stop的实际覆盖缺口，复用完整既有场景首轮通过：744冻结源、541执行输入与.5一致，13生命周期边界、4活动升级/回滚及真实poweroff，外层165.634秒、race活动探针52.88秒/零skip。每次切换均观测256KiB部分传输、公开目标旧完整hash保持、旧host/worker回收、同pane/原Token保留、文件冲突显式解决与新TCP恢复。产品是普通build，仅活动探针race；只读挂载源复制到容器可写/work构建，未宣称/work后置hash。独立44项复核无未解差异，正常退出0及精确CID/私有镜像删除已核验；systemd日志保留tmp.mount卸载FAILED，随后正常poweroff，不宣称日志全零失败。同期七类架构审查无新增确定性生产偏差，修正15/16旧长跑时态及08两处已完成却仍写待办的记录。三文档当前并行表、部分进展和结论同步，原42条/29勾选保持。正常服务场景不替代受控失败回归、掉电、历史发行、长稳/资源或外部门槛；697原长跑继续。见[服务补验](15-implementation-status.md#evidence-install-systemd-recovery-validation)。

**最近完成批次：当前sync Stop与丢ACK恢复补验（2026-10-29）。** 补齐上批六包遗漏的既有syncack整包；743同源、541执行输入与.5完全一致，macOS/Linux各一次race+vet通过，各4顶层/7子用例、0skip，包42.802/28.722秒。四种已持久化未知提交后的真实宿主RESTART均保留日志/源/旧基线，旧client回收、server不变、先读恢复且仅一次上传；32-watch准入、映射归档及未来schema拒绝也通过。无生产/测试修改、预算放宽或重复尝试。清理证据限fixture明确join/Reaped/runtime空断言；原生私有tmux目录与精确Linux容器正常回收，非完整PID清单。独立29项核验、七类复核及08/15/16同步，原42条及29/42保持；不作为重复worker Stop、超过5秒排空、提交途中强杀、长稳或资源验收。见[专项结果](15-implementation-status.md#evidence-sync-stop-ack-integration-validation)。

**最近完成批次：长跑与候选源码覆盖核对（2026-10-29）。** 697源521份执行输入与当前.5的541份逐项比对：487相同、34修改、20新增、无删除；54差异为25生产Go、3协议/生成、23测试及3独立诊断输入。14份轮换观察/runner/collector字节保持，但新sync Stop改变实际资源排空，路由attempts含运行字段持久化剔除，不能把旧运行追认为当前组合长稳；安装器不在默认长跑执行范围。两路独立只读复核和七类文档审查完成，已有专项逐组对应，未新增测试或重启实验。08/15/16同步，原42条及29/42保持；当前候选默认时长/资源及外部门槛仍待，文件数量变化不单独构成再跑12小时的理由。见[覆盖边界](15-implementation-status.md#evidence-long-run-source-coverage)。

**最近完成批次：四目标CI打包入口（2026-10-29）。** 原compile矩阵改为既有make release，build.sh仍执行原go build ./...；新增精确归档SHA及包内SHA256SUMS pin校验，固定CI版本且禁签名。独立741冻结源的Linux arm64普通用户容器串行四目标首轮通过，29命令/4归档/56成员/24份二进制静态目标信息均核验；源/工具后置hash、正常退出与精确容器清理通过，独立复核包含ELF/Mach-O头。七类架构复核完成，541执行输入与.5不变；仅工作流/文档变化，不运行目标程序、不冒充远端CI或原生平台验收，本地umask077权限结果不外推。08/15/16同步，原42条及29/42保持；签名/发布/外部与资源门槛仍待，697长跑原样继续。见[CI打包验证](15-implementation-status.md#evidence-release-ci-validation)。

**最近完成批次：Linux长跑中期资源只读分析（2026-10-29）。** 原697源日志已保存293个完整检查点/2344条八进程样本；截至15:37:57为1731轮、4小时56分12秒，原容器/StartedAt保持，应用退出未产生。Agent前四完整小时RSS中位数57.45→59.82MiB，末段回落不作为无泄漏证明；全部角色/峰值及不完整小时保留。原FD+4与测试goroutine14+6门禁在各检查点前完成，不声称无瞬时越界或生产进程goroutine已测量。独立32项复算、源双树697hash核对及七类架构复核完成；插件目录/依赖符合文档，当前541执行输入与.5一致。本批无新负载或产品修改，原42条及29/42保持；自然轮换、尾部、清理、资源稳定及外部门槛仍待。见[资源观测](15-implementation-status.md#evidence-default-rotation-linux-resource-observation)。

**最近完成批次：恢复修复候选包 .5（2026-10-29）。** 同一739文件冻结源码的macOS/Linux arm64未签名归档已首轮构建、验收并交付至dist/local-20261029.5；541份Go/proto/全部module与上批738已验输入逐项一致，包含四份恢复修复。两平台各52实际命令、14成员/六程序/双插件及全部既有诊断/确认/配置保护检查通过；实际归档helper首次与重复stage-only各校验14成员和私有review，无current/receipt/config/state/安装links，标准服务文件前后保持。两个agent均正常退出/reap，原生PID/runtime及精确Linux容器清理独立核验；源/工具/交付哈希与七类复核完成。08/15/16同步，原42条及29/42保持；未签名、Commit/BuiltAt unknown，暂存不是激活，.4历史及697长跑范围独立。见[候选验收](15-implementation-status.md#evidence-local-candidate-20261029-5)与[交付说明](../dist/local-20261029.5/README.md)。

**最近完成批次：同步停止重试与安装失败恢复（2026-10-29）。** sync client Stop改为单次清理owner与各调用者独立截止，资源未排空不再重试假成功，关闭错误及未知提交源/日志保留；安装Stage补metadata/父目录同步及已发布目标重试屏障，Rollback早期失败仅恢复可信已提交选择，随机服务临时文件允许中断重试且保留旧现场。停止7条与安装恢复13条父/子同测红绿通过；Linux实际syscall证明原14/0→17/17同步，9精确EIO及新进程恢复通过。738同源双平台六相关包race/vet及真实sync子进程通过，各152父/子pass，1/2 skip不计通过。原生应用0但采集器因缺PID清单退出1，保留失败并限定清理证据为fixture Close/Wait；Linux应用/采集器0、精确CID及副本已清理。七类复核与独立审查完成，08/15/16同步，恢复Phase2项至29/42、13项待验，原42要求保持。.4归档和697长跑不追认本批修改。见[合并验收](15-implementation-status.md#evidence-recovery-conformance-validation)。

**最近完成批次：分析器依赖、节点状态诊断与 .4 交付（2026-10-29）。** 独立analyzer的x/sys已最小升级0.44.0，同库两目标复扫零finding，725同源双平台各16终端捕获及清理通过。修复只读状态误要求node.login.v1及doctor被不可用清单阻断：实际运行端NODE_DISABLED/NODE_STATE_UNAVAILABLE、未知清单null和正确Dashboard提示均保留，登录能力及UID/实例/截止校验仍严格。有效同测红绿与729源双平台四包race/vet通过，各196父/子pass、无skip；同537份Go/proto/全部module输入的729源.4实际归档两平台首次通过，各50命令、两agent正常退出及精确清理，12文件已交付。七类复核及独立核验完成，08/15/16同步，恢复诊断与Dashboard两项，原42条文字/顺序不变、当前29/42、13项待验。未签名、Commit/BuiltAt为unknown；.3历史错误、725/729不同证据和697独立长跑范围分别保留。见[依赖修复](15-implementation-status.md#evidence-terminal-analyzer-remediation)、[节点诊断](15-implementation-status.md#evidence-node-status-validation)、[实际归档验收](15-implementation-status.md#evidence-local-candidate-20261029-4)及[交付说明](../dist/local-20261029.4/README.md)。

**最近完成批次：端口握手分支与独立分析器依赖（2026-10-29）。** 已补8个Send-EOF/非配额ResourceExhausted握手及已建立流对照分支，以受控公开Connect transport驱动真实Worker/TCP，验证既有payload/同监听/槽回收/无重放和正常清理；724同源双平台完整client包race/vet均首次通过，各7顶层+8分支、无skip，原5秒预算保持。嵌套VT analyzer已新增独立CI扫描，固定v1.1.4两arm64静态目标均无受影响已导入包，但保留GO-2026-5024 Windows模块公告；原生精确CI命令亦退出0，不称零漏洞或远端CI已跑。源后置/精确Linux清理及七类复核完成，08/15/16同步、原42条及29/42保持。没有生产/协议/依赖升级或产物重建，697长跑独立继续。见[端口分支](15-implementation-status.md#evidence-port-handshake-branch-validation)与[独立依赖扫描](15-implementation-status.md#evidence-terminal-analyzer-security-validation)。

**最近完成批次：CI终端语义回归入口（2026-10-29）。** 已将原有test-terminal-state.sh接入macOS/Linux默认CI独立步骤，修复缺analyzer时终端语义专项一直跳过的入口遗漏；只增加调度，不改生产、测试断言、模块或原90秒/60秒预算。同一719基础源加唯一环境wrapper的720源，逐字CI run block在macOS/Linux各执行一次：实际专项2.18/0.82秒通过，各16捕获、无skip；原生六PID/runtime独立缺席，Linux应用/collector退出0、精确CID缺席且源码副本正常删除。原/派生源码后置hash与七类复核通过，08/15/16同步；远端GitHub Actions尚未实际执行，原42条及29/42保持，不重建.3或改变697长跑。见[CI终端验收](15-implementation-status.md#evidence-terminal-ci-validation)。

**最近完成批次：本地候选包 .3 交付（2026-10-29）。** 同一717文件冻结源码已通过macOS/Linux arm64既有make release和实际归档验证，交付至独占dist/local-20261029.3；530份Go/proto/module与已验719完全一致，包含诊断、操作确认和配置恢复修复。两侧14成员、六程序、双插件清单/hash/pin、startup/list/doctor、缺--yes零启动和坏配置原件/备份保留均通过；正常Shutdown/reap、源事后hash和精确Linux清理已核验，无失败重跑或强制清理。七类架构复核完成，08/15/16同步，原42条及29/42不变。未签名、Commit/BuiltAt为unknown，不追认697长跑、外部连接或正式发行；空服务器诊断不证明实际路由。见[候选报告](15-implementation-status.md#evidence-local-candidate-20261029-3)及[交付说明](../dist/local-20261029.3/README.md)。

**最近完成批次：诊断与操作确认（2026-10-29）。** 已补06要求的实际路由尝试列表和损坏配置恢复说明，以及05要求的stop/restart非交互--yes确认；运行字段有界、按代际发布且不持久化，原回退授权/停止实现/保存语义保持。三项有效同测红绿及独立审查完成；718同源双平台受影响race/vet各179顶层通过，安装专用skip和无测试包单列。现有4处E2E调用仅追加--yes，719源仅两测试文件和环境脚本不同；双平台两条真实CLI生命周期/端口恢复E2E及tagged vet均通过，无skip。首轮旧Go环境前置失败保留，实际验收仅修PATH；源后置/正常Linux清理和七类复核通过，08/15/16同步。恢复三项，原42条文字/顺序不变、当前29/42；707归档与697长跑不追认这些后续变更。见[合并验收](15-implementation-status.md#evidence-diagnostic-confirmation-validation)。

**历史启动与中期观测：新Linux默认12小时（2026-10-29，最终结果见文首）。** 已验宿主/观察修复和最终collector组成独立697源，于10:40:43启动；collector33067、精确容器232b672a…65f3，四初始worker/UID1000/只读源码已核验。17:46只读检查截至17:45:47已2474轮、7小时4分2秒且原容器仍运行；默认凭据12小时，最早22:41自然到期，恢复尾部下界23:11、外层截止23:55（Asia/Shanghai）。整轮恢复、至少30分钟尾部、资源及最终清理仍待验；原Linux失败保留。后续启动策略、诊断/确认、节点状态、同步停止与安装恢复修复及.5候选不在该冻结源中。见[独立运行](15-implementation-status.md#evidence-default-rotation-linux-current)。

**最近完成批次：默认入口补齐采集器回归（2026-10-29）。** 发现已有13项Python采集器关键回归未进入make test及CI，已新增test-tools并由test依赖；普通用户/Python3.10+前置避免root跳过生命周期测试却返回成功。同一最终四文件在macOS/Linux各13项通过、无skip；Linux实际root调用退出2且未运行suite，make -n test确认原三组Go入口保持。只改Makefile及文档，采集器/测试断言/生产输入不变；七类架构复核完成，08/15/16同步。未声称远端CI已运行，不重建707候选或变更697长跑；原42条及29/42保持。见[默认工具检查](15-implementation-status.md#evidence-default-tools-validation)。

**最近完成批次：本地开发候选交付（2026-10-29）。** 同一707文件冻结源码已通过既有make release构建macOS/Linux arm64的v0.0.0-local.20261029.2归档，并交付至独占dist/local-20261029.2。两侧14个归档成员、六二进制版本、双插件清单/hash及校验pin均已核验；实际解包CLI/agent的隔离startup JSON、新文本说明、停止后不自启、正常Shutdown/reap与清理通过，Linux为UID1000且精确容器已删除。相对已验启动策略产品输入仅一句CLI说明变化，本批已实际覆盖。完整源清单随包提供，未签名，Commit/BuiltAt为unknown，不作为正式发行或发布者认证。七类架构复核及08/15/16同步完成；原42条及29/42不变。697源12小时运行独立保持，签名/分发、资源及外部平台门槛仍待。见[候选包报告](15-implementation-status.md#evidence-local-candidate-20261029)与[交付说明](../dist/local-20261029.2/README.md)。

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

**默认12小时验收新进展：2026-10-29 02:37:12（Asia/Shanghai）。** 原macOS运行已通过：4216轮、实际工作12h30m1.03382625s，四worker各自然替换一次，恢复尾部1802.031秒；应用/collector退出0、owned_cleanup无失败，13个记录PID已不存在，原runtime/source已删除，防休眠进程正常退出。独立复核`/tmp/shiki-default-native-final-review-20261029`通过；server-port瞬态不可用未被采样，保留false、不声称同时退役。仅覆盖原495文件/411份Go-proto-module，Linux原运行仍在到期前阶段，新修复另行验证。见[轮换记录](15-implementation-status.md#evidence-default-rotation-validation)。

**原Linux默认12小时已失败（2026-10-29 09:13:50）。** 原578文件源码运行43,219.82秒后，在server-sync旧代跨12小时退役后的替换进程身份读取阶段报`readlink /proc/274459/exe: no such file or directory`；4,191轮最后检查点仍recovered=false，无四角色恢复或30分钟尾部。该PID角色未保存，不能断定是无关短命child或候选worker退出。原七PID清理无报错，精确容器已不存在；新代未登记，不声称逐PID新代清理。失败证据保留，观察夹具已完成独立双平台确定性修复验证，不自动重启12小时，不覆盖当前633产品修复。见[实际运行记录](15-implementation-status.md#evidence-default-rotation-validation)。

**只读中期观测：2026-10-28 21:51。** 从两轮现有日志保存完整前缀：macOS459个检查点、7小时44分/2610轮；Linux38个检查点、37分33秒/227轮，均recovered=false。旧版macOS Agent前七个完整小时RSS中位数从63.77升至75.95 MiB，当前80.06 MiB；Linux当前54.49 MiB，但运行不足一小时且源码不同，不能比较修复效果或认定稳定。全部角色/高点保留，RSS不等于保留堆或泄漏证明；不追加负载、不重启，29/42保持。见[只读观测](15-implementation-status.md#evidence-default-rotation-observation)。

**此前批次：残余资源增长的观测开销调查（2026-10-28）。** 当前29/42不变。本批仅8个诊断文件变化，生产输入不变；默认/延后导出的同二进制短预检均通过。唯一一组普通Linux负载/空载各317.709/317.672秒完成，136/128份原始归档、各68份采样JSON、6个关闭后profile及精确进程/容器清理独立核验通过。Agent末段load−control为−260,788 B/min、+9.98对象/min；heap负值受idle3高点回落主导，不表示稳定。关闭后仍有+24,240 B/+228对象差值；旧参照为+79,456 B/+302对象。保留全部样本，不由一组干预推定因果或无泄漏。按预声明停止追加短窗口实验，资源门槛仍未关闭。全面架构复核与08/15/16同步完成，见[观测开销报告](15-implementation-status.md#evidence-memory-observer-validation)。

**上一批：请求生命周期与跨schema服务验收（2026-10-28）。** 当前29/42、13项待完成。Agent共享pool请求回调保留已确定性复现并修复；专项与Linux修前/修后/同二进制空载三组完成，修后后段load−control仍+15,597 B/min、+92.35 objects/min，不认定无泄漏。macOS与Linux完整race/E2E各46包通过；两次完整运行的生产输入相同，Linux最终快照仅多一处syncack夹具预算修正，macOS对该修正另有四场景race补验；不称两侧最终测试源码逐字相同。6/10项skip不计通过。真实Linux systemd开发schema升级、旧版拒写、有限恢复及再次同步已通过独立复核，不代表产品自动恢复。历史失败均保留，见[生命周期与回归](15-implementation-status.md#evidence-pool-request-lifetime-validation)、[跨schema服务](15-implementation-status.md#evidence-schema-service-upgrade-validation)。本批全面复核与08/15/16同步完成；原12小时运行和外部门槛保持待验。

## 本批收尾工具复核：原697源Linux（2026-10-29）

仅新增仓库外只读核验入口与准备记录，当前541执行输入无变。七类复核：目录/生产依赖与SDK-host-plugin边界保持；无RPC、权限、能力或认证改动；工具只读源/原证据、输出新review文件，不修改状态/日志/去重；进程身份按Linux namespace记录核验，不能拿同号host PID当回收证据，最终Docker只读缺席检查在终态后执行；原消息/文件/终端/TCP限额与轮换时钟不变；CLI/TUI/安装/候选产物不变；pending/语法准备与最终自然轮换验收明确区分，后者尚待真实结果。根对照原harness/epoch/port语义审阅入口并重核23个hash；未新增产品测试或强造成功数据。原42条及29/42保持，08/15/16同步。

## 本批完成复核：安装组合覆盖与进度时态（2026-10-29）

长跑继续期间完成七类只读复核：218份生产Go的插件/宿主依赖方向无违规，四个旧插件实现位置不存在；业务保持在plugins/sync/port，宿主/SDK保持internal/plugin与pkg/plugin。RPC注册、方法权限、撤销/到期取消与实例能力准入保持；运行字段从持久server/去重结果剔除，Stage同步屏障与Rollback可信已提交选择校验存在；宿主关闭准入、撤权并join子进程/回调后释放writer lease，sync Stop仍是单次清理owner及独立调用者等待。文件/消息限额、分类准入、首帧预算、长期流取消与半关闭边界未发现新增确定性偏差；stage-only/激活分离，服务临时文件随机排他创建，备份在停写锁下执行，回滚不还原旧认证状态。此次静态复核不替代运行验收。

发现并立即修正文档同步偏差：15/16的旧1744轮macOS观测明确标注2026-10-28历史，并指向macOS完成/原Linux失败/697新运行；08中“正式负载结果仍待”和“下一步确定性释放测试”改为对应后续已完成证据。当前区笼统“不再追加新专题”限定为停止既定短窗口内存实验，独立有据工作继续。三文档新增相同并行表，08进展与剩余表补齐新Stop/ACK及当前Linux运行。原42条要求和29/42保持，不以文案修订制造完成项。

当前安装补验有具体覆盖依据：最近真实systemd源仍使用旧安装Stage、服务文件写入和sync Stop；完整既有场景能触发当前真实产物发布/重用、unit写入及四次带活动流的启停。744文件冻结源、541执行输入与.5一致；仅外部工具环境限制构建并发，生产/测试/超时不改。原完整场景首轮165.634秒通过，13生命周期/4活动切换及正常关机、精确CID删除见文首和[服务报告](15-implementation-status.md#evidence-install-systemd-recovery-validation)；原始tmp.mount卸载FAILED保留，随后正常poweroff，不泛称系统日志零失败。正常场景不证明Rollback备份/journal失败分支、fsync EIO或硬件掉电。697原长跑不变，新源码的短时服务证据不追认为默认时长/资源通过。

独立最终回执：`/private/tmp/shiki-install-systemd-recovery-20261029-1g42h_9t/independent-review.json`，SHA256 `c1234049b63d1700a98f7b659de96be9045233d55fcdc4b330fe996c1cde365f`，44项检查通过、0未解差异。完整744双树、541执行输入、工具/21原始证据hash、13/4实际记录、正常poweroff/cleanup状态一致及精确CID/私有镜像缺席已独立核验；没有追加测试。

## 本批完成复核：恢复修复候选包 .5（2026-10-29）

本批仅冻结/构建/实际归档验证及交付，不修改生产。739源manifest `4cdb694c706d66c88774101e35c0bdacddc597e5e9ce86d43a802bb046cfa87b`；541份Go/proto/根及嵌套module与上批738源全部相同，其余仅报告/进度变化和排除环境脚本。七类复核：plugins/sync/port业务、internal/plugin宿主及pkg/plugin SDK归属和依赖保持；RPC/版本能力/授权保持且实际归档节点只读/严格登录拒绝检查通过；schema/revision/去重保持，私有首次/重复Stage不生成current/receipt/config/state/links；两agent正常Shutdown/wait/reap，原生两个PID与runtime缺席、Linux UID1000正常退出且精确CID缺席；文件/终端/网络默认不改，14平铺成员/hash/模式与本地pin验证；六版本一致，stage-only仅私有review不注册服务，标准ServiceFile真实账户位置只读核对未变；前批race/vet、9EIO、native collector1、skip、当前归档与697长跑分别记证，不合并外推。

macOS/Linux首次构建及实际检查分别42.551/127.876秒，每侧52命令；新增helper首次及重复暂存逐项核对14产物及review，拒绝把staged/NOT_STARTED_OR_READY标作已激活。两个归档SHA256分别 `058677021ec5fa559a83ae897e367b338ecebffdd783a10a9392380dd0020ede` / `22482c5dded5ca4539a728476e510869530180b1aff47a7765138aa4580522cd`。Linux精确CID `e38fa9376e4d1edde017ea400f39b0c5cc6c50b05c9c6f45779cea837909614b` application/runner均0，无OOM、errors或强制清理。两侧标准服务文件实际均在暂存前后缺席，不外推已有服务迁移验收。原生两个agent PID51181/51239已独立确认缺席；该证据只属于本次候选，不补造上批普通suite缺失的PID inventory。

根重新计算交付12文件、全部归档成员和739源清单，实际读取两侧stage结果；旧.2/.3/.4文件hash保持。独立工具/实际归档/文档复核与[候选报告](15-implementation-status.md#evidence-local-candidate-20261029-5)、`/tmp/shiki-local-candidate-20261029.5-root-artifact-review.json`分别留存。08/15/16同步、原42条和29/42保持；未签名及unknown元数据、完整平台/外部账户/实际登录重启/资源稳定和正式发布门槛继续保留。

## 本批完成复核：同步停止与安装恢复（2026-10-29）

按04/06/10/11确认四处偏差并立即修复，未改设计标准。目录/SDK-host-plugin生产依赖保持，architecture双平台通过；RPC/授权/能力无改动，closed仅拒绝业务，实际完成必须等待清理；unknown日志/源及当前授权状态保留，Stage元数据与跨目录发布完整同步，revision/schema/去重不变；单清理owner跨Stop请求截止继续运行，回滚未知/未提交选择不自动Start；文件关闭/EIO错误不吞，临时路径仅清理本次新建对象，终端/网络限制不变；安装CLI/receipt/路径不变，既有归档不追认后续修改；测试分别列有效行为红绿、syscall观察、普通suite、skip和原生采集器失败。只读复核HTTP授权先于幂等重放、撤销registry复核/监控、agent变更回执不持久化Token及配置解析泛化错误，未发现该范围新增确定性偏差。

738冻结源manifest `0c412a80da9e48fc3c0774fc1dc1b0620e4e4c665165b4b1f210323a5f9108b4`，541执行输入与工作树相同。两平台六包race/vet、各152父/子pass；native1 root-only skip，Linux另1 isolated-space skip均不计通过。原生app0/collector1原因是普通suite没有processes.json，未改采集器或补造清单，保留原失败；原生仅fixture Close/Wait/watch归零证据，非独立PID普查。Linuxapp/collector0，exact CID75614d7e…03c8b3缺席，source后置/副本正常删除。两侧总44.559/78.095秒，不是单包耗时。

同期Stage专项581项执行/工具清单的before/after仅artifacts.go不同，旧新实际fsync为14/0与17/17；9精确EIO每案原选择/receipt/配置/身份保持及新进程两次Stage通过。该专项比最后transaction/filesystem改动更早，不能合并为同一源码。7条Stop和13条安装恢复同测红绿独立记录，测试字节相同；受控lifecycle和错误点不冒充真实服务/物理断电。

七类详细表、命令、原失败、范围和证据见[合并验收](15-implementation-status.md#evidence-recovery-conformance-validation)。独立回执 `/tmp/shiki-recovery-independent-20261029`，根复核 `/tmp/shiki-recovery-final-root-review-20261029.json`。08/15/16同步，恢复Phase2生命周期至29/42；原42文字/顺序保持。.4/697保持各自源范围，完整资源稳定、外部平台/账号与正式发布未完成。

## 本批完成复核：分析器依赖、节点诊断与 .4 归档（2026-10-29）

对照00–14发现并立即修复两项诊断偏差和独立分析器已知模块公告，没有降低既有要求。复核覆盖全体已实现架构类别，并结合本批改动和保留证据核验；不声称逐行证明全部实现。

| 七类审查 | 发现、修正及证据范围 |
| --- | --- |
| 目录、依赖与所有权 | 节点修复位于client/CLI/UI；插件业务仍在plugins/sync、plugins/port，host/SDK分别在internal/plugin、pkg/plugin。架构包双平台通过；仅嵌套analyzer x/sys升级0.44.0，根模块0.48.0及VT0.1.0不变。 |
| RPC、准入、版本能力与授权 | GetNodeStatus私有只读协商不要求登录能力；OpenNode/login/cancel仍严格要求node.login.v1。版本、同UID、实例、header隔离、截止与取消均由真实UDS测试验证；不新增协议或虚报能力。 |
| 持久与运行态、原子性、revision及去重 | 读取运行端真值，磁盘enabled反转不能替代状态；doctor未知清单为null而非空清单，独立诊断继续。配置及损坏身份证据保留，无schema/持久写入/去重改动。 |
| 进程、启停、取消、崩溃恢复 | helper负责关闭连接；已有协商失败/截止/取消回收保持。实际归档每平台两个私有agent正常Shutdown、wait/reap和socket/runtime清理；Linux精确CID缺席，无强杀。697长跑原实例未重启。 |
| 文件、终端、网络及错误限制 | 身份路径错误不重建身份；文件/TCP/终端生产语义及默认限额不变。升级后analyzer双平台各16捕获通过，未知清单不触发远程探测，未把静态双目标扫描扩大为Windows运行验证。 |
| CLI/TUI、配置、安装与产物 | node默认/status/显式socket、doctor及Dashboard显示实际原因和恢复建议；只读查询不触发登录。归档保持14平铺成员/六程序/双插件；未签名、Commit/BuiltAt unknown明确，不安装用户服务。 |
| 行为证据、声明与门槛 | 有效同测红23失败后相同回归绿；729源两平台各四包196父/子pass和vet，无skip。729候选两侧各50实际命令通过，12文件交付独立核验。旧失败保留，资源/平台/外部/正式发行仍待。 |

分析器725源manifest `3c87d75a9bd2d18ee26a7bf86fe59ac36b1ad10c3ec4b20bbb4fd891da634a4e`：同govulncheck v1.1.4、同2026-10-27数据库，Darwin/Linux arm64各21模块、115/117包，两次复扫零finding；终端语义双平台16捕获通过。该证据不覆盖后续节点生产修改。

节点green729源manifest `23a1eae89e90cbcb062629627ebd0cf8ac779093c7ad3d869d64dd281a277bdb`：两平台相关四包race/vet完整通过，原生54.18秒、Linux159.10秒总运行；首次错误Go bootstrap的准备失败单列为零测试，不伪装首次成功。red仅四生产文件不同，测试相同；不以编译失败冒充行为红。

候选729源manifest `5dc84ef42f548dd0468ace433c71554a991d6da38220db58e396a242e7896758`：537份Go/proto/全部module与green完全一致，文档变化独立记录。两侧实际构建/归档检查首次通过（39.875/127.102秒），禁用、磁盘反转同实例、状态目录错误及严格登录拒绝均有原始输出；macOS/Linux归档SHA分别为 `e05877b2854844d5d03142b4a9757bddaee052b8a09b7c26849f2b4c33b268f6` / `d0d7fea318447b94ece652115d22e755e5645f62cdbc6c454e0bdc9ede5e80cb`。旧.2/.3不变；697默认12小时源不追认本批。

见[分析器修复](15-implementation-status.md#evidence-terminal-analyzer-remediation)、[节点诊断](15-implementation-status.md#evidence-node-status-validation)与[归档验收](15-implementation-status.md#evidence-local-candidate-20261029-4)。独立复核回执位于 `/tmp/shiki-resource-capacity-independent-20261029`。08/15/16同步，恢复两项至29/42，原42条文字/顺序和外部门槛保持；历史报告新增后续说明不改变其原始运行结果或旧时点闭环hash。

## 本批完成复核：端口握手分支与分析器扫描（2026-10-29）

七类全架构复核：插件测试仅依赖公开SDK/protocol，业务仍在plugins、宿主internal/plugin、SDK pkg/plugin；受控transport测试不改RPC/准入/授权/能力；持久/运行state、原子性/revision/去重不变；真实Worker/Bridge/TCP的终态、既有连接、Stop与释放原监听均动态验证，成功断言先于父取消/cleanup；默认5秒拨号及半关闭/错误语义保持，无测试修改生产默认值；CLI/TUI/配置/安装产物不变，CI仅增加嵌套module扫描；证据明确受控transport不是HTTP2/跨进程验收，静态目标扫描不是目标运行，module-only公告及未执行远端CI保留。未发现新增确认生产偏差。

724冻结源为723当前文件加唯一环境脚本，manifest48e37a1d588b1c99c0fee3a5f09cec05186a5f3780a791c4513a4b7ce3e2f434。所有生产/proto/根及嵌套module与.3候选相同；相较已验CI基础源仅新增两Go测试。口径澄清：此前530项计数为所有Go/proto加根go.mod/go.sum，不含analyzer内go.mod/go.sum；完整计入这两文件的旧基线为532项、本批新增两测试后534项。嵌套三文件另有独立冻结/扫描/hash证据，未将缺少扫描冒称原根扫描覆盖。

macOS/Linux完整port client包race6.413/6.049秒，各7顶层与8分支通过；vet与launcher/collector均0，无skip/fail/race或强制清理。等待握手终态分支实际5.00/5.01秒，父worker仍live；正常Stop/连接计数/重bind断言通过。原生没有daemon/worker子进程，不冒称完整外部PID inventory；Linux精确CID缺席、只读mount、源后置一致和owned副本删除均核验。独立证据`/tmp/shiki-resource-capacity-independent-20261029/port-handshake-evidence-independent.json`。

分析器扫描固定govulncheck v1.1.4/Go1.26.10，Darwin/Linux arm64 CGO0各21模块及115/117包，DB2026-10-27T21:16:18Z；两图均无受影响已导入包。仅模块公告GO-2026-5024涉及x/sys0.41.0的Windows NewNTUnicodeString，修复版0.44.0；该Windows包不入两图，但本批未修该模块公告，不称无漏洞。产品根module另用0.48.0。原生CGO1实际执行新增CI固定go -C/run命令退出0；远端CI与其他目标仍待。首次offline go list-m元数据不足的预备失败保留，零扫描；正常proxy/sumdb下相同源完整扫描，未改变版本。独立审查与每次实际命令/输出见[扫描报告](15-implementation-status.md#evidence-terminal-analyzer-security-validation)。

08/15/16同步收尾，原42条和29勾选保持。.3交付与697长跑各自来源/验收范围不变；原失败、外部账号、完整平台、资源稳定与正式发行门槛继续保留。

## 本批完成复核：CI终端语义专项（2026-10-29）

七类复核覆盖全体已实现边界：目录/SDK/host/plugin归属与530份Go/proto/module输入保持已验基线；RPC传输/准入/授权/能力不变；持久/运行状态、原子性/revision/去重不变；进程实现与取消保持，专项实际覆盖tmux附件/原pane存续及正常清理；终端颜色/光标/宽字符/备用屏幕/尺寸仍由原16捕获断言，不修改文件/网络限额；CLI/TUI配置/安装产物保持，新增仅CI调度，VT analyzer留在独立module；历史默认suite的skip不计通过，新步骤两平台实际执行并分别记录本地与远端CI范围。未发现本批新增确认生产偏差。

719基础源manifest为d84e39927a9243fe49a5f927e356ba2bba822ad114c943d53a7d8f6c06b47998，720派生源仅加环境wrapper；CI原run block逐字执行、已有脚本和测试预算不变。native实际专项2.18秒、总17.795秒，Linux实际0.82秒、collector122.113秒；均唯一一次且无skip。每侧16捕获/fixture清理证据通过，原生六记录PID与runtime独立确认缺席；Linux普通用户runuser和非root guard通过，不用宿主文件UID替代容器numeric UID记录。Linux应用/collector退出0、精确CID缺席、源码副本正常删除，无强制清理；迟到docker top因容器已删除返回错误，仅保留该观测，不重跑通过用例。collector根无inventory不冒充完整PID证据，子目录fixture记录单独核验。

见[专项报告](15-implementation-status.md#evidence-terminal-ci-validation)。本批08/15/16同步、29/42保持。该批收尾时排入的analyzer嵌套module扫描及端口握手分支补验已在上方后续批次完成，分别保留来源与证据。外部账号、完整平台、资源稳定与正式发布门槛保留。

## 本批完成复核：本地候选包 .3（2026-10-29）

本批仅冻结/构建/验收/交付，530份Go/proto/module与已验719源完全一致。对照00–14复核七类边界：插件业务仍在plugins/sync、plugins/port，宿主internal/plugin与SDK pkg/plugin分离；RPC/准入/授权/能力不变，归档CLI通过实际IPC查询；schema/revision/去重不变，私有配置及无效原件/备份均不变；两agent正常退出/reap且socket/runtime清理，Linux UID1000容器正常退出后精确CID缺席；文件/终端/网络限制不变，归档14个平铺文件模式/大小/hash及pin核验，空inventory不外推数据面；六程序和双插件版本匹配，未签名和unknown元数据明确；包内smoke与此前race/vet/E2E、697长跑及外部/资源/发行门槛分别记录。未发现本批新增确认偏差，不改设计基线。

两平台首次通过，旧.2未覆盖或修改。根重新计算12交付文件和源manifest，核对两份原始smoke结果及Linux精确容器清理；独立审查覆盖源码、工具、七类与归档。证据见[报告](15-implementation-status.md#evidence-local-candidate-20261029-3)及`/tmp/shiki-local-candidate-20261029.3-root-artifact-review.json`。08/15/16同步，42原条目与29勾选保持。

## 本批完成复核：诊断、确认与配置恢复（2026-10-29）

按02/05/06/12/13/14发现三处明确契约偏差并修复：实际尝试列表、破坏性非交互确认和损坏配置恢复说明。已完成七类审查：目录/SDK-host边界无变；additive本地诊断字段不改变RPC授权或实际策略；字段clone/代际检查、server和去重回执清除及持久校验到位；取消不额外拨号、Stop/Restart实现不改；无文件/终端/网络限额变更或秘密字段复制；CLI确认/白名单提示及加载器只读恢复说明符合原契约；同测红绿、718双平台受影响包和719双平台两条实际CLI E2E分别有真实证据，skip不计通过。

718源manifest46bdc6b9e941c85ed7c4b9ecc0279c1f0b521d75f47274a7295725876a8edf00；719源47ad6d0ff7b3fe567b4981113a8d06d3a7fc18739306753c4007d8c1b0605c27仅改两个E2E文件4个--yes及增加选定脚本，原断言/预算不变，全部production/proto/module相同。原生/容器各179顶层通过；root-only/isolated-space跳过和agent命令无测试明确保留，随后两平台两选定E2E均无skip通过。首次PATH旧Go导致预启动失败、外部review Docker错误文本大小写误判均保留，不追改为成功或因此重跑已通过组。

精确容器清理及源事后哈希由根/独立审查确认；原生以现有fixture正常退出与清理断言为证据，不冒充完整独立PID清单。三处修复收尾后恢复29/42，原42条原文/顺序保持。07默认工具入口批次证据与707归档、697进行中长跑各自隔离，均不追认后续生产输入。详细七类表、所有失败/通过证据及范围见[合并验收](15-implementation-status.md#evidence-diagnostic-confirmation-validation)。

## 本批完成复核：采集器默认测试入口（2026-10-29）

按07质量入口与关键恢复测试、08 Phase5行为分支要求核对，发现13项已有Python回归未由Makefile/CI调用；已补test-tools依赖并拒绝root/Python低版本。原生UID501/Python3.12.7与Linux UID1000/Python3.11.2同四输入各13项无skip通过；实际Linux root退出2且无suite，默认入口dry-run只用于证明调度，不冒充整套Go执行。文档旧Nine计数修为Thirteen。

七类复核：目录/SDK-host依赖保持；RPC/准入/授权/能力无变化；状态/原子性/revision/去重语义无变化；现有真实子进程/快照清理回归加入默认入口；文件/终端/网络/限额不变；变更仅开发测试前置、不改产品CLI/配置/安装/产物；双平台实际工具结果、模拟旧Python版本guard与未执行的远端CI范围分别记录。采集器及13项测试文件匹配已验最终collector，所有Go/proto/module匹配707候选；没有新负载或运行快照修改。Linux正常退出/精确容器清理及源后置检查通过，未发现其他确认偏差。

证据见[默认工具检查](15-implementation-status.md#evidence-default-tools-validation)。08/15/16同步收尾，原42项及29勾选保持；外部CI、长稳和正式发布门槛不因此关闭。

## 本批完成复核：本地候选包交付（2026-10-29）

对照00–14复核已有实现及本批产物；产品输入相对启动策略已验基线仅一行CLI范围说明，实际新归档已覆盖该说明。未发现本批新增的确认生产偏差；旧资源/外部/平台门槛保留。

| 七类审查 | 结论与证据边界 |
|---|---|
| 目录、依赖、SDK与宿主 | 插件入口/业务保持plugins/sync、plugins/port，host为internal/plugin、SDK为pkg/plugin；707源清单与两侧构建输入一致，无生产依赖变化。 |
| RPC、准入、授权和能力 | 归档CLI经实际IPC及agent.startup.v1协商门槛成功查询；无新增能力或原始握手记录声明。 |
| 持久与运行状态、原子性、修订和去重 | 私有配置查询前后hash不变，运行目录/Unix socket隔离并清理；不修改持久schema、revision或去重实现。 |
| 进程、启停、取消、崩溃与恢复 | 两侧实际agent正常Shutdown、exit0和reap；离线查询失败且未自启；Linux普通UID1000容器正常退出、无强制清理且精确CID缺席。 |
| 文件、终端、网络、限额和错误 | 两归档各14个平铺普通文件，模式/大小/hash及校验pin核对；tsnet关闭，不将本批smoke当作sync/port数据面或终端回归。 |
| CLI/TUI、配置、安装与产物 | 实际六程序版本一致，双插件清单匹配；startup JSON与新文本均验证。未签名且Commit/BuiltAt为unknown，源清单提供准确来源；本批不声称已安装服务。 |
| 行为证据与支持声明 | 原生launchd absent与普通Linux manager unavailable如实记录；未用unknown证明启用。候选包不继承608签名或697长跑结果，独立复核交付文件和证据，无新增全套重复测试。 |

交付路径与验证见[本地候选报告](15-implementation-status.md#evidence-local-candidate-20261029)。根代理重新计算交付文件、归档及内部校验和；08/15/16同步记录本地完成范围，保留42条原文/顺序和29项勾选。

## 本批完成复核：启动策略只读展示（2026-10-29）

对照11的安装配置展示、12的界面范围、13的本地IPC及14的传输/错误/资源契约完成七类审查。修复新增输出writer匿名嵌入导致ReadFrom绕过限额、未知plist类型误判，并补共享查询准入；未放宽文档要求或回写服务策略。

| 七类审查 | 结论与证据边界 |
|---|---|
| 目录、依赖、SDK与宿主 | OS配置观察归agent，前端经internal/client及IPC；插件目录/业务、SDK-host依赖及module不变，双平台架构检查通过。 |
| RPC、准入、授权和能力 | additive GetStartupPolicy+agent.startup.v1，身份/能力先于单查询permit；旧agent明确unsupported、跨server拒绝。只读报告不混入Preferences编辑契约。 |
| 持久与运行状态、原子性、修订和去重 | 无持久写入或缓存；展示agent加载时配置来源与当前受管配置观察，跨文件/管理器不是原子快照，变化或绑定不明报unknown。 |
| 进程、启停、取消、崩溃与恢复 | 总3秒预算、实际子进程输出限额/取消与busy后释放测试通过；真实systemd查询保持同一PID/出生/cgroup/hash，最终正常stop/reap/poweroff。 |
| 文件、终端、网络、限额和错误 | 文件受限读取、64KiB上限、有限reason不回传命令/环境；CLI安全文本/JSON。无工作负载、数据同步、TCP或终端行为修改。 |
| CLI/TUI、配置、安装与产物 | config startup区分按需/登录/开机；systemd持久default.target链接和Linger、launchd覆盖有明确范围。未改host标准服务，无真实重新登录/重启声明。 |
| 行为证据与支持声明 | 双平台各四完整包race/vet、110顶层入口（含helper）无skip；Linux四个真实政策状态与精确清理通过。原生实际仅absent，enabled/覆盖使用确定性夹具，不冒充原生正控。 |

原生706源manifest `779878e1a35eb543f517eed2dc5b9f71e7f29e183058831de2d449486a407b26`；最终Linux706源 `7c7134feac7ec99500e07e2f85162f1713d253089b0bce163d752fc4882be12a`，仅runner.py/scenario.py两个诊断输入不同，其余704相同。首轮/tmp noexec未开始测试、第二次只读tools装配在启动前失败，分别保留；修正仅自有诊断目录。原生冻结后的一行CLI范围说明和12文档更新单列静态复核，不追认旧二进制覆盖新文案。报告：[功能验收](15-implementation-status.md#evidence-startup-policy-validation)、[真实systemd](15-implementation-status.md#evidence-startup-policy-systemd-validation)。

该本地缺口闭合，Phase4安装复合条目的外部/真实重启登录条件仍未完成，原42条和29/42保持。新697源默认时长运行不含本功能；继续独立观测，不为纳入新实现而重启。

## 启动时初审：默认时长与启动策略（2026-10-29）

新697源由696工作树输入加一个仅设置环境的shell组成，239份生产Go/proto/module与已验692源一致；host七文件、observer九文件、最终collector三文件分别匹配既有证据。原macOS成功和Linux失败均由实际终态及精确PID/CID核验确认，不复用原运行。只读实际容器检查已确认新四epoch、八角色UID1000与二次业务检查点推进；证据位于新运行目录及`/tmp/shiki-resource-capacity-independent-20261029`。构建并发p1与harness GOMAXPROCS2不代表过滤环境后的生产进程只有两线程。

七类初审：目录/依赖与协议/授权保持已验生产版本；持久/运行状态及revision不变；进程观察与清理使用已验修复；文件/终端/网络工作量、TTL和断言不变；安装/CLI未因长跑新增功能；默认到期、恢复尾部和退出清理仍未发生，不能提前验收。此启动子批已完成独立初审，整轮保持进行中。

并行交付核查发现11的启动策略展示尚未落实：agent.auto_start只有CLI按需语义，配置接口没有登录/开机服务策略。修复归入原Phase4安装条目的本地工作，不把它推给外部账号门槛；当时实现/协议/能力及测试尚待验收；此后已完成的启动策略批次见上文，两个源码范围分开记录。12末尾“所有功能尚待实现”和18原Linux“继续运行”的状态说明已修正，不变更目标要求或外部门槛。当前29/42不变。

## 本批完成复核：宿主清理所有权（2026-10-29）

按00–14及七类要求复核现有实现边界，发现的handler未join、启动晚于首次Kill、停止拒绝响应缺no-store均已在本批修正。仅两份生产host文件变化，其余为内部/跨进程回归；没有修改设计契约以迁就代码。

| 七类审查 | 结论与实际覆盖 |
|---|---|
| 目录、依赖、SDK与宿主 | plugins/sync、plugins/port拥有入口及业务；host位于internal/plugin，SDK位于pkg/plugin；双平台完整架构检查通过，无宿主内嵌业务回退。 |
| RPC、准入、授权和能力 | admission与handler Add共享互斥，先关闭准入再Wait；撤权/首帧取消/背压中断保持，停止拒绝no-store；未新增能力声明。 |
| 持久与运行态、原子性、修订和去重 | 实例锁与runtime保留到实际join；Supervisor在回调结束前不关store或替换writer，持久enabled/revision/receipt保持。 |
| 进程、启停、取消、崩溃与恢复 | Stop每调用者独立截止；Launch普通失败在预算内同步清理，真实截止可提前返回但不释放所有权；Client结束后再次Kill覆盖晚启动，真实子进程与确定性交错证据分开。 |
| 文件、终端、网络及错误 | 双平台真实sync/port、授权/半关闭/取消/背压、worker与host轮换及四种在途传输恢复通过；未改数据重放、限额或终端契约。 |
| CLI/TUI、配置、安装和产物 | 产品命令、配置、安装、schema/module不变；私有process接口不暴露Config测试钩子。693只比692多一个诊断shell，运行快照不变。 |
| 行为测试与声明 | 有效red4/green2、最终晚启动反事实红、原生两包及11项补验、Linux七组race/vet通过；显式排除opt-in资源循环。Linuxapp/collector0与精确清理核验；原生夹具断言不冒充完整独立PID清单。 |

报告：[清理所有权](15-implementation-status.md#evidence-host-handler-ownership)、[受影响链路](15-implementation-status.md#evidence-host-cleanup-affected-validation)。692基础源manifest为`930af07af6b17bac4d11da1326f830a9bc1a4132d31541e50d51ee7444c2ad88`，693诊断源为`a7a231ddf76f702b4d22454160c790251d289ae496988fed8fcf3bd6f4f6ae65`。693内collector仅含初版只读正常清理修复；异常postcheck保留保护另由最终13项Python测试验证，不追认容器覆盖。主报告原“无短TTL负载”已明确为无新增验收负载，现有12秒/30秒行为回归仍如实记录。

独立审查与root复核后恢复Phase0插件原型及Phase2生命周期两项，三文档当前29/42一致。保留旧失败、旧源码默认时长验收边界及资源/外部/正式发布门槛；本批不解释历史heap差值、不重跑12小时，不追加已停止的内存实验。

## 已完成子批复核：同源预检与采集器清理（2026-10-29）

与正在修复的宿主生命周期分开记录：同637文件的macOS/Linux真实预检及最终采集器13项测试已完成独立复核；不等待整个复合条目才同步。预检只包含初版只读删除修复，最终异常保护有独立冻结测试，不能互相追认覆盖。

| 七类审查 | 结论与证据边界 |
|---|---|
| 目录、依赖、SDK与宿主 | 本子批仅验收脚本和观察工具；插件业务位于plugins/sync与plugins/port，host/SDK边界无变化。637源明确排除并行host候选。 |
| RPC、授权、版本和能力 | 预检实际执行IPC、TLS/H2、go-plugin与CoreAPI链路；不变更协议、capability或默认12小时凭据。 |
| 持久与运行态、原子性、修订和去重 | 原active/paused/conflict与持久配置断言保留；采集器只处理自有源码副本，不改产品schema或业务状态。 |
| 进程、取消、退出与恢复 | 两平台应用/collector均退出0，正常自有资源清理成立；最终collector在runner未退出或输入postcheck未完成时保留快照。live-runner保护仅静态核验。 |
| 文件、终端、网络与错误 | 原工作负载继续验证终端回复、文件收敛、端口数据；只给复制目录增加owner权限，不改文件或外部链接目标。不可读输入负控仍失败。 |
| CLI/TUI、配置、安装和产物 | wrapper编译并发固定p1且metadata一致；产品界面/安装不变。没有启动新的12小时或内存负载。 |
| 行为测试与验收声明 | 双平台各15轮预检、完整manifest一致和正常清理；最终13项Python测试1.328秒无skip；保留原红结果与异常失败，不声称自然轮换、失败归档分支或资源稳定通过。 |

报告：[整机预检](15-implementation-status.md#evidence-rotation-merged-preflight-validation)、[采集器清理](15-implementation-status.md#evidence-acceptance-cleanup-validation)。独立回执在`/tmp/shiki-resource-capacity-independent-20261029`，最终采集器源/manifest/log在`/tmp/shiki-collector-final-20261029`。此子批完成时宿主修复仍在验证、保持27/42；其后完成的宿主批次及当前29/42见文首，两个源码范围不合并追认。

## 本批完成复核：成功上传流资源释放（2026-10-29）

| 七类审查 | 结论与证据边界 |
|---|---|
| 目录、依赖、SDK/宿主 | 仅host connection与SDK-private coretransport两生产文件及对应两测试；插件业务仍在plugins/sync、plugins/port，无host回退，双平台架构检查通过。 |
| RPC、准入、授权、能力 | 精确client stream已消费唯一响应及终态后释放；能力/版本/权限不变，header/trailer与远端错误保留，真实生成UDS/h2和完整插件链验证。 |
| 持久/运行状态、原子性、修订、去重 | 仅取消注册所有权变化，无schema/配置/receipt/日志清空，不为降低内存删除语义状态。 |
| 进程、启停、取消、崩溃与恢复 | caller保持存活时注册归零；child取消不撤共享pool，server/bidi响应与半关闭不误关闭；两平台原取消race及链路/退出清理通过。 |
| 文件、终端、网络、限额与错误 | 上传成功/失败、文件插件、端口half-close、worker退役及终端语义均有受影响回归；不修改超时/限额、不重放数据。 |
| CLI/TUI、配置、安装与产物 | 产品界面/安装/布局未变；仅Linux验证shell单独冻结，native632与Linux632+工具范围明确。 |
| 行为、验收、支持声明 | byte-identical新测试red3/green、两平台各两完整包与六包命名race、scoped vet及16项VT捕获通过，无skip；初始fixture盲区、历史内存待验保留，29/42不变。 |

独立复核确认全部632产品输入、两生产差异与工具/日志hash、原生6PID/runtime和Linux正常退出/精确CID清理；未发现其他确认的生产偏差。报告：[所有权验证](15-implementation-status.md#evidence-resource-ownership-audit)；root回执位于`/tmp/shiki-resource-owner-root-review-20261029.json`和native terminal/Linux证据目录内`root-independent-review.json`，另有`/tmp/shiki-resource-capacity-independent-20261029`独立审查。root首次误按数组顺序比较map导出的进程记录，已改为按PID比对完整身份集合，无需重跑产品。

<a id="evidence-load-capacity-protocol"></a>

本批08/15/16同步闭环。普通构建负载工具的完整回复、已传载荷、实际DATA重叠、错误保留与受控worker代际审查继续，尚无正式容量结论；结果前协议不改变产品设计或资源稳定门槛。

## 本批完成复核：系统Tailscale非干扰与原生schema（2026-10-29）

对照00–14及七类要求完成审查；生产基线630文件保持，新增代码只在test/diagnostics和脚本工具，不更改模块、schema、协议或capability。独立报告`/tmp/shiki-coexist-native-schema-independent-20261029/REPORT.md`，根独立复核分别位于最终两轮证据目录的`root-independent-review.json`。

| 类别 | 实际核验与结论 |
|---|---|
| 目录/依赖与SDK-host所有权 | 插件仍在plugins、host/SDK依赖方向不变；native认证/进程探针与容器NodeControl探针仅为诊断工具，固定source与tool分开记录 |
| RPC/准入/版本/授权 | 原生30RPC证明候选撤销前可用、恢复前后持续远端拒绝且有效凭据正控成功；NodeControl探针漏声明共同能力已修，仅正确协商后接受SDK派生needs_login，不以任意offline冒充节点启动 |
| 状态/原子性/revision/去重 | native恢复有锁、备份hash、受限sync子树与fsync，host/auth/项目外部库存保持；容器已有系统state为{}，未推定已有账户身份保持，新增内嵌节点state与system分开且实际权限拒绝 |
| 进程/关闭/取消/恢复 | 容器21边界existing tailscaled原代际不变，真实poweroff0/精确CID清理；native最终B代际纳入记录，12PID/自有job/plist/session/runtime回收，无兜底；新native脚本受控信号取消未由本轮验收 |
| 文件/终端/网络语义 | 原生已知基线双向同步、冲突/暂停/KEEP_BOTH谱系/回执不丢；stationary pane跨client边界保持，不恢复文件或旧认证状态，不宣称任意新写后的旧备份安全 |
| CLI/TUI/配置/安装产物 | 两普通用户分别验证TLS全生命周期与tsnet未登录pending；real HOME/gui domain及自定义路径，原生标准job/plist/staging双预检，清理仅本次所有权；tailscaled版本真实后缀和同源码A/B标签范围明确 |
| 行为证据与声明 | native第四轮与容器第二轮实际通过，源/工具/上游module哈希和正常清理双重复核；失败、不完整轮和私有原状态删除后不可rehash范围保留；三文档同步，42原条与29勾选不变 |

具体发现并修正均为验收工具：本地Go工具链选择、初始shell dispatcher身份基线、最终B进程记录遗漏、NodeControl offer的Capabilities遗漏及安全失败诊断。没有修改设计来迁就测试，没有把runner自报passed替代完整证据审查。实际syscall EACCES替代原先硬编码的“未用系统socket”声明；2字节系统state明确为语义空状态。已有生产回归不重复运行，新场景用真实当前/开发旧产物验证；原Linux长跑继续保留原源码范围。

本批文档闭环凭据：`/tmp/shiki-coexist-native-schema-document-verification-20261029.json`；原42条要求与顺序保持、29项勾选及Phase计数保持、三份当前摘要一致。当前408份产品目录文件与go.mod/go.sum/Makefile共411份输入匹配已验630来源，新诊断工具独立记录；语法、格式、相对链接及diff检查完成。

## 本批完成复核：服务二次启动与恢复后撤权（2026-10-29）

本批生产来源固定为已验630文件，独立冻结新诊断工具。对照00–14进行七类复核，生产输入未变化；不是重复运行整个仓库测试。独立报告为`/tmp/shiki-service-recovery-independent-20261029/REPORT.md`，schema与第三轮二次启动均已通过独立复核，两条原失败均保留。本批闭环完成，不关闭外部/物理重启或资源门槛。

| 检查类别 | 结论与待完成内容 |
|---|---|
| 目录/依赖/所有权 | 插件业务仍位于plugins/sync与plugins/port，host/SDK边界不改；新增Go探针在test/diagnostics并由显式tag启用，不作为产品二进制 |
| RPC/准入/版本/授权 | 真实production TLS/Connect探针证明撤销前可用、撤销后及受控恢复后远端拒绝；固定endpoint及有效凭据正控排除网络失败冒充拒绝；协议/capability不变 |
| 持久/运行态/原子性/revision/去重 | 仅受控恢复sync子树，host/auth/credentials/project不回滚；保持mapping与回执；第三轮服务二次启动完整场景已验证状态保持及新数据面 |
| 进程/取消/崩溃/恢复 | schema正常poweroff与精确清理通过；第三轮二次启动实际观察新PID1与unit InvocationID、服务自动启动先于CLI，不能凭旧PID或复用内核宣称物理重启 |
| 文件/终端/网络语义 | 发现并修正夹具“无共同基线新路径自动上传”的错误预期，遵守文档10显式首次选向；保留原失败，不增加超时或修改生产来迁就测试 |
| CLI/TUI/配置/安装/产物 | 使用真实installer与普通用户服务、独立本地Claude/TCP替身；auto_start关闭、源/工具只读挂载；不操作host服务或原长跑 |
| 行为证据与验收声明 | schema 30次实际RPC与源/工具/退出/清理双重复核通过；二次启动第三轮完整通过、前两次失败不计通过；630源/8工具、两host四worker和两轮poweroff/精确CID与卷清理独立核验，原42条要求与29项勾选不变，外部/原生物理重启/正式发布门槛保留 |

已修正的工具偏差还包括归档保留校验后的真实权限、成功路径强制验证证据导出、清理用精确不存在结果加成功Docker清单双重核验。原失败的工具冻结副本保持不动。旧schema历史第三轮只有撤销记录观察，新30次RPC不能倒填为旧轮证据。原私有恢复状态随容器删除，事后复核范围限导出回执、摘要、冻结断言与进程/构建身份。第三轮真实验证已有基线自动双向和新路径显式首次决策，并验证配置快照只创建新会话、不回滚项目文件；内核boot ID保持真实值。报告见[二次启动](15-implementation-status.md#evidence-service-boot-validation)与[跨schema恢复](15-implementation-status.md#evidence-schema-service-upgrade-validation)。

## 本批完成：固定版本下载与Agent生命周期（2026-10-29）

显式HTTPS Fetch实现固定版本/平台、公钥独立输入、先验签后取归档、签署size/hash及私有receipt事务；CLI新增fetch而保留sign/verify。当前工具对此前608源94,476,199字节native归档实际下载、独立OpenSSL认证及真实helper client stage-only通过，私钥删除、无激活/残留helper；测试TLS根只在独立测试进程，不代表正式来源/身份或本批应用产物重新构建。

已确认并修复：迟到事件/Configure回复重建已删除映射缓存或覆盖新修订；PutMapping/Resolve/维护探测持宿主全局锁等待网络；成功转发预检在服务器/凭据变化或父请求取消后仍提交。事件/缓存短锁重验，网络与worker在全局状态/存储锁外，按server串行probe且等待可取消，结果核验pool指针和完整server record；mapping提交复核修订/身份/根重叠，历史请求结果不重新应用已过期配置。转发reconcile在锁内取得client快照，修复依据为静态所有权检查和受影响race，未伪称取得旧竞态red。单port插件forwardMu操作串行不等于全局状态锁，未做无依据重构。

共同源码`/tmp/shiki-fetch-mapping-final-20261029-wt1r8jvv/source`共630文件，manifest SHA256 `ba0e8bb7398541d6e0a8b225d14bafa0e5c0ddb4c4873bbb31abee444310862a`；540份执行输入与工作树一致，17个Go变化（10生产/7测试），proto/module不变。双平台各8完整包race、命名CLI、两条选定E2E与相关vet通过，无失败/skip/无测试包；native E2E63.110秒/Linux71.747秒，hostrotation218.494/209.369秒。E2E本轮子进程普通构建、parent race；hostrotation独立采集worker race日志，不能扩称全部子进程race。Linux普通UID1000无遗留进程，正常退出0、无OOM/强制清理，精确CID移除由root再inspect确认。原有效red与共享工作树中间态故障、TLS夹具/检查器错误分别保留，后者不充当产品red。连接初步green两例因维护探测准入失败，最终原子准入修复后通过；该初步raw路径曾被覆盖，只保留工具转录复原，明确不充当原始日志或通过证据，最终冻结日志完整。

七类全面复核覆盖目录/依赖、RPC/授权/能力、运行/持久状态与原子性/去重、进程/取消/恢复、文件终端网络语义、CLI配置/安装产物、行为证据/支持声明。已确认偏差同批修复；无新增协议/capability/schema，不放宽文档要求。独立审查与逐条命令/结果见[合并验收](15-implementation-status.md#evidence-fetch-mapping-validation)、[下载](15-implementation-status.md#evidence-release-fetch-validation)、[映射](15-implementation-status.md#evidence-mapping-event-lifetime-validation)、[转发](15-implementation-status.md#evidence-forward-preflight-validation)。08/15/16同步，原42要求逐字/顺序保持，恢复mapping项后29/42（8/11、7/7、5/6、6/6、3/6、0/6）。Linux原12小时、资源/正式发布及外部平台/账号门槛仍待，不将旧macOS长跑通过追认本批修复。

## 本批完成：节点授权生命周期与端口准入（2026-10-29）

确定性红证据确认节点在线授权lease保留client流成功、调用者取消及建流中取消的回调；修复仅解绑结束请求，不撤销共享授权，server/bidi每条响应和半关闭仍受epoch控制。真实HTTP2 generated upload与同连接活动terminal验证成功/错误/取消和隔离。旧TLS-only内存实验不执行该路径，不宣称历史斜率已解释。

端口List/Check/Watch已授权静默body在生成handler首Receive前无宿主门的缺口，经真实TLS/io.Pipe跨Token/连接红测复现。共享文件首帧工具提取后保留全部FILE错误/默认，新增port metadata32/watch32及5秒首请求预算，成功接收后解除短预算。worker原scans32把长Watch和短查询混用，也已由32个真实Watch挤掉List的独立插件链复现；现metadata/watch/ForwardPort三类独立，重复超时Stop共享一个实际完成信号，不累加waiter或报告假成功。ForwardPort配置上限/CONNECT/半关闭不改。私有AutoMTLS gRPC解码前预算不是本批新增保证，远端慢body在host先被限制，不把受信host故障窗口说成已解决。

节点整包最初共用45秒的多个验证目标耗尽预算失败；原生产授权代码同overlay/45秒也在后续重新授权阶段超时。单独原用例曾在39.08秒通过，原失败不追认为成功。现将四RPC形态、peer地址、授权epoch和移除peer拆成各自独立真实双节点fixture，每例仍45秒、三个原assert helper逐字不变，普通地址拒绝和EOF断言保留。最终node native race159.457秒/16顶层无skip，Linux101.468秒通过，vet通过；不更改任何生产deadline。mapping晚事件重新插入已删除缓存项只列静态候选，尚未复现或修改。

同620文件快照（manifest7b0fc491304c32a9fb1036a5baf135cf612ed209e117103712ea04bedb5e40a0）双平台各11个有测试包race及受影响vet通过。2无测试包和1opt-in资源skip单列；native文件Watch真实链5.668秒/Linux3.629秒，实际port子进程链均确认32条初始扫描事件后再断言限额/隔离/取消复用，并核验child race日志与PID回收。Linux正常退出0、无OOM/强制回收、UID1000无遗留子进程，精确CID移除已独立inspect。533份执行输入与工作树一致；本批17执行文件变化（8生产/9测试），不把上批诊断两文件重复计入，proto/module不变。

七类全架构复核覆盖目录/依赖与SDK-host-plugin所有权；RPC/并发/版本能力/授权；运行与持久状态/原子性/revision/去重；进程/取消/恢复；文件终端网络语义与限制；CLI/TUI配置/安装产物；测试证据与声明。计数/回调均运行态，不新增协议/capability或迁移；插件业务仍在plugins，跨进程用例位于test。未发现新增已确认设计偏差；新端口默认值作为实现策略写入14，不改规范为旧实现免责。独立复核报告`/tmp/shiki-node-port-independent-review-20261029/REPORT.md`，详细红绿/失败/共同源码/命令见[端口与节点验收](15-implementation-status.md#evidence-port-node-lifetime-validation)和[节点授权记录](15-implementation-status.md#evidence-node-authorization-lifetime-validation)。08/15/16同步，原42条保持，恢复Phase0勾选后29/42。

等待期间同时取得原macOS12小时自然恢复进展：02:07:08四角色已恢复，每worker仅替换一次，旧4PID/CoreAPI socket已消失，新PID路径经规范化核验。server-port的不可用采样为false，保留真实值、不要求所有角色同时观察到瞬态。恢复尾部至少运行到02:37:08，最终退出/清理仍待；Linux原长跑尚未自然到期。本次只读证据在`/tmp/shiki-default-native-recovery-observation-20261029-_79traj8`，第一版观察器把/tmp与/private/tmp原始字符串直接比较导致失败，规范化后复核通过，不是长跑失败或改变测试断言。两侧旧源码不覆盖本批修复。

## 本批完成：同源签名构建与诊断回收（2026-10-29）

同一608文件快照分别在原生macOS arm64和普通UID1000 Linux arm64容器执行完整`SHIKI_VERSION=v0.0.0-local.20261029 SHIKI_SIGNING_KEY=... make release`。六个可执行文件与双插件清单来自当前冻结源码；可信源码verifier使用独立公钥、固定版本/目标认证，解包仅用认证私有副本。macOS客户端与Linux客户端/daemon真实helper仅暂存通过，无服务激活或ready声明。Linux构建108.485秒、21命令全0、236项断言通过；native1320项断言包含两份608源清单校验，不冒充1320个行为测试。独立OpenSSL与产物复核native101项/Linux138项通过，私钥已删除；Linux普通用户无遗留子进程，容器正常退出0、无OOM/强制回收，精确CID已移除。归档/清单/命令证据见[当前源码发布构建](15-implementation-status.md#evidence-current-release-validation)。

另一条工作确认诊断持有`r.Close`会保留已关闭Runtime：真实daemon正对照保留绑定方法并GC，退出helper后weak清空与AddCleanup通知成功；诊断旧回调保留在同一测试下失败，成功关闭后释放回调则通过。原生红3.621秒、绿0.693秒、race1.726秒及tagged vet通过，无skip；初次port0夹具错误另列，不计行为红。失败关闭仍保留回调供defer重试。仅修改test/diagnostics，生产Runtime/协议/schema/capability不变；没有新负载或配对实验，也不把旧残余差值和运行中的增长认定为已解决。新诊断回归未声称Linux已运行。见[所有权验证](15-implementation-status.md#evidence-memory-closed-ownership)。

七类架构独立复核覆盖：目录/SDK-host-plugin依赖；RPC准入、版本能力与授权；持久/运行态、原子性、revision与去重；进程/启动关闭/取消恢复；文件终端TCP语义与限制；CLI/TUI配置及安装产物；行为证据与文档声明。全类别结合关键实现抽查未发现新增确定性偏差，不声称逐行证明整个仓库。独立报告位于`/tmp/shiki-release-independent-review-20261029/REPORT.md`。原插件目录与进程边界保持；release工具不成为新宿主服务，验签不自动执行或建立信任根，暂存不修改活动安装。三个构建源副本608文件逐份匹配，262份生产/构建输入与当前工作树一致；并行诊断修改另行记录，未混入已冻结发布源码。

08/15/16同步，原42条要求与顺序保留，当前29项已勾选/13项待验；正式身份/公钥分发撤销/真实安装源、外部平台与资源门槛保持。两条原12小时实验继续使用各自旧快照；本批签名构建或诊断结果不能外推到原长跑，也不能由原长跑追认覆盖本批改动。

## 本批完成复核：文件RPC资源准入（2026-10-29）

静态核验与行为红证据确认：宿主无跨连接文件并发门；worker上传首header在active登记之前，Stop可能先报告成功；SDK外层在impl.Stop超时后仍标记stopped，使重试假成功。前两项由宿主/worker独立修复，第三项由独立审查发现并同批修正。新默认策略为metadata32、transfer16、watch32及首帧5秒；按单OS用户Service共享，授权先行、类别独立，不把更换连接/Token当新预算。

代码在internal/server、plugins/sync/server、pkg/plugin及test对应层；无插件业务导入host内部、无host进程内业务回退，跨进程用例在test/integration。SDK传输回调增加context-aware FileWorkerService，本地FileService保持原形状；wire/proto/capability/schema不变。计数和reader状态不持久化；原revision/去重/未知ACK恢复不改。实际gRPC handler返回后释放阻塞Recv，不能宣称在handler内先join；reader取消终态锁存并且不允许新context恢复读。Stop失败保留stopping，worker与SDK都重试实际等待。

宿主真实HTTP2首帧5秒、早父截止、取消、三类别饱和、跨Handler/Token/connection、权限先行和terminal/metadata隔离专项通过；补测异常/panic释放、首Receive成功清deadline、真实Watch超过5秒继续事件亦通过。早期SendHeaders夹具并未保持请求body开放，已排除该无效“红”，新io.Pipe夹具在旧接线冻结副本真实复现；root真实sync子进程32watch初始RESCAN确认后的第33项旧实现成功、修后拒绝，fixture权限/代理调用错误原记录保留，不改代理白名单。

全架构七类独立审查及最终冻结复审已完成；522份执行输入在macOS/Linux和工作树相同，较上一批仅16份变化（9生产/7测试），proto/module不变。双平台各10包race、vet通过；2无测试包和1opt-in资源skip不是通过。原生memoryprofile标签编译通过（无测试执行），Linux608文件冻结哈希/正常退出/精确容器清理通过。实际证据补齐后恢复Phase0勾选，当前29/42。14补齐默认策略和范围，04说明SDK源码适配；不宣称服务端新增整次按size强制deadline。离线内存所有权分析为另一组旧证据：具体样本关联runtime缓存与诊断Close方法值持有，不能解释全部残余或把本批限额当泄漏修复。详细报告见[文件准入](15-implementation-status.md#evidence-file-admission-validation)与[关闭后所有权](15-implementation-status.md#evidence-memory-closed-ownership)。

本批收尾凭据：`/tmp/shiki-file-admission-document-verification-20261029.json`，原42条要求逐字/顺序一致，29项勾选及Phase计数8/11、7/7、5/6、6/6、3/6、0/6一致；三份当前摘要逐字相同，169个所检查相对链接存在，522份执行输入在工作树/原生/Linux一致，git diff --check及Go格式检查通过。首次文档核验的Docker错误文案大小写断言误判已保留记录，改用规范化匹配并独立再次inspect精确CID确认删除，不改写实际容器正常退出结果。01:18原macOS检查点3773轮/11小时10分、Linux1430轮/4小时4分，均recovered=false；原runner/句柄及Linux长跑CID仍活跃，无OOM/restarting。默认到期/恢复尚未通过，等待期间已完成本批独立工作。

## 上一批全面架构复核：离线发布认证与同步期限（2026-10-29）

对照00–14与AGENTS逐类复核；独立审查未发现尚未修正的新增确定性偏差。完整执行源码变化限于本批两条工作，go.mod/go.sum和四份proto与原Linux长跑冻结输入相同。插件入口/业务仍位于plugins/sync、plugins/port；宿主internal/plugin、公共SDK pkg/plugin；跨进程测试保留在test下，无宿主内嵌插件回退。

| 类别 | 本批核验及修正 |
|---|---|
| 目录与依赖 | releaseauth是internal离线工具，脚本只做命令接线；deadline在SDK私有传输与sync业务，架构race通过 |
| RPC/准入/权限/能力 | 短unary默认5秒；文件预算按大小并保留更早父截止；Connect/gRPC边界、UID/scope/实例授权、capability与默认TTL保持 |
| 状态/原子性/revision/去重 | 超时后未知提交重观测，无重复写/盲目删除；持久schema不变；release输出不覆盖，认证同流复制且receipt最后写 |
| 进程/取消/恢复 | 单次RPC计时器随完整响应/错误/close回收，不限制Watch/终端/端口长期流；既有worker/tmux归属保持，验证器不启动服务 |
| 文件/终端/网络 | 首帧/中途/最终ACK均覆盖文件期限；原revision/hash/条件提交与TCP半关闭保持；严格PEM防跳过损坏首块，定界元数据/归档拒绝特殊文件 |
| CLI/TUI/配置/安装 | 默认本地未签名包，显式外置私钥追加签名；独立公钥和固定目标验证成功才生成私有副本；真实helper仅stage，manifest/plugin校验仍执行 |
| 证据与文档 | 红绿、真实socket、虚拟时钟、现有developer binary暂存和当前工具源码分别记录；未以本地签名关闭正式身份/下载源/分发或长稳门槛 |

审查发现并关闭：PEM解码跳过首个损坏块、打包校验sidecar覆盖/符号链接写、失败清理使用可变路径递归删除的风险、CLI输出失败与已完成事务的文档歧义，以及18“不会签名”的过时说明。所有清理仅处理已识别的本次文件，不宣称敌手可写父目录下的原子CAS；异常替换可以保留不完整证据。真实归档验收经独立标准库Ed25519与102项断言复核；原版本/平台错误用例只到名称校验，已追加规范名称别名并实际走到签名元数据不匹配拒绝，原记录保留。

07/11补齐目录/信任边界，14补充当前大小预算公式，18/脚本说明同步；AGENTS落实用户要求的长跑期间持续推进独立任务、冻结源码和低并发约束。两轮12小时原运行不重启，各自旧源码不替代本批检查。详细结果见[截止时间](15-implementation-status.md#evidence-file-deadline-validation)与[离线签名](15-implementation-status.md#evidence-release-trust-validation)。

本批收尾实核：原42条要求文字/顺序与HEAD逐字相同，29项勾选及Phase计数8/11、7/7、5/6、6/6、3/6、0/6一致；三份当前摘要逐字相同，194个相对链接存在，512份执行输入与最终Linux冻结源相同，git diff --check通过。凭据：`/tmp/shiki-sign-deadline-document-verification-20261029.json`。Linux补验exact CID已再查删除，退出0且未强制清理；00:32最新原macOS3516轮/10小时25分、Linux1165轮/3小时18分，均recovered=false，原runner/CID存活且无OOM/restarting。此观测不算自然到期验收。

## 当前复核状态

当前 **31/42项已勾选、11项待完成**；Phase 0–5分别为 **9/11、7/7、5/6、6/6、3/6、1/6**。首个全自动alpha及公开下载认证完成，新增关闭工程/实际发行地址和发布材料/支持入口两项。sync client停止超时重试清理缺口已修复，同测红绿及同源双平台相关包/实际插件链通过，插件生命周期项恢复。节点只读状态诊断/UX偏差已修复，有效同测红绿、双平台相关四包race/vet及.4实际包内验收通过，相关两项恢复勾选。宿主回调清理与晚启动回收修复已完成同源双平台相关验证和七类复核，两项恢复勾选。转发限额隔离缺陷已完成修复、双平台受影响与普通完整负载验证，恢复勾选。原42条要求不变。上一批sync日志、迁移栅栏、pending删除准入与SDK状态目录修复已完成同源双平台验收和独立证据核验，恢复共同基线条目勾选；终端语义项保持已勾选。首个公开预发布已完成；完整Phase 0–5、无泄漏、外部部署和稳定版交付仍未完成。

剩余11项已在[08路线图](./08-roadmap.md#剩余11项本地工作与外部门槛)逐项区分可继续本地推进的工作与外部门槛。已通过的本地回归不替代外部验收；已停止的短窗口内存实验不再追加，独立实现、已确认缺口的验收和文档审查继续并行推进。以下旧批次证据保留当时的源码与时间范围，当前工作以文首并行表和最新批次为准。

- **本批观测开销调查已完成并停止。** 只有8份诊断输入变化。两次短预检默认60/延后6个profiles均符合策略；唯一Linux正式配对的同源码/二进制/计划、136/128原始文件、各68份JSON及6关闭后profiles、工作量和清理独立核验通过。Agent末段heap差值−260,788 B/min受idle3高点回落支配，对象差值+9.98/min；关闭后+24,240 B/+228对象。所有数据保留，不能推定无泄漏；不追加该短窗口实验，见[报告](15-implementation-status.md#evidence-memory-observer-validation)。

- **上一批请求生命周期修复与回归收尾。** 确定性红绿、真实TLS多流、并发取消和半关闭专项通过；Linux三组普通构建原始归档334/334/326份及清理已独立核验。macOS完整46包通过（E2E218.155秒、hostrotation225.822秒）；Linux串行完整46包通过（E2E217.938秒、hostrotation226.082秒）。两侧各15包无测试，6/10项skip不是通过。最终Linux测试源只比macOS完整源多一处syncack启动探测预算修正，原生四场景race23.050秒补验；生产输入相同，make lint通过。两侧32份终端捕获独立重放及限定夹具/容器清理通过。并发Linux原运行的插件构建signal:killed、冲突恢复超时及先前lint失败完整保留；未证明构建被杀或原超时的唯一原因。见[报告](15-implementation-status.md#evidence-pool-request-lifetime-validation)。
- **跨schema服务验收完成。** A529/B569实际开发产物在Linux systemd完成1→2→旧程序拒写2→停写后仅恢复sync子树1→再次升级2并同步新文件。第三次正常poweroff退出0且精确CID消失；前两次夹具失败保留。root和独立review重核1098份A/B源码、6份工具及导出回执。保留撤销记录，不声称撤销Token实际RPC拒绝；敏感原状态未导出，删除容器后只复核留存摘要/回执与冻结断言。只覆盖B无新业务修改的受控恢复，不是自动产品恢复或历史正式发行兼容，见[报告](15-implementation-status.md#evidence-schema-service-upgrade-validation)。
- **上一批资源与时长观测（历史）。** 修后Agent晚期load−control仍+15,597 B/min、+92.35 objects/min；保留端口缓冲峰及空载daemon峰。原12小时实验九PID仍存活，20:21:02 checkpoint为2103轮、6小时13分52.778秒、recovered=false；原495文件版本不覆盖新修复。Linux实际12小时尚未开始。

### 上一批已完成证据（历史版本）

- **终端语义与接管尺寸已完成。** 549文件冻结快照在macOS/Linux通过（2.27/0.84秒），各16份真实前端字节捕获独立重放一致，验证宽字单元、颜色、光标、alternate/main屏恢复、重连与接管后的当前尺寸；进程/容器清理通过。见[终端语义报告](15-implementation-status.md#evidence-terminal-state-validation)。真实Claude账号及完整物理终端体验仍是独立门槛。
- **历史545文件内存对照已完成，当时增长未归因。** 545文件冻结快照的双平台四组串行运行完成，同平台使用同一二进制/源码；Agent后段load−control heap斜率分别为62,281/65,347 B/min。单对实验不能判断因果或认定无泄漏，且该快照早于本批sync日志修复。见[配对诊断](15-implementation-status.md#evidence-memory-idle-control-diagnostic)。
- **sync与SDK修复已通过最终双平台验收。** prepared/committed、末块前提交可能性、未知结果冲突保留、schema1迁移持久栅栏、固定staging/SDK状态目录FD及未来schema字段拒绝已实现。pending状态按冲突/操作/迁移路径并集计数；host删除准入核对真实paused、revision与pending，致命持久化失败后的状态调用在锁后复查lifetime。SDK原生state/storage/auth race（2.396/1.332/4.283秒）及Linux五个fsync点/新进程恢复、两个portable与两个ENOSPC分支通过（18.94秒，无跳过），root已独立核验。见[sync日志](15-implementation-status.md#evidence-sync-journal-validation)和[状态目录](15-implementation-status.md#evidence-state-directory-validation)。
- **v3最终合并已通过。** 568文件/457份Go/proto/module使用同一源码manifest（SHA-256 `4499d601da33214dfbefc7f89c5566a6788f1a3ab0f445d805423da0c702bbbf`）；macOS/Linux均46包通过、15包无测试，显式skip分别6/10项，跳过不算通过。E2E204.984/192.045秒、hostrotation220.483/215.485秒、syncack45.395/34.394秒。两侧application/collector均退出0；root独立重放32份终端捕获一致，核验原生终端夹具六PID/runtime无残留及精确Linux容器消失，未将该清点扩大为全仓库进程独立清点。make build lint通过，最后夹具修改后的make lint及E2E vet亦通过；上一批root核验当时491份执行源（Go/proto/module/Python/shell）与v3逐份hash一致，凭据为final3下的`root-current-source-verification.json`。详见[合并验收](15-implementation-status.md#evidence-journal-conformance-validation)。
- **历史合并失败完整保留。** v1路由夹具失败及pycache导致collector失败分别记录；v2核心E2E只等磁盘删除就提前pause/unmap，正确触发PENDING_SYNC拒绝。仅修夹具等待真实active、pending=0、conflict=0，focused37.033秒通过，未绕过归档保护；随后v3整套成功不追改v1/v2为通过。
- **2026-10-28默认12小时中期观测（历史，非当前运行状态）。** 当时macOS原runner82454/句柄11096未重启；root于19:18:15（Asia/Shanghai）核验原runner/test82622/两host/四worker/pane共九PID存活。最新checkpoint为19:17:18、1744轮、5小时10分9.0735秒、recovered=false；最早10月29日02:07:04到期后还需至少30分钟尾部。495文件/411份源码早于本批修改，不覆盖新修复；Linux实际12小时尚未开始。该中途观测不计自然到期通过；后续原macOS已完成、原Linux失败及新697源Linux运行另见文首最新进展。见[中途观测](15-implementation-status.md#evidence-default-rotation-observation)。

本批请求生命周期与跨schema服务验收及文档同步已收尾；下一批只推进明确剩余条件，实际长时运行和残余资源增长仍待完成。外部账号、历史发行、物理多设备、原生平台、重启/登录持久化和正式发布门槛分别保留。每批须全面复核并同步08/15/16，跳过不算通过，未验能力不得宣称支持。

## 只读中期观测与报告校正

本次不运行新实验，不修改生产、诊断或验收工具。相对Linux长跑冻结清单，499份执行输入（Go/proto/全部module/Python/shell/Makefile）逐份不变且无新增/删除，其中Go/proto/module为458份；216份生产Go import静态检查未发现插件引用host internal或宿主引用插件业务。按00–14及AGENTS复核目录/依赖/SDK归属、RPC/准入/授权/能力、状态/原子性/revision/去重、进程/取消/恢复、文件/终端/网络、CLI/TUI/安装及行为证据边界；原有实现与上批验收范围保持，未发现新的确证生产偏差。该只读检查不代替尚未完成的长稳或外部验收。

发现并立即更正POOL-REQUEST-LIFETIME-VALIDATION文首“完整回归待完成”的过时摘要；后部实际完整回归、原生夹具补验、不同测试快照、skip及历史失败不变。08/15/16加入同一只读观测段，原42条要求和29项勾选保持。当前中期观测不作为新通过条目，不弱化设计或资源门槛。

保存macOS459、Linux38个完整检查点及八角色原始序列；独立从prefix复算CSV逐行、首末/全程极值、末30分钟极值/OLS/CPU差速率及逐小时中位数一致，前缀与inventory哈希一致。root另外核验原生九PID、Docker客户端、精确容器与容器八PID存活，容器OOM/restarting均false。证据为`/var/folders/1n/q373cln10fqb0gl9z1ds5d8m0000gn/T/shiki-existing-rotation-observation-dm095nlk`，含完整前缀、算法、统计与live-verification；历史142检查点归档保留。源码版本/时长不同，RSS不推定保留堆或因果，默认12小时、恢复尾部与清理仍待完成。文档校验完成后继续原句柄，不追加短资源实验或无依据整套回归。

## 默认时长并行调度与启动复核

新增Linux长跑已实际启动，精确CID、只读mount与PID/network/IPC/cgroup隔离已检查；独立临时状态/UDS/随机端口及容器tmux不触及原macOS。外层信号/清理只作用于新进程组和记录CID，restart=no，不自动替换运行。实际启动前VM余量约2.66GiB；脚本仅为Go构建追加-p=2并记录metadata，不是CPU/内存硬配额，也不进入显式产品env。构建、资源短实验已结束，后续避免并行重负载。

578份冻结文件逐份核验，458份Go/proto/全部module输入不变；七类目录/依赖、RPC授权/能力、状态、进程、文件/终端/网络、CLI/TUI/安装及证据边界沿用原实现复核，无新产品能力或规范修改。shell语法与diff检查通过。root实查Linux八进程身份与五个live executable哈希、四初始epoch及首个资源检查点；独立防休眠只绑定新Docker PID96513。以上证明启动与隔离，不证明12小时到期、恢复、尾部或清理。原macOS九PID存活，21:20:57为2439轮/7小时13分48.182秒、recovered=false。

原串行是执行调度计划，现经资源/隔离证据调整为并行；TTL、时钟、固定判据与原macOS进程未改。各平台不同源码必须分别验收。08/15/16同步运行句柄和剩余门槛，42条要求与29/42计数保持。

## 上一批复核：诊断隔离与测量边界

本批对照00–14复核七类边界。222份生产输入（216个Go、4个proto、根go.mod/go.sum）逐份不变；全部458份Go/proto/module输入中454份不变，仅4份memoryprofile标签测试变化；另4个诊断脚本变化。生产目录/依赖、RPC准入/授权/capability、持久化/revision/去重、进程和取消、文件/终端/网络语义、CLI/TUI/安装均沿用上批实际输入与审查证据，插件依赖静态扫描无违规。未修改规范、产品接口/默认值、安全配置或原12小时工具。新增policy仅存在于tagged诊断及私有worker wrapper，未知值在fixture启动前拒绝。

默认与延后导出的同binary短预检、tagged vet/脚本静态检查通过；正式pair核验六进程策略、双GC、全部窗口、固定工作量、raw hash和精确清理。四个worker在退出前保留JSON，但没有closed profiles；只有两个host在closed.AfterGC后导出，不能宣称四worker也有关闭后profile。本次未修改生产，未无故重跑完整产品回归。原/新pair的诊断源码和二进制不同，各自条件内部匹配；短对照不支持普遍因果或无泄漏结论。生产生命周期静态复核没有发现第二个已证保留原因。

本批实验已按预声明停止，29/42和Phase计数保持，原42条要求不变，08/15/16当前摘要同步。 root收尾核验492份已有执行输入与本次快照一致、191个相对链接存在、三份摘要逐字相同，git diff --check通过；凭据为`/tmp/shiki-observer-batch-document-verification-20261028.json`。原12小时九PID仍存活，最新20:59:38为2319轮/6小时52分28.812秒，未到期；不能将等待或短诊断记作长稳通过。

## 上一批全面复核与收尾

按00–14及AGENTS逐类核对：201个生产Go文件依赖扫描违规为零，全部458份Go/proto/module输入与最终冻结源一致；插件入口/业务位于plugins/sync、plugins/port，SDK/宿主边界与test下跨进程测试保持。RPC/HTTP2/UID/实例/授权与capability不变，默认TTL不变；请求取消仅释放本请求，客户端流在完整终态后清理，server/bidi半关闭不撤共享池。固定目录FD、原子性、schema拒绝、revision/去重、sync未知提交与pending删除保护均保留。终端控制权/代际、TCP半关闭/限额/固定目标、CLI类型化IPC和独立插件产物符合文档。

本批夹具修复包括并发测试失败路径defer cancel，以及将syncack启动探测的1秒预算限制在探测context、后续RPC恢复文档默认；未改变生产预算或放宽断言。原生完整回归、原生受影响四场景补验、Linux最终串行完整回归和lint通过；旧Linux两包失败保留，资源争用是可能因素而非已证唯一原因。32份终端捕获独立重放，精确夹具和Linux容器清理验证；不外推全仓库进程清点或无泄漏。

跨schema服务真实运行及独立复核通过。有限恢复仅在停写后针对sync子树，保留当时schema2证据，不改host/auth/项目文件；撤销表记录保留，不声称额外RPC实拒。敏感原状态未导出，容器删除后的独立验证仅覆盖留存摘要/回执、冻结断言、身份与清理。没有新增产品恢复命令或能力宣称。原失败、不同快照和外部门槛均分列。

文档收尾核验：原42条要求文字/顺序不变，29/42及Phase计数8/11、7/7、5/6、6/6、3/6、0/6一致；08/15/16同批更新，剩余13项区分本地任务与外部门槛。默认12小时原进程持续运行，残余资源增长不计验收完成。 root收尾实核181个相对链接均存在、三份当前摘要逐字一致，git diff --check通过；凭据为`/tmp/shiki-pool-batch-document-verification-20261028.json`。

## 上一批收尾：进度同步与全面复核

发现的记录偏差是：专项结果已完成，却只追加到后文或独立报告；顶部和08条目仍保留“终端语义待验”，并重复上一批计数。本次将已完成、修复中和外部门槛分别写回原条目，保留历史失败，不修改00–14或42条要求。

| 复核类别 | 本批已确认与待完成 |
|---|---|
| 目录与依赖 | 插件业务保持plugins/sync、plugins/port；宿主internal/plugin、公共SDK pkg/plugin；跨进程测试在test下，无进程内回退 |
| RPC、准入与版本 | 不新增协议/capability或改变默认TTL；真实worker未来schema初始化失败须保持failed且不广告Protocol；SDK未来字段拒绝已修；v3同源双平台验收及旧写入方拒绝通过 |
| 持久化、revision与去重 | 新日志保留未知提交证据；确定未发完整输入才可重新观察后条件重试；旧schema迁移恢复栅栏及最终双平台ACK丢失/回退矩阵通过 |
| 进程与恢复 | 致命持久化失败取消writer并报告UNHEALTHY；归档不得复用同ID丢失阶段文件；host删除要求真实paused/revision及pending并集，锁后复核fatal lifetime；最终双平台轮换及pending删除准入回归通过 |
| 文件、终端与网络 | 阶段文件固定目录FD；终端549语义验收完成；内存545对照核验实际文件/TCP工作量，不能外推新源码无泄漏 |
| CLI/TUI、配置与安装 | 未增加用户选项或未验支持声明；历史安装529证据保留；sync ledger schema与核心安装schema分别管理 |
| 行为证据与文档 | 当前29/42；v3双平台各46包通过、6/10项skip不计通过；开发专项、最终冻结验收、原12小时运行及外部门槛分列 |

本次收尾核验：原42条要求与HEAD逐字、顺序一致；实计29/42，Phase计数为8/11、7/7、5/6、6/6、3/6、0/6；08/15/16当前状态一致，08/15/16/17/18相对文件链接全部存在，限定这五份的git diff --check通过。当前architecture包双平台1.357/1.200秒通过；32份终端语义捕获独立重放一致。原v1/v2失败、历史Stats因果限制、memory545与默认12小时495的源码边界保留，不因本次功能回归通过宣称资源稳定或外部发布完成。

## 历史批次：安装、在途轮换与五窗口诊断

以下保留安装529/在途530/内存527批次的原记录；其“本批”“待验”和计数仅表示当时状态，不覆盖上方当前结论。

上一批 **28/42项已勾选、14项待完成**；Phase 0–5分别为 **8/11、6/7、5/6、6/6、3/6、0/6**。原42条要求保持不变。此次同步补齐本批已完成工作，复合条目仍按全部条件验收；终端项因03规定的语义屏幕验收缺证暂撤勾选，不能仅凭marker/任意ANSI输出认定光标、颜色、alternate-screen和resize重连状态正确。

- **安装磁盘预检与备份修复完成。** 停旧服务前按真实目标文件系统估算staging/备份，停止后重新取有界清单和空间检查；固定目录FD，防备份包含自身，并补齐tar→backups目录→prefix的fsync。双平台install/helper整包race通过，Linux普通用户16MiB tmpfs九项无跳过，strace确认真实fsync顺序。529文件快照实际LaunchAgent上传活动升级/回滚90.41秒及真实systemd的13次生命周期边界通过。见[安装验收](15-implementation-status.md#evidence-install-space-validation)。
- **在途文件自然轮换矩阵完成。** 530文件快照的client/server×upload/download在macOS/Linux八格均通过，原两场景也重跑通过；hostrotation整包220.396/211.039秒，架构检查通过，无失败/跳过。8MiB传输在真实部分内容阶段到期，验证旧公开目标、临时文件清理、基线、恢复及原stats流；内部30秒TTL、测试内宿主和真实worker子进程，不代替默认12小时或提交后最终ACK丢失验收。20ms采样是离散观察；最后仅修改一处代理header说明注释，未改执行语句。见[在途轮换报告](15-implementation-status.md#evidence-host-rotation-inflight-validation)。
- **五窗口普通构建内存诊断完成，泄漏门槛未通过。** 同一527文件快照双平台319.58/320.33秒，各80次文件更新、160次TCP及等时空闲、334份原始归档核验和清理通过。Agent最后三个空闲点的post-GC heap仍约增加76,326/68,164 B/min；采样本身和维护流量影响尚未分离，不能宣布稳定或无泄漏。当时待做的相同11个采样边界空载对照现已完成，见[545文件配对报告](15-implementation-status.md#evidence-memory-idle-control-diagnostic)，归因仍未完成。见[多窗口诊断](15-implementation-status.md#evidence-memory-windows-diagnostic)。

本批完成全架构类别复核，插件入口/业务仍在plugins/sync、plugins/port，宿主internal/plugin与公共pkg/plugin依赖隔离；未增加协议、capability、默认TTL或发行承诺。最终make build lint通过。上述为专项、真实服务和诊断证据；上一批507文件完整race/E2E各44包通过（226.898/203.361秒）仅适用于该历史快照，不算本批全仓库复验。原502文件WatchStats失败因果仍未确认，505文件Linux失败及所有跳过均保留，见[历史合并验收](15-implementation-status.md#evidence-conformance-regression-validation)。

原macOS默认12小时实验继续原runner82454/句柄11096，未重启：2026-10-28 17:18:47（Asia/Shanghai）检查点为3小时11分38秒、1080轮、recovered=false，原宿主/四worker/pane仍存活。最早10月29日02:07:04到期后还需至少30分钟恢复尾部；原495文件/411份源码早于本批修改，结果只适用于原版本。Linux实际12小时待原实验退出清理后串行执行。[先前中途观测](15-implementation-status.md#evidence-default-rotation-observation)不计自然到期或内存验收。

剩余本地验收包括终端语义状态、服务端提交后丢失最终ACK时真实worker重观察及远端再次编辑保护、内存增长归因和实际长时运行。外部账号、历史发行、物理多设备、原生平台、重启/登录持久化及正式发布仍分别待验；每批须全面对照文档并同步08/15/16后才能收尾。

以下其余章节继续保留历史实现与批次过程；“当前”“运行中”及计数仅适用于各记录，最新状态以本文顶部和08为准。

## 历史批次全面复核与进度同步修正（2026-10-28）

本批此前已有实现/报告结果，但08/15/16仍将专项标为“正在验证”，顶部还混用上一批“当前”结论；这是执行与记录遗漏。现同步条目下部分进度、28/42计数和证据边界，保留原42条验收要求，不通过改规范使现状合理化。

| 复核类别 | 本批结论与处理 |
|---|---|
| 目录、依赖、SDK/宿主 | plugins业务与internal宿主隔离不变；跨进程测试在test下，架构测试通过 |
| RPC、准入、版本、授权 | 不新增协议/capability；在途夹具验证TLS身份与HTTP/2，连接级流控调整仅在私有测试relay；不修改生产取消语义 |
| 持久/运行态、原子性、revision/去重 | 安装预检和停止后备份重新盘点；修复真实backups父目录缺失fsync，观察到tar→backups→prefix成功顺序；不声称抵御任意并发写入/掉电 |
| 进程归属、启停、取消/恢复 | 新安装源码真实LaunchAgent及systemd活动升级/回滚通过；旧worker/socket和独占容器清理核验；原12小时进程未动 |
| 文件、终端、TCP语义/限额 | 在途轮换核验部分文件清理、旧目标及恢复基线；内存实验核验TCP半关闭/精确载荷；03终端语义和06提交后ACK丢失仍缺专项证据 |
| CLI/TUI、配置、安装/产物 | 无新增选项/默认值；11空间预检缺口已修，真实空间不足在停止前拒绝；外部登录/签名渠道继续未支持 |
| 行为验收、性能与安全边界 | scoped race、真实服务、诊断各按其快照报告；内存增长保留待归因，跳过不算通过，未称本批重新跑全仓库 |
| 文档与接受标准 | 08/15/16同批同步，17/18和脚本索引更新；终端项暂撤勾选，安装/长稳复合条目仍未勾选 |

在途测试开发中的提前stats取消已定位为私有relay的HTTP/2连接窗口被阻塞文件占满：Negotiate探测请求声明240字节却5秒未转发任何body，触发既有失败策略。仅将测试连接接收窗口调为8MiB，单流保持1MiB；旧日志保留。这不是生产取消缺陷的证据，也不能解释原502文件WatchStats事件。源码冻结后只修正ReverseProxy处理transport header的说明注释，报告已披露。

详见[安装修复](15-implementation-status.md#evidence-install-space-validation)、[在途轮换](15-implementation-status.md#evidence-host-rotation-inflight-validation)、[多窗口诊断](15-implementation-status.md#evidence-memory-windows-diagnostic)。本地剩余缺口按顶部继续安排，不降低03/06/10/11要求。

收尾核验：与HEAD逐字比较，原42条要求完整保留；复选框实计28/42、各Phase计数与08/15/16一致；相关七份文档相对链接全部存在，git diff --check通过。当前工作树test/architecture再次通过（0.394秒），go.mod/go.sum与529文件实际服务快照逐字节一致。

## 上一批进度记录偏差与修正

发现08、15、16的顶部连续堆叠历史批次，导致28/42与“29/42不变”、探测“已修复”与“尚未完成”、已退出测试与“运行中”并存。这违反同批同步要求。本轮合并当前摘要，将历史证据明确限定为对应快照，逐项登记新失败并同步撤勾；不改42条原要求，也不修改设计契约来迁就实现。本轮独立八类架构复核及协议/依赖检查通过（0.293秒）；随后只读补审tmux/session修复未发现新增偏差。两项失败已有修复、双平台专项及最终507文件完整race/E2E证据；三份进度文档同步并核对原要求、勾选数量和链接后，本批收尾。

## 检查范围

每批检查目录和依赖方向、SDK/宿主归属、RPC 与 capability、授权、持久状态与运行态、事务/版本/幂等、进程生命周期与取消、文件/终端/TCP 语义、配置默认值、CLI/TUI、安装与产物。缺失功能必须保持未实现，不能出现在能力声明中；跳过的测试不算通过。

## 本批发现及修复

| 文档基准 | 发现的偏差 | 修复与验证位置 |
|---|---|---|
| 04/06/09 插件退役准入 | grant到期后的Stop窗口仍可能广告ready/Protocol，current仍可取到失效实例 | List/current共用实时有效性检查；保留持久enabled/revision；真实阻塞Stop与Done回归先红后绿，双平台完整插件race各3轮通过，见PLUGIN-READINESS-VALIDATION |
| 06/14 轮换验收证据 | 旧测试只在handler defer采样ctx.Err，无法完整观察之后的真实取消 | 独立Context.Done观察并join handler退出；严格拒绝提前返回/普通请求cleanup，最终双平台整包通过；原Linux全套失败和原因未定保留，见ROTATION-VALIDATION |
| 07 插件目录和依赖 | 插件业务曾放在宿主 internal，入口放在 cmd；SDK 间接依赖宿主 | 入口和业务迁移到 plugins/sync、plugins/port；宿主保留 internal/plugin；SDK 共用 state/config/limits/buildinfo，CoreAPI 私有 UDS transport 位于 pkg/plugin/internal；test/architecture 检查生产依赖与旧目录缺失 |
| 09 双端 capability | 客户端 worker 与服务端数据能力曾混用 | client 使用 sync.worker.v1/port.worker.v1，server 使用 sync.revision.v1/port.tcp-half-close.v1，CoreAPI 使用 core.scope.v1；manifest 严格验证版本、side 和能力 |
| 04/07 默认插件位置 | 客户端默认目录未对齐约定 | 本地 ~/.shiki/plugins，远端 <state_dir>/plugins；构建分别输出 shiki-plugin-sync/port，生成相邻 manifest |
| 10/14 流取消 | 取消与首次请求发送可能争用 Connect 请求头；背压 Send 可能阻止 worker 退出 | client/bidi 在注册取消关闭前初始化请求头；单消息请求关闭与发送同步，独立 transport context 可取消；服务端 worker 退役设置响应写 deadline，唤醒 HTTP/2 背压发送 |
| 04/10 同步任务生命周期 | 相同 revision 的 Configure 会阻塞后续暂停；映射删除后的 watcher/账本清理不完整 | 同 revision 返回已保存状态；订阅权威映射事件后读取列表；删除停止并 join watcher、关闭树并归档账本；列表读取失败不推断删除 |
| 10 watcher | Linux 重命名目录时先 Add 新路径再 Remove 旧路径可能移除同一 inode 的 watch | 先收集安全目录，再移除旧路径，最后注册/核验新路径；Linux 普通用户递归 watcher 重复测试通过 |
| 10 同步限额与重试 | 固定限额、流式固定总超时及认证重试不符合契约 | max_auto_sync_size 默认 10 MiB、可配置 1..1 GiB，实际 I/O 使用限额；取消固定流式 30 秒截止；网络最多 5 次抖动退避，认证失败等待权威事件 |
| 03/06 进程状态 | dead pane 未知退出码被当作 crashed | 未知退出码报告 unknown；自动重启默认关闭、次数/退避/抑制持久化，只对确认 dead pane 使用无 -k 的 tmux respawn，安静进程不触发重启 |
| 03 配置快照 | 需要明确配置恢复与进程/文件恢复的边界 | 仅保存会话配置与安全 metadata；恢复创建新会话并使用当前文件；删除来源会话允许，项目仍被快照引用时拒绝删除 |

## 当前验证证据

- macOS 已通过插件目录/依赖检查，宿主/SDK 传输和插件生命周期 race 回归；取消竞争复现用例 TestRepeatedMappingRevision 连续 20 次通过。
- 真实 CLI→agent→TLS daemon→tmux 端到端已验证启动竞争、控制者/观察者、daemon/agent SIGKILL 恢复、Token 撤销和保留 pane；同一套测试加入实际双端 sync 插件，验证后台同步、初始冲突、冲突恢复、条件删除、暂停和解绑。
- sync 的 Linux 普通用户容器 race 回归已通过；递归 watcher 重复 5 次通过。port 双插件跨进程集成在 macOS/Linux race 下通过。
- 监控和自动重启批次的 session/tmux/server/daemon 集成 race 与 vet 通过。CPU 初次采样和不支持的平台值保持缺失；PID、UID、启动身份变化使样本失效。
- 整套工程将在本批变更收敛后重新运行；历史全套通过不代表新增代码自动通过。每条具体结果以对应测试输出为准。

## 尚未满足的门槛

生产转发/自动策略、TUI、连接优先级/有界重连和安装事务审计已有本地验证，连接/安装合并版本已通过 macOS 全套 race 与 Linux 全套含 E2E。后续受限CLI诊断、smart活动菜单/verbose、节点路径、两agent设备模拟、24轮资源回收和Linux双架构内嵌运行也已有专项证据。首次同步向导、项目目录选择和退出恢复菜单已有真实 PTY 专项证据。目录/保留日志后端、实时退出事件与 signal 有界重启的双平台专项现已通过，macOS完整E2E180.243秒通过；安装器只读应用就绪/初始Token/异步登录等待与最终发行包暂存验收现已通过；最终macOS与冻结Linux整套race含E2E已通过；下方保留首次失败与修复的完整证据。Darwin/Linux amd64已有翻译执行smoke，不能替代原生Intel或完整平台验收。原生LaunchAgent与容器真实systemd实测已通过（见末尾新批次）；外部Tailscale/原生VPS、重启登录持久化、WSL、生产长稳、完整性能/最终安全、正式签名分发仍未完成。不能据此标记Phase0–5完成。逐项状态同步见 [08 路线图](./08-roadmap.md) 与 [15 实施记录](./15-implementation-status.md)。

## 路线图同步遗漏修复（2026-10-28）

发现已完成批次只追加实施/审计记录，没有同步 08 的复选框；15 和本文件顶部也残留生产转发/TUI 未实现的旧描述。现按已有测试证据勾选协议、核心 CLI、同步、转发和 TUI 等已完成条目；复合条目若仍缺连接策略、真实服务、跨平台或发布验收，保持未勾选并记录已完成部分与剩余条件。没有缩减原条目要求，也没有以本地测试替代外部验收。

AGENTS.md 明确要求同批更新 08/15/16，三份状态一致才算完成该批；历史测试证据保留其源码/环境边界。本次是文档状态修正，不据此声称最新连接/安装源码已通过全套测试。

## 安装事务与内嵌产物审计收敛（2026-10-28）

按 07/11 的短时 helper、普通用户宿主、不可变产物与保留数据契约复核，修复首次安装失败/中断后无法恢复、停止旧服务后日志写入失败未重启、特权 prefix 可写祖先、systemd 字面美元转义，以及 CLI 自动启动与升级竞争。首次失败仅移除受管服务/链接，保留身份与配置以便重试；已提交但没有上一版本的首次安装拒绝回滚且不停止当前服务。PendingAction 严格校验，恢复使用独立有界 context 并报告恢复错误。

客户端 start.lock 覆盖停止、备份、切换、启动和探测全过程，备份只跳过当前持有的精确锁 inode。root prefix 逐级使用 nofollow 文件描述符验证受保护祖先。服务文件、bootstrap、密钥、清单和内嵌缓存读取均有界并拒绝特殊文件；子进程输出截断至固定上限；严格 JSON 拒绝转义/嵌套重复键、未知字段和尾随值。对不存在 prefix 的维护命令不会创建安装目录。

证据：macOS/Linux 普通用户 install/CLI/plugin/strictjson race 和 vet 通过；最后恢复修复后 install/CLI 两平台再次通过。Linux root 专项确实执行并通过可写祖先拒绝测试。新鲜构建及真实 darwin-arm64 tar/helper/Bash wrapper stage-only、重复使用不可变版本、大小与同大小内容篡改拒绝均通过，未创建真实配置/服务激活/current/receipt。`SHIKI_EMBED_VERIFY=1` 的真实提取 sync 子进程和 CoreAPI File.Stat 验证已通过。实际 systemd/LaunchAgent、外部登录/应用就绪、Linux/其他架构内嵌运行及正式签名发行仍待验收；本地摘要不是发布者签名。

## 连接候选、重连预算与路径复核（2026-10-28）

按 02/06/13 修复同时配置路线时优先 TLS，以及后台每五秒无限重连的偏差。候选按 tsnet→显式 TLS，每条均完成受 deadline 限制的认证、版本和 daemon ID 探测；短调用为下一候选保留预算，不延长父截止。仅真实网络失败或父调用仍有效的子探测超时允许回退；认证、权限、证书、协议、身份失败及用户取消立即停止。成功 TLS 路线保持至网络故障，不周期跳回 tsnet。

前台、后台和插件共用一次初始连接加最多五次重连的运行期预算；指数抖动、冷却与耗尽不可被 Resolve/maintenance 绕过。耗尽显示 RECONNECT_EXHAUSTED，显式服务器/凭据更新才能重置；不重放业务、输入或 TCP 数据。Connect v2 将本地 ECONNRESET/EPIPE 映射为 Canceled，分类器只在确有该本地 syscall 原因时认作网络故障，远端 Canceled 与真正 context 取消仍禁止回退。

ServerRecord.connection_path=8 与协商元数据只存在运行态，daemon ID 继续事务固定；doctor/TUI 使用实际路径且不伪造 RTT。专项 agent/connection/ui/CLI/admission/architecture race 及 vet 通过，含真实 TLS 候选、预算耗尽后前后台不再拨号、显式更新恢复、认证失败等待凭据更新和持久状态不含路径。随后独立复核发现 SDK 的 Relay 是空闲 peer 也可能带有的 home DERP region：Node.ConnectionPath 现仅在 backend Running、身份未过期且 peer Active 时展示观察到的 direct/relay，否则 unknown；真实 LocalAPI 解码回归覆盖空闲旧地址、DERP/peer relay、过期/登出/状态失败，并已通过 macOS race。

## 合并回归与 amd64 翻译执行（2026-10-28）

连接策略与安装源码收敛后，`go test -race -count=1 ./...` 在 macOS arm64 全部通过；`SHIKI_TEST_DEBIAN_MIRROR=https://mirrors.tuna.tsinghua.edu.cn ./scripts/test-linux.sh -tags=e2e -count=1 ./...` 在冻结源码的 Linux arm64 普通用户容器全部通过，E2E 包 151.445 秒。固定工具向独立临时目录重新生成八份 protobuf 文件，逐字节比较一致；`go mod verify` 通过。构建通过，lint 发现 daemon main 的 import 排序，已修正。该快照不包含随后发现的 Node.Active 路径修复、CLI connect/doctor 权限修复及新增资源循环测试；不将旧整套结果计作后续改动通过。

ARM 宿主上的 Darwin amd64/Rosetta：CGO_ENABLED=0 构建三宿主命令，file 确认 Mach-O x86_64，`arch -x86_64` 实际执行版本、隔离 CLI→agent 的 start/status/ports list/config get/stop/restart；新 instance ID、最终 socket 消失及锁释放均通过。Linux amd64/Docker：固定多架构 index `632dff7351799a5a9954a48dc06670ade72ee44e8c351cdc0e5a57af2fea21af` 的 amd64 manifest `88750ccdd0f2a77fcc00a14b1a48d6409bd8ddb5d7c2dd6ec0b4a2e916daf9d9`，实际 Go1.26.10 linux/amd64 与 uname x86_64；普通用户 race 的八项 IPC/锁停启、TLS 身份与撤权、AutoMTLS/CoreAPI、四种数据流、生产 port 半关闭和真实 tmux 多观察者测试全部通过，无跳过。临时进程已回收，未改系统软件或真实服务。

上述 amd64 结果是 macOS27.0.1 Apple M5 Pro ARM 宿主上的翻译/模拟运行，既不是原生 Intel，也不是 WSL 或完整 amd64 回归。Debian bookworm 包为 tmux3.3a-3/iproute2 6.1.0-3/libc6 2.36-9+deb12u14，保留 HTTPS 与官方包签名验证。各专项与完整回归的覆盖范围分别记录，不能互相替代。

Linux 内嵌运行随后完成：在只读宿主快照的容器内可写副本，以普通 UID1000 执行 `SHIKI_VERSION=dev SHIKI_EMBED_VERIFY=1 ./scripts/build-embedded.sh`；arm64 与 amd64 分别通过真实提取 sync 子进程/CoreAPI race（1.322/1.613 秒），四宿主版本匹配目标，生成 staging 被清理，容器正常退出。未修改生产构建脚本或激活真实服务。复现与平台限制记录在 [相关验收摘要](15-implementation-status.md#evidence-platform-validation)。

`test-linux.sh` 增加可选且严格限定的 `SHIKI_TEST_PLATFORM=linux/arm64|linux/amd64`，不设时保持 Docker 默认；实际 amd64 selector 和非法值立即拒绝均验证。运行期间脚本被重写曾导致 node 测试已通过但 shell 继续读取错位尾部而退出 127，已将完整流程封装函数并在同一已解析列表退出，防止回读被修改的文件。最终版本下 Node.ConnectionPath 专项 Linux race 再次通过且完整脚本退出 0（1.040 秒）。该问题不解释为节点逻辑失败，也未将最初非零退出算整次通过。

## 有界传输性能基线（2026-10-28）

新增 `internal/connection/benchmark_test.go`，使用现有真实 UDS/TLS 夹具和生产 Connect/protobuf 适配器；将夹具参数推广为 testing.TB，仅影响测试。测量预热 unary、16×64 KiB 的完整 client stream 上传，以及 bidi 首响应早于 CloseSend、半关闭后最终数据/EOF 的完整生命周期，所有路径验证结果而非仅计时。macOS Go1.26.10 Apple M5 Pro 的三轮 500 ms 样本均通过：unary UDS 71.0–76.7 µs、TLS 104.5–107.8 µs；1 MiB 上传 UDS 717–793 MB/s、TLS 769–783 MB/s；bidi 生命周期 UDS 113.1–122.8 µs、TLS 223.8–241.5 µs。原始适配器测试全包 race 与 vet 通过。

范围和复现命令见 [相关验收摘要](15-implementation-status.md#evidence-benchmarks)。该基线排除握手、JWT/业务授权、agent/插件、tmux、磁盘和 WAN，运行时仍有其他开发负载；没有据此建立正式性能门限或宣称比 SSH 更快。生产长稳和完整负载性能保持待验收。

同一基准在固定 Go1.26.10 Linux arm64 容器普通用户、只读冻结源码/模块、无 race、GOMAXPROCS=8 下三轮通过（包 10.472 秒）：UDS/TLS unary 分别 57.4–58.3/71.3–72.4 µs，1 MiB 上传分别 1580–1892/1224–1267 MB/s，bidi 生命周期 87.5–91.1/116.6–119.0 µs。环境、并行度与负载不同，因此不将两组结果作为受控 OS 性能比较。

## CLI 权限、活动菜单和日志隔离复核（2026-10-28）

发现 agent 已允许受限 Token 协商，但 CLI connect 仍额外调用 GetSystemInfo；doctor 也把已有权限的统计/插件诊断放在 system.info 成功分支。现在 connect 只读等待 agent 的已认证 ready、协议与固定 daemon ID，并核对配置/凭据 revision 未并发变更；等待最多15秒且遵守调用方更短截止，不通过轮询反复重置重连预算。doctor 各授权诊断独立执行，doctor/TUI 对耗尽状态给出明确 `shiki connect <server-id>` 恢复提示。

按05的活动会话菜单补齐 Observe/Create/Manage/Exit；创建收集项目内目录和描述并保留原映射默认会话，菜单遵循认证权限，未连接的默认会话仍快捷连接。verbose 复用宿主有界脱敏日志：raw期间不竞争终端输出，恢复后本地每插件最多20条；dashboard Plugins面板两端每插件最多10条。明确这是尾部而非完整回放，JSON管理输出不被日志污染。TUI选中server被删除/自动切换时取消并清空旧查询/屏幕/dialog，推进generation拒绝旧scope回包。

真实 UDS agent→TLS daemon 的 stats-only/observer 初次connect及显式重连都成功，GetSystemInfo仍被拒绝；伪ready/身份不符/配置凭据变化/取消均拒绝。真实PTY验证四项菜单、只读不发写入/resize/control、创建字段正确、终端恢复后才取日志；JSON/noninteractive安全与两端日志scope/Tail/Follow亦验证。CLI/client/ui/architecture/admission race及vet通过。进一步发现首次同步向导、crash菜单和目录枚举仍缺实现，已继续安排，不将当前批作为完整05体验验收。

## 两个独立 agent 的本地设备模拟（2026-10-28）

新增 `test/e2e/multidevice_test.go`，只改测试，复用实际CLI和发布形态子进程。两个独立配置、状态、socket、Token、映射/本地根连接同一TLS daemon和真实sync插件。持久前端IPC句柄分别归属各agent：两个有写资格的观察者以相同control_revision竞争，严格一成功/一Aborted；独立只读凭据拒绝接管。显式控制转移到B后停止A，B原agent ID/attachment/Claude PID及同步文件提交保持有效。

A离线编辑、B在线提交后，恢复A得到精确共同基线/两端hash与revision冲突；B再次提交，使A旧generation的KEEP_LOCAL返回Aborted且三侧原内容不被覆盖。读取最新冲突后显式KEEP_REMOTE，再由A后续提交，三根最终收敛。没有用纯state计数替代文件和流的行为证据。

冻结源码 `go test -mod=readonly -race -tags=e2e -count=1 -run '^TestTwoIndependentAgentsSharedTLSProject$' -v ./test/e2e` 在macOS arm64通过（场景12.26秒/包13.671秒）；通过最终test-linux脚本在普通UID1000 Linux arm64通过（26.56/27.587秒），无跳过。该测试race覆盖harness，默认子进程构建未额外加race，区别于下方opt-in资源测试。它不是物理多设备、外部tailnet或06所有组合故障验收。

## 24 轮资源回收验收（2026-10-28）

`scripts/test-resources.sh` 默认24轮、接受8–100轮，普通测试不隐式加长；`--linux`使用冻结源码/普通用户。缺tmux或OS资源测量失败直接失败，本机root执行拒绝，以免将E2E skip算通过。测试目录只增加共享资源观测夹具与专用轮次，不引入生产代码依赖。

实际终端attach→Ctrl-Q/SIGTERM交替：每轮CLI Wait、attachment归零、原pane PID保留；真实port双进程反复完整half-close响应和idle停止：目标socket归零、stopped状态的bound port/connections归零、端口可重新绑定，最终Supervisor.Close后worker PID为ESRCH；真实插件反复Launch/Stop/父context取消：Done、PID退出、Core订阅结束和运行目录清空逐轮断言。预热后检查点必须连续三次回到固定容差内，容差不随轮数增长；外部进程只测真实FD/启动身份，不假称读到了其goroutine。

macOS/Linux各24轮三路径最终全部通过，无selected skip。plugin host FD/goroutine分别4/2→4/2、7/2→7/2；port host分别21/47→21/47、26/47→26/47，两worker FD13/14不增长。终端harness/daemon/agent FD分别8/13/11→7/12/11、12/13/11→11/12/11，harness goroutine5→4；最终终端整条E2E281.96/277.17秒，harness和实际CLI/agent/daemon/sync子进程均启用race。

初次Linux冻结遇并行UI编辑造成编译窗口，未记通过；首次启用实际子进程race后，额外进程退出等待耗尽原总context，发生在已完成资源检查点后的原E2E尾部。仅opt-in总预算改为基础3分钟加每轮8秒，各操作/回收独立截止未放宽，之后完整重跑通过。详情和命令在 [相关验收摘要](15-implementation-status.md#evidence-resource-regression)。这是有限生命周期资源回归，不代表生产长稳、RSS/CPU容量或任意并发负载无泄漏。

## 崩溃事件与安装探测复核发现及收敛

新增实时CLAUDE_CRASHED事件的macOS测试通过后，冻结Linux测试发现exit7/signal场景等待事件超时；signal整数/符号解析已修复。以 FIFO 明确释放进程退出的 Linux 普通用户测试连续三轮（12子场景）通过，7.823秒；TTY 极快退出时 tmux 3.3a 偶发永久缺失退出字段的根因尚未证明。生产对确认死亡但原因缺失发送 CLAUDE_STOPPED/EXIT_REASON_UNKNOWN，前端已有相应处理；不得从屏幕文本推断退出码。持久unknown持续复查、后来出现可信异常及known后不重复unknown的确定性测试已通过，未知原因不自动重启。已确认signal异常的可选有界重启继续沿用不杀活pane、持久预算及主动Stop抑制；最终双平台证据见下方后端验收，路线图实现条目已勾选，但上游间歇丢失退出原因的限制保留。

安装闭环独立审计另确认：升级在Active=A/Pending=B/current=B中断时，原probe只验证A清单却执行current的B helper，可能以B进程自校验后报告Version=A，并允许显式发行Token。现已拒绝pending或current/Active不一致，并校验明确选中版本；对应回归和完整本地就绪收敛见下方安装验收。此处是实际代码问题的修复记录，不归因于缺少外部环境。

## Sync / SDK 最终复核（2026-10-28）

复核基准：04 插件归属与生命周期、06 文件/网络边界、07 依赖方向、09 worker 与 scoped CoreAPI、10 条件提交/监听、13 映射暂停/归档、14 终态和取消。测试宿主夹具及真实 subprocess 集成已迁至 test/，plugins/sync 生产代码不导入 internal；pkg/plugin 的宿主传输依赖由 SDK 私有 UDS transport 替换。

本次补充修复：相同 mapping revision 不重启 watcher，也不等待长数据操作而阻塞后续暂停；preview 结束前再次核对两侧 generation；发送失败在 context 仍有效时读取最终写入状态；删除映射先停止/join，再归档活跃账本、释放树，保留项目文件与冲突/请求证据；认证失败等待 host server/config 事件；root 路径消失或 inode 替换返回 FailedPrecondition，不解释成文件删除。符号链接和特殊文件跳过并发出有界 host log 提示。低于 1 MiB 的自动传输上限不会妨碍读取合法 .shikiignore：策略文件独立使用 1 MiB 受根限制读取。

验证结果：

- macOS：sync 各包、pkg/plugin 各包、test/integration/sync、test/architecture 全部 race 通过，相关包 vet 通过。
- 取消/重复配置/认证事件/删除任务压力：TestRepeatedMappingRevision、TestAuthFailureWaits、TestAuthoritativeRemoval 连续 20 次 race 通过。
- Linux arm64，golang:1.26.10-bookworm，普通用户 shikitest：完整 sync + config + 真实 subprocess integration race 通过；最终 ignore/root 检查变更后 filetree/client race 再次通过。原生递归 watcher 重复 5 次通过；macOS filetree race 重复 3 次通过。
- 超过原固定 10 MiB 的实际传输、自定义较小上限、另一侧在 preview 中变化、symlink 提示、root 替换、源读取中变化不提交、映射移除不删除两端内容，均有行为测试。
- 依赖在所有当前 UI/监控 imports 落盘后执行 go mod tidy；go mod verify 返回 all modules verified。

边界仍明确：fsnotify 是提示，溢出/注册失败使用周期核对；每次重试重新观察版本而非重放写；普通文件系统不能对任意外部写者承诺强内容 CAS。Linux 容器结果不是 systemd、VPS、外部 tailnet 或其他 CPU/OS 的验收证据。

## 配置命令与前端边界批次

按 07/13，`config get/set/edit` 仅通过 agent 的 Preferences/ServerConfig 类型接口读写。默认 scope 为偏好，`--server` 指定服务器记录；不提供读取 Token 的字段，不直接编辑 agent 状态或 bootstrap YAML。交互编辑使用私有 JSON 临时文件，非交互必须显式 `--file`；解析拒绝重复/未知字段，revision/ID/凭据指示/运行状态不可编辑。保存带原 revision，宿主最终执行条件提交。

真实 agent UDS 测试验证读取、单字段修改、文件编辑、过期 revision 拒绝、非法 detach key、只读字段与秘密字段拒绝；修改后 bootstrap YAML 字节保持不变。私有编辑文件测试覆盖权限、symlink、FIFO、编辑器路径含空格及不执行 shell 替换。

同时发现 CLI attach 的重试判断直接导入宿主 connection 包；已迁到 internal/client，CLI 仅调用前端接口。`test/architecture` 扩展检查 cmd 宿主不得导入插件实现，CLI 不得直接依赖 internal/client 之外的宿主包。

## 诊断与协议交互复核

- stats 命令曾通过前端代理调用 NegotiateProtocol，违反 13 的代理白名单；改为已允许的 GetSystemInfo 能力查询，没有扩大代理授权。核心 E2E 增加真实 CLI stats/doctor/config 路径，避免宽松 mock 掩盖错误。
- 05/12 要求会话重命名，但 09 的 UpdateSessionRequest 未给名称字段。为实现既定 UX，同步在 09 与 proto/types.proto 追加 name=6，并允许 name update_mask；保留既有字段号、不可变资源 ID 和 tmux 标识，唯一性由宿主事务检查。未采用删除重建冒充重命名。
- 初次使用固定govulncheck v1.1.4得到No vulnerabilities found；后续证据审计确认当时仅扫描CLI导入模块，不能代表daemon/agent/plugin的全部依赖，范围纠正见下方本批安全审计。CI 增加相同扫描、真实 CLI E2E，以及 Linux scanner 所需 iproute2。远端 CI 尚未执行。

## 本地与 Linux 停启复核

Linux 核心 E2E 暴露 `agent stop` 仅等待 socket 删除：关闭 listener 早于插件/node 清理，立即 start 启动的子进程因旧实例仍持锁而退出，随后 CLI 等待不到 ready。修复后 stop 同时等待 socket 删除和实例锁释放；start 在启动新进程前观察旧实例锁，期间仍尝试握手，并遵守原有有界启动截止。前端只读探测锁，不修改 PID 记录，也不强制夺锁。延迟 worker 清理的 flock 回归已通过。

Linux 端口 E2E 另暴露 Shutdown 响应被硬关闭截断：应用 handler 写完响应后，HTTP/2 END_STREAM 仍由 net/http 在 handler 返回后提交。runner 增加有界 graceful drain，再取消并强制回收剩余长流，避免用固定 sleep 猜测响应是否发出。macOS Shutdown 生命周期 race 连续 20 次通过，核心 E2E 通过；Linux arm64 普通用户容器合并 core/port E2E 已通过（105.66 秒），包含本批停启修复；这仍不等于 systemd 或其他未运行平台验收。


## 生产端口转发注册、恢复与审计（2026-10-28）

按 04/06/09/10/13/14：agent 持久化 ForwardSpec、revision 与 request_id 结果，client port worker 持有真实监听。CreateForward 先保存期望，再绑定；绑定失败保留 ID/error，重放返回原结果。StopForward 先保存 disabled，再取消/join，最后移除期望并保留去重日志。Server 删除/身份替换检查关联 forward。CoreAPI 按启用的 host spec 校验 server/session/精确目标；端口与文件能力分离。CLI 已接入 ports list/forward/stop，状态计数来自 worker 实际 listener。

本批审计修复：写盘结果不确定时使 agent degraded 并取消任务；返回会话 ID 必须匹配；确认会话删除后停用、join、移除，不由失败扫描推断删除；连接监测同时关闭没有活动 stream 的空闲 listener。恢复只对新确认的远端 route 重建 listener，不重放旧 TCP 会话/缓冲数据。短 CLI 请求结束不取消已接收的持久任务。远端 allowlist/port.forward 权限在此批首次 CONNECT 时检查；无拨号权限预检与自动策略留在下一批，不声称已完成。

验证：macOS agent/CLI/plugin/architecture race、相关 vet 通过；真实 CLI→agent→client plugin→scoped CoreAPI→TLS daemon→server plugin→TCP E2E 最后通过（44.57 秒）。覆盖大于 256 KiB 延迟 half-close 响应、CLI 退出、绑定冲突稳定 ID/error、Create/Stop 精确重放、agent 重启、活动与空闲 listener 撤权关闭、凭据恢复、旧字节不重放、stop 后重启不恢复。Linux arm64 golang:1.26.10-bookworm 普通用户合并 core/port race E2E 通过（105.66 秒，root 汇总），包含该测试发现并修复的 agent 锁释放等待和 HTTP/2 Shutdown graceful drain。外部 VPS/tailnet 与其他平台结果没有据此推定。

本批 macOS `go test -race ./...` 已通过，覆盖新增配置命令、TUI、监控、安装事务基础和全部插件集成；随后核心 CLI E2E 加入实际屏幕预览与保留 PID 的重命名检查，也已通过（37.45 秒）。该证据对应自动转发与受限 Token 准入下一批改动之前的版本，下一批仍需重新验收。后续一次 Linux 全套尝试在 Debian 软件源 HTTP 502 阶段中止，没有执行测试；容器脚本改为 HTTPS 源和有界下载重试，保留包签名校验。

## 受限 Token 准入与 TUI 有效权限复核（2026-10-28）

基准为 02/06 的认证与身份固定、09 的无业务权限协商、13 的 agent 代理与只读元数据，以及 05/12 的只读 Token 不展示接管操作。发现 agent 原本在每次协商后强制 GetSystemInfo，导致仅获 system.stats/session.attach 等权限的有效 Token 无法使用已获授权 API；TUI 也缺少权限信息来隐藏变更/接管。

修复：ProtocolAgreement 追加 daemon_id=5、granted_permissions=6，同步 09 与 protobuf 并用固定工具生成。仅远端 daemon 的已认证协商填写这些字段；权限通过宿主已知权限集合展开 * / namespace.*、去重排序，不包含 JWT、账户或身份声明。Agent 优先使用该身份固定/复核，每次使用缓存连接仍重新认证协商，并更新运行期元数据；旧端缺少 ID 时保留同连接 GetSystemInfo 回退，拒绝无法验证的身份。Agent 发送实际支持的能力列表，避免空 offer 产生空交集。Stats 直接调用获准的统计 RPC，不再额外要求 system.info 或开放前端 NegotiateProtocol 代理。

真实测试还发现普通业务 PermissionDenied 会错误退役整个连接池，取消其他已授权流；现在仅认证/网络失效退役连接，方法权限拒绝保持其余连接和运行期授权提示。身份不符仍在准入时严格拒绝。

TUI 从 agent ServerRecord 的已认证运行期协议读取有效权限；隐藏缺少权限的创建、删除、重命名、编辑、接管按钮和快捷键，提交已打开表单前再次检查当前提示。只读 Token 的 Enter 明确请求 observer；旧端权限未知时隐藏变更/接管，仅保留只读观察请求交由服务端最终授权。当前屏幕/统计读取不依赖 GetSystemInfo。UI 提示不替代服务端逐方法/流授权。

验证证据：

- macOS 普通用户：真实 Agent.Run UDS → TLS daemon 业务处理器 → tmux 的 test/integration/admission race 通过。system.stats-only Token 连续两次（含缓存身份复核）成功统计、GetSystemInfo/会话列表仍 PermissionDenied；身份已固定且运行期 metadata 仅含确切 system.stats，未泄露 Token/Token ID/账户名。
- 无 system.info 的只读 Token 成功查询会话、读取可见屏幕、建立 observer；重命名、删除、接管仍被服务端拒绝。observer 在另一方法被拒后仍接收后续真实 tmux 输出。错误 daemon ID 和已撤销凭据均拒绝。
- 旧端身份回退成功与 PermissionDenied 失败分支、通配符仅展开已知有效权限、只读/未知权限 UI 隐藏及快捷键拒绝、权限变化后已打开表单拒绝提交，均有专项测试。
- internal/auth、internal/agent、internal/client/ui、cmd/shiki、internal/server、test/integration/admission、test/architecture race 通过；受影响包 vet 通过。Linux 全套和整轮冻结验收由根任务独立执行，本批不据此宣称外部 VPS/tailnet/systemd 门槛完成。

## 通用私有文件与规范漂移检查

配置、SDK 状态和客户端日志原先在检查 regular file 之前进行阻塞打开，FIFO 可使启动等待不存在的对端。现在打开使用 `O_NOFOLLOW|O_NONBLOCK`，仍然在读取/写入前验证所有权、权限和文件类型；没有放宽私有文件要求。真实 FIFO 无 reader/writer 的 config/state/client 回归及 agent 启停测试均通过 race。

新增 `test/architecture/protocol_test.go`，对比 09/13 文档中的 protobuf 与四份源码的词法 token，检查字段号、消息形状、流向及 RPC 列表，只忽略格式/注释并替换已确认的 module 占位符。后续协议批次必须同时更新规范和源码，不能仅靠生成文件无漂移来宣称与文档一致。

TLS 文件读取也完成同类修复：agent 的 CA 原先用无限制 ReadFile 后检查长度，daemon 的证书/私钥也可能阻塞特殊文件。现在 CA/证书/私钥均在已打开的普通文件上有界读取（每文件 1 MiB），保留系统 CA/证书轮换常用的显式 symlink；不会把该规则用于放宽文件同步边界。TLS 服务直接使用已解析证书，不再由 ServeTLS 无界重读路径。连接、agent、daemon 真实 TLS 进程测试及配置/状态/架构 race 回归通过，相关 vet 通过。


## 自动转发策略与无拨号预检复核（2026-10-28）

实现按 04/06/09/10/13/14 分层：server port 插件执行 scanner、UID/启动时间/祖先链与前后会话根验证；daemon CoreAPI 只提供与持久记录和当前准确 tmux pane 匹配的实时根元数据。未知、歧义、变化中的归属不授予自动权限。client port 插件执行显式 auto_allow_ports 策略并返回候选；host 校验当前配置/远端能力/CheckPort、保存 ForwardSpec 后调用 StartForward。插件生产代码仍不导入 internal，host 不导入插件业务实现。

新增 CheckPort（remote facade、私有 server worker、scoped CoreAPI）要求 port.forward，只做权限/所有权验证，不拨号；manual CreateForward 现在也在持久化之前完成此预检，修复上一批首次 CONNECT 才验目标权限的缺口。CONNECT 再验证同一 endpoint/session/automatic/source_identity；默认 auto_forward=false，client 与 server 两端独立 allowlist，保护端口自动模式始终排除。新的 capability 和字段已同步 docs09/proto，词法一致性测试通过。

automatic/source_identity 为 host-assigned 字段，前端不能伪造。显式 stop 原子保存 disabled 和来源抑制；同一 endpoint/session/进程身份在 agent 重启后不会自动复活，新进程身份可重新参与规则。重启后的 automatic spec 必须重新通过发现/授权才绑定，能力退化与扫描/进程观测失败关闭 automatic runtime 但保留 desired；manual 不因扫描失败删除。成功扫描的策略/来源变化才清理自动记录。自动绑定配置变化保存新的 revision 后重新应用。

审计另修复 scanner 失败不应使用代表远端连接失效的 Unavailable：本机扫描前置条件失败使用 FailedPrecondition/PORT_SCAN_FAILED，避免连带取消授权不相关的 manual TCP stream；真实 E2E 保持手动连接贯穿扫描故障并完成往返。停止自动配置时，worker Start 与每次 Core CONNECT 都重查当前自动规则，避免旧 desired 在配置变更窗口扩大授权。

当前证据：macOS 相关 agent/CLI/SDK/plugin/daemon/auth/architecture race、跨进程 port 集成与 vet 通过；完整 CLI→agent→两端插件→TLS daemon→TCP 自动策略 E2E 通过（含活动 manual 流穿越扫描故障，104.48 秒）。先前 Linux 普通用户自动策略 E2E 通过 112.79 秒；加入最后扫描错误分类/活动 manual 流断言后，正在对新的冻结 source snapshot 再验，不把旧结果视为新断言通过。已验证测试源为 Debian 官方签名包镜像 https://mirrors.tuna.tsinghua.edu.cn，bookworm arm64、Go 1.26.10、tmux 3.3a-3、iproute2 6.1.0-3、libc6 2.36-9+deb12u14；TLS/包签名校验未关闭。


本批最终冻结 snapshot 复验完成：`SHIKI_TEST_DEBIAN_MIRROR=https://mirrors.tuna.tsinghua.edu.cn scripts/test-linux.sh -tags=e2e -run 'TestCLIPortForwardTLSLifecycleAndRecovery|TestRealOwnershipAuthorizationAndNoDial|TestUnknownAmbiguousAndFailedOwnershipScan|TestDiscoveryRequiresExplicitPolicyOwnershipAndNoBinding|TestForwardPermissionPreflightDoesNotPersistDeniedIntent|TestAutomaticMetadataIsHostAssigned' -v ./test/e2e ./plugins/port/server ./plugins/port/client ./internal/agent` 全部 PASS。完整 CLI/TLS 自动策略 E2E 110.74 秒，包含最后 scanner FailedPrecondition 分类与活动 manual TCP 流保留断言；真实所有权、无拨号、非归属/歧义排除、client 策略、权限预检不持久化拒绝意图、禁止前端伪造 automatic 字段均通过普通用户 Linux race 检查。该 snapshot 也包含最后的当前配置重查和远端 automatic/check capability 门控，未在运行容器里修改源码。本批生产/测试源码已冻结，交 root 执行合并回归。

## 子进程输出上限复核（2026-10-28）

按06/10的有界输出和取消要求，发现 tmux 的 boundedCapture 与 port scanner 的 boundedOutput 匿名嵌入 bytes.Buffer，提升了 io.ReaderFrom；io.Copy 或 os/exec 可绕过自定义 Write，导致实际输出无上限。改为命名 buffer 并仅暴露所需 Len/String/Bytes；tmux 保持有界保留并排空输出，scanner 对超限输出拒绝解析，scanner 子进程增加1秒 WaitDelay。没有改动插件归属或宿主/SDK依赖方向。

新增回归同时覆盖 io.Copy 快路径和真实子进程超大 stdout/stderr。macOS 专项包分别2.325/2.436秒通过，完整 port/server race 4.373秒及相关 vet 通过；冻结 Linux arm64 普通用户专项包分别2.028/2.058秒通过，脚本退出0。Claude版本/登录诊断同类修复由安装就绪批次验收，历史整套通过不覆盖这些新变更。

## 首次同步、目录选择与退出恢复前端验收（2026-10-28）

首次运行从选择项目、确认暂停映射、明确是否启用同步到创建会话/attach/detach已通过真实PTY测试。首次同步覆盖跳过、拒绝启用、逐文件keep-remote、暂停、过期generation拒绝，以及保存后worker失败时回查实际enabled状态；不自动启插件或批量覆盖。固定request_id按操作/范围派生，不使多次不同变更撞幂等键。目录选择验证分页token与嵌套scope、菜单保留字、错误项目/越界响应拒绝和取消；缺少权限/能力时手动输入仍由daemon最终校验。

退出恢复验证实时crash、已crashed、只读观察者日志、手动提示、已自动恢复不重复restart、未知STOPPED和普通STOPPED；日志/查询/确认重启均在termios恢复后执行，不重放旧输入。保留日志去控制字符并明确截断及非持久历史范围。最新前端三包race分别13.412/4.405/2.705秒，vet通过；此前含architecture/admission的五包race通过。目录/日志后端与退出监控的最终合并验收仍单独跟踪。

独立契约复核另发现并交后端批次修复：同进程unknown→known→unknown不能重复结束通知；目录名必须能形成合法请求路径；GetSessionLogs的3秒截止必须覆盖可取消的操作锁获取。权限、能力、显式代理白名单符合09/13，ListFiles仍不开放。最新四份schema使用固定工具隔离生成，八份生成文件逐字节一致；文档词法/生产依赖检查、make lint及make build通过。

当前合并普通套件 `go test -race -count=1 ./...` 在 macOS 全部通过（含 integration）；随后仍有日志锁/目录名/退出通知去重、tmux通用命令上限及readiness实例一致性收尾，这些差异由所属专项及最终冻结回归补验，不将本次结果冒充最终冻结验收。

## 目录、保留日志及退出监控后端收敛（2026-10-28）

最终project/session/tmux/server/agent/auth/architecture七包race在macOS与普通UID1000 Linux arm64全部通过，无skip/race警告。Linux对应耗时1.737/11.575/2.410/7.525/3.502/1.973/1.084秒。真实Agent→TLS验证目录file.read、日志session.attach与错误权限拒绝，原ListFiles仍拒绝；目录根/每层nofollow、cursor变化Aborted、UTF8/反斜线/扫描上限、日志128KiB/2000行/身份复核均覆盖。日志3秒截止包含可取消的锁等待，无遗留等待goroutine。tmux通用命令合计1MiB输出上限、超限拒绝解析及1秒WaitDelay已验证；Claude版本合法前缀加超限stdout也被拒绝。

本次Linux真实退出事件分别观察到可信exit_code=7与exit_signal=15，生成有sequence的CLAUDE_CRASHED；exit0和主动Stop不产生假崩溃。TERM/KILL预算测试各known=3、unknown=0，最多2次重启、跨manager重建不充值、主动Stop持久抑制全部通过；确定性已知signal测试另验证相同预算约束。alive pane拒绝无-k自动respawn。unknown→known→unknown测试确认只发送一次未知结束、可升级可信崩溃且不降级重发。

仍保留观察限制：本次Linux保留日志fixture的退出原因known=0/unknown=1，文本正确、ExitCode=nil；macOS同专项known=1/unknown=0。tmux3.3a间歇缺失STATUSREADY的根因尚未证明，因此没有从计划执行的exit/kill或屏幕内容伪造退出字段，也不把未知分支算作可信崩溃证据。完整macOS E2E180.243秒通过；最终安装源码冻结后的Linux全套仍待运行。

## CLI命令、帮助与能力最终对照（2026-10-28）

按05/12/13检查当前首版命令树，修正参考示例的必填session/snapshot/server/conflict参数、create --project、插件side及remote server范围、plugin logs语义、私有JSON配置编辑与node/agent状态区别。模板明确远端项目name/path和已存在目录；sync的cwd便利选择不推广为所有会话命令的隐式项目选择。13将快照映射写成sessions snapshot并补plugins config。没有删除原设计功能或把未来GUI/未发布渠道当现有命令。

新构建工具逐一执行69个应用帮助入口，全部退出0；sessions create帮助明确--project。CLI与architecture race分别13.461/1.239秒通过，git diff --check通过。目录/log/sync的权限、capability、代理与daemon声明一致。结合07目录依赖、09/13词法和11安装说明复核，路线图文档一致性项勾选。该批最后仅sessions create帮助文字在Linux冻结之后变更，独立CLI回归覆盖；没有改变业务行为。

## 安装本地就绪与最终发行包收敛（2026-10-28）

DaemonReadiness仅注册在同UID的admin.sock，独立instance header/版本/daemon_id校验，不扩展NodeControl登录职责、不注册远端或agent代理。实际检查state View、listener、全部已启用插件Health，并复核插件全集、revision/启用状态及相同存活实例；查询中重启/增删/切换不乐观ready。Claude --version和auth status --json有界执行，仅保留严格loggedIn布尔对应状态，不泄露账户元数据；local_application_ready始终另带external_peer_acceptance=pending。

安装器绑定receipt选中release/helper/宿主build，pending事务或current不一致先拒绝；client独立Agent握手也验build，最终再查原Node instance。异步登录只读等待URL/online/失败/截止，不重复开启已有attempt；有效TLS-only配置不被强制要求不会使用的key。首次Token只在明确初次激活意图或显式probe选项交付到TTY/新0600文件，普通probe/升级/重复安装不增发；重复bundle携带发行选项明确拒绝并提示probe。

真实helper+daemon+sync bootstrap macOS E2E26.60秒通过，含Token文件/显式TTY writer、普通probe不增发、已有输出不增发、实际TLS认证、Claude登出与插件失败pending以及管理/远端listener注册边界。最新install/CLI/server/plugin/architecture race、build、含e2e的vet与diff检查通过。最后真实darwin/arm64发行包LICENSE逐字节匹配仓库且受release.json/SHA256SUMS约束；解包helper重复stage-only复用唯一不可变版本，install-client.sh wrapper通过，大小变化及同大小LICENSE篡改均拒绝，未写入current/receipt/实际配置/状态或激活服务。证据：/tmp/shiki-release-license-final-xfwt3art/evidence.json。

该批源码在根任务Linux冻结后没有变化；最终Linux全套含E2E正在运行。本地摘要不是发布者签名，真实systemd/LaunchAgent、外部Tailscale/Claude账号、生产长稳及正式发行仍保留门槛。

## 最终冻结Linux合并回归的首次失败（2026-10-28，已修复并复验）

普通用户arm64全套race含E2E发现两项此前专项未复现的问题，不能记整套通过。TestDashboardCancellationWithoutKeyRestoresTerminal触发Bubble Tea 1.3.10 shutdown关闭cancelreader 0.2.2的epoll文件，同时readLoop读取Fd，race检测报错；前端批次正在修复取消/读循环/文件所有权。TestSignalRestartBudgetAndExplicitStop的TERM分支观察到unknown原因却有残留NextAttempt/LastSignal15字段，后端正在区分持久状态逻辑与复用JSON目标对象的观测偏差，不能以放宽断言解决。08的TUI/退出监控勾选已撤回，15同步标注；修复后须重跑专项和最终冻结合并验收。

同一冻结运行最终退出1，E2E包另有TestInstallerLocalReadinessAndExplicitTokenDelivery在初次ordinary-user probe返回exit1（3.95秒），daemon日志为空；安装owner正在普通UID1000 Linux复现。其余包的通过输出仍保留，但E2E包与整套均不算通过；macOS安装证据不代替这个Linux新路径。

定位结果：TUI上游killed关闭路径没有等待readLoop退出，需由前端持有可取消输入并保证读取结束后再关闭底层句柄。unknown记录字段残留来自Snapshot.Get对复用目标执行json.Unmarshal，omitempty省略字段保留旧值；生产restartStep每次创建零值记录，持久状态本身已正确清零，但公共状态读取仍应返回完整新值，正在修复该边界而非放宽断言。安装probe在有界协商循环前创建Unix客户端，socket父目录尚未建立时提前失败，正在将等待覆盖客户端创建。三项均保留失败测试和各自回归。

TUI竞态修复已冻结：前端自有dashboardInput保存原终端Fd/Write，但不实现cancelreader.File的Name，使Bubble Tea仅持有无原生资源的fallback wrapper；前端统一Cancel唤醒、等待当前Read退出、再关闭原生epoll/kqueue资源，并拒绝后续读取。Run返回前完成这一过程，保持终端交接。两项真实PTY测试各20次race在macOS/Linux arm64通过（27.516/3.039秒）；取消后立即连接真实Agent终端，验证准确输入x、Ctrl+Q及termios恢复。CLI/client/ui/architecture广包race两平台通过，vet通过。依赖版本不变；go mod tidy仅将已固定cancelreader v0.2.2列为直接依赖，go mod verify通过。最终冻结整套仍需重跑，原失败没有被抹除。

共享状态读取修复已冻结：Snapshot.Get在fresh同类型临时值上解码，成功后才替换目标；NotFound/非法JSON/类型错误不改原对象。确定性回归先复现raw JSON已省略字段而复用目标仍残留旧signal/deadline，再验证标量/map/slice/pointer/null/自定义JSON及别名不受错误修改。未改schema或磁盘格式；审计生产调用未发现依赖隐式合并，原信号安全断言不变。mac与普通用户Linux的SDK状态/宿主/认证/node/plugin/sync/真实sync集成/架构十包race通过；正确的port集成专项另通过。两平台TERM/KILL各known=3、unknown=0，持久预算/Stop与unknown复用目标清零均通过。

安装启动修复已冻结：先在Linux普通用户稳定复现3/3初次probe因admin.sock不存在失败；现将Unix客户端创建纳入原20秒启动期限，仅重试缺失路径，所有权/权限/文件类型异常仍立即失败，父取消仍有效。延迟socket/父截止/非法普通文件回归通过；Linux安装/CLI race2.463/1.019秒、真实bootstrap连续3次22.01/19.89/24.23秒通过，无skip；mac安装race4.063秒、bootstrap29.81秒通过，vet/diff检查通过。复现与修复证据：/tmp/shiki-bootstrap-linux-luk34e2n/evidence.json。

根任务已重新同时运行macOS与冻结Linux完整race含E2E；330份Go/proto/module文件校验与第二次Linux快照一致，最后make lint、diff检查通过，go mod verify通过。最终结果尚在等待，不提前记整套成功。

## 最终合并验收与路线图同步（2026-10-28）

三项合并失败修复冻结后，macOS执行 `go test -race -count=1 -tags=e2e ./...` 全部通过，E2E190.197秒；Linux执行 `SHIKI_TEST_PLATFORM=linux/arm64 SHIKI_TEST_DEBIAN_MIRROR=https://mirrors.tuna.tsinghua.edu.cn ./scripts/test-linux.sh -tags=e2e -count=1 ./...` 全部通过，E2E173.475秒，脚本退出0。两套均包含完整普通包、真实插件集成、核心/转发/双agent/安装bootstrap E2E。330份Go/proto/module文件与最终Linux快照哈希一致，没有用后续源码冒充冻结结果。

最新make build、make lint、go mod verify、git diff --check通过；固定工具隔离生成八份协议文件一致，协议文档词法与生产依赖检查通过。69个应用帮助入口与05/12/13命令映射已核对。08恢复TUI/退出监控勾选，并同步CLI/文档一致性完成项；15与本记录更新最终证据。原42条路线图要求完整保留，不缩减验收范围。

本轮本地批次完成；真实systemd/LaunchAgent生命周期、官方外部Tailscale策略/网络与Claude账号、物理多设备/完整平台、生产长稳/完整性能安全、正式签名发行/支持入口等门槛仍未完成，Phase0–5整体不宣称交付。

## 服务管理器、安全审计与持续负载批次（2026-10-28，历史过程记录）

继续处理仍可在本地完成的门槛：Docker Desktop私有cgroup namespace内已成功运行真实systemd252为PID1；默认只读cgroup尝试失败，追加CAP_SYS_ADMIN后仅重挂容器自身cgroup成功，不使用privileged或宿主cgroup/Docker socket挂载。真实安装/停启/升级/回滚现已验收通过。Linux普通UID1000的system service与user-agent、真实cgroup/HOME/PATH/cwd已核实；同一pane PID1325跨11项生命周期操作保活，旧worker消失，原身份/Token/配置/状态保留，实际systemctl poweroff退出0，完整场景42.86秒。证据为 /tmp/shiki-systemd-m7q9jsgm/evidence.json、shutdown.json与source-sha256.json，复现见 [相关验收摘要](15-implementation-status.md#evidence-systemd-validation)。

macOS27.0.1 arm64、UID501原生LaunchAgent完整生命周期67.41秒通过，覆盖真实sync/port插件、SIGKILL自动重启/新instance/旧前端拒绝、明确停止15秒不重启、A→B升级/B→A回滚、卸载保留数据。仅本次临时程序/配置/状态与标准job/plist，最终job/plist清除且未用进程fallback。Mac/Linux各自的SIGTERM取消清理均另行验证，不算重复的完整生命周期通过；复现及限制见 [相关验收摘要](15-implementation-status.md#evidence-launchd-validation) 与 [相关验收摘要](15-implementation-status.md#evidence-systemd-validation)。

真实服务测试发现NodeControl状态漂移：Agent在显式禁用tsnet时返回约定offline/NODE_DISABLED，而daemon新增实现返回协议注释未定义的disabled；安装器因此跳不过客户端离线状态。按13状态集合统一到offline/NODE_DISABLED，并仅在配置明确禁用（daemon还须显式TLS）时进入应用检查；该修复由服务验收批次完成，不扩大离线放行范围。另修复daemon Initialize/StartDaemon错误地将不可变插件目录作为0700私有状态创建/检查：产物由安装器管理，manifest/owner/mode/hash验证仍保留；新增测试确认初始化不创建或修改产物/受管symlink。systemd WorkingDirectory原引号被解析为路径内容，改用有效~ home语义并由/proc/PID/cwd实测。三处均遵守既有07/11/13要求，不通过改写设计来承认偏差。

安全扫描范围已纠正：初始-C cmd/shiki模块扫描只覆盖CLI导入模块；全仓库采用govulncheck v1.1.4 -scan=package ./...，实际覆盖全部入口的imported package集合。发现compress1.18.5的GO-2026-5841，已升级1.18.7并tidy/verify通过；复扫该公告消失、零受影响已导入包。x/crypto的GO-2026-5932涉及未维护openpgp（无修复版本），当前未导入该包，仅模块级trace仍保留；不将其隐瞒或宣称所有模块无公告。CI改为全仓库package扫描，远端CI仍未运行。数据库时间2026-10-21T00:27:29Z。

新增显式opt-in持续应用负载测试，使用真实TLS agent/daemon、双端sync子进程与持续tmux终端，交替64KiB/1MiB本地/远端写入并校验hash、同步状态、终端回显和原进程身份，周期采集实际FD/RSS/CPU。最终1分钟工具自检macOS/Linux分别26/27轮通过（完整测试159.08/165.88秒），payload8,585,216/8,650,752字节，平均收敛305/225毫秒，所有固定FD/goroutine阈值通过。测试证书改为所选时长加1小时，持续范围上限8小时避开12小时worker凭据轮换，逐轮12秒watchdog取消阻塞Send，终端tail限128KiB；双向都包含1MiB写。初期一小时运行发现工具问题后主动停止，不算完成时长证据。修正后新一小时双平台冻结源码运行已开始，完成前不称长稳验收。所有宿主/worker均启用race，worker测试launcher在生产环境过滤后设置私有报告路径，退出后检查报告；独立真实race注入在macOS/Linux分别4.476/3.165秒通过，确认子进程报告确会使测试失败。该工作负载不包括WAN、真实Claude账号或生产容量，RSS/CPU仅观察值，不能以FD阈值推断完整资源无泄漏。


安全源审计逐一检查21文件65处Snapshot.Get，未发现依赖旧隐式JSON合并的生产调用。新增真实TLS重放验证当前Token/权限/撤销仍需重新授权，以及namespace/未决intent跨重启和主状态rename失败的保留/关写行为；macOS/Linux重点race通过。扫描darwin/linux×arm64/amd64四目标均无受影响已导入包，静态扫描不是硬件执行。详见 [相关验收摘要](15-implementation-status.md#evidence-security-validation)，未宣称fsync/ENOSPC/掉电所有注入点通过。

本次进度遗漏再次纠正：08/15仍残留真实服务未激活描述，而独立验收报告已有结果；现在三份同时同步，并修正文档索引。08服务保活原型勾选，Phase4按11完整安装及升级复合条目保留未勾选并列出剩余条件；42条原始要求未删改。既有190.197/173.475秒全套证据属于上一批，不能覆盖本批服务修复和compress1.18.7。新全套已启动；macOS当前已报告node无效key专项超时，正在调查，不记整体通过。

本批整体系结构复核已覆盖目录/依赖/SDK归属、RPC准入、状态与重放、生命周期、文件/终端/网络边界、CLI/配置/产物与测试范围。进一步发现agent诊断节点在无法登录时仍广告node.login.v1，正按实际可用能力修复并补真实UDS测试；在专项和合并检查完成前，本批不宣称收敛。原生VPS、重启登录、既有用户迁移、传输中升级、外部Tailscale/Claude、物理多设备、完整平台/性能/生产长稳、正式发布仍是明确门槛。

持续负载状态更新：本轮macOS一小时尝试在前置撤权步骤等待CLI退出15秒超时，尚未进入持续计时，已记失败并调查；Linux一小时冻结源码任务仍在运行。另查出06要求的同版本5分钟3次agent崩溃熔断尚未实现，已安排本地修复，不将其归入外部环境门槛。本批仍在进行。


## 后续失败定位与文档约束修复（2026-10-28，历史过程记录）

首轮新合并macOS整体失败：node无效key拒绝后新有效key登录仍保持旧denied，E2E包自身198.318秒通过。Linux整体通过，E2E178.950秒；普通全套会跳过opt-in原生服务/资源循环，不能代替专项。另在无网络隔离Linux root容器补验安装可写祖先拒绝，PASS1.058秒。原失败未被重跑掩盖。

节点根因：新attempt已发布，但异步runLogin尚未换generation，旧SDK拒绝可覆盖新attempt。新增确定性回归先在旧代码0.56秒失败；现在新attempt在同一锁下立即退役旧generation，旧watch初始化失败/状态/拒绝、跨越新attempt的SDK启动和已完成attempt的迟到worker都不得修改当前节点。最终完整node包macOS/Linux各10次race通过206.603/168.413秒，协议/状态集合/取消与凭据要求不变；模块README同步NodeControl与独立DaemonReadiness边界。

agent诊断节点不再广告node.login.v1，真实UDS同时覆盖禁用与损坏节点，必需能力拒绝、状态诊断、登录fail-closed和损坏文件保留。macOS agent/install/architecture race6.733/4.046/1.137秒，Linux2.434/2.423/1.115秒通过；模块README及launchd说明清除过时实现状态。

macOS一小时尝试在撤权等待CLI退出15秒失败，未进入计时；一次新的1分钟工具自检通过不消除该失败。后续真实PTY+UDS测试稳定复现：Terminal同步Receive→本地Write，输出堵塞后无法读取远端最终错误。现拆独立接收与有界输出队列（64帧、排队protobuf大小1MiB；当前写中和接收中另受每帧上限），任何失败取消并join所有pump，正常EOF排空原始bytes，满队列明确报错要求重绘。首轮10次双平台终端专项53.162/15.283秒通过，涵盖远端认证错误中断blocked输出、正常EOF完整UTF8/ANSI字节、慢输出显式超限。独立review另识别Send被远端最终状态关闭时可能先以EOF覆盖认证错误，已加最多1秒的最终接收仲裁，真实并发发送终态测试和最终合并仍在进行。尚无证据证明首次15秒失败唯一由此造成，不能把推测写成唯一根因。

06的同版本5分钟3次agent崩溃预算此前未实现，属于本地实现缺口；正在补实例锁内私有启动记录、稳定degraded诊断、不自动启动节点/资源/插件及显式修复后恢复，并更新真实服务反复SIGKILL验收。期间不宣称该批完成，08终端项暂撤勾选。17已记录原型事实/假设/条件工作量与外部未知等待，08复盘条目勾选；原42条要求保留。

本批修复后的最新专项：agent崩溃熔断已实现并通过真实三次SIGKILL、并发CLI、明确停止/显式恢复，以及原生LaunchAgent106.81秒和真实systemd完整生命周期复验。systemd同一pane跨13个操作保活，第三次agent异常后诊断PID稳定15秒且无worker；正常stop退出0且15秒不重生。终端最终实现（含发送/接收终态仲裁）双平台各10次race通过71.096/22.584秒，独立并发终态专项亦有修复前失败/修复后通过对照。build/lint/vet/module校验通过；最终双平台整套正在执行，macOS新一小时实验已进入计时，Linux此前冻结实验仍运行，均未计完成时长。

守卫细节与范围：0600且最多4KiB的startup.json仅由持instance锁的agent读写，拒绝FIFO/symlink/权限错/未知schema/损坏和时间回拨；异常终止以未正常结束标记在下次持锁启动识别，显式返回的运行错误只计一次，五分钟外记录过期，长时间运行打断快速崩溃序列，正常退出清除未熔断失败，换build重新计数。第三次异常持久熔断后进入degraded/AGENT_CRASH_LOOP，无tsnet/连接恢复/worker；停止诊断进程不清除熔断。--recover-crash-loop只允许显式恢复有效熔断记录，不绕过损坏或时间回拨；前端只发IPC和启动参数，不改后台文件。配置Load/Normalize/实例锁取得之前的明确错误不在已取得实例身份的崩溃预算中，继续报错，不宣称覆盖所有启动失败。

新guard相关macOS整包race agent/client/install/CLI/architecture分别7.165/8.351/4.924/13.329/1.344秒，Linux分别3.543/3.177/2.634/3.460/1.112秒；Linux真实CLI E2E10.485秒、macOS13.99秒。普通用户安装包root-only跳过由独立隔离root专项补证。新的服务证据 /tmp/shiki-systemd-8e_gpc_2/evidence.json确认13项操作、crash_loop状态/稳定PID2541/显式恢复PID3291/重新受user-service管理PID3336、poweroff0；LaunchAgent最后job/plist不存在且无fallback。详见更新后的两份service报告。


## 本批最终合并收敛与三文档同步（2026-10-28）

最终macOS `go test -race -count=1 -tags=e2e ./...` 与冻结Linux arm64普通UID1000同范围完整race均退出0，E2E分别207.127/177.266秒。日志 /tmp/shiki-merged-macos-complete-20261028.json 与 /tmp/shiki-merged-linux-complete-20261028.json。真实三次崩溃CLI E2E18.12/10.63秒、终端EOF/认证最终状态1.83/0.53秒、子进程race报告注入3.08/2.16秒通过。普通全套明确skip两平台root-only安装和opt-in插件资源循环，mac额外skip opt-in LaunchAgent；root-only另有实际隔离root1.058秒，原生服务另有106.81秒，systemd最新实际98.39秒。24轮资源回归沿用此前专项，不由skip替代。

根审查曾提出diagnostic启动时短暂starting可能导致普通start误成功；完整调用链证明dial先GetStatus并把starting/stopping转Unavailable，只有稳定degraded后才交startupResult验证reason，状态与reason同锁发布。因此疑点不成立、生产未改，无需为已排除的问题重跑或修改设计。所有后续最终证据对应同一生产冻结状态。

最终make build/lint、含e2e的相关vet、go mod verify、git diff --check与test/architecture通过；四份文档proto词法、8生成文件及生产依赖边界保持一致。新恢复命令的真实help与05/12已对照，全部新增文档链接存在。复核覆盖宿主/SDK/插件路径依赖、协议准入与capability、持久/运行状态、条件操作与授权重放、启动/停止/崩溃恢复、文件/终端/网络边界，以及安装/CLI/产物与测试范围；本批发现的实际偏差已修复，不能由此宣称未运行环境或所有潜在缺陷已验收。

原42项路线图字符串逐项与HEAD对照未变；服务保活原型和原型复盘新增勾选，终端修复后恢复，总29项已验收、13项仍未完成。08/15/16同批同步。当前macOS/Linux一小时实验仍在运行，各自冻结源码与日志独立；最终时长、尾部恢复和清理成功前不标通过。新的跨版本进程/活动升级/持久故障/混合负载轮换与外部/平台/发行门槛保留，完整Phase0–5未交付。


## 跨版本、在途升级、持久化故障与发布准备批次（历史过程记录）

继续按06/09/11/13/14执行真实进程矩阵和实际服务边界验证，不将合成协议fixture称为不存在的历史正式版本。首个确定性跨进程发现：只协商terminal.v1的TLS peer仍收到agent转发GetStats，违反可选能力准入。对应测试先失败，正在补每次当前协商结果的method门控和不兼容诊断，根08能力一致性项暂撤勾选；生产修复未完成前不借旧全套结果宣布完成。

发布准备核对发现shiki-install --help被当未知命令，子命令--help输出ErrHelp且退出2。现新增准确顶层/子命令帮助，解析帮助在defaults/user lookup/key读/文件和服务操作前完成，内部_user-*入口不列入帮助；版本命令拒绝多余参数。FIFO key/不存在账号和目录的测试确认无访问副作用，macOS/Linux cmd/shiki-install race1.508/1.018秒通过；真实构建helper帮助也执行通过。激活/校验/就绪语义未改变。18发布准备仅是草稿，未知发布tag/签名身份/实际支持入口明确未确定；root README旧服务状态已按实测纠正。

持久化专项优先隔离小tmpfs的真实ENOSPC和strace按具体FD/阶段确认的fsync errno注入，不将errno模拟当物理掉电；活动升级需在真实部分temp/已发TCP请求/终端输入确认点之后切换，不能用空闲升级作为在途恢复证据。以上均尚待实际结果，三份文档按同一进行中状态更新。


### 持久化故障实测收敛

跨模块故障测试位于test/fault，不将host集成放入插件实现包。macOS真实backup rename阻塞/失败撤销及相关state/auth/filetree race通过；最终Linux无网络、普通UID1000、cap-drop=ALL、no-new-privileges、默认seccomp/只读rootfs下，strace只跟踪自身child的fsync/rename/unlink，不捕获凭据buffers。5个注入点分别为primary临时文件fsync、primary rename后目录fsync、撤销表rename后目录fsync、同步exchange后项目目录fsync、历史副本fsync；每个由实际FD路径与唯一INJECTED记录确认，随后独立新进程恢复。

提交前失败保持旧primary/目标且清临时文件；primary rename后错误返回不确定，但重开看到新值与同一成功request receipt。撤销rename后错误不恢复旧Token，已有bound call也被monitor撤销，损坏primary不能回退到未撤销旧备份。同步exchange后目录/history fsync失败仍保存旧inode在journal transfer名，拒后续写；新进程恢复把旧内容安全存history，不覆盖当前目标，然后允许新的条件写。专用8MiB tmpfs实际写到ENOSPC，state/sync填充量8,380,416/8,376,320字节，失败时原数据保留、释放空间后显式重试成功。

最终容器18.26秒退出0并移除；证据 /tmp/shiki-faults-dx3lwuc7 的results/source-sha256/isolation/exit/cleanup。macOS相关包1.650/2.743/2.717/4.430秒，Linux1.287/2.194/2.126秒。首次128MiB编译tmpfs不足在测试前失败，不算应用ENOSPC；后将编译/work与/faults分离。最终选中测试没有skip，普通go test中的Linux故障helper仍opt-in跳过，不能替代专项。无生产hook/依赖/语义修改，无需靠放宽原断言通过。EIO是syscall注入，tmpfs不证明持久硬件掉电耐久；远端ACK丢失及所有故障点仍未穷举。08/15同步相同限定结论。

最新持续负载结果：Linux普通用户冻结源码的一小时实验已完整通过，计时1h0m0.384s、1,577轮、491,651,072 payload字节，收敛min/mean/max53/267/1007ms，全部固定FD/goroutine检查与尾部恢复/清理通过，总测试3714.54秒退出0且容器消失。353文件源码hash已从实际只读挂载保留；该快照早于后续节点/终端/崩溃预算/capability修复，不能覆盖那些新代码。RSS/CPU仅观察值，不据此宣称无所有类型泄漏或生产容量；详见[相关验收摘要](15-implementation-status.md#evidence-soak-validation)。macOS一小时实验仍在运行。

新版本/能力修复专项已经双平台通过；helper进一步拒绝非install的stage-only和不适用布局/产物参数，帮助按命令展示，uninstall wrapper只在验证helper后剥离bootstrap bundle/pin。实际本地bundle/helper/wrapper证明错误stage返回2、普通缺安装返回1且不创建目录。新完整合并正在执行，macOS认证测试在新Token绑定前过期而失败，正在调查秒级JWT与一秒夹具有效期；该轮macOS不得记整套通过。未完成批次保持进行中。


## 进度同步纠正与最新合并结果（2026-10-28）

此次发现的记录偏差是：专项报告已有完成结果，08的页首/条目却仍写“正在调查”，并在文末追加新进度，使当前状态与历史日志混杂。现将08整理为当前条目状态和剩余门槛，历史失败/修复仍保留在本记录及另一份实施/复核记录；15/16页首同步当前结论。恢复文档/帮助/capability一致性勾选，42条原始要求不改，计29项已勾选、13项待完成。复合条目的部分完成必须直接更新条目说明，不能只写到独立报告或等整个Phase结束。

四层版本专项已完成，macOS/Linux独立进程矩阵40.604/43.422秒通过。修复按当前原子协商快照的逐方法capability准入、mapping的major/minor/required agreement验证及forward minor验证；双方版本/缺失能力诊断仅通过严格白名单ErrorInfo重建。旧连接同地址对端替换后重新协商，缺失能力仅阻止受影响方法，不启动进程内业务fallback。未来schema原文保留，测试专用迁移不广告为任意历史格式支持；详见[相关验收摘要](15-implementation-status.md#evidence-version-validation)。

独立审查另发现维护命令忽略--stage-only会继续执行真实操作，已在任何账号/目录访问前拒绝不适用参数（含显式false）；帮助按真实各命令参数生成。uninstall wrapper验证本地bundle/helper后只剥离bootstrap参数。实际wrapper拒绝stage-only返回2，缺安装返回1且目录未创建；helper双平台race1.495/1.025秒通过。18修正交互probe可能启动登录的描述，不再称所有probe只读。

首轮最新macOS全套仅auth的TestRefreshIsAtomicAndExpiryCancels失败：10:37:54.939568开始，到10:37:55.048419绑定时，按生产JWT整秒exp生成的一秒Token已过期；拒绝符合契约。仅将测试建立有效流的fixture调整为三秒，并保留原子refresh/revoke断言、增加JWT/registry expiry一致性、不提前取消、实际到期取消及过期拒验。生产TTL/校验/monitor未改；双平台各20次race60.769/60.655秒、完整auth包4.312/3.710秒通过。失败日志保留，不用一次重跑抹去原因。

最终macOS完整race含E2E退出0，40包通过、E2E196.232秒，日志/tmp/shiki-version-fault-merged-macos-final.json。Linux完整race同范围退出0、40包通过、E2E183.381秒，日志/tmp/shiki-version-fault-merged-linux.json；Linux全套早于上述test-only调整，后续auth专项独立补证。build/lint、go mod verify及diff检查通过。普通全套的opt-in/root-only跳过不当作专项成功；真实故障、服务与持续负载各用独立报告记录。活动升级仍待完整结果；下文补记macOS一小时完成结果，不因限定实验提前关闭Phase4/5复合门槛。


最新持续负载补证：macOS冻结源码一小时实验完整退出0，计时1h0m0.107s、1,540轮、480,378,880 payload字节，收敛min/mean/max135/318/918ms，总测试3700.65秒。固定FD/goroutine检查、尾部冲突/恢复/清理均通过；六个跟踪进程和临时源码目录独立确认消失。日志/tmp/shiki-soak-macos-terminal-final.log及源码清单/结果JSON已保留，资源观测表见[相关验收摘要](15-implementation-status.md#evidence-soak-validation)。该快照包含终端/节点/崩溃守卫修复，早于本批能力/helper修复；不由RSS观测声称内存无泄漏。Linux一小时结果保持先前快照范围，两平台均不替代生产长稳/凭据轮换。

活动升级当前仍未通过完整场景：/tmp/shiki-systemd-pu0im7nr中agent升级/回滚分别18.56/18.30秒通过，daemon升级后文件恢复/冲突显式解决/终端重连完成，随后ListForwards连续deadline exceeded，30秒轮询失败。新冻结运行补充user-agent journal和进程证据定位；尚未确定生产根因，不通过加大超时或空闲升级结果关闭门槛。

本批独立只读架构复核确认插件路径和SDK/宿主依赖方向、逐方法capability准入、mapping/forward协商核验、安全ErrorInfo白名单及helper操作参数边界符合文档。状态/授权/生命周期/终端/文件/TCP结果仅限对应专项与完整回归范围；活动升级新失败继续调查。08/15/16当前状态一致，42条要求逐条与HEAD核对未变（29已勾选/13未勾选），文档本地链接与diff检查通过。


## 活动恢复、清理契约与轮换批次（2026-10-28，历史过程记录）

上一轮是有效进展：三文档纠正、42项要求核对与双平台一小时完成证据已落盘。当前继续完整Phase0–5目标，本地与Linux容器范围不变。

活动升级失败已定位为实际恢复缺口，而非ListForwards RPC死锁：daemon正常停止使活动Port流返回Canceled，worker清理监听后报告FORWARD_STREAM_FAILED；连接池随后认证/协议验证仍ready，但same-route对象gate永久跳过重新绑定。真实agent+port worker/认证TLS回归先失败14.956秒；拟仅对已结束流原因允许同一已验证route重建监听，旧TCP及数据不复活，绑定/授权失败和显式停止抑制保持。修复与真实systemd四边界复验尚未收敛。

全面契约审计新增确证缺口：docs06 auto_cleanup配置（默认关闭、idle_threshold、warn_before）没有schema/调度/预警实现，docs05的清理建议也未实现；Session.last_active仅创建时赋值。它们不是外部门槛或后续豁免。正在补显式Stop资格、真实dead pane且无attachment的持续观测、独立持久预警窗口和条件删除；活跃shell/未知状态不删除，项目文件不受清理影响。last_active以有效附着/用户输入更新，不以Claude输出或静默推断闲置；本批未完成前不广告新capability。

插件12小时默认短期凭据已有到期回收代码，但原测试只证helper退出，未证真实sync/port业务重建；正在用显式测试TTL和真实worker/Supervisor补到期取消回调、旧PID/socket回收、新instance及业务恢复证据，不改变生产TTL或时钟，也不冒充默认12小时全链长稳。监控磁盘生产逻辑已覆盖state目录与无session项目根，本批补缺失路径必须unknown、inventory错误不得伪造空列表的行为验证。


本批已完成独立轮换专项：test/integration/rotation实际运行生产Supervisor及四个sync/port worker，使用既有Config.CredentialLifetime的12秒客户端夹具，生产默认12小时不变。双平台每场景各3轮race通过，macOS102.456秒、Linux普通UID1000容器98.938秒，无选中skip/子进程race报告；vet/架构检查通过。实际到期取消旧CoreAPI回调、回收旧PID/移除socket，新instance恢复双向SHA256文件和原loopback监听；旧TCP终止，新连接327680字节+半关闭尾部正确且无重放。宿主mapping/forward期望及Ready重放是fixture，远端中间链为UDS；不代替真实Resources/服务端自身到期或默认12小时整链。两次夹具失败与边界在[相关验收摘要](15-implementation-status.md#evidence-rotation-validation)保留，无生产修改。

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

UDS原始bind-public→chmod实现通过编译overlay同一测试确定性失败，修后权限就绪再hardlink无覆盖发布。临时alias清理失败返回错误；Close不删已替换inode，未修改全局umask。最终connection双平台各3次race4.657/4.345秒；原crashguard真实E2E各3次45.789/47.934秒（后续临时unlink报错小修由完整合并覆盖）。SIGKILL窗口可能残留私有随机名，不声称任意时点零残留；不支持socket hardlink的文件系统明确失败。见[相关验收摘要](15-implementation-status.md#evidence-uds-publication-validation)。

最新真实systemd活动升级完整187.62秒、poweroff退出0，容器已移除；agent升级/回滚18.61/18.18秒、daemon升级/回滚12.92/13.11秒，同一pane1600跨13边界。包含清理失效latch与最终RPC错误修复，不含随后UDS原子发布。证据/tmp/shiki-systemd-u1qv9m13/evidence.json，详见SYSTEMD/UPGRADE报告。

独立八类全架构复核未发现新增已确证生产偏差；修准03及清理报告的活动时间持久化说明：五秒是健康维护循环间隔，观察/写盘失败可能延长，崩溃可能丢失自最后成功持久化以来的活动，不能承诺最大五秒丢失。自动清理独立资格计时不依赖该近似值。目录/依赖、RPC与权限、持久/运行状态、生命周期、文件/终端/TCP、CLI/TUI与安装、能力声明和证据范围均复核；外部/平台/发布门槛保持。

最终382份Go/proto/module冻结清单/tmp/shiki-roadmap-final-source-20261028.json，实际Linux只读快照全部逐份匹配，记录/tmp/shiki-roadmap-final-linux-snapshot-20261028.json。build/lint、go mod verify和diff检查通过。全套日志/tmp/shiki-roadmap-final-{macos,linux}-20261028.json；macOS出现端口unknown-owner测试PORT_SCAN_FAILED和节点移除peer后连接仍成功，正在调查；不得记录该轮整体通过。端口错误是保守拒绝而不是已证实越权，初始日志不足以确定退出PID为唯一原因。

本轮macOS完整回归已退出1：40个测试包通过（含E2E209.403秒），port/server归属与internal/node移除peer两个包失败，不能记全套通过。port错误为保守拒绝，真实归属夹具改为从真实SystemScanner结果筛选自身稳定监听端点，再执行真实OS身份复核；unknown仍严格要求PermissionDenied。新增真实退出子进程的过期PID快照，严格要求FailedPrecondition且TCP不发生拨号。生产授权不变，整包race各10次macOS30.501秒/Linux22.252秒通过；原现场底层OS错误未记录，不断言其唯一原因。文件ownership_test.go hash cdc2840ce2073a8c7ef32dcaf36bb135f8f8625e3a789407960f5b91bf2fc34e，晚于382清单，按追加测试证据记录。node移除peer失败仍待调查，Linux整套尾部仍运行。

Linux本轮382源码全套race含E2E已退出0、42包通过；它不覆盖随后端口测试修正。macOS真实节点失败确认额外生产缺口：控制面peer移除后engine peer图先更新，旧userspace数据路径可能仍完成握手；adapter未绑定已解析peer并在握手后核验，不能把这种连接视为已授权。正在增加拨号前后精确peer身份/有效性检查和过时连接关闭，保留原typed userspace-only调用，不退回系统网络。08的Phase0四形态/准入复合项暂撤勾选，当前27/42、15项待完成；文档能力一致性也待整批收敛恢复，不用Linux本轮通过掩盖macOS暴露的缺口。

后续peer准入修复将Node.Dial解析的PublicKey绑定到typed userspace adapter，拨号前后核验engine精确单IP路由、非self、同key且未过期，成功但过时的连接立即关闭。独立只读审查确认无系统fallback；前后观测不是对未来任意peer变化的原子lease。补充DNS/hostname/IPv4/IPv6真实入口时发现测试控制面的旧注册身份产生同名歧义，保留生产InvalidArgument拒绝，仅给现peer通过真实control发布唯一名称；修正后单轮mac专项17.922秒通过。

383份源码再次全套复验仍未收敛：该冻结node测试尚含重复名称fixture，macOS在该处失败；另CLIThreeProcessTLSLifecycle在remote disable sync返回REMOTE_RPC_FAILED（39.04秒），正在调查，不能将整套记成功。独立复核另发现既有Node.Dial Status错误吞掉父context取消/截止分类，按14补修中。日志/tmp/shiki-roadmap-converged-{macos,linux}-20261028.json，383清单及实际Linux逐份匹配记录/tmp/shiki-roadmap-converged-{source,linux-snapshot}-20261028.json。当前27/42保持，待真实收敛后再恢复勾选。

最终节点准入专项收敛：internal/node完整包race各3次macOS71.763秒/Linux arm64普通用户61.781秒，退出0且无skip。包含真实peer移除、唯一DNS/hostname/IPv4/IPv6、四种RPC、旧连接回收和真实LocalAPI取消/截止；后握手检查overlay14分支先红0.628秒、Status ctx两分支先红0.727秒。生产仍仅typed userspace；精确peer/key观测不构成未来变化的原子lease。08恢复Phase0传输准入项，当前28/42、14项待完成，能力一致性等待本批remote插件停用失败调查。报告[相关验收摘要](15-implementation-status.md#evidence-node-admission-validation)；这些专项不把原失败全套追认为成功。

383快照全套已结束：macOS40包通过，node DNS fixture与core E2E失败，E2E包213.636秒退出1；Linux41包通过，只有相同DNS fixture失败，E2E214.839秒通过，脚本退出1。最终node专项已修正fixture与追加ctx分支并双平台通过，旧全套依旧记失败。

core E2E调查发现独立可证明的共享连接缺陷：resolveLocked对已有pool的verifyRemote遇调用者取消/截止仍invalidate/failRemote，会取消无关流并进入冷却。sync停止可触发这个路径，但原失败没有内部细因，不把该机制宣称为原事件唯一根因。正在补受控TLS探测先红与共享流保活回归，再修调用者取消隔离；不得重试不确定mutation。08暂撤Phase1 TLS取消/重连复合项，当前27/42、15项待完成。


## 本批最终收尾（2026-10-28）

最终路线图29/42、13项待完成，原42条要求逐字保留；Phase0/1/2/3/4/5分别8/11、7/7、5/6、6/6、3/6、0/6。部分完成写在原条目下，未新增较小的交付范围，原生/外部/正式发布门槛未勾选。

共享连接取消缺陷已修：调用者取消/父截止只结束当前探测，不invalidate既有健康pool或改变其预算；下一独立请求仍重新认证。真实TLS+活动TerminalStream两分支先红0.875秒，修后race3次2.488秒；macOS完整agent7.959秒、agent集成8.542秒、原core E2E38.914秒通过，Linux取消/重连预算/认证专项1.924秒和原core E2E43.326秒通过，命令均退出0。Linux过滤后的integration/agent未匹配测试，不计通过；原39.04秒REMOTE_RPC_FAILED唯一原因没有被证明，不以重跑替代原失败。见[相关验收摘要](15-implementation-status.md#evidence-shared-connection-validation)。

最终源码清单/tmp/shiki-roadmap-delivery-source-20261028.json共384文件；相对383全套快照仅internal/agent/connections.go、connection_retry_test.go、internal/node/transport.go、dial_context_test.go、node_test.go变化。对应最终node完整包双平台race各3次71.763/61.781秒无skip，以及上述agent/真实core E2E补验覆盖这些变更。最终架构race1.356秒、make build lint、go mod verify及git diff --check通过；协议源未在该收尾变化，先前8份固定生成文件逐字节一致证据保持。

证据边界：382快照Linux42包含E2E183.474秒通过，macOS40包通过但两包失败；383快照macOS40包通过但node DNS fixture及core E2E失败（E2E包213.636秒），Linux41包通过但相同DNS fixture失败（E2E214.839秒自身通过）。两轮失败均保留，最终以明确范围补验收敛，未称384源码重新整套全绿。普通全套的root权限、长资源循环、原生LaunchAgent、真实systemd活动升级和注入故障等opt-in跳过不算通过，分别参考其独立专项且遵守快照范围。

最终独立全架构检查覆盖目录/SDK/宿主、RPC与准入授权、持久/运行状态及幂等、生命周期与取消、文件/终端/TCP语义、CLI/TUI/配置、安装产物、能力声明和证据，未发现新增已确证偏差。已修准五秒活动合并的持久化范围，不承诺异常写盘下最大五秒丢失。UDS任意SIGKILL可能残留私有临时名、peer前后检查不等于后续变化原子lease、真实tmux未知退出暂停均明确保留。

下一批继续本地/Linux容器完整凭据轮换、混合负载与性能/资源验收；历史发行组合、物理多设备、原生VPS/重启登录、外部账号、原生平台和签名分发仍独立待验。完整Phase0–5目标保持进行中，不将本批收尾视为成品交付。


## 真实宿主轮换与混合负载批次（2026-10-28，已收尾；以下保留过程记录）

上一批384源码修复/补验和29/42同步属于已完成工作，本轮继续完整Phase0–5，不缩减交付范围。批次开始时既有rotation仅Supervisor+fixture期望/Ready；本轮使用生产agent运行入口/Resources、daemon Runtime、Core/Ready、持久映射与forward及真实TLS，客户端/服务端分别自然到期。采用内部构造的显式短TTL依赖，现有命令仍零值12小时，不新增配置或环境暗门；宿主在测试进程、四worker独立子进程的范围将如实记录，不能当作默认12小时独立宿主binary长稳。

混合负载扩展现有opt-in真实命令测试，默认原soak不变；增加并发TCP与终端/64KiB/1MiB双向文件同时运行、半关闭与无重放、固定进程/FD/goroutine观测和子进程race报告。先双平台一分钟自检，再冻结源码做受控持续运行，结果必须等实际结束。

初审时发现确定状态窗口：CoreAPI grant到期已经取消回调/拒绝Call，但Instance.Stop等待期间Supervisor.List仍可能返回ready和Protocol，daemon依据它显示可用。当时据此补确定性到期/停止未完成回归并派生实时可用状态，保留持久enabled/revision，不能仅用等待隐藏窗口。默认12小时超过稳定预算重置窗口；短TTL<一分钟不能用于推导长期重启预算，测试不能修改或绕过既有预算。

readiness修复已冻结并完成专项：List/current统一核验grant有效性、实际deadline、未取消lifetime、未关闭Done及实例ready；退出窗口派生failed/PLUGIN_INSTANCE_ENDED且清Protocol，不改变enabled/revision。原实现对deadline期间真实Stop受阻及Done两分支先红1.580秒；最终internal/plugin完整包race各3次macOS11.732秒/Linux11.013秒，无skip。报告[相关验收摘要](15-implementation-status.md#evidence-plugin-readiness-validation)，vet/diff通过。

开发期间真实hostrotation首轮完成双端场景（mac开发包71.412秒），但后续加强stats持续消费与恢复后新帧断言，当时等待最终双平台复验，旧缓冲帧不作为存活证明。首验测试错误要求manual stop后仍有spec，已按13修成spec删除+成功持久request receipt，原失败保留。混合模式首版两平台一分钟各25轮/100次1MiB TCP自检通过，后续增加同宿主空载终端测前/后基线并改精细观测，须以新快照复验后再跑持续负载，不能混记。

最终新增宿主矩阵：macOS client36.13/server35.98秒（包74.820秒）、Linux client34.36/server34.26秒（包71.325秒）通过，无skip/child race报告；强化非nil配置负TTL、精确错误及无目录/无socket副作用测试单独mac1.622/Linux1.023秒通过。默认宿主回归mac agent8.048/server12.636/architecture1.352秒通过，Linuxarchitecture1.155秒。报告[相关验收摘要](15-implementation-status.md#evidence-host-rotation-validation)。两宿主在测试进程，四worker独立；生产命令无TTL入口，零值12h不变。

本轮冻结390份Go/proto/module清单/tmp/shiki-host-mixed-final-source-20261028.json，与Linux实际只读快照逐份一致（/tmp/shiki-host-mixed-final-linux-snapshot-20261028.json）。完整macOS43包通过、E2E210.593秒；Linux42包通过、E2E186.391秒自身通过，但旧Supervisor rotation/sync在15.36秒返回old callback not canceled: started=1 active=0 canceled=0，整套退出1。该测试只在handler返回的defer即时采样ctx.Err，当时据此补真实Context.Done与handler终态时间观测，不能把返回后请求本身自动cancel冒称到期取消；到期前结束必须仍判失败。原失败保留，未称Linux全套通过。build/lint/go mod verify/diff通过。

混合负载最终一分钟自检双平台均通过，测前/测后各20次空载终端与混合期使用相同1ms观测：mac163秒量级、Linux173秒量级，25轮/100条1MiB TCP、各方向104857600字节、EOF尾2900字节、文件8519680字节；准确时长162.25/172.59秒。这两次运行仅算自检；随后各10分钟受控实验按mac→Linux串行执行，并保留真实snapshot、process/container及清理证据，结果见下文。

旧轮换取消观察器已通过独立只读审查：真实Context.Done观察与handler返回分别记录，拒绝到期前返回、普通成功返回后请求cleanup等假证据。AfterFunc记录的是取消的观察时间，不是精确取消瞬间或原失败的唯一因果证明。修正后旧rotation整包race各3次macOS102.311秒/Linux99.378秒通过，无skip/child race；最终又补等待handler退出与观察完成共同成立的join，保持到期断言不变，最终join源码整包补验macOS35.033秒/Linux34.154秒均退出0、无skip/child race，四份测试hash再次核验一致，vet/diff通过。原390源码Linux全套失败不改写为成功。

最终差异的独立八类架构复核通过，未发现新增已确证偏差：目录/依赖、SDK与宿主归属；Connect/插件gRPC与授权准入；持久期望/修订/请求记录；进程退役/取消/恢复；文件冲突/暂停与TCP半关闭/无重放；CLI/TUI/默认配置；安装与产物边界；能力声明与验收证据。最终join源码清单为/tmp/shiki-host-mixed-delivery-final-source-20261028.json（391文件），相对390全套只改变旧rotation的harness并新增observation测试；生产及混合负载源码未变，受影响包已双平台补验。

默认12小时门槛保持未完成：后续须独立执行实际agent/daemon二进制，所有构造参数零值，在每平台真实运行至少12小时并核验到期后恢复；建议12小时30分负载、13小时外层截止，串行平台观测时间单列。不得把当前30秒测试、最长8小时固定PID的旧soak或未开始的计划记成通过；只有实际观察到两侧退役重叠才能声称同时退役。

macOS十分钟混合实验已完整通过：实测10分0.131秒、247轮、988条TCP，每方向1,035,993,088字节及28,652字节EOF尾，文件77,135,872字节；完整core703.29秒、runner退出0。终端混合min/mean/max1/2/6毫秒，TCP386/414/444毫秒，文件131/298/439毫秒；所有固定资源检查通过，不能推导完全无FD增长或不存在任何泄漏。8个记录PID全部消失，实际及外层快照删除，证据/tmp/shiki-mixed-10m-macos-mxjuj2uh/evidence.json。该快照与最终391源码仅旧rotation两份测试不同，生产和mixed一致；Linux随后串行启动，仍待真实完成。

## 本批最终收尾与证据边界（2026-10-28）

08/15/16已同步：原42条要求逐字保留，29项勾选、13项待完成，Phase0–5依次为8/11、7/7、5/6、6/6、3/6、0/6。新增证据补进原条目下，不将组合要求的部分通过算成全部完成。此前过程中的“正在”描述属于当时记录，以本节及页首为准。

双平台混合实验按macOS→Linux串行完成。macOS实测10分0.131秒、247轮/988条TCP，每方向1,035,993,088字节，EOF尾28,652字节，文件77,135,872字节；Linux实测10分0.154秒、249轮/996条TCP，每方向1,044,381,696字节，EOF尾28,884字节，文件78,249,984字节。完整core场景703.29/709.77秒，两侧应用runner退出0，无选中skip或子进程race报告。混合终端min/mean/max分别1/2/6与1/2/5毫秒，TCP386/414/444与362/393/432毫秒；同一宿主前后各20次空载样本及固定8进程、FD+4、测试goroutine+6边界通过。这是有节奏的race负载样本，不代表饱和容量、SLA或全部内存泄漏验收；RSS增长如实记录，完整资源/性能门槛不勾选。

macOS8个跟踪PID全部消失，实际及外层快照删除。Linux应用runner退出0，但外部取证driver因大小写敏感匹配“No such object”未接受实际“error: no such object”，自身退出1并留下container_absent=false；保留原evidence.json。独立精确CID inspect退出1且明确no such object，docker ps -a按相同CID过滤为空，确认容器确已移除；另存cleanup-recheck.json，root也独立重复核验。Linux实际/外层快照均删除，不以改写原失败或重跑十分钟代替复查。证据目录/tmp/shiki-mixed-10m-macos-mxjuj2uh与/tmp/shiki-mixed-10m-linux-bmpvopwv，完整数据/复现与限制见[混合负载报告](15-implementation-status.md#evidence-mixed-load-validation)。

最终391源码清单/tmp/shiki-host-mixed-delivery-final-source-20261028.json已核验未变。Linux混合快照410份源码/工具hash与这391份逐一匹配；macOS409份快照仅旧rotation的harness/observation两份测试不同，生产及mixed源码一致。390源码完整macOS43包通过、E2E210.593秒；Linux42包通过、E2E186.391秒自身通过，旧rotation观察失败使整套退出1，仍记失败。最终rotation补验为双平台各3轮102.311/99.378秒，最后join细化后再整包35.033/34.154秒通过；不声称391源码重新整套全绿。构建/lint/模块校验已通过，后续仅两份测试变化并获对应补验。

最终全架构复核覆盖目录/依赖、传输与准入、持久/运行状态、生命周期/取消、文件/终端/TCP、CLI/TUI/配置、安装/产物及能力/证据，未发现新增已确证文档偏差。插件退役状态修复不改变持久期望与重启预算，内部短TTL无用户入口，默认仍12小时；取消观察器不把提前结束或普通请求cleanup当自然到期。后续继续本地/Linux容器，默认时长实际轮换和完整资源/性能另行验收；外部账号、历史发行/物理多设备、原生平台及签名分发保持待验，完整Phase0–5目标仍进行中。


## 默认时长轮换与内存诊断批次（2026-10-28，已收尾；以下保留过程记录）

新批次进行中：实现默认12小时凭据自然轮换的独立宿主验收工具，并诊断混合负载RSS增长。短预检只证明工具与业务/清理流程；实际12小时到期、完整生产内存/容量验收未通过。长跑保持生产默认TTL，无CLI/YAML/环境覆盖；诊断仅测试夹具私有通道，不新增产品pprof能力。上一批已完成证据和29/42计数保持。

本批新增确定偏差并已修复：sync client/server的OnSkipped曾附带mapping_id/project_id字段，但生产RecordLog明确拒绝非空任意Fields，调用错误被忽略，导致符号链接/特殊文件跳过提示未进入真实host日志。仅删除两个worker的Fields，保留reason code和实例归属，不放宽宿主安全白名单。真实worker→CoreAPI→RecordLog→WatchLogs的双端×symlink/FIFO四场景旧实现全部红6.451秒，修后绿7.973秒；macOS scoped race integration9.375/client5.231/filetree4.572/reconcile1.371/plugin5.226/architecture1.350秒，Linux普通用户只读快照integration12.373/client2.553/filetree2.276/reconcile1.049/plugin4.494/architecture1.181秒，无选中skip，vet通过。见[相关验收摘要](15-implementation-status.md#evidence-sync-skip-log-validation)；测试也验证正常文件仍传输/列出、跳过项保留源端且未传输。

默认时长工具已实现并通过独立架构复核：实际普通构建CLI/agent/daemon与四插件，24小时证书/Token、12小时默认凭据不变；短预检macOS76.29秒/Linux82.09秒，各15轮，应用/collector退出0，清理及源码无变化检查通过。预检明确natural_rotation_passed=false，未执行12小时到期。最终退役观察采用窗口内5毫秒采样、保守LastPresent跨最早TTL边界，否则证据不足即失败，并记录最大采样间隙；不声称精确同时退役。到期窗口只持续原终端和held TCP，文件短周期暂停，不能作为在途文件提交轮换证据。

普通/race短诊断表明只读RPC阶段持久状态数量不变，GC后活堆小幅变化；文件/TCP进一步定位port tunnel每次Read分配256KiB的热点，当前正在验证每流读缓冲+每帧独立payload的优化。此为分配压力改进，不把RSS增长直接判为泄漏或声称根因已穷尽。默认预检快照早于该后续优化，补验范围须另记。


## 默认时长工具与分配优化最终收尾（2026-10-28）

本批已收尾：修复双端sync跳过告警，完成默认12小时独立宿主验收工具及双平台短预检、普通/race内存诊断、端口分配优化和双平台混合补验。原42条要求不变，当前29/42、13项待完成。默认12小时实际到期尚未执行，完整生产内存/容量、外部账号、原生平台与正式发布仍待验收；不把预检或局部分配下降当作整个Phase5完成。

生产改动仅本批的三处：sync client/server去除被host拒绝的任意日志Fields；port tunnel将逐Read的256KiB分配改为每流私有读缓冲加每帧实际n字节独立payload。没有增加生产CLI/YAML/env/协议/pprof入口，默认12小时与重启预算不变。既有DATA上限、发送后所有权、半关闭与取消语义保留；不能直接把scratch交给Send后复用，因为运输层trace/stats可能在返回后仍读取消息。真实保留帧测试在危险复用overlay上红、正确实现绿。

默认工具最终短预检macOS76.29秒/Linux82.09秒，各15轮，应用和collector均退出0，natural_rotation_passed=false；两平台实际493文件快照记录、应用输入未变，所有记录native PID与Linux精确CID/快照清理通过。此快照早于随后tunnel优化，不能追认它覆盖优化；最终优化由下述受影响包/真实混合补验覆盖。额外checkpoint后主动TERM两平台应用143、collector1，原始中断保留，全部进程/容器/快照清理且无需强制回收；证据/tmp/shiki-collector-cancel-validation-wthrt10y。

v4普通/race分阶段profile均完整通过31.73/40.40秒：16次256KiB双向文件与32次1MiB有节奏TCP、等时idle、双GC采样，所有SHA256/半关闭/精确target接受数及六子进程退出通过。诊断是fixture入口，不冒充shipped命令长跑。32KiB分块基准每1MiB的8.71–8.76MB/op降为1.319MB/op；同诊断port-client累计分配普通381,427,504→120,065,456字节、race602,955,840→329,874,240字节。RSS并未同比降低，server race分配还存在变动；因此只认定此工作负载的客户端分配压力改善，不宣称生产无泄漏、整体容量提升或原十分钟RSS原因已全部确定。详细前后快照/profile/失败记录见[相关验收摘要](15-implementation-status.md#evidence-memory-profile-diagnostic)。

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

验收限于原生macOS agent升级/回滚及上传方向；daemon是普通TLS子进程，Claude是shell fixture，A/B是当前源码的两个测试版本。race构建不代表已捕获所有worker race报告。完整报告见[LAUNCHD-ACTIVITY-VALIDATION](15-implementation-status.md#evidence-launchd-activity-validation)。08/15/16同步29/42、13项待完成，原42条要求保持不变；外部登录、历史发行迁移、重启/登录持久化、其他平台和正式发布门槛保留。

默认12小时实验未重启，原句柄11096于14:44仍存活，14:44:39检查点37分30.572秒/219轮、recovered=false。其495文件/411份源码快照保持原样；本批新增测试不改生产输入。实际到期与恢复尾部尚未发生，Linux实际12小时仍待本轮完整退出后串行执行。


## 原生下载方向与共享验收语义收尾（2026-10-28）

原生agent下载中的A→B→A通过87.29秒（17.79/18.54秒），同一最终夹具的上传回归通过85.03秒（17.83/16.08秒）。共享exercise使用具名direction/replacedHost两个独立参数，方向只决定源/公开目标/临时文件位置，被替换宿主只决定独立直连daemon上传是否取消。Linux原四个组合保持agent+upload两次、daemon+download两次及所有原断言；原生download+agent真实运行证明两个轴没有混用。新增原子性观察：真正部分temp/staging出现并暂停传输后，公开目标SHA仍等于改源前完整内容；恢复仍验证两端最终8MiB SHA、冲突显式解决及所有残留临时文件回收。

两正常run的test/wrapper均退出0，fallback_actions/errors为空。root分别核验14个唯一PID已退出、成功root已删除、最终job/plist不存在；下载实际415份Go/proto/module与当前源码一致，两run的507文件清单逐字相同。root还核验两次边界的旧公开目标SHA链：首边界对应初始化内容，回滚边界对应上一边界完整新payload。下载未确认输入次数[1,0]，上传[0,1]；confirmed/new各1，旧TCP请求各1，符合未确认输入可到达一次、禁止重放。samepane、daemon/server worker身份、持久冲突ID/generation、暂停mapping及停止回执保留。

证据：`/tmp/shiki-launchd-download-yo6cl_oq`与`/tmp/shiki-launchd-upload-regression-gja3jybo`，各有root-independent-verification.json；共同source清单SHA256为`ff64afaefa6685c17085332b99e1f2108faf2e2f811a5f5334bbe300767f7d51`。本批没有再次运行取消，先前runner143/test1结果仍限定于当时上传夹具。Linux仅交叉编译，不将其历史活动验收追认为本次新增断言已运行。race构建仍不声称完整捕获所有worker race日志。

已完成全架构基线与本批差异复核：生产代码/依赖/协议/SDK与host/plugin边界不变，持久化/授权/生命周期/文件终端TCP/CLI配置/安装产物/能力声明保持设计；新增行为仅test/e2e和测试脚本。独立审查逐行确认两轴语义、Linux四组合、部分数据与公开目标原子性、新内容及清理断言；native编译及cleanup guard race、Linux arm64交叉编译和shell语法检查通过。完整报告与源码/日志hash见[原生活动升级](15-implementation-status.md#evidence-launchd-activity-validation)。08/15/16同步，原42要求不变、29项勾选/13项待完成。接下来当前冻结源码完整双平台race回归另行取证，不能将历史通过与专项拼接成整套通过。


## 历史完整回归与轮换流失败取证（2026-10-28；后续结果见顶部）

502文件/415份Go/proto/module冻结源码`/tmp/shiki-full-race-20261028-5h0eo7fo`以count=1、race、e2e tags运行全部包。macOS43包通过、hostrotation包失败，runner238.212秒/退出1，E2E224.579秒自身通过；Linux arm64普通用户容器44包通过，runner278.313秒/退出0，E2E199.408秒通过。15个包无测试；macOS6个、Linux9个场景skip不计对应验收完成。精确skip/失败/包时长和日志SHA见该目录summary.json。

实际Linux容器CID `c35ad017aa5d3ec137a870363490402b17e3e9dea749020e415d39d00f053910`运行时只读挂载已inspect核验，实际502文件逐份与共同冻结源码匹配；完成后结构化容器清单确认精确CID不存在、实际挂载快照已删除。共同冻结源码执行前后502文件完全一致。保留全部原日志，不将后续补验改写该次macOS失败。

失败发生于真实client插件自然到期：旧PID/socket已回收，新文件/TCP/冲突/暂停/停止回执断言已成功，但要求恢复后新鲜Stats帧时无关流已返回Canceled。server场景通过。现仅增强hostrotation失败观测（父/流context状态、时间与服务器快照），不忽略Canceled或重新打开流；独立client三次race109.866秒通过仍不足以解释整套失败。正在隔离源码副本追踪共享pool首个invalidate及probe/parent期限；内部探测超时、远端取消与本地网络错误尚为候选，未认定根因或声明修复。

相关Phase 2生命周期验收暂恢复待验，当前28/42、14项待完成；原42要求不变。本批尚未收尾，生产代码没有因猜测修改，实际12小时原冻结实验继续运行，不与本轮诊断共用进程或状态。

## 真实签名包与自动tag发布批次复核（2026-10-29）

- 目录/依赖：发布工具属于scripts/workflows，预构建包输入只在e2e测试驱动；插件业务、宿主和SDK边界保持，生产Go/proto/module未改。
- RPC/能力/授权：没有扩大产品能力；按用户新授权以tag推送替代人工发布审批，签名环境仅v*tag，签名与contents-write发布任务分离。
- 状态/原子性/revision/去重：产品持久化未改；发布保留同run/source草稿，完整22资产核验后公开，冲突不覆盖；不声称能抵抗任意外部并发编辑的事务原子性。
- 进程/生命周期：真实LaunchAgent及systemd包验收保留原崩溃/升级/回滚断言，目标helper和新装缺口已补，所属服务/PID/容器正常清理；原697运行不触碰。
- 文件/终端/网络：安装验证认证归档原始payload；文件hash、冲突/暂停、TCP半关闭和不重放按限定场景保留；fetch拒绝重定向，GitHub下载后独立verify。
- CLI/安装/产物：无产品命令变化；tag格式、固定来源、22文件及发布说明明确；源包与新tag重建包不视为同字节产物，历史正式迁移无对象。
- 证据/声明：本地38工具测试、真实签名smoke及包安装各自范围独立；旧A-helper窄证据、Linuxfresh夹具失败保留。首次远端tag、公开下载及外部/平台/长期资源门槛未提前勾选。


## 首个全自动公开发行的七类收尾复核（2026-10-29）

实际tag `v0.0.1-alpha.1` / `f0b5f83dce599c8e0d715bda5e7806a0ef6aaa72`的Release运行40895007350全部成功，22公开资产下载认证与两平台有限暂存验收通过，详见[公开alpha证据](15-implementation-status.md#evidence-public-alpha-release-20261029)。本次执行无生产源码变更；独立审阅按00–14契约和已实现架构核对：

| 范围 | 本批复核结论 |
|---|---|
| 目录/依赖/SDK与host | 插件业务保持plugins/sync、plugins/port，host与SDK保持internal/plugin、pkg/plugin；无进程内回退或依赖方向变化。 |
| RPC/准入/版本能力/授权 | 产品传输、能力和认证未改；发行精确tag/source/master校验、签名环境tag限制与发布写权限分离实际通过。 |
| 状态/原子性/revision/去重 | 产品持久化未改；草稿完整资产/digest复核后公开、冲突拒绝覆盖；不外推为抵抗任意外部编辑的事务保证。 |
| 进程/启停/取消/恢复 | 新归档仅version/help/stage-only，无服务激活；旧92cafaf真实服务证据独立；本轮临时提取/容器清理，不动原697长测。 |
| 文件/终端/网络/限制 | 归档、payload、pin与签名完整验证；断网普通用户容器执行，GitHub重定向由下载工具处理，不放宽产品fetch的拒绝策略。 |
| CLI/TUI/配置/安装/产物 | 四目标固定版本与commit，24二进制静态目标正确；BuiltAt仅4个保留变量可验证，20个优化删除；六次暂存无current/config/state激活。 |
| 行为证据/文档声明 | CI及实际公开验收分别记证，未把skip算通过；新tag、旧签名候选、历史开发归档证据分开，外部/平台/长稳/其他分发未提前关闭。 |

08/15/16当前结论与并行表已同步为31/42、11项待完成，仅关闭两条已具备实际公开证据的原要求。更新README、11、18、19中过时的“未公开”当前叙述，保留历史失败/来源和冻结tag内发布说明；不改设计要求来迁就现状。


## Linux默认12小时终态与文档复核（2026-10-29）

[原697源终态](15-implementation-status.md#evidence-default-linux-final-20261029)已实际独立核验通过：12h30m3.603s/4368轮、四角色自然替换、1803.013s恢复尾部、11所属PID清理、应用/collector退出0及精确CID缺席。再次只读核对原始证据、事件和五份hash，未改运行源码、日志或预算。

七类复核：目录/依赖及SDK-host-plugin所有权未变；RPC/准入/版本能力/授权未变；持久/运行状态、原子性/revision/去重未变；进程/取消/恢复以原冻结契约核验、无新实验；文件/终端/网络语义及限额未放宽，TCP无重放与资源门禁按原证据判定；CLI/TUI/配置/安装与发行产物未变；行为证据严格区分原697源与公开alpha，不把测试harness goroutine或短时采样当生产无泄漏证明。旧Linux578失败、旧macOS495成功范围保持。

修正08/15/16当前表与18/19中仍等待原697终态的叙述，保留标明时间的历史中期记录；原42条要求文字/顺序及31/42不变。下一步先分析已有完整资源记录、审查当前alpha的覆盖差异，再选择有判别力的针对性验收；本批没有追加长跑或操作既有环境。


## 资源/密钥/GPL与Homebrew准备批次复核（2026-10-30）

证据见[合并记录](15-implementation-status.md#evidence-alpha2-distribution-preparation-20261030)与20/21/22。独立审查并行核对更新器、公式信任链和许可证身份；LICENSE归档读取改为对实际打开FD及路径身份核验，防消费时路径替换，相关负例通过。真实Homebrew暴露的staging附加文件和LICENSE搬移均由最终libexec/bundle布局解决，没有放宽14成员内容/hash或修改安装helper。

七类结论：目录/SDK-host-plugin依赖保持，新增Formula与发布工具归属已同步07；RPC传输/能力/授权不变，签名key与发布写权限分离；产品状态/revision/去重未改，Formula使用单文件SHA条件更新、同版冲突拒绝和有界重读；进程/取消/恢复仅按原资源证据判断，brew不激活服务；文件/终端/网络语义与限制未变，发布/安装完整验签，测试skip不算通过；CLI/TUI/配置及受管安装保持，包管理与服务激活/升级明确区分；资源RSS不当heap泄漏证明，临时钥撤信不冒充生产撤销，原生macOS brew前置失败不算通过。

用户明确要求GPL-3.0-only及Homebrew首发alpha.2，当前正文/README/文档和未来归档按此更新；alpha.1 MIT冻结保持，Formula按已认证实际归档LICENSE识别。08/15/16同时同步、31/42不变；实际新tag/公开分发尚待，不提前关闭复合条目。
