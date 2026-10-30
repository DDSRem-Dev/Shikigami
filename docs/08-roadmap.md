# 开发路线图

**当前进度（2026-10-31）：37/42，剩余5个复合门槛。** c1e5ece同源的macOS Intel/ARM、Linux双架构及真实WSL2完整产品/服务矩阵全部通过，平台兼容条目已勾选。官方节点key到期后的同身份重新认证与业务恢复已有证据；完整认证、主机生命周期/外部身份升级、长稳资源及最终支持声明仍待。三核受限并行与异步watch收尾测试已本地验证，后续CI验证这一执行策略，详见[23](23-tsnet-external-acceptance.md)与[26](26-native-platform-acceptance.md)。

**当前CI：** [41119194452](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41119194452)绑定c1e5ece，已completed/success、13/13任务通过。主代理独立核对五平台全部必需阶段、同源、清理及无未完成项；Intel旧失败用例1.88秒通过，旧瞬时原因未直接复现。实际Mac ARM节点测试合计375.41→43.57秒，Epoch40.70→1.06秒、Address36.71→1.24秒，原45秒预算保持；首次冷缓存与后续warm轮分开记录，四原生warm均已保存。最后一批只调整三核CI资源和异步watch测试观察，生产Go/依赖保持c1e；45顶层node专项、同步10次、权限专项、27工具及七类审查通过，完整新CI待提交。三份进度已同步37/42，原42条文字/顺序保持；预约key、冻结长测和认证身份未改。

以下是按各批源码范围保留的历史记录，当前状态以本摘要和原条目的最新部分进展为准。

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

**状态：Phase 0–5 正在实施，尚未完成全部阶段验收。** 当前证据见 [15 实施记录](./15-implementation-status.md)。 本文是排期与阶段范围的唯一来源。原 14 周是未经验证的粗估，新增本地 agent 后不能保留为交付承诺；以验收门槛推进，完成 Phase 0 的原型后再估算工期。05/12 的目标体验不能替代逐项验收；已有实现与缺口见 15/16，GUI 仍为后续范围。

<!-- active-parallel-work:start -->
**当前工作与验收边界（2026-10-30）：**

| 工作 | 当前状态 | 后续条件 |
| --- | --- | --- |
| 原697源Linux默认12小时 | 终态通过；740组5920资源样本/12代际及到公开alpha源码差异已完成分析 | 生产heap/object归因、当前候选长期覆盖仍待，不从RSS推断无泄漏 |
| 发行密钥轮换/撤信 | 临时测试密钥12步演练及异常清理通过，真实签名身份保持 | 显式旧钥仍可认证旧钥签名；在线撤销未提供，生产身份操作需独立记录 |
| GPL与Homebrew alpha.2 | 全自动Release/Formula、22公开资产认证与四目标原生真实tap完整链通过，见22 | 分发条目已关闭；完整产品平台与用户macOS27工具环境限制单列 |
| 完整交付门槛 | 当前36/42、6项待完成 | 外部账号、完整产品平台、主机生命周期与长期资源依路线图推进 |

36/42是原始复合条目的验收数，不是代码完成率；历史批次中的旧计数保持当时范围。独立工作并行推进，不重复已通过检查。
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

## 最近进展与剩余条件

