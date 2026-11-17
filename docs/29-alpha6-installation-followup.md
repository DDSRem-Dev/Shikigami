# alpha.6 安装问题与实机修复记录

本记录整理用户从 alpha.4 至 alpha.5 的真实安装反馈，以及已随 alpha.6 发布的修复。源码测试、公开发行、两端升级和公网稳定性分别记录；当前尚未完成的验证不记通过。

## 问题清单

| 用户遇到的问题 | 定位与修复 | 验证边界 |
|---|---|---|
| 管道安装输入 y 后似乎无响应 | alpha.5 已修复控制终端读取，确认/秘密输入不读取脚本管道；保留 PTY 回归 | 新发行复验 |
| APT、Chrome、系统服务执行慢，误以为卡死 | alpha.5 已增加阶段、耗时及10秒心跳；等待用户输入时停止进度刷新 | 不伪造下载百分比 |
| Keyring password 看不到字符 | 明示其为浏览器专用密钥环口令、隐藏输入及二次确认；已有登记复用 | 不重置密钥环 |
| 密钥环服务203/EXEC Permission denied | alpha.5 已修复严格 umask 下公共程序路径权限 | 私密凭据权限保持 |
| Mac .zshrc permission denied | 分别处理安全启动文件，显示准确受保护路径，继续引导 | 不接管root所有或符号链接文件 |
| home0775 导致本地 worker不可用 | 快速安装批准计划后，只移除所选账户真实home的组/其他用户写权限 | 不递归chmod、不更改所有者 |
| WORKER_NOT_CONFIGURED | 修复可复现的home祖先信任失败；插件提示区分本机与VPS安装 | 不把未知worker缺失一律归因home |
| 服务器名填IP、地址填9527 | 标签/地址提示分开，拒绝纯端口，输入错误原地纠正；IP标签可作地址默认值 | 不绕过身份pin |
| 已保存错误地址难以纠正 | 显示保存地址，setup支持显式地址修正；保留凭据和daemon身份 | revision检查保持 |
| Token输入Enter/Ctrl+C似无响应 | 明示隐藏输入及提交/取消；提交后显示连接验证阶段；PTY验证终端恢复 | 不记录Token |
| yes/y行为不一致 | 引导统一接受y/yes/n/no及大小写；无效输入重新提示 | 完整端口配置替换仍默认拒绝 |
| region PLUGIN_START_FAILED | 显式region支持安装受管daemon drop-in允许既有六动作sudo授权；必要时仅重启daemon | core仍NoNewPrivileges=true，不增sudo动作 |
| 端口配置后REMOTE_RPC_FAILED | 已接受修改后只重试状态读取，沿用原revision和截止 | 不重放修改 |
| 控制面抖动被误认为要重新授权 | 临时离线继续检查已保存连接，不自动发起节点登录；节点授权与应用Token拒绝分开 | 信任失败仍停止 |
| shiki仅报service unavailable | 智能启动指出本地映射、远端项目或会话读取阶段；恢复提示按节点/凭据/网络区分 | 保留原错误码与结构化详情 |
| 浏览器掉线、鼠标不能操作 | 缓解短探测失败取消共享流；重叠调用仅共享同一成功身份探测；查看器明确Controller后可输入 | 业务RPC持续鉴权，无输入重放 |
| 画面冻结但Connected，不能重连 | 显式Reconnect screen可重新建立只读attachment；等待旧连接关闭；旧代HTTP响应不得改变新代控制状态 | 不自动接管、不自动重放 |
| 查看器启动受慢status阻塞 | 状态检查与画面建立并行；状态失败不伪装Chrome已停 | 外部网络仍单列 |
| 本地socket发布偶发失败 | 避免Mac大小写不敏感路径上的临时名别名提前公开 | 确定性回归及整包race通过 |
| 一次性auth key登录竞态 | 在tsnet启动前传入内存key，避免空key初始注册误拒绝 | 真实控制面夹具、秘密不持久化及缓存旧身份回归通过 |
| 开发hotfix、调试bin残留 | alpha.6签名发布并两端升级后，清理确认为本任务开发产物的路径 | 配置、凭据、项目、profile、agent-remote保留 |

