# Mac 与 VPS 双客户端验收

2026-10-30完整业务矩阵已通过，最终OS资源回收已独立通过。按08原条目验收多设备路径映射、终端接管、后台冲突及06§9/12的恢复边界；不新增或拆小原42个checkbox。

## 隔离环境与来源

A为原生macOS27.0.1 arm64、UID501；B为已有Debian VPS原生amd64上的新临时普通用户，独立HOME/配置/凭据/本地目录。B同时运行专用daemon，项目目录与B本地目录分开。daemon只监听VPS loopback TLS，A通过专用SSH TCP转发连接并验证localhost证书与应用Token；B直接访问同一个TLS服务。SSH仅调度与承载加密字节，不转发agent UDS或冒充本地UID；不复用既有tsnet身份、不改系统防火墙、不安装服务或系统软件。

初始六程序/双插件manifest固定于`c4c5a56fe6fe104f9cb94df5a6bb3b918412c1f5`构建，版本`v0.0.1-alpha.2-local.multihost.1`。双端测试使用其中CLI/agent/daemon及插件，短时installer不部署。源码690文件清单与两平台二进制hash独立记录；这是候选构建，不是已公开alpha.2。终端提示缺陷修复后，仅CLI替换为固定源码的`v0.0.1-alpha.2-local.multihost.2`，其余产品程序仍为上述c4产物；不得把新CLI结果追认为旧公开包或长期运行包含该修复。

[test/diagnostics/multihost](../test/diagnostics/multihost/)为显式验收工具，实际用户业务经过真实CLI/agent/daemon RPC；条件提交原语另用本轮独立Token/CA经真实HTTP2/TLS直接调用daemon文件RPC，先认证协商并校验daemon ID，不扩大前端代理白名单。配置所有可写路径须在各自0700根目录；只读sync.snapshot仅提取指定mapping/path的baseline/operation及staging摘要，拒绝凭据路径。helper按UID/PID/启动身份/可执行文件核验自己启动的进程，不停止共享tmux服务器。controller清理与操作系统账户/目录/SSH清理分别核验；前者成功不代表后者完成，也不代替所有历史worker退出检查。

使用明确标记的shell fixture验证终端，匹配`MULTIHOST_REPLY:`防止把输入回显误当实际远端执行。本批不验证真实Claude、tsnet P2P/DERP、物理主机重启、Intel Mac或WSL完整平台，不把VPS称为裸金属设备。

## 已遇到的问题与修正