**2026-10-31两步推进：** 完整原生四目标与实际WSL2产品/维护矩阵已接入CI，17项工具检查及静态检查通过，首轮CI41090262712与42eb93e轮已有真实失败，完整平台尚未通过，见[26](26-native-platform-acceptance.md)。节点显式Self.Expired跨BackendState分类及AuthURL出现时旧授权退役已修复，原生node整包race40项无skip和vet通过；官方干净候选Mac首轮登录超时后第二轮两端已online，失败及后续验证见[23](23-tsnet-external-acceptance.md#独立认证环境重建与状态修复2026-10-31)。重启预检发现本地节点未入网被永久标成REMOTE_INCOMPATIBLE，已修为本地auth_required等待并重验远端身份，定向race/vet、agent整包及干净m7官方节点同身份自动恢复通过。新专用认证环境保留到相关认证/安装升级验收结束；不清除仍有依赖的节点。VPS进程更新后另一处连接停在REMOTE_TRUST_OR_PROTOCOL_REJECTED，当前保留失败并调查。原42条、36/42及六个复合门槛保持。

以下按实际完成范围列出进度；部分工作完成也必须同步，不等待整个复合条目满足才记录。

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
| 默认凭据轮换 | macOS原12小时通过；旧Linux失败保留，697源Linux12h30m/4368轮、四角色轮换/恢复尾部及清理已独立通过 | 完整已有记录与当前alpha覆盖差异分析已完成；生产heap/object归因及当前版本长稳仍待 |
| 端口分配与混合负载 | 单连接限流隔离修复、双平台五包race/vet、普通32轮完整混合负载及默认128准入/单槽恢复/清理通过 | 仅限定源码/环境/负载证据；资源稳定、生产长稳及外部网络仍待 |
| 服务安装与升级 | 原生LaunchAgent、真实systemd生命周期及安装529补验通过；同容器二次PID1启动与开发schema升级/拒写/有限恢复后撤销Token实际RPC拒绝通过 | 自动状态恢复并未提供；外部身份跨升级、其他原生平台及重启/登录持久化待验收；两公开版本同schema升级/回滚已通过24，不能称历史schema迁移 |

## 阶段概览

| 阶段 | 交付范围 | 已勾选/总项 | 依赖 |
|---|---|---|---|
| Phase 0 | 工程、协议、本地 IPC/agent 设计和高风险原型 | 10/11 | 已确认范围 |
| Phase 1 | CLI + agent + daemon、TLS 直连、Project/Session、tmux 终端 | 7/7 | Phase 0 |
| Phase 2 | 内嵌 tsnet、双端插件、基础双向文件同步 | 6/6 | 核心生命周期稳定 |
| Phase 3 | TCP 转发、监控诊断、后台运行体验 | 6/6 | 插件路由和身份明确 |
| Phase 4 | TUI、配置快照、模板、多设备打磨、安装升级 | 4/6 | 关键故障场景验证 |
| Phase 5 | 首个正式版本验证与发布 | 4/6 | 全部首版门槛 |
| 后续 | GUI、WSS、文件快照等独立迭代 | 不计入42项 | 首版稳定及再次评审 |

条目全部勾选也不单独构成整阶段或发布验收；外部环境与跨阶段交付门槛仍按各条记录。

<a id="剩余11项本地工作与外部门槛"></a>
## 剩余5项：本地工作与外部门槛

37/42是原始复合条目的验收计数，不是代码完成率。以下不拆改原要求、不提前勾选；用于说明为什么已完成修复和补验未必增加勾选数。本批发现的认证生命周期缺口继续修复和复验，独立工作按下列剩余条件推进；不等待整个复合条目关闭才记录局部进度。

| 原条目 | 本地/容器已具备或仍可推进 | 关闭整项仍需 |
|---|---|---|
| Phase 0 tsnet认证原型 | 官方双端登录、进程身份保持、key首用/复用拒绝与交互取消已验；peer撤销旧流回收已有本地与官方复验证据；新增官方node key到期后的同身份认证/业务恢复通过，见23 | 控制失联分类、最终agent撤销/重新认证、未使用key撤销及审批等待/移除/批准已验；自身明确撤销/管理员拒绝分类、未使用Auth Key自然到期及其余11认证矩阵 |
| Phase 4 安装器与服务 | 原生LaunchAgent、容器systemd、二次PID1启动及未登录system tailscaled非干扰已验；本地启动策略配置展示已完成双平台和真实systemd四态验证 | 外部账号、原生VPS及重启/登录持久化 |
| Phase 4 升级回滚与卸载 | 原开发schema迁移/拒写/恢复证据保留；公开alpha.1→alpha.2实际活动升级/回滚、重复安装/卸载及兼容矩阵双平台通过，见24 | 外部登录状态跨升级、主机重启/重新登录及其他原生平台完整场景 |
| Phase 5 长稳、资源与性能 | macOS原12小时通过，Linux578源失败保留；697源Linux12h30m/4368轮及恢复/清理已独立通过 | 完整已有记录与源码差异已分析；生产heap/object归因、当前候选长时/资源与负载证据仍待，不能跨源码追认 |
| Phase 5 支持声明 | 持续保持实现/验收/外部门槛分开 | 所有相应支持条件实际通过 |

成功上传流释放、端口准入隔离及普通混合负载、观察夹具修复与同源双平台预检均已取得各自证据；macOS原默认时长验收已通过，Linux578源失败保留，697源新Linux默认时长与终态核验已通过，宿主清理修复及同源双平台相关验证已完成。资源与其他可执行本地验收按剩余门槛推进。等待类工作与独立实现/审查并行，隔离源码/状态/证据；不把整张路线图串行阻塞在长跑上。外部门槛保留待验，不以反复运行已通过的本地测试替代；全套回归仅在代码变化、失败或明确未解问题需要时重跑。

## 剩余工作分工（2026-10-30）

2026-10-31更新：当前5项是复合验收门槛，以下工作包（含本批已关闭的局部工作）与原项多对多对应，不新增原始checkbox、不将37/42当代码完成率。完成概览和详细执行条件见[19计划盘点](19-local-acceptance-closure.md#plan-inventory-20261030)。原计划盘点未启动新实验；后续官方tsnet实际运行及当前剩余条件见23，尚未发布包含登录修复的新版本。

| 我可自主推进 | 对应原项 | 结束条件/限制 |
|---|---|---|
| 本批已完成：公开alpha.1/alpha.2实际包升级、回滚、重复安装及兼容组合 | Phase0恢复/版本；Phase4升级回滚；Phase5审查/分发 | 双平台真实服务与混合版本均通过，详见24；同schema不冒充迁移，服务升级不冒充Homebrew跨版升级，tsnet外部身份跨升级另验 |
| 按新证据继续调查生产heap/profile及所有权；本批18产物复核及慢流取消缺口已处理 | Phase5长稳/资源；审查 | 先明确对象、持有者、释放点和可证伪假设，再做必要诊断；未发现新根因不改代码，不循环追加短测 |
| 固定当前候选的长时/恢复/性能验收方案并执行必要场景 | Phase5长稳/资源；审查 | 明确新Stop等触发路径、默认凭据轮换、恢复尾部、资源指标和清理；历史12小时不重写，时长通过不等于无泄漏 |
| 按证据矩阵补关键恢复/认证/数据分支与可用原生CI平台专项 | Phase5审查；平台；Phase4运维 | 只补实际缺口或变更影响；先核对重复Stop、超过宿主预算排空、提交时进程退出等与契约的关系；现有runner可用范围内执行，交叉编译不算原生 |
| 跟踪已启动CI、处理实际失败，维护平台支持与验收声明 | Phase5审查；支持声明 | 对应源码/平台/证据，不能由本地通过关闭用户环境门槛；最终声明依赖其余条件 |

| 需要用户提供条件 | 对应原项 | 用户配合后可由我执行的工作 |
|---|---|---|
| 已提供官方tailnet、Chrome控制台、独立测试key和审批/撤销授权；仍需真实Claude登录及尚未具备的认证场景条件 | Phase0认证；Phase4登录/运维；Phase5安全/支持 | 其余认证矩阵和Claude闭环；未使用key撤销、审批等待/移除/重新批准已验，自然到期任务已调度，无需再次索取同批key或授权 |
| 已提供原生Debian amd64 VPS；主机重启/系统安装及重新登录范围仍需确定 | Phase4服务/升级；Phase5支持 | 当前已运行普通用户隔离daemon；开机/登录启动、自然网络切换和主机重启恢复仍待；进程重启不能替代 |
| Mac＋VPS双客户端已验；原生Intel/ARM与实际WSL2完整产品平台已由CI补齐 | Phase5平台已关闭；最终支持仍待其他门槛 | 不再要求用户另行提供相同平台；物理重启登录、外部账号/Claude及未验OS版本仍按对应条目处理 |

已结束的原12小时、完整资源记录分析、隔离密钥演练、alpha.2发布/Homebrew自动化及本批双平台公开包验收不再列为等待。纯文档提交9d28dd6的[CI40923247799](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40923247799)及54feb47的CI40924428349现已确认成功，和Release40912019720分开记录；原盘点批次未启动新12小时任务；当前c4c5a56双平台正式观察现已启动，见文首。

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
- [ ] 完成 11 的 tsnet 登录/一次性 key/身份持久化/认证失败/NodeControl 原型，记录 SDK/OS 版本与脱敏结果。
  - 2026-10-31新增：官方节点key单次短期到期、旧终端/TCP回收、CLI9/agent8/VPS7干净产品重新认证后同IP/daemon/凭据自动恢复及新终端/双向文件/TCP已验；SDK组合到期分类与有效attempt等待修复有本地race证据，干净重启后SDK缺旧期限的显示边界独立保留。预约未使用Auth Key自然到期仍为15:56，完整认证矩阵不提前关闭。
  - 固定SDK、本地testcontrol/DERP和NodeControl已有证据；官方双端登录、一次性key首用/复用及无效key拒绝、进程身份保持与交互取消已验。官方节点移除暴露旧流回收缺口；入站/出站peer代际、每秒本地校准和共享5秒观察预算已修复，本地SDK/生命周期回归通过，精确源码与测试范围见[23](23-tsnet-external-acceptance.md#peer撤销修复与复验边界)。前次重新认证及clean最终构建重启/应用重连已通过；再次官方移除后的附件回收与主节点保持正常已验证，本次认证与clean恢复、控制面断网保留通道和独立connect已验证；独立未使用key撤销及审批等待/移除/批准已通过；自身明确撤销/管理员拒绝分类、key自然到期及其余认证矩阵保持待验。
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

证据：`test/e2e/core_test.go` 的真实 CLI→agent→TLS daemon→tmux，以及 `test/integration/admission` 的受限 Token/观察者/撤销测试。macOS/Linux 已有本地闭环结果；Claude 使用可控 shell fixture，真实账号与完整终端视觉验收仍待完成。

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
- [ ] 按 11 实现安装器、Linux systemd 服务、macOS LaunchAgent/Linux 用户服务。
  - 11要求的启动策略展示已补齐：shiki config startup只读区分CLI按需与受管登录/开机策略，未知不猜测启用；双平台受影响race/vet及真实systemd启用/禁用运行中/错配置/恢复绑定四态通过。仅配置观察，不替代实际登录/重启，见[启动策略验收](15-implementation-status.md#evidence-startup-policy-validation)。
  - 2026-10-29实际锁定tailscale.com v1.98.10源码构建的系统tailscaled在21边界保持原进程/unit/config/socket/state，Shiki双角色安装/升级/回滚/卸载/重装通过；另一个UID1001内嵌tsnet启用未登录返回node_needs_login/退出3，独立252B state与访问系统socket/state实际EACCES通过。正常poweroff及精确容器清理独立复核。系统state是2字节空JSON，不能证明已有认证身份保持；不是官方二进制或已认证双节点共存。见[共存报告](15-implementation-status.md#evidence-tailscale-coexist-validation)。
  - 2026-10-29第三轮持久卷二次PID1自动启动验收通过：同容器正常poweroff后重新start，daemon与user-agent无需CLI拉起；身份/Token/映射/revision/基线/冲突/paused/forward保留，既有路径自动双向、新路径首次显式方向、fresh TCP及配置快照创建新会话通过。双host/四worker当前产物与UID/代际、两次正常退出与精确CID/home卷清理独立核验；前两次夹具失败保留。仅容器PID1重启，不代表物理VPS或原生登录；见[二次启动报告](15-implementation-status.md#evidence-service-boot-validation)。
  - 安装器、服务渲染、NodeControl/bootstrap、内嵌产物及隔离smoke已实现；macOS27 arm64原生LaunchAgent与Linux arm64容器真实systemd/systemd --user均通过安装、实际应用就绪、停启、崩溃自动恢复、升级/回滚和卸载保留数据。保留未勾选：11的完整安装要求还涉及外部登录/真实账号、可信发行来源、原生VPS和重启/登录持久化。
  - 本批按11补齐停止旧服务前的磁盘预检、停止后新清单复核和备份父目录fsync；双平台整包race、真实16MiB tmpfs及fsync跟踪通过。529文件快照原生上传升级/回滚90.41秒、真实systemd 13次边界补验通过，见[安装空间验收](15-implementation-status.md#evidence-install-space-validation)；不把历史下载/取消结果标成新源码验证。
- [ ] 重复安装/升级/回滚、自定义 config、非交互登录、依赖检查和卸载保留数据。
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
  - 2026-10-31关闭：c1e5ece的CI41119194452全13任务成功；同源原生Mac双架构、Linux双架构、真实WSL2完整race/E2E/终端及服务维护、活动升级回滚、来源一致性和清理均通过，主代理已独立核验。证据见[平台验收闭环](15-implementation-status.md#evidence-native-platform-c1e-20261031)与[26](26-native-platform-acceptance.md)。仅覆盖记录的runner/内核与本地产品/服务范围；外部账号、真实Claude、物理重启登录/自然切网及长稳仍在其他条目。后续三核调度候选只改CI/测试，保持生产Go/依赖，实际执行策略由新CI另验。
  - 关闭前历史：macOS ARM与Linux ARM容器既有证据保持。原生Intel的bf2e645（树同c83c95a）与58806be均已通过完整产品、终端及LaunchAgent生命周期/活动升级下载上传；实际tmux3.7c，不覆盖3.8。58806be的Linux amd64/arm64与真实WSL2产品/E2E/embedded/terminal通过，systemd维护仍失败。c10852f的真实WSL2完整产品/systemd维护已通过，两Linux产品链通过但首次daemon激活仍失败；已实际复现/opt0777祖先权限拒绝，继续改用/var/lib及前置校验。c108的Mac双架构全部阶段通过，tmux3.8实际运行已验；e2f的WSL2在/var/lib新布局完整通过，两Linux产品测试新失败后未进入维护。退出事件/真实HTTP拒绝夹具已修并通过专项race，独立维护准入的27项工具检查通过，当时下一轮完整平台仍待；保留arm64旧useradd超时未知原因。896b的两Linux及WSL完整矩阵现已通过，包括/var/lib正向维护；Mac Intel登录准备出现单次超时，未复现，Mac ARM运行中。已确认并修复gVisor的ARM64 race CAS错误，固定上游可构建修复快照，本地44顶层node race、CAS回归、全包build/vet与漏洞扫描通过。新依赖尚需完整平台验证；上述为关闭前历史过程；最终c1e同源完整矩阵已通过，详见[26](26-native-platform-acceptance.md)。
- [ ] 长时间后台运行、资源泄漏、故障注入、安全边界与性能回归。
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
- [ ] 经验证后才能把“设计目标”改成“已支持”，不得预先勾选完成。

## 后续迭代

1. GUI：Wails + React/TypeScript + xterm.js，通过本地 agent；版本、平台打包、托盘和通知需专门验证。见 12，不计入首版工期。
2. 企业代理/WSS 隧道：专用双端适配和代理测试，不能以 Connect-Web/gRPC-Web wrapper 冒充原生双向流。
3. 同步增量优化、三方合并、独立目录/worktree 文件快照与恢复。
4. 终端历史录制/回放、Claude 官方对话恢复集成。
5. 团队多租户、访客沙箱、插件市场、云配置等另行设计安全边界。

后续功能无既定发布日期，不因 UI 草图或协议预留就承诺已可用。

## 最近完成批次与当前工作

当前进度以文首及“剩余6项”表为准：36/42；官方网络、Homebrew渠道及本批恢复/版本/多设备与有限审查原项均按各自证据关闭。官方节点移除暴露旧流回收问题，peer生命周期修复及最终5秒状态观察预算已取得本地回归证据，前次重新认证及clean最终构建重启/应用重连已通过，随后官方移除复验已确认旧附件回收及主节点不受影响；控制失联分类、认证恢复与clean控制面断网复验已完成，自身明确撤销分类及完整认证矩阵仍待，见23；没有因此关闭Phase 0全认证矩阵或Phase 4主机重启条件。原macOS12小时和697源Linux12h30m及终态核验已完成，旧Linux失败保留，不能追认为包含本批修复的当前版本长稳。原有插件清理、系统共存、跨schema恢复和自动发布证据保留各自源码范围。

当前c4c5a56两正式长稳任务与明日真实key到期任务均有独立证据和结束条件，运行中不记通过。耗时验证与独立实现、七类审查、证据分析和文档同步并行；冻结每组输入、隔离状态并限制构建并发。原Linux578失败保留，后续697源恢复/尾部已通过；旧源码结果不外推为当前源码通过。

2026-10-31官方节点key单次短期到期已实际发生：官方回读期限后SDK进入NeedsLogin但Self.Expired=false，旧终端和TCP已关闭。已修复组合证据分类（仅NeedsLogin、已知self公钥及非零期限且超过期限1秒，或显式Self.Expired），11项定向race与vet通过；CLI对有效expired/uninitialized尝试继续原有界等待，最终整包race42顶层/183含子项、28.607秒、零skip通过。干净m8重启后SDK不再提供旧期限，实际为needs_login；CLI9仅替换前端，01:34 Chrome重新认证后同IP/daemon/凭据自动ready，终端新回显、文件双向收敛及新TCP精确回显通过；不把该恢复追认干净候选到期分类已现场观测，详见[23](23-tsnet-external-acceptance.md)。CI优化已实现四原生并发、按资源选择p1/p2、隔离用户复用私有缓存副本及安全失败行号；17项工具检查和独立七类审查通过，实际收益待新CI，旧失败保留，见[26](26-native-platform-acceptance.md)。原42条及36/42保持。


2026-10-31追加平台修复：已适配tmux3.8对已退出pane返回空PID的实际格式变化，仅dead缺PID记0，live缺PID仍拒绝；退出证据与精确tmux身份保持，PID0不进入信号/活跃归属。确定性修前红，本机3.7b两包race65顶层零skip/fail及vet、独立七类复核通过；真实3.8平台须待新CI，不用本机旧版本覆盖替代。Mac排队实测已降至Intel5秒/arm64 2分27秒，整轮耗时仍待，详见[26](26-native-platform-acceptance.md)。原42条及36/42保持。


**2026-10-31 CI修复与性能收敛：** 首轮固定源原生Intel（tmux3.7c）完整产品及LaunchAgent验收通过；58806be轮Linux双架构与实际WSL全产品/E2E/embedded/terminal通过，两Linux全包race约17–19分钟降至9分钟。该轮Mac的tmux3.8空PID与GOMAX1期限失败已分别取得红绿和修复；systemd专属根曾移至/opt，0755隔离容器的生产路径对照通过；后续发现托管镜像可将/opt设为0777，修复并未覆盖这一环境，现补/var/lib及逐层前置校验。arm64 useradd超时仍待原因，新固定阶段诊断保持60秒预算。构建参数对齐实测185.545→152.838秒且六二进制/双manifest完全一致。新增按角色/平台/来源隔离的partial/warm缓存，16缓存、18平台、8构建元数据工具检查及独立七类审查通过，远端新缓存和修复后整轮结果待CI。详情及失败/源码范围见[26](26-native-platform-acceptance.md)；同身份官方到期恢复见[23](23-tsnet-external-acceptance.md)。原42条及36/42保持，受保护长测和预约key到期未修改。