## 网络事实与未解决边界

2026-11-16实测：VPS daemon持续active、NRestarts=0；短vmstat及I/O压力采样未见拥堵。SSH空操作两次超过8秒，所测连接累计TCP重传34次、rto约15秒。Mac到VPS的10个ICMP请求仅1个返回；服务器到1.1.1.1的5个请求全部返回、约0.68ms，网卡计数未见errors/dropped。ICMP可能受限速，不能把该样本当业务TCP丢包率，也不能确定具体运营商/路由段。

这些证据证明非Shikigami路径也有延迟，不能通过重装、换Token、禁用鉴权或放宽超时宣称彻底解决。新版本应消除已确认的软件放大因素；公网浏览器持续画面、控制输入与真实CLI响应须在部署后实测，并诚实保留失败。

## 清理范围

用户明确要求保留现有agent-remote。清理仅涉及Shikigami临时hotfix/诊断目录、无运行引用的本地构建bin、测试临时文件和旧下载脚本。签名正式发行目录及正常安装备份不作为dev删除；~/.shiki、/var/lib/shiki、密钥环、Tailscale身份、浏览器profile和用户项目保留。源码仓库中的生产源文件、测试、版本文档保留。

当前状态：源码修复、公开CI、alpha.6签名发行与两端升级完成；确认的旧开发产物已清理，短时CLI采样及浏览器交互验证状态见下。

## 发布前CI跟进