| 轮次 | 实际结果 | 处理 |
|---|---|---|
| 1 | 12.40秒失败：连续terminal.open触发既有100ms重绘节流；两个helper正常清理 | 仅ResourceExhausted按150ms最多5次重试，其他错误不重试；不放宽产品限额 |
| 2 | 27.37秒失败：真实CLI显式接管成功后仍显示read-only observer；正常清理 | 修复产品接管模式提示，见下文 |
| 3 | 45.14秒失败：修复后的真实CLI观察/接管提示及Ctrl+Q均通过，Dashboard夹具过早发q，最终超时；正常清理 | 改为等待真实界面/目标面板/所需内容，再发送退出；不使用固定延迟猜测就绪 |
| 4 | 35.61秒失败：双端Sessions与终端阶段通过；CAS负例误用不支持的ctrl-x先被参数校验拒绝 | 改为契约允许的ctrl-]，继续使用旧revision验证Aborted；不更改产品错误优先级 |
| 5 | 39.06秒失败：终端、配置CAS及不同路径并发通过；夹具经前端代理调用raw StatFile被PermissionDenied拒绝 | 按13保留前端白名单；条件提交原语使用独立认证TLS文件RPC，普通同步继续走agent/真实插件 |
| 6 | 61.40秒失败：.3完整产品的普通上传SIGKILL恢复、基线核对与B流保持通过；冲突解决SIGKILL后重试返回Canceled | ListConflicts可返回扫描前持久化旧冲突；改为等待新worker同revision的active/conflicted，incomplete日志必须由后台恢复消除，再判断结果；不重试Canceled写请求 |
| 7 | 85.84秒失败：两类SIGKILL完整恢复和条件写竞争均通过；重连旧attachment接管在agent返回PermissionDenied | 依13前端所有权先拒绝旧附件，夹具原NotFound预期属于后续daemon层；改为精确PermissionDenied，不放宽产品权限 |
| 8 | 160.95秒失败：离线冲突、过期generation拒绝、KEEP_BOTH及真实Sync界面通过；副本碰撞前置下载已被5秒父截止终止 | 8MiB双向32KiB/25ms限速仅下载理论即≥6.4秒，误超13/14既有短RPC预算。碰撞专项改1MiB，保留同名注入/无覆盖断言；独立两类SIGKILL仍8MiB。不延长产品截止；异步RPC失败立即显示 |
| 9 | 84.65秒失败：普通上传恢复通过；冲突解决重启后GetSyncState在恢复writer后等待超出单次5秒查询截止 | 先只读journal等待incomplete operation消除，再查询live状态；仅只读查询DeadlineExceeded在原60秒恢复上限内重观察，不重放写、不延长产品预算 |
| 10 | 124.52秒失败：前八阶段通过；1MiB人工慢桥仍触发ResolveConflict默认5秒父截止，A映射归档清理失败但两helper退出清理通过 | 准备仅碰撞专项使用显式90秒诊断前端预算（产品本来允许caller指定），保持helper更早截止、其他默认前端/CLI与全部内容保护断言；不改生产默认 |
| 11 | 149.37秒失败：前九阶段及副本碰撞完整通过；撤A后旧终端/TCP已关闭，但新GetProject落在pool退役窗口返回Canceled，A归档因撤权后pause须验证远端而失败 | 先在凭据有效时暂停A映射，再撤权；观察ListServers稳定auth_required/CREDENTIAL_REJECTED后精确要求Unauthenticated。cleanup对已paused映射直接显式归档；不把Canceled记认证拒绝、不声称A同步由撤权终止 |
| 12 | 157.31秒通过：10阶段全部完成；两helper及controller正常清理 | 两类8MiB强杀、三组条件竞争、离线/冲突/TUI、1MiB副本碰撞及撤A保B全部闭合；OS资源独立回收见下文 |

每轮重新初始化专用状态并发行新的独立测试Token，保留失败结论；不在失败后重用已撤销凭据。各轮events及原始临时证据曾独立保存在本轮closeout目录；现已按用户要求删除，未归档进docs，仅保留本文脱敏结果与固定hash，不再声称旧路径可访问。

## 终端接管提示修复

原`internal/client.Terminal`在TakeControl返回成功后，仍把READY中的observer状态传入BeforeRaw，导致真实CLI模式提示错误。现在等待同一attachment有序输出中的CONTROL_CHANGED及新的control_revision，再报告controller；unary成功本身不直接放行输入。确认前输出最多64帧/1MiB，随后保持原顺序交给既有事件循环，输入代次、旧输入作废和当前TTY尺寸同步保持。消费后释放缓存引用；READY和控制确认超时回调在Stop失败时join，避免迟到回调取消健康终端。确认等待最多5秒且可取消，错误/EOF/崩溃/错序/重复READY/旧revision/溢出不进入raw。

真实PTY+Connect回归先红后绿，覆盖RPC早返回、延迟控制事件、前置输出顺序、提前输入不重放及窗口尺寸同步。最终客户端整包race17.257秒；CLI24.145秒、UI2.859秒、架构1.375秒race通过，相应vet通过。两平台CLI实际构建并部署；第三轮真实CLI已复验两种模式提示、退出0及原pane保持。此处为第3轮阶段记录；完整矩阵随后已在第12轮通过，最终CI41051011635十任务均成功。

## 有限验收矩阵

第12轮已实际完成：独立身份/映射、同机配置CAS与重叠拒绝、同/异路径条件提交和同名新建、A离线/B写入、修改与删除、相同内容采纳、第三次修改失效旧generation、预览与KEEP_LOCAL/REMOTE/BOTH、冲突副本碰撞保护、无前台冲突持久化、真实Sessions/Sync/Ports展示、同时接管唯一胜者及只读拒绝、重连新attachment、独立loopback转发、仅A撤权而B继续工作。

