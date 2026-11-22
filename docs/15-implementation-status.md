# 实施与验收记录


<a id="evidence-viewer-transport-diagnostics-20261123"></a>
## 查看器传输诊断（2026-11-23，未发布）

已在 browser viewer 增加本地传输观测：屏幕数据 KiB/s、二进制消息/s、最近采样最大到达间隔、最近数据年龄和 WebSocket 发送缓冲区。显示文案说明这些值只描述本机 WebSocket 到达与发送状态，不冒充远端 RTT、RFB 帧率、解码时间或实际屏幕呈现帧率。实现没有丢弃、重排或合并 RFB 字节，也没有调整画质、分辨率、控制权或重连语义。

- [x] Node viewer UI 测试：24 项通过，0 失败。
- [x] `node --check plugins/browser/client/web/viewer.js` 通过。
- [x] `go test -race ./plugins/browser/client ./plugins/browser/server ./plugins/browser/runtime` 通过。
- [x] 变更范围复核：仅 `plugins/browser/client` 静态资源、测试和对应文档；无 host 业务依赖、RPC/权限/持久状态/进程策略变更。
- [ ] 真实公网查看器采样、滚动低帧率、断线恢复和长期体验仍待验；alpha.10 仍为草案，未发布或部署。

现网基线（alpha.9，2026-11-23）：正式 `work` viewer 走 `tailscale-relay`，约2.5秒观察后保持 Connected/Read-only；旧版 viewer 显示本地接收约12.2 KiB/s、发送缓冲0 KiB。该样本没有控制、账号或页面写入，也没有改变远端 Chrome；viewer关闭后短期 URL/JSON 文件已删除。因为查看器尚未部署本批诊断代码，这只能作为旧版链路基线，不能解释真实帧率或宣称卡顿修复。

本批没有新建持久测试项目、远端文件、测试服务或开发二进制。短时真实查看器采样若执行，必须在关闭查看器后复核临时资源并删除；正式 work profile、登录页、密钥环、agent-remote 与历史发行备份保持不变。


<a id="evidence-tigervnc-cleanup-20261123"></a>
## TigerVNC 对照测试环境清理（2026-11-23）

- [x] 删除本机退出的 `shiki-rfb-framerate-20261122` 临时 Debian 12 容器及其未被其他容器使用的 `debian:12-slim` 镜像；本机无 `shiki-tigervnc`/`shiki-rfb` 容器、镜像、测试脚本、测试进程或临时目录残留。
- [x] 只读扫描 VPS 的 `/tmp`、`/var/tmp`、`/root`、`/home/shiki`、Shikigami 状态目录和 Docker 资源；没有发现命名测试文件或 Shikigami 开发包。清理了 5 组已无进程引用的旧浏览器运行时 JSON 及对应的 `/run/user/1000` 临时目录。
- [x] 保留当前运行的 `work` 浏览器实例、正式 daemon/keyring/region 服务、正式配置/凭据/项目，以及 `agent-remote` 和其他用户 Docker 服务；未执行全局 Docker prune，未删除用户缓存或账号数据。
- [x] 清理后复核：VPS 三项 Shikigami 服务为 `active`，仅当前浏览器运行时文件保留；VPS Docker 仅保留原有 `agent-remote`/RustDesk 服务；本机测试资源扫描为空。




<a id="evidence-tigervnc-congestion-20261122"></a>
## TigerVNC 拥塞窗口核查（2026-11-22）

- [x] 核对 Debian 12 bookworm 的`tigervnc-standalone-server_1.12.0+dfsg-8`源码与当前runtime调用的`/usr/bin/Xtigervnc`；Debian源文件仍保留延迟尖峰后独立拥塞分支。
- [x] 按上游`895a86cff25b3d410af043098740d425cdad2bdc`的精确逻辑复现无符号窗口绕回：`baseRTT=100ms`、`minRTT=201ms`、初始4KiB窗口时，旧逻辑比例缩窗后再减4KiB并绕回，最终被4MiB上限钳制；补丁逻辑保留4KiB下限。原始补丁、脚本、结果和SHA见[隔离证据](evidence/tigervnc-congestion-20261122/README.md)。
- [x] 核对该补丁尚未进入上游`v1.16.2`稳定标签，当前上游`master`已包含；“直接升级最新稳定版”不能作为现成修复。
- [x] 在一次性Debian 12容器中应用原始上游补丁并成功编译`rfb`公共库目标；这只证明补丁可编译，不是完整`Xtigervnc`运行时或帧率验收，结果见隔离证据。
- [x] 在一次性Debian 12容器中完成stock与patched `Xtigervnc`对照：640x360/24-bit、33ms全屏动画、625,000 B/s与双向55ms代理、raw RFB客户端；两组均4次更新，约0.419/0.418 FPS，均值间隔2.387/2.393s，原始字节与错误数一致。完整结果见[隔离证据](evidence/tigervnc-congestion-20261122/README.md#isolated-runtime-ab-comparison-2026-11-22)。这是合成raw传输样本，不代表Tight/JPEG、noVNC、Tailscale或真实滚动体验。

该发现与低帧率/追赶症状有关，但不能单独解释触控板步进、网页滚动条略好或所有冻结。要纳入产品必须有可审计签名runtime artifact或发行版含补丁包，并完成升级/回滚、进程归属、依赖、实际滚动及长期稳定验收。

<a id="evidence-scroll-followup-20261122"></a>
## 滚动体验跟进与已完成 CI（2026-11-22）

- [ ] 正式alpha.9用户补充：使用触控板，按指导resize缩小后没有明显变化，拖动远端网页滚动条稍好；Fn+↓在该机器触发音量，不能计为Page Down测试。尚未独立记录这次实际framebuffer尺寸，不把主观对照扩大为排除带宽或已定位根因。整体操作可用的反馈保留，滚动流畅度、长期冻结与beta体验门槛仍开放；无需用户反复重做相同resize。
- [x] noVNC1.7/Smooth固定源ce85e60的[CI44949686567](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44949686567)于2026-11-21T21:54:33Z完成，attempt1，57/57成功，无失败、取消或跳过。暂缓采用的网络库候选eb4478c的[CI44951709516](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44951709516)于22:18:38Z同样57/57成功；它证明该候选检查通过，不是性能收益或当前恢复代码的完整CI。两轮均等待原运行结束，没有为新修改取消检查。
- [x] 本地提交0bdb51f恢复后的全部非docs文件与ce85e60逐文件相同，工作树当时干净，实验文本哈希全部通过；后续提交若只补文档不能冒称其独立完整CI已通过。正式环境保持alpha.9，未发布或部署alpha.10。
- [x] 一次性本地容器TigerVNC更新上限对照已完成，结果与限制已记录；未降低正式帧率、未改vendor滚轮语义。现有noVNC公开API没有sendWheel或滚轮灵敏度配置；增加合成事件可能提高位移准确性，也可能加重画面负荷。未来输入候选必须独立验证撤权/重连/失焦、方向与余量、突发及修饰键，不伪装成官方API或已提高实际FPS。

本批先同步实际用户反馈与固定源CI证据；目录/依赖边界、RPC/能力/权限、持久化/revision/去重、进程归属/取消恢复、RFB字节顺序及文件网络边界、CLI/安装布局均未改变。验证不跨越源码、平台与实机效果边界，08/15/16保持一致。




**TigerVNC上游核查（2026-11-22）：** 当前Linux runtime调用系统`Xtigervnc`，现场记录为1.12.0。上游提交[`895a86cff25b3d410af043098740d425cdad2bdc`](https://github.com/TigerVNC/tigervnc/commit/895a86cff25b3d410af043098740d425cdad2bdc)（2026-10-22）修复延迟尖峰后无符号拥塞窗口下溢：旧逻辑在按比例缩窗后仍继续减4KiB，极小窗口可能绕回最大值；补丁把第二个分支改为`else if`。GitHub比较确认该提交不在v1.12.0中，但它位于远离v1.12.0的后续开发历史，不能把整条TigerVNC主线或未构建的系统包当作可直接升级方案。该修复与用户的低帧率症状相关，尚未证明现场曾触发，也没有修改用户VPS系统包；需要在受控环境确认发行版可用的包含补丁版本、升级/回滚及实际滚动效果后才考虑纳入安装流程。

<a id="evidence-tsnet104-deferred-20261122"></a>
## 丢包模拟与候选暂缓（2026-11-22）

当前产品源码已恢复Tailscale1.98.10、原gVisor及Go1.26.11，保留ce85e60的noVNC1.7、默认Smooth与Fence测试。此前1.104.2/Go1.27.4在隔离环境通过兼容测试，但没有现场性能收益证据；受控模拟反而出现低带宽丢包下吞吐下降，因此不作为本次卡顿修复发布。研究分支`fix/browser-latency-tsnet104`的固定eb4478c CI继续运行，不取消/改写原检查；它不代表当前恢复后的发布候选，也不表示已部署。正式两端一直是alpha.9。

- [x] 完成11个内存双gVisor实验，均完整传输2MiB、SHA256匹配、exit0；固定Go1.27.4、MTU1280、双向各55ms传播/5Mbps串行链路，单向仅数据包0或1%独立丢弃，seed901/902，ACK不丢。无丢包两版约4.48Mbps；1%旧Reno/RACKoff为3.174/2.614Mbps，新CUBIC/default为0.967/0.681Mbps。同新版seed901控制：Reno/off2.657、CUBIC/off0.941、Reno/default3.005Mbps，不支持单独归因RACK。一个控制样本投递调度迟到20.480ms，其余约2–4ms；恢复到达追踪有未匹配重分段/冗余包，不能当完整TCP恢复状态。
- [x] 原始输入、依赖锁定、源码、11份结果及校验清单保存为只读用途文本[实验材料](evidence/netstack-simulation-20261122/README.md)；无测试二进制进入仓库，原临时目录已清理。模型不包含真实WireGuard/DERP、HTTP/2、浏览器或Tailscale自定义缓冲配置，不能用1%模拟反推原网络丢包率，也不能宣称新SDK现场必定退化。
- [ ] 保留用户最新反馈：正式alpha.9在Smooth并暂停背景视频后整体操作可用，滚动仍低帧率；调整实际远端分辨率的对照尚未回报，长期冻结/断线恢复和beta体验门槛仍开放。默认Smooth及图像资源释放的源码改进尚未部署。

本批只撤回本任务未发布的候选改动，并补充实验记录；原tailnet-only拨号/peer admission、代理边界、RPC权限、持久状态/去重、进程归属、CLI/安装及工具链恢复到已验证基线。生产差异逐文件对照ce85e60，不把未采用SDK的217项测试或研究CI算作当前发布性能验收；08/15/16保持同一结论。


**后续滚动审计（静态/隔离，不是实机结论）：** noVNC1.7 `_handleWheel` 使用50px累积门槛、每事件每轴最多一步且到阈值清零；执行原函数的隔离输入中，`[150]`产生1步、`[50,50,50]`产生3步、`[30,30,30,30,30]`产生2步余30、`[49,-50]`没有步进。这证明事件分批/残量会影响滚动步进，不证明用户卡顿只有这一原因。FBU可在同一JS任务连续flip，不能把flip计数宣传为屏幕实际FPS。默认远端1440×900，CSS缩放不减少远端像素；需要控制者显式勾选resize才请求远端尺寸变化。数据队列16×64KiB是容量上限，不能当作实测积压。下一步保持Smooth和同一静态长页，对照显式resize前后及滚轮/Page Down；尚未收到结果，不改vendor输入、不自动重启或调整用户分辨率。

<a id="evidence-tsnet104-candidate-20261122"></a>
## 网络栈隔离迁移实验（2026-11-22，已暂缓采用）

- [ ] 原网络严重卡顿/冻结与真实公网吞吐门槛继续开放。此处记录未采用候选的历史测试范围；实验从ce85e60创建隔离worktree，不调用用户已安装Shiki、不连接外部Tailscale账号，不把本地回归当作实机改善。正式两端及冻结alpha.9资产保持原版本。
- [x] 官方Tailscale1.104.2与gVisor `v0.0.0-20260915211658-a6f909f08a72`候选接入，最低Go1.27.3、工具链固定官方稳定1.27.4。上游启用CUBIC及新版默认SACK/RACK，注入路径改为三个有界队列；不是一行参数回补。依赖134→145项、62版本变化/13新增/2移除，必须覆盖文件监视、WebSocket、CLI/TUI及跨平台，不能继承旧SDK验收。来源：[发行](https://github.com/tailscale/tailscale/releases/tag/v1.104.2)、[netstack](https://github.com/tailscale/tailscale/blob/v1.104.2/wgengine/netstack/netstack.go)。
- [x] 初轮真实编译发现Engine.PeerForIP删除及cellbuf/ansi不兼容，完整失败保留为迁移原因。SDK适配改用裸IP WhoIs(port0)，只认live节点精确/32或/128地址、预期公钥、非自身、未Expired；握手前后分别查询、≤5秒且更早context优先，失败关闭已握手连接，继续走userspace netstack并绑定原generation/authorization/peer撤销机制。没有Server.Dial/SystemDial回退。旧新检查均是live peer-map观察，不冒充原子WireGuard路由租约；真实移除测试只替换已删除API的观测点为ErrPeerNotFound，不把任意LocalAPI错误当删除成功。
- [x] 修补新版useproxy feature可由环境禁用导致hook缺失的兼容风险：五个实际proxy hooks缺失时节点初始化失败，不能SetProxyFunc表面成功却绕过系统代理。子进程在真实init阶段测试禁用环境；不修改用户环境或打印原环境内容。TUI使用官方cellbuf0.0.15兼容MVS选择的ansi0.11.8，不降低SDK依赖或删除功能。
- [x] Go1.27.4下shiki/agent/daemon构建20.172秒成功；首组安全race26顶层/105含子例、0skip/0fail，包18.600秒（含编译wall78.245秒）。仅非deadline取消测试由30ms父截止改为5秒，真正deadline子例保持30ms，随后专项race重复10次通过1.858秒；没有改已在执行检查的冻结源或放宽结果断言。
- [x] 完整node race63顶层/217含子例通过、0skip/0fail（76.491秒）；Go1.27.4下全部cmd交叉构建CGO0通过：darwin/arm64 7.976秒、darwin/amd64 22.370秒、linux/amd64 23.903秒、linux/arm64 25.615秒。交叉构建不算对应平台原生运行。
- [x] 工具脚本112项复验通过（21.011秒）；首轮16项cache测试因固定假Go版本仍为1.26.11而拒绝，改测试输入为1.27.4后通过，没有放松生产平台版本检查。
- [x] Mac arm64主机相关race通过：internal/client20.605秒、internal/agent70.767秒、internal/connection2.732秒、cmd/shiki53.990秒、test/architecture1.450秒；browser/sync/port各可用平台包race均通过，无测试文件的入口不计测试通过，Linux专属行为仍由后续原生CI验证。独立terminal analyzer仅升级Go基线并保留自身依赖版本，`go test -race ./...`成功编译、没有测试文件，不计行为验收。
- [ ] 新源完整CI、公开包升级/回滚、账号状态兼容、自然切网、长稳和公网性能仍未验证。

**工具链来源与检查范围：** CI/release各setup-go固定1.27.4，platform_support严格匹配，WSL固定官方下载`go1.27.4.linux-amd64.tar.gz` SHA256 `7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840`；Linux runner固定官方多架构bookworm索引`sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea`，来自只读官方Go JSON与Docker manifest核对，不以元数据核对冒充完整镜像运行。上述脚本变更不包含在先前105项Go回归范围内，单独验证。历史原工具链与公开alpha.9证据保留。

**本轮用户反馈：** 正式alpha.9在暂停Claude登录背景视频并选Smooth后，用户明确反馈点击/输入等整体操作已经可用，但滚动仍卡、帧率不足。此改善发生在旧正式版本，不是网络库候选部署结果；滚动与冻结长期验收仍开放。先前给用户的open命令漏写`--read-only=false`，造成只读链接，已纠正并由用户继续操作；只读默认是原安全契约，不因此改为默认取得控制。建议的窗口resize对照仍待用户实测，不默改远端分辨率。

**隔离实验收尾：** 候选源码已提交8c4af99并快进主工作树，原实验worktree及日志共1,093文件、10,297,495字节已清理，Git仅余主工作树。首次常规worktree remove因残留文件拒绝；随后只删除固定的本任务临时目录，并经prune dry-run确认仅本任务失效登记后清理登记，没有清理其它工作树。源码与验证汇总已入库，未安装dev二进制；正式服务/配置不变。

**七类架构复核：** ①变更位于node SDK边界、工具链与tests，不把插件业务搬入host；②仅适配现有身份查询，RPC消息/权限/能力/transport选择不扩张，应用协议没有以SDK版本冒充升级；③原状态持久化、revision及request去重保持，未读取用户凭据；④握手前后预算/失败关conn/lease撤销与SDK有界队列关闭须回归；⑤保留tailnet-only、固定peer及代理策略，不通过猜测窗口或公开VNC端口规避问题；⑥新Go基线同步构建、CI、平台检查和安装说明，预构建用户不需自行安装Go；⑦局部安全测试、交叉构建、全平台CI、正式部署性能严格分列。当前仅候选，不宣称卡顿解决。

<a id="evidence-viewer-memory-latency-batch-20261122"></a>
## 后续查看器改进与延迟归因（2026-11-22，源码未发布、严重卡顿未关闭）

- [x] 固定官方noVNC1.7.0源码，commit `63107bd06d9e1f6136ff21aeda8cd62cbf0d433e`、归档SHA256 `06dac239be3973ed79a00e357a68aab996e6ed2fd496ee2cc6e6a34020344ac9`，61项payload清单与完整来源核对。纳入已绘图Image.src释放、JPEG扫描优化及H264能力探测VideoFrame.close；无私改vendor。上游依据：[图像释放](https://github.com/novnc/noVNC/commit/23b7219a5d39577f752be939ee25db2484d465b6)、[JPEG扫描](https://github.com/novnc/noVNC/commit/356eab4f4d259b6db7d1c7b84d43408b2e7acb09)。本机image decode短测0–7ms，因此不能把本次严重卡顿归因于解码或宣称升级已解决。
- [x] 新查看器默认Smooth（quality3/compression6），用户仍可选Balanced/Clear，重连保留本窗口选择；权限、分辨率与控制权不随画质改变。正式alpha.9仍默认Balanced。无并发下载的同一15fps动画交替Balanced/Smooth各12次：Balanced343–1034ms、中位439.5ms；Smooth146–720ms、中位288.5ms。只说明该页面短样本改善，未证明真实网站或长期体验。早先口头约465ms为汇总误差，以这里完整24样本计算为准。
- [x] 真实vendor行为回归覆盖异步解码乱序/flush/释放后可见像素快照、64帧源释放、错误尺寸拒绝及JPEG18个分片点；旧1.6在两项释放测试失败。Image/canvas为double，不等于浏览器GC、真实图片像素或性能验收。补齐51个Fence解析/转发子用例，保留二进制payload/flags、跨DATA分片、只读与revision变化，旧revision按键仍拒绝；未发现Fence生产缺陷。
- [ ] 用户反馈低帧率、输入延迟及冻结/断开恢复仍开放。远端点击至双rAF短测2–27ms、图像解码0–7ms；双rAF不证明实际合成呈现，跨主机时钟未校准，不推断单向网络延迟。前批不同工具轮询/rAF测量不作为严格对照。

**传输证据：** 用户确认套餐5Mbps（理论625kB/s）。自有随机2MiB通过正式API上传3.796秒、首次下载16.129秒；其后下载15.554/9.780秒与动画重叠，仅为竞争负载证据，不算干净吞吐对照；该重叠画面测试出现4546/9155ms及一次10秒未观察到目标变化。独立分段客户端经正式agent IPC、不读取Token：首metadata219ms，全部32个64KiB DATA及终帧/EOF14076ms，本机write+hash5390μs、fsync3ms，完整hash匹配。首轮探针错误使用err==io.EOF而非errors.Is，全部数据20398ms到达后误报EOF，已修正，不能记首轮终态成功。源码审计无每块应用ACK或fsync，HTTP2客户端默认4MiB流窗口，不能归因为64KiB窗口等待。单次SSH/SCP同文件约1秒、显示1.7MB/s且hash一致，小文件可能受突发带宽影响，不能声称5Mbps套餐突破或定位ISP；后续doctor显示tailscale-direct，之前relay观察保留，路线可变，尚未证明丢包/中继/拥塞算法哪个为主因。

**验证和架构：** `make test-viewer`28/28通过；`go test -race -p 2 ./plugins/browser/client ./plugins/browser/server ./test/architecture`全部通过（1.987/1.341/1.383秒），`git diff --check`通过。源码边界仍为插件内嵌静态资源与插件本地测试，无host业务依赖；RPC、能力、身份授权和RFB字节保序不变；没有修改持久数据、revision、请求去重、进程归属及启动恢复；未扩张文件、网络、剪贴板或终端权限，默认画质仅作用于viewer；安装布局和冻结alpha.9资产不变。新增测试验证真实vendor逻辑/生产Fence分支，但浏览器GC、Linux运行时及长期公网门槛独立保留。此前固定8f7481d的CI44945455780为57/57成功，不覆盖本批未提交源。

**清理：** 自有loopback内存HTTP夹具已停止；删除两个自建远端标签，保留原about:blank及Claude登录页，点击网站自身暂停按钮暂停背景视频，无账号操作。本机临时viewer关闭、TaskSpace31完成，临时instrumentation移除。远端自有2MiB上传文件核验固定路径/无symlink/完整SHA后删除；本机19文件13,231,736字节临时目录及1文件1,622字节探针目录已删除；SSH会话关闭。未安装dev二进制，未删除或重置用户Chrome profile/密钥环，agent-remote保留。

**下一归因候选：** 官方Tailscale1.104.2（2026-10-21发布）netstack已从Reno改CUBIC并更新恢复算法，来源注明修复旧CUBIC整型溢出后才启用；该版本要求Go1.27.3。当前仍固定1.98.10及原Go工具链，未盲改协议栈/窗口、未部署候选。需要独立兼容性、安全边界和真实节点验证，不能凭上游注释宣称现场改善。

<a id="evidence-alpha9-latency-followup-20261122"></a>
## alpha.9 后续严重卡顿反馈（2026-11-22，未解决）

用户在本次人工验证进入Claude登录页后明确反馈：Ego测试窗口和自行打开的日常查看器都仍严重卡顿、难以使用。因此前一批短时点击/输入/重连成功仅为功能证据，不代表可用体验已通过；本次进入登录页的事实保持，账号授权仍暂停。正在继续分段测量输入、画面传输与实际绘制；新修正尚未完成，不改写正式alpha.9资产。

已识别测量干扰：Ego页面visible/focused时requestAnimationFrame仍出现约1000/16毫秒交替间隔，因此基于该回调或外层工具轮询的约1秒结果不能当作纯网络延迟。改为在本地查看器临时包装真实noVNC send、message和display.flip，以pointerdown到目标像素实际变化为界：静态页10次128–366毫秒，15fps自有彩色动画下10次316–808毫秒；不外推其他网站/浏览器长期体验。连续更新与Fence实际启用；一次队列为空的快照不排除其他层积压。VPS短时vmstat未见CPU、swap、磁盘等待或steal饱和；公网5次ping109.675–116.126毫秒、无丢包不代表tsnet链路或长期网络。

现场TigerVNC1.12.0+dfsg-8、FrameRate=60；尝试通过vncconfig在线设置15失败，随后读取仍为60，没有实际改变或重启Chrome。后续代码及验证、测试资源清理尚待完成，原网络长期体验门槛保持开放。


<a id="evidence-alpha9-viewer-20261122"></a>
## alpha.9 真实查看器交互复验（2026-11-22，已进入登录页并完成临时收尾）

用户补充授权新建验证工作区后，在Ego TaskSpace30完成正式Mac/VPS alpha.9的真实viewer复验。work仍为原实例`712be7ec4555dc68c4f91a66d5c9dc76`、running/browse，原网络路由为tailscale-relay；测试使用自有data: HTML页面，没有新增远端测试文件或HTTP服务。没有修改生产源码、正式资产、出口、DNS或代理。

Balanced的5次点击至结果区域canvas变化观测为924、978、1116、890、1006毫秒；Smooth为939、999、1115、988、907毫秒。所有值包含Ego鼠标dispatch与canvas像素轮询工具开销，不是纯RTT。alpha.8前次使用不同夹具，不能严格作版本性能对比，也不能据此推导优化因果或全局流畅。真实截图确认Count5和输入alpha9-test回显；滚动后观察到SCROLL_END_OK。

5次显式Reconnect均成功，新连接均回到只读，再由操作者明确Take control。首次测试错误期待重连后自动成为Controller，导致15秒等待失败；随后实际截图为Connected/Read-only，符合设计，记录为验收假设错误而非连接失败。后4次连接观测为1099、1459、1343、1469毫秒，同样含工具/观察开销。最后截图确认reconnect-5真实输入回显；中间各次输入只取得动作receipt，没有逐次最终截图，不扩大为每次输入均有画面确认。

自有页面的只读原生观察为webdriver=false、ja-JP、Asia/Tokyo，未伪造或修改这些属性。导航claude.ai出现Cloudflare检查后交接用户；用户明确反馈已进入Claude登录页。助手重新接管TaskSpace30后，截图实际观察到claude.ai/login的正常Google、Apple和email登录表单，没有输入账号。本次人工验证后进入登录页通过，不证明以后不再出现挑战，也不等于Claude账号授权成功；助手未代做挑战。交接结束后，CLI close-viewer返回state=closed，TaskSpace30以task.finish({keep:[]})成功收尾一次。远端Chrome登录页有意保留供用户后续使用，没有关闭；最终status仍是同实例712be7ec4555dc68c4f91a66d5c9dc76、running/browse，region revision保持原值。本轮先核对精确名单、普通文件和无symlink后删除9个本机临时文件（8张截图及私有viewer回执，共1,142,073逻辑字节）和精确临时目录，最终确认截图另343,393字节随后删除，合计10文件、1,485,466逻辑字节；这不是实测释放磁盘空间。没有新增VPS文件或服务，未执行账号登录。本轮临时查看器/工作区/截图清理完成，远端登录页属于明确保留的用户使用状态。最后doctor为7插件ready、core HEALTHY、CPU15.9%，terminal/sync/forward均0；这是当次采样，不证明长期稳定。

七类复核：仅使用正式两端与既有viewer，host/SDK/plugin归属不变；控制权只由新连接确认和显式Take control获得，未自动提权；同profile/instance和持久凭据保持，未新增远端文件/服务；重连只重建查看连接、不重启Chrome；鼠标、输入、滚动经既有受限通道，网络/文件限额及路由不改；画质使用公开选项，UI状态与CLI健康分别取证；实际截图、动作receipt、带工具开销的计时、错误验收假设、本次人工进入登录页及仍未进行的账号授权分列。不修改既有协议、权限或发布范围，也不把本次短时功能通过升级为beta/长稳通过。

<a id="evidence-alpha9-observer-fixture-20261122"></a>
## 发行后 CI 采样夹具竞态修复（2026-11-22）

文档提交`6106fec8`的[CI 44937051142](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44937051142)最终52项成功、1项失败、2项下游矩阵跳过。唯一失败是Intel Mac的`TestDefaultRotationProcessObserverOwnsAndJoinsSamples`：异步观察器启动40毫秒后测试就结束子进程，首次探测已看到进程不存在，导致LastPresent为空。日志不能区分调度延迟与系统探测耗时；这不是产品超时的证据。

夹具改为等待真实身份探测返回首次alive，再结束其拥有的子进程；等待有截止、探测失败仍失败，原2秒退出join及非空/有序时间戳、正采样间隔断言保持。仅修改测试，不更改生产观察器、产品、协议、安装或已发布alpha.9资产。Mac arm64的两项相关测试以`-race -count=20 -p=1`通过，共40次顶层执行、1.821秒，覆盖自有进程退出与未知错误/取消负例；未生成持久测试bin，自有sleep进程均Wait回收。后续完整CI以本修复提交的GitHub检查结果为准，不将旧失败或下游skip计为通过。新版本短时查看器实测另列；本次Cloudflare人工进入登录页已另有证据，长期体验、自然切网及账号授权等外部门槛保持开放。


<a id="evidence-alpha9-upgrade-20261122"></a>
## alpha.9 两端正式升级与健康复验（2026-11-22，后续交互与本次登录页验证另记）

已将Debian12/amd64 VPS与Mac arm64从公开签名alpha.8升级为alpha.9，固定来源`0b4847d879d10bd2a79b9df348857ab27dafa848`。先经CLI显式停止work，再复用两端完整原receipt Plan执行安装；VPS setup复用原dedicated加密凭据并更新root管理的runtime/helper，没有重新签发或输出设备Token。升级与健康检查已完成；其后浏览器短时功能实测见[交互记录](15-implementation-status.md#evidence-alpha9-viewer-20261122)，本次人工验证进入Claude登录页已通过；严格版本性能A/B、长期稳定及Claude账号授权仍未完成，不能宣称卡顿或挑战循环永久解决。

VPS的SHA256SUMS清单19项、Mac清单15项全部匹配已验签bundle（均包含release.json），VPS两项root支持二进制另外核对一致。两端安装首次均退出3、报告node_uninitialized；后续实际节点online，原daemon_id及100.71.151.36保持。随后正式doctor报告alpha.9、core healthy、7个插件ready，插件配置revision全部保持。首次pending没有被计作ready；安装器本地探测与后续真实远端RPC健康分别记录。

Mac配置、LaunchAgent plist与凭据的3项hash保持；VPS配置、Token、加密credential、keyring policy、加密keyring及auth current/previous/lock共8项hash保持，未在文档输出其内容。profile列表逐结构一致，正式alpha.5–9发行目录及升级backups保留。Chrome仍为158.0.8127.36，新增x11-utils7.7+5；work新实例`712be7ec4555dc68c4f91a66d5c9dc76`实际running、mode=browse，region revision保持原值。普通模式原生diagnose明确返回failed_precondition，属于模式契约的正确拒绝，不冒充诊断完成或网站验证通过。

### RPC与浏览器交互证据边界

升级后12次默认预算的projects/sessions/browser status调用全部成功，耗时0.180–0.211秒；升级前对应12次为0.179–0.241秒。该小样本只证明当次RPC健康，不能量化画面或输入改善。升级前alpha.8真实viewer在原网络tailscale-relay下完成5次点击和输入回传：点击至结果区域canvas变化为2039、2588、719、1130、1331毫秒，count最终5、输入alpha8-test真实回传；采样含Ego操作和轮询开销，不是纯RTT。旧实例原生观察为webdriver=true、ja-JP、Asia/Tokyo。

升级完成时原Ego TaskSpace29消失，listTaskSpaces返回空列表；当时依据浏览器skill异步询问是否允许新建TaskSpace且尚未收到答复，因此未绕过继续浏览器操作或将交互A/B记为通过。此历史阻塞随后取得用户授权，新增实测见[本轮交互记录](15-implementation-status.md#evidence-alpha9-viewer-20261122)。未使用的alpha.9 viewer已关闭。当时尚未测试人工Cloudflare与Claude；后续本次人工进入登录页证据见顶部，账号授权仍未进行。未代做挑战、未修改出口、DNS或代理；普通模式running本身不能替代网站或账号验收。

### VPS清理完成与本机收尾

升级worker释放旧锁后，VPS原测试project的sync历史取得原state.lock排他锁，核对精确树结构且无symlink后删除19文件、11,906字节；没有绕过锁或编辑正式ledger。VPS本轮root安装staging已清理，普通UID loopback测试服务核对PID/cwd/cmdline后停止并清理；复扫/root、/tmp、/var/tmp未见本轮staging。最终VPS projects与sync projects列表均空，3个正式systemd服务active，正式发行与backups保留。本机本轮暂存目录已在关闭SSH复用连接后删除，共74个文件、976,054,591逻辑文件字节（不是实测释放磁盘空间）。临时目录复扫12,874条，仅剩保留的正式tmux socket；56个macOS系统保护目录仍不可访问，未绕过权限。正式安装、alpha.5–9、升级备份、凭据、浏览器profile与agent-remote保留。

本批没有改写产品源码、正式签名资产或冻结发布说明。安装复用原Plan与身份，普通用户宿主/插件和root受管支持的归属不变；模式能力与原授权边界保持；原子安装receipt、升级备份、配置revision及凭据保留分别核对；Chrome先显式停止后重新启动，不把宿主重启当作无感恢复。只清理经准确身份和原锁验证的测试数据，不扩大文件/网络权限。升级记录将当时已完成的健康探测、实际RPC和前版本viewer采样，与当时尚未执行的新版本交互/公网验收分列；后续短时交互及本次人工进入登录页已完成，见顶部独立记录。长期体验、自然切网与Claude账号授权仍待验，beta不推进。

<a id="evidence-public-alpha9-release-20261122"></a>
## alpha.9 公开发行与独立资产验证（2026-11-22）

固定标签`v0.0.1-alpha.9`指向`0b4847d879d10bd2a79b9df348857ab27dafa848`。[发布前CI 44924752906](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44924752906) attempt1最终57/57成功；[发行流水线44929424103](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44929424103) attempt1最终60项成功、1项条件Homebrew检查跳过，跳过不计通过。[Release](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-alpha.9)于UTC2026-11-21T18:13:58Z公开，共23个资产。两端正式升级已完成，独立部署证据见[alpha.9升级记录](15-implementation-status.md#evidence-alpha9-upgrade-20261122)；本次人工进入登录页已独立验证；原网络长期体验、自然切网、真实账号与物理重启继续开放。

独立验证23资产精确清单、GitHub元数据size/digest、四目标Ed25519签名与完整payload hash、PROVENANCE/tag/run/source、公钥及固定源码bootstrap一致。34个二进制只读核验实际GOOS/GOARCH和Go1.26.11；四目标`shiki`另从ELF/Mach-O真实Go字符串符号读取Version/Commit/BuiltAt，分别一致为alpha.9、上述完整源码和2026-11-21T17:19:42Z。其余30个二进制不宣称均保留这些字符串；未执行下载的跨平台二进制。

独立审计脚本曾有两项错误假设，均保留失败与修正范围：正式alpha.8样本的debug/buildinfo不含`-ldflags`，故改用实际ELF/Mach-O符号中的Go字符串；首轮alpha.9签名/payload/GitHub摘要已通过，但要求全部二进制保留BuiltAt时，在linux/amd64的shiki-agent失败。源码确认agent只调用不引用BuiltAt的buildinfo.String/Version，链接器可消除未用字段；改为四目标CLI必须验证三字符串，其余30个仍核平台/工具链及签名payload，不降低任何资产、签名或来源门槛。修正后的完整audit实际通过；没有改写公开资产或将失败测试冒充成功。

| 平台 | 归档SHA-256 | 签名绑定的SHA256SUMS pin |
|---|---|---|
| linux/amd64 | `80b3fef983016d1297f911a7887d18d6db453fda0cd634c1fc1a495e97e1eb94` | `86c06eb1278c3cfd9d4ddcc095b93771a856a60c8fab4fb5e3ec615823232043` |
| linux/arm64 | `39a0ad3e14ee213992498981f256b370d4fd11394509ae51283b7dae38ff6790` | `c1b4aeb2b55e77b4eddfa296c55956f4772b60b9a3dfa6230f0a384f9620771f` |
| darwin/amd64 | `c413f342769e8f7c603168b51dbd46020e584b1236d95866a570b158138a1585` | `97abb022f46146d07035902d814cb4c47a09d5a29ab510233b60f4f76d74c03e` |
| darwin/arm64 | `52086542daac7e28bbb814ad5d715a50dd03371e765f13708cfc913a812dfcea` | `addbccad912290a12669097a20a17f9f55439f332651983e110b0af3b5ad593c` |

Homebrew公式已自动推进，bot提交[`e0888009cc634de8c6e09b0962b44934498f2f7b`](https://github.com/DDSRem-Dev/Shikigami/commit/e0888009cc634de8c6e09b0962b44934498f2f7b)；从认证资产重新渲染与已下载公式逐字节一致，公式SHA-256为`7c57e9bbdc86922edccb677d4a9b2929d703d6e4c19777267847b7202b534ddb`。这不是新版本真实tap安装/重装或受管服务升级验收。

临时审计报告曾位于`/tmp/shiki-alpha9.gGYgWZ/public-audit.json`（SHA-256 `5ddc3679a29f658ece13ec22565643b1ee626be3408f42102c8d5f793f6c2c56`）；本轮曾采集release/run/tag元数据、alpha8-buildinfo-sample.json与audit-assumption-correction.json，现均已按清理授权删除。临时路径不是永久公开存档，摘要保存在本文，公开来源以固定Release与Actions链接为准。可信Ed25519 SPKI指纹为`e21f6dd671ea5042c43c492b759b933dda73985f89a823b43c651ff7c787c17d`。

本批七类复核：没有修改冻结生产源码；目录与SDK/host/plugin依赖方向保持；协议、准入、授权和能力不因发行核验扩大；持久格式、revision、去重与状态恢复仍按候选修复契约；服务/Chrome进程归属、取消和显式启动保持；文件、终端、RFB及网络限额不变；CLI/安装/四目标产物与已冻结说明一致。验证明确区分本地回归、公开签名来源、实际部署及公网人工验收，条件skip不计通过，不以发行完成推进beta或宣称Cloudflare/卡顿解决。

## alpha.9 发行、部署、短时交互与本次登录页验证完成（2026-11-22）

固定源码`0b4847d879d10bd2a79b9df348857ab27dafa848`的完整CI、alpha.9签名公开发行和独立资产验证已完成，见[发行证据](15-implementation-status.md#evidence-public-alpha9-release-20261122)。两端正式升级和RPC健康检查已完成，见[升级证据](15-implementation-status.md#evidence-alpha9-upgrade-20261122)；本机临时材料收尾、短时浏览器交互及本次人工进入Claude登录页均已完成，见[交互与网站证据](15-implementation-status.md#evidence-alpha9-viewer-20261122)；长期体验、自然切网和Claude账号授权仍待验，此记录批次不修改冻结生产源码。

- [x] 用户授权alpha.9公开发行、升级两端及重启Chrome，页面已保存。
- [x] 固定提交完整CI通过后创建不可变tag，完成签名发行及独立公开资产验证。
- [x] 只部署正式签名alpha.9，检查原身份、凭据、浏览器profile及日常模式。
- [x] 完成本轮两端有明确范围的元数据扫描；经既有API暂停并移除旧验收映射，删除两个已停止测试会话、测试项目及两端原验收目录。
- [x] 升级worker释放锁后，以原state.lock和精确结构校验清理VPS旧测试sync历史及本轮VPS安装staging/loopback夹具。
- [x] 完成本机`/tmp/shiki-alpha9.gGYgWZ`临时材料收尾及SSH连接关闭；保留agent-remote、正式安装/历史发行/升级备份、正式归档记录和真实用户数据，不宣称全盘无残留。
- [x] 新版本原网络自有页面短时点击/输入/滚动与显式重连复验，范围及计时限制见本轮交互记录。
- [x] 本次用户人工验证后进入Claude登录页，助手截图复核并完成查看器/临时文件收尾。
- [ ] 长期体验、自然切网及Claude账号授权；不承诺以后不再出现挑战。

<a id="evidence-alpha9-cleanup-20261122"></a>
### 升级前旧验收环境清理与扫描证据（历史快照）

用户本次明确允许删除旧本地Shiki测试目录，取代先前保留该验收项目/会话的安排；agent-remote、正式凭据及work profile仍保留。初次直接unmap被`MAPPING_MUST_BE_PAUSED`拒绝且未产生修改；随后正常CLI pause得到`rev_2214f959bb59b5ae5342fb469abd9391`，再unmap成功。原`terminal-check`与`jp-auth-check`两个测试session均为stopped、attachments=0，经正常CLI删除；测试project随后通过正常CLI删除，没有直接编辑状态库。

两端`beta-accept-20261118-235852`目录删除前逐项核对仅含原`from-mac.txt`（18字节）、冲突副本（19字节）、`terminal-check.txt`（18字节），名称及hash匹配后各删除55字节。本地空`/Users/rem/Shikigami-Acceptance`父目录也已删除，VPS projects根保留。首轮删除检查因冲突副本文件名末尾误多抄一个“1”而拒绝，未删除任何文件；重新列出并核对后才执行正确清理。此前Mac lsof显示sync worker与一个终端cwd引用测试目录，因此先停用映射，未直接在活动同步下删除或杀掉用户终端。

| 扫描面 | 实际覆盖与结果 | 排除及结论边界 |
|---|---|---|
| Mac home | 对`/Users/rem`无深度限制枚举路径与元数据，访问84,860个目录、516,703个文件条目，权限/读取错误0；命名候选仅已知验收目录、正式路径/命令链接及源码repo，没有新增待删开发bin/归档候选 | 不跟随symlink、不跨文件系统；跳过2个同名agent-remote目录、6,597个依赖/cache目录、116个.git、565个目录symlink、12个Library/指定软件缓存根，以及正式state/install/keys和本repo各1。未读取文件内容，不覆盖排除区域或任意改名材料 |
| Mac repo与正式安装 | 初扫repo ignored构建文件0、untracked均为本批源码/测试；提交后补查Git仅主worktree、无未追踪开发产物。三条.local/bin命令链接均指向正式current，当时current为alpha.8 | .config/shikigami下release-keys仅确认目录归属，不读取密钥内容；正式alpha.5–8发行目录及升级backups保留；不清通用软件缓存 |
| Mac临时目录补扫 | 初次只检查`/private/tmp`及TMPDIR根层已知前缀未发现候选，不能覆盖子目录。随后递归补扫发现90个旧shiki-test-* tmux socket；逐个核对socket类型、本UID、连接均为ECONNREFUSED，删除前复核inode相同后全部unlink，0个活动socket。复扫12,881条，匹配项仅本轮alpha.9 staging、正式tmux shiki-501及CI审查代理当前目录 | 不跟随symlink，排除node_modules/.git及本轮staging内部；56个macOS受保护TemporaryItems访问拒绝，未提权或绕过。正式tmux和当前任务目录保留，未将拒绝区域计作无残留 |
| VPS | 对`/root`、`/home`、`/tmp`、`/var/tmp`、`/opt`、`/usr/local/bin`、`/etc/systemd/system`递归元数据扫描共1,258条、错误0，未发现新增dev候选 | 不跟随symlink，排除依赖/cache/.local/profile，保留agent-remote、正式alpha.5–8发行/备份、root支持程序及系统私有目录；不是全磁盘或排除区域内容扫描 |

补充同步私有数据核查：本地sync_mappings已为空，旧map由插件正常归档，ledger中archive的state为archived、operations/conflicts均0。源码复核确认Unmap关闭tree/staging并保留正式归档tombstone。清理时成功取得原state.lock的排他非阻塞flock，验证目标全目录无symlink及精确tree顶层结构后，删除该map的trees子目录（6文件共2,722字节，含3个旧history副本）及空staging子目录；正式ledger的归档、去重和防ID复用记录未修改。VPS对应已删除project的sync历史已只读列清单，但LOCK_EX|LOCK_NB仍阻塞，故未删除、未绕过或替换锁，待升级worker重启释放后再核查清理。

补充运行资源核查：Git只有主worktree，无未追踪开发产物；Docker列表没有shiki测试容器，其他容器保留；VPS systemd仅3个正式Shiki服务，未发现dev服务。清理后正式doctor仍报告alpha.8、7插件ready，work浏览器running并保留；身份、凭据和正式浏览器数据未变。本批清理只使用已有CLI/API及经精确清单、原有锁核验的测试文件删除，未改变认证、插件配置或浏览器启动模式。上表“未发现”仅适用于本次扫描规则及覆盖面；不等于全机无残留。VPS旧测试sync历史、发行/部署后新材料收尾、alpha.9实际安装、原网络稳定性与人工Cloudflare验证另行登记，不能以此次清理提前关闭。


## 浏览器兼容性与响应路径优化（2026-11-21～22，发行前候选验证记录）

以下保留发行前候选源码、隔离复验及失败记录的原始范围；现已随alpha.9公开，发行证据见上，部署与公网人工验收另记。

用户接受“优先日常浏览兼容性”：新启动默认browse，不启用Chrome调试接口；需要原生浏览器诊断时显式切换diagnostic并确认重启。旧实例不静默重启、不伪报browse。此取舍不修改浏览器指纹，不自动处理Cloudflare挑战，不改变出口、代理、DNS或账号登录安排。

已定位的独立代码瓶颈：每个浏览器帧复制整个agent状态；只读RFB请求在慢socket写入期间占用全局控制锁；状态查询在外部runtime/region等待期间占用worker锁；控制通知与约1MiB图像队列共用容量。候选改为单记录即时授权读取、只读写入独立于控制锁、状态检查两阶段释放锁并核对期间变更，以及有界独立控制队列。RFB字节不可丢弃/重排，真正修改桌面的输入仍在权限和控制revision检查下串行发送。当前传输Send不能被控制通知抢占；长期慢连接仍可能取消，不承诺消除网络延迟。

查看器新增Smooth/Balanced/Clear画质选项（noVNC公开quality/compression参数），默认保留原Balanced；显示本机WebSocket接收速率及发送缓冲区，明确不是RTT或远端画面延迟测量。依赖详情折叠以腾出显示空间。Chrome主动正常退出与异常依赖退出区分，状态不得再把用户关闭窗口统一误报为未知依赖崩溃。

局部证据：固定4MiB无关状态的授权基准，原实现21.589–27.827ms，新单记录读取0.762–0.775µs；仅代表该函数/该受控状态规模，不代表真实帧延迟或整机加速比例。只读阻塞回归已在旧实现确定失败；慢状态检查旧实现两个阶段均阻塞Stop，修改后18个Linux并发场景通过。首轮固定源码快照的组合race已通过：Mac cmd/shiki53.765s、client20.244s、agent72.514s、server36.457s、host plugin5.213s、SDK2.435s、state2.595s、browser client1.809s、architecture1.187s；普通UID的Linux arm64隔离容器中runtime3.661s、browser server21.411s、install5.636s、architecture1.134s、daemon跨进程integration10.560s通过。对应Mac go vet通过；Linux运行按脚本冻结源码、只读挂载，未使用用户状态。该快照不覆盖随后发现的持久格式回滚补修，补修须独立验证。真实Chrome模式切换与画面A/B仍另行验证，不能计为实机修复完成。

查看器24项JS回归通过（无跳过），包括画质重连保留、旧状态/控制回调隔离及mode缺失不猜测。Ego隔离合成页在1280×720下折叠画面高362.58px、无横向溢出，检查了Details、只读、失败/unknown展示；这是布局/模拟UI证据，不是真实RFB或网络性能。测试页loopback服务和临时目录已关闭删除。

独立复核发现并修复最初方案的回滚格式回归：Mode不写入profiles.json.Runtime，磁盘维持alpha.8可严格读取的shape；仅浅复制Runtime投影，不增加整库复制。模式权威在新`<instance>.launch.json`，旧`<instance>.json`只接受无mode的legacy。加载前验证ID和固定私有路径，再核对全部Spec/Region后恢复内存mode；新文件缺mode、缺失/损坏/字段不一致均拒绝，不能猜成diagnostic。内存深拷贝、未发布保存失败恢复与PublishedError保留mode。独立审查直接对照alpha.8真实Spec/database及旧manager路径校验，无剩余阻塞问题；本地旧shape读写后再升级不等于正式两端服务回滚验收。

最终补修快照普通UID Linux arm64 race：runtime3.742s、browser server21.323s、architecture1.233s通过；覆盖模式持久兼容、新命名必须有mode、状态/流并发及X11就绪修正。随后仅新增的路径逃逸、非固定路径和旧名携带新mode负向测试在普通UID禁网容器定向通过，无后续生产变更。Linux amd64 runtime交叉编译及Linux相关go vet通过；锁定生成器重复生成17个protocol Go文件逐字节未变。

真实Chrome隔离验收先发现候选X11匹配缺陷：官方Chrome的第一项res_name带profile路径，最初要求两项均为google-chrome导致15秒超时。修为有界quoted res_name解析、不以其作身份、不记录其值，严格核对第二项res_class=Google-chrome与本Chrome PID，并拒绝重复或多余属性行；未延长15秒期限。修补后普通browse实例窗口就绪、原生webdriver=false、/diagnose返回409拒绝、关闭最后窗口退出0且stopped通过。此前失败保留；这不证明Cloudflare放行。

研究依据：[Cloudflare挑战循环排查](https://developers.cloudflare.com/cloudflare-challenges/troubleshooting/challenge-solve-issues/)、[支持浏览器与自动化限制](https://developers.cloudflare.com/cloudflare-challenges/reference/supported-browsers/)、[noVNC公开API](https://novnc.com/noVNC/docs/API.html)、[Tailscale连接类型](https://tailscale.com/kb/1257/connection-types)、[MDN webdriver](https://developer.mozilla.org/en-US/docs/Web/API/Navigator/webdriver)。官方资料支持分别检查浏览器配置、挑战网络稳定性与中继延迟，不能证明本次Cloudflare拒绝的单一原因。已有截图显示重复挑战；页面标识不等于站点侧原因。不得用本机浏览器或自动化通过冒充远端人工登录通过。

最终真实独立runtime两模式复验通过：UTC2026-11-21 16:18:23.324～16:18:41.638（18.314s），官方Google Chrome158.0.8127.36、Linux arm64普通UID、加密测试keyring与Chrome sandbox保留；容器network none、零宿主mount、非privileged、无cap_add。Docker默认namespace门禁曾导致sandbox失败；仅任务容器使用基于Moby default的专用seccomp，增加USER/PID/NET相关clone及仅NEWUSER的unshare许可，其余默认限制和clone3 ENOSYS保持，未改宿主策略。browse实际running/browse、窗口本PID/class匹配、合成loopback页原生webdriver=false、diagnose409、正常关窗exit0/stopped。diagnostic新实例running/diagnostic、原生diagnose200并含observed browser.context、同合成页webdriver=true、SIGTERM后exit0/stopped。两次/proc固定debug flag观察均为空，原因未解释；未把最初精确flag断言失败追认为成功，仅由真实CDP功能证明诊断可用。没有自动处理Cloudflare、修改webdriver或使用真实账号。

该验收binary SHA256为`cf8a6b9de9042d65f6b7d604ace935ae5b50ae90e1932d68117e306b2228ca7a`，33个runtime Go文件清单摘要`6c867ae505ee64cc9ecbe6e6d32aa2801fd75afb4809cce38455039f55211c48`；不是正式发行签名产物。独立runtime测试不代表完整systemd安装、两端Shiki链路、真实原网络画面A/B或Cloudflare放行。真实网络体验、公开发行/升级和用户人工验证继续保留开放。

本批清理完成：合成查看器loopback服务/TaskSpace关闭，专用Chrome测试容器及镜像、私有测试profile/keyring/脚本、临时测试bin/截图及本批新建的.tools目录均已删除；未执行Docker prune或清理用户通用缓存。正式Mac/VPS服务、agent-remote、原项目/会话/profile与凭据未改。用户于2026-11-22明确授权提交推送、CI全通过后发布alpha.9、升级两端并重启已保存页面的Chrome；另授权全面扫描并清理Shikigami开发/临时数据及旧本地测试目录。正式发行与清理执行证据另行登记，不能提前记完成。

七类最终源码复核：业务仍在browser插件、共享单记录读取在SDK状态包、宿主只负责授权/路由，架构检查通过；RPC只增加模式字段及逐层Start能力门槛，既有权限和当前控制revision不放宽。profiles旧shape与launch新代次分离，atomic发布、request_id去重及内存失败恢复均有回归。Chrome独立进程归属、断线不结束浏览器和显式重启保持，正常退出/依赖异常/取消分列。RFB不丢字节不重放、mutating输入保持授权线性化，队列/超时有界；文件与终端行为不扩展。CLI显式诊断及模式切换确认、x11-utils安装依赖与新旧端报错已同步；查看器模式unknown不冒充browse。实现/本地回归、隔离Chrome、正式发行/部署及公网人工验收分别记账，08/15/16/27/30一致，beta未推进。

- [x] 候选组合回归、七类架构复核与隔离Chrome两模式启动验证完成（范围和失败历史见上文）。
- [x] 正式alpha.9两端升级后，本次用户人工验证进入Claude登录页且助手截图复核；仅限本次，不代表账号授权或长期免挑战。
- [ ] 原网络真实使用延迟与稳定性达到可接受水平；beta门槛仍开放。


## alpha.8 用户体验复验反馈（2026-11-21，卡顿与网站验证未通过）

用户反馈仍卡顿，访问claude.ai时勾选Cloudflare后返回验证页。B06实际流畅性与网站可用性未通过，先前短时功能成功不覆盖此失败。账号登录继续暂停，未尝试代做挑战、修改出口/代理/DNS或浏览器指纹。

只读检查：daemon及7插件ready/core ready，现场路由tailscale-relay；work状态为crashed、reason为browser dependency exited。用户随后明确说明已主动关闭远端Chrome窗口，因此该记录不能算意外崩溃，也不能用于解释关闭前卡顿。VPS journal显示23:54:20起依赖清理、23:54:28 runtime退出；源码将某受监控子进程Wait()==nil归为通用dependency exited并标crashed，未保存组件身份。这是退出诊断粒度不足，尚未修改/部署修补。

VPS只读观察：Asia/Tokyo、NTPSynchronized=yes，可用内存约2.5GiB、load约0.18/0.23/0.25；本次时间窗未发现内核OOM记录，不能由事后采样排除使用过程瞬时资源问题。所查Chrome默认内容设置及claude.ai例外未发现JavaScript/Cookie禁止；受管策略仅密码保存/Chrome同步/浏览器账号登录限制，不等于禁止网站登录。VNC日志显示已有Tight/JPEG编码，不能把卡顿简单归为未压缩传输。原生诊断因浏览器已关闭返回failed_precondition，不记成功。

代码固定带remote-debugging-pipe，上一轮原生诊断webdriver=true；仅登记真实运行事实及待核查的兼容性因素，不断言它导致本次Cloudflare拒绝。未取得失败页错误码/Ray ID或站点侧原因，出口IP地理库分歧仍保留；日本时区/语言不能证明网站资格或挑战会通过。未重启用户已关闭的Chrome、未清理Cookie/配置、未执行外部供应商检测。

- [x] 收录用户失败反馈、纠正主动关闭与意外崩溃的区分，核对服务/日志/有限配置。
- [ ] 卡顿端到端归因、浏览器退出诊断改进及候选复验。
- [ ] Cloudflare循环原因与用户手动验证成功；缺少失败页证据，不能宣称已解决。

七类复核：本批只有只读诊断和文档，目录/依赖、RPC权限能力、持久schema/revision、进程策略、文件/终端/网络契约及CLI/安装布局均未修改；区分用户反馈、当前采样、旧版证据与未测试项，08/15/16/30一致，不推进beta。

<a id="evidence-alpha8-upgrade-20261121"></a>

## alpha.8 两端升级与自动化复验（2026-11-21，功能子项通过，稳定性仍开放）

用户授权升级VPS和本机，再自动完成可执行测试；Claude授权继续暂停、网络专项暂跳。本批只部署公开签名alpha.8（固定源`3c0d9454cf139eb854e046ae74a1579c9045f7b4`），未安装开发bin，未修改生产源码、DNS、出口、代理或路由，未重启物理主机。保留agent-remote、现有映射、两个原会话、work profile及凭据。记录日期采用本轮工作区日期；现场采样原时间戳保留其原值，不据此宣称主机时钟已验收。

- [x] Debian12/amd64与Mac arm64正式alpha.7→alpha.8签名升级；独立信任公钥、归档hash及checksums pin核验通过。
- [x] VPS18个/Mac14个已安装payload、两项root支持程序匹配已验签alpha.8；receipt active/current一致、无pending，原安装Plan保持。
- [x] 实际终端attach/detach保活、配置快照恢复语义，以及sync/port/browser/region下列功能子项复验。
- [x] 清理本批测试会话、快照、同步测试文件、转发、HTTP夹具、浏览器测试页和暂存文件；原文件、原会话及配置/凭据保留核对通过。
- [ ] 默认空shell仍为`/bin/sh`，方向键交互缺陷未修复；B02不得整体勾选。
- [ ] 浏览器状态偶发超时与一般连接稳定性、长用体验继续开放；不以短时通过抵消失败。
- [ ] 真实Claude授权/任务、切网/睡眠、物理重启、新环境首装/本批回滚及其它原生平台保留未测；不发布beta。

升级时沿用完整原Plan和已注册加密凭据。daemon普通用户、原节点地址/daemon身份、Mac配置/plist/凭据及VPS配置/Token/auth/加密credential/keyring摘要均保持；插件配置revision保持，7个实例均alpha.8 ready。支持setup在明确停止Chrome后更新两项root程序并复用密钥环，官方依赖步骤将Chrome157.0.8103.78更新到158.0.8127.36。work profile定义及地区revision不变，随后启动成功。未采集停止前标签页清单，不能宣称原打开页面全部恢复。两端正式升级备份保留。

安装器服务激活后的首次探测均出现`node_uninitialized`/退出码3；后续节点online，未重新登录或换Token。VPS应用授权仍`CLAUDE_LOGIN_REQUIRED`，与core ready分列。升级前同步曾offline/REMOTE_RETRY_EXHAUSTED，升级重启后active；这是该次重启恢复证据，不能替代自然断线恢复。更新支持前首次`browser stop work --yes`返回`unavailable: service unavailable`，只读确认仍running且无pending任务后，新一次显式stop成功；首次失败保留。

### 实机功能与限制

| 项目 | 正式alpha.8实际结果 | 不能据此关闭的边界 |
|---|---|---|
| B01基线 | 客户端/daemon/7插件alpha.8 ready，最终doctor core ready、节点online，实际项目/会话查询成功 | doctor不检查Claude登录；短时健康不是长期可用性 |
| B02终端 | 临时shell普通用户与cwd正确；实际detach/attach后PID和环境标记相同；配置快照恢复为新PID且不带原环境标记。原两个tmux PID在升级/收尾均保持 | 默认`/bin/sh`方向键问题仍在；快照不恢复进程/文件/对话 |
| B04同步 | 隔离子目录双向初始/后续更新、pause/resume、keep-local/remote/both、重命名、删除、删除对修改冲突均核对两端字节；清理后active、pending=0、conflicts=0 | 6次resolve使用默认调用路径，耗时0.594–4.938秒；均小于旧5秒，不能声称本次实机跨越旧截止或验证完整15分钟上限。部分提交/未知结果故障注入仍依赖已有隔离回归 |
| B05端口 | 仅127.0.0.1 HTTP夹具；首次0.695秒返回正确。停止后端listener时新连接失败、已有TCP仍可响应(0.127秒)；同一forward/listener恢复后新请求1.065秒成功，未改3000/40000 allowlist | 后续浏览器阶段向原临时端口请求另有reset；查询发现forward已在另一个随机端口listening。未取得该次重绑定原因，单列一般连接稳定性，不能冒充目标拒绝回归失败或证明无故障 |
| B06浏览器 | 鼠标点击/英文退格、滚动、中日IME实际转换、显式剪贴板发送/接收缓冲区、CLI上传/取回以及远端Chrome原生文件选择/下载均通过，文本/字节匹配。强制只读查看器不能取得控制；第二可控查看器接管后旧页Read-only且撤销旧控制提示 | 未读写用户OS剪贴板、未做账号登录/Cookie续期；不宣称主观流畅或长期低延迟 |
| B06重连 | 5次显式Reconnect均先Connected+Read-only，再确认Take control成功；重连1.243–1.677秒，总含重新接管1.907–2.333秒。关闭所有查看器后Chrome同实例running，重开后测试页内容保留 | 原生文件测试时出现一次状态查询unknown/timeout，Refresh status恢复；此失败仍保留，不把5次重连通过推广为状态/所有竞态闭环 |
| B07地区 | host为Asia/Tokyo、ja_JP.UTF-8；JP只读plan changes为空、changes_dns=false。远端work原生Chrome诊断timezone=Asia/Tokyo、languages=ja-JP/ja且同实例配置一致 | 未调用外部供应商，出口IP地理库分歧未解决；Secure DNS/WebRTC/权威DNS仍unknown。缺fonts-noto-color-emoji警告保留，未为测试临时安装 |

同步keep-both正常冲突在默认路径2.094秒完成，两端两份内容均保留。测试只使用既有验收项目内独立子目录，不修改原三文件。浏览器原生上传28bytes、下载30bytes均经实际文件通道与内容校验；本地查看器只承载VPS画面，没有用本机Chrome冒充远端环境。短采样12次默认projects/sessions/browser-status查询全成功，0.174–0.465秒；与上述超时分别登记，不声称本批所有请求无失败。

收尾通过摘要对比确认：原三文件、本机/VPS配置与凭据未变；原两会话PID不变，临时两会话和一个配置快照已删除；临时forward为空，HTTP夹具监听已关闭，临时目录及浏览器上传/下载文件已清理；owned远端测试标签页关闭，原about:blank保留，work同实例running、无controller、restart_required=false。同步测试文件先经正常同步删除并核对两端为空，再移除空目录。VPS本批升级暂存目录、本机下载包/验签工具/测试脚本/截图与私有临时日志已移除，SSH复用连接已关闭；正式release、安装备份与agent-remote保留。验收摘要保存在本节，未将Token、引导URL或账号秘密写入仓库。

七类文档复核：目录/SDK-host-plugin归属不变，无业务源码修改；RPC、准入、权限能力不变且不扩大负向授权验收；原Plan/凭据/文件保留，测试使用真实revision与request_id但不冒充故障原子性矩阵；tmux/Chrome独立生命周期按实际验证、物理重启另列；文件冲突和端口拒绝语义核对，终端默认shell及一般连接失败仍开放；CLI/安装只用正式包，签名payload及root支持程序复核；实现回归、公开发行、本批功能实测和外部门槛分开，跳过/unknown不计通过。08/15/16及30同步更新，历史alpha.7失败不删除。

## alpha.8 授权发行（2026-11-20，公开发行与独立验签完成）

用户明确要求发布v0.0.1-alpha.8，用于下一轮修复后测试。本次范围为提交当前修补、固定源CI、不可变新tag、四目标签名归档/公开Release及自动Homebrew更新。本发行批次结束时VPS/Mac仍为alpha.7；随后两端升级见本页2026-11-21记录，升级与现场复验不由发布成功替代，账号授权及网络专项维持既有暂停安排。

变更包含会话错误码/未知结果提示、ResolveConflict复合预算和显式timeout、未决keep-both副本保护、端口目标ECONNREFUSED隔离、浏览器attachment控制归属和迟到响应提示。沿用上一批七类审查，发布批不改RPC方法、权限能力、持久schema或安装布局；插件生产边界与SDK封装保持。已知实机失败和默认shell体验复验保留，不将alpha预发布写成beta验收通过。

本次发布前本地复验：10个受影响包/架构的race组合通过（browser/client与architecture复用上一批有效缓存，其余8包新执行）；对应go vet通过，查看器18/18、无skip，git diff --check通过。Darwin上既有sudo PTY平台跳过不计Linux通过；完整四平台检查最终结果见下方固定源CI及发行记录。

首轮完整CI44572726453（源86f4bcc）暴露两类遗漏，失败保留：协议诊断回归误按protobuf map编码字节顺序比较，定向race100次22次失败；改为解码后核对完整ErrorInfo schema及错误码，1000次通过，errors/SDK/version整包race各5轮通过。多主机诊断夹具仍断言ResolveConflict旧5秒默认，并漏传显式timeout标记；本地先复现默认断言，再新增显式5秒用例RED复现遗漏，修后按新15分钟默认、2分钟父截止、显式5/1/90秒和较早取消验证，multihost整包race2.895秒、vet通过，client/architecture有效缓存通过。该批只改测试/诊断helper与文档，生产行为、权限、状态和安装布局未改；不放宽既有字段断言或未知结果保护。docs25注明旧证据时点；随后新固定源CI通过后才创建tag，结果见下文。

固定源CI终态：3c0d945的CI44579854243共57项最终成功；attempt1为56成功/1 macOS终端runner未领取取消，等待其它长项结束后仅补跑该任务，attempt2成功。按同源各job最新成功结果合并，不把取消计通过。首轮86f4bcc的CI44572726453最终45成功/7失败/1 runner未领取取消/2前置跳过；WSL唯一失败为旧multihost超时断言，其后未执行阶段不计通过。新固定源完整WSL已成功。新tag已推送，同源发行结果与独立公开验证如下。

- [x] 取得alpha.8公开发行授权，编写固定版本发布说明。
- [x] 冻结提交3c0d9454cf139eb854e046ae74a1579c9045f7b4的完整CI44579854243通过，创建并推送不可变v0.0.1-alpha.8 tag。
- [x] 同源发行检查、四目标构建/签名、公开23资产和Homebrew更新成功。
- [x] 下载公开资产独立验证签名、来源、内容和公式。
- [x] 两端升级（后续2026-11-21批次已验证，见本页顶部）。
- [ ] 修复后全部实机门槛闭环（本批功能通过项与仍开放项见顶部；不等于全部验收通过）。


<a id="evidence-public-alpha8-release-20261120"></a>

### alpha.8 公开发行证据

公开发行终态：[Release44593959710](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44593959710)成功，60项成功、1项条件跳过（不计通过）。于`2026-11-19T20:20:26Z`公开[v0.0.1-alpha.8](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-alpha.8)，`draft=false`、`prerelease=true`，精确23资产；自动Homebrew更新提交`5cb2a5c3c341127b407cd45feaf7986a765543ee`。标签固定源3c0d9454cf139eb854e046ae74a1579c9045f7b4，旧标签与资产未改。 首轮发行复核50项成功、4项取消、4项跳过（含未执行的构建/签名/发布）；3个ARM取消任务未取得runner，Intel内部包任务运行中取消、日志无测试fail事件，取消具体原因未明。第二轮仅补跑未完成项，3项成功、ARM发行工具再次未获runner取消；第三轮该项成功并完成后续发行。最终按同源最新job结果为60成功/1条件跳过；取消、未执行阶段不计通过，未改源码或放宽检查。

从公开Release重新下载23资产，以仓库独立公钥核验四目标Ed25519签名、归档/逐文件SHA256、checksums pin、精确资产清单与GitHub资产摘要；PROVENANCE绑定固定tag/source/run，bootstrap与冻结源码逐字节一致。四目标shiki内嵌版本、提交和构建时间从ELF/Mach-O的buildinfo字符串符号读取，OS/架构由Go build info核对；初版临时核验脚本误假定go version -m暴露ldflags，断言失败后改为直接读取实际符号，再完整重验通过，公开包未修改；Mac arm64仅在私有临时目录执行version，返回alpha.8及3c0d945完整SHA，未安装或启动服务。由认证资产重新生成Formula，与自动更新的远端公式逐字节一致，Ruby语法通过。公钥SPKI SHA256：`e21f6dd671ea5042c43c492b759b933dda73985f89a823b43c651ff7c787c17d`。

| 目标 | 归档SHA256 | SHA256SUMS pin |
|---|---|---|
| linux-amd64 | `f02903a62109529e7a1627722896305bcd2b0d3101e0f649b9d8c78a224e68c8` | `8de243f94a742bb34d9dc0e059a4b20eeca3e72f6dca5a9b24cc2fc592cc7f4a` |
| linux-arm64 | `757c743ed1f1d476e816fff2b45f9e7e2a32dc2ee0f5698f58d61f15ca1e7cfa` | `941cf908cff3b0ae650823e80d4816e08ceaea25cd4a32861cff27049a44b35a` |
| darwin-amd64 | `ef71b473f635cc1c90e25ce4a7bf2d822b9773fb123235a7e92840c70707ee71` | `aafb95f2cf2fda690bb85e57a2b1773d7140a3b3afbd57dd50ac204b878235a1` |
| darwin-arm64 | `f131a376db1d25b7d17cddacdf16d621d76194d3b9c90c9102d201690584d735` | `e12030f4d1ed6373f29d0875fdb4218a26512814767875584fbb680ac11b5ebf` |

发行收尾七类复核：插件/宿主/SDK依赖边界保持；RPC传输、准入和权限能力未改；持久schema、revision和去重不变；取消、进程归属与服务启动策略未改；文件/终端/网络语义按本批回归记录，未把间歇实机失败改为通过；CLI/安装布局不变，下载入口统一alpha.8，Homebrew公式仅由流水线认证资产生成；实现回归、公开发行、两端部署和外部门槛分列，跳过不计通过。README、08/15/16、18/22/28/30同步。

## 用户授权自动化实机验收（2026-11-19～20，本轮完成，失败待候选复验）

用户明确授权助手接手命令和可自动化验收，只有确需人工的步骤再暂停。正式两端仍为alpha.7；未安装开发bin。只在既有beta验收项目/专用临时资源内操作，保留agent-remote、原两会话、work浏览器、凭据/节点身份。网络专项暂跳，Claude授权继续暂停。

同步批：先以原最新generation明确keep-local解决已保存REMOTE_CONFLICT_V5的副本，再验证VPS该副本hash后明确keep-local解决原件；两端原件为LOCAL_CONFLICT_V5、副本为REMOTE_CONFLICT_V5，映射active、0 pending/0 conflict。独立测试子目录覆盖本地/远端首次显式方向、已有基线双向更新、重命名（旧名删除、新名首次选择）、已有基线删除、暂停后的双端修改冲突、keep-both双版本、删除对远端新编辑冲突及显式保留远端。新keep-both2.497秒完成；所有结果用两端字节/哈希独立核验。测试新增文件经正常同步删除，两端临时目录已清理，用户原文件和原副本保留。

本批resolve均显式2分钟预算以进行alpha.7现场恢复/功能核验，其他查询和pause/resume用正式默认预算；不把这次功能成功当成旧默认超时已修复或新源码已部署。相关通过记录与未发布源码分开，B04新版默认交互复验仍未关闭。

会话/端口实机补充：通过真实shiki attach新建临时无Claude shell，detach/reattach后PID1528619及环境标记保持；配置快照恢复为新PID1545654、标记unset，目录/描述/tags一致，源和恢复会话均无Claude。恢复会话与临时快照已删除；源临时shell曾供浏览器测试HTTP服务使用，最终清理见下文。

B05失败：既有allowlist为3000/40000，无配置修改。新转发绑定127.0.0.1随机端口，3次真实HTTP返回正确（2.951/1.586/1.737秒），lsof确认仅loopback。通过shiki终端Ctrl+C停后端后下一请求失败符合预期；再启动后端并独立从VPS localhost核验响应成功，但本地forward仍offline/REMOTE_UNAVAILABLE、bound_port=0，原socket拒绝连接。原失败转发已显式stop并核对移除。期间独立SSH master曾40秒超时后broken pipe，重新认证后恢复，此诊断通道异常单列，不冒充Shiki转发失败根因。源码已定位目标ECONNREFUSED被当作整个转发Unavailable，修补与隔离验证见下文，不部署开发bin。

B05源码修补完成（未发布）：服务端仅在授权及allowlist检查后、实际loopback Dial明确ECONNREFUSED时生成固定Unavailable/shiki.port/TARGET_CONNECTION_REFUSED零metadata标记；公共错误过滤限前8详情/每项2KiB、拒绝protobuf未知字段并重建规范详情。插件业务经SDK helper使用，客户端只在CONNECTED前结束该新TCP而保留原listener和其它连接，不自动重试/重放；一般Unavailable、权限/认证及已建立流失败仍沿原策略。取消/截止优先，cause/wrapped/joined错误不携带目标拒绝标记。

验证与复核：新目标拒绝用例先RED复现listener关闭；实际root架构检查发现port业务直接导入pkg/errors违反SDK白名单（FAIL0.597秒），未放宽测试，已改SDK封装并复核导入。独立review另发现取消组合标记优先级问题，8例RED0.627秒后修复。修后race全包errors1.329秒、SDK2.662秒、port/client6.440秒、port/server4.211秒、architecture1.409秒均通过，新增场景无skip。最终root组合race：真实双worker/TLS port集成18.728秒、agent72.635秒、server36.596秒通过；go vet相关错误/SDK/port包通过。真实集成验证同一本地临时端口、后端拒绝时既有TCP保留、恢复后新连接成功、半关闭尾数据和worker回收。正式alpha.7的B05失败继续保留，隔离修补通过不能代替发布后实机复验。

七类源码复核：port业务留在插件，固定错误诊断归公共错误包并通过SDK使用；RPC方法/权限/能力及持久schema不变，只新增严格限定详情。未知错误仍脱敏，取消和进程回收保持；无TCP重试/字节重放，无安装、配置、网络策略变化。06/10/14与08/15/16同步，先前漏判依赖边界的review结论已撤回，更正后的架构检查才计通过。

浏览器实机子项：保留原work实例及原网站标签，只新建loopback测试页。真实鼠标点击、英文输入/退格、滚动、Fcitx拼音候选及中文测试、Mozc候选及日本語テスト均通过；测试页回报独立核验已提交文本。显式剪贴板Mac查看器→VPS文本框、远端Copy→查看器文本框通过；未读取/覆盖用户OS剪贴板，系统剪贴板权限路径不计通过。CLI上传并取回hash一致；VPS Chrome原生选择暂存文件、页面读取正确；Chrome实际下载到暂存区并经CLI取回字节一致。最初keyboard.type未在canvas发送文字，逐键事件及GTK显式Open后成功，记录为测试操作修正，不抹去过程。

浏览器失败保留：连接中曾出现状态查询超时，后恢复；计划5轮重连/接管测试在15秒观察期限未确认Controller而中止，后来实际界面亦显示operation timed out。15秒观察期限短于产品30秒预算，不能将观察超时本身认定为底层超时，亦不能将5轮计通过。刷新后一次明确接管恢复；没有增加产品超时或自动重放输入。强制只读查看器Connected且Take control禁用已验；持续低延迟、网络恢复及长稳仍未通过。

浏览器源码修补（未发布）：POST control固定attachment/instance/revision，锁内核对当前连接，转发绑定原attachment取消上下文，拒绝旧请求改绑；仅当前generation、匹配scope流事件授予/撤销控制，HTTP/快照不授权。已确认流结果优先于迟到HTTP超时，撤权同步提示；重连取消旧control/status请求，旧finally不释放新刷新槽。新增Go/JS回归先RED再GREEN；代理race、vet通过，最终root race browser/client2.073秒、architecture1.658秒，viewer18/18通过、无skip。独立复核发现撤权后旧提示残留，同批补第18例；未更改操作超时、远端协议、权限能力、持久schema、生产依赖或安装布局。27契约与08/15/16同步；这些竞态修补不等于已证明所有实机卡顿根因或已在alpha.7生效。

最终实机收尾（2026-11-20）：第二个可写查看器显式接管成功，旧查看器变Read-only、新查看器Controller；旧alpha.7的旧提示仍显示You control已现场复现，权限徽标与提示矛盾单列，不能宣称旧版UI正确。最后一次默认5秒sessions list在5.047秒deadline_exceeded；后续显式30秒查询0.714秒成功。此为间歇失败与随后恢复，不视为延长预算解决问题。网络专项仍暂跳，未擅改路由/代理/服务或进行Claude授权。

清理及最终状态：仅删除本轮两个临时会话、一个配置快照、测试转发、HTTP fixture、上传/下载测试文件和查看器；测试Chrome标签关闭，原两标签保留，原work实例未重启。最终原两会话保留、forwards为空，loopback3000已关闭。同步一度offline/REMOTE_RETRY_EXHAUSTED、0 pending/0 conflict；doctor仍ready/7插件ready。核对当前revision后以独立request_id显式resume，恢复active（末次观察1.091秒），原件LOCAL_CONFLICT_V5、副本REMOTE_CONFLICT_V5及terminal-check三文件两端字节一致，冲突为空。此为一次显式恢复，不计自然自恢复或持续稳定通过。SSH私有控制连接、临时脚本/截图/查看URL随后统一关闭清理；项目、映射、用户原文件、副本、凭据和正式安装保留。

- [x] 自动化恢复本次部分keep-both，双版本两端保留。
- [x] 正式alpha.7同步功能子项及新增测试资源清理（resolve显式预算）。
- [x] 完成本轮port/browser/配置快照自动化执行、记录失败及专用测试资源清理；不表示全部功能通过。
- [ ] B05后端恢复、B06控制/状态超时及同步重试耗尽的候选实机闭环；短时恢复不关闭稳定性门槛。
- [ ] 最终发行候选默认预算实机复验、人工账号/体验及未测环境门槛；beta未发布。

七类复核（实机操作部分；源码修补另见上文）：测试未部署源码、改变RPC/能力/权限或持久格式；使用正式CLI经agent/插件执行同步决策，SSH仅做普通UID测试输入与独立文件核验。最新generation和独立request_id保护每个明确动作，无超时盲重试；不直接编辑ledger。文件变更限验收目录，未启停正式服务或修改网络/安装布局。原失败不被本次成功覆盖，08/15/16同步，外部结果与隔离回归分列。

## keep-both部分完成与恢复修补（2026-11-19，实机未通过）

用户以固定request_id `beta-keep-both-v5-01` 对from-mac.txt选择keep-both后返回deadline_exceeded。只读核对：本机原文件仍为LOCAL_CONFLICT_V5，唯一新副本保存REMOTE_CONFLICT_V5；映射conflicted、pending_files=2、conflict_count=2。原请求持久结果为failed/RESULT_UNKNOWN_RECONCILE_REQUIRED，副本日志为remote原文件→local副本的committed/possible，远端副本尚缺失，原冲突未解决。这是部分完成，不能解释为全部成功仅丢响应；不重复旧请求或换ID碰运气，不删副本、不编辑ledger。

代码审查确认ResolveConflict共用短unary默认5秒，而keep-both需要多次远端观察/读写，外层预算会提前截断已有按大小计算的文件预算。journal在未知结果后保留两端和冲突是必要安全行为：不能凭目标缺失断言从未提交，也不能自动恢复已经被用户删除的副本。修补同时检查操作预算、显式timeout/父取消、未知结果提示以及副本递归冲突保护；不能仅延长期限就宣称实机故障解决。

当前源码修补（未发布）：ResolveConflict采用有界复合文件预算，保留单流大小预算、metadata短预算和较早父截止，显式5秒也不被默认策略提升；CLI在发送前/后分开报错，保留真实request_id和错误码，不承诺已消失冲突能经CLI重放，不自动查询或重试修改。交互等待提供进度，JSON结果仍只在stdout。插件对未决副本或存在未决副本的原路径拒绝再次keep-both，状态区分CONFLICT_COPY_REVIEW_REQUIRED；两端不一致时，只有最新generation的显式方向决定才能解决副本，原始冲突继续保留。已settled历史副本的新冲突仍支持全部正常选择。Copies仅落盘而尚无fileOperation的窗口也纳入两端重观测；两端相等才收敛，绝不由单端缺失自动补写。

- [x] 记录固定请求与部分提交证据，区分保留数据和操作成功。
- [x] 源码修补与隔离行为/竞态回归、七类架构复核（平台跳过及既有限制另列）。
- [ ] 正式候选发行/升级及同场景实机复验；B04与beta门槛仍未通过。

验证：新增副本回归先RED复现旧版误分类及重复/嵌套copy，再GREEN。精确race：sync10.707秒、client1.198秒、CLI7.884秒，新增场景无skip。真实UDS/HTTP2调用延迟5.1秒验证越过旧5秒截止仍成功，并覆盖显式5秒/100毫秒、父deadline/cancel、进度回收、原始ID、JSON与不自动重试。全包`go test -race -p 2 ./internal/client ./cmd/shiki -count=1`通过（20.290/53.263秒）；既有TestConsoleSudoPipe在Darwin跳过，单独-v确认，不能计为Linux sudo PTY通过。root组合`go test -race -p 2 ./plugins/sync/... ./internal/server/session ./test/architecture`通过：sync/client17.299秒、架构1.364秒，其他相关包复用有效缓存，不冒充新实机运行；sync根包无测试文件。`go vet -p 2 ./cmd/shiki ./internal/client ./plugins/sync/client ./internal/server/session`通过。正式alpha.7只读复查仍为原冲突和唯一副本冲突，generation/两份内容hash未变；未代执行恢复。

七类复核：生产目录和依赖边界不变；无RPC/能力/授权扩张；ledger schema、原子条件提交及请求去重保持，未知请求不重放。阶段文件/取消/重启证据保留，进度协程结束前回收；原有task互斥锁等待不支持取消的限制仍在，前端预算不表示worker排队回收上界或status即时返回。文件副本恢复须观察相等或用户明确方向，不自动恢复被删副本，旧generation拒绝；超时规则是复合文件操作策略，不修改网络路由或以预算掩盖丢包。CLI所有Open入口统一传递显式timeout标记，进度不污染JSON，安装布局/正式bin未变。独立复核发现显式5秒入口漏传及未prepare副本被ignore隐藏的问题，已同批修正并回归；10/13/14契约说明与08/15/16一致，本地检查和外部/发布门槛分开。

本轮尚未部署开发bin或改变正式服务、映射及测试文件；网络专项继续暂跳、Claude授权保持暂停。

## 用户终端保活回传与同步准备

用户按已有terminal-check的退出查看/重新附着步骤回传：普通用户shiki、测试项目目录正确，前后PID均3280565，SHIKI_RESUME_CHECK均resume-1795098825-3280565，terminal-check.txt仍为SHIKI_TERMINAL_OK。本子项按用户实测回传记为通过；不是助手独立重放，也不扩大为B02全部场景或默认shell修补通过。中文弯引号导致的Bash续行已由Ctrl+C及正确ASCII引号恢复，不记作终端传输故障。

随后助手只读核对本机对应测试目录存在且为空，同步映射已存在、state=paused。下一步由用户在该测试目录创建from-mac.txt并显式resume，查看status/conflicts；新映射无共同基线时的初始冲突须逐项确认，不自动选择覆盖方向。助手本轮未恢复同步、写测试文件或改变远端会话。网络专项仍按用户要求暂跳，Claude授权继续暂停。

后续用户执行resume后立即见reconciling/空冲突列表；助手复查确认已形成from-mac.txt（仅本地存在）和terminal-check.txt（仅远端存在）两项INITIAL_SYNC_CHOICE_REQUIRED。期间一次状态为offline/REMOTE_UNAVAILABLE，随后真实projects list成功、sync状态恢复conflicted；该短暂失败保留，不扩大为持续离线。下一步仅指导用户分别keep-local和keep-remote，助手未代执行冲突决策，双向字节一致性仍未验。

用户随后对terminal-check.txt连续3次确认keep-remote，前台均deadline_exceeded。助手没有重提resolve或改变冲突选择；只读复查当前映射active、冲突列表空，本地from-mac.txt为from-mac-v1\n，terminal-check.txt为SHIKI_TERMINAL_OK\n。插件持久请求记录有该远端保留冲突的2条failed/RESULT_UNKNOWN_RECONCILE_REQUIRED，本地保留冲突有1条succeeded；不能将三次超时都解释为已成功但响应丢失，也不能从记录数量确定每次请求的具体阶段。当前两文件共同基线已建立、operations为空，说明数据随后经核对收敛；VPS当前文件内容的独立用户核验仍待完成。ResolveConflict当前普通IPC默认5秒且包含多轮远端观察/传输；未取得逐阶段计时，不通过提高预算或新ID重放掩盖故障。数据收敛与前台超时分别登记，B04整体不计通过，sync resolve的超时阶段及未知结果恢复提示仍待修复/复验。

后续用户回传VPS两文件内容正确，并依次确认本地v2→远端、远端v3→本地、暂停期间本地v4不传输、resume后v4传输正常；上述为用户执行反馈，不冒充助手独立重跑。双端冲突步骤中用户立即查询仍为reconciling/空列表，本地保留LOCAL_CONFLICT_V5；助手随后只读查询确认conflicted、1 pending/1 conflict/CONFLICTS_PENDING。共同基线与两侧哈希分别匹配paused-local-v4、LOCAL_CONFLICT_V5、REMOTE_CONFLICT_V5，确认已形成有共同基线的双端修改冲突，两侧差异未被自动抹平。下一步由用户显式keep-both，成功响应及两端保留副本仍待验；原resolve前台超时问题保持开放。

- [x] 同会话进程/临时标记及文件保留的用户回传证据。
- [x] 测试目录和既有paused同步映射的只读核对。
- [ ] 初始文件同步及冲突决策、双向/暂停/删除后续功能验收。

七类复核：本批仅记录用户证据及只读状态，生产依赖边界、RPC权限能力、持久格式/去重、进程与取消恢复、文件/终端/网络限制、CLI和安装布局无变更；通过项限本子项，后续指令不算已执行，08/15/16同步。仅文档变更，差异格式检查通过，不重复软件测试充当新功能实测。

## 切回原网络的分段诊断（2026-11-19，间歇问题未定责）

用户随后明确暂时跳过网络专项排查，先逐项执行其他功能验收；网络项保持未完成，不要求先修网络才做非账号测试。下一步使用已有terminal-check验证detach/attach的同进程与临时环境标记保留，然后再进行sync、port、browser及快照测试。Claude授权仍暂停；此安排是测试顺序调整，不是这些项目已通过。

用户要求先分析原网络供其自行处理，再在原网络继续验收。此时正式alpha.7自动保持ready/tailscale-direct，agent与daemon身份不变，doctor/core及7插件ready；未再执行connect或修改任何网络配置。原网络默认en0网关10.0.0.1；系统HTTP/HTTPS/SOCKS/PAC开关均未启用，这不等于检查了所有应用或路由器代理。路由前段为10.0.0.1→192.168.1.1→100.70.0.1，经运营商网络到VPS共13跳；只能证明有多级路由，不能单凭地址证明每层NAT、将星号判为转发丢包或将两级路由定为根因。

无线统计（只保留信号/协商字段）：5GHz信道44、160MHz、802.11ax、RSSI -35dBm/噪声-96dBm、协商2401Mbps。支持当前信号很强；协商速率不等于互联网速率，不能排除历史干扰。非交互wdutil无管理员认证不可用，改用只读system_profiler取得统计，无权限策略修改或密码请求。初组网关/VPS各30次全部收到；同一序号出现网关73ms与VPS210ms尖峰，采样与无线信息读取可能重叠，不能排除诊断扫描造成抖动。随后不读无线信息、不启动浏览器、不改配置重复60秒采样：

| 测量 | 结果 |
|---|---|
| 网关10.0.0.1 | 60/60，均值2.814ms，最大5.014ms，无超过30ms样本 |
| VPS公网地址 | 60/60，均值106.437ms，最大122.681ms |
| 上级网关192.168.1.1（独立20次） | 20/20，均值4.186ms，最大18.122ms |
| VPS 1200字节payload、DF（10次） | 10/10，均值104.589ms；只排除这一路径/包长/窗口的明显失败，不宣称完整PMTU验收 |
| Shiki sessions list/browser status（12次） | 全部成功，0.254–0.455秒，无延长期限 |
| Mac en0该60秒平均流量 | 下行3.031Mbps、上行0.948Mbps；不是全屋WAN流量，不能单独证明无排队 |

结论：当前原网络短请求和空闲连接恢复正常，先前丢包/重传及热点改善证据仍成立，但不能把历史3/8 ICMP未回复当成持续37.5%丢包率。优先排查间歇性的原宽带路径损耗或路由器/WAN排队；当前不支持持续弱信号、服务未启动或已被迫DERP作为主因。网络类别、运营商、Mesh/中继与其他设备流量已向用户询问，尚不能凭网络段自行补全。建议用户在同一宽带做主路由有线对照、查看全网尤其上传占用；仅在故障时依据网关/终点共同采样区分局域网与外网，必要时请供应商核查去程/回程。没有证据就不要求改桥接、MTU、DNS或开放防火墙。用户处理前不继续账号或声称稳定性通过。

- [x] 当前原网络只读分段测量，扫描干扰后的独立静默复查；保留历史失败及当前恢复两组证据。
- [ ] 用户确认网络拓扑/流量或执行同宽带有线对照；故障时定位具体损耗段。
- [ ] 处理后在原网络重测真实浏览器、会话及后续B02/B06/B08/B09；短时恢复不能直接关闭beta门槛。

七类复核：本批无生产源码/目录/依赖、SDK-host-plugin边界、RPC能力权限或持久schema改变；使用既有只读RPC与有界网络探测，未创建/删改会话或应用数据。身份与进程所有权、取消/恢复、网络限额及CLI安装保持；未访问新诊断供应商、改代理/DNS/路由/防火墙或启停服务。外部样本及未知点如实分列，08/15/16同步，检查差异格式和三处证据一致性；无代码变更，未重复软件回归冒充网络通过。

## 手机热点对照（2026-11-19，明显改善但稳定性仍未闭环）

用户明确告知Mac已切换手机热点，在同一正式alpha.7、同一work Chrome实例、1440×900默认画质及相同无公网请求data测试页下复测。首轮本机agent ready、内嵌节点online，但服务器停在RECONNECT_EXHAUSTED，8个远端列表/状态调用在12–16毫秒内被拒绝；这属于已耗尽的重连状态，不能计为热点上8次实际网络探测失败。按现有doctor恢复指引显式执行一次`shiki connect <saved-server-id>`，0.637秒恢复ready/tailscale-direct，daemon身份与credential_revision保持；未重启服务、清空状态或重新登录节点。有限自动重连后要求显式恢复符合02的最多5次契约；普通命令仅显示service unavailable的恢复提示仍有改进空间。

| 同类测量 | 原网络（上一批） | 手机热点 |
|---|---|---|
| ICMP，8次、间隔0.5秒 | 3次未收到回复；收到部分均值127.1ms | 8/8收到，平均87.879ms、范围84.966–97.542ms |
| 默认画面点击→变色 | 首组5次2372–6843ms，中位4566ms；其他样本最慢7977ms | 8次1466/904/2122/909/1127/926/1006/1023ms，中位1014.5ms |
| 远端列表/状态 | 操作期间约0.25–4.16秒，另一次查看器状态超时 | 重连后首组8次0.211–1.024秒；点击期间10次0.221–1.927秒；18次全部成功 |
| 新建空shell | 用户此前2次deadline，之后助手一次1.033秒成功 | 新建独立临时会话0.485秒成功，ready/Claude stopped，默认预算不变 |

这支持原网络路径对卡顿有显著影响；对照还包含显式重建连接和不同时段，不能归咎具体Wi-Fi设备/运营商或排除软件传输效率问题。ICMP不代表Tailscale丢包率，8次低丢包窗口不等于长稳；热点仍有约2秒迟滞，B06/B09和beta稳定性不因此通过。准备测试页时连续剪贴板粘贴/回车未完成导航，15秒观察超时；检查空地址栏后单独粘贴、核对并回车成功。该准备失败单列，不伪称所有输入/剪贴板路径通过，不混入8个已就绪测试页点击样本。

- [x] 原样热点对照、显式连接恢复、18次远端查询及一次真正创建测试；原始失败与恢复分别保留。
- [x] 临时会话准确删除，自有测试tab/viewer关闭且查看凭据撤销、任务截图清理；最终仅保留原jp-auth-check/terminal-check，work原实例running、attachments=0，连接ready/tailscale-direct。
- [ ] 剩余画面迟滞、剪贴板交互与切网恢复提示优化/复验；长期稳定性及beta发行仍待办，Claude授权继续暂停。

七类复核：本批无新增生产源码/依赖/SDK-host-plugin边界变化；仅现有认证的connect和会话/浏览器RPC，无权限能力或协议扩张。连接与会话request_id沿现有原子/去重契约，测试创建/删除各一次，不重试未知修改；保留正式bin、身份、项目文件、密钥环、agent-remote、独立Chrome所有权及进程。未改变超时、DNS/代理/路由或默认显示设置；有限样本与未测门槛明确分开，08/15/16同步。文档差异/三处证据一致性检查通过，不把上一批本地代码修补或本次网络改善宣称为新发布版本。

## alpha.7会话超时与浏览器卡顿实测（2026-11-19，稳定性未通过）

用户连续两次创建`jp-auth-check --no-claude`返回deadline_exceeded，并报告浏览器几乎不可用。先查询确认该会话不存在，再用固定request_id只发起一次创建：正式alpha.7默认预算下1.033秒成功，未启动Claude；读取该新pane进程允许列出的环境，确认为TZ=Asia/Tokyo、LANG=ja_JP.UTF-8、LANGUAGE=ja_JP:ja；保留该会话供后续使用。这不证明原故障已经修复，也不证明改用资源ID是修复。创建RPC默认15秒，前置项目查询及创建内部region检查各有5秒预算，原始两次错误没有阶段证据，不能断言由哪一段产生。

现场证据：本机agent状态12–56毫秒；关闭查看器时远程列表/浏览器状态约0.6秒。远程region inspect三次0.281–1.710秒；VPS root/helper inspect23–24毫秒，普通shiki经受限sudo33–35毫秒。采样时VPS CPU约95–99% idle，无明显内存/IO压力；这些是有限窗口，不排除其他时段负载。随后真实查看器一次状态查询超时，画面仍连接；并行远端列表/状态最高约4.16秒。路径采样为tailscale-direct，不能据此宣称持续直连或归因relay。

在work原实例中仅新建自有data测试页，不访问账号或新的外部供应商。默认1440×900、quality6/compression2，首组5次点击至canvas变色为3620/4566/2372/4980/6843毫秒；后续对照最慢7977毫秒。直接观察私有X11及共享只读VNC连接上的同一测试像素，VPS画面变色已发生而本机仍可延迟数秒，说明至少存在画面返回链路延迟。3次本地WebSocket/画布采样没有长任务，末批画面消息抵达后很快可见；诊断记录仅保留测试页时序/字节计数，临时截图已清理，不将实际账号页面或原始输入纳入文档/测试fixture。降低quality1/compression9仍有5440毫秒，缩小960×600仍有4766毫秒；两组样本不足以宣称优化有效，均恢复原设置。

到同一VPS的8次ICMP有3次未收到回复（37.5%，有限样本且ICMP可能受限）；独立SSH TCP短测8次均成功、0.232–0.691秒，其内核累计重传段数30→34、重传字节20016→20932。支持链路存在损耗，不能据此把ICMP比例当作Tailscale丢包率，不能定位运营商/主机方向。内核ss不能代表gVisor内层TCP；目前产品未导出其重传计数，端到端根因仍待进一步取证。当前依赖隔离gonet/HTTP2实验未复现秒级Flush/Nagle等待：默认Nagle关闭；6字节和64KiB各100次echo最大分别108.5/936.2微秒；同连接未读16MiB响应下10次短请求208–755微秒。该实验不包含公网/WireGuard，不作为现场网络通过。

已修源码：会话环境检查经插件RPC返回Canceled/DeadlineExceeded时，Manager.Create原先只识别本地context sentinel，会误存为FailedPrecondition；现保留跨RPC码及request_id重放结果。回归先RED确认误分类，再GREEN验证失败清理、名称/单会话配额释放、无tmux残留及不重复调用环境检查。`go test -race ./internal/server/session ./test/architecture`通过（23.683/1.659秒）；不扩大超时、不跳过实际region检查。此修复尚未发布/部署，且不解释用户原始deadline来源或修复公网丢包。

CLI同批修补：`sessions create`前置server/project选择失败明确标注CreateSession尚未发送；创建调用的Canceled/DeadlineExceeded/Unavailable提示结果可能未知、先查询同server/project的sessions list，并显示此次实际request_id。结果未知时仅以原参数/原ID核对原结果；列表缺失不能证明失败，同ID不会重跑已记录失败，确认原请求失败后才能显式使用新ID创建。明确业务拒绝不冒充未知结果；不自动重试、不自动补偿查询、不打印描述/tags/凭据。错误wrap保留原code/detail，控制字符转义，成功JSON和失败stdout契约不变。真实UDS/HTTP2 fixture覆盖显式/自动ID、两阶段失败及相同payload重放；相关CLI race回归通过（5.246秒、无skip），`go vet ./cmd/shiki`及差异检查通过，独立复核指出持久失败会被同ID重放，已修正不能永久禁止新ID的恢复措辞，精确CLI race回归再通过（2.026秒）；正式安装尚不包含此提示。

- [x] 保留失败证据、确认不存在后仅一次创建成功；复现真实画面延迟及有限网络损耗证据。
- [x] 会话跨RPC错误分类修复及失败/去重/清理race回归。
- [x] CLI创建失败阶段与原request_id恢复提示，真实IPC/HTTP2回归。
- [ ] 超时与卡顿的端到端根因闭环、最终候选实机稳定性复验；B02/B06/B09不得整体计通过，beta仍不发布。

七类复核：改动限宿主session错误分类与CLI创建错误提示，无host/plugin业务依赖跨界；RPC能力/权限不变，保留既有安全公开错误及原始取消/截止语义；持久schema不变、去重失败码一致；进程所有权和失败清理保持，不自动重试修改或重启Chrome/tmux；文件/终端/网络限额和默认期限不变；正式两端bin/安装配置不变。测试与外部失败分列，08/15/16同步；未用当前修补覆盖历史发行证据。自有测试页/viewer已关闭、查看凭据撤销，画质/分辨率恢复，任务截图/临时netstack脚本已清理；work原实例、项目、正式版本、agent-remote和账号状态保留，Claude授权继续暂停。

## VPS日本地区检查与显式应用（2026-11-19）

用户要求账号授权只在VPS Chrome内完成，先核对日本环境与出口；明确批准IPinfo和ipapi.co查询，随后明确选择先应用日本时区/语言、账号登录继续暂停。通过正式alpha.7 region/browser诊断分别在主机与work Chrome执行：两方在两家服务均观察到同一出口IPv4；IPinfo声明JP/Tokyo，ipapi.co声明US/Los Angeles。结果是地理库分歧，不是两个实际出口或流量经过美国的证据，也不能推断Claude使用的数据、账号资格或登录保证。实际IPv6出口、权威DNS递归来源和Claude登录页面可用性未验证。

应用前系统为Asia/Shanghai、en_US.UTF-8；work Chrome原生Intl为Asia/Tokyo，navigator为ja-JP/ja，目标平台Linux。按用户审阅的精确region plan/revision执行后，operation为applied；再inspect实际系统为Asia/Tokyo、ja_JP.UTF-8、LANGUAGE ja_JP:ja，状态ready且无待确认DNS操作。既有DNS服务器及chrome_mode=unchanged保持；未修改路由、代理、NTP、账号或重启应用。work原Chrome实例继续运行，复查原生东京时区和日语一致，browser restart_required=false。region操作的restart_required=true提示既有进程环境不会自动更新，不能据系统值宣称所有旧shell/Claude已采用新默认；后续授权前应使用新会话核对。

- [x] 用户授权的两供应商、主机与实际远程Chrome出口对照及地区配置检查。
- [x] 明确批准后应用日本系统时区/语言，实际值和Chrome再次核对；任务临时JSON及目录已清理。
- [ ] IP地理库分歧与服务实际地区识别仍未确定；Claude授权按用户要求暂停。

七类复核：使用现有公开region/browser契约，SDK-host-plugin目录边界、RPC/权限能力和数据schema无代码变更；计划/revision检查及既有事务记录保持。系统配置变更与已启动进程环境分开，普通UID/独立Chrome所有权、文件/终端/网络限额和安装布局不变。外部查询仅显式批准的两供应商，未发账号/Token/Cookie或综合指纹；本机查看器不冒充VPS Chrome证据。08/15/16同步，查询分歧、未测项及账号暂停如实保留。

## 用户终端方向键反馈（2026-11-19，诊断中）

用户提供alpha.7基线输出：版本一致、项目查询成功、本地3个及远端4个插件ready，work浏览器running；这是用户回传的B01证据，不是新独立实测。B02普通用户与项目路径、测试文件读写正常，但空shell中方向键显示`^[[A/B/C/D`，回车后/bin/sh报not found。B02交互编辑尚未通过。

源码核对发现`internal/server/session/environment.go`的非空地区环境包装在argv为空时显式使用`/bin/sh`，与现场shell一致；Debian的sh可能没有交互行编辑。该证据不能单独证明所有按键转发正确。建议当前测试shell临时exec干净交互bash做对照，保留会话和文件；是否恢复待用户实测，未修改生产源码、用户登录shell或服务。

- [x] 记录用户B01基线和B02失败，定位候选shell选择路径。
- [x] 用户确认切换干净交互Bash后方向键正常。
- [ ] 默认交互shell行为修复及相应回归。

七类复核：本批仅记录诊断和有界临时操作建议，目录/SDK-host-plugin边界、RPC/权限能力、持久schema/revision/去重、进程管理与恢复、文件/终端/网络协议、CLI/安装布局均无生产变更；临时shell替换不作为修复已部署证据。08/15/16一致，B02不因文件读写通过而整体勾选。

## beta 用户实机验收计划（2026-11-18，计划完成、执行待办）

用户要求详细可执行测试流程、助手/用户分工，并以完成测试后进入beta为目标。[30 实机测试手册](30-beta-user-acceptance.md)按正式alpha.7帮助及源码核对项目/会话、sync冲突、port转发、browser控制/文件/持久化、region只读计划、网络恢复、采样、首装及可选重启。真实账号授权由用户完成；主机重启、整机DNS/代理变更不因编写计划获得执行授权。本批未运行新实机用例、未新建测试项目、未改变两端服务或创建beta标签。

已交付的alpha.7文档固定源9f55e5a的CI44120621767最终57/57成功；首轮3项ARM Mac因托管runner未领取而取消，同源原条件补跑通过。该证据不覆盖本次尚未执行的用户实测，历史beta发布前门槛与新增实机建议分别记录。

- [x] 对照设计基线、当前支持声明和正式CLI帮助完成可执行手册及分工。
- [ ] B01–B09新一轮用户实测、偶发超时定位及发现的问题修复/复验。
- [ ] B10真实新向导首装补验；B11物理重启另约窗口，不执行则保留未实测。
- [ ] B12最终候选回归、beta签名发布与公开包验收。

七类复核：本批仅文档新增，无生产目录/SDK-host-plugin依赖、RPC准入/授权/能力、持久状态/原子性/revision/去重、进程所有权/取消恢复、文件/终端/网络限额、CLI或安装布局变更。采样不记录项目正文/原始秘密，不重试修改；新映射暂停、逐项冲突决策、显式浏览器控制与持久boot选择保持。旧通过、此次计划和实际外部验证分别标注；08/15/16进度一致。命令帮助与文档校验不冒充真实测试通过。

本批校验：正式alpha.7命令帮助与对应源码核对；手册25个shell代码块语法及嵌入Python语法通过，相对链接和新增内容秘密模式检查通过。独立只读复核未见阻塞项，已将当前窗口PATH设置移到首次短命令之前，并区分强制只读查看器与允许申请控制的查看器。未执行这些实机测试命令。

## alpha.7 发布、两端升级与实测（2026-11-17，公开发行、两端升级、交互实测与清理完成）

用户已明确批准发布v0.0.1-alpha.7并升级VPS/Mac，允许新建ego-browser工作区继续画面、鼠标控制及显式重连实测，测试后清理本批Shikigami开发/临时产物。agent-remote、正式版本与安装备份、节点身份、凭据、配置、项目、profile和密钥环保留。账号登录、主机重启及网络/代理/路由变更不属于本次操作。

两项生产补修的固定源c5c8f96a4c6e96b95c7993a0d81fca7bb86b0b1f已完成[CI44043767221](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44043767221)57/57成功；WSL2完整阶段通过，无未执行阶段，新增身份预算回归2.54s、浏览器本地清理回归0.21s通过。此前浏览器补修6850e77的独立CI44037921171亦57/57通过。发布说明冻结时生产代码保持该已验源码；标签的完整CI、签名公开资产及两端部署结果在下文分别记录。

七类复核：目录及SDK/host/plugin依赖、RPC/鉴权/能力、持久状态与去重、进程所有权与取消、文件/终端/网络语义、CLI与签名安装布局均沿用已复核实现，无新接口或配置策略；冻结说明区分本地SDK清理与远端释放确认、原调用预算与网络故障。CI通过不替代真实画面/鼠标、网络长稳、账号或物理重启证据。

标签固定源为`cbf3cdfcec1c598e7d9ee4279c4054cdc2eb96bb`，分支CI44091466319和发行44091646249分别执行。发行首轮WSL2 job134068085354在13:20:03–14:05:03 UTC失去hosted runner通信，GitHub注释明确记录该原因，日志资产返回404/BlobNotFound；无法确认具体测试或底层失联原因。同源分支WSL2 job134067362834已成功。发行macOS Intel internal job134070372881达到既有20分钟上限被取消；已取得日志含1081个测试pass事件、无fail事件，session包尚未结束，因此整项不计通过。后续同源补跑保持原断言和超时，结果另记；不移动标签或覆盖签名资产。发行macOS ARM unit-pkg job134070372451在13:23:58–14:23:30 UTC排队后取消，steps为空；GitHub明确注释hosted runner多次未能领取任务及ARM容量限制。该项未执行，不计通过，保留到同源补跑。分支CI的macOS ARM unit-cmd job134071095835同样因runner未领取而于14:28:30 UTC取消，steps为空；亦须原条件补跑。

发行等待期间的alpha.6交互基线（不是alpha.7验收）：在用户已授权的新工作区中，实际work画面从只读显式接管，新建仅用于测试的远端标签，通过键盘载入本地data测试页；鼠标按钮计数0→1、输入alpha6-test、滚动至END均由画面确认。一次显式重连恢复Connected/Read-only，再次明确接管后关闭测试标签，原about:blank保留；随后关闭本地viewer，未停止Chrome。状态查询多次短暂unavailable后恢复，不能计公网稳定通过。测试脚本一次误等Read-only session，而实际重连文案为Read-only，属于观察条件错误，不能记成产品重连超时。

固定标签源cbf3cdf的[分支CI44091466319](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44091466319)在attempt2达到57/57成功：首轮56成功、1个ARM容量取消；只补跑该未执行作业后通过，未改源码、断言或超时。当时发行自身CI、签名公开资产、部署及实测分别待验；最终结果见下文，不由分支CI替代。

alpha.7已于2026-11-17T15:25:25Z公开（draft=false、prerelease=true），固定标签源cbf3cdf；发行44091646249 attempt2最终60项成功、1项条件跳过（不计通过）。此前三项异常在原源码/原断言/原超时下补跑通过，WSL2为14:48:53–15:16:23 UTC，Intel internal为14:48:54–15:04:04 UTC。23公开资产、四目标Ed25519签名、归档安全成员、checksum pin、payload哈希/大小/可执行模式、PROVENANCE源码与run、bootstrap来源和远端资产元数据均独立核对；Mac原生二进制报告alpha.7/cbf3cdf。Homebrew由bot提交9525301693a8e260b9ae5cd85c5bd477ddb2116c更新，使用已验资产重新生成Formula后逐字节一致。

两端正式升级已验证：Debian12/amd64的18个payload、Mac arm64的14个payload及VPS两项root支持程序均匹配独立已验签bundle；release.json逐字节、SHA256SUMS签名pin、receipt active/current一致且无pending事务，原Plan保持。daemon与LaunchAgent实际可执行路径指向正式alpha.7；daemon active/running、NRestarts=0、显式region的有效NoNewPrivileges=no，keyring/region guard服务active。两节点原地址及daemon身份保持；Mac配置/plist/凭据、VPS配置/Token/加密凭据/keyring/region策略文件逐字节保留，插件持久bucket及profile定义的canonical hash一致。7个local/remote插件均alpha.7 ready。安装器切换后的首次探测返回node_uninitialized/exit3；稍后Mac为local_application_ready、两节点online，VPS仍application_pending/CLAUDE_LOGIN_REQUIRED，未重登或换Token。支持升级按批准计划执行APT与官方Chrome检查并复用密钥环；Chrome显式停止且无pending jobs后更新runtime，随后work一次启动成功，Chrome157.0.8103.78、原region revision保持；浏览器交互结果另记。

alpha.7实机交互已完成：15:42:14–15:50:21 UTC约487秒观察窗口，真实work屏幕中鼠标计数0→1、键盘输入alpha7-test、滚轮至END；显式Reconnect screen约1806ms恢复Connected/read-only，原计数及文本保留，没有自动接管。再次明确Take control后计数1→2、追加-reconnected成功。该窗口未观察到断屏或状态unavailable；只证明本次短时操作，不外推公网长期稳定。仅本轮测试标签被关闭，原about:blank保留；本地viewer关闭后原Chrome实例仍running且attachments=0。测试工作区已finish关闭。早先工作区在长时间CI等待后消失、列表为空，按用户已授权新建工作区继续，未将工具工作区故障记作产品故障。

随后独立CLI采样（查看器已关闭，15:50:59–15:52:58 UTC）：12次默认5秒预算projects list，11次成功（0.175–1.319秒），第8次在5.020秒返回deadline_exceeded/operation deadline exceeded，之后4次恢复成功。保留原超时且未重放修改，不能据此宣称链路完全稳定；该错误本身不能区分底层网络或服务响应的原因。与升级前12/12成功的短窗口并列，不能据样本推断版本导致网络变差或已消除历史故障。交互结束后再次核对两端签名payload、receipt和全部基线保留项通过。

- [x] alpha.7生产源码完整CI与发布授权。
- [x] alpha.7签名公开发行及四目标独立验证。
- [x] VPS/Mac签名升级、数据保留验证与浏览器实测。
- [x] 本轮测试产物清理与最终交付记录。

本轮清理已完成：核对准确任务目录、所有者、无符号链接及运行引用后，删除VPS升级staging的32个普通文件（逻辑大小427,395,945字节）和Mac私有验签/升级/观察目录的82个普通文件（727,517,002字节）；此前10张alpha.6基线截图已删除。逻辑大小不是实测磁盘释放量。测试标签、viewer和工具工作区已关闭，临时SSH维护连接已关闭；正式Chrome继续运行。清理后VPS仍运行正式alpha.7，daemon active/running、NRestarts=0。agent-remote、正式release/backups、配置、身份、凭据、项目、profile和密钥环保留。

收尾七类复核：本次仅同步发行、部署、清理证据和当前安装指南，生产目录及SDK/host/plugin依赖、RPC准入/授权/能力、持久状态/原子性/revision/去重、进程归属/取消/恢复、文件/终端/网络语义与限额、CLI契约及安装布局均无新增改变。08/15/16状态一致；短时实测、1次CLI超时及未验账号/物理重启分别记录，不将历史稳定性组合项追认通过。

<a id="evidence-public-alpha7-release-20261117"></a>

### alpha.7 公开发行证据

不可变源`cbf3cdfcec1c598e7d9ee4279c4054cdc2eb96bb`；[Release](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-alpha.7)，[成功发行工作流](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44091646249)，独立信任公钥SPKI SHA-256为`e21f6dd671ea5042c43c492b759b933dda73985f89a823b43c651ff7c787c17d`。四目标仅Mac arm64在本机执行version；其余跨目标为签名、平台、来源和文件核验，不冒充原生运行。

| 目标 | 归档 SHA-256 | 签名 checksum pin |
|---|---|---|
| linux-amd64 | `19993dfbb5fbb23e531db7dcbc6ed23cedd96deee5e7c3e759062767cd7d2d3b` | `69fba11a3f3f0200d410f2f8e916e071fef602fb3653421e2e3a4349fb883b84` |
| linux-arm64 | `96af4e73e1284d77336e7813d7a9926ca17738b1082778cbabe522dc37a0a657` | `9146369b704ee917d68db7a7929ced167b4627db84da1a3f30a27f94bbbaf139` |
| darwin-amd64 | `6b4db34e72bb3b13119f1f9cdd134509148ce7400a9fa1cd0aea63ebf61ce112` | `bd508d2b1304c8531d18b764b5d590e8dc5025da46dfa4fec358f296d6c50d7c` |
| darwin-arm64 | `775311e76aad102151bdfff303132809f98a5f6924351b213ff6943037b6efb8` | `6099f1437f67f5907fc8a992885cdba6a29516e935b1f941671b0d2dd0f90c9b` |

## alpha.6 安装问题统一修复（2026-11-17，公开发行与两端升级完成，实机复验进行中）

用户授权发布alpha.6并升级VPS/Mac后清理Shikigami开发产物，明确保留agent-remote。完整问题表与清理边界见[29 安装问题跟进](29-alpha6-installation-followup.md)。本批覆盖：地址/标签与端口输入校验、原地纠正和保留pin的地址修正；y/yes/n/no与秘密输入说明/PTY取消恢复；控制面离线不重新发起登录且不误换Token；准确的启动失败阶段；受保护shell文件逐个处理与批准后仅修正准确home写权限；显式region支持的受管daemon策略及有条件daemon-only重启；插件修改只读状态恢复；短探测不误杀共享流、仅同一成功在途检查合并；浏览器显式重连与旧代回调隔离、独立启动状态/画面。

本地证据：引导/地址/PTY专项race三轮7.188s，最终3.731s，节点授权/Token区分三项十轮1.617s；连接池agent整包race63.943s、architecture1.626s；浏览器实际JS行为10/10通过，原HEAD首批6/6失败，真实loopback重连20次×20轮race通过、client整包race1.902s；安装器Mac两包race3.742s/10.697s，Linux arm64测试在隔离Debian12容器实际执行通过。相关vet/actionlint/diff检查通过，最终组合复验另记，不外推公网稳定性。

七类复核：目录及SDK/host/plugin依赖保持；RPC、权限和能力未扩展，region仍仅已批准六动作sudo，core NoNewPrivileges=true；证明仅内存，持久identity/revision/去重不变；原截止/取消和进程归属保持，屏幕重连不自动接管；文件/终端/网络限额保持，home不递归修改，秘密不输出；CLI与安装计划明示可操作错误及变更，签名旧版本不覆盖；原红新绿、本地/容器结果与公开CI、部署、外部网络门槛分开。

补充全量回归发现：首次 `make test` 在Unix socket发布与一次性auth key登录两项失败，其余已执行包通过。Mac默认大小写不敏感文件系统上，临时socket名可能与公开名仅大小写不同，现绑定前排除别名并新增确定性回归；connection整包race三轮4.971s通过。tsnet启动会自行注册，原实现晚传auth key可把空key注册拒绝归入新登录（旧实现十轮六次失败）；现于SDK启动前仅内存传入key，复用已有新注册，仅缓存旧身份仍Running时明确请求注册。node整包race73.473s、key注册/持久秘密检查及四种启动状态十轮10.562s通过。未延长deadline或跳过身份验证。查看器原生disconnect亦等待旧attachment有界关闭，最终JS行为10/10通过。最终完整复验与公开CI另记。

完整本地 `make test` 复验通过：106项Python工具测试、10项查看器行为测试、全部Go race包和两组发行工具测试；其中hostrotation218.350s、插件跨进程集成65.296s。随后独立复核补齐auth key恢复中SDK处于NoState/Starting的分支，在原5秒调用预算内等待状态明确，避免接收旧身份为新登录或盲目重复注册；实际SDK身份轮换及状态/截止专项race最终三轮9.204s通过；旧逻辑overlay下三种延迟启动回归失败，真实SDK虽Running但应用15秒后仍authenticating，临时overlay已清理。正式控制面的单次key消费语义仍按SDK契约，未为本批在用户节点上重登或重置身份；最终node整包race复验74.538s通过。`make lint`、actionlint、diff检查通过。

CI首轮固定源 `ccce4fa72fa3fbfd6f0cfa23bdf574219921eeda` 的 [43896776154](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43896776154) 结束为失败：49项成功、WSL2一项失败、3项macOS ARM作业取消且无执行步骤、2项后置矩阵跳过；取消/跳过不计通过。WSL2产品、E2E、embedded、terminal及发行工具全部通过，systemd-lifecycle在crash-guard阶段扫描 `/proc/*/stat` 失败（test-systemd.py原353行；error_kind=other），原异常类型无法由公开证据确认。

2026-11-17补修仅涉及验收检查器：先筛选准确插件可执行名，再按字节解析stat的最终右括号；仅将ENOENT/ESRCH作为进程退出，权限错误及损坏worker数据仍失败。固定异常分类新增ProcessLookupError/IndexError/UnicodeDecodeError，不导出异常正文或私密路径。旧代码的上述三种脆弱情况均确定性复现，但不冒称已确认原WSL具体异常。工具测试112/112（24.391s）、平台工具30/30（1.234s）、最终专项6/6通过，diff检查通过。七类复核确认生产目录依赖、RPC/授权、状态/事务、进程策略、网络/文件边界、CLI/安装布局不变；仅提高进程快照检查的正确性和失败分类，崩溃恢复断言及时间限制保持。新固定源完整CI结果见下段。

固定源 `377551af19b2f4a51e1057b782148c14a915797a` 的 [CI43906750506](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43906750506) 最终57/57成功。WSL2复验完整通过：incomplete_stages为空，systemd 13项操作、agent/daemon升级与回滚、清理均通过。期间macOS ARM预检查/终端作业因hosted runner多次未能接收而取消；Intel CLI作业一次达到15分钟上限且日志API返回404，不能确定具体耗时原因。同源补跑后该CLI作业8分50秒成功；没有改变断言或超时，没有移动标签，取消/跳过均待补跑成功后才计通过。不可变alpha.6标签指向该提交；[发行44017118492](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44017118492) 已成功（60项成功、1项条件跳过；跳过的复用Homebrew分支不计通过）。2026-11-17T05:29:09Z公开23资产，draft=false、prerelease=true；四目标独立验签、归档成员/payload/平台/固定源码核对通过。Homebrew由bot提交25740ba5cb9baeb258bf988f9e32d429bf22d37a自动更新，本机用已验公开资产重新生成Formula并逐字节比对通过。



2026-11-17实机部署：VPS Debian12/amd64与Mac arm64已通过签名alpha.6安装器升级；全部安装payload、VPS两个root支持程序哈希匹配公开manifest，运行daemon及LaunchAgent均指向正式release。首次安装器探测返回node_uninitialized，随后两节点online，Mac local_application_ready，VPS application_pending/CLAUDE_LOGIN_REQUIRED。原daemon ID、tailnet地址、Mac配置/凭据、VPS配置/设备Token/加密凭据/密钥环字节保持；插件持久bucket与升级备份一致，事务记录变化不冒称整个状态文件未改。local sync/port/browser及remote sync/port/region/browser全部alpha.6 ready。旧90-region-helper.conf仅在内容精确匹配且新50受管策略生效后移除。Chrome显式停止后更新runtime；首次恢复请求service unavailable，确认stopped且无systemd待执行任务后再次启动成功，work profile保留。当时浏览器界面复验因原工具工作区不存在而未完成；现已获alpha.7新工作区授权，结果见本页alpha.7记录。项目请求短时采样另记，不宣称公网长期稳定。

开发产物清理：确认正式服务接管、路径所有者/inode及运行引用后，删除38个Mac开发/诊断/测试路径（逻辑大小5,273,780,157字节，不等同实际磁盘释放量）；Docker残留共享句柄仅在所有容器均无对应挂载后排除，未停止/删除其他容器。VPS已删除哈希匹配的alpha.4旧脚本、精确旧策略及本批升级staging；Mac另清理26个已完成的alpha.6测试/CI helper与日志，本批私有验签/观察临时目录也已在证据入文档后删除。agent-remote、正式release/backups、身份、配置、项目、profile和密钥环保留。七类复核确认此次仅按既有安装/服务归属和授权完成部署，无接口、权限白名单、持久身份或网络配置新增变更；指南补齐实际CLI诊断和显式重连步骤，跳过/账号/物理重启仍不计通过。


### 发布后交付记录 CI 的进程观测补修（2026-11-17）

交付文档提交f0a06bde34124d8881f3d52cea2a884079028af1的[CI44026669305](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44026669305)结束为52项成功、1项失败、2项后置作业跳过，跳过不计通过。失败job133850471668为macOS Intel重型集成：TestRealHostRotationRecoversInflightFiles/server/download在inflight_test.go:95调用resources.Children，直接子进程的Exe查询返回`unknown error: proc_pidpath returned 0`。此错误本身不能证明当次进程退出或权限问题，不能仅凭字符串忽略。

补修仅涉及测试夹具test/fixtures/resources：正常候选仍只查询可执行路径；明确缺失或上述Darwin模糊结果触发现有processidentity.Inspector独立确认，只有ErrDisappeared才能跳过。活进程、权限/未知错误仍失败，expected数量、直接子进程范围、原截止、回收断言不变，不增加重试/超时。真实私有子进程在枚举后、Exe查询时回收的四分支回归，在旧错误处理overlay下全红；修后resources/processidentity专项race三轮通过（2.565s/4.974s），vet与diff检查通过，overlay与子进程均已清理。固定测试源码的受影响整包race复验全部通过：hostrotation219.045s、rotation35.447s、syncack42.019s、port14.586s、architecture1.405s。首次调用误设GOTOOLCHAIN=local而命中本机Go1.26.3，未运行测试；改用项目固定Go1.26.11后获得上述结果，不将前次工具链拒绝计通过。随后be7fada固定源CI的进程夹具检查通过，整轮的独立浏览器失败见下段。

七类复核：改动仅为test夹具复用已有test诊断观察器，生产目录/SDK与插件边界不变；RPC、权限、能力、持久状态/revision/事务、生产进程与网络/终端行为、CLI和安装布局均无改动。无法确认消失仍失败，不以重跑或放宽断言掩盖未知结果。已发布alpha.6标签、签名资产和两端生产二进制保持不变；这批不是新的产品版本。


### alpha.6 发布后发现的本地 attachment 清理竞态（2026-11-17）

固定源be7fada的[CI44031253619](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44031253619)已结束（56成功、1失败）；WSL2 job133863472571的唯一产品失败为plugins/browser/client/TestViewerExplicitReconnectReleasesPreviousAttachment（stream_test.go:220，新的WebSocket Dial失败，耗时0.01s）；公共证据不含原始错误正文，不能补造HTTP状态。该轮systemd维护13项操作、升级/回滚与清理通过；产品阶段失败导致E2E/embedded/terminal/release阶段未执行，不能计通过。macOS进程观察夹具的补修在本轮原平台及WSL已通过。

源码检查与独立确定性复现确认：WebSocket关闭ACK早于旧SDK stream清理及双向pump退出。原实现只看context取消，既可能在取消前拒绝新Dial，也可能在已取消但清理未完成时提前创建第二条远端stream。旧代码在阻塞SDK Close的回归中opened=2；不是把这条复现冒充原WSL未公开的错误正文。

修补限定browser/client：attachment.done在本地SDK Close返回、socket清理及双向pump退出后关闭；这不是远端释放确认；最多一个新请求等待旧done，额外竞争者409，等待不取消既有连接。等待和远端READY共享原operationTimeout截止，取消、scope、Origin/CSRF、单attachment、只读和显式控制权保持，不重放输入。真实loopback测试在旧Close阻塞期间要求新Dial在原预算内504且opened仍1，释放后只创建一次只读新连接；单等待者、额外竞争者及取消隔离有确定性覆盖。专项race十轮、client整包race（2.060s）、前端10/10、vet和diff检查通过；独立只读复核未发现阻塞问题，架构整包race通过（1.659s）。固定源6850e77899f6a8f3424bb9ad393b01ce478c8ab4的[CI44037921171](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44037921171)最终57/57成功；WSL2公开证据无未执行阶段，产品/E2E/embedded/terminal/release及systemd生命周期全部通过，Linux/macOS维护验收通过。该固定源尚不含下述身份复核预算修补。

七类复核：生产业务仍归plugins/browser/client，未引入host依赖或新远端RPC/权限；新增仅内存完成信号与单等待标志，不改持久配置/修订/去重。本地清理完成前不释放连接所有权，所有等待受原预算和生命周期约束；网络/输入限额及安装布局不变，CLI/公开能力不新增。此修补尚未进入已发布且不可覆盖的alpha.6资产；该次记录时两端仍为已验签alpha.6；用户随后已授权alpha.7发布及升级，部署与实测结果见本页alpha.7记录，不能由源码回归直接声称该竞态已在用户环境消除。

### 已有连接身份复核预算跟进（2026-11-17）

下列采样时两端运行公开alpha.6。07:21:40–07:31:06 UTC的11次默认5秒项目查询，8次成功（0.180–2.315s），3次失败（2.552/5.057/2.553s）；这三次未保留原始错误正文，不能补造分类。随后独立普通查询捕获2.517s的`shiki: unavailable: service unavailable`。另12次结构化查询均成功（0.181–2.548s）；后半段诊断与原采样交叠，可能共享在途身份检查，不能据此认定输出格式影响可靠性。doctor另次快照为online/direct、7插件ready；这些成功不否定失败或证明公网长期稳定。

只读审查确认一条与2.5s吻合但尚未在现场独立证明的代码路径：已有池的verifyRemote固定将调用剩余预算分成两份；默认5s调用的探测约2.5s到期，callerLimited分支直接返回而不使用为route替换预留的后半预算。探测错误经既有脱敏成为Unavailable/service unavailable；该文案也可能来自其他故障，不能反推唯一根因。

修补只对已有池的身份复核使用原调用剩余预算、最多5s；首次建立多路线的公平分摊保持，CLI/短RPC总预算不变，不增加业务重试或跳过身份验证。父预算≤5s到期仍仅失败该调用，不计池失败；剩余预算>5s且<10s时现在可以完成一次5s探测并进入已有持续失败观察，恢复仍受父截止约束；≥10s或无父截止的5s上限保持。确认的可用性策略缺陷与现场根因分开记录，不能宣称此修补消除公网故障。确定性延迟回归中，旧代码在4s父预算约2.08s返回Unavailable，父仍剩约2s；修后2.5s释放探测，完成真实HTTP/2认证和终端READY，业务计数为1且仍在原父预算内。短400ms调用要求父截止错误、原共享终端仍传输且池/重试/失败观察不变；6s调用验证完整5s失败可计入观察。专项race60.455s通过，最后仅增强测试释放发送的取消保护后专项4.151s通过。独立最终复核未发现阻塞问题；vet、架构race（1.402s）和diff检查通过，最终固定测试源码的agent/connection/architecture整包race分别72.285s/2.776s/1.198s通过；该生产源码c5c8f96的CI44043767221已57/57通过；alpha.7标签cbf3cdf的发行门槛另记。不可变alpha.6资产不包含它。

七类复核范围：仅host agent连接策略及真实HTTP/2测试，SDK/插件依赖边界不变；无新RPC、权限或能力，pin/凭据代际和每次独立认证保持；只内存探测预算变化，持久状态、revision、原子写和去重不变；父截止/取消、进程所有权和共享流隔离保持；终端输入、文件、端口及网络配置不变；CLI总超时、签名资产和安装布局不变；本地源码验证、固定源CI与当时尚未部署的生产修补分开登记。

- [x] 已确认问题汇总与源码回归覆盖（29记录）。
- [x] alpha.6固定源完整CI、签名公开发行与资产验证。
- [ ] 两端签名升级、稳定性复验与开发产物清理。


<a id="evidence-public-alpha6-release-20261117"></a>
### alpha.6 公开发行独立验证（2026-11-17）

来源固定为377551af19b2f4a51e1057b782148c14a915797a，源码CI43906750506与发行44017118492结果见上。23资产包含四归档及各自签名/SHA256/checksum pin/验证回执，外加bootstrap、公钥和PROVENANCE。使用仓库独立可信公钥（指纹e21f6dd671ea5042c43c492b759b933dda73985f89a823b43c651ff7c787c17d）验证签名；校验来源运行、版本/平台、平面普通文件成员、完整payload哈希。bootstrap与冻结tag逐字节一致；Mac arm64及VPS amd64原生--version均为alpha.6/377551a/go1.26.11。未在另外两个架构执行本批安装；四目标验签不冒充四平台实机部署。

| 目标 | 公开归档 SHA-256 |
|---|---|
| linux-amd64 | `f9d1862208819dcb617e0e76453455b58522bb9075df9e519bf5ca54a1e12e8a` |
| linux-arm64 | `e18af21e7f1e4a61fc718c35c98004a90f9f3c088add571823e2fa464f571077` |
| darwin-amd64 | `6b465b5a812ecaeeb8ba60f2c6d6d03d914799c967edeced70bec60fa297fb7a` |
| darwin-arm64 | `971691659fb534d5d0bcb9af31bf497308c3a580f2a7b7ad2838cd7880317ec5` |

## alpha.5 实机连接波动跟进（2026-11-16，本地源码验证及 Mac 热修复试用，公网故障仍复现）

用户已完成 VPS 密钥环登记/解锁、Tailscale 授权、Mac 配对及 sync/port/region/browser 向导，真实远程 Chrome `work` 画面已出现；Claude 账号登录和物理重启没有新增验收证据。Mac 主目录原0775导致插件目录祖先检查失败，现场仅移除主目录组写权限后本地插件ready；VPS daemon 的 `NoNewPrivileges=true` 与region固定sudo helper冲突，用户应用独立systemd覆盖后region/browser ready。这两项现场修复不等于alpha.5安装器已自动处理，后续安装诊断/权限策略仍需单列修复，禁止绕过插件目录或sudo白名单校验。

连接故障现场：正式alpha.5查看器状态曾200 → 约5002ms的504 → 200 → 503并断屏；用户确认未启用代理/VPN。临时诊断agent仅在私有临时目录构建，记录故障分类而不记录Token、URL或原始错误；2026-11-16T10:31:26.644Z及10:31:31.646Z连续本地身份探测超时，紧接10:31:31.646Z的共享流取消，bad=true、pool_canceled=true，随后新路线探测也超时。证明两次5秒探测会放大链路慢响应为共享流终止，不证明底层网络为何慢，也不能归因于中继或VPS资源不足。该轮临时agent已退出并恢复正式LaunchAgent；再次通过RPC确认原`work`实例仍running。随后本机切换热修复试用，见下段；正式发布包和签名未改动。

本批修复目标：保留每次新RPC的身份复核、原请求deadline和信任失败立即撤销；完整探测超时需至少两次、且从首次观察到完整超时开始的持续失败窗口达到30秒，才替换共享池。成功身份协商清除窗口；在后续失败检查中观察到已认证Receive进展会重置窗口，静默流不因两次短慢探测立即断开，真正持续无进展仍有界恢复。CLI仅在插件修改已接受之后，对临时Unavailable/DeadlineExceeded继续查询同一revision，保持原等待截止，不重放修改、输入或控制权接管。

验证状态：CLI真实UDS回归确认两次临时查询失败后成功且配置修改只提交一次，覆盖永久错误、revision变化和取消/截止；专项race三次通过。连接池真实HTTP/2专项race通过（49.734s）；首轮整包race的CLI（46.675s）和architecture（1.365s）通过，agent的runtime夹具因误用真实用户plugins_dir而失败，现将其隔离到夹具私有目录，保留首次失败并重跑agent整包；go vet和diff检查通过。agent整包race复验通过（62.594s），七类架构复核完成；修复尚未公开发布；已在用户 Mac 以独立路径运行本地 agent 热修复，但后续实测仍复现故障，不宣称公网稳定通过。

实机热修复试用：2026-11-16T10:48Z 构建 `local-hotfix-7ad65ab3765e`（基于4462f1f及本批未提交补丁；Version仍为alpha.5以匹配内置插件，Commit明确标为本地构建）。二进制、构建记录、原LaunchAgent备份与回退脚本保存在用户私有hotfix目录；只原子替换LaunchAgent程序路径并保留安装器managed标记，没有覆盖签名release/current或用户CLI链接。新agent及local sync/port/browser均ready，真实VPS Chrome保持原实例。CLI修复二进制也已构建，但用户默认CLI链接仍是正式alpha.5。

真实查看器失败保留：首轮观察中状态查询失败但画面暂留Connected，约62秒状态恢复、95秒控制权由远端流确认，随后点击空白/滚动未观察到画面响应；约146秒仍断开。Connected标签不证明帧仍在到达，不能将本轮计为鼠标或稳定性验收通过。局部修复没有消除全部故障；继续用不含Token/URL/请求头的时序诊断区分探测排队、传输与服务端响应。现场原始观察记录保存在私有 `live-observation-1.json`。

后续隔离诊断：同一补丁加临时HTTP trace的agent记录请求写出通常0ms、首响应约2–4秒或5秒探测超时，path查询0ms；一轮54次探测43次成功、11次未完成，该区间没有池主动失效。诊断v4额外区分浏览器方法和TSMP，专项race50.011s通过；但重启后节点成为 `offline/NODE_CONTROL_UNAVAILABLE`，未获得有效TSMP对照，不能计通过。Mac无代理环境变量，系统HTTP/HTTPS/SOCKS/PAC开关均0；直接HTTPS访问controlplane.tailscale.com与github.com均在TLS握手报SSL_ERROR_SYSCALL（约7.03s/6.30s），显式--noproxy的控制面复查也失败（5.13s）。这是当前Mac至外部服务的连接失败证据，不证明具体ISP/NAT/中继或VPS故障，也不把它等同于先前全部断线的根因。

临时诊断已停止，LaunchAgent恢复为独立路径的无诊断热修复；原签名发行物、配置、Token及远程Chrome未被重置。当时建议用户切换Mac网络做同条件对照；后续用户要求保留网络复测，见下段。不能宣称安装重跑或加长超时能解决问题。

同网复测（2026-11-16T12:28–12:34Z，用户暂不切换网络）：无重装/凭据重置时，原热修复agent节点已online；显式无代理HTTPS检查恢复，控制面302/1.238s、GitHub200/0.726s。初次新查看器仍断屏，状态200/503交替。随后临时v4诊断复测中，TSMP成功12次（120–852ms）、超时1次、取消2次（取消不计通过）；同一时期身份探测及GetBrowserStatus仍出现deadline，不能用节点可达排除TCP丢包/队头阻塞，也不能据此断言服务端处理阻塞。

第二个查看器在181秒DOM观察中保持Connected，期间23次状态/控制HTTP请求有5次非200；控制请求200且流确认Controller，鼠标滚动后约11.1秒观察到实际画面变化。该窗口未出现主动池撤销引发的屏幕断开，最终BrowserStream取消来自明确关闭查看器；不把这次局部改善覆盖初次失败或声称长期稳定。VPS单次采样CPU18.7%、已用内存1715.7MiB、最小磁盘空闲35340.2MiB；不能据此推断全部资源或I/O健康。隔离诊断停止并恢复无诊断热修复，证据为私有 `retest-trace.json` 与 `retest-browser-observation.json`。本轮仅补充实测与文档，七类复核确认生产源码/协议/权限/持久身份未新增变更，已有签名发行物不变；精确根因及完整稳定性仍待验证，换网不是继续排查的前置要求。

<a id="evidence-public-alpha5-release-20261116"></a>
### 并发探测与启动诊断补修（2026-11-16）

同一服务器代际的并发调用现在仅共享其实际等待的成功身份探测；复用要求相同池、相同完成门、完整持久记录及凭据revision一致，任何后续探测均清除旧证明，失败不形成缓存。后续独立调用仍探测，每个业务RPC仍鉴权，原deadline、信任失败立即停止及禁止重放保持。CLI启动读取失败增加本地服务器/映射、远端项目/会话阶段说明，用%w保留错误码、取消及结构化诊断。

真实HTTP/2并发/后续独立探测、旧成功被新失败替代、凭据更新和等待者取消专项race三轮通过（5.057s）；agent整包race63.943s、architecture1.626s通过；CLI真实PTY/UDS启动错误及现有首次运行/会话专项race2.110s通过；agent/CLI vet通过。七类复核：依赖和SDK/host/plugin归属未改变；RPC/权限/能力未扩展；复用证明仅内存态，持久身份/原子性/revision/去重保持；不新增后台任务或延长取消；文件/终端/网络限额保持；CLI仅增强错误上下文，签名发行布局不改；本地测试不替代外部稳定性或公开CI，原现场失败继续保留。

用户授权SSH诊断后实际登录VPS：daemon active/running，NRestarts=0，NoNewPrivileges=no；三次vmstat采样无blocked进程、无swap、wa/st均0，采样CPU idle96–99%。这些短采样不能排除间歇性I/O/网络问题。SSH首次握手超时、两次认证阶段连接关闭，随后只读命令成功；不将连接关闭误判为密码错误或已经确定的Shikigami根因。真实交互稳定性仍待复验。


本批后续实机证据：新本地构建 `local-hotfix-ec0873d4a3e0` 已在 `~/.local/share/shiki/hotfixes/coalescing-20261116T135216Z/` 保存源码补丁、构建摘要、上一版LaunchAgent/CLI链接及回退脚本；已切换Mac agent与默认CLI链接，未改签名release/current目录。CLI整包race47.204s通过。重启后节点短暂offline随后online；五次默认期限项目查询仍全部Unavailable（4.972/2.523/2.519/2.525/2.524s），之后真实PTY裸命令成功进入“Create a new remote project / Project:”提示并主动Ctrl+C退出，未创建项目/映射。浏览器open仍返回Unavailable，未获得可验的查看器，不计浏览器通过。

独立SSH对照：三个已有SSH连接上的true命令分别超过8s、超过8s和3.925s成功；后续只读状态命令8.004s完成。用SSH_CONNECTION确认所测TCP连接，服务器ss记录其累计重传34次、bytes_retrans9368、rtt847.39ms、rto14916ms（并非丢包率或逐包根因）。20秒有界strace仅统计flock/fsync/fdatasync：34次flock、无syscall错误、未观察到fsync；汇总CPU计时不是锁等待时长。daemon持续active、I/O压力avg10/60/300为0。证明额外SSH路径也出现慢响应与重传，不确定具体ISP/NAT/主机网络段，不将这些短采样推导为Shikigami已修复或所有延迟都来自同一根因。当时新热修复尚未提交/推送/发布，当前公网稳定性仍PENDING。


## alpha.4 实机安装反馈修复（2026-11-16，本地与公开发行验证完成）

固定源 `21fcb5dc1406c7e47d8d12584110433aeaa773d2` 的 [CI43814011028](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43814011028) 已57/57通过，无失败或跳过job。不可变 `v0.0.1-alpha.5` 的 [Release43819036138](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43819036138) 首次运行成功：60项成功，Homebrew复用分支按契约跳过1项（不计通过；源CI四目标Homebrew均通过）。2026-11-16T07:41:05Z公开，draft=false、prerelease=true；23资产、四目标签名/摘要/来源/成员、公开macOS arm64提示/版本/帮助/首次和重复暂存均验证通过。自动Formula提交 `62df599ad717541e4050f921347597d097567a07` 与从已验签资产重新生成的公式一致。

后续文档/Formula同步提交 `2a0e1883ec6097dac49bfda1d65584ef25cb741c` 的CI43826191775最终56项成功、1项失败；macOS Intel `unit-test-support`失败：`TestFrontendExplicitTimeoutIsScopedAndPreservesEarlierCallerDeadline` 于 frontend_timeout_test.go:124 报caller deadline被延长，测试耗时0.09s。运行时源码和公开alpha.5未改变。Connect通过相对毫秒预算传输超时、在服务端分发时重建绝对deadline；仅加入25ms分发延迟即复现旧断言失败（5s误测5.026919625s）。现只修正测试：HTTP入口检查实际预算并计入序列化耗时/毫秒截断，保留可控延迟，增加真实RPC取消；本地interceptor要求较早父deadline精确相等。原生相关race连续50次通过（multihost10.887s、client1.811s）；两整包race3.132s/20.846s及定向go vet通过；隔离无网络Debian12 arm64相关race连续25次通过（5.574s/1.018s），不修改生产超时或放宽契约。七类复核确认仅测试观测与文档变化，SDK/host/plugin归属、RPC/授权/能力、持久化/身份、生产进程/网络/安装布局均不变；测试资源仍有界回收。原失败保留；本段记录本地修复证据，新提交完整CI结果以关联Actions为准。

用户 Debian 12 / amd64 输出确认：下载与 Ed25519 验签成功；`curl | sudo bash` 下 installer PGID 与 TPGID 不同，第一次确认未继续；直接文件执行后系统支持计划可确认；耗时依赖阶段没有进度，随后出现口令输入；密钥环登记最终因普通 UID 无法执行 runtime 而 `203/EXEC`。未执行密码验证，不能把这次错误归因于口令或 GNOME keyring。

修复实现：仅 console 提示首读触发 sudo PTY 的 SIGTTIN 前台交接，普通会话终端保持 readiness-first；macOS `/dev/tty` 的 POLLNVAL 对仍有效的非阻塞 FD 使用有界可取消重试。非交互安装步骤显示固定阶段、完成/失败与耗时，10秒 heartbeat 返回前 join，不跨秘密输入、不复制子进程输出/参数/错误内容。公共应用目录和服务文件显式设置权限，批准后修复旧目录；目录 FD/O_NOFOLLOW 验证祖先，保留共享祖先权限与私密0700/0600。密钥环失败显示固定 systemd 状态及 journal 命令，保留原密钥环。

复核期间拒绝了两项中间实现：全局 read-before-poll 导致原生 `TestTerminalTakeoverWaitsForStreamConfirmation` 阻塞（root race 165.292s 以 SIGQUIT 取栈后结束，不计通过），改为仅 console 首读；共享 `/etc/opt` 不应自动 chmod，现保留其管理员权限，受限共享祖先明确阻止安装。旧源码分别复现 Linux sudo 管道确认超时和 macOS `/dev/tty` 的 file-already-closed。最终整包验证结果在下方记录，不能用专项通过掩盖这些中间失败。

已完成证据：真实 Linux sudo PTY 确认/秘密输入/取消；隔离 Debian12 arm64 root 与 UID65534 跨权限测试，实际重现旧0700执行失败后同 inode 修复，验证公共策略可读、凭据不可读、拒绝 symlink/外来 owner、共享祖先不扩权和服务文件0644；Linux install整包最终3.990s通过。原生 bootstrap 15项 HTTPS/Ed25519测试、发行工具33项通过。Linux CI 增加显式 sudo 终端与 root/普通UID 权限门禁，普通用户测试中的环境 skip 不算这些门禁通过。

补充真实服务验证（固定源码 `21fcb5dc1406c7e47d8d12584110433aeaa773d2`）：Debian12 arm64/systemd252.39，隔离无网络容器、精确源码生成unit和runtime、umask0077。普通UID1000首次provision及停止后正常unlock均Result=success/ExecMainStatus=0；加密GNOME文件格式/owner/私密权限和服务外凭据不可读均通过。初次容器 `/run` private propagation 令systemd子命名空间的凭据mount消失；只修正隔离容器挂载传播后，新建干净容器以原始unit通过，没有产品权限绕过。证据 `keyring-smoke/`；这是手动暂存fixture的实际服务测试，不等于签名安装事务、Chrome、真实账号或用户VPS完整安装。容器关机退出0并清理。

公开包独立验证：全部23资产与GitHub size/SHA-256一致；四个归档离线Ed25519验签、payload pin、verification receipt、version/OS/arch、构建来源与PROVENANCE均匹配。bootstrap与标签源码一致。所有目标二进制构建架构匹配；不导入buildinfo的region helper通过签名payload核验，其余版本/提交标记匹配。原生macOS arm64四个主程序版本/帮助、真实 `/dev/tty` quickstart输入 `n` 后取消，以及首次/重复stage-only安装通过，未激活宿主服务。Formula从公开签名资产重建逐字节一致、Ruby语法通过。下载时一次TLS连接错误经有界重试恢复，最终每个文件均按摘要核对，不把下载尝试错误隐去或计成资产通过。证据包含 `verification-summary.json`、`smoke-summary.json`、`public-prompt.log`、源/发行CI元数据与公开Release JSON。

最终原生整包 race 通过：client20.738s、install10.893s、shiki-install4.008s、architecture1.441s；make lint（全仓go vet与gofmt门禁）通过。Linux client整包含显式sudo PTY通过，最后增加初始PGID!=TPGID断言后该专项再次通过；权限最终整包见上。证据工作目录 `/tmp/shiki-alpha5-public-verification/`，冻结源码与最终CI/发行结果见本节开头。固定源CI与alpha.5公开发行验证均已完成；alpha.4标签/资产保持不变。真实用户VPS完整重跑、账号及物理重启仍 pending。

**alpha.4 已发布（2026-11-16）：** 固定源 `12664aac1d5b2ae6f1d5bb81e4eb59033009f9b0` 的 [CI43749533031](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43749533031) 57/57 全部通过；不可变标签 `v0.0.1-alpha.4` 的 [Release43754919496](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43754919496) 第二次尝试成功，最终 60 项成功、Homebrew 复用分支按契约跳过 1 项（同源发布前四平台 Homebrew 均成功，跳过不计通过）。23 个公开资产、四目标签名/摘要/成员/来源、自动 Formula 及 macOS arm64 版本/帮助/首次和重复离线暂存已独立核验。新向导真实 VPS/Mac 首装、账号授权与物理重启仍未实测。

首次 WSL2 失败证据：`TestCLIThreeProcessTLSLifecycle` 在 `core_test.go:217` 首次 PTY 初始化调用处失败（8.45 秒），187/444 是清理日志位置，不是额外故障；尚未进入终端就绪输出读取。公开证据不含原始 errno，原因未确定，不宣称已证实基础设施故障。产品测试、发行工具和 systemd 生命周期通过；embedded-plugins 与独立 terminal 阶段未执行，不能计通过。同一源码发布前 WSL2 成功；相关 helper、PTY 依赖与平台脚本自 alpha.3 未修改。保留首次失败并在新 runner 上重试同源完整 WSL2，不改变断言或移动 tag。

第二次尝试在新 runner 重跑相同标签源码，WSL2 全部必需阶段及清理通过；`TestCLIThreeProcessTLSLifecycle` 为 32.79 秒成功，`incomplete_stages=[]`。未修改代码、断言或超时，也未移动标签；首次 PTY 初始化失败未复现，原始系统错误仍未知。最终成功不抹去首次失败和未运行阶段。

<a id="evidence-public-alpha4-release-20261116"></a>
## alpha.4 公开发行与独立核验（2026-11-16）

Release 于 `2026-11-15T21:14:03Z` 公开，`draft=false`、`prerelease=true`，精确源码为上方 `12664aac`；自动 Homebrew 提交为 `87883018623b3d78068bab208d7ec4400285a29d`。历史版本和归档均未改写，alpha 不占 stable latest。

- 全部 23 个公开文件逐个核对 GitHub 资产大小及 SHA-256；四平台归档以仓库可信公钥离线验签，固定 version/OS/arch、归档摘要、payload pin、成员清单与公开 verification receipt 一致。Linux 20 成员、macOS 16 成员；bootstrap 与标签源码逐字节一致，PROVENANCE 的源码、签名工作流和 run ID 匹配。
- 所有二进制的 Go 构建目标匹配归档；除不导入 buildinfo 的 region helper 外，其余二进制内版本/提交标记存在。region helper 通过签名归档、payload 摘要和来源验证，不宣称其提供版本命令。四个 macOS arm64 主程序实际运行的版本/提交匹配，`shiki setup --help`、`shiki-install quickstart --help` 和公开 bootstrap 帮助通过。
- macOS arm64 使用真实公开归档完成首次与重复 `--stage-only` 安装；未建立 active/current、未创建运行配置或状态，未激活本机服务。不是新向导的真实完整首装，也不是公开包完整升级/回滚。
- 自动 Formula 从已认证公开资产重新生成，和 bot 提交逐字节相同，Ruby 语法通过。公钥 PEM 仅以 DER 等价核对处理跨平台换行；未降低信任身份要求。本轮未重复四平台实际 tap 安装。
- 临时独立验收脚本最初错误假定 `go version -m` 保留 `-ldflags`，并假定每个支持 helper 都导入 buildinfo；按实际构建契约修正观察方式后完成上述核验。只修正验收脚本，不改产品或发行资产，不把静态标记当作跨平台实际运行。

独立证据保存于 `/tmp/shiki-alpha4-public-verification/`：源 CI/最终 Release JSON、WSL 两次阶段记录、公开资产元数据、`verification-summary.json`、`smoke-summary.json` 与 Formula 重建文件。临时文件不是新的公开分发源。08/16 同步关闭发行门槛，真实账号、完整新向导双机首装、物理重启及公开包完整服务升级/回滚仍 pending。

## 一键安装增量（2026-11-16，本地实现与隔离回归完成）

用户要求与新增基线见 [28](28-quickstart.md)。新增 `bootstrap.sh --quickstart`、受信 `shiki-install quickstart`、`shiki setup`；不修改已发布 alpha.3，不增加 RPC、插件业务或第三方依赖。

已完成：下载/验签工具引导、签名归档自动传给系统支持安装、普通用户/Claude 官方安装编排、私有终端登录和连接凭据、客户端双端插件准入/端口完整替换确认/持久浏览器启动查看器、重复运行保留配置与失败状态。客户端不把远端仅检查 executable 的 HealthCheck 当作 Claude 账号已登录。

本地证据：bootstrap 15 项 HTTPS/真实签名夹具通过（包括向导输入保留、旧版本拒绝、无 TTY/自动化拒绝及清理）；客户端 13 项专项 race 通过（含真实 UDS/HTTP2 CLI 分派、继承 installer 登录与并发 AlreadyExists）；客户端/原安装事务/架构整包 race 通过（43.434s/11.250s/1.375s）；发行/发布/Homebrew 工具 28 项通过。shellcheck warning 级及 Bash 语法通过；全级 shellcheck 原有 SC2015/SC2016 提示保留，未冒称无提示。

最终复核已修：同源支持经签名来源、产物 hash、凭据摘要、策略、授权和服务文件检查后跳过重复 setup，保留运行 Chrome；普通 UID 使用真实 auth.Manager 验证 Token，过期/撤销时向导内确认后创建确定性后继文件，重试复用；部分 enrollment 读取原口令继续、绝不重置已存在 store，未提交密文或外部冲突仍保留人工检查；实际系统计划打印后再次确认；PATH 不覆盖 dotfiles 链接，失败不阻塞连接；client Plan 默认值与底层 receipt 一致；两端接续有效登录及并发 AlreadyExists。

最终安装专项 13 项顶层 race 通过（cmd 3.760s / install 1.219s），包含真实前台 PTY、系统计划拒绝、签名/撤销校验、登录恢复/取消、默认配置重入和私有文件保护；新增后整包 race 再通过（cmd/shiki-install 3.801s，internal/install 10.410s），vet 通过。初期真实 PTY 夹具因匹配输出后停止 drain 导致退出等待，已改持续 drain；Token 夹具的配置父目录已修为0700；失败轮不追认为通过。

独立 Linux arm64 容器使用 `golang:1.26.11-bookworm`，镜像摘要 `sha256:dc40a13b61304647ed7e5d581d246b86413dfc29d63c6d018310feb93cd98fc0`，2 CPU/3 GiB、network none、普通 checker UID1000、只读冻结源码/module cache。20 项顶层 quickstart/help/installer 检查全部通过、零 skip（cmd 2.074s，install 4.473s）；包含 Linux build-tag 实现编译，不表示实际 APT/systemd/Chrome 全向导。创建夹具用户时容器报告 home chown 警告，测试实际成功，不称无警告完整用户安装。证据 `/tmp/shiki-quickstart-verification-20261116/`，1014文件源码 manifest SHA-256 为 `27de87699e672be9574de82eef3a28d23be2629cb28afe892048b390b6321a70`；完成后核对 Go/scripts 与冻结副本一致；Linux amd64 两个入口交叉构建通过，不替代 amd64 运行。测试容器已自动移除，临时拉取的专用 Go 镜像也按精确标签回收，不操作已有用户容器。

本地批次不包含真实 APT/官方 Claude 安装、Linux systemd/Mac LaunchAgent 完整新向导、真实账号、公网或物理重启；这些门槛仍未完成。后续 alpha.4 公开发行证据见上节，公开 alpha.3 不含本功能。

**原清单进度（2026-11-12）：42/42，原清单最后一项已关闭。** 功能、平台、验收依据及未支持范围统一见[支持声明](18-release-readiness.md#current-support)。Phase 0–5各阶段为11/11、7/7、6/6、6/6、6/6、6/6；认证生命周期和长稳/资源/性能沿用2026-11-01用户确认，历史实测/失败及源码范围保留。生产heap/object归因、真实Claude登录/推理及物理主机重启仍未实测。2026-11-15已发布alpha.3，新增地区/浏览器及发行证据单列如下，不扩大原42项或宣称稳定版。

<a id="evidence-public-alpha3-release-20261115"></a>
## alpha.3 发布执行（2026-11-15）

用户授权的`v0.0.1-alpha.3`已发布。固定源`d9056c3ffed86b2bbac9cd5e89c433eaa3093cb2`包含查看器EOF及下述CI修复，[CI43723367228](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43723367228)57/57通过。不可变tag的[Release43727419792](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43727419792)成功，60项通过、复用检查Homebrew分支按工作流跳过1项；四平台Homebrew证据来自发布前同源CI，不将跳过计通过。23资产于`2026-11-15T15:48:03Z`公开，`draft=false`、`prerelease=true`。新增功能的隔离实测和外部门槛沿用下节精确范围，不因发行扩大支持声明。

提交`dea8f77742e2af25620bd3d1a1dac577a18f8cb2`已推送master；[CI43712715128](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43712715128)未通过。四平台build/vet已执行，随后gofmt拒绝`plugins/region/helper/engine.go`、`engine_linux_test.go`和`internal/plugin/policy.go`排版；WSL的stages.json记录build-lint exit2并阻断后续维护，未将未执行项计通过。三文件格式修正经Go1.26.11规范LF输入比对通过；无语义改动。四目标compile与四平台Homebrew通过仍仅绑定原提交，新完整矩阵另验。

第二轮`d7be3ad278962921c447713f3a0f12450c1cdff4`的[CI43714541188](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43714541188)为56/57：`TestHelperClientCancellationClosesItsCallAndOwnedGroup`的`unix.Poll`返回EINTR，测试立即误报退出失败。现仅修正测试等待：按单一2秒截止重算剩余预算，EINTR继续，其余错误/超时/缺少POLLIN仍失败。冻结`/work/alpha3-poll-fix`、普通shikitest、Go1.26.11执行`go test -p=1 -race -count=1 -timeout=90s ./plugins/region/helper -run 'Test(OwnedCommand|HelperClientCancellation|NativeCommandCancellation)'`通过（4.249s）。首次启动命令因login shell重置PATH未运行测试；改用绝对Go路径后通过。生产代码未变，完整矩阵和公开发行仍待后续门禁。

第三轮`9575ec00dfaac42cfc0141ae180270a9ae311d35`的[CI43718718959](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43718718959)为56/57，插件测试已通过。唯一失败`maintenance (linux, amd64)`在准入返回`runtime_identity_changed`；下载的preflight记录为Linux/amd64、Go1.26.11、kernel `7.0.0-1014-aws`，日志未输出当前runner的具体差异字段，不推断其内核值。独立runner间复用了要求完全一致的runtime身份；维护尚未执行，不记通过。工作流改为维护runner本机重新执行preflight及三项前置检查，各项成功后才mark；不移除严格身份/源码/取消检查、不复用其他runner资源测量。`python3 -B scripts/test-platform-ci.py`30项通过（4.179s）；本地actionlint未运行，隔离容器不能联网下载工具。下一完整CI负责实际工作流及原生维护验收。

公开产物独立验收：通过`gh release download`取回全部23资产，在隔离Linux普通UID下使用固定tag源码构建的可信verifier及仓库预置信任公钥，四目标Ed25519签名、归档/payload摘要、版本/平台、PROVENANCE、验证收据、公开资产名称/大小/digest及固定源bootstrap全部匹配。SPKI SHA-256为`e21f6dd671ea5042c43c492b759b933dda73985f89a823b43c651ff7c787c17d`。Linux两架构各20成员，包含region/browser及helper/runtime；macOS两架构各16成员，包含browser客户端，不包含region/helper/runtime。Linux amd64实际执行9个公开二进制版本命令，版本/commit一致；region/browser/files帮助及首次/重复client stage-only安装通过，均为`staged / NOT_STARTED_OR_READY`，未激活服务。受限helper无version入口，其身份由签名成员校验覆盖。

自动Homebrew提交`280db0e52a7ff7e36e8663f5873ee0a530070b2b`已推进master；从GitHub读取该提交的Formula，与从上述公开认证资产独立生成的公式逐字节一致，SHA-256为`17f55ab49d5c5a8e851ede92952bcb08b22329beaefed2c95227960741e8039c`。这证明公开公式来源与内容，不替代本轮未执行的公开tap全平台安装或公开包实际升级/回滚。

验收工具失败保留：Windows默认`git archive`产生CRLF公钥（116字节），公开与Git规范LF公钥为113字节；采用`git -c core.autocrlf=false archive`导出同一tag后精确比较通过，未放宽产品验签。容器Python不支持`tarfile.extractall(filter=...)`，在全部成员已认证且严格限制为已知普通文件后修正临时解包调用，只续跑尚未完成的smoke；不重复已通过签名检查，不修改发行产物。

发布后清理（2026-11-16）：按用户授权移除本项目16个容器、7个镜像和2个命名缓存卷，BuildKit按事先登记的缓存ID回收9.702 GB。随后`docker ps -a`、image/volume清单均为空，`docker buildx du`为0 B。删除本机`.ci-artifacts/`（含公开包下载副本）、`.tools/`、已登记的Go临时构建目录、两份浏览器下载目录、测试日志与OpenSSL临时目录，以及12份会话验收脚本；检查两个仓库临时目录均不存在，匹配的测试进程和命名测试目录均为0。源码、验收文档、GitHub日志与正式发行资产保留；未清空共享Go模块/宿主工具缓存。四个DNS监督进程退出0；两个浏览器监督进程在主动停止/删除容器后报告143/137，属于此次清理终止，不重跑验收或改写此前结果。

<a id="region-browser-20261115"></a>
## 地区环境与远程浏览器扩展（2026-11-15，实现及本地/容器验收完成）

用户已确认[27设计](27-region-browser-plugins.md)并授权完整实现。新增region/browser插件及独立特权helper/浏览器运行服务，使用`proto/extensions.proto`固定用途协议；不是远端通用执行。用户要求可跳过需要其账号/管理员/外部平台参与的测试，但须最终逐项披露，不把跳过记通过。

已交付设计、类型化schema/SDK、宿主路由、CLI、查看器、诊断、region/runtime及安装维护，并完成授权范围内的本地/隔离容器增量验收。下文按时间保留失败、修复及各自产物证据，最后闭环段落是当前结论。Windows自动Go1.26.11被Application Control阻止，使用授权Docker Linux工具链，不放宽主机策略。本记录不更新历史42项计数或公开发行版本；外部门槛见27。

首批证据：隔离Debian12 amd64 systemd容器，Go1.26.11，普通`shikitest`用户，冻结目录`/work/sdk-20261115`，`go test -p=2 -race -count=1 ./pkg/plugin ./pkg/plugin/diagnostics ./pkg/protocol/extensions/...`通过：SDK 2.281s、诊断1.041s，生成扩展包无测试但编译通过。初次执行被容器`/tmp`的noexec阻止；仅在可丢弃容器内调整该挂载后完成检查，未修改宿主挂载或削弱产品校验。这不覆盖尚未运行的CLI/宿主/插件全链路、真实浏览器画面和安装流程。

后续冻结快照`/work/host-20261115`的定向race检查通过：`internal/auth`、`internal/plugin`、`internal/agent`、`internal/server`、`internal/server/session`、`internal/client`、`cmd/shiki`和`test/architecture`。查看器首次编译发现`files.go`漏导入`errors`，补齐后仅重跑`plugins/browser/client`通过（1.378s）。独立`/work/region-20261115`的`go test -p=1 -race -count=1 ./plugins/region/...`通过：helper 1.220s、server 1.030s，两个入口包编译通过。真实系统变更、Chrome和安装仍待验；源码文件系统事务测试不冒充真实主机DNS/重启。

用户追加确认：整机Chrome托管策略强制禁止密码保存、Chrome同步及Chrome账号登录；管理员计划披露影响全部官方Chrome实例，拒绝覆盖冲突策略。网站登录仍允许。原生Chrome诊断保留实际网络路径，域名供应商的连接前DNS重绑定风险如实披露，不能把预检或连接后检查写成阻止保证。

用户另确认管理员启动解锁：通过systemd加密凭据和固定用途解锁服务向普通用户密钥环提供stdin口令，保持无人值守开机目标，不重置已有密钥环。无TPM时使用host-key并披露整盘/root密钥失窃边界；实现和真实服务验收尚待完成。完整依赖Debian12容器镜像已构建；Ubuntu24.04镜像首次构建因官方APT下载返回502失败，未记为平台通过，单独重试不重启现有已验环境。

真实Debian12链路已通过：普通用户CLI → agent IPC → 直连TLS daemon → 独立region插件 → sudo固定helper，完成inspect、JP计划/应用、同request_id重放、过期revision拒绝、主机诊断和回滚。真实tmux `/bin/sh`场景证明旧pane的PID及环境不变，新会话和显式重启收到`LANG=ja_JP.UTF-8`、`LANGUAGE=ja_JP:ja`、`TZ=Asia/Tokyo`；随后恢复原系统设置。这不是Claude账号登录或推理验收。

真实root守卫另在`--network none`的可丢弃容器通过：固定路径helper、sudo白名单、systemd守卫、实际`/etc/resolv.conf`变更、30秒确认租约、无人确认后的独立恢复、显式确认与回滚、外部编辑冲突保留和安全卸载预检。DNS/TLS端点为容器私有夹具：将8.8.8.8仅绑定到该容器loopback，以`probe.example.test`及临时CA完成真实解析和证书校验；没有连接公共DNS或第三方服务，不冒充真实出口/公共DNS地区验证。首次脚本错误地期待确认状态`applied`，实际正确状态为`confirmed`；修正脚本后从确认后的步骤继续，不更改生产状态契约。

实际冲突恢复发现：管理员恢复冲突文件后，旧helper仍因已清除pending但保留recovery_problem而无法完成卸载。已修复root-only `prepare-uninstall`：仅在待恢复记录仍对应当前desired时重新验证并恢复全部目标；外部差异仍拒绝覆盖，普通RPC不能绕过管理员复核。扩展既有卸载回归并通过定向race（1.059s），实际容器复查得到`disabled`、无pending且原resolver恢复。CLI同时将`rollback_conflict`返回非零退出码并保留结构化证据；定向server/client/CLI回归通过。浏览器启动预算与加密密钥环等待对齐：server插件启动120秒，远端browser插件管理默认等待150秒，显式用户截止仍优先。

增量验收：已安装helper在普通UID下实际拒绝五类请求：sudo启动guard、管理员activate、inspect额外argv、JSON任意路径以及不经sudo直接执行。region诊断统一采用`observed`/`warning`/`unknown`，不把配置匹配宣称风控通过；`TestRegionDiagnosticComparisonDoesNotPromoteMissingEvidence`定向race通过（1.127s）。Ubuntu24.04完整依赖镜像在有界APT重试后构建成功，独立断网systemd255容器启动成功；只记录环境就绪，尚不属于产品平台验收。

原生DNS owner增量：Debian12 resolvconf 1.91+nmu1、openresolv 3.12.0及Ubuntu24.04 systemd-resolved 255.4分别在独立`--network none`容器完成真实DNS/TLS检查、30秒截止无人确认恢复、显式确认和回滚。Debian两个owner还证明竞争DNS源/独占源在计划中报告阻塞原因、应用时拒绝，原peer保留。resolved需要活动链路；仅loopback环境返回Network is down，增加容器内dummy接口后完成，不连接外部网络。原测试误用了守卫unit名称、openresolv参数顺序及“计划必须失败”的断言，按真实契约修正；不据此修改产品行为。

原生验收发现并修复：Ubuntu标准`/etc/default/locale -> ../locale.conf`需要操作固定canonical文件并保留alias归属检查；systemd-resolved生成目录/文件属于systemd-resolve，读取仅对两个固定输出增加有界、fd验证、禁止symlink跟随的只读例外，不放宽root写权限；helper创建resolved配置目录时显式0755，避免调用者私有umask让服务无法读取。Debian resolvconf会删除记录注释，改用root私有、参与同一事务与开机校验的记录摘要证明归属，不认领无证明的同名记录。

进一步定位空tail冲突的真正原因：空文件字节在JSON omitempty往返后成为nil，而reflect比较误认为内容改变。文件状态现在按kind/target/mode及字节内容比较，空与nil字节等价；不将不存在的文件与空文件合并，不忽略权限或外部内容变化。既有失败夹具由管理员恢复原检查点后，经root-only预检恢复，再验证新事务。helper定向race通过1.231s；resolved文件信任、locale alias与空检查点回归通过1.035s。实际resolvconf与resolved新事务均恢复原DNS，openresolv拒绝竞争exclusive且完成卸载预检。

浏览器CLI文件链路增量：实际上传已提交，但客户端收到canceled。根因是`internal/plugin/extension_proxy.go`在Connect client-stream首次Receive已消费单响应及终态、remotePool释放调用上下文后，再Receive一次。删除多余读取，不取消终态验证；新增`TestBrowserUploadProxyPreservesTerminalResult`覆盖真实HTTP/2两跳及pool生命周期，成功回执和终态DataLoss均保持，定向race通过1.096s。更新隔离agent后，恢复同request_id上传验证通过；303360字节SHA256为`38ae72e0775c2d67e4c414396839c191a11af2ed44997bbcb8f885312d672363`，重复上传、列表、下载内容与0600权限、覆盖确认及profile删除通过。此前同场景已完成profile创建重放和过期revision拒绝；没有把失败上传响应记为成功。

真实密钥环验收在完整依赖Debian12容器完成：systemd252 `LoadCredentialEncrypted`从root保护的host-key密文向UID1000提供口令，创建非空口令专用加密keyring，停止创建unit后通过独立unlock unit重新解锁，均得到active。首次因Docker Desktop私有`/run`未共享传播导致空凭据挂载；仅在可丢弃容器内`mount --make-rshared /run`后通过，未打印口令或采用明文后端。这是手工受控runtime夹具，不冒充安装器、重启或TPM验收。

Chrome首次CLI启动在Prepare阶段失败：实际plugin环境只有经过宿主过滤的HOME/PATH，缺少XDG_RUNTIME_DIR与DBUS_SESSION_BUS_ADDRESS。修复runtime manager按当前UID选择`/run/user/<uid>`并检查已存在的私有归属目录，systemd子命令明确使用该用户固定bus；不扩大宿主环境继承。Prepare失败改为可见FailedPrecondition。`plugins/browser/runtime`、`plugins/browser/server`定向race分别通过1.234s、1.176s，实际Chrome链路仍继续验证。

冻结宿主交叉编译通过：Linux arm64的CLI/agent/daemon/region/helper，以及darwin amd64/arm64的CLI/agent。这只证明这些冻结源的目标编译，不代表Chrome、安装包、ARM或macOS原生运行通过；后续helper原生兼容修复另行编译，不追认早先产物。

实际查看器增量：Debian12普通UID运行官方Google Chrome157.0.8103.78，经过CLI/agent/直连TLS/独立插件、loopback查看器显示真实页面，显式接管后完成英文地址导航及`日本語 / 中文`剪贴板粘贴。初次剪贴板被拒绝：固定noVNC使用Z_FULL_FLUSH而不结束zlib流；服务端现在只在flush边界和完整有界格式载荷均成立时接受。`TestRFB*`定向race通过1.132s。一次误用旧`/work/host-20261115`快照导致已有元数据上限用例失败；改用完整冻结源后通过，不修改或削弱断言。

原生下载→CLI取回通过：远程Chrome点击下载生成`browser-proof.txt`，47字节，SHA256 `f1be8ac6aae86a947a366273123622e6691e234e01c6866656b4ce683fbf2709`，CLI验证内容及0600权限。直接VNC同样复现XIM快速输入绕过组合，完整链路按键字节及顺序正确；尝试独立IBUS_ADDRESS又暴露GTK模块仍在会话总线上监视服务。最终使用原生Fcitx GTK3前端，Fcitx与Chrome同选定密钥环总线、私有显示/配置不变，已有同名输入法拒绝接管。真实noVNC中零人工延迟输入`nihao`/`nihongo`，Pinyin/Mozc候选及提交`你好ニホンゴ`通过。冻结`/work/browser-native-gtk-20261115`的Chrome/diagnostic/launch/IME定向race通过1.188s。

安装/发行冻结快照`/work/install-lf-20261115`通过manifest生成和release Go用例及48项Python工具用例。Windows导出的shell换行和manifest 0777权限不属于Linux规范产物：冻结导出规范化LF/0644后验证，仓库补充shell LF属性，不放宽生产manifest权限。真实Ubuntu安装发现APT维护目录归属和systemd255凭据ACL掩码差异，正在修复并实际复验；不以这些工具用例宣称系统安装完成。

完整静态复核发现11项事务、恢复、持久化、传输和敏感目录准入缺口。浏览器状态修复已通过独立冻结`/work/browser-state-fix-20261115-00ZnY3`中的runtime7项及server13项定向用例（2.045s、2.881s）：覆盖上传硬链接发布中断、初始化清理、重试冲突、真实私有socket延迟就绪、落盘后错误的revision/receipt保持和64KiB/8MiB编码上限。就绪的systemd属性与同步错误为受控注入，未冒充真实系统重启或掉电；region与宿主准入修复继续进行。

并行修复交付：helper冻结`/work/region-conformance-20261115.41I68X`全包race通过4.544s，journal schema2持久化操作归属及前驱，覆盖ABA回滚拒绝、部分应用管理员恢复、进程组取消/输出有界及DNS备用服务器。宿主冻结`/work/host-conformance-final-20261115-bJnBk0`的项目/插件/SDK/server/client定向race与SDK回调追加检查通过；真实sync冻结`/work/host-conformance-sync-20261115-vYC13c`通过，覆盖状态目录祖先/子孙/别名/旧关联/缓存后重定向和List/Stat/Read/Write/Delete/Watch。三处新增集成夹具API错误已修正，不省略失败文件。

Ubuntu24.04 amd64/systemd255.4/UID1001实际安装验收已通过：20文件私有Ed25519签名bundle、完整setup及专用加密keyring创建、离线维护、错误口令不损伤旧凭据、真实重新解锁、daemon重启保留Chrome、运行中拒绝重新注册凭据、异身份unit阻止卸载、回滚后卸载成功并保留278个profile文件/加密凭据/无关哨兵。普通备份排除敏感档案；TLS与shell夹具正确返回application_pending，不宣称Claude可用。产物SHA256为`75cd6d0856c0ad5c3d17511f6186306505c90ccbd54ce43bfd0460320ad4a357`；该历史产物不含后续schema2/持久化/GTK增量，最终增量另验。APT仅校验root lists父目录，允许APT自管_apt partial；systemd255凭据0550/0440必须通过fd与ACL校验，不能只按mode放宽。凭据ACL三项（含20表项和真实ACL）、安装依赖定向检查通过。

真实Chrome默认诊断已通过：当前profile原生`ja-JP/ja`、`Asia/Tokyo`与启动配置分别取证，外部访问disabled、WebRTC未授权和实际Secure DNS不可见均保持unknown；无网络容器online=false、原生webdriver=true如实报告，不修改这些值或宣称防检测。

最终schema2原生验收：冻结`/work/final-region-schema2-20261114T233157Z.oKRl5H/source`构建helper SHA256 `754ad58c6e2a609267a5d5e01e2e12b2c7e7d34a9a948227a9db94146307ad35`。Ubuntu resolved255.4、Debian resolvconf1.91/openresolv3.12三个新断网容器均通过普通UID sudo六动作、非法调用拒绝、JP→US→JP代际链与旧A回滚拒绝、逆序归属恢复、guard重启与35秒无helper调用下的30秒自动恢复、确认保持、外部编辑冲突保留和root安全恢复。实际DNS与证书验证TLS共20次握手，HTTP应用字节为0，不访问公网。resolved短时密集事务触发原生5次/10秒限制，真实记录rollback_failed后由守卫自动恢复；未弱化unit或添加隐式重试。结束时原系统状态逐字恢复、授权disabled，再停止夹具。私有证据归档：`local://final-region-schema2-{resolved,resolvconf,openresolv}-evidence.tar`。

最终宿主/安装增量：`TestBrowserProfileAuthorizationAndPersistence`定向race通过，含真实TLS→daemon→gRPC子进程的未授权上传、空闲发送者接收worker错误、部分上传取消/临时文件清理及无发布；集成artifact补齐真实browser manifest和双端声明，未删失败场景。真实sync保护与GTK依赖/凭据ACL回归通过。冻结`/work/final-integration-20261115-r3/source`的959文件manifest SHA256为`17c78e72faa51aa6d1988f24a1fea6b7f09c8f97d3735033180d6ecefdae72c1`，普通UID离线构建10个可执行文件和发行工具，20文件私有Ed25519签名包独立验签。归档SHA256 `705d00d24535b23a68f7630adfd5618131ba7e9d20c4d8dbaa4198c87e50f3d4`，SUMS pin `cfc6a8622ef12e8650f8360271fad2cdeef0e3e6dbbfbae33b75fb87720eacf0`。

该最终包实际Ubuntu setup通过，安装helper/runtime分别匹配签名与receipt，Chrome157及观察到的18个runtime/Chrome/Fcitx进程均为UID1001，专用加密collection解锁、gnome-libsecret参数、Fcitx私有配置及选定总线名称归属均有实证。离线维护保持同一实例，6个凭据/档案/哨兵/源摘要和原系统默认不变。空、已撤权schema1夹具先按设计拒绝；经明确授权逐字归档后才新建schema2，未迁移或推断归属。最后显式停止Chrome、网络保持none。证据：`local://final-integration-installation-evidence.json`、`local://final-host-integration-evidence.json`、`local://final-integration-source-sha256.json`。

真实查看器最终增量：服务端原生文件选择框上传48字节多语言文件并显示收到的内容；多个查看器显式接管后旧控制者降为只读，控制者关闭不会自动提升观察者；显式resize实际得到1600×374 framebuffer。查看器本机上传45字节成功，但下载最初HTTP500：Connect v2正常终态包装EOF，`== io.EOF`误判。改用`errors.Is`，保留finish/长度/hash检查，新增包装EOF及缺finish回归；最初测试误用v1错误构造签名，改为v2 `WithCause`后定向race通过1.069s。冻结`/work/browser-download-final-20261115`独立构建插件SHA256 `de1ca27063c0ef32f63af4e30e3db90a4c38c0cf3b7cfed7f9c3b405e917e4f8`；恢复生产错误脱敏后真实网页下载完成，本机实际文件45字节、SHA256 `fc99965f92140121a7b6884216e9f631cde707723f0eb2fbf20f1cede50e68b2`，正文与原上传完全一致。诊断用原始错误输出仅存在隔离临时源码/客户端，未进入仓库或签名包，已撤下。

证据边界：上述签名包关闭在包装EOF修复之前，不能声称包含该后续两文件增量；其实际客户端构建/UI证据单列。原生ARM/macOS/WSL2、真实账号、公网地区观察、物理重启与官方公开包未验，完整操作清单见27；未发布新版本。


<a id="evidence-first-beta-candidate"></a>
## 首个 beta 候选修复与验证（2026-11-13—14，发布前验收通过）

基线 `f4a28dd` 的 [CI 43258490322 attempt 2](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43258490322) 已完成57/57；后续代码审计和离线真实签名复现发现新增安装/插件路径未被该矩阵充分覆盖，不能直接发布。修复范围为 bootstrap 认证与清理、发布/重跑/Homebrew 资产一致性、插件发现与启用/动作、原始 manifest 完整性、发行者/版本绑定、有界下载/解包和可恢复更新/卸载。新增真实签名 HTTPS → CLI → agent → 独立 SDK 插件进程验收，以及内置 sync/port 动作和失败报告回归。

已完成的独立 Linux amd64/Go1.26.11 检查：最终 bootstrap 私有 HTTPS + 真实 Go Ed25519 签名夹具10项、发布工具32项、Homebrew公式7项全部通过（bootstrap 安装器子进程为受控返回码夹具；删除的源码文本断言不计行为证据）；`actionlint v1.7.7` 与 bootstrap shell 语法通过。SDK、插件存储/传输、host supervisor 和 sync client 定向 race 检查通过；后续 `cmd/shiki-install`、`pkg/plugin`、`internal/agent`、`internal/server` race 检查亦通过。源码分别冻结于独立验证目录，不追认为后续变化自动通过。

本地发现 Windows checkout 将 LICENSE 转为 CRLF，导致严格许可证摘要检查失败；已将 LICENSE 固定为 LF，恢复官方文本的35,149字节与 SHA-256 `3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986`，未放宽 verifier。受管安装已由整目录 release symlink 迁移为独立私有 plugins_dir 和受保护 `.builtin` 指针；目录迁移/旧版回滚保留用户插件数据，包目录、停放目录及硬链接所有权标记不混入普通状态备份。

后续进程回归发现并修复：SDK 将 worker 的 Go 结构体/映射切片直接交给 `structpb.NewStruct`，导致内置 JSON 动作返回内部错误；现经 JSON 编码构造 protobuf 对象，空 sync 结果保留数组类型。真实第三方插件发现/启停/重启/损坏拒绝，以及 sync、port 的动作发现、原始动作和直接 CLI JSON 命令 race 回归通过（61.763秒）。首次容器输入从 Windows 挂载继承 mode0777，导致源 manifest 被严格权限检查拒绝；只修正冻结快照权限为0644，未放宽产品校验。进程测试的取消先于 cleanup 导致 SIGKILL，已修正测试生命周期，让 SIGTERM 正常回收先于取消兜底。

真实公开 Linux amd64 alpha.1、alpha.2 归档均经仓库 Ed25519 公钥独立验签：SHA-256 分别为 `ccaeedcdc6e802f29fd1125304813eb2be2af0fd604f5f6e06da914707a957ba`、`f8a56c3cc15ce4a8a05c472fb1db7ca2a5fa319f83eea2b381b627e1b9cefd15`。两个真实旧 helper 均完成：stage/config → 新版私有目录迁移 → 新 helper 的 legacy-config 委托旧 helper → 旧版卸载 → 新版再升级/卸载；第三方包、恢复 journal、原目录和锁 inode 全部保留。此项验证真实 helper/文件布局，不冒充物理重启或旧版服务带业务全链路验收。

最终接受前拒绝回归通过：宿主已接受后失败、schema 直接拒绝、拒绝响应丢失后重放恢复三类均恢复旧包/启用状态、清除已完成 journal，并允许后续有效更新（race 1.867秒）。最终真实 HTTPS 签名 marketplace CLI/agent/SDK 进程回归亦通过（103.90秒），覆盖 schema 拒绝、错误运行版本、降级/发行者替换拒绝、启停、JSON 命令、宿主重启与卸载。受管布局/脚本 race 回归通过（3.931秒）；测试长目录改为短路径，不放宽100字节 socket 限制。Git archive 的 CRLF/0664 输入问题通过 `core.autocrlf=false` 和 `tar.umask=0022` 修正；中断丢失的检查结果不记通过。

候选 Linux 包构建通过，SHA256SUMS pin 为 `d59a410eab66b3ab09949a3cc987b81e412d9f6bbffb6abead3e4316c0cb07a8`，安装器 SHA-256 为 `7ca25af94b63d6ce8894a65748734ba719567e52d53eb2444ee447a588771c60`。真实 Go 签名/私有 HTTPS bootstrap 首先完成实际安装器 stage-only，并确认未激活和临时目录清理；随后在独立、无外网、私有 cgroup 的真实 systemd PID1 容器中完成完整用户服务安装，返回 `local_application_ready`，CLI 实测 agent `ready`，systemd MainPID=217。真实卸载后该 PID 已退出，用户配置/状态保留，服务文件与 bootstrap 工作目录移除。Tailscale 显式禁用，`external_peer_acceptance=pending`；临时签名密钥不代表公开发行签名。

候选源码 `058ea5d9dd526d059b4946f5d7d52cf2a261d3a4` 的 [CI 43464849797](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43464849797) 已失败：两 macOS preflight 的下载取消测试发送 TERM 后，输出管道20秒内未关闭；其他96项工具测试通过。两 Linux preflight、四平台编译/Homebrew 与 WSL2 通过，下游依赖矩阵未展开，不记为通过。此前本地验收冻结树为 `c2cbf9bbd31b20f4e86efc541d85f3da90c9ba9f`，不追认为后续修复自动通过。

本次 CI 修复将 Perl 下载监督器的无界阻塞读取改为有界 readiness 等待，取消时先终止并 waitpid 回收生产子进程，再退出/清理管道；不依赖 curl 刷新 stdout 或下载期限才处理取消。现有下载取消回归扩为 HUP/INT/TERM，要求5秒内返回129/130/143并清空工作目录，不延长原20秒上限、不跳过 macOS。Linux bootstrap 10项通过（9.620秒）；请求级事件隔离后，三种信号定向回归通过（1.379秒）。保留下载大小/HTTPS/签名校验及安装器回滚等待约束。

修复已提交推送为 `87cfefbc2316f2eef8b9262c0d872da773d41a55`。[CI 43470576055](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43470576055) 前两次为55/57：macOS arm64 unit-test-support/maintenance 均 runner_id=0、steps=[] 即 cancelled，注解为 `The job was not acquired by Runner of type hosted even after multiple attempts`；未将未调度记为通过。第三次实际调度后 unit-test-support 通过，整体56/57，仅 maintenance 的 launchd-lifecycle 退出1（282.029秒）；其公开产物没有原始失败行，不将之前 runner 容量问题当作本次执行失败的解释。

LaunchAgent 生命周期测试仍在卸载后断言整个 plugins_dir 不存在，与本批私有目录/第三方数据保留契约必然冲突。`a5032c1` 改为要求 `.builtin` 发行链接移除、0700 私有目录保留，并写入用户数据标记，验证经崩溃恢复、升级、回滚和卸载后字节不变；服务、进程、锁、命令链接及原配置/状态/备份检查保留。仅更新过时测试契约，未修改生产行为；Darwin/arm64 交叉编译通过后，真实原生验收亦已通过，见下方完整 CI。

最终固定提交 `a5032c112ffdace9b9ab6455f7280480223a8482` 的 [CI 43484685340](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43484685340) attempt 1 为 completed/success，57/57作业通过，无失败、取消或跳过。四平台单元/集成/E2E、发行工具、embedded、Homebrew、WSL2、terminal 与原生 maintenance 均完成。公开 macOS 维护证据中，`TestNativeLaunchAgentLifecycle` 为261.426秒，活动上传/下载升级分别为134.563/122.721秒，均 exit_code=0、state=passed；旧卸载断言修正后的真实流程已闭环。macOS 排队容量告警保留，本地监视器超时只重新接续观察，没有取消或重启该轮远端验收。本批发布前门槛关闭，不代表 beta 已公开发布。

最终记录提交 `96961ab` 的 [CI 43495414864](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43495414864) 首轮失败：`TestThirdPartyRuntimeDiscoveryPersistenceAndFailClosed` 在 Intel macOS 的 RESTART 调用返回 `deadline_exceeded`（测试65.46秒），下游维护/终端未执行。测试通过 `frontend.Options{Timeout: time.Second}` 建立的客户端将全部后续 RPC 限制为1秒；`internal/client` 默认普通 RPC 为5秒、生命周期 RPC 为15秒。改用默认选项，启动探测仍保持每次1秒、总计20秒，测试总期限及全部行为断言不变，不增加自动重试。生产代码不变；本地定向 race 与新提交远端 CI 尚待完成。

预算修正后，Linux amd64 定向 `go test -race -count=1 -timeout=10m -run '^TestThirdPartyRuntimeDiscoveryPersistenceAndFailClosed$' ./test/integration/agent` 通过（35.255秒），覆盖真实 CLI/agent/插件子进程、重启去重、配置、恢复与失败关闭。首次重启旧临时构建容器因其入口脚本退出而终止验证进程（137），不计验收；随后独立容器使用保留文件执行成功。新提交的远端完整 CI 尚待执行。

真实公开 marketplace、发行密钥/tag/新 Release、真实 Claude 登录/推理、物理重启和生产 heap/object 归因不由隔离夹具替代。原42/42保留历史范围；[08](08-roadmap.md)、[16](16-architecture-conformance.md)和[发布门槛](18-release-readiness.md#beta-readiness)同步记录。


**插件 marketplace 安装批次（2026-11-12）：** `internal/pluginmarket` 现提供签名 envelope、Ed25519 trust store、严格 JSON/HTTPS/平台/归档身份校验和流式 SHA-256 验证；`cmd/shiki` 现提供 `plugins trust add/list`、`plugins market add/remove/list/search`、签名 marketplace 安装和更新。安装前完成 index、签名归档元数据、归档名称/大小/hash 与 manifest 的验证，手工 URL/path 安装不进入自动更新。已验证 Linux amd64 CLI 目标编译及 `internal/pluginmarket` 测试编译；Windows 执行被 Unix-only 源码和 Application Control 阻断，未将其记为通过。外部 marketplace/发行资产验证未执行，未关闭既有路线图或发布门槛。


**CI 重跑与验证（2026-11-12）：** 提交 `91fe5c7` 的 [CI 43093710222](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43093710222) 首次执行的 57 项矩阵中，WSL2 的 `actions/upload-artifact@v7.0.2` 因 artifact 服务返回 HTML（`Unexpected token '<'`）在五次重试后失败；WSL 产品与维护测试本身已完成，首轮最终 53/57，另有四个下游 terminal/maintenance 作业未调度，不将该基础设施失败计为源码回归。完整重跑 attempt 2 已 completed/success，57/57 作业通过，无失败、取消或跳过，包含 WSL2、四平台插件/E2E/集成、terminal、maintenance、embedded、release 与 Homebrew；本轮无源码变更，首次失败与重跑均按实际结果保留。
**CI 修复批次（2026-11-12）：** `e4d660f` 的 [CI 43068229844](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43068229844) 实际失败在四个 native preflight 的生成协议漂移检查；同轮 WSL2 job 也失败，公开产物未提供其私有 Linux 细节。根因是本批 proto 扩展提交了 `proto/plugin.proto`，但遗漏锁定版本生成的 `pkg/protocol/plugin` 绑定；生成绑定补齐后，生成代码占用的旧手工动作服务及重复 handler 一并移除，客户端、服务端和准入路径统一使用 `Plugin.InvokeAction`。协议包定向测试、固定工具重复生成和 `git diff --check` 已通过；远端复验尚未完成，不把本批记为 CI 通过。
**Go 安全补丁批次（2026-11-12）：** Go 1.26.10 的标准库 `net/http` 被 `govulncheck` 报告 GO-2026-6859，已将项目工具链及所有 CI/release、WSL、Linux 固定测试入口升级到 Go 1.26.11，并记录官方归档摘要和 `golang:1.26.11-bookworm` 多架构 manifest pin。版本文件、校验摘要、缓存键与当前诊断断言已同步；Windows 本地插件构建因 Unix-only 传输符号不可用而未作为成功证据。远端 CI 复验待新提交。
**格式化修复批次（2026-11-12）：** CI 43076087975 的四个 native preflight 在 `make lint` 失败，实际是 Go 1.26.11 `gofmt` 检出的四个源码文件漂移；已修正 `plugins/port/client/worker.go`、`pkg/plugin/manifest.go`、`internal/client/client.go`、`internal/agent/ipc.go`。Linux 定向包测试 `plugins/port/client`、`pkg/plugin`、`internal/client` 通过；本地复现已确认漏洞扫描和模块校验通过，WSL2 仍在 product-and-maintenance 阶段失败，公开日志没有私有 Linux 细节，远端复验待新提交。
**CI 失败复现与修复（2026-11-12）：** 提交 `e4d660f` 的 [CI 43082895990](https://github.com/DDSRem-Dev/Shikigami/actions/runs/43082895990) 完成 56/57 个任务，仅 `unit-internal (darwin, amd64)` 失败；日志明确失败用例为 `TestSDKInteractiveRecoveryWaitsForRegisteredMap`，在 45 秒内未通过“缓存 self map 仍可被 SDK 观察”的测试屏障。当前工作树已将测试门槛改为只要求 `Running`、self 存在且无认证 URL，同时继续校验旧注册 key 和未授权状态，避免把 `Self.Online` 的平台时序差异当作协议完成。Windows 本机该包因 Unix-only 符号无法编译，Linux 容器启动因 Docker 返回 125 未执行；待远端复验。
**本批CI修复与验证（2026-11-05）：** 补充提交`17ed77f`的[CI42017919620](https://github.com/DDSRem-Dev/Shikigami/actions/runs/42017919620)已completed/success，57/57任务通过，无失败、取消或跳过任务。修复首次tsnet注册被重复启动、过早公开URL及发送期间deadline被清理取消覆盖；host与独立SDK保持相同取消语义。macOS arm64两项原失败用例及四分支注册回归已直接读取日志确认pass；全部原生平台、WSL2、插件/E2E、终端和服务维护通过。旧CI41888706987的失败/容量取消及首轮CI42012596520的56/57结果保留。文档提交`d49310d`的稳定性复验[CI42023610286](https://github.com/DDSRem-Dev/Shikigami/actions/runs/42023610286)已结束，56/57通过，唯一失败为Intel macOS显式断网恢复登录等待URL超时；后续`ff338f9`的[CI42028867098](https://github.com/DDSRem-Dev/Shikigami/actions/runs/42028867098)已结束56/57，所有节点认证回归及Intel内部包通过，唯一失败为arm64快速退出pane的空PID解析。有效授权SDK复用及失败退役已通过本地与该轮节点验证；本批继续修复tmux 3.8关闭PTY但waitpid状态未到的合法过渡，并明确禁止据此自动重启或开始自动清理，最终tmux/session/架构race与vet通过；冻结`5254892`的原生macOS arm64隔离tmux 3.8两整包race亦通过（71顶层/104含子项，零fail/skip），该提交的[CI42034435896](https://github.com/DDSRem-Dev/Shikigami/actions/runs/42034435896)已completed/success，57/57全通过，macOS arm64原失败及新增回归已核对日志；单轮成功按其来源记录，稳定性继续用完整矩阵复验。原42项验收登记、平台矩阵、测试预算和断言未削弱；本轮未发布新包。

**追加多轮稳定性验证（2026-11-05起）：** 用户要求再次触发完整CI。冻结生产/测试源码`886de184ad11f35a7b6879c2ca1b9d2dbab27a53`，计划追加3轮完整矩阵，逐attempt要求57个job全success，并核对四平台各7个必需E2E/重型集成用例实际pass。首轮CI42040152398 attempt2已完成56/57，唯一WSL2产品测试的端口较早截止用例失败，不把本轮计入成功；发现并修复共享准入错误分类两次读取ctx.Err导致DeadlineExceeded误判Canceled的竞态，修后FILE/PORT较早截止真实wire各50轮race、server/架构整包race及vet通过，修复提交`e95ff1c`的CI42063751769 attempt1与attempt2连续两轮均57/57通过（含WSL原失败项及新回归），每轮四平台28条加WSL7条必需记录与下游产物ID均核验；随后文档提交`d71a235`触发的第3轮CI42079305988 attempt1最终56/57，唯一macOS Intel CLI测试超时及失败清理竞态，本轮不计成功；已修正PTY测试输出消费与cancel/join顺序并通过最终200次定向race、CLI/架构整包race及vet，生产代码及原8秒期限未改；`c96abf5`的CI42085722699已57/57通过，四平台原CLI回归共16条、必需集成/E2E35条及产物来源均核验；随后统一补齐相邻CLI智能首次运行/目录/同步及UI取消/交接测试的同类夹具缺口，本地定向与整包race/vet通过，保持各自5/6/8秒期限。最终基线`bfdefc4`的CI42092895871 attempt1为56项成功、1项macOS arm64维护因GitHub runner容量不足而未启动取消，明确不计完整成功；同源码attempt2与attempt3已连续两轮57/57完整通过，每轮CLI16条、必需集成/E2E35条、WSL各阶段及四下游产物来源均核验；文档提交`6a86188`触发的第3轮CI42126955938最终56/57，唯一新的macOS Intel CLI登录等待父截止边界失败（原PTY回归仍通过），本轮不计成功；已统一状态RPC与定时等待的截止处理并保留最后已接受状态，原350ms/15秒期限及实际RPC错误边界不变，18分支各20轮race共360次通过；独立复核还修正终端附加日志期间取消覆盖既有鉴权拒绝，真实IPC/PTY六场景各10轮race共60次通过。最终CLI/客户端/架构整包race及vet通过，新源码完整连续轮次另记。补修提交0642f71的CI42132144143随后被新漏洞公告GO-2026-6865阻断（compress1.18.7，修复版1.20.2），已最小升级至1.20.2，根module/analyzer同标准扫描及上游溢出/解码限额race通过；该轮8成功、5失败、12组跳过不计完整通过，WSL仅preflight完成且具体失败原因未公开，升级后全仓build/vet、节点/客户端/CLI/架构race及模块校验通过，远端矩阵重新核验。依赖修复87fa715的CI42133128357又遇模块代理HTTP503、WSL准备阶段失败及macOS Intel预检15分钟总预算耗尽；已新增三次有界模块下载并将预检任务总预算调整为30分钟，原测试/扫描/warm阶段期限与断言保持；四条脚本路径、actionlint、83工具及30平台测试通过，本轮失败仍保留，最终CI基线重新累计完整轮次。最终CI基线fd78178的CI42135628202 attempt1与attempt2已连续两轮57/57完整通过，每轮四平台144条关键CLI、35条必需集成/E2E、WSL13阶段/cleanup及四下游产物来源均核验；文档提交b7c51bd触发第3轮CI42174388098，macOS Intel真实节点控制面恢复测试出现机器身份一致性断言失败（10.96秒、未超过原45秒），CLI144条回归仍通过；本轮最终56/57、不计成功；真实SDK屏障确认旧Self在URL清空后提前完成并授权的竞态，已收紧为已知旧Self须新注册映射到位，保留原机器身份/取消/期限及真实待登录URL复用。九项各10轮race、取消双场景各10轮、最终node整包178条及客户端/CLI/架构race与vet通过，原CI合并断言具体分支仍未确证；前两轮保留为历史证据，新基线连续完整轮次另记。节点修复09adb4a的CI42181018164 attempt1与attempt2已连续两轮57/57完整通过，每轮CLI144条、node40条、必需集成/E2E35条、WSL13阶段/cleanup及四下游产物来源五组均核验；本次仅同步文档，触发相同生产/测试/依赖/CI配置的第3轮完整复验，结果待实际完成后另报。三项原不稳定真实节点用例各50轮race已通过（150 pass、0 fail/skip，883.157秒），590个源码输入哈希保持，内部期限未改；完整CI结果单独记录。本轮不扩大原42项验收或发行声明。

<a id="support-closeout-20261105"></a>
## 支持声明闭环（2026-11-05）

本批完成原路线图最后一项：将功能、平台、角色、证据来源、未支持范围及发行版本统一到[18 当前支持声明](18-release-readiness.md#current-support)。原42项要求文字和顺序保持，Phase 5为6/6，总计42/42。认证生命周期和长稳/资源/性能沿用此前用户确认，不追加或编造外部实测。

源码基线为`0727b3b4cc0f1ccdc0f7a9ec932838de9ea09075`；仓库已有`b948664`的CI41269137757、57/57成功记录，本轮未重新查询或运行该CI。公开安装入口仍固定alpha.2；未创建tag、构建新包或发布。新版本的同源流水线、归档、安装及渠道验收在发布时单独记录。

修正README与设计/验收索引中的旧进度描述；18和26中的旧平台矩阵明确为历史批次，原失败、源码范围和结果保留。macOS客户端、Linux daemon、WSL2 Linux文件系统与systemd的角色边界独立列明；配置快照不宣称恢复文件或Claude历史，真实Claude、物理重启及生产heap/object归因继续披露未实测。

已完成只读源码与帮助/能力核对，`go test ./test/architecture`通过（退出码0）。整批仅文档变更，不修改运行时认证、测试判定或Formula，不重跑12小时/外部环境测试。 文档核验：744个本地链接目标（含516个锚点引用）全部有效；原42项要求文字/顺序保持且全部勾选，08/15/16当前摘要逐字一致，变更仅14份Markdown，`git diff --check`通过。七类架构复核见[16 本批记录](16-architecture-conformance.md#support-closeout-20261105)。

**认证生命周期关闭（2026-11-01）：** 用户确认完整 tsnet 认证生命周期测试已通过。本记录关闭 Phase 0 对应复合门槛，保留 23 中的历史失败、SDK 可观测性边界和脱敏证据范围；不把该结论扩展为真实 Claude 登录或物理主机重启/重新登录验收。

**扩大范围复查（2026-10-31）：** 继续检查Mac全HOME及可读系统/临时目录、VPS完整可持久文件系统，额外删除Mac377项测试残留（98,262,156文件字节，含358个失效tmux测试socket）和VPS旧测试服务drop-in目录/配置（2项154字节）。VPS复扫项目名/已知测试标识无命中、无遍历错误，进程/账户/服务/容器挂载及cron无项目残留；本轮SSH连接和socket已关闭移除。Mac部分TCC、废纸篓及root/系统私有区域无权限，不能宣称全盘零残留；源码、发布签名备份和其他项目保持。39/42不变，详见[全机复查](24-public-package-compatibility.md#whole-host-cleanup-audit-20261031)。

**历史CI状态（2026-10-31，后续成功记录见下）：** 最终源码8fdfe25的[CI41197195478](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41197195478)尚未全绿：Linux arm64在原生安装/服务生命周期/升级维护阶段运行60秒后失败，内部具体步骤因安全artifact读取TLS超时尚未核实；不能套用上一轮13bcd0a的useradd超时结论。其他仍在运行的任务不计通过，最终支持门槛继续保留。本机相关race与实际Mac/VPS安装矩阵各自范围的通过不等同整轮远端CI成功。


**本批 CI 修复与耗时收敛（2026-11-01，已核验）：** GitHub Actions run [41269137757](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41269137757) 对提交 `b948664` 已完成并以 `success` 结束，57/57 个任务通过，覆盖四平台 preflight、单元/集成/E2E、四目标 Homebrew、WSL2、terminal 与 maintenance。原生 preflight 的失败根因是 workflow 调用了不存在的 `scripts/test-platform-tools.py`，现改用仓库实际入口 `scripts/test-platform-ci.py`；集成与 E2E 在 preflight 后直接并行，移除不消费的中间 artifact 和 compile 阶段无写入来源的冗余缓存恢复，运行时依赖仅在缺失时安装并启用 apt 重试，同分支并发取消避免旧提交占用 runner。terminal job 也加入 tmux/unzip/ip/openssl/dbus-user-session 的缺失检查、macOS Homebrew 安装及 Linux apt 有界重试，实际 Linux/macOS terminal 均通过。WSL2 保留完整产品/系统维护验收，本轮约 20 分 50 秒；整轮从 03:25:47 到 04:03:25 UTC 约 37 分 38 秒，受 WSL2 与托管 runner 排队影响，不能保证整轮低于 20 分钟，未削弱覆盖范围。另将 WSL 上偶发的 120ms Connect 超时夹具放宽为 500ms（仍早于服务端 5s 预算），修复 `internal/server` 的跨平台时序脆弱性。扫描后删除未被 workflow、Makefile、文档或测试引用的 `scripts/compare-memory-windows.py`、`scripts/test-install-space.py` 和 `scripts/test-schema-upgrade.sh`。按架构基线复核目录依赖、RPC/授权、状态原子性、进程生命周期、文件/终端语义及 CLI 合约，未发现生产边界偏差；本地 `actionlint`、平台工具 30 项、脚本工具 83 项、race 专项及 `git diff --check` 通过。

**最近已核验CI（本批修复前）：** 文档提交3b2d360的[CI41131301002](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41131301002)已completed/success、13/13通过，实际32分43秒。代码提交045b463的[CI41125907702](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41125907702)也已13/13通过，五平台各730文件源码摘要、必需阶段及清理已独立核验；其整轮46分51秒→36分09秒的性能比较仍按冷暖缓存/并行/排队边界记录于[26](26-native-platform-acceptance.md#ci-045-final-20261031)。下述既有CI不代表本批新修改已通过远端CI；本批相关本地race已通过，未重跑12小时。

以下是按各批源码范围保留的历史记录，当前状态以本摘要和原条目的最新部分进展为准。历史批次中要求真实Claude账号的待办仅表示当时计划；当前范围以[11验收范围](11-automated-installation.md#claude-acceptance-scope-20261031)为准，不再要求用户提供Claude登录。

**前序认证与平台基线（2026-10-31，36/42）：** 原生四目标和实际WSL2的完整产品/终端/安装维护矩阵已实现，17项工具检查、actionlint与语法检查通过，首轮CI41090262712与42eb93e轮已有真实失败，完整平台尚未通过，详见[26](26-native-platform-acceptance.md)。认证状态的两处确定缺口已修复：显式Self.Expired不受BackendState限制；非空AuthURL使旧应用授权退役，而单纯控制失联保持已有有效peer通道。修前红、针对race34.610秒、node整包40项无skip/race322.712秒及vet通过，独立授权生命周期审查通过。官方m6候选两端已构建核验；Mac首轮登记后触发LOGIN_EXPIRED，第二轮两端实际online，当前不计完整认证通过，见[23](23-tsnet-external-acceptance.md#独立认证环境重建与状态修复2026-10-31)。VPS进程更新后另一处连接停在REMOTE_TRUST_OR_PROTOCOL_REJECTED，当前保留失败并调查。保护两项c4长测及预约key到期任务；专用新认证状态有后续依赖，暂保留而非提前清理。

**上批收尾：36/42，剩余6项（2026-10-30）。** 四目标原生Homebrew已验；Mac/VPS最终10阶段矩阵157.31秒、两类8MiB真实强杀恢复、条件竞争、后台冲突/TUI和撤A保B通过，操作系统账户/目录/转发清理已独立核验。结合公开包版本/既有ACK专项与[CI41047781448](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41047781448)十任务全成功，新增关闭Phase0恢复/版本/多设备、Phase4多设备体验、Phase5审查/扫描/关键分支三条；连同本批Homebrew共减少四条门槛。原42条文字/顺序保留，详情及失败记录见[25](25-multihost-acceptance.md)。完整认证、主机生命周期/外部身份升级、其余原生平台、资源性能长稳及最终支持声明六项继续待验。以下较早批次计数是当时记录，不代表当前进度。

**环境清理收尾（2026-10-30）：** 在此前专项清理之外，补充删除811项已确权历史临时对象；本轮closeout、归属审计目录及一次性清理工具也已删除，原始日志/报告未归档进docs。124项来源不足的历史对象及1项未确权且有VM缓存引用的脚本保留，不声称所有旧临时文件均已清空。8个受保护进程身份、运行中的长测容器、expiry输入hash/预约和代理连接复核保持；正式签名备份保留。当前源码提交cc0bb7e的[CI41051011635](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41051011635)十任务全成功。仍为36/42；两项12h与10月31日15:56的独立到期测试尚待实际结果，详见[清理边界](25-multihost-acceptance.md#历史临时材料最终清理)。

**此前阶段记录：双主机业务矩阵通过、OS清理尚未完成（2026-10-30）：** 第12轮10阶段/157.31秒全部通过，两helper及controller清理通过；源码/部署hash和737条事件独立核验完成。最终OS账户、目录、转发回收正在按精确所有权执行；完成前暂保持33/42，不提前勾选。范围与失败记录见[25](25-multihost-acceptance.md)。

**有限故障矩阵继续与清理预检（2026-10-30，33/42）：** 前八阶段已有通过步骤，整轮仍待；第九轮单次只读恢复状态查询超时，已在原60秒总限内先看journal再重观察；第十轮碰撞人工慢桥仍超过默认5秒且A映射归档失败，helper退出通过，最终清理预检因此正确拒绝，未删除本轮runtime。v6仅碰撞专项设显式90秒诊断前端预算，真实IPC测试确认默认5秒、专用90秒、原前端仍5秒、显式1秒及更早200ms父截止；整包race2.778秒和两平台构建通过。普通CLI/产品预算不改，不将此专项外推为默认CLI慢链路性能。当前表和历史进度歧义已修正；七类独立静态复核无新增确定生产偏差。

**双主机矩阵后半部进度（2026-10-30）：** 两种SIGKILL、条件竞争、离线冲突、旧generation拒绝、KEEP_BOTH及真实Sync界面已有实际通过步骤；整轮仍未通过，33/42不变。第七轮纠正agent旧附件所有权拒绝的错误预期，第八轮碰撞夹具的8MiB人工慢下载超过既有5秒父截止；仅碰撞数据改1MiB并在注入同名文件后解除限速，两类硬崩溃仍8MiB，失败立即呈报、无写重放。完整记录见25。另将07中过时的发行状态明确为已有公开alpha.2预发布、无稳定版。

**双主机恢复与夹具收敛（2026-10-30，33/42保持）：** 第六轮普通上传SIGKILL后完整恢复及B终端/TCP连续性通过；冲突解决强杀后过早再次Resolve返回Canceled，失败保留。夹具现等新worker同mapping revision的active/conflicted及恢复journal条件，再判断已恢复；不重试Canceled写请求。独立review另发现上传Send EOF可能遮蔽条件拒绝，helper v5复用既有1秒有界终态读取，四个旧行为反证失败、整包race2.775秒及两平台构建通过。67项Python工具测试通过。清理历史dist候选说明导致的13处链接已改为明确历史清理说明；没有将旧文档归档回docs。整轮与最终CI仍待。

**取消错误修复与条件提交工具补齐（2026-10-30）：** CI41039024257捕获请求已取消时发送层偶发返回EOF；共享连接层现保留Canceled/DeadlineExceeded，正常显式关闭仍EOF。确定性12分支先红后绿，connection整包race2.482秒、启动IPC专项race1.532秒及vet通过。双主机前五轮失败保留；条件文件原语改用独立认证TLS/H2且核对同一daemon，普通同步仍走agent和插件，前端raw文件代理限制不变。新两平台产品构建固定为local.multihost.3，完整矩阵和最终CI仍待，不重启旧c4长测。33/42保持，详见[25](25-multihost-acceptance.md)。

**双主机验收发现并修复终端接管提示（2026-10-30，整轮仍待）：** Mac＋VPS独立普通用户/状态/TLS测试已启动。真实CLI暴露接管后仍显示observer，现等待有序CONTROL_CHANGED确认后报告controller，保留输入代次与提前输入丢弃；超时回调join及缓存释放同步补齐。客户端、CLI、UI和架构race/vet已通过；修复后真实观察/接管提示与正常detach均通过。重绘节流和Dashboard过早按键两处夹具问题单独修正，失败不改记成功；其余并发/冲突/崩溃矩阵仍待，保持33/42。详见[25](25-multihost-acceptance.md)。

**Homebrew 四目标原生渠道验收完成（2026-10-30）：** CI [41037444120](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41037444120) 的 macOS/Linux × arm64/amd64 四个真实渠道任务全部成功，公开alpha.2的 tap/install/test/reinstall/test/uninstall/untap、完整bundle及清理前后环境一致已独立核验。关闭Phase 5签名/分发原条目，当前33/42、9项待完成；完整产品平台、主机重启、Claude与长期资源条件仍独立保留。用户Mac未运行brew或更改Xcode/CLT；具体范围见[22](22-homebrew-distribution.md#四目标原生-ci验收)。

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

扫描修复证据：`/var/folders/1n/q373cln10fqb0gl9z1ds5d8m0000gn/T/shiki-scanner-ipv4-device-20261029-1e5si8x0/summary.json`；原Ubuntu失败日志：`/var/folders/1n/q373cln10fqb0gl9z1ds5d8m0000gn/T/shiki-ci-40859588149-3chdt4cn/ubuntu.log`。

**Actions自动签名配置（2026-10-29）：** 用户选择Actions签名并授权新建专用Ed25519密钥与本机私有备份。公钥`configs/release-signing.pub`及SPKI SHA-256指纹`e21f6dd671ea5042c43c492b759b933dda73985f89a823b43c651ff7c787c17d`已确定；GitHub `release-signing`环境已实际配置并读回复核：DDSRem必需审批、仅master、禁管理员绕过，私钥Secret与公钥Variable均已设置。备份目录0700/私钥0600，密钥对已本机验证。手动工作流严格选择同仓库成功CI push/master的精确commit，四目标无密钥构建，可信workflow提交预编译verifier，审批后才单步持钥签名、公钥匹配和逐包验签；仅上传Actions artifact，不创建tag/Release。实际CI运行40859588149在未完整成功时被新gate拒绝且未写来源输出；5项Python行为测试、actionlint、实际签名shell的四目标微型归档/临时测试密钥/真实Go verifier通过，产出22文件；公钥及私钥不匹配两负例均失败；另只读验证旧.5两真实归档通过，不追改旧源码范围。七类独立架构复核无新增确定性偏差；新增逻辑均属发布工具，产品边界/RPC/授权/状态/生命周期/I/O/安装契约未变。远端签名流程尚未执行，审批/发布和原平台/资源门槛保持；原42条及29勾选不变。操作、备份、信任及轮换/撤销限制见[发布准备](./18-release-readiness.md#github-actions-自动构建签名与发布)。

本批本地签名编排证据：`/var/folders/1n/q373cln10fqb0gl9z1ds5d8m0000gn/T/shiki-sign-actions-smoke-vp5exv28/summary.json`；环境配置与公钥身份记录：`/tmp/shiki-release-signing-setup.json`（不含私钥内容）。

**预发布版本与提交审阅（2026-10-29）：** 用户选择`v0.0.1-alpha.1`。已整理首次实现提交范围：CLI/agent/daemon、sync/port独立插件与SDK、协议、状态/认证/安装、测试工具、CI及docs；Git候选共652文件，bin/dist/工具缓存/临时证据不纳入。按用户要求，CI中四处Action引用改为GitHub官方latest稳定Release标签：checkout v7.0.1、setup-go v7.0.0（2026-10-29查询）；已核对两者action.yml的Node24运行时及当前使用参数，工作流仍为push/pull_request和只读权限，不使用提交哈希引用。现有CI配置经actionlint检查通过，涵盖macOS/Linux检查与四目标未签名打包；CI仍使用独立`v0.0.0-ci`，不冒充预发布产物。完整首次提交检查另修正7份Python脚本的无效尾部空白（AST一致），以.gitattributes将两份终端raw样本标为binary，样本字节/hash保持；本批仅版本记录和提交准备，用户已授权将审阅后的改动直接commit到`master`并推送触发CI；不创建发布分支、tag或Release。提交与执行状态以master实际提交历史和GitHub CI运行记录为准，授权或本地检查不算远端验收通过。七类复核确认生产结构、RPC/授权、状态/幂等、生命周期、I/O语义、安装契约均未变，版本命名不增加能力或平台声明；原42条/29勾选及资源/外部门槛保持。

**首发兼容基线确认（2026-10-29）：** 用户确认项目从未通过任何渠道对外发布可用版本；上一轮GitHub只读检查亦显示Release和tag均为空。历史正式版本迁移在本次首发没有验收对象，记为“不适用：无历史发行”，不是测试通过；旧批次中的“等待历史正式产物”由本说明更新。开发A/B版本的schema迁移、未来schema拒绝、协议/插件版本边界、升级失败恢复、回滚及数据/认证保留要求继续适用；首次对外发布后应保存固定源码、产物和状态格式，作为后续真实升级基线。七类一致性复核确认本批仅澄清验收对象，目录依赖、RPC/授权、状态、生命周期、文件/终端/网络、CLI/安装和能力声明无变更；不重跑行为测试。原42条及29勾选保持，物理设备、账号、原生部署、资源与发布门槛继续待验。用户已确定首个预发布版本为`v0.0.1-alpha.1`；本批未提交、推送、创建tag或发布。

## 历史验收摘要与原始证据

按用户要求，源码目录中的独立 Markdown 验收报告已删除，未归档或迁移正文。
原有验收摘要、冻结来源、失败/skip 与待验门槛仍保留在本页及 08/16/17/18；
原报告链接现指向已有摘要段落，不表示这些摘要等同完整原始日志。
按用户2026-10-31清理授权，原始临时JSON、日志、冻结源码及测试归档已删除；各批次路径仅表示当时来源，脱敏结论保留在docs，公开发行资产不受影响。临时目录不是永久证据仓库，
若原始材料不可取得，必须重新核验对应结论，不得只凭旧勾选宣称当前版本通过。
既有 dist 候选是固定交付快照，其中的源码清单和说明可能保留当时路径，不能据此访问已删除报告。

更新：2026-10-29。目标保持为路线图中的完整 Phase 0–5；本文件记录当前证据，不缩减首版范围。逐项完成状态及复合条目剩余门槛已同步 [08 路线图](./08-roadmap.md)，架构复核详见 [16 一致性检查](./16-architecture-conformance.md)；后续每批必须同步三份文件。实现从仅含设计文档的干净工作树开始，实际 module 确认为 `github.com/DDSRem-Dev/Shikigami`。

<!-- active-parallel-work:start -->
**当前工作与验收边界（2026-11-05）：** 原42项已关闭。当前源码支持范围及证据统一见[18支持声明](18-release-readiness.md#current-support)。用户确认与独立实测分别记录；未实测平台/账号/重启、资源归因和后续功能边界保留。下一发行版的tag、构建签名、实际包验收与渠道更新尚未在本轮执行；不能将当前源码结论追认为alpha.2已包含所有修复。
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

**最近完成批次：恢复修复候选包 .5（2026-10-29）。** 同一739文件冻结源码的macOS/Linux arm64未签名归档已首轮构建、验收并交付至dist/local-20261029.5；541份Go/proto/全部module与上批738已验输入逐项一致，包含四份恢复修复。两平台各52实际命令、14成员/六程序/双插件及全部既有诊断/确认/配置保护检查通过；实际归档helper首次与重复stage-only各校验14成员和私有review，无current/receipt/config/state/安装links，标准服务文件前后保持。两个agent均正常退出/reap，原生PID/runtime及精确Linux容器清理独立核验；源/工具/交付哈希与七类复核完成。08/15/16同步，原42条及29/42保持；未签名、Commit/BuiltAt unknown，暂存不是激活，.4历史及697长跑范围独立。见候选验收与历史交付说明（临时候选包与说明已于2026-10-30清理；验收摘要保留于本文档集）。

<a id="evidence-recovery-conformance-validation"></a>

**最近完成批次：同步停止重试与安装失败恢复（2026-10-29）。** sync client Stop改为单次清理owner与各调用者独立截止，资源未排空不再重试假成功，关闭错误及未知提交源/日志保留；安装Stage补metadata/父目录同步及已发布目标重试屏障，Rollback早期失败仅恢复可信已提交选择，随机服务临时文件允许中断重试且保留旧现场。停止7条与安装恢复13条父/子同测红绿通过；Linux实际syscall证明原14/0→17/17同步，9精确EIO及新进程恢复通过。738同源双平台六相关包race/vet及真实sync子进程通过，各152父/子pass，1/2 skip不计通过。原生应用0但采集器因缺PID清单退出1，保留失败并限定清理证据为fixture Close/Wait；Linux应用/采集器0、精确CID及副本已清理。七类复核与独立审查完成，08/15/16同步，恢复Phase2项至29/42、13项待验，原42要求保持。.4归档和697长跑不追认本批修改。见合并验收。

<a id="evidence-terminal-analyzer-remediation"></a>

<a id="evidence-local-candidate-20261029-4"></a>

<a id="evidence-node-status-validation"></a>

**最近完成批次：分析器依赖、节点状态诊断与 .4 交付（2026-10-29）。** 独立analyzer的x/sys已最小升级0.44.0，同库两目标复扫零finding，725同源双平台各16终端捕获及清理通过。修复只读状态误要求node.login.v1及doctor被不可用清单阻断：实际运行端NODE_DISABLED/NODE_STATE_UNAVAILABLE、未知清单null和正确Dashboard提示均保留，登录能力及UID/实例/截止校验仍严格。有效同测红绿与729源双平台四包race/vet通过，各196父/子pass、无skip；同537份Go/proto/全部module输入的729源.4实际归档两平台首次通过，各50命令、两agent正常退出及精确清理，12文件已交付。七类复核及独立核验完成，08/15/16同步，恢复诊断与Dashboard两项，原42条文字/顺序不变、当前29/42、13项待验。未签名、Commit/BuiltAt为unknown；.3历史错误、725/729不同证据和697独立长跑范围分别保留。见依赖修复、节点诊断、实际归档验收及历史交付说明（临时候选包与说明已于2026-10-30清理；验收摘要保留于本文档集）。

<a id="evidence-security-validation"></a>

<a id="evidence-terminal-analyzer-security-validation"></a>

<a id="evidence-port-handshake-branch-validation"></a>

**最近完成批次：端口握手分支与独立分析器依赖（2026-10-29）。** 已补8个Send-EOF/非配额ResourceExhausted握手及已建立流对照分支，以受控公开Connect transport驱动真实Worker/TCP，验证既有payload/同监听/槽回收/无重放和正常清理；724同源双平台完整client包race/vet均首次通过，各7顶层+8分支、无skip，原5秒预算保持。嵌套VT analyzer已新增独立CI扫描，固定v1.1.4两arm64静态目标均无受影响已导入包，但保留GO-2026-5024 Windows模块公告；原生精确CI命令亦退出0，不称零漏洞或远端CI已跑。源后置/精确Linux清理及七类复核完成，08/15/16同步、原42条及29/42保持。没有生产/协议/依赖升级或产物重建，697长跑独立继续。见端口分支与独立依赖扫描。

<a id="evidence-terminal-ci-validation"></a>

**最近完成批次：CI终端语义回归入口（2026-10-29）。** 已将原有test-terminal-state.sh接入macOS/Linux默认CI独立步骤，修复缺analyzer时终端语义专项一直跳过的入口遗漏；只增加调度，不改生产、测试断言、模块或原90秒/60秒预算。同一719基础源加唯一环境wrapper的720源，逐字CI run block在macOS/Linux各执行一次：实际专项2.18/0.82秒通过，各16捕获、无skip；原生六PID/runtime独立缺席，Linux应用/collector退出0、精确CID缺席且源码副本正常删除。原/派生源码后置hash与七类复核通过，08/15/16同步；远端GitHub Actions尚未实际执行，原42条及29/42保持，不重建.3或改变697长跑。见CI终端验收。

<a id="evidence-local-candidate-20261029-3"></a>

**最近完成批次：本地候选包 .3 交付（2026-10-29）。** 同一717文件冻结源码已通过macOS/Linux arm64既有make release和实际归档验证，交付至独占dist/local-20261029.3；530份Go/proto/module与已验719完全一致，包含诊断、操作确认和配置恢复修复。两侧14成员、六程序、双插件清单/hash/pin、startup/list/doctor、缺--yes零启动和坏配置原件/备份保留均通过；正常Shutdown/reap、源事后hash和精确Linux清理已核验，无失败重跑或强制清理。七类架构复核完成，08/15/16同步，原42条及29/42不变。未签名、Commit/BuiltAt为unknown，不追认697长跑、外部连接或正式发行；空服务器诊断不证明实际路由。见候选报告及历史交付说明（临时候选包与说明已于2026-10-30清理；验收摘要保留于本文档集）。

<a id="evidence-diagnostic-confirmation-validation"></a>

**最近完成批次：诊断与操作确认（2026-10-29）。** 已补06要求的实际路由尝试列表和损坏配置恢复说明，以及05要求的stop/restart非交互--yes确认；运行字段有界、按代际发布且不持久化，原回退授权/停止实现/保存语义保持。三项有效同测红绿及独立审查完成；718同源双平台受影响race/vet各179顶层通过，安装专用skip和无测试包单列。现有4处E2E调用仅追加--yes，719源仅两测试文件和环境脚本不同；双平台两条真实CLI生命周期/端口恢复E2E及tagged vet均通过，无skip。首轮旧Go环境前置失败保留，实际验收仅修PATH；源后置/正常Linux清理和七类复核通过，08/15/16同步。恢复三项，原42条文字/顺序不变、当前29/42；707归档与697长跑不追认这些后续变更。见合并验收。

**历史启动与中期观测：新Linux默认12小时（2026-10-29，最终结果见文首）。** 已验宿主/观察修复和最终collector组成独立697源，于10:40:43启动；collector33067、精确容器232b672a…65f3，四初始worker/UID1000/只读源码已核验。17:46只读检查截至17:45:47已2474轮、7小时4分2秒且原容器仍运行；默认凭据12小时，最早22:41自然到期，恢复尾部下界23:11、外层截止23:55（Asia/Shanghai）。整轮恢复、至少30分钟尾部、资源及最终清理仍待验；原Linux失败保留。后续启动策略、诊断/确认、节点状态、同步停止与安装恢复修复及.5候选不在该冻结源中。见独立运行。

<a id="evidence-default-tools-validation"></a>

**最近完成批次：默认入口补齐采集器回归（2026-10-29）。** 发现已有13项Python采集器关键回归未进入make test及CI，已新增test-tools并由test依赖；普通用户/Python3.10+前置避免root跳过生命周期测试却返回成功。同一最终四文件在macOS/Linux各13项通过、无skip；Linux实际root调用退出2且未运行suite，make -n test确认原三组Go入口保持。只改Makefile及文档，采集器/测试断言/生产输入不变；七类架构复核完成，08/15/16同步。未声称远端CI已运行，不重建707候选或变更697长跑；原42条及29/42保持。见默认工具检查。

<a id="evidence-local-candidate-20261029"></a>

**最近完成批次：本地开发候选交付（2026-10-29）。** 同一707文件冻结源码已通过既有make release构建macOS/Linux arm64的v0.0.0-local.20261029.2归档，并交付至独占dist/local-20261029.2。两侧14个归档成员、六二进制版本、双插件清单/hash及校验pin均已核验；实际解包CLI/agent的隔离startup JSON、新文本说明、停止后不自启、正常Shutdown/reap与清理通过，Linux为UID1000且精确容器已删除。相对已验启动策略产品输入仅一句CLI说明变化，本批已实际覆盖。完整源清单随包提供，未签名，Commit/BuiltAt为unknown，不作为正式发行或发布者认证。七类架构复核及08/15/16同步完成；原42条及29/42不变。697源12小时运行独立保持，签名/分发、资源及外部平台门槛仍待。见候选包报告与历史交付说明（临时候选包与说明已于2026-10-30清理；验收摘要保留于本文档集）。

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

## 历史阶段进度（截至2026-10-29）

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

以下表格及紧随两段保留首次官方tsnet验收前的历史状态；其中“尚未/仍需”不表示当前结论。2026-10-30官方P2P/DERP、一次性key复用/独立撤销、最终agent节点撤销恢复及远端CI新证据以文首和23为准。

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


<a id="evidence-default-linux-final-20261029"></a>
## 原697源Linux默认12小时最终核验（2026-10-29）

原运行于北京时间10:40:43启动，23:11:50.363完成采集，应用/collector均退出0。实际工作`12h30m3.603119998s`、4368轮、740检查点；四角色各自然替换一次，全部在原12小时到期后的两分钟恢复预算内；恢复尾部`1803.013336668s`、30个恢复后检查点。持有TCP在原port到期窗口结束、没有重放，两侧各两条新socket与原socket分离。原Linux578源失败记录不覆盖或改写。

预先准备的只读终态核验器`/tmp/shiki-default-linux-final-review-20261029/verify.py`现已实际执行，输出`final-review.json`为`verified`（SHA-256 `6721253be580748bfc76c34bde6def473b410746d26f55a3671c4060bcf7208e`）。原始证据目录`/tmp/shiki-default-rotation-linux-current-20261029`；697源manifest固定SHA-256为`660dc6e93b3ce1300d084f02665a4e9dcf88178d7e6fd044a2ce05d366e7cca2`。核验器检查保留697源、16份冻结契约、日志/JSONL完整事件多重集、12个原/新进程身份、四角色退役/替换时序、TCP/恢复尾部及终态；独立审阅再次核对证据/日志/事件/核验器/manifest哈希。

11个所属PID的fixture清理明确成功，collector无强制清理、无中断、无错误；源码后置检查无变更且collector快照移除。Docker全ID清单及精确CID inspect确认`232b672a061cf26edcd4432df11dee3ef1960d02e38d0cb35bd1555110ec65f3`已不存在。未在macOS探测同号Linux PID，未读取私有失败runtime，未重新启动或清理任何运行中容器。

边界：该运行是原697冻结源的Linux arm64普通构建、真实默认12小时插件凭据自然轮换证据，不代表当前alpha组合、外部tsnet/Claude、全平台或无泄漏。FD+4/测试goroutine+6按原夹具在checkpoint前预算内连续三次达标，不能外推全时FD上限、生产goroutine、连续RSS/CPU限制或所有进程优雅退出；没有宣称四角色严格同时退役。完整已有资源记录仍可离线分析，之后应核对697到公开alpha执行输入变化及具体未覆盖路径，再决定针对性补验，不按文件数量或旧源成功直接重跑12小时。

本批仅只读核验和文档同步，无生产/测试/依赖/配置变更。七类架构与文档复核无新增偏差；08/15/16及18/19当前状态同步，原42条文字/顺序和31勾选不变。长期资源复合项、外部账号/原生部署/物理平台及其他发行门槛仍待；Homebrew和隔离测试密钥轮换/撤销演练可独立推进，本批没有启动这些新任务。


<a id="evidence-alpha2-distribution-preparation-20261030"></a>
## 资源报告、密钥演练、GPL与Homebrew首发准备（2026-10-30）

本批并行完成[完整资源与源码差异审查](20-long-run-resource-review.md)、[临时测试密钥12步演练](21-release-key-rotation.md)与[Homebrew渠道准备](22-homebrew-distribution.md)。资源原始5920样本/12代际和全部峰值保留，未确认新生产保留根；没有启动新短内存或12小时实验。公开alpha.1的生产Go/proto/module在本批保持不变，GPL正文与发行工具变更不追改原697运行范围。

按用户要求，当前项目从MIT改为GPL-3.0-only，根LICENSE采用GNU完整正文，README和设计/构建/发布说明同步；alpha.1固定MIT归档保留。Homebrew首发固定alpha.2，当前不提交alpha.1 Formula。新tag完整CI/签名/公开Release后自动生成并更新master的Formula：四目标归档签名/摘要、实际LICENSE全文白名单一致性、Ruby语法、远端22资产来源及摘要、文件SHA条件更新全部受检查。更新器防版本回退、稳定渠道被预发布替换、同版冲突和无界重试；不新增Secrets或活动workflow。

本地工具整套55项通过8.459秒，随后新增FD消费身份回归后的生成器7项定向通过；8项更新器行为回归覆盖CAS、并发较新版保留、同版幂等/冲突、稳定渠道、远端Git身份及草稿/缺资产拒绝，actionlint通过。临时密钥演练12步及3项回归通过，实际私钥目录清理，不操作真实发行身份。证据包括`/tmp/shiki-postrelease-delivery-20261029/tool-tests.log`、`formula-final-tests.log`及20/21/22列出的各自证据目录。

真实Linux arm64普通用户Homebrew完成安装/test、仅包revision升级/test、重装/test和卸载共7步，均退出0，三次test共6次实际helper暂存；此为alpha.1认证包的兼容fixture，不是Homebrew公开首发或跨产品版本升级。两处实际包管理问题已修：Homebrew staging的工具文件不纳入包，完整14认证成员置于libexec/bundle以避免Homebrew4/7元文件整理移走LICENSE。原始失败均保留，未改helper或放宽hash。最终native公式方法、2次helper暂存与签名篡改拒绝通过，但本机完整brew因macOS27的Xcode/CLT不足前置失败，门槛保持。

七类独立审查未发现剩余确定性生产偏差。原42条文字/顺序及31/42保持；本准备批次提交时尚待实际alpha.2 tag全流程、22公开产物认证及远端tap验收，随后已在[公开验收批次](#evidence-public-alpha2-release-20261030)完成；保留准备阶段范围。macOS brew完整链、真实账号/原生平台/长期资源等门槛继续待验。


<a id="evidence-public-alpha2-release-20261030"></a>
## alpha.2 自动发布、GPL与公开下载验收（2026-10-30）

[v0.0.1-alpha.2](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-alpha.2)的受保护annotated tag固定到`fa35debd9c74d0ad44704e3a7897e0e0c1fcc3f2`。[Release40912019720](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40912019720)完整成功，Ubuntu/macOS CI、四目标打包检查、四目标正式包、签名/独立验签、公式生成/Ruby语法、公开发布和Formula更新全部实际执行。两平台各56项工具回归通过；Ubuntu耗时4.968秒、macOS5.974秒。macOS前两次因托管runner容量不足取消，runner_id为0且steps为空；仅重试该任务，第三次通过，不把基础设施取消改写为测试失败或通过。重复的master CI40911980224为避免重复执行同源检查被取消，不能将其记为成功。

Release于`2026-10-29T17:30:47Z`（北京时间2026-10-30 01:30:47）公开，非草稿预发布，共22资产。随后GitHub Actions bot自动提交`85003591044fca15f82c9c220b2051766f1e47aa`，仅新增`Formula/shikigami.rb`；Formula SHA-256为`53fe01dcc5f9aa1d4c4dc5aab8d9d4ea05013631993835eb1ce9bb4d293f15ea`。版本、四目标URL/hash、签名和GPL-3.0-only标签来自认证产物，没有手改公式；其后从公开下载重新生成的公式与自动提交逐字节一致。GITHUB_TOKEN提交不启动新CI，当前发布job已执行生成/语法检查，另有真实tap验收见[22](22-homebrew-distribution.md)。

22公开资产下载至`dist/releases/v0.0.1-alpha.2/`，由可信仓库verifier与预先固定公钥逐一验证Ed25519、远端大小/hash、归档14成员、全部payload hash、checksums pin、来源和验签回执。四个归档LICENSE全文均与根GNU官方GPLv3正文摘要一致，README明确GPL-3.0-only；旧alpha.1的MIT源码/归档保持冻结。24二进制版本、完整commit、Go1.26.10及OS/arch正确，4个shiki保留的BuiltAt匹配固定运行开始时间`2026-10-29T16:29:06Z`，其余20个被链接器消除，未宣称静态验证了不存在的变量。

本机macOS arm64和普通用户Linux arm64各执行12条version/help命令；macOS client首次/重复stage-only两次，Linux client/daemon首次/重复四次，合计6次实际helper暂存通过。Linux为UID1000、断网、cap-drop ALL、只读根；全部暂存14成员hash、0600 review、显式路径和不激活状态通过，无current/config/state/plugins初始化，现有服务文件不变。输入后置hash、临时解包清理和精确容器缺席独立核验，独占验收镜像标签已移除。此项不等于新归档完整服务升级或全平台验收，旧92cafaf生命周期证据单独保留。

证据目录`/tmp/shiki-postrelease-delivery-20261029/`保存`alpha2-release.json`、`alpha2-run.json`、`alpha2-publish.log`、双平台CI日志及两次runner容量记录；`public-alpha2-acceptance/summary.json`为passed，另有`public-verification.json`、`binary-build-metadata.json`、两平台summary、`container.json`与`cleanup-verification.json`。公开Release及Issues均由无登录curl验证TLS并返回HTTP200。临时证据不是永久存档，不可取得时不能扩大本摘要结论。

本批生产Go/proto/module未改。七类架构复核、原42条文字/顺序及31/42保持；原生macOS完整Homebrew安装仍受Xcode/CLT不足阻止，原生amd64/WSL、真实账号/网络、原生部署/重启登录、物理多设备和当前版本长期资源门槛保留。原697源12小时成功、完整资源报告和隔离密钥演练均已完成，不再列作等待；未重新运行长测或操作生产密钥。


实际远端tap验收补充：`/tmp/shiki-homebrew-alpha2-public-20261030/verified-summary.json`为passed，克隆HEAD精确为8500359，Formula摘要与自动提交相同；Linux aarch64/Homebrew4.6.20/UID1000实际tap、install、test、reinstall、test、uninstall六步均退出0，4次helper暂存、14成员/13payload hash及GPL全文验证通过。未激活服务或生成current/config/state；容器die0/destroy事件与全CID独立inspect缺席通过。此为当前alpha.2真实远端安装链，旧alpha.1 fixture的revision升级仍独立保留，不外推当前发行跨版本升级或原生macOS完整brew通过。


## 完成计划与剩余条件盘点（2026-10-30）

对照08全部42原项、19当前候选矩阵、20资源报告、21演练和22真实tap结果，按阶段复算9/11、7/7、5/6、6/6、3/6、1/6，总计31/42。11项均保留原文与checkbox；区分已有实现/局部证据、可自主补验和用户环境条件。具体分工在08，汇总在19；不是新功能实现或已执行实验。

清理当前计划表中的三类过期状态：alpha.2自动Homebrew已通过而非待发布；697运行及恢复尾部已结束而非仍等待；首发时无历史版本不适用于如今可用的alpha.1/alpha.2组合。17的旧29/42及旧失败规划标注为历史，20中已完成的分发/演练不再写为待推进。没有把这些文案修正当验收新增通过，既有原生macOS工具、真实账号/平台和长期资源条件保持。

<a id="evidence-official-tsnet-20261030"></a>
## 官方tsnet登录、路径与业务流验收（2026-10-30）

完整来源和脱敏矩阵见[23](23-tsnet-external-acceptance.md)。已用重新验签的公开alpha.2在Mac arm64与Debian12原生amd64 VPS普通用户环境完成官方双端登录、两端进程重启身份摘要/地址保持，以及真实P2P和受控DERP上的终端、同步和TCP传输。两条路径均经认证且attempts仅tsnet，4MiB内容读回校验一致；移除Mac单进程DERP开关后重新连接为P2P。没有主机重启或真实Claude使用，业务流不经SSH。

原版登录CLI过早结束、SDK health拒绝遗漏有实测失败记录，现已修复CLI/向导等待和NodeControl分类。一次性key首次成功；原host复用仅30秒超时，修复host官方复验2.233秒明确denied/INVALID_AUTH_KEY；实际交互URL获取后取消返回LOGIN_CANCELED。测试修复二进制与公开alpha.2宿主/插件分开记录，未覆盖发行包。cmd/shiki完整race21.349秒、internal/node完整race157.044秒、专项race12.873秒、相关vet与架构0.415秒通过；七类复核见16。

仅关闭原Phase2首次登录/状态持久化/节点名/P2P-DERP展示一项，当前32/42，阶段9/11、7/7、6/6、6/6、3/6、1/6，原42条文字/顺序不变。独立key过期/撤销、审批/节点撤销、主机重启/登录及其余支持条件继续保留。用户明确确认后已在Chrome移除专用节点；实测暴露已有连接未按peer撤销及时回收、缓存online与附件残留，本地peer生命周期修复已通过相关回归，官方复验仍待，详见23及下节。临时状态保留供后续认证矩阵；强制DERP环境已撤去，主节点正常P2P。旧文档CI40923247799/40924428349成功不替代本次修复的远端CI。


<a id="evidence-peer-revocation-20261030"></a>
## peer撤销后的旧流回收与状态观察预算（2026-10-30）

官方移除专用测试节点后，原alpha.2的新请求超时、旧终端最终取消，但原节点缓存online且远端附件仍残留，不能按最终网络超时记撤销通过。现按06的节点撤销/控制面暂时不可达区分要求修复：入站在固定SDK flow回调捕获peer身份代际，出站在拨号前捕获；登记与状态发布互斥，删除/换钥/过期或删除后重加关闭旧连接。IPN通知仅作刷新提示，另每秒经LocalAPI读取live peer集合并过滤InNetworkMap；控制面暂断、普通health变化或观察失败不当撤销。队列、关闭归属及限制详见[23](23-tsnet-external-acceptance.md#peer撤销修复与复验边界)。

无外部deadline的Dial原可长期占用串行观察许可，阻止后续peer校准；最后将总5秒上限移入共享queryStatus，包含许可等待和LocalAPI查询，保留更早调用者截止。该项修前测试6秒明确失败，修后5秒返回且许可释放。仅补丁末尾两文件变化，未重启或追认之前的全包验证。

源码基于54feb47工作树；最终逐文件摘要为`/tmp/shiki-peer-lifetime-source-final.json`，前一冻结源为`/tmp/shiki-peer-lifetime-source.json`。前一冻结源完整internal/node race226.285秒通过，日志`/tmp/shiki-peer-final-node-race.log`；最后仅node.go及peer_registry_test.go变化，最终相关专项race106.481秒通过、无skip，覆盖真实入站/出站移除、无bus的明确过期、取消/许可、观察失败保留其他peer及5秒自身截止；vet与diff检查通过。日志`/tmp/shiki-peer-status-cap-{red,green}.log`；不能把两个来源合写为最终源码整包race通过。

七类审查见16。只增加本地实现/回归证据，计数保持32/42，原42条文字/顺序不变。官方撤销修复复验尚未通过。前两次Chrome重新认证提交后仍NeedsLogin，失败记录保留，不推断根因；第三次用基于最终生产源的诊断overlay生成fresh key/URL，经Chrome同账户Connect后，SDK报告machineAuthorized=true、authURL=false、authRoutine成功并进入Running，NodeControl online地址100.118.23.89，控制台Connected，证据`/tmp/shiki-tsnet-3vsvzo1b/node-reauthenticated.json`。该次成功随后由clean最终构建的进程恢复、同地址online和应用重连独立核验，详见23；这不代表下述再次移除后的恢复已通过。不持久化URL/凭据，不改公开alpha.2或原签名归档；后续外部结果独立追加。

<a id="evidence-official-revocation-recheck-20261030"></a>
## 官方撤销旧附件回收、认证恢复与控制面失联（2026-10-30）

在上批最终构建上开启主Mac与专用节点两个只读终端，远端client_count稳定为2。Chrome再次移除专用节点后，client_count降为1；row时间记在查询前，故采用首次count1下一条row时间作为回收保守上界并向上取整为13秒；专用终端首次检测exit1在26.809秒，向上取整为27秒。两者从Chrome提交前触发标记计时，包含UI操作和观察间隔，不是控制面撤销精确时延。全部主Mac查询成功、主终端保持至观察完成、tmux始终ready；控制台确认只剩主Mac/VPS两节点，完成观察后正常detach主终端。证据为私有`/tmp/shiki-tsnet-3vsvzo1b/revocation-fixed-observation.json`，源提交字段固定63924ce。最初临时观察器未设置PTY尺寸，两终端在删除前exit1；补齐夹具尺寸后才执行移除，该次准备失败不算产品撤销失败，也不算通过。

被移除节点最初仍显示online，重启后BeginLogin无URL。SDK支持磁盘netmap缓存，但本轮日志的“load netmap from cache:”是加载错误，不能据此确认加载成功或唯一根因。Self.Online反映控制map轮询健康，Running/地址不能单独作为当前控制面在线证据。已修复控制状态与既有peer授权分离；未取得新鲜控制观察的缓存Running不得建立新授权。显式注册须满足当前attempt完成门槛，已有URL继续原注册，取消与完成同锁裁决，登录启动/取消观察共用有界许可。daemon按授权生命周期保留HTTP服务，CLI控制失联时继续真实连接探测，不自动重登录。

本次Touch ID后使用63924ce诊断构建完成官方认证，online地址100.101.136.82、SDK Running且URL清除，Token/原daemon ID/能力重验后P2P ready；这不是candidate4重新登录的证据。随后candidate4 clean构建在原状态目录同地址恢复；仅为Mac CLI/agent，VPS仍candidate3。官方单进程代理仅阻断controlplane.tailscale.com约11.807秒：offline/NODE_CONTROL_UNAVAILABLE、新sessions RPC持续成功、原只读终端存活，放通约2.063秒后online同地址，正常detach退出0。未注入新终端输出，不把进程存活扩大为恢复后新payload验收。另独立场景验证断网期间connect ready且login_id未变化。最初12秒脚本预算早于产品15秒等待截止，另一次把会主动更新revision并退役连接池的connect混入保流测试，两次失败记录保留，均不用于宣称断网保流通过。完整证据与范围见23。

本地验证按冻结源码分层：node完整race310.580秒；随后注册/取消竞争专项82.302秒；最后仅取消观察改为串行queryStatus，6项专项11.242秒通过，各层无skip/race/失败。真实SDK已有URL测试与同key状态机单测是独立证据，不合称同key真实SDK闭环。server/architecture整包race43.386/1.378秒、CLI整包race20.597秒通过，相关vet/diff通过。清单`/tmp/shiki-control-outage-source-final.json`关联前两快照；candidate4/host-build.json记录97份生产/依赖输入构建前后相同，版本v0.0.1-alpha.2-local.tsnet.4、Commit unknown。七类审查见16；本批不覆盖公开alpha.2产物，不关闭完整认证矩阵。

[CI40940419677](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40940419677)精确源码`63924ce4271345bcb1d463e94e59a773d6f29a59`六个job成功：macOS14/Ubuntu24.04的构建、lint、race、E2E、协议漂移及终端屏幕步骤，另四目标未签名包构建/校验。node整包race185.047/35.375秒、架构1.205/1.107秒、E2E239.568/177.592秒。它补充上批最后5秒预算补丁的双平台整包证据，不改写原本地226.285/106.481秒的不同冻结范围，也不覆盖本轮恢复实现。workflow无skipped步骤，但go test非verbose；真实LaunchAgent、systemd活动升级与12小时测试需opt-in，CI未启用，不能声称全套无skip。日志`/tmp/shiki-ci-40940419677-review.log`。


## 最终agent官方撤销恢复与验收夹具修复（2026-10-30）

本批最终agent官方移除、offline诊断、同进程显式认证及应用重新验证已通过，详见[23本轮记录](23-tsnet-external-acceptance.md#最终agent再次撤销与恢复cicli补修2026-10-30)。主Mac与原tmux保持，未重启agent、删除身份目录或使用诊断overlay。CLI在pending控制离线阶段提前退出已单独修复；不将后续GetStatus取URL恢复倒推为旧CLI成功。CI40998046799整体failure及其macOS未执行/跳过边界保留；凭据扫描夹具改为SDK关闭后检查最终store和全部文件，三次定向race13.190秒通过，不降低断言。CLI真实IPC先红后绿，最终完整包race40项/22.312秒通过，无skip或race；源码与日志摘要为`/tmp/shiki-ci-and-cli-pending-verification.json`。完整认证矩阵、平台/主机/长期条件保持待验，32/42不变。


## 未使用key撤销与自然到期调度（2026-10-30）

正式生成后未消费的key撤销已有独立外部证据：全新节点明确INVALID_AUTH_KEY，1.932秒；进程退出0、密钥文件删除。另一枚独立key最短1天有效期，仍未提交注册，真实自然到期不能由已撤销或消费key代替。candidate5 CLI+candidate4 agent来源、官方操作/清理及调度见[23](23-tsnet-external-acceptance.md#未使用auth-key撤销与独立自然到期准备2026-10-30)。精确99bee22六job CI已通过，旧macOS失败保留，未开启专项不计通过。32/42保持；审批待批/移除/批准结果见下节，明确自身撤销/管理员拒绝分类及其余外部门槛继续待验。


## 审批待批、移除与批准恢复（2026-10-30）

用户授权的临时手动审批已完成并恢复原关闭：新节点待批无地址且有效Token仍被拒绝；控制台Remove待批节点后仍不得访问，但SDK旧approval reason不能代表明确管理员拒绝。保留目录同agent显式取消/重新申请，经Chrome批准后online并通过Token/原daemon ID/能力和session重验。专用agent退出0、PID不存在且控制台节点已删除，原主节点业务正常。CLI5在待审批新attempt URL返回前早退已单独修复；细节、证据范围和回归见[23](23-tsnet-external-acceptance.md#官方节点审批待批移除与批准恢复2026-10-30)。独立自然到期任务已装载但未执行，32/42保持。

<a id="evidence-frontend-cancellation-20261030"></a>
## 前端释放与真实HTTP2慢订阅回收（2026-10-30）

依据13的ReleaseFrontend契约和14的流取消契约，使用真实受限UDS/HTTP2、小接收窗口及同一连接上的独立健康订阅进行定向所有权验证。不是RSS窗口或用人工channel冒充网络阻塞。

发现并修复三种有确定反例的路径：

- WatchEvents的Send被真实流控阻塞时，Registry取消派生context不能解除net/http响应写，订阅队列继续保留。
- 最后一个业务Send刚好填满接收窗口时，业务handler虽返回，Connect最终error envelope仍阻塞，整个HTTP请求未结束。
- Done已关闭、回调尚未执行时，退出路径停止AfterFunc可漏掉取消写期限；用公开Context.AfterFunc hook确定性延迟回调，移除同步兜底的同测试overlay再次失败。

实现仅调整agent请求生命周期：每响应独立output guard，取消后阻止新Send，设置1秒流级收尾write deadline，覆盖业务输出及Connect终态；取消回调幂等，停止回调后同步核对context已取消并兜底，HTTP handler退出时锁内关闭writer所有权。没有新Send goroutine、不关闭共享HTTP2连接、不修改后台sync/listener所有权；1秒只约束已取消RPC的收尾，健康长流没有新增超时。初版立即reset会破坏原Canceled语义，原测试断言保持并修正为统一收尾预算。

真实Events的cancel、Close、ReleaseFrontend、终态窗口耗尽，以及Logs的cancel/Close均验证订阅数回基线、业务和HTTP handler退出、同一连接健康流继续。确定性测试另覆盖回调停止竞争、重复取消不延长截止、finish后不访问writer、正常完成不设deadline。修前红证据保留于 `/tmp/shiki-subscription-lifetime-race.log`、`/tmp/shiki-subscription-final-status-probe.log`、`/tmp/shiki-subscription-callback-red.log`；最终定向race绿见 `/tmp/shiki-subscription-callback-final-green.log`。

最终五输入hash冻结于 `/tmp/shiki-subscription-final-source-v2.json`。最终native四包race/vet全部通过：agent36.635秒、connection2.264秒、client11.070秒、plugin5.102秒，命令退出0，冻结hash保持。最终同测红证据另保存在 `/tmp/shiki-subscription-exact-final-tests-red.log`：最终测试不变，仅overlay回原ipc，两个释放分支均失败；去除同步兜底的独立overlay也失败。最终定向race及整包结果见 `/tmp/shiki-subscription-final-verification.json`，定向verbose无skip；非verbose整包日志不宣称逐项无skip。

Linux最终5文件与native冻结完全相同；agent整包race30.044秒、vet及architecture0.036秒通过，同组还完成公开包矩阵7.43秒。证据 `/tmp/shiki-subscription-linux-final-20261030-DoRHUa/`：整体退出0、45.389秒、691项源hash保持、精确容器/镜像已清理；manifest SHA256 `fd5b2c34f67f5f55103de4be9815b1e92ac30d2b387f169e792d3f1f73e1121b`，日志 `b3ef11573c7603b91c8f20f6d220e8af762f8fce88a206627be61762220fffae`。其余三个包生产源码未变，采用前组各自通过证据，没有无依据重复。

较早native四包race34.613/2.469/11.245/4.896秒属于最后兜底修正前，不追认为最终源码。Linux首组四包race31.358/2.167/4.135/4.589秒及vet/architecture也属于兜底前；该组后续公开包矩阵被Docker bind所有者与父进程argv误匹配阻塞，未计通过，源与容器清理已核验。后组已用容器原生私有状态目录通过矩阵并对最终修改独立补验；没有重启旧检查或将旧源追认为新源。

该缺口来自独立生命周期假设，不解释569源的+228对象；18份历史profile复核、已修旧根与长稳边界见20。原12小时不会追认为当前修复长稳，公开alpha.2也不包含本修复。

<a id="evidence-current-default-rotation-start-20261030"></a>
## 当前候选默认轮换启动与待验终态（2026-10-30）

本批目的为取得c4c5a56当前候选对新Stop自然worker退休/排空及恢复尾部的实际覆盖。不是重跑旧697为其改结论，不以RSS斜率为理由追加短测；两侧11份default_rotation Go及test-default-rotation.sh与旧697字节相同，原TTL、断言、阈值和观察方式保持。正式产品为当前源码普通build，未冒充公开alpha.2归档。

两平台先预检，再各自只启动一次正式观察：

| 平台 | 预检 | 正式输入、证据及启动 |
|---|---|---|
| Linux arm64容器 | `/tmp/shiki-current-c4c5a56-linux-preflight/`；测试89.38秒，应用/collector0、无source变化、精确CID缺席及snapshot删除。预检不是12h验收。 | `/tmp/shiki-current-c4c5a56-linux-12h/`；collector53299，caffeinate53300；首checkpoint北京时间10/30 17:02:37.890598、9轮。 |
| macOS arm64原生 | `/tmp/shiki-current-c4c5a56-native-e3fvvgvz/preflight/`；测试85.41秒、实际工作1m2.810s/15轮，应用/collector0、8 PID缺席、source/临时binary删除，无强制清理。 | 同base的 `formal/`，固定输入在 `input/`；collector58148、runner58257、caffeinate58149；首checkpoint北京时间10/30 17:05:12.470559、9轮。 |

两预检和两正式source均690文件，manifest SHA256为 `7855442685b43d826e82aa8bc31aed1c3b5d89f721a4e11d8e46f36ff4ba9d08`；558份Go/proto/module输入与c4c5a56逐git对象匹配，正式源再次重hash保持。Linux冻结挂载只读，native从同一固定input再复制；Python自动pycache属于collector排除项，不作为产品源。两个长跑以独立session持久执行，受13小时外层截止约束；每组build -p=1，当前编译已结束。caffeinate仅随各collector生命周期抑制系统空闲休眠，不改电源设置或阻止显示器休眠。

Linux最早自然到期为北京时间10/31 05:02:34.914992，初始最早终点05:32:35.343351；Mac分别为05:05:06.445537和05:35:08.305964。这些时间不是通过承诺：四角色必须各自然替换一次，满足新PID/birth/hash/UID/parent身份、旧socket消失和2分钟恢复预算；实际all_roles_recovered后仍须至少30分钟尾部，应用/collector退出0、冻结hash及精确资源清理均完成才可判定。

原终端/stats保持；原held TCP结束且不重放；持久mapping/forward/revision及最后daemon ID/credential revision按原断言核验。轮换窗口暂停短周期文件业务，不声称覆盖任意在途提交。资源门禁仍是FD基线+4、测试harness goroutine+6、8秒内连续三次满足；不是生产goroutine或连续RSS硬上限，不构成无泄漏证明。测试禁用Tailscale，使用独立TLS/24h应用Token与证书、shell终端夹具；不覆盖真实Claude、官方tsnet认证或没有调用的慢前端RPC。官方未使用key到期任务及现有节点完全独立。

启动时结果为**运行中**；两组现已完成并通过[终态核验](#evidence-current-default-rotation-final-20261031)，旧495/697源记录保持原范围。根目录外的launcher JSON、正式evidence及run.log保存实际PID/命令/原始观察，PID只作为此启动记录，不可单独用作未来清理授权。


## 四目标原生 Homebrew CI 准备（2026-10-30）

现有CI新增四目标原生runner：macos-15/arm64、macos-15-intel/amd64、ubuntu-24.04/amd64、ubuntu-24.04-arm/arm64。仅分支/PR验证已公开渠道，tag复用CI在发布前跳过此job，避免依赖尚未公开的资产。可信checkout取得Formula版本/摘要及最后修改提交，真实远端tap固定该提交；版本随自动发行公式更新，不永久绑定alpha.2。

实际brew操作仅允许普通用户的GitHub托管runner，核对uname/RUNNER_ARCH并拒绝Rosetta。完整install/test/reinstall/test/uninstall、14成员及所有摘要校验与Formula自身Ed25519验证分别记录。只回收本轮登记的tap/keg/新装未用依赖，路径身份不符拒绝删除；前后tap、已装包、服务及受保护状态必须一致，finally与always清理保留历史失败。私有缓存/信任目录不上传。机器解析只读取stdout，stderr独立留证。

11项隔离Python回归、actionlint和语法检查已通过；没有在用户Mac执行brew或改Xcode/CLT。四目标实际结果仍待，不以工具测试代替渠道验收。c4c5a56的CI41029690521及a8112d9的CI41031430621均已成功；原42条及32勾选保持，默认12h与预约key到期测试不受影响。


## 四目标 Homebrew 原生验收终态（2026-10-30）

四渠道CI及独立证据复核均通过，来源、123条命令、八次完整bundle检查、两次清理及前后环境一致见[22](22-homebrew-distribution.md#四目标原生-ci验收)。本批仅关闭原Phase5签名/分发项，33/42、9项待完成。用户Mac未执行brew；本机工具前置失败、完整服务/平台与长期资源门槛独立保留。生产源码不变，未创建新tag或Release。


## 双主机终端提示修复与有限恢复验收（2026-10-30，第1–3轮历史记录）

来源与逐轮真实结果见[25](25-multihost-acceptance.md)。c4候选双端真实CLI/agent/daemon已接通，第一轮节流夹具失败、第二轮模式提示产品缺陷、第三轮真实CLI修复已验证但Dashboard夹具早按键失败；均保留失败且各轮正常停止。当时有限文件/终端/转发/撤权及两次部分传输SIGKILL尚待整轮结果，未提前关闭Phase0/4多设备或Phase5恢复条目；后续第12轮及OS清理已通过，最终结论见25。旧tsnet三节点已按用户要求移除，专用进程/账户/home与旧私有状态已精确回收；预约expiry和c4长稳独立保留，详见23。


## 官方认证预检发现的启动恢复修复（2026-10-31）

m6专用agent正常重启后，同一节点/IP已online，既有server却永久REMOTE_INCOMPATIBLE。确定根因为启动早期本地node未授权的FailedPrecondition被当成远端协议拒绝，重连episode永久停止。现以不可序列化的本地typed cause识别该准入等待，显示auth_required/NODE_AUTH_REQUIRED，每秒仅重查本地准入，不耗远端网络重连预算；恢复后仍重新验证Token、daemon ID和能力。远端返回相同文案不得伪装本地等待，不自动BeginLogin、不扩TLS fallback、不重放旧流。

真实node.NewRPCPool的修前测试失败；修后agent五项race2.638秒、node三项race38.906秒与两包vet通过。冻结719文件的新候选m7双平台构建通过，agent整包race37.341秒、68顶层/187含子项、零skip/fail通过，94文件hash未变；干净m7 Mac正常重启后无需显式connect即同IP online、同daemon和凭据代次ready/tailscale-direct；此前node322.712秒不追认为本次新cause的覆盖。官方m6显式重新connect可恢复同daemon，已通过shell终端回显、双端插件ready、明确初始方向后的文件上传及loopback TCP真实回显。当前只是诊断基线，尚未完成节点key过期。

2026-10-31官方节点key单次短期到期已实际发生：官方回读期限后SDK进入NeedsLogin但Self.Expired=false，旧终端和TCP已关闭。已修复组合证据分类（仅NeedsLogin、已知self公钥及非零期限且超过期限1秒，或显式Self.Expired），11项定向race与vet通过；CLI对有效expired/uninitialized尝试继续原有界等待，最终整包race42顶层/183含子项、28.607秒、零skip通过。干净m8重启后SDK不再提供旧期限，实际为needs_login；CLI9仅替换前端，01:34 Chrome重新认证后同IP/daemon/凭据自动ready，终端新回显、文件双向收敛及新TCP精确回显通过；不把该恢复追认干净候选到期分类已现场观测，详见[23](23-tsnet-external-acceptance.md)。CI优化已实现四原生并发、按资源选择p1/p2、隔离用户复用私有缓存副本及安全失败行号；17项工具检查和独立七类审查通过，实际收益待新CI，旧失败保留，见[26](26-native-platform-acceptance.md)。原42条及36/42保持。


2026-10-31追加平台修复：已适配tmux3.8对已退出pane返回空PID的实际格式变化，仅dead缺PID记0，live缺PID仍拒绝；退出证据与精确tmux身份保持，PID0不进入信号/活跃归属。确定性修前红，本机3.7b两包race65顶层零skip/fail及vet、独立七类复核通过；真实3.8平台须待新CI，不用本机旧版本覆盖替代。Mac排队实测已降至Intel5秒/arm64 2分27秒，整轮耗时仍待，详见[26](26-native-platform-acceptance.md)。原42条及36/42保持。


**2026-10-31 CI修复与性能收敛：** 首轮固定源原生Intel（tmux3.7c）完整产品及LaunchAgent验收通过；58806be轮Linux双架构与实际WSL全产品/E2E/embedded/terminal通过，两Linux全包race约17–19分钟降至9分钟。该轮Mac的tmux3.8空PID与GOMAX1期限失败已分别取得红绿和修复；systemd专属根曾移至/opt，0755隔离容器的生产路径对照通过；后续发现托管镜像可将/opt设为0777，修复并未覆盖这一环境，现补/var/lib及逐层前置校验。arm64 useradd超时仍待原因，新固定阶段诊断保持60秒预算。构建参数对齐实测185.545→152.838秒且六二进制/双manifest完全一致。新增按角色/平台/来源隔离的partial/warm缓存，16缓存、18平台、8构建元数据工具检查及独立七类审查通过，远端新缓存和修复后整轮结果待CI。详情及失败/源码范围见[26](26-native-platform-acceptance.md)；同身份官方到期恢复见[23](23-tsnet-external-acceptance.md)。原42条及36/42保持，受保护长测和预约key到期未修改。

**2026-11-01 CI长任务缓存修复：** 运行`41226652766`显示Linux、macOS ARM及多数阶段通过；macOS Intel E2E冷构建子进程被系统终止，heavy integration在内部45秒构建预算内超时，embedded测试通过但setup-go缓存归档触发10分钟job取消。修复在三个长任务中关闭setup-go自动缓存，恢复preflight生成的module/build缓存，并为E2E和embedded预取Go模块；embedded预算增至15分钟。`actionlint`、`git diff --check`、平台29项、脚本83项及架构race本地通过；新远端结果尚待验证，历史失败保留，不把本批修复写成已全绿。

后续运行`41230062520`确认三个长任务均能执行缓存恢复，但preflight的`ci_go_cache.py plan`缺少成功状态变量，实际没有保存供下游复用的缓存；Intel heavy/E2E仍以`signal: killed`结束测试内编译并触发超时，其他任务与embedded通过。当前补丁为preflight缓存计划显式设置构建与验收成功状态，仍待下一轮远端验证；在此之前不宣称CI全绿。

运行`41233288832`确认Linux preflight已写入缓存，但macOS Intel preflight在10分钟预算内被取消，故下游矩阵全部跳过；当前将preflight预算增至15分钟以覆盖首次冷构建，所有构建、lint、工具测试和缓存保存步骤仍为必需项，等待下一轮远端结果。

`41235197414`证明Intel preflight及native cache保存成功，heavy integration通过；E2E的race子进程编译仍被系统终止。当前在preflight一次性预热E2E/集成所需race目标并将预算增至20分钟，保留全部原测试与失败分类，等待下一轮远端验证。

该运行的Linux arm64 E2E另有一次daemon重启后的瞬时`service unavailable`，属于本地agent重连尚未完成的有界竞态；已在该单一恢复查询中增加10秒、仅匹配该错误的重试，其他错误仍立即失败。测试编译通过，修复待下一轮远端验证。

`41239995440`中race预热、Intel E2E/heavy、Linux arm64 E2E和embedded已通过；terminal/maintenance四项失败均为不可解析的`actions/download-artifact@v7.0.2`版本，未执行产品测试。CI现改用release workflow同样使用的`v8.0.2`，待下一轮验证artifact下载及最终维护阶段。

`41242641679`显示workflow配置变化会使Go cache source key无必要地失效，Intel preflight在20分钟内无法完成再次冷race构建，其他平台通过。`scripts/ci_go_cache.py`现排除`.github/workflows`及自身helper对构建来源的影响，同时保留非Actions Go工具版本变化；16项cache测试、83项脚本测试、actionlint和diff均通过，待远端复验。

`41247430410`的主矩阵、artifact链路、WSL2和大部分terminal/maintenance任务已通过；旧Linux amd64 systemd在隔离账号`useradd --create-home`阶段60秒超时，旧macOS arm64 terminal语义专项约30秒失败。修复已保持隔离账号、真实systemd和完整终端断言：systemd预创建私有home并使用`--no-create-home`，账号创建遇到短暂passwd锁时最多三次20秒重试，仅在精确UID/GID/home身份已提交时接受超时后的完成；终端脚本每次使用新建0700 `TMUX_TMPDIR`。本地平台工具29项、脚本83项、actionlint、终端专项3次及`git diff --check`通过；旧run重跑的Linux maintenance已通过，待本提交远端CI作为最终证据。

新提交`2d6980e`的远端运行确认Linux systemd和WSL2成功，但macOS arm64 terminal仍以`terminal-semantics`失败；原始测试输出继续留在私有日志。`platform-ci.py`现仅将已写入的固定checkpoint文件名和数量导出到公开失败摘要，以便定位失败阶段而不泄露屏幕数据、路径或凭据；平台工具30项及本地终端重复验证通过，新的远端修复运行仍待。

`2421ba3`的terminal公开摘要为零个checkpoint，说明失败早于首个屏幕断言；`test-terminal-state.sh`现分别标记analyzer构建、测试二进制编译和测试启动的running/done阶段，失败时只保留固定阶段名。此改动不改变测试命令、超时、tmux隔离或私有日志策略；本地terminal重复运行和平台工具30项通过，远端新运行仍待。

`41255473767`的macOS arm64摘要显示`phase-analyzer-build.done.json`与`phase-test-compile.done.json`、`phase-test-run.running.json`，即失败位于首轮语义测试执行；本机同Go版本/同架构连续5次完整终端专项通过。为吸收托管runner短暂tmux/资源抖动，测试脚本现在对全新隔离fixture最多重试一次，真实断言失败仍需两次均通过才结束；下一轮完整CI负责最终证据。

`41257594777`显示上述重试分支受shell errexit影响未执行第二次（阶段约27秒即失败）。已改用显式`if`控制并给每次尝试分配独立0700 `TMUX_TMPDIR`，避免失败fixture的tmux状态影响后续尝试；本机终端专项与工具检查保持通过，等待新远端证据。

后续`41259732438`确认两次独立尝试均在首个语义checkpoint前失败，阶段约24.9秒；本机同架构终端专项仍通过。现将fixture等待agent UDS就绪的有界窗口由5秒增至15秒，作为托管资源抖动的测试夹具稳定性修复，不改变生产超时、终端协议或断言，待新CI确认。

`41262112652`的Darwin amd64 preflight失败原因为`go install protoc-gen-connect-go@v2.0.2`向proxy.golang.org建立TCP连接超时，工具下载/生成尚未开始，其他平台前置通过。`scripts/dev-setup.sh`现对protoc下载和三个版本锁定的生成器安装各执行最多3次、带连接和总时限；本地`make tools generate-proto`通过，下一轮验证远端网络抖动恢复。

新运行的Darwin amd64 preflight已通过重试继续完成；macOS arm64 terminal仍在两次隔离尝试后失败，阶段约27.6秒且无屏幕checkpoint。公开摘要新增固定失败类别枚举，原始测试日志继续只存私有目录，下一轮用于定位具体fixture边界。

同轮Darwin arm64 `internal/client` race在`TestTerminalTakeoverFailureDoesNotEnterRaw/cancel`报告`context.Canceled`与Connect Canceled码的观察顺序差异；生产`terminalCause`保留调用方取消原因，测试现接受这两个等价结果，仍严格检查未进入raw及终端状态未改变。本地20次race通过，待远端复验。

复核发现部分HTTP/2取消返回无cause的Unknown Connect错误，导致仅测试放宽仍不稳定；现由`terminalCause`统一映射本地及标准取消消息到Connect Canceled/DeadlineExceeded并附加本地cause，保留`errors.Is`语义，其他错误不改变。本地20次race通过，提交后由完整CI复验。

同轮Darwin arm64 `TestPortAdmissionSharesBudgetBeforeFirstFrame/WatchPorts`因测试在32个请求尚未进入计数器时提前探测，收到`plugin is unavailable`；已等待对应精确admission计数达到上限后再断言ResourceExhausted。本地10次race通过，生产端口预算未改。


## 原生CI安装路径修复与证据同步（2026-10-31）

c10852f的CI41104833762已取得WSL完整产品及systemd维护通过；两Linux产品链通过、首次安装失败和cleanup通过分别保留。独占systemd容器冻结c108，模拟/opt0777后真实installer退出1，分类为system_prefix_ancestor_unprotected；整体诊断含构建和清理109.055秒，首次安装未单独计时。证据位于`/var/folders/1n/q373cln10fqb0gl9z1ds5d8m0000gn/T/shiki-native-initial.sje9gvp9/`，仅本地私有保存；CID901b50ad34476d6127fedeb6a222221ec6776b75a961f330ef996291f2c27750及专属镜像tag清理后缺席。未修改原主机/opt、HOME、服务或受保护长跑。

CI专属根改为/var/lib并在任何账户/缓存/构建操作前核对全部祖先；失败结果继续可见，目录创建到归属登记屏蔽取消信号，恢复后由finally清理。新增安装器错误固定分类及固定目录诊断，原始stderr不上传。21项平台工具测试1.078秒、四文件AST和diff通过；架构/协议检查0.440秒通过，四份更新文档367条本地链接无断链，42条文字/顺序/状态保持、36/42。新原生正向结果仍需下一轮CI；本轮不重新构建容器正例或追认旧WSL证据。具体平台与缓存时间见[26](26-native-platform-acceptance.md)。


## CI测试时序与独立维护验收（2026-10-31）

c108的Mac双架构完整产品/LaunchAgent已通过，e2f的实际WSL2在/var/lib新布局完整通过：全包race665.037秒、E2E195.992秒、systemd255.314秒，13项生命周期与双角色四个活动升级/回滚边界、数据保持、精确清理及来源核验通过。e2f原生Linux两架构仅各有一个产品测试失败，E2E/维护未执行，不代表安装路径失败；实际缓存恢复及分阶段时长见[26](26-native-platform-acceptance.md)。

退出事件测试保留四种真实进程场景、8秒总context及400毫秒静默窗口，允许unknown后首次已知信号/退出码，但检查scope、顺序、正确原因且最多一次crash。新增受控观察unknown→signal15→unknown确定性场景验证禁用自动重启时不计划重启、不重复事件。临时真实tmux观察包装取得旧断言红例1.098秒；最终测试不强迫所有tmux版本提供信号。最终3项顶层/4项子例race5.753秒及vet通过；早期全session race21.482秒属于初稿，不追认为最终全包证据。

认证测试使用本地真实Noise/HTTP2控制面返回HTTP400；确认needs_login可以先于真实拒绝，改用已安装的LocalAPI订阅或独立SDK初始health快照屏障，分别验证订阅后与订阅前错误。退休watcher代次、Node、observer、HTTP transport和hijacked连接均清理，原30/15秒期限保持。四组认证×5共20次顶层race43.912秒；最终独立SDK oracle两种时序包4.764秒、vet通过。旧测试本机10次通过，未直接复现原CI瞬时失败；临时overlay移除watch拒绝分支时新用例16.15秒失败仅证明覆盖能力。报告`/tmp/shiki-auth-health-20261031/review.json`，session私有证据`/tmp/shiki-session-exit.IwZpAivb/`。

Linux及WSL普通产品失败后可继续独立维护；前置四阶段须真通过，重验六字段原生身份、提交与所有受跟踪/非忽略输入，取消或来源变更拒绝执行。finalize原必需阶段保持，维护成功不能掩盖产品失败；Mac仍按原成功链执行。27项平台工具检查1.200秒、actionlint、Python AST、WSL内嵌Bash语法及独立七类审查通过。生产Go、协议、依赖和超时不变，完整平台等待新CI；08/15/16同步，原42条/36已勾选保持。


## 原生Linux闭环与ARM64 race依赖修复（2026-10-31）

896b986的CI41115363744原生Linux amd64/arm64及WSL完整通过，源码未变、未完成阶段与失败列表均为空。原生两架构build/lint17.717/13.500秒、全包race385.477/400.338秒、E2E176.416/168.868秒、systemd239.720/245.831秒；两旧失败用例全部正向通过。两平台实际/opt可被其他用户写入，/var/lib、/var、/三层均安全；新布局维护13操作、四活动升级/回滚边界、数据保持和cleanup通过。partial缓存实际恢复12.708/15.550秒后保存本轮warm；WSL产品566.848秒、E2E185.237秒、systemd247.212秒完整通过。e2f的两Mac完整通过另存；896b Intel唯一失败在peer_lifetime_test.go49的loginInteractive准备，尚未进入撤销断流断言，Mac ARM运行中，见26。

CPU诊断冻结896b源、独立overlay及状态：本机两核race同一连接用例32.48秒，两登录9.706/9.530秒、首次RPC4.543秒、清理0.104秒；另轮CPU采样29.92秒wall/50.77秒CPU，gVisor Sleeper/fetch/nextWaker及调度热点突出，未解析ExternalCode不猜成加密。直接调用旧依赖CAS的1→2交换实际返回false且保留1；汇编原文件、模块zip和objdump一致，排除本机误改。新CAS回归在旧依赖三个分支均红，包0.612秒；这是race && arm64路径错误，不能声称默认非race产品同样忙循环。

上游修复[PR14585](https://github.com/google/gvisor/pull/14585)于2026-09-09合并，原commit1b2946c日期为08-23；固定第一份包含修复的自动Go快照`v0.0.0-20260909230408-9b1144b679cb`。原修复commit含未生成模板，不能直接编译；10-30最新快照要求Go1.27.3，独立候选被拒绝，均未进入主树。最终仅gVisor、mdlayher/socket0.5.1、x/exp6ae5c78190dc、x/time0.15.0四个要求版本变化，Go/toolchain及Tailscale保持。该依赖更新包含上游其他变更，不能描述成仅一行回补，须完整平台验证。

同GOMAX2/p1/race、原45秒期限和相同业务用例，新候选1.81秒、包3.436秒；这轮overlay未命中规范/private路径，因此仅比较真实原用例总时长，不报告其分阶段收益。冻结896源+独立最终modfile的node整包44顶层/132含子项，51.200秒零fail/skip；主树新增CAS三分支race1.533秒通过。主树全包build39.847秒、vet22.515秒、go mod verify3.356秒、包级govulncheck4.029秒全部通过。工具完整记录位于`/var/folders/1n/q373cln10fqb0gl9z1ds5d8m0000gn/T/shiki-ci-dependency-tnkdjc05/`，性能/候选证据位于`/var/folders/1n/q373cln10fqb0gl9z1ds5d8m0000gn/T/shiki-node-perf-896b986-79uy476y/`。独立tidy会把可选负载诊断UI专用mod依赖引入根图，已排除，原sum历史行保留合法。

Intel登录失败未复现：冻结896旧依赖本机4次通过、随后110秒整组截止，不记为case失败复现；候选依赖的两次phase诊断2.01/2.08秒，四次登录URL等待160–162ms、online349–398ms，原15/45秒不变。无法从公开安全行判断Intel失败属于哪一阶段，不改生产/增加重试；`/tmp/shiki-peer-login-896b-20261031/review.json`保留范围与清理。新依赖完整CI和Intel复验待下一轮；七类审查通过，42条/36勾选保持，既有长测不跨源码追认。


## 三核CI受限并行与远端收尾观察（2026-10-31）

c1e5ece包含ARM64 race CAS修复后，重新验证三核调度：实际3CPU且至少6GiB选择两个Go包并行、各GOMAXPROCS=1；两核/低内存仍p1，四核仍p2/GOMAX2，探测失败仍保守回退。原588三核p2/GOMAX1的node超时发生在已确认的旧CAS缺陷下，不能把当时临时p1/GOMAX2规避永久当作修复后的CPU要求。实际新三核时长与完整维护仍须新CI确认；本机18逻辑CPU/64GiB上的Go参数验证不是内核三核/7GiB模拟。

独立验证保留全部过程：第一轮私有HOME/TMPDIR过长，触发既有100字节配置/Unix socket限制及tmux长度拒绝，228.138秒精确终止、源码未改；第二轮短路径源c1e完整432.598秒，47包PASS、2包FAIL、16无测试，631顶层PASS/2FAIL/3skip。setgid权限分支受临时目录继承gid0而进程gid20影响，macOS静默清除非所属组的setgid位；仅将新的独占状态根规范为当前用户/组后，该权限/owner专项1.347秒全过。没有放宽产品权限检查或路径限制。3skip分别为root system-prefix、opt-in资源回收和独立终端analyzer，仍不当PASS，完整平台有各自专用门槛。

另一失败为sync移除测试：本地GetSyncState已NotFound后立即断言远端HTTP处理器Watches=0，把异步取消传播当成跨进程原子操作。现在使用同包已有5秒waitFor观察远端退出；本地删除、归档、冲突证据及双端文件不删断言保持，未修改生产取消或重试。定向race重复10次包2.998秒零fail/skip通过；最终测试源仅增加该观察修正。末轮显式GOFLAGS=-p=2、GOMAX1、短HOME/TMPDIR及UID501/GID20，node整包45顶层/136含子项50.995秒（wall55.356）零fail/skip，冻结源保持。早期轮主go test是p2，测试内go build默认p1；两种构建调度如实区分，不把旧轮耗时当最终策略精确基准。

27项平台工具1.214秒、两Python AST/diff及七类审查通过。旧全套两失败没有改记一次同源全绿；对应修正专项已过，最终五平台CI负责完整闭环。独占5个运行状态根按UID/开放引用清理；冻结源码副本与临时运行脚本也已删除，共1,464文件、12,411,908字节。源码清单、补丁、结果、摘要及安全/私有证据保留在`/var/folders/1n/q373cln10fqb0gl9z1ds5d8m0000gn/T/shiki-ci-p2-gomax1-c1e5ece-aojc7z8w/`，清理见该目录artifact-cleanup-receipt.json；历史源码路径不再表示现存文件。此前性能诊断17对象/745文件67,297,128字节与peer-login冻结源6,193,001字节已精确删除，历史路径仅作已执行记录，相关summary/hash/profile/清理收据仍保留；未触碰认证身份、原长跑与预约到期任务。


<a id="evidence-native-platform-c1e-20261031"></a>
## 同源五平台兼容验收闭环（2026-10-31）

CI41119194452绑定c1e5eceb76e0e380949d0619c4babe731900311c，最终completed/success、13/13任务成功；四构建、四Homebrew及五完整平台均通过。主代理逐一核对source.commit、source_unchanged=true、incomplete_stages为空、所有必需阶段passed、原生systemd cleanup passed及Mac三项LaunchAgent具名PASS；结果、逐阶段时间和精确平台身份见26，独立收据为`/var/folders/1n/q373cln10fqb0gl9z1ds5d8m0000gn/T/shiki-ci-41119194452.y7ilv6bt/root-platform-verification.json`。

仅关闭原始“macOS Intel/ARM、Linux、WSL客户端与Linux daemon兼容性验证”一项，36→37；Phase0–5计数10/11、7/7、6/6、6/6、4/6、4/6。00–14契约及原42条文字/顺序不变。artifact明确external_accounts与physical_restart_login为not tested，这些属于认证/安装/升级/最终支持的独立门槛；不宣称全部OS发行版、真实Claude、物理重启、自然切网或长稳通过。旧896 Intel单次登录失败保留，本轮同用例1.88秒正向通过不等于定位其旧瞬时根因。

实际ARM节点用例合计375.41→43.57秒（44→45顶层，新增CAS回归），Epoch40.70→1.06秒、Address36.71→1.24秒、PeerRemoval108.21→11.55秒；原45秒预算保持。全产品顶层用例elapsed合计1098.77→777.64秒（635→636），不同于包含编译/调度的stage时长，不将冷缓存成本归为测试回退。新依赖四原生warm均真实保存；最后一批三核资源选择与sync测试观察不修改生产Go/依赖，后续CI为执行策略提供单独验证。


<a id="evidence-ci-045-final-20261031"></a>
## CI修复与耗时优化最终验收（2026-10-31）

045b463a9f3d5949b938685c3d44fd23bf531133的CI41125907702已completed/success、13/13任务成功：四目标构建、四Homebrew和五完整平台全部通过。主代理独立校验五份source.json各730个文件摘要与固定提交一致，result.state=passed、source_unchanged=true、incomplete_stages为空；完整产品、必需跨进程/E2E、具名终端/内嵌插件、真实systemd或三场LaunchAgent维护及清理均通过。实际条件skip保留，未作为通过；外部账号与物理重启登录仍明确not tested。独立收据、各阶段和任务时长见[26最终记录](26-native-platform-acceptance.md#ci-045-final-20261031)。

最后批次只改变三核CI资源选择与sync测试的远端异步退出观察，生产Go及依赖仍与c1e一致；实际Mac ARM 3CPU/7GiB、p2/GOMAX1完整通过，两原45秒用例2.10/2.31秒、sync权威移除0.08秒通过。整轮46分51秒→36分09秒，Mac ARM任务40分35秒→21分58秒，含冷暖缓存/并行/排队影响；Intel与WSL分别35分32秒→36分02秒、23分48秒→26分58秒，未描述为全平台等比例加速。全部旧失败、局部补验与先前源码范围保留，未增加生产或CI期限/重试，未减少必需阶段；sync远端收尾仍明确由立即断言改用已有5秒有界观察。

最终收尾仅更新docs08/15/16/26；三份当前摘要及37/42一致，原42条文字/顺序保持，平台条目的关闭依据仍是已完成五平台验收。诊断源码/脚本、临时二进制和独占状态已按收据清理，必要安全证据与哈希保留，唯一CI监测进程在终态自然退出；原冻结长测、预约key到期、认证身份与签名备份不改。文档提交不改变已验证代码范围，也不追认新的长稳/外部身份结论。


<a id="evidence-current-default-rotation-final-20261031"></a>
## c4双平台默认轮换、自然key到期与精确清理终态（2026-10-31）

三项等待均已实际完成。本批没有启动、重跑或停止原运行；先读取原始终态，再独立比对固定来源、事件、恢复尾部与所属资源，最后清理已无引用的静态残留。

| 场景 | 北京时间完成 | 实际结果 |
|---|---|---|
| c4 Linux arm64默认轮换 | 05:32:41 | 应用12h30m1.556524841s、4371轮、740检查点；四角色各自然替换一次，恢复尾部1802.030959秒、30检查点；应用/collector均0 |
| c4 macOS arm64默认轮换 | 05:35:13 | 应用12h29m59.808079333s、4192轮、741检查点；四角色各自然替换一次，恢复尾部1802.044518秒、30检查点；应用/collector均0 |
| 独立未使用Auth Key自然到期 | 15:56:08 | 在15:55:52保守门槛之后，只调用一次登录，2.449517秒明确denied/INVALID_AUTH_KEY；key删除，agent正常退出0，专用job卸载 |

Mac应用elapsed的计时起点晚于worker ready，原值保持；逐角色12小时下界、原2分钟恢复预算、end_not_before及恢复后至少30分钟均独立成立，不将其四舍五入伪造成精确12h30m。Linux替换观测距各自最早到期约1.27–1.36秒、Mac1.36–2.00秒；仅Mac server-port采样到不可用，不声称同时退役。原持有TCP终止且无重放，原mapping/forward/revision和业务恢复断言通过。

两组690文件manifest一致，558份Go/proto/module执行输入（含测试）与固定c4c5a56fe6fe104f9cb94df5a6bb3b918412c1f5逐git对象匹配，后置source未变。主代理直接复算原始事件、四角色新PID/birth及同hash/UID/parent、30个恢复检查点和尾部。Linux精确容器已不存在；Mac12个已登记进程及runner/collector/caffeinate共15个PID缺席，运行快照与fixture已自动删除。独立证据位于`/tmp/shiki-c4-final-audit-20261031-bvok_yst/`，audit.json、root-final-verification.json、完整资源统计和source差异分别保留；原两证据根仍保留结果、日志、source清单与events。

新增精确清理：native input副本和两process-guard删除693文件、14,100,508字节；预约到期目录中两二进制、配置/plist、私有agent/tsnet状态、空runtime及scheduler日志共10项目标删除20文件、7目录、72,890,138字节。合计回收86,990,646逻辑字节；删除前重验UID/dev/inode、摘要、无符号链接/开放引用和登记进程缺席，原35份长测与8份到期证据摘要保持。root-cleanup-verification.json独立确认目标缺席；到期清理收据在其保留证据根cleanup-receipt.json。正式Mac/VPS认证身份和签名备份仍有用途，本批不动。

此前CI清理37个收据目标均复核缺席、监控PID不存在；指定六证据根仍保留153文件、8,111,975字节，主要为JSON、日志、profile、哈希及核验工具。因此“临时程序/运行状态已清理”成立，“所有数据均删除”不成立。核验收据位于`/var/folders/1n/q373cln10fqb0gl9z1ds5d8m0000gn/T/shiki-ci-cleanup-coverage-audit-20261031-xj053i6n/`，不扩大删除至用户原有环境。

完整c4资源与当前触发路径覆盖见[20](20-long-run-resource-review.md#current-c4-final-20261031)，自然到期的来源/一次调用/官方库存边界见[23](23-tsnet-external-acceptance.md#unused-auth-key-expiry-final-20261031)。三项等待已关闭；按用户确认“测试通过即闭环”的口径，Phase 5 长稳、资源与性能复合门槛现已关闭，该批路线图为41/42（2026-11-05已完成支持声明，当前42/42见文首）。生产存活heap/object归因、真实Claude和主机重启登录仍是技术/支持边界，不据此宣称无限容量或零泄漏；最终支持声明在该批时仍待。没有新假设不追加同一12小时长测。


## 不重启用户主机的验收计划调整（2026-10-31）

用户明确Mac与VPS均不可能重启，要求采用其他验证方式。本批仅复核文档和既有证据、更新执行计划，没有重启/注销用户会话、操作服务/节点、清理Tailscale记录或启动新测试。按该明确约束，将真实主机重启从等待用户执行的任务移除，以专用服务管理器重新拉起、同身份/数据/业务恢复、已有独立持久卷二次PID1和原生LaunchAgent证据组成替代验收；按需隔离VM只覆盖其实际启动范围。

现有二次PID1/LaunchAgent/TLS维护结果先复用，未完成的重点是保留官方tsnet身份贯穿真实安装器A→B→A和专用管理器恢复；来源、PID/instance、身份/凭据、资源状态、新业务及精确清理为结束条件，见[11替代矩阵](11-automated-installation.md#no-host-reboot-acceptance-20261031)。真实硬件重启/重新登录继续标为未实测支持边界；替代计划不提前记通过，其他认证、Claude和资源归因仍待。08/11/15/16/19及索引同步新约束，原42条与37/42不变。


<a id="evidence-forward-installer-auth-fixes-20261031"></a>
## 转发缓存所有权与安装器登录等待修复（2026-10-31）

确定性回归先实际执行StopForward删除配置/缓存，再释放先前读取旧快照的ListForwards；旧实现重新创建孤儿缓存，修前失败。查询现仅返回worker观测，不再写生命周期缓存，apply/suspend/stop继续管理其所有权；没有新增锁、网络调用、schema或协议。转发专项race3.364秒；完整agent race38.105秒、真实port worker的integration/agent race9.293秒、architecture1.423秒通过，共197父/子测试、无失败或skip。证据`/tmp/shiki-forward-cache-lifetime-20261031-7prbtuxc/`含red、源码摘要、tests.jsonl、verification及ownership-review。

安装器在有效NODE_APPROVAL_REQUIRED/NODE_KEY_EXPIRED中间态会重复BeginLogin并提前结束，Native还会丢弃相应私有登录URL。现在与CLI采用相同的有界中间态判断，保留原attempt，不扩大业务授权；明确失败终止，15秒整体/2秒单次观察及父截止不变，authenticating沿用既有有界等待。URL继续仅经私有helper管道到控制TTY，不进入公开JSON。中间态/终态/父截止和Native私有URL回归先红后绿；完整install、cmd/shiki-install、architecture race分别8.906/1.494/1.196秒通过。该本地序列不冒充官方URL自然过期或真实审批拒绝实测。

本批只关闭上述具体缺陷，不关闭完整资源或认证条目。Chrome只读确认保留Mac/VPS两节点Connected；真实安装器同身份组合仍在准备，官方Claude Code2.1.309仅安装到VPS专用普通用户HOME，官方登录流程已取消且退出；实际auth status退出1、loggedIn=false，未取得登录凭据、未执行真实推理。按用户后续明确要求，真实Claude登录/推理移出交付验收，继续验证未登录状态映射及其余安装器组合，见[11验收范围](11-automated-installation.md#claude-acceptance-scope-20261031)。实际Mac/VPS未重启、未注销。


## 按用户要求调整Claude验收范围（2026-10-31）

用户明确无法提供真实Claude登录，要求移除这一验收环节。当前Phase 0–5不再等待真实Claude账号登录、授权后调用或推理闭环；保留未实测支持范围，唯一基准见[11验收范围](11-automated-installation.md#claude-acceptance-scope-20261031)。这不是验收通过记录，原42条文字/顺序及37勾保持。同身份真实安装器升级回滚、服务恢复、Tailscale认证及生产资源归因继续推进。

本轮官方Claude Code 2.1.309在专用VPS用户下返回auth status退出1、loggedIn=false，登录流程已取消且未取得凭据，未执行真实推理；该负向观察不代表安装器组合或真实账号通过。之后验证安装完成与应用pending分离，并继续用明确shell fixture验证会话/状态/升级恢复。产品readiness、认证权限和协议不变，不将unknown或未登录改称ready。当前待办、README、安装/发布/平台说明与历史范围标注已同步，不再请求用户提供该账号。


## 外部安装子项补记与未完成范围（2026-10-31）

本轮VPS官方身份下的实际安装、升级/回滚、专用systemd恢复、卸载保留数据及恢复原环境已取得证据；Mac A/B首装均未在原30秒内达到节点就绪，未执行后续升级/回滚。最新诊断已否定“Server.Start未返回”的猜测，定位SDK控制面公钥请求的TCP拨号等待，具体网络原因仍待定位。来源、失败观察、恢复和清理边界见[24本轮记录](24-public-package-compatibility.md#external-installer-20261031)。

此前仅报告Claude范围调整完成，遗漏同步上述子项且未明确整个批次尚未结束；本次补齐08/15/16及24。37/42不变来自Mac真实失败及认证/资源剩余条件，不是继续等待Claude登录。临时服务/runtime及VPS官方Claude临时安装已清理，业务夹具与预备包仍待收尾，专用身份保留。工作树中两份CI诊断脚本的改动另待验证/提交，不算本次文档提交完成的功能。


## Mac用户态网络修复与验收计时修复（2026-10-31）

**历史已撤销方案，当前以[最终系统代理适配](24-public-package-compatibility.md#macos-system-proxy-20261031)为准。** Mac受管节点首装失败已通过独立SDK/普通OS拨号对照定位并修复，详细机制与范围见[24网络根因](24-public-package-compatibility.md#macos-route-fix-20261031)。Darwin适配采用上游纯userspace网络策略；node/architecture整包race48.272/1.378秒通过，节点准入/撤权/用户态业务传输未放宽。真实Mac安装矩阵随后从冻结源运行，仍保持37/42，不将独立/key成功当作整项验收。

另修复CI准入夹具：held请求的Token提前一次性持久生成，独立probe仍使用单独Token/连接，避免32次串行持久写与首帧5秒预算重叠。通过仅测试overlay给Manager.Create注入每次170ms延迟，原WatchFiles8.26秒失败→修复后0.45秒通过；文件/端口准入race33项、0失败/跳过，24.757秒。生产auth源码、5秒首帧、各类并发上限和父截止均未改。实际b30c585 CI41190701695为12/13成功，Intel该子用例7.43秒失败；公开脱敏artifact无原错误正文，受控复现不能宣称已证明其唯一根因。此前Linux useradd超时与旧node setup失败另保留。

安全诊断新增8类固定node错误前缀分类，仅输出源码位置和固定kind，原始后缀/URL/key不输出，28项工具测试通过。受控实验源范围、hash与结果原在Darwin TMPDIR的`shiki-admission-setup-20261031-feis7ftc/experiment.json`；依用户最终清理要求届时移除原始临时证据，以本记录保留脱敏结论。


## Mac现场差分与修复范围收窄（2026-10-31）

13bcd0a全socket解绑方案未通过完整外部业务，已通过同版本/CLI/plugin/代理、仅网络adapter差异的诊断否定，见[24](24-public-package-compatibility.md#macos-route-counterexample-20261031)。此后实现保留SDK默认UDP/接口策略，仅为受管Mac补现有静态HTTP(S)系统代理解析；原始HTTP200、node online和本地race仍只代表各自范围。测试common文件的durable baseline等待也已修正，保留此前冲突/失败，不改变产品行为。三份当前摘要同步37/42，源码修复和完整安装矩阵完成后再记录实际关闭。


## 静态系统代理替代实现完成（2026-10-31）

Mac保留SDK原有UDP策略，仅为SDK HTTP(S)路径采用既有系统代理的启动快照；具体来源、支持限制与验证见[24](24-public-package-compatibility.md#macos-system-proxy-20261031)。node/architecture整包race51.199/1.373秒通过；新增scheme优先/系统读取错误/NO_PROXY等专项race1.548秒通过。真实CONNECT保留目标TLS校验，命令2秒/64KiB受限，显式凭据拒绝且错误不回显。原13bcd0a方案失败保留，完整实际安装矩阵尚未执行。

13bcd0a的CI41194399587另发现Linux amd64隔离useradd60.061秒超时：该平台产品测试392.187秒及E2E181.386秒已通过，但systemd-lifecycle在账号创建阶段失败，未进入安装/升级；清理和源码一致性通过。不能将该CI整轮记成功，也不能从此前相同arm64超时推断具体NSS/锁根因或扩大预算。此CI不代表后续系统代理源码的检查结果。


## 两项安装门槛关闭（2026-10-31）

Mac冻结8fdfe25的local.proxy.1/.2完整原生矩阵passed=true，六组业务、四次旧TCP EOF无重放、18个旧host/worker出生身份回收；实际卸载保持与原环境恢复均通过。结合VPS官方身份systemd和历史公开包/平台/schema证据，独立审查确认关闭Phase4两项，39/42、Phase4为6/6。来源、失败观察顺序修正及未实测范围见[24终态](24-public-package-compatibility.md#installer-final-20261031)。随后按用户授权清理整个测试环境，不再保留专用身份等待其他门槛；清理收据结论见[24](24-public-package-compatibility.md#test-environment-cleanup-20261031)。

<a id="ci-node-races-20261105"></a>
## CI节点时序与取消原因修复（2026-11-05）

`41888706987`的`unit-internal (darwin, arm64)`失败于`TestTwoEmbeddedNodesAuthorizationEpoch`（15秒后仍needs_login）及`TestSDKPeerShutdownErrorRetainsNetworkCause`（对端关闭后得到本地Canceled）。其他macOS arm64取消任务的GitHub注释为“The job was not acquired by Runner of type hosted even after multiple attempts”与capacity constraints，不能混同为代码失败，也不能记为通过。

登录SDK的启动观察可在`requestLogin`获得状态发布许可前公开临时URL；调用方完成该URL后，worker可能再次发起注册。现在只在本次登录已明确接管/启动后公开URL并记录本次URL观察；不改变online、密钥、过期、撤销或身份授权判定。现有接管测试补齐启动前隐藏→接管后公开→真实完成的顺序，修前确定性失败；四项状态专项race通过，三个真实SDK登录/授权集成用例各重复10次race通过（44.266秒）。

对端关闭测试在本机10轮race中复现一次：probe已达到截止，但错误树仅保留context.Canceled。发送操作在transport子context被清理取消后没有回读caller原因。host及独立SDK发送路径现在保留caller的Canceled/DeadlineExceeded；SDK已关闭流的相同缺口同步修复。确定性回归修前失败，网络重试分类和原真实对端测试断言不放宽；错误诊断仅增加原因类型及布尔字段，不输出凭据或原始URL。完整包回归与远端最终状态在完成后补记。

本地最终验证：node整包race通过（49.820秒）；实际对端关闭用例15轮race全通过（71.111秒）；host connection与独立SDK coretransport整包race通过（2.448/6.493秒）；架构race通过（1.390秒），agent路线选择/截止专项race通过（1.950秒），三处受影响包vet及diff检查通过。首轮node全包暴露旧cached-Running夹具缺少loginStarted前提，已明确为已接管注册后复验通过，原断言未降低。 远端新提交尚待实际运行，不从本地结果推定整轮成功。

首轮远端复验保留失败：`c1d0c0b`的`unit-internal (darwin, arm64)`仍在初始注册超时，peer shutdown已通过。单独50轮本地旧修复验证未复现（90.348秒），因此不以本机重复通过代替远端结果。进一步对照固定SDK v1.98.10确认`tsnet.Start`已为新节点同步提交交互登录，而URL异步到达；第二次StartLogin会与首个注册及旧URL通知竞争。本次只对`NeedsLogin && !HaveNodeKey && Self无公钥`接管已启动注册；已观察URL仍接管，已有持久/过期身份无URL仍显式请求。接管不等于认证，不合成URL、不设置sawAuthURL或提前放行业务。

新增确定性测试修前因二次POST失败；四种状态覆盖无Self、零key、持久身份尚无map和过期身份。首次无key接管版本的实际授权epoch用例与状态回归20轮race通过（37.597秒）；最终HaveNodeKey约束与完整node回归单独验证。登录失败诊断只增加阶段、fixture主机名与状态布尔值，不输出URL/凭据。

最终四分支注册回归及node完整race通过（50.718秒），node vet与diff检查通过。GitHub查询遇到TLS/EOF连接中断，期间不推定尚未读到的远端结果；下一源码提交及远端补验继续单独记录。

补充远端来源：`17ed77fe9b34740744091034fcfa9909b12453de`的CI42017919620，macOS arm64内部包job127216098994已success；直接读取job日志确认首次注册四分支、授权epoch（1.25秒）与peer shutdown（4.08秒）均pass。整轮完成状态另行记录，不将单job成功当作全矩阵通过。

最终远端验证：CI42017919620已completed/success，57/57任务成功，来源固定`17ed77fe9b34740744091034fcfa9909b12453de`。完整四原生平台、WSL2、单元、真实插件集成、E2E、内嵌产物、Homebrew、终端及原生维护均完成；无取消/跳过任务替代必需验收。前文“仍待”保留当时过程，本段及文首是本批最终结论。验收记录提交仅修改文档，生产与测试源码保持该通过来源。

稳定复验继续：仅文档变化的`d49310d`触发CI42023610286，macOS arm64节点包再次通过，但Intel macOS的`TestSDKExplicitLoginAfterControlOutage`在等待新URL时15秒后仍authenticating。原首次注册及peer shutdown修复没有回退；新失败来自已有身份的显式恢复。20轮本机原测试通过（30.467秒）不替代远端失败。固定SDK的旧默认登录成功分支可覆盖同时排入的interactive goal；原runLogin每次重启SDK使控制面恢复与显式注册争用。真实SDK测试补充同实例/代际断言后修前确定失败（1.04秒），保留新URL、新node key和原machine identity要求。

本次仅在interactive且当前SDK持有效authorization lifetime时复用注册入口，BeginLogin保留该SDK观察代际，worker在opMu下再查资格；资格失效时回到原stop/start。Auth Key、取消/拒绝及无有效授权继续使用隔离SDK代际。旧Running仍不能完成新attempt，实际AuthURL/身份失效照常撤销旧授权；不增加重试或预算。边界与整包、远端验证完成后补记。

本批复用边界定向race通过（1.926秒）；真实控制面失联/显式恢复加同SDK断言20轮通过（218.717秒），初版复用node全包/架构race通过（60.671/1.448秒）。独立复核随后发现API失败终态会屏蔽晚到撤权通知、却保留旧epoch的问题：真实SDK回归用overlay旧错误路径确定失败（0.84秒），未回退共享源码。现在错误裁决在opMu内先撤销授权、清地址并记录失败，再stopLocked退役SDK/监听/连接；状态记录写失败也执行退役，只有当前未完成attempt可清理，后续worker不会被旧清理误关。三项定向race（含LocalAPI失败与叠加持久化失败两个真实场景）通过3.593秒，独立复审无剩余阻塞。最终node全包与远端结果继续按真实来源补记。

最终本地冻结回归：node整包race61.309秒、架构race1.458秒及node vet通过。远端CI42023610286已完成56/57，唯一Intel登录恢复失败保留；全部其他业务/平台/终端/维护任务成功。本批追加修复的新提交远端结果尚待，不以本地通过替代。


## CI tmux退出过渡状态修复（2026-11-05）

`ff338f9`的CI42028867098完成56/57，唯一失败为arm64 `TestRestartStableWindowQuietProcessAndExplicitStop`的空PID解析；Intel内部包及其余平台/集成/E2E/终端/维护成功。arm64日志确认显式断网恢复10.75秒、授权epoch1.17秒、peer shutdown4.43秒均pass，原认证失败没有重现。

对照tmux 3.8上游`format.c`与`server-fn.c`：关闭PTY使`pane_pid`为空，`pane_dead`还需`PANE_STATUSREADY`，故waitpid结果到达前可合法返回空PID和dead=0。此前仅允许dead=1缺PID的适配不覆盖此窗口。现在保留精确tmux身份，把该观察表示为退出原因未知及运行态`ExitPending`，不采样PID、不计稳定存活、不推断异常或安排自动重启；未确认的退出字段不授予异常退出证据。确认结果到达后仍按原退出原因升级。自动清理要求确认死亡，pending取消原预警/倒计时；原tmux原子删除guard保持。

新增会话回归修前确定失败（0.39秒），cleanup复核回归在缺guard时确定失败（0.53秒）；修复后三项回归（含原CI用例）各20轮race通过37.925秒。真实进程测试在原观察预算内区分临时pending与最终unknown，不把前者当永久暂停结案。整包、架构、vet及最终远端来源另行补记；本机tmux3.7b不替代远端3.8实测。

最终独立七类复核无阻塞；补充CaptureLogs前后比较ExitPending，禁止跨退出就绪状态的快照冒充稳定日志。AbnormalExit与DeleteDead显式拒绝pending，即使存在未确认旧字段也不执行变更命令；两项确定性回归修前红、修后绿。上游依据：[tmux 3.8 pane_dead](https://github.com/tmux/tmux/blob/3.8/format.c#L2257)、[pane_pid](https://github.com/tmux/tmux/blob/3.8/format.c#L2580)、[先关闭fd再等待状态](https://github.com/tmux/tmux/blob/3.8/server-fn.c#L373)。

最终冻结源码验证：tmux整包race8.209秒、session整包race21.688秒、架构race1.378秒，tmux/session vet及diff检查通过。三份当前CI摘要一致，原42项清单文字/顺序/勾选未变；下一提交远端仍须独立确认。

补充原生tmux 3.8实测：冻结`5254892b725c944ce44b981d69a85a467853d2af`，官方release tar SHA256 `e79c699c7e949dccd0a4a125e17b8d1261e311b16979dbc8eb34542f3966d82e`隔离构建（启用utf8proc、关闭jemalloc），实际server版本及专用0700 socket核验为3.8。tmux/session整包race9.673/22.178秒，71顶层、104含子项全pass，零fail/skip；命令33.871秒退出0。root逐份核验日志SHA256、JSON测试结果和清理收据，12个任务socket均无服务、私有socket根已移除，未改全局安装或PATH。证据目录`/var/folders/1n/q373cln10fqb0gl9z1ds5d8m0000gn/T/shiki-tmux38-native-4xsjqol1/logs`。这补充前文本机旧版本限制；远端全矩阵仍单独记录。

最终远端CI42034435896已completed/success，来源固定`5254892b725c944ce44b981d69a85a467853d2af`，57/57任务全部成功。arm64内部包job127272252065日志确认原tmux用例0.62秒、新过渡0.29秒、清理0.26秒，以及显式登录恢复0.69秒/授权epoch6.29秒/peer shutdown4.39秒均pass。全部四平台、WSL2、集成/E2E、内嵌产物、终端与原生维护实际完成，未用取消或skip替代必需任务。历史56/57失败保留。收尾提交仅同步08/15/16记录，生产与测试源码保持该通过来源；七类边界复核无新增变动，清单文字/顺序/42项勾选保持。


## 追加CI稳定性复验（2026-11-05起）

用户要求额外多轮完整验收。冻结886de18（与前轮5254892仅三份记录差异），先完整重跑CI42040152398，按attempt独立保存57项状态，不把旧attempt成功混入新轮。四平台compile/preflight/unit/integration/E2E/release/embedded/Homebrew保持；原生terminal/maintenance是Linux amd64及macOS arm64两目标，另有完整WSL2。

独立源码快照下三项原不稳定真实节点测试各50轮race：显式断网恢复、首次授权epoch、peer shutdown各50 pass，总150 pass、零fail/skip。单Go进程GOMAXPROCS=2/-p=1，包875.611秒、整体883.157秒退出0；最大单次11.02/2.01/5.12秒，内部预算未改。590个Go/module输入及源码归档哈希核验一致，6份证据SHA复核通过，原始JSON与verification.json位于`/tmp/shiki-node-stability-3vpkfxif/evidence`。该定向重复不替代完整平台CI。

2026-11-06追加失败定位：CI42040152398 attempt2的WSL2 job127341328488失败；本轮artifact13430625424确认internal/server TestPortAdmissionFirstFrameDeadlineAndCancellation/earlier-wire-parent在port_admission_test.go原198行、0.54秒失败。节点、tmux、原生systemd及清理均pass，product失败使WSL后续E2E/terminal/发行阶段未验，不计通过。公开摘要未保留call.err/body，不能声称已证明具体错误码。

源码复核发现独立明确竞态：admissionContextError连续两次ctx.Err取样，第一次nil、第二次DeadlineExceeded时错误返回Canceled。修复为单次快照后分类，FILE/PORT共用路径一并恢复DeadlineExceeded语义。确定性过渡回归修前两分支均Canceled失败，修后race通过1.557秒；原500ms真实wire子项在本机旧代码50轮race通过38.173秒，不替代WSL失败。该竞态与失败现象相符，但远端具体同因仍待验证；不延长原500ms/120ms/5秒预算、不接受错误Canceled或缺少终帧。

最终本地冻结回归：port500ms和file120ms较早parent截止各50轮race通过，100个真实wire子项零fail/skip（包37.747秒、命令41.384秒）；四份相关源码hash前后相同，证据位于`/tmp/shiki-port-deadline-q0o837yi`。server全包race36.444秒、架构race1.381秒及server vet/diff检查通过。远端旧轮继续完成，修复的新提交与连续完整成功结果另行记录。

旧源码886de18的attempt2最终56/57，唯一WSL2失败保留，其余56项全部成功；四平台8个E2E/重型集成job中的28条REQUIRED记录已逐份验证pass、无同名fail/skip。证据按attempt独立保存于`/tmp/shiki-ci-stability-20261105/42040152398-attempt-2`；本批新源码提交后的完整矩阵及连续轮次单独核验，不将旧通过继承给新提交。

最终artifact来源复核：本轮两Linux下游下载预检ID13429320871，两macOS arm64下游下载ID13430197338，四者均与本attempt上传ID/时间匹配，4份下载日志SHA及4平台上传元数据保存在artifact-audit.json，all_four_downloads_verified=true。该来源核对不替代WSL失败，也不把旧attempt结果算作新验收。

修复后第1轮完整成功：CI42063751769 attempt1，源码`e95ff1ca07719c6dac34a3a920a70447de4e8ba9`，57/57 success，无failed/cancelled/skipped任务。WSL artifact13434298539的source.commit吻合，原端口earlier-wire-parent0.54秒pass、FILE/PORT新回归pass；全部stage passed、incomplete_stages为空、cleanup passed，七个REQUIRED用例实际通过。四原生平台8个集成/E2E日志另核验28个必需用例pass，合计35条。四平台预检上传与四下游下载ID全部匹配本轮，metadata精度跨秒检查按秒级完成时间包含最后1秒处理，严格ID/attempt/SHA保持；初始审计误报记录保留，不涉及CI断言或预算变化。证据目录`/tmp/shiki-ci-stability-20261105/42063751769-attempt-1`。第二轮完整重跑固定同一源码，结果见下文。

修复后第2轮完整成功：CI42063751769 attempt2，源码仍为`e95ff1ca07719c6dac34a3a920a70447de4e8ba9`，57/57 success，无failed/cancelled/skipped任务。WSL artifact13438066604来源吻合，13个stage及cleanup均passed，七个REQUIRED用例、原端口earlier-wire-parent（0.53秒）和新增FILE/PORT回归均pass；四原生平台另外28条必需记录逐项通过。本轮Linux下游实际下载ID13435588184、macOS arm64下游下载ID13437302412，四下游与本attempt上传记录及来源全部匹配。证据目录`/tmp/shiki-ci-stability-20261105/42063751769-attempt-2`包含独立jobs/run、required-tests-result、wsl-verification和artifact-audit；短暂GitHub查询网络失败恢复后重新取得实际结果，不作为测试失败或通过。此提交仅同步三份记录，触发生产/测试源码不变的第3轮完整CI，结果待实际完成后单独报告。

本次文档批次七类复核：目录依赖与SDK/host/plugin归属、RPC准入/版本/能力/授权、持久状态/原子性/revision/去重、进程所有权/取消/恢复、文件/终端/网络语义与限额、CLI/TUI/配置/安装布局均无代码变化；测试证据按run/attempt及源码独立核验，失败历史保留。三份摘要保持一致，原42条清单文字、顺序与勾选不变，外部环境和公开发行边界未扩大。

2026-11-06继续复验发现第二类不稳定：文档提交`d71a235871cb9ef5b5977ba07ec054d19843986a`触发CI42079305988 attempt1，macOS Intel unit-cmd job127430373137的TestSmartActiveSessionPTY/manage在原8秒context期限失败（8.01秒），随后提前关闭PTY slave与仍在退出的renderer/input reader产生os.File.fd数据竞态。本机原manage测试50次race通过34.871秒，不能替代远端失败。代码检查发现测试发送q后不再读取PTY输出，且失败清理未join命令；可控真实PTY实验在q已处理后以65535字节final frame造成shutdown Write阻塞，恢复消费65592字节后约91毫秒正常退出。该实验确认输出背压机制，但输出量被主动放大，不宣称已精确重现原远端超时同因。

修正仅位于cmd/shiki/smart_session_test.go：等待退出期间持续消费PTY输出；失败清理先cancel、关闭master唤醒阻塞写并等待命令协程，再关闭slave。正常测试仍使用原8秒context，5秒只作失败清理上界，所有终端恢复、权限、会话与日志断言保留，不以取消/skip计通过。四种菜单动作各50次race共200次通过（包10.390秒）；生产实现、依赖、能力、持久状态、限额、CLI和发行布局均未变化。整包验证及远端最终结果另记。

本批最终本地验证：cmd/shiki整包race26.205秒、architecture race1.376秒、cmd vet及diff检查通过；UI原有取消/交接两项各10次race，共20 pass、0 fail/skip（14.489秒）。背压实验源码/日志SHA与20条实际结果已复核，改动文件hash及各日志hash记入`/tmp/shiki-ci-stability-20261105/smart-pty-verification.json`。七类增量复核无新增产品偏差：仅测试fixture输出消费和清理顺序调整，SDK/host/plugin边界、RPC/授权/能力、持久状态/原子性/revision/去重、产品进程/取消、文件/终端/网络契约、CLI/配置/安装布局均保持；原42项清单不变，失败证据及外部发行边界保留。

独立复核追加修正失败收尾：若5秒join清理界限也超时，仍标记测试失败，但禁止随后defer提前关闭slave，改为只在命令finished后由清理协程关闭；启动前失败仍正常关闭PTY。最终测试文件重新验证：四动作各50次race（200次，9.908秒）、cmd整包race26.935秒、architecture race1.141秒及cmd vet/diff通过，源码和日志hash记入`smart-pty-final-verification.json`。前述10.390/26.205/1.376秒结果对应追加修正前的测试快照，不冒充最终来源。旧远端轮次的四平台28条及WSL7条必需记录均已实际核验pass，WSL artifact13440499420的13 stages/cleanup和原端口/新增回归均通过；这些局部成功不消除macOS Intel CLI失败。

第三轮旧基线最终为CI42079305988 attempt1 56/57 success，唯一失败为上述macOS Intel unit-cmd，未出现其他失败/取消/跳过任务。四平台28条与WSL7条必需记录、WSL原端口及新增回归、四下游实际下载产物ID（13439873392, 13440730060）均完成本attempt来源核验；证据按该run/attempt独立保存。这仍是失败轮次，不计连续成功；本次测试fixture修正提交后重新按最终代码/测试基线累计3轮完整成功，不改产品实现、原超时或必需断言。

相邻PTY夹具增量复核：c96abf5推送后，确认UI取消/交接两项同样在退出后停止消费输出，失败清理未等待dashboard或attachment结束；CLI首次运行、目录及同步测试也未join调用协程，部分失败路径先关闭slave再取消context。该结论来自具体生命周期顺序，不宣称这些用例已在远端失败。仅补修测试：CLI四组共用smartPTYProcess，以finished等待与业务error分离，避免预期失败用例把等待超时算作正确错误；UI登记dashboard与attachment两个阶段，退出等待持续drain，清理cancel全部→关闭master唤醒写→join→关闭slave，清理超时仍失败并延后关闭活跃输入。保留原5/6/8秒期限、目录/权限/同步决定、终端状态及旧reader抢输入断言。当前CI42085722699来源c96abf5不含此工作区补修，单独记录，不重启现有运行。

相邻补修定向本地验证：CLI所有TestSmart用例20轮race通过9.582秒；UI取消/交接各20轮race，共40 pass、0 fail/skip，3.634秒（dashboard-owned-pty-20-verification.json）。生产代码、go.mod、RPC、能力与安装布局未改；整包验证和最终来源hash另记。

相邻补修最终冻结验证：cmd/shiki整包race20.878秒、internal/client/ui整包race1.424秒、architecture race1.376秒，cmd/UI vet及diff检查通过；6个测试文件源码hash与日志hash保存在`/tmp/shiki-ci-stability-20261105/pty-neighbor-verification.json`，40条UI实际pass已逐条复核。独立七类复核确认目录和SDK/host/plugin依赖、RPC准入/版本/能力/授权、持久状态/原子性/revision/去重、产品进程与取消所有权、文件/终端/网络契约与限额、CLI/TUI/配置/安装布局均无产品改动；只修正测试自身终端所有权与输出消费。三份摘要一致、原42条文字/顺序/勾选保持。当前远端c96abf5继续完成，不将其结果冒充这些工作区补修的验证；下一提交及完整连续成功另记。

c96abf5远端完整结果：CI42085722699 attempt1最终57/57 success，无failed/cancelled/skipped任务；四平台CLI的observe/create/manage/exit共16条实际pass，原macOS Intel manage0.08秒pass。四平台28条及WSL7条必需记录通过，WSL artifact13444890597来源吻合、13 stages/cleanup及原端口/FILE/PORT回归通过；四下游实际下载ID与本attempt预检上传一致。证据在`/tmp/shiki-ci-stability-20261105/42085722699-attempt-1`。本轮只证明c96abf5，不包含上述相邻工作区补修，最终基线连续轮次从补修提交后记录。

产物来源审计补充：macOS Intel本轮上传回执18:42:52.940 finalized artifact13443427092，runner step完成18:42:52Z，API created_at为18:42:54Z，旧按同一时钟+1秒比较误报已保留。改为强制本attempt上传回执位于对应runner step、精确产物ID、上传ZIP SHA256等于API digest、API run/source及下游实际下载ID全匹配；API创建时间来自不同时间来源，仅记录差异，不作为同钟假设。此为仓库外证据审计修正，不改CI任务、断言或超时，不把任何失败任务转为通过。

最终基线首轮外部调度失败：CI42092895871 attempt1，源码`bfdefc4798244ecd828747b1c07e42debb625461`，56项success、maintenance (darwin, arm64) job127493014582 cancelled。该job无runner、steps为空，排队19:52:18Z至20:07:19Z，GitHub annotations明确多次未获hosted runner及macOS arm64容量约束；因此不归因为产品测试失败，也不把取消算完整通过。四平台CLI16条（原Intel manage0.05秒）、必需集成/E2E28条及WSL7条实际通过，WSL artifact13447951551的13 stages/cleanup和原端口/FILE/PORT回归通过。来源、逐attempt jobs/run、runner-capacity-cancellation.json及原日志保存在`/tmp/shiki-ci-stability-20261105/42092895871-attempt-1`；本轮仅3个下游运行，第四个取消不得宣称4/4验证。生产/测试基线不再修改，下一次完整重跑单独核验。

最终基线第1轮完整成功：CI42092895871 attempt2，源码仍为`bfdefc4798244ecd828747b1c07e42debb625461`，57/57 success，无failed/cancelled/skipped任务。四平台CLI菜单16条实际pass，原Intel manage0.08秒；四平台28条加WSL7条必需集成/E2E均实际pass，WSL artifact13451181404来源吻合、13 stages/cleanup及原端口/FILE/PORT回归通过；四下游下载ID（13448798043, 13449282711）与本attempt上传ID/ZIP digest/API run及源码全部对应。证据目录`/tmp/shiki-ci-stability-20261105/42092895871-attempt-2`，CLI/WSL/required/artifacts四组已核验。GitHub查询网络暂时EOF后自动恢复并取得实际结果；前一轮容量取消保留，不计成功。随后同一代码/测试基线全矩阵attempt3继续验证。

最终基线第2轮完整成功：CI42092895871 attempt3，源码仍为`bfdefc4798244ecd828747b1c07e42debb625461`，57/57 success，无failed/cancelled/skipped任务。四平台CLI菜单16条实际pass，原Intel manage0.10秒；四平台28条加WSL7条必需集成/E2E均通过，WSL artifact13459558799来源吻合、13 stages/cleanup及原端口/FILE/PORT回归通过；四下游实际下载ID（13460090103, 13460110186）均匹配本attempt上传ID、ZIP digest与API run/source。全部证据独立保存在`/tmp/shiki-ci-stability-20261105/42092895871-attempt-3`。最终代码/测试基线已连续两轮完整成功，容量取消attempt1与旧源码失败继续保留。此提交仅同步三份记录，触发同一生产/测试代码的第3轮完整验证；结果待实际完成后单独报告。

本次记录批次七类复核：目录及SDK/host/plugin依赖、RPC准入/版本/能力/授权、持久状态/原子性/revision/去重、进程所有权/取消/恢复、文件/终端/网络语义限额、CLI/TUI/配置/安装与发行布局均无实现修改；每轮实际测试及产物按attempt和源码独立核验，取消/skip不算通过。三份当前摘要一致，原42项清单文字、顺序和勾选不变；不扩大外部验收和发行声明。

第三轮追加发现登录等待边界：CI42126955938 attempt1，来源`6a86188605890999f2507fae5df2a20174135f5c`，macOS Intel unit-cmd job127595847542的TestPrepareTailnetLoginKeepsAcceptedAttempt/parent_deadline在原350ms处失败，实际错误为deadline_exceeded: operation deadline exceeded，缺少既有login is pending提示。原PTY四动作均实际pass（manage0.18秒），不是PTY失败复发。prepareTailnetLogin在ticker等待收到Done时返回pending提示，但若正在GetStatus RPC内截止则直接返回RPC error，存在依赖调度时机的行为分歧。原始日志与hash、失败结果及PTY通过记录保存于`/tmp/shiki-ci-stability-20261105/42126955938-attempt-1/login-deadline-failure-summary.json`。本轮不计完整成功，保留前两轮历史通过；按02/05/13契约修正截止提示与原因，不重启或取消已接受的登录，不扩大RPC授权、不延长等待期限，确定性回归及最终验证另记。

本轮登录等待补修：prepareTailnetLogin与显式node login统一处理本地等待结束和在途GetStatus中断，仅本地context实际结束且RPC为Canceled/DeadlineExceeded时恢复pending提示及原始cause；独立服务端截止和真实PermissionDenied/Unauthenticated/Unavailable/Internal继续保留。node login先保存成功的next，错误不再清空最后已接受状态；不新增BeginLogin/CancelLogin、不延长原350ms父截止、CLI15秒等待或100ms轮询。最终相同真实Unix/HTTP2回归在隔离旧生产源码下五个本地中断分支确定失败、八个真实RPC错误控制通过；修后18分支各20轮race共360子项pass、零fail/skip，61.552秒，包含原parent_deadline20次通过。受控在途RPC以释放门区分本地截止与wire毫秒编码可能更早的服务端截止，不把独立远端错误误判本地取消；证据见`/tmp/shiki-ci-stability-20261105/login-final-polls-verification.json`及`login-final-wire-before.jsonl`。

独立终端复核补修：runTerminal收到不可重试终端错误后，verbose日志读取阶段可能使父context结束，原无条件context检查会覆盖已有认证拒绝。修正为保留已取得的不可重试结果，网络重试仍由context终止；正常detach、Claude恢复、输入恢复及五次退避策略不变。冻结旧源码真实IPC/PTY回归四个鉴权错误分支失败、两个取消控制通过；最小补修后六场景各10轮race共60子项pass、零fail/skip，24.237秒，同时断言仅一次attachment、无重连、终端恢复及实际取消原因。此问题来自独立复核，不冒充远端登录失败同因；证据为`/var/folders/1n/q373cln10fqb0gl9z1ds5d8m0000gn/T/shiki-attach-error-xo92rimo/evidence/verification.json`。

本批最终合并冻结验证：cmd/shiki整包race25.974秒、internal/client整包race17.264秒、architecture race1.394秒，cmd/client vet及diff检查通过。六份改动生产/测试文件在验证前后hash一致，日志与收据为`/tmp/shiki-ci-stability-20261105/combined-cli-verification.json`。独立七类复核通过：目录及SDK/host/plugin依赖不变；RPC传输、准入、版本/能力及权限不变，实际拒绝仍原样保留；持久schema、原子性、revision及去重不变；已接受登录归属、进程取消及恢复不变，释放门失败路径也回收；文件/终端/TCP限额及输入恢复保持；CLI只恢复既定等待诊断并保留错误原因，配置/安装/发行布局不变；原红新绿、重复race及旧失败来源分别记录，原42项清单文字/顺序/勾选不变。三份摘要同步，新提交远端每轮另行核验；CLI证据审计增加原登录边界及新回归，后续四平台共144条必需CLI子项，不仅检查job总状态。

旧轮最终结论：CI42126955938 attempt1、源码6a86188最终56/57 success，唯一macOS Intel CLI登录等待失败，未有其他失败/取消/跳过job。WSL artifact13460949939的13 stages/cleanup、原端口及FILE/PORT回归和七项REQUIRED通过，另四平台28条必需集成/E2E记录实际通过；四下游下载产物ID均与本attempt上传ID、ZIP digest及API run/source匹配。完整jobs/run、失败原日志、required与WSL验证及artifact-audit按本轮独立保存。此结果仍是失败，不计为最终连续成功；本批新生产/测试源码推送后重新累计完整轮次。

追加依赖扫描阻塞与修复：提交0642f71触发CI42132144143 attempt1，四原生preflight均被GO-2026-6865阻断，日志明确compress v1.18.7、fixed v1.20.2。官方公告发布于2026-11-05T22:54:32Z，涉及fse/huff0双block解析panic；实际控制面链为internal/node→tailscale/tsnet/controlclient→zstdframe→compress/zstd→huff0.ReadTable，因此升级实际使用的间接依赖，不降低package级扫描标准。go.mod仅将compress提升至1.20.2、go.sum增加对应两条校验；新模块要求Go1.25，项目Go1.26.10满足，未升级Tailscale或引入其他依赖。旧checksum保留不表示选中旧版本，无关tidy差异未应用。

本轮旧CI最终为8个success、5个failure、12个未展开矩阵的skipped job组，不是57项完整通过。第五个失败为WSL artifact13463777143，源码0642f71匹配、ZIP hash匹配上传回执、cleanup passed，但只有preflight阶段通过，其余产品/维护阶段未验；公开证据不含具体错误，不能断言与漏洞同因。原生四份扫描原日志、WSL失败摘要和jobs/run保留于`/tmp/shiki-ci-stability-20261105/42132144143-attempt-1`，本轮不计连续成功。

升级后相同govulncheck v1.1.4 -scan=package ./...根module及独立analyzer均退出0；根module无受影响的已导入包，仍保留三条module-only公告，不称全部依赖无漏洞。上游TestDecompressCountOverflow和tailscale zstdframe TestDecodeMaxSize实际race通过、零fail/skip，go mod verify通过，证据为`/tmp/shiki-ci-stability-20261105/compress-v1202-verification.json`。首次显式GOTOOLCHAIN=local使用系统Go1.26.3，在加载前因最低版本要求退出、未实际扫描，改用已缓存Go1.26.10后完成实际扫描；该预检失败日志保留。

依赖升级最终冻结验证：全仓build25.068秒、vet16.463秒及模块校验2.849秒通过；internal/node整包race61.977秒、internal/client17.126秒、cmd/shiki25.841秒、architecture1.450秒全部通过，包含真实控制面zstd双节点及刚补修的CLI回归。模块文件及CLI源码验证前后hash一致，证据为`/tmp/shiki-ci-stability-20261105/dependency-combined-verification.json`。本批七类复核通过：只升级既有间接解码依赖；目录和SDK/host/plugin归属、RPC传输/准入/版本/能力/授权、持久状态/原子性/revision/去重、进程启动取消恢复、文件/终端/TCP限额、CLI/TUI配置安装及公开发行均未改契约；实际安全扫描和本地行为证据与待验远端平台分开。原42条清单保持，08/15/16同步，不以skip关闭本轮验证；下一提交及连续完整矩阵另记。

依赖修复87fa715首轮CI42133128357 attempt1最终10项success、2项failure、1项cancelled、12组未展开矩阵skipped：Linux arm64 preflight在package加载时从proxy.golang.org下载gvisor ZIP收到HTTP503，扫描未完成，不是新的漏洞finding；另外三原生平台根与analyzer扫描实际通过。WSL artifact13464846172的上传/API/ZIP digest及source87fa715匹配，rootfs/Go下载校验和cleanup通过，但停留在WSL2-import阶段（包含目录创建、import及路径转换），未执行产品测试，底层私有错误未导出，不能断言具体命令或与503同因。该两项证据分别为proxy-failure-summary.json与wsl-failure-verification.json。

macOS Intel preflight job127614458663最后触发15分钟job总上限，GitHub annotation明确The job has exceeded the maximum execution time of 15m0s；Build and lint在23:59:35Z至00:09:08Z完成后，现有10分钟上限的race cache准备仅运行2分27秒即被整体取消。最终取消记录和原日志保存于`/tmp/shiki-ci-stability-20261105/42133128357-attempt-1/macintel-preflight-cancellation.json`。这不是业务测试超过自身期限；本轮仍不能算完整成功。按实际构建耗时修正preflight任务总预算，并在扫描前对依赖下载做有限重试，所有功能测试、扫描门槛及原阶段上限不变；具体差异与验证另记。

CI预检收敛完成：仅将preflight总任务预算15→30分钟，使现有构建/静态检查和race缓存阶段有完整执行时间；原race缓存10分钟上限及全部功能测试期限/断言不变。扫描前新增5分钟上限的模块下载步骤，根module和独立analyzer各最多三次，失败后仅2/4秒退避，第三次保留实际非零退出码；不重试扫描或测试，不设置continue-on-error，默认proxy/sumdb与readonly保持。此预算针对CI准备执行量，不修改产品性能或等待要求。

提取实际workflow shell以go/sleep stub验证四路径：双方第三次才成功、根三次全失败返回23、analyzer三次全失败返回29、首次成功不重试，均符合调用上限及错误传播；这是脚本行为验证，不冒充真实网络恢复测试。actionlint（含shellcheck）、make test-tools 83项、platform工具30项及diff检查通过，workflow SHA冻结。证据为`/tmp/shiki-ci-stability-20261105/preflight-download-retries/verification.json`。独立七类复核通过：仅CI下载和总运行预算，目录/SDK/host/plugin依赖、RPC准入/版本/能力/授权、持久状态/原子性/revision/去重、生产进程/取消/恢复、文件/终端/TCP契约、CLI/TUI/配置/安装/发行布局均未改；测试和扫描门槛保持，失败/取消/skip仍不算通过。三文档同步、原42项清单保持；新CI提交后的连续完整57/57轮次另行核验。

最终CI基线第1轮完整成功：CI42135628202 attempt1，源码`fd78178a4afb3ef2ad3492cd942c374740cb8af3`，57/57 success，无failed/cancelled/skipped任务。四原生平台各36条关键CLI子项共144条实际pass，含原parent_deadline、登录/终端新增边界及PTY四动作，原macOS Intel parent_deadline0.35秒、manage0.08秒。四平台28条及WSL7条必需集成/E2E均通过；WSL artifact13465279553来源吻合，13 stages及cleanup均passed，原端口earlier-wire-parent0.52秒及FILE/PORT回归通过。四下游实际下载ID13466460349与13465216931均匹配本attempt上传ID、ZIP digest及API run/source。证据目录`/tmp/shiki-ci-stability-20261105/42135628202-attempt-1`的cli/wsl/required/artifacts四组核验完成；历史失败保留，此后同一生产/测试/依赖/CI配置基线执行完整attempt2。

本次证据记录七类复核：目录/SDK/host/plugin归属、RPC准入/版本/能力/权限、持久状态/原子性/revision/去重、进程所有权/取消/恢复、文件/终端/网络限额、CLI/TUI/配置/安装/发行布局均无实现变动；实际测试与产物按attempt/source独立核验，跳过不算通过。三份当前摘要一致、原42项清单文字/顺序/勾选保持，外部平台与公开发行边界不扩大。

最终CI基线第2轮完整成功：CI42135628202 attempt2，源码仍为`fd78178a4afb3ef2ad3492cd942c374740cb8af3`，57/57 success，无failed/cancelled/skipped任务。四平台144条关键CLI实际pass；四平台28条与WSL7条必需集成/E2E通过；WSL artifact13481765494源码吻合、13 stages及cleanup全部passed，原端口earlier-wire-parent与FILE/PORT回归亦通过。四下游实际下载ID13480426112与13480208012均匹配本attempt上传ID、ZIP digest及API run/source，全部证据独立保存在`/tmp/shiki-ci-stability-20261105/42135628202-attempt-2`。至此最终代码/测试/依赖/CI配置连续两轮完整成功；过去失败、取消及环境准备错误仍保留。本次仅提交三份记录，触发同基线第3轮完整验证，实际结果另报，不提前计为第三轮成功。

两轮记录批次七类复核：目录与SDK/host/plugin归属、RPC传输/准入/版本/能力/授权、持久状态/原子性/revision/去重、进程所有权/取消/恢复、文件/终端/网络语义与限额、CLI/TUI/配置/安装/发行布局均无新增实现变动；测试和产物按run/attempt/source逐项核验，原期限与必需断言保持。08/15/16当前摘要同步，原42条文字、顺序及勾选不变，不扩大外部环境或发行声明。

最终基线第三轮出现新的真实节点边界失败：CI42174388098 attempt1、源码b7c51bd，macOS Intel unit-internal job127750940301的TestSDKExplicitLoginAfterControlOutage在10.96秒触发control_outage_test.go336的机器身份保持断言；原45秒context尚未到期，同SDK/generation检查此前已通过。该断言合并current为空、previous为空与Machine不等三种情况，公开失败行不足以确认究竟是身份丢失还是状态观测时序，不能直接归因。原始日志和hash保留于`/tmp/shiki-ci-stability-20261105/42174388098-attempt-1/internal-failure-summary.json`；保持原身份要求与deadline，确定性分析和修正另记。本轮四平台144条CLI回归仍实际pass，包括原登录父截止、终端鉴权边界与PTY四动作；不能以这些局部成功抵消节点失败，前两轮完整通过保留为历史证据。

第三轮最终结果为CI42174388098 attempt1 56/57 success，唯一失败是上述macOS Intel节点恢复身份断言；没有其他失败、取消或跳过job。四平台144条CLI、四平台28条加WSL7条必需集成/E2E全部实际pass；WSL artifact13484015373来源吻合、13 stages/cleanup及原端口/FILE/PORT回归通过，四下游产物ID/digest/run/source核验完成。这些局部通过独立保存，不抵消节点失败。

真实SDK时序调查证据：原恢复测试仅细分诊断后20轮race全部通过，未重现远端自然时序。另以同一真实控制面/SDK将新注册的首个完整MapResponse暂存，先发送协议允许的KeepAlive，确定性观察到Running、Online、空AuthURL却仍携带旧Self公钥，且控制面旧key索引已被迁移删除。旧生产在该状态提前online并恢复授权，真实屏障回归确定失败；释放完整新map后再核对Machine不变及node key轮换。KeepAlive只放大合法中间窗口，不冒充已确定原CI合并断言的具体失败分支；证据位于`/tmp/shiki-ci-stability-20261105/node-control-recovery`。

修复按13的“旧Running不得完成新attempt”要求收紧完成条件：已知旧Self时，仅看到URL清除不再作为注册完成证据，必须观察到不同的非零Self；首次无旧Self仍在fresh非零Self时完成。requestLogin继续采用已有URL、零重复POST；真实TestSDKPendingInteractiveURLCompletesWithoutAnotherKeyRotation原样保留，证明同一次待注册key不因重开URL再次轮换，与旧已授权Self公钥不是同一概念。旧fake把cached old Self当作待注册key并允许URL清除即完成的期望改为先保持pending/unauthorized，再见新Self才完成，同时保留URL呈现、零重复注册、机器身份和原期限断言。对已知旧Self的同key观察保持pending至原截止，不能宣称仅凭URL消失证明同key重新认证；没有新增SDK私钥读取、反射、TestHooks生产依赖或通知watcher。

该生产收紧后的初次整包冻结验证已通过：node race72.015秒、client17.153秒、CLI25.730秒、architecture1.361秒及相关vet4.818秒；对应源码和日志哈希保存在node-control-recovery/combined-verification.json。最后取消回归将同时保留原offline-poll场景和新增live-old-map场景，定向重复及补齐测试后的最终node验证另记，不把这份快照验证冒充后续测试文件来源。

本批最终验证完成：相同旧生产node.go overlay下五类回归顶层均失败，原not-started/same-key/new-key安全对照仍通过；新生产九项定向各10轮race共90顶层及40子项pass、零fail/skip，包343.981秒。最后取消测试以table保留原offline-cached-map、追加live-poll-old-map；旧生产前者pass后者fail，最终生产两分支各10轮共20子项及10父项pass。补齐后的最终node整包race178条（57顶层）全部pass、零fail/skip，包72.196秒/命令73.611秒，node vet1.146秒及diff检查通过。生产node.go从定向至最终补齐验证hash相同；测试增量来源分开记录，统一收据为`/tmp/shiki-ci-stability-20261105/node-control-recovery/verification.json`。

本批七类复核通过：目录/SDK-host/plugin依赖归属不变；仅收紧当前注册完成与再次授权时机，旧Self+清URL不得提前完成/吞掉取消，RPC传输/权限/能力不扩大；持久schema、原子性、revision和去重保持；SDK复用、代际、取消/完成锁裁决及原租约在单纯控制面失联期间的保留不变；机器身份、节点key轮换、文件/终端/TCP及原45秒/15秒等期限不改；CLI/配置/安装/发行布局不变。真实屏障、原红新绿、重复及整包验证分别有证据，原CI具体合并分支仍未确证，不以合成测试改期望冒充设计放宽。三文档摘要同步，原42条文字/顺序/勾选保持；下一提交远端新增四平台40条关键node记录审计，与CLI144、必需集成/E2E35、WSL13 stages/cleanup及四下游产物核验共五组，最终连续完整57/57另行累计。

增量边界复核：本批增强的是previousKey已知时的完成证据；previousKey为零仍沿用既有首次fresh非零Self语义，未增加SDK注册epoch接口，也不扩展该分支的注册代次保证。只读检查没有给出此分支的具体复现或失败证据，不把理论回调时序当作已证实缺陷或本批已解决事项。最终取消table复核确认原offline场景和新live-old-map场景均保留。

节点映射修复后的两轮完整成功：源码`09adb4a7b778525a795e1aa9bc0d35f6ed85fa23`，CI42181018164 attempt1与attempt2均57/57 success，无failed/cancelled/skipped任务。每轮四原生平台144条CLI及40条关键node记录均实际pass，含原控制面恢复用例、真实SDK映射屏障、待登录URL复用和取消边界；四平台28条与WSL7条必需集成/E2E均通过。attempt1的WSL artifact13486272865源码吻合、13 stages及cleanup全部passed，四下游实际下载ID13485203800,13485790174均匹配本attempt上传ID、ZIP digest及API run/source。attempt2的WSL artifact13487803711源码吻合、13 stages及cleanup全部passed，四下游实际下载ID13487337113,13487616767均匹配本attempt上传ID、ZIP digest及API run/source。五组证据按run/attempt独立保存于`/tmp/shiki-ci-stability-20261105`。历史失败保留，不将其转为成功；本次仅同步三份文档，触发同代码/测试/依赖/CI配置第3轮完整验证，结果待实际完成后另报。

本次记录批次七类复核：目录与SDK/host/plugin归属、RPC传输/准入/版本/能力/授权、持久状态/原子性/revision/去重、进程所有权/取消/恢复、文件/终端/网络语义与限额、CLI/TUI/配置/安装/发行布局均无实现变更；测试和产物按attempt/source逐项核验，原功能期限与必需断言保持。08/15/16摘要同步，原42条文字、顺序和勾选不变，不扩大外部环境、首次零previousKey观测或公开发行保证。

<a id="plugin-manifest-refactor-20261111"></a>
## 插件 Manifest 与 UI 注入实现记录（2026-11-11）

以下为 Phase 5 后扩展——插件 manifest 声明式 UI 注入重构的已实现项：

| 组件 | 文件 | 状态 |
|---|---|---|
| PluginManifest JSON 类型及 ValidateManifest | pkg/plugin/manifest.go | ✅ 已实现 |
| ConfigField schema 及 ValidateConfig 预校验 | pkg/plugin/schema.go | ✅ 已实现 |
| UIContribution、StatusPayload、NotifyPayload 类型 | pkg/plugin/ui_contribution.go | ✅ 已实现 |
| ActionRequest/Result 类型及 ActionImplementation 接口 | pkg/plugin/action.go | ✅ 已实现 |
| Plugin.InvokeAction 生成 gRPC 绑定 | proto/plugin.proto、pkg/protocol/plugin/plugin_grpc.pb.go | ✅ 已实现 |
| AgentActionsClient/Handler Connect service 非生成扩展 | pkg/protocol/agent/agentconnect/agent_actions.go | ✅ 已实现 |
| UIActionCapability = "ui.action.v1" 能力常量 | pkg/plugin/capabilities.go | ✅ 已实现 |
| Lifecycle.InvokeAction 及 runtime 实现 | pkg/plugin/lifecycle.go | ✅ 已实现 |
| grpc.go 注册生成 PluginServer；lifecycleClient.InvokeAction | pkg/plugin/grpc.go | ✅ 已实现 |
| host.go Manifest 扩展 PluginManifest 字段 | internal/plugin/host.go | ✅ 已实现 |
| PluginRegistry 第三方插件目录扫描 | internal/plugin/registry.go | ✅ 已实现 |
| SettingsRegistry 活跃插件 UI 注入记录 | internal/plugin/settings.go | ✅ 已实现 |
| ActionRouter 路由 InvokePluginAction 到 Supervisor | internal/plugin/action_router.go | ✅ 已实现 |
| InvokePluginAction agent 层接入，ipc.go 拦截器扩展 | internal/agent/ | ✅ 已实现 |
| 动态 CLI 命令从 manifest 注册 | cmd/shiki/dynamic_commands.go | ✅ 已实现 |
| plugins/sync 静态插件声明 | plugins/sync/manifest.json | ✅ 已实现 |
| plugins/port 静态插件声明 | plugins/port/manifest.json | ✅ 已实现 |

**待实现（TUI/GUI 渲染，下一阶段门槛）：**

| 组件 | 状态 |
|---|---|
| TUI 状态栏状态槽渲染（订阅 plugin.status.<id>.v1，展示 StatusPayload.text/state） | ⏳ 待实现 |
| TUI/GUI dashboard 面板区渲染（UIDashboardPanel 声明） | ⏳ 待实现 |
| 设置界面从 ConfigSchema 自动生成表单 | ⏳ 待实现 |

以上待实现项不影响 CLI 通道的 InvokeAction 路径和第三方插件注册能力；InvokePluginAction 可通过 `shiki plugins <id> <command>` 实际调用并返回文本/JSON 结果。本批不修改原 42 条清单计数，不替代任何既有验收门槛，不扩大外部环境或发行声明。