首轮CI的WSL2产品、端到端及终端测试通过，systemd进程枚举检查失败。已补修检查器的退出竞态和stat字节解析，权限错误/损坏worker仍失败；112项工具测试及30项平台工具测试通过。原失败及取消/跳过项保留于08/15/16，新固定源377551a的CI43906750506已57/57成功；WSL2的13项systemd操作及清理通过。macOS容量取消和Intel CLI一次超时在同源补跑后通过，未更改断言/超时。alpha.6发行44017118492已成功（60项成功、1项条件跳过），23公开资产及四目标独立验签、payload/source/平台/Formula一致性核对通过。两端已用公开签名包升级，精确发行证据见[15](15-implementation-status.md#evidence-public-alpha6-release-20261117)。

2026-11-17升级前基线：Mac节点online但已保存服务器为RECONNECT_EXHAUSTED，导致查询立即失败；使用保存连接显式connect恢复ready，未更换凭据。之后三次默认5秒预算的项目查询全部成功（0.534/0.424/0.476秒）。这是升级前的恢复证据，不计alpha.6部署后稳定性通过。


## alpha.6 实机升级与清理（2026-11-17）

VPS Debian12/amd64与Mac arm64均运行正式alpha.6/377551a，安装文件全部匹配已签名manifest；VPS region helper/browser runtime和Mac LaunchAgent也已切换正式产物。两节点保持原tailnet地址及daemon身份，Mac配置与凭据、VPS配置、设备Token、加密凭据和keyring逐字节保持；插件配置bucket与升级备份一致。profile列表和保存的地区设置保持。local sync/port/browser与remote sync/port/region/browser全部ready。没有重新登录节点、换Token或修改路由/DNS。

安装器激活后的首次探测为node_uninitialized，之后复查两节点online，Mac local_application_ready；VPS仍为application_pending/CLAUDE_LOGIN_REQUIRED，未执行账号登录。Chrome在支持程序更新前显式停止；首次恢复请求返回service unavailable，查询确认仍stopped且无systemd启动任务后再次启动成功。运行profile为work，Chrome157.0.8103.78，region revision保持；这些过程中的失败不从记录中抹除。

升级后默认5秒预算的项目查询进行12次间隔采样：第1次失败（2.540s，只记录command_failed，未保留原始错误正文），随后11次连续成功（0.293–2.252s）。该约3分钟窗口证明现有连接可继续工作，不能证明公网长期稳定或消除底层网络故障。浏览器界面验证遇到原ego-browser工作区消失，工具列表为空；已请求用户允许新建工作区继续，不把尚未进行的鼠标/重连复验记通过。

清理完成的旧开发产物：Mac的38个确认路径，包括临时hotfix、开发bin/.tools、诊断截图、测试目录和Python缓存，逻辑文件总大小5,273,780,157字节（不是实测磁盘释放量）。删除前核对UID/dev/inode、正式服务接管和运行引用；系统虚拟化文件共享的残留句柄仅在Docker所有容器均无对应挂载后排除，未停止或删除其他容器。VPS删除哈希匹配的旧alpha.4下载脚本，以及验证完成后的本批私有升级staging。原90-region-helper临时策略在新50受管策略确认生效后精确移除。agent-remote、正式发行及安装备份、配置、身份、项目、profile和密钥环完整保留；本机另外26个已完成的alpha.6测试/CI helper与日志已清理；本批本机验签/观察临时目录也已在证据入文档后删除；临时SSH维护连接已关闭。


交付文档提交后的CI44026669305又暴露资源测试夹具的Darwin进程观测错误，52成功/1失败/2跳过。已增加独立消失确认与确定性回归，未修改生产二进制或发行资产；原始错误仍不能证明该次确实退出，修复及后续验证详见08/15/16。受影响的hostrotation、rotation、syncack、port和architecture整包race全部通过；后续be7fada固定源CI的原macOS平台与WSL夹具检查通过；该轮结果为56成功、1个独立浏览器测试失败，详见下文。


## 发布后追加的重连修补（尚未进入 alpha.6 安装包）

be7fada的CI44031253619中，WSL2唯一产品失败落在显式WebSocket重连的新Dial；systemd维护13操作仍通过。进一步构造真实loopback回归，确认关闭ACK早于本地SDK attachment清理完成，旧context取消判断可能拒绝正常重连或提前开启第二条stream。源码已改为本地SDK Close返回、socket清理及双向pump退出后的完成信号（不代表远端释放确认），最多一个有界等待者，等待和READY共享原预算，不取消现有连接或自动接管；旧代码确定性失败，修后专项/整包race和前端10/10通过。固定源6850e77的[CI44037921171](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44037921171)最终57/57成功；它尚不含下述身份复核预算修补。

已发布alpha.6标签和签名资产保持不变，因此该次记录时两端的alpha.6不含此追加修补。现已通过alpha.7交付，发行说明见[alpha.7](releases/v0.0.1-alpha.7.md)；用户已授权alpha.7发布、两端升级和新建浏览器工作区；发行及部署状态见08/15/16，不能由源码验证宣称所有重连问题已在实机解决。


## 追加的调用预算修补（尚未进入 alpha.6 安装包）

新一轮约10分钟的默认5秒项目查询11次中8次成功、3次失败；另次查询捕获2.517s的`service unavailable`。后续doctor和12次结构化查询成功，但诊断有交叠，不能推断输出格式或共享探测就是差异原因，也不能覆盖之前失败。

独立源码审查确认已有连接的身份复核会将剩余预算额外折半，而短caller超时直接返回、未使用预留给替换连接的后半时间。它与现场耗时吻合，尚非现场根因的直接证据。修补仅使这次复核在原调用剩余预算内最多等待5秒；不延长CLI总超时、不改首次多路线分摊、不跳过鉴权或重放操作。旧代码提前失败、修后同预算完成认证和业务的回归已通过，agent/connection/architecture整包race通过。精确预算区间、回归及CI见08/15/16；这项修补现已随alpha.7交付并升级两端，结果见末节。


## alpha.7升级前追加基线（2026-11-17）

用户授权新建工作区后，正式alpha.6两端完成一次真实work浏览器基线：显式接管后鼠标创建测试标签、按钮计数0→1、键盘输入alpha6-test、滚动至END；显式重连后恢复只读，再次接管后关闭测试标签，原about:blank保留。关闭本地viewer未停止远程Chrome。状态查询仍多次出现unavailable并恢复，因此这次可操作结果不等于网络稳定。观察脚本一次误等Read-only session，而实际重连状态是Read-only；该脚本超时不算产品失败。

14:25:39–14:27:39 UTC的12次默认5秒预算项目查询全部成功（0.196–3.141秒），采样与上面的查看器测试分开进行，不覆盖历史失败或替代alpha.7部署后验证。最初两次误用不存在的单数project命令已排除，不属于网络样本。


## alpha.7公开发行与两端升级（2026-11-17）

alpha.7固定源cbf3cdf，分支CI57/57；发行44091646249 attempt2为60成功、1条件跳过，三项失联、作业超时及容量异常在原条件补跑后通过。23资产、四目标签名/成员/哈希/大小/来源和Homebrew Formula独立核对完成，精确证据见[15](15-implementation-status.md#evidence-public-alpha7-release-20261117)。

两端正式升级已验证：Debian12/amd64的18个payload、Mac arm64的14个payload及VPS两项root支持程序均匹配独立已验签bundle；release.json逐字节、SHA256SUMS签名pin、receipt active/current一致且无pending事务，原Plan保持。daemon与LaunchAgent实际可执行路径指向正式alpha.7；daemon active/running、NRestarts=0、显式region的有效NoNewPrivileges=no，keyring/region guard服务active。两节点原地址及daemon身份保持；Mac配置/plist/凭据、VPS配置/Token/加密凭据/keyring/region策略文件逐字节保留，插件持久bucket及profile定义的canonical hash一致。7个local/remote插件均alpha.7 ready。安装器切换后的首次探测返回node_uninitialized/exit3；稍后Mac为local_application_ready、两节点online，VPS仍application_pending/CLAUDE_LOGIN_REQUIRED，未重登或换Token。支持升级按批准计划执行APT与官方Chrome检查并复用密钥环；Chrome显式停止且无pending jobs后更新runtime，随后work一次启动成功，Chrome157.0.8103.78、原region revision保持；浏览器交互结果另记。

alpha.7实机交互已完成：15:42:14–15:50:21 UTC约487秒观察窗口，真实work屏幕中鼠标计数0→1、键盘输入alpha7-test、滚轮至END；显式Reconnect screen约1806ms恢复Connected/read-only，原计数及文本保留，没有自动接管。再次明确Take control后计数1→2、追加-reconnected成功。该窗口未观察到断屏或状态unavailable；只证明本次短时操作，不外推公网长期稳定。仅本轮测试标签被关闭，原about:blank保留；本地viewer关闭后原Chrome实例仍running且attachments=0。测试工作区已finish关闭。早先工作区在长时间CI等待后消失、列表为空，按用户已授权新建工作区继续，未将工具工作区故障记作产品故障。

随后独立CLI采样（查看器已关闭，15:50:59–15:52:58 UTC）：12次默认5秒预算projects list，11次成功（0.175–1.319秒），第8次在5.020秒返回deadline_exceeded/operation deadline exceeded，之后4次恢复成功。保留原超时且未重放修改，不能据此宣称链路完全稳定；该错误本身不能区分底层网络或服务响应的原因。与升级前12/12成功的短窗口并列，不能据样本推断版本导致网络变差或已消除历史故障。交互结束后再次核对两端签名payload、receipt和全部基线保留项通过。

本轮清理已完成：核对准确任务目录、所有者、无符号链接及运行引用后，删除VPS升级staging的32个普通文件（逻辑大小427,395,945字节）和Mac私有验签/升级/观察目录的82个普通文件（727,517,002字节）；此前10张alpha.6基线截图已删除。逻辑大小不是实测磁盘释放量。测试标签、viewer和工具工作区已关闭，临时SSH维护连接已关闭；正式Chrome继续运行。清理后VPS仍运行正式alpha.7，daemon active/running、NRestarts=0。agent-remote、正式release/backups、配置、身份、凭据、项目、profile和密钥环保留。