两类硬崩溃要求先观察远端真实部分临时文件与仍完整的公开目标、本地prepared记录/完整staging及旧baseline，再SIGKILL agent，确认旧host/worker退出、旧listener不可连接，B终端/TCP保持。新owner扫描恢复后核对完整内容、远端revision对应新baseline且无pending operation。缓冲可能使commit_state为possible，不强行称incomplete；不把该部分上传场景冒充提交后ACK丢失，后者沿用既有四场景syncack实测范围。

## 共享连接取消分类修复

CI41039024257的Ubuntu启动IPC取消用例实际失败：caller已经取消后关闭的发送流返回EOF。共享connection发送层增加caller context检查并重建取消错误，主动关闭且context有效仍返回EOF。确定性unary/server-stream × cancellation/deadline × Send/SendHeaders/CloseSend共12分支修前失败、修后通过，另2个正常关闭对照保持EOF。完整connection race2.482秒，原启动IPC专项race1.532秒，vet通过。

后续完整双主机轮次改用`v0.0.1-alpha.2-local.multihost.3`整套两平台产品（含全部host和匹配插件manifest），冻结源为e50d03d加最终connection修复，commit元数据unknown；二进制和完整源码manifest另存hash。helper v4双平台构建及整包race1.680秒通过；四目标Homebrew公开包、已运行c4默认12h与本候选严格分源。

## 验收工具的发送终态

独立review发现helper在条件写header/chunk发送EOF时直接返回会遮蔽远端Aborted/AlreadyExists。v5复用产品既有FinalSendError，仅EOF有界读取一次最终响应（1秒），caller取消/硬错误不额外读取，不重放消息；读取到空终态或成功也不能把失败发送升级为成功。四个原行为定向反证全部失败，最终整包race2.775秒及两平台构建通过，产物与完整冻结源码另存hash。该修正仅诊断工具，不改变产品授权、持久状态或限额。恢复屏障新增只读状态/日志判断，当时完整矩阵未通过，未新增勾选；第12轮及OS清理完成后才关闭三个原条目。

## 人工慢链路碰撞预算

v6仅创建独立collision前端时显式传入90秒预算，ResolveConflict更早的helper90秒parent仍保持；其他前端、真实CLI/TUI及产品默认5秒不改。碰撞数据1MiB，两类SIGKILL8MiB保持；等待完整本地副本、核远端目标不存在、注入同名sentinel后解除限速，继续要求Aborted/AlreadyExists及原本地、原远端、sentinel三份内容保留。这是显式诊断预算下的条件碰撞验证，不能证明默认CLI在人工慢链路5秒内完成。真实UDS/H2行为测试覆盖独立预算与更早parent保留，完整helper race2.778秒、两平台构建通过。

## 第12轮独立核验

737条事件，157.309938秒，10个phase全部完成，result与两helper/controller-cleanup均passed；events SHA-256 `bc0f02950ba1979e1be4b1390c5448b703b06bde46455549fed8b23c687ba5f3`，controller `ca0396932dcddbaae0a48dc6b269a35785524f6da45916f68bd50682d1ea77a4`。两平台部署各8个产品/manifest/helper文件与冻结hash逐一相同；241个生产Go/模块/构建输入与当前源码逐字一致。

普通上传和冲突解决的8,388,608字节源分别在远端仅有1,310,720和524,288字节时强杀；公开旧目标与旧基线保持，完整stage/source hash匹配，两次均prepared/incomplete→新进程后台committed，旧agent及两个worker消失、旧listener拒连、最终revision/hash对应新baseline且operation为空，B终端/TCP及原pane保持。三组条件写各一成功一拒绝，胜者未被覆盖；碰撞sentinel与原本地/远端版本保持。

A映射先按13在有效凭据下暂停，再撤Token；旧A终端/TCP关闭，ListServers到auth_required/CREDENTIAL_REJECTED、remote_protocol为空且path=unknown后，新GetProject精确Unauthenticated。B同instance的终端、TCP和baseline.txt同步继续；不声称A同步由撤权终止。所有mapping显式归档、session/project删除、Token撤销、helper退出均通过；这一结果仍与OS账户/目录/SSH回收分别核验。原第1–11轮失败与第10/11轮controller归档失败均保留。

## OS资源回收与当前结论

最终清理先核inode/UID、全部Docker挂载及进程引用，然后删除本轮31个精确本地对象（runtime、v1–v6 helper、冻结构建、传输归档与生成缓存），VPS专用UID995账户/同名组、根目录和已无server/session的私有tmux目录；不停止共享tmux server、不触碰其他容器。SSH专用转发已取消，最后确认本轮无命令master没有其他复用client或listener，再通过准确control socket退出并删除空目录。原VPS SSH/Web/RustDesk监听PID保持；7897代理实际TCP可达；8个受保护进程PID/启动时刻/命令不变，c4 Linux容器仍运行，expiry非秘密输入hash和预约runs=0保持，未读取auth.key。

清理回执SHA-256 `d60cc2425fd4951467a1fac21ab60afdb6e64841a917e9aabcc20ce85ab22597`。完整业务结果、部署hash、OS回收及独立审查一致；08/15/16关闭上述三个原项，计36/42。公开alpha.2不含本批产品修复；本批没有新tag/Release，正在运行的c4长测也不覆盖后续terminal/connection修复。CI858df71/92a35f1/423485a/aa394b4分别成功，原473710f取消竞态失败仍保留。


## 历史临时材料最终清理

2026-10-30在23已有111项、16项VM缓存目录及仓库bin/dist专项之外，再逐项核对源文件hash、模块/build metadata、明确文档或已确权脚本引用，清除811项历史临时对象：第一组409项，第二组402项。每组执行前核验inode/UID/mtime、全部9个容器挂载、宿主进程及打开文件；第一组13项目录仅有经识别Apple Virtualization的47个只读缓存句柄，无容器或真实宿主使用，VM保持运行且未重配；第二组402项均无引用。未按名称前缀直接删除，未执行全局容器/缓存/tmux清理。

第一组先删除9项，随后遇到UID501所有的0555冻结源码目录而PermissionError；原失败保留。确认同inode/UID及本次部分删除后更新该项元数据，逐目录验证设备/所有权后仅补owner权限，剩余400项全部删除。第二组402项零错误。三份清理回执SHA-256依次为`e064f2e5818461b8ab9f76a1f87c78632463db357e12accb891f862333be23d5`、`740e85c4d42b58c5d7e47ab5b402003cc04d3c9bb876461f91234a9a0214bca0`、`1f42cdee44e2307a39dfc213b8678d0f432f463cdf14c7e7ce1fb850db9af381`。这些是历史清理数量，不覆盖前文双主机31对象/VPS账户/SSH专项范围。

最终20:12:33（北京时间）删除`/tmp/shiki-closeout-20261030`与`/tmp/shiki-historical-ownership-20261030`两个自有审计目录，并删除一次性清理脚本；原始日志、失败轮次events、归属报告和清理回执均未复制进docs。删除前后8个保护进程PID/启动时刻/命令相同，c4 Linux容器仍运行，expiry非秘密输入hash保持、预约未执行（runs=0）、auth.key仅验证存在未读取、7897代理实际TCP可达。最终清理JSON摘要SHA-256为`074a7ea4a841e7eee157180beeb9690d818448e9d8ce1d8e354c67beda7932b0`。VPS原服务已在关闭专用SSH master前核验，之后没有远端操作，未虚称再次连接复查。

保留边界：在系统与用户临时目录的盘点中，124项历史对象来源不足，另`/tmp/shiki-homebrew-linux-acceptance.py`缺少归属证据且仍有1个VM缓存引用，均不删；未确认用途的real-signing目录及正式签名私有备份保留。当前双平台c4默认12h及未使用key到期任务、冻结输入/来源记录/容器/预约继续隔离保留；不能为宣称环境全部清空而中断测试。两平台完整尾部最早10月31日约05:35，到期任务15:56执行，实际结果和最终资源回收仍待验。

最终源码提交cc0bb7e的CI41051011635十任务全成功；本次收尾仅修正文档历史状态和记录清理，没有产品行为/版本/能力变更，也没有新tag或Release。36/42及原42条文字顺序保持；历史失败仍保留为失败，临时证据删除不产生新增验收通过。
