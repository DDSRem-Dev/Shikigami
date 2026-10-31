# 官方 tsnet 外部验收

**当前状态（2026-10-31）：** 未使用Auth Key自然到期测试已于15:56明确拒绝，并完成专用key、进程、job、二进制及私有状态清理，详见[终态记录](#unused-auth-key-expiry-final-20261031)；c4双平台长测也已完成。原Mac/VPS两节点的认证身份仍为后续认证/安装验收保留，本批未重新查询其在线状态，也未清理该环境。路线图37/42，完整认证项仍未关闭；下面早期记录保留各自时间和源码范围。

2026-10-30：用户已提供官方 tailnet、已登录的 Chrome，以及家庭宽带 Mac 与不同网络的 VPS。首次真实网络批次已关闭08的Phase 2首条；该批当时32/42，10项部分完成。其余认证矩阵和平台门槛不随此项关闭。后续官方节点移除暴露旧流残留，peer生命周期修复已有本地回归证据；第三次重新认证已online，随后不含诊断代码的最终构建重启及应用重连通过；官方再次移除已验证旧附件回收与主节点不受影响；自身状态分类与显式恢复的本地修复及分层回归已完成；旧63924ce诊断构建已重新认证并恢复应用连接，candidate4 clean重启同地址online。最终候选已完成两个独立的官方控制面受控断网场景：已有终端保持及控制恢复、无持有终端时显式connect复用授权。首轮混入主动连接重配的失败记录保留，最新补验已完成最终agent再次撤销恢复和独立未使用Auth Key撤销，99bee22六job CI通过；独立自然到期现已完成（见文末），审批待批/移除/批准及恢复原设置已实测，完整认证矩阵未关闭。

## 来源与隔离范围

主链路使用经过可信仓库公钥重新验签的公开 `v0.0.1-alpha.2`，源码 `fa35debd9c74d0ad44704e3a7897e0e0c1fcc3f2`，Go1.26.10、Tailscale SDK1.98.10。Mac为Apple Silicon/macOS27.0.1；VPS为Debian12、原生x86_64、Linux6.1.0-10-amd64/systemd252/tmux3.3a。VPS daemon使用专用普通用户及隔离状态目录，通过独立transient systemd service运行。既有Docker/Web/RustDesk服务未修改；没有重启主机、启用开机服务或改全局防火墙。

主业务流的CLI、agent、daemon及双端插件均为原公开包。登录修复另外编译：CLI为 `v0.0.1-alpha.2-local.tsnet.1`，用于key首用及登录观察；key复用复验的独立agent为 `v0.0.1-alpha.2-local.tsnet.2`。不能把工作树修复追认为公开alpha.2已包含，也未移动tag或替换已签名资产。

临时私有证据位于 `/tmp/shiki-tsnet-3vsvzo1b/`；本机测试状态保存在用户私有tsnet测试目录。密钥、登录URL、SSH密码和节点私有身份不进入仓库。本页只保留脱敏结论，临时证据不可取得时不扩大这些结论。

## 实际结果

| 场景 | 结果与证据 | 边界 |
|---|---|---|
| Mac/VPS官方交互登录 | 两端online，节点名和内嵌地址正确；`mac-first-online.json`、`vps-first-online.json` | 原CLI早退错误已保留，用户通过后续状态中的URL完成认证 |
| 两端应用进程重启 | `_machinekey`、`_current-profile`与profile记录摘要不变，同地址online且应用重新认证连接；`restart-identity-verification.json` | 不等于OS重启或用户重新登录 |
| P2P | 认证后`tailscale-direct`、attempts仅`tsnet`；`connect-result.json` | 无direct TLS地址，无SSH业务流 |
| 终端、同步、TCP | 普通用户shell命令返回正确；首次远端文件显式keep-remote下载，4MiB本地文件显式keep-local上传后经TCP HTTP读回，SHA256一致；`initial-sync-and-forward.json`、`p2p-terminal-sync-tcp.json`、`p2p-upload-tcp-4mib.json` | shell-only；没有真实Claude登录/调用，未据marker宣称完整终端视觉验收 |
| DERP | 仅Mac测试agent设置SDK开关`TS_DEBUG_ALWAYS_USE_DERP=true`，禁用该进程UDP；认证路径为`tailscale-relay`，同一远端会话终端及双向变化同步通过，4MiB TCP读回SHA256一致；`derp-connect.json`、`derp-terminal-sync-tcp.json` | 真实DERP传输，不是自然网络切换、既有流无中断或所有代理兼容证明 |
| 恢复正常路径 | 正常关闭测试agent，移除DERP开关后启动，重新认证连接为`tailscale-direct`；`after-derp-connect.json` | VPS及系统网络设置未改 |
| 一次性Auth Key首用 | 非复用、非ephemeral、1天有效期的key从0600文件读入，独立新节点3.991秒online；`auth-key-first-use.json` | key有效期不等于已登录节点的node-key有效期 |
| 同key第二个新节点 | 原alpha.2 agent保持needs_login直到30秒CLI超时；Chrome显示该key已失效、类型Single-use，第二节点未加入。修复后2.233秒返回denied/INVALID_AUTH_KEY；`auth-key-reuse.json`与`auth-key-reuse-patched.json` | 保留原失败，不把“未上线”单独算明确拒绝通过；已消耗key不能再证明自然到期拒绝 |
| 无效Auth Key | 随机生成且从未签发的key由独立节点提交官方控制面，7.627秒明确denied/INVALID_AUTH_KEY；临时key文件删除、测试agent正常停止；`auth-key-invalid.json` | 不冒充真实过期或撤销key |
| 应用登录尝试到期 | 取得官方URL后不使用，实际10分钟attempt截止后返回needs_login/LOGIN_EXPIRED；`interactive-expiry-start.json`、`interactive-expiry-result.json` | 不等于控制面URL令牌或Auth Key的自然到期 |
| 交互取消 | 修复CLI/agent实际取得官方URL后按login_id取消，返回needs_login/LOGIN_CANCELED，无地址；`interactive-cancel.json` | 没有自动打开或保留URL；不等于URL自然过期 |

## 发现及修正

1. CLI仅在authenticating状态轮询，SDK在URL生成或key完成前发布NeedsLogin时过早退出。CLI及添加服务器向导现在继续等待有ID、未过期且reason为NODE_AUTH_REQUIRED的有效attempt；已接受attempt不重复BeginLogin，取消/拒绝/过期仍终止，原有超时不变。
2. SDK部分控制面HTTP注册错误只通过health `login-state`报告，不进入ErrMessage。NodeControl现在读取该结构化warning的Error参数，并订阅初始health快照，避免订阅前错误丢失；只识别明确key拒绝，网络/代理/限流/服务端故障不猜为无效key。只终止当前代际非交互attempt，原始错误不输出或持久化。实际官方复用复验通过。
3. 本机系统HTTP/HTTPS/SOCKS代理为本地代理端口；未传代理环境的隔离agent未取得登录URL，显式传HTTP_PROXY/HTTPS_PROXY后成功。SDK该版本在此Darwin路径没有系统代理回退；未修改系统代理，不把观察差异扩大为全部网络根因已证明。Shiki业务h2c仍固定tsnet dialer且禁用环境代理。
4. 最初隔离目录未部署插件，功能按契约不可用。复制并校验对应平台的公开插件后，两端显式启用。Mac用户主目录0775触发插件父目录安全检查，测试插件迁至0700隔离目录后成功；没有放宽检查或修改用户主目录权限。插件启用后的能力变化通过显式重新connect重新协商。端口server显式allowlist仅允许测试loopback端口，client仅绑定127.0.0.1。

## SDK附属监听

VPS普通网卡出现SDK PeerAPI TCP监听。核对固定SDK `ipn/ipnlocal/peerapi.go`：netstack模式使用空IP在普通网卡监听，端口由tailnet IP派生；HTTP处理前调用`WhoIs`校验来源。实际从Mac连接VPS公网该端口TCP成功，但未认证GET收到零字节后关闭，证据`peerapi-public-probe.json`。

它不是Shiki `:9527` RPC、NodeControl或SDK LocalAPI。不得宣称公网端口不可达，也不能由一次拒绝推定所有PeerAPI请求安全；最终安全审查保留该SDK附属暴露面。未添加影响未经验证的`ts_omit_peerapiserver`构建标签。

## 回归与剩余门槛

- CLI与向导真实UDS/HTTP2/PTY回归先红后绿，完整cmd/shiki race通过21.349秒，vet通过；日志 `/tmp/shiki-tsnet-cli-wizard-package.log`。
- NodeControl真实SDK HealthTracker→LocalAPI bus回归修前失败，专项race12.873秒、完整internal/node race157.044秒通过，无skip、未重启测试；vet通过。日志 `/tmp/shiki-node-key-{health-red,health-race,full-race,vet}.log`。
- 登录修复阶段原生架构测试0.415秒通过；后续peer适配审查架构测试0.404秒通过，不重复成功检查。七类源码/文档复核见16。此前文档CI40923247799、40924428349均已确认成功；peer修复精确63924ce的CI40940419677现已成功，范围及skip边界见下文。

**早期批次的剩余记录（后续结果见下文）：** 当时仍需独立未使用key的撤销/自然过期、节点审批等待/拒绝/批准、URL自然过期及完整控制面离线分类。测试用已消费key不能替代这些场景。节点移除原失败、官方旧附件回收、后续重新认证和不同候选的证据见下节；完整自身撤销分类与最终候选再次移除后的恢复仍待；官方受控断网的两个限定场景已通过，不扩大为所有离线分类或自然网络切换通过。

## peer撤销修复与复验边界

用户在最终操作时确认后，已通过Chrome移除专用一次性节点，当时控制台只剩主Mac/VPS两节点。原alpha.2移除后新请求5秒超时、旧终端最终canceled退出，路由offline/RECONNECT_EXHAUSTED且attempts仍仅tsnet；主Mac业务保持成功。但原agent仍显示online，远端session附件计数仍为1，不能把最终网络超时当完整撤销验收通过。证据`node-removal-{start,observed,later,final}.json`；时间为采样上界，不宣称精确撤销时延。

源码确认旧入站listener没有按peer撤销关闭已接受连接，出站连接也仅绑定自身节点授权生命周期。现对入站和出站连接保存peer公钥、地址、身份代际及SDK代际：入站使用固定SDK公开flow回调，仅接当前节点精确地址和配置端口，在连接排队前捕获身份；出站在握手前捕获。登记与peer状态发布互斥，删除、换钥、明确过期或删除后重加均关闭旧连接，不按重新分配后的同一IP认领旧流。此绑定依赖SDK的WireGuard与peer观察，不是密码学socket身份或全局原子控制面事务。

SDK初始NetMap的Peers可能陈旧，通知队列也可能丢事件；实现以通知为刷新提示，并每秒通过LocalAPI读取live peer集合，过滤InNetworkMap为false的历史记录。状态观察串行发布以避免旧结果回填；控制面暂时不可达、普通health变化或观察失败不等同撤销，保留仍有效的peer通道。使用SDK Expired判断，不自行把本地墙钟与控制面时钟混用；测试不承诺自然过期的精确时延。

接收队列最多32条，满时关闭新连接。正常Close释放连接registry和自身授权回调；节点停止先失效代际，锁外注销listener、等待已开始的handler、排空队列并同步关闭注册连接。迟到SDK回调遇closed即关闭传入连接；SDK本身的协程由SDK.Close收尾。SDK注册/注销不持node或listener锁，避免与flow回调的SDK锁反转。

最后审查发现，无外层deadline的Dial可能无限占用状态观察许可，使周期校准无法推进。共享queryStatus现将许可等待与LocalAPI调用一并限制在5秒，保留更早父截止；修前6秒断言明确失败，修后5秒结束并释放许可。调用者自身取消和内部观察超时仍分别保留原错误分类，不把超时当凭据撤销。

| 检查 | 结果与源码范围 |
|---|---|
| 旧流撤销红绿 | 原入站与出站闲置RPC在显式移除后仍存活，修前失败日志`/tmp/shiki-peer-lifetime-red.log`；修复后真实SDK测试通过。 |
| 完整node race | 226.285秒通过，无skip；属于最后5秒预算修正前的17文件冻结源`/tmp/shiki-peer-lifetime-source.json`，日志`/tmp/shiki-peer-final-node-race.log`。 |
| 最终5秒预算红绿与关联race | 修前`TestStatusObservationHasOwnDeadlineAndReleasesPermit`6秒失败；最后仅node.go和peer_registry_test.go变化，相关专项race106.481秒通过、无skip。日志`/tmp/shiki-peer-status-cap-{red,green}.log`，最终摘要`/tmp/shiki-peer-lifetime-source-final.json`。 |
| 关联行为 | 实际入站/出站移除、完全停用bus后的明确SDK Expired均在独立3秒断言内结束闲置流；不是测试parent deadline导致。另覆盖观察失败保留其他peer、身份删除后重加、队列/迟到回调、同步回收和可取消许可。新peer首连用例先等待peer发布，未先试连；不表示传播前即可连接。 |
| 静态与边界复核 | 最终相关vet、diff检查通过；peer适配架构测试0.404秒通过，七类审查见16。未重启旧全包测试，也不追认为最终源码整包重跑。 |

第三次重新认证已有实际成功证据：前两次Chrome提交后fresh SDK仍NeedsLogin，失败记录保留，根因未确定。第三次由基于最终生产源的诊断overlay生成新key和fresh URL，经Chrome同账户Connect后，SDK RegisterReq报告machineAuthorized=true、authURL=false，随后authRoutine成功并进入Running；NodeControl online，内嵌地址100.118.23.89，控制台显示Connected。证据`node-reauthenticated.json`明确attempt=3、diagnostic_only_overlay=true及最终生产源码manifest。密钥、登录URL及原始诊断文本不写入本记录；不能将成功倒推为前两次失败原因已确定。

**第二次移除前的记录：** 当时官方控制面的修复后节点撤销/附件回收尚未通过；后续结果见下节。第三次认证最初使用诊断overlay，之后正常停止诊断agent并换为不含诊断代码的最终构建；进程重启后同地址online，重新校验Token、原daemon ID和能力并恢复P2P应用连接。peer修复不凭空推断被移除节点自身仍报online应属于何种拒绝原因。此节的局部结果不扩大原Phase 2通过范围或提前关闭Phase 0完整认证矩阵。

完整Claude使用、主机开机/重新登录、自然网络变化、第二台物理客户端、其余原生平台、当前候选长期资源和macOS完整Homebrew仍待。当前不需要再次提供VPS密码或已有tailnet账户；新增凭据与影响账号的必要操作按具体步骤引导。

### 重新认证后的最终构建复核

四个Mac/Linux宿主从最终生产源码重新构建，版本`v0.0.1-alpha.2-local.tsnet.3`、Commit `unknown`、BuiltAt `2026-10-29T19:25:28Z`，不冒充已提交或已发布产物；参数与摘要见私有`candidate3/host-build.json`。双平台插件与manifest保持此前匹配版本，哈希未变。最终Linux daemon在原隔离service内替换，全部部署文件哈希与最终清单一致。Mac测试agent在原状态目录恢复，同地址online；专用节点与主Mac均通过Token认证、原daemon ID校验和能力协商，路径`tailscale-direct`、attempts仅`tsnet`。原tmux会话仍ready。证据`reauth-clean-restart.json`、`reauth-clean-connect.json`、`main-final-connect.json`及`reauth-session-list.json`。这不是主机重启、完整长稳或修复后再次撤销验收；当时专用节点保留在线认证状态，随后开展下述再次移除复验。

## 官方再次移除：旧附件回收与后续恢复

本轮使用上批最终clean构建，证据`revocation-fixed-observation.json`记录精确源码63924ce。主Mac和专用节点同时只读attach同一tmux会话，client_count为2；Chrome移除专用节点后降为1，主Mac全部查询成功，主终端保持至观察完成，tmux一直ready。控制台确认仅主Mac/VPS两节点，观察完成后正常detach主终端。最初临时观察器遗漏PTY尺寸，两终端在任何删除动作前exit1；补齐尺寸后才建立有效前置状态并执行移除，不把夹具准备失败归为产品失败。

| 观察 | 结果 | 计时边界 |
|---|---|---|
| 远端旧附件回收 | client_count 2→1 | row时间在查询前，使用首次count1的下一条row作为保守上界，向上取整13秒；从Chrome提交前计时，非精确撤销时延 |
| 被移除节点终端 | exit1 | 同一标记后26.809秒检测退出，向上取整上界27秒；不等于服务端附件回收时点 |
| 主Mac与远端tmux | 查询全部成功、主终端未退出、tmux ready | 仅本轮观察窗口；非长期无中断证明 |
| 被移除节点自身状态 | NodeControl仍online | 旧适配仅依据SDK Running；当时未记录Self.Online，不能断言磁盘缓存是本次原因 |
| 最初显式重新认证 | clean进程重启后BeginLogin仍no-op且没有URL | 当时恢复失败；后续独立认证成功见下文，不覆盖本条失败 |

触发后的53个样本中，最大相邻采样起点间隔0.712秒；主Mac查询全部成功、终端保持。该次观察终态专用路由为connecting/unknown且attempts为空，不能报告offline或假定已尝试回退；主路由为ready/tailscale-direct、attempts仅tsnet。32/42和Phase 0完整认证矩阵未关闭。

### 自身状态调查与重新认证

固定SDK的LocalAPI `Self.Online`由`health.GetInPollNetMap()`填充，表示当前是否处于控制面map连接；SDK `Running`及已有map不能单独证明控制连接仍有效。map请求普通错误可保留旧Running状态，但该结构化连接信号不能区分节点撤销、普通断网和超时，不得据此报告denied或INVALID_AUTH_KEY。

SDK支持磁盘netmap缓存，但本事件没有成功加载缓存的证据。next2诊断中的`load netmap from cache:`只在读取失败时记录；成功标签为`loaded netmap from disk cache`。当次Self.InNetworkMap=false，NoState随后转为NeedsLogin，注册响应machineAuthorized=false、authURL=true。源码的Cache.Load没有按年龄失效的TTL，缓存写入还要求控制面授予对应能力。因此不能把此前clean长时间online、后续诊断启动NeedsLogin的差异归因于缓存成功加载、自然到期或某个已确定的唯一根因。

随后使用旧`63924ce`源码的`diagnostic-next2`隔离构建取得当前URL，由Chrome完成认证，NodeControl online、内嵌IPv4为`100.101.136.82`；证据`revocation-fixed-reauth-result.json`。此前一次仍NeedsLogin的结果保存在`revocation-fixed-reauth-retry.json`，没有改写为成功。重新连接经Token、原daemon ID和能力验证后ready，路径`tailscale-direct`、attempts仅`tsnet`，见`revocation-fixed-reconnect.json`。该恢复发生在新状态适配部署前，不能据此证明candidate4已完成官方移除后的恢复验收；全过程保留节点持久身份，没有删除状态目录。

### 控制状态与peer授权分离的修复及回归

最终实现用当前有效self、公钥、控制map连接及无待处理URL确认online。控制连接不可确认时显示offline/NODE_CONTROL_UNAVAILABLE；Running仍带认证URL时显示needs_login，避免隐藏待处理认证。已经建立且未被明确失效的peer授权生命周期保留，RPC复用、入站listener和新peer连接不因单纯控制面中断被取消；从未取得有效控制观察的缓存状态不能建立新授权。daemon按数据面授权保活HTTP服务，诊断仍显示控制面离线。明确节点过期/身份状态失效继续取消授权，普通断网不被猜为撤销。

新的显式BeginLogin可从上述离线状态启动；健康online的重复登录仍保持原行为。登录启动与状态读取共享有界观察许可，清除旧URL标记；已有URL直接采用，不重复触发交互注册。只有当前注册启动后取得有效控制观察，并确认新key或已观察URL完成，才完成attempt。取消查询也使用共享观察许可，最终提交在锁内再次检查完成状态，避免迟到取消覆盖成功或旧查询回填。原request_id去重、单attempt、身份保留和5秒启动预算不变；取消不会凭缓存Running判定登录已完成。

| 检查 | 结果与源码范围 |
|---|---|
| 状态分类/真实SDK断控制红绿与完整node race | 基础18文件冻结源`/tmp/shiki-control-outage-source.json`：完整node race 310.580秒、34项顶层测试通过，无skip；日志`/tmp/shiki-control-outage-node-race.log`。真实SDK夹具切断控制连接，已有终端、新RPC和新TCP流保持可用并在控制恢复后继续；不是本轮官方网络验收。 |
| 注册启动与取消提交补充 | 已有URL被重复启动、成功后被迟到取消覆盖均先红后绿；注册阶段冻结源`/tmp/shiki-control-registration-source.json`专项race 82.302秒通过，日志`/tmp/shiki-control-final-race.log`。不同于前述整包源码，不追认310.580秒覆盖后续修改。 |
| 最后取消观察串行化 | 最终冻结源`/tmp/shiki-control-outage-source-final.json`追加专项race 11.242秒通过，日志`/tmp/shiki-control-cancel-serial-race.log`；验证取消查询不能把旧状态跨观察顺序发布。最终源没有再跑一遍完整node包。 |
| host与架构 | daemon真实HTTP2保活及明确撤权等关联race 43.386秒、架构race 1.378秒通过；`/tmp/shiki-control-outage-host-race.log`。 |
| CLI与静态检查 | CLI关联race 20.597秒通过，`/tmp/shiki-cli-control-outage-race.log`；相关vet通过。 |

candidate4从上述最终node及对应host/CLI生产源构建，版本`v0.0.1-alpha.2-local.tsnet.4`、Commit `unknown`、BuiltAt `2026-10-30T03:23:51Z`；精确参数、各生产文件与二进制摘要在`candidate4/host-build.json`。不含诊断overlay；专用节点clean进程重启后已观察到同一`100.101.136.82`地址online。它不是已发布资产，也不能替代主机重启、完整平台回归或官方撤销后的最终候选恢复验收。

### 最终候选的官方控制面断网观察

最终验证使用Mac专用candidate4 agent，临时代理只阻断该进程通往官方controlplane的连接；VPS继续运行candidate3，主Mac和系统网络设置不变。分为两个独立场景，避免将显式连接重配与无中断断网验收混在一起。

| 场景 | 实际结果 | 证据与边界 |
|---|---|---|
| 已有终端与控制恢复 | 阻断窗口11.8069秒；NodeControl报告offline/NODE_CONTROL_UNAVAILABLE，新的sessions RPC持续成功；已有只读终端从断网前至恢复后全程存活。控制恢复后同地址online，正常detach退出0。 | `official-control-outage.json`，passed=true；仅专用Mac控制连接受控阻断，非自然网络切换、长期离线或VPS新host实现的官方验收。 |
| 离线时显式connect | 另一个无持有终端的阻断窗口中connect成功ready、路径tailscale-direct、attempts仅tsnet；NodeControl仍offline/NODE_CONTROL_UNAVAILABLE，login_id未改变，没有自动发起认证。 | `official-control-outage-connect.json`，passed=true；证明保留授权仍可连接，不将connect期间的路由替换当作旧流连续性。 |

失败与准备记录保留：首轮在持有终端期间执行connect，终端随后exit1，见`official-control-outage-connect-failed.json`。源码确认connect无条件UpsertServer会改变revision，继而retireLocked/cancel旧连接池；该动作混入主动断流，原轮保持失败，不归因于控制恢复。另一前置观察仅等待12秒，短于产品15秒预算，记录在`official-control-outage-preflight-failed.json`，不把夹具提前退出算产品超时或通过。最终两个场景均为独立执行，未覆盖这些失败记录。官方保流仅观察既有终端存活及正常detach退出0，没有发送新的终端marker，不能扩大为断网期间新终端payload已读回；本地host真实HTTP2的新字节验证另属本地证据。

验证后已恢复专用agent原普通代理配置（7897端口），candidate4同地址online且connect ready；临时代理进程已停止并确认不存在，见`candidate4-normal-restored.json`。

本轮关闭的是上述限定控制面断网子场景。独立未使用Auth Key的撤销/自然到期、节点审批、URL自然过期、明确自身撤销分类、candidate4再次移除后的完整恢复和其他平台/主机/长期门槛继续保留。

## 精确提交CI补证

[CI40940419677](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40940419677)在`63924ce4271345bcb1d463e94e59a773d6f29a59`完成双平台测试及四目标未签名包构建，共六job全部成功。macOS/Linux node整包race185.047/35.375秒，架构1.205/1.107秒，E2E239.568/177.592秒；协议漂移、模块/依赖扫描、构建/lint及终端屏幕步骤均成功。记录补充最终peer修复源码的双平台整包覆盖，不覆盖上述后来完成的控制状态、登录恢复与host/CLI修改。原本地226.285秒与最后106.481秒的不同冻结范围保持。

workflow步骤无skip，但go test未开verbose，不能从日志无SKIP字样推导全套无skip；E2E中的真实LaunchAgent、systemd活动升级和12小时实验需要独立opt-in，CI未启用。node及architecture测试源码无Skip分支。日志留于`/tmp/shiki-ci-40940419677-review.log`，未为了补文档重新运行测试。


## 最终agent再次撤销与恢复、CI/CLI补修（2026-10-30）

candidate4的agent/CLI生产输入属于92fe17a，没有诊断overlay；VPS仍candidate3，主Mac仍公开alpha.2。两个只读终端稳定连接后，Chrome移除专用节点100.101.136.82：旧附件2→1，专用终端退出1，所有主Mac查询成功、主终端持续存活、tmux始终ready。附件回收观察上界9.923秒、终端退出观察上界22.960秒从Chrome提交前标记计时，包含UI操作与采样；不报告精确SDK撤销延迟。被移除节点最终offline/NODE_CONTROL_UNAVAILABLE，未再显示online；它仍不能区分明确撤销与普通控制断联。证据`candidate4-revoke-{preflight,observation,trigger,summary}.json`；原状态目录未删除，未重启agent绕过恢复。

同一agent显式BeginLogin取得新attempt，candidate4 CLI却约1.3秒遇offline就提前退出；随后只读GetStatus取得该attempt的URL，证据`candidate4-relogin-pending.json`。通过用户Chrome同账户Connect后，最终agent报告online、URL清除、新地址100.71.129.114；无Touch ID再介入、无诊断overlay、无进程替换。重新连接完成Token、原daemon ID及能力验证，ready/tailscale-direct且attempts仅tsnet，两端查询原session仍ready，见`candidate4-reauth-result.json`与`candidate4-reauth-connect.json`。该闭环属于最终agent；现场CLI早退保持失败，后续CLI修复另行验证，不能追认为candidate4 CLI已通过。

CLI根因是nodeLoginActive没有覆盖仍有有效attempt的offline/NODE_CONTROL_UNAVAILABLE。修复仅将该reason且login_id非空、expires在未来的状态纳入已有轮询；其他离线失败、无ID和过期仍终止，不再次BeginLogin或自动Cancel，不放宽自身/父截止。真实Unix/HTTP2 NodeControl序列先红后绿，交互URL和Auth Key在线结果、截止及拒绝分支覆盖单列；修前日志`/tmp/shiki-cli-control-pending-red.log`；最终CLI整包race40个顶层测试22.312秒通过，无skip或race，日志`/tmp/shiki-cli-control-pending-race.log`，源码及日志清单`/tmp/shiki-ci-and-cli-pending-verification.json`。独立七类审查通过；新远端CI待实际结果，不继承旧Linux通过。

[CI40998046799](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40998046799)精确92fe17a整体failure。Ubuntu完整测试及四目标打包成功；macOS的TestInvalidAuthKeyAndNoCredentialPersistence在SDK仍活动时遍历netmap-cache遇lstat ENOENT，make test失败，随后test-e2e未执行、终端屏幕step跳过。日志`/tmp/shiki-ci-40998046799-failed.log`。夹具现先Node.Close并等待SDK/本地worker与store退出，再DisableRecovery重开最终持久store检查解码值，原Walk全文件和两种key检查保持，所有I/O错误仍失败。定向race三次均通过，总13.190秒，日志`/tmp/shiki-ci-key-persistence-race.log`；这是最终落盘与关闭后文件不含key证据，不是任意运行瞬间取证。没有修改SDK、生产状态存储或忽略ENOENT。


## 未使用Auth Key撤销与独立自然到期准备（2026-10-30）

用户确认后，通过Chrome生成独立、从未提交给任何节点的Auth Key：single-use、有效期1天、非ephemeral、无tags。第一枚生成后立即由管理页撤销，页面显示无有效key、2枚recently invalidated；随后才启动从未存在tsnet状态目录的隔离节点，以0600文件读取被撤销key。官方控制面1.932秒明确返回denied/INVALID_AUTH_KEY，无地址、无URL；测试agent正常SIGTERM退出0，key文件已删除。不是无效随机key或已消费key的复用试验；也不代表已登录节点的node-key撤销。证据`unused-key-revoked-console.json`与`unused-key-revoked-result.json`。

本轮使用candidate5 CLI，版本v0.0.1-alpha.2-local.tsnet.5、提交99bee22b47adaafa08db06351db56226848383db，SHA256 b3c5a6e1cbaa00791e2937fb421e04984eccc34959544af91bcb1c5ee5e2c5fe；复用candidate4 agent（相同node/agent生产源码），SHA256 10a617240282902621441a521184280f85373b98bb4dbf145f71a75f5bffde06。构建记录candidate5/cli-build.json核验72项实际编译/嵌入/依赖输入匹配提交且构建前后不变；不把本轮源码当成公开alpha.2产物。

第二枚独立key采用相同限制，控制台显示2026-10-31到期，目前只保存在用户私有目录的0600文件，从未提交注册。以实际取得生成结果之后24小时再加5分钟为保守最早测试时刻：北京时间2026-10-31 15:55:52。到期日网页没有精确秒级时间，因此该时刻是本地验收门槛，不宣称官方精确失效时间。自动验收定在北京时间2026-10-31 15:56，已bootstrap到当前用户GUI域专用label dev.shikigami.tsnet-unused-expired.run-goeshw8f，读回runs=0、not running和日历触发时间；未到期前不启动测试节点或使用key，自然到期仍待验。冻结CLI5/agent4、配置/脚本摘要在私有next-unused-expired/manifest.json，调度preflight/activation.json分别记录提前门槛及激活。runner先检查时间门槛，到期独占一次、核对非秘密输入hash，只提交一次key，finally删除key并TERM/Wait自有agent；超时/非零退出/清理失败不计通过，结果落盘后请求卸载专用job。未写标准LaunchAgents目录，依赖当前Mac GUI登录会话、网络及7897代理；睡眠可延迟触发，关机/登出跨过调度不保证补跑。结果文件为该私有目录result.json，不得凭调度安装就关闭到期验收。

[CI41005895032](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41005895032)已对精确99bee22完成六job全部success，补上上一轮macOS缺失检查。macOS/Linux CLI race22.800/9.724秒、node整包206.020/47.786秒、server38.186/35.466秒、架构1.174/1.068秒、E2E209.733/170.129秒，终端屏幕步骤与四目标未签名包构建成功。日志`/tmp/shiki-ci-41005895032-review.log`。CI未开启的真实服务/12小时专项仍不算通过，旧40998046799失败记录保留；本批没有因此重复本地全套或重建agent。


## 官方节点审批、待批移除与批准恢复（2026-10-30）

用户明确授权临时开启手动审批并测试后恢复，已由Chrome核实初始关闭。专用新节点shiki-test-approval-goeshw8f使用candidate5 CLI/candidate4 agent与独立私有目录，既有主Mac、once及VPS身份不变。临时开启审批后，经Chrome同账户与Touch ID完成交互登录：页面显示“Logged in, but device not connected”及“Needs approval”，NodeControl denied/NODE_APPROVAL_REQUIRED、无地址/URL；提供原有效应用Token后sessions调用仍FailedPrecondition。证据approval-start.json、approval-pending.json与approval-pending-admission.json，主Mac同期查询成功见approval-main-before.json。

当前控制台只有Approve和Remove，没有独立Reject。通过Remove移除待审批节点100.76.33.78后，控制台恢复3台原设备；SDK仍报告旧NODE_APPROVAL_REQUIRED，业务继续拒绝，证据approval-removed-pending.json。这证明待批节点被移除仍不得访问，不证明SDK可区分明确管理员拒绝、未审批与被移除；不把它冒充完整审批策略拒绝矩阵。

保留原节点目录，在同agent显式CancelLogin旧attempt后BeginLogin重新申请。candidate5 CLI遇active denied/NODE_APPROVAL_REQUIRED约0.3秒提前退出；稍后GetStatus取得同一新attempt的URL，用该URL重新登录，仍需管理员批准且业务拒绝，见approval-reapply-pending.json。Chrome明确批准新申请100.68.23.49后，agent自动online且URL清除，connect通过原Token、daemon ID及能力协商，ready/tailscale-direct，原tmux session仍ready，见approval-approved-result.json。该现场结果属于旧CLI5与不变agent，后述CLI修复另有本地回归证据。

CLI修复仅将denied/NODE_APPROVAL_REQUIRED且login_id非空、expires在未来的attempt纳入原有有界等待，等待URL或online；输出仍denied、业务仍禁止，INVALID_AUTH_KEY等明确拒绝即使附有效attempt字段也立即终止。无重复BeginLogin、自动Cancel或新注册，不改变原wait与父context截止。真实UDS交互/AuthKey序列修前均提前退出，日志/tmp/shiki-cli-approval-pending-red.log；最终CLI整包race40项23.867秒通过，无skip/race/失败，日志`/tmp/shiki-cli-approval-pending-race.log`，两文件源码与审查清单`/tmp/shiki-cli-approval-verification.json`；独立七类审查通过，新提交CI须另按实际结果判断。自然到期调度继续使用已冻结CLI5，不为这一等待分支修改既有试验。

最终正常TERM专用agent退出0且精确PID不存在，删除控制台专用审批测试节点，保留本地隔离证据；将手动审批恢复关闭并重新加载确认，主Mac/once均查询原session成功，见approval-cleanup.json。未改变node-key180天期限、自动更新、ACL或主Mac/VPS服务。


## 本批清理状态（2026-10-30）

按用户要求回收已完成的测试环境：通过用户Chrome精确移除`shiki-test-mac-goeshw8f`、`shiki-test-once-goeshw8f`、`shiki-test-vps-goeshw8f`，最终管理台显示Add your first device且无machine行；恢复操作前标签页。此为资源清理，不将其充作新的撤销/认证验收。两个本机agent按各自配置正常停止，VPS专用transient service停止后自动消失，旧daemon及插件退出；只关闭专用tmux会话并精确终止测试HTTP进程，未停止共享tmux server。

未使用key自然到期任务自包含于`next-unused-expired`，不依赖上述三节点，保留整目录、预约launchd、GUI/网络/代理及冻结输入。当前c4双平台默认12h运行、快照及容器继续保留，签名私钥备份不是测试垃圾。状态目录/账户/构建残留的最终回收另记，不能将停止进程当全部文件清理完成。若今后继续官方网络验收，需要明确重新授权的测试节点，不能声称旧三节点仍online。

文件/账户清理已完成：本机run-goeshw8f的28个旧测试顶层项删除，仅next-unused-expired保留；VPS专用用户/同名组及/home/shiki-tsnet-test删除，UID1000无进程，死tmux socket目录删除。旧构建证据目录的115个条目删除，仅candidate4/host-build.json与candidate5/cli-build.json作为expiry来源记录保留。历史原始临时日志不再可用，正文中的脱敏结论与固定来源不改变，不虚称仍可从旧路径取证。停止清理回执摘要SHA-256为`27be4c705cd72b68f84e02bf5a0ff8419443d16200b199060f9334941a533aaf`；原系统SSH/Web/RustDesk监听及进程保持，7897实际TCP可连接，expiry冻结输入hash和c4进程身份复核保持。

历史文件补充清理：按文档逐个核对路径和运行/容器引用后，111项旧实验残留删除（约1.69GiB allocated，零删除错误）；另16项已结束测试目录仅被Apple Virtualization保留只读句柄，经核实无容器挂载/进程使用后删除（5,082,578,194逻辑字节），VM未停止或重配。仓库旧bin/dist已清理，dist含1,869,899,435逻辑字节；开发.tools和用户文件保持。旧候选包README断链13处已改为清理说明，docs验收结论保留。对应回执SHA-256：111项`c2335974ff658a61060b47f3d7fc32be745979191804b02b270027b18dff43a5`、16项`3a6123da28db945d48d34b67ccc6f717de033ef39c115669ee33daa8eab8466b`、仓库产物`7783bb9845b5b13bdbaec7b2fb59df76e1053d49aa3d18a7669d9a79bf627ed4`。这些不是当前双主机runtime或保留预约/长测资源的清理证明，后者见25最终结果。


最终补充历史清理及本轮临时审计材料回收已完成，811项确权对象与未知/运行中保留边界见[25清理终态](25-multihost-acceptance.md#历史临时材料最终清理)。expiry输入与runs=0仍保持；此记录不新增认证验收通过。


## 独立认证环境重建与状态修复（2026-10-31）

在原已清理环境之外，新建隔离Mac agent与VPS普通用户daemon，主服务、系统Tailscale和受保护长测均不修改。两端候选为v0.0.1-alpha.2-local.auth.6，基于ed7da9e加本批node修复；产品/插件摘要逐项核验，未使用诊断overlay。终端仅shell夹具，不宣称真实Claude。专用账号、HOME、配置、身份与节点保留至认证和后续安装升级依赖验收完成，再精确清理。

两处确定实现缺口已取得修前红/修后race及整包验证：Self.Expired跨全部BackendState保持expired/NODE_KEY_EXPIRED；非空AuthURL撤回旧应用授权，控制面单纯失联仍保留先前有效授权。SDK1.98.10的私有keyExpired和公开Self.Expired并非相同来源，官方Expire是否提供该公开标志仍待观察，不能将这一修复直接认定官方到期通过。自身移除、管理员明确拒绝、官方URL有效期与本应用10分钟attempt仍分别记录。

首轮实际结果：Mac完成浏览器认证并在控制台出现，但NodeControl始终未online，最终本应用attempt返回LOGIN_EXPIRED；保留失败，不将网页登记计为成功。VPS首次Connect又返回登录页，完成Google登录并再次Connect后，网页Connected且本地NodeControl实际online，SDK版本与Linux内核已记录。Mac正在进行保留身份的新显式尝试；完整双端业务、官方节点key过期、旧流回收及重新认证后同daemon业务恢复尚待。预约未使用auth key自然过期仍为10月31日15:56，使用原冻结CLI5/agent4，不替换为本批候选。


### 第二轮登录及业务预检

Mac保留同一状态目录的新显式BeginLogin通过：官方两节点Connected，本地两端online；Mac→VPS应用认证固定同一daemon ID并报告tailscale-direct，未配置direct-TLS。旧超时根因尚无足够证据，不改写为成功。测试插件最初因原Mac HOME为0775被安全检查拒绝；仅将本轮插件拷到可信私有临时目录并重启专用agent，原HOME权限未改，身份/IP保持。重启暴露启动早期未认证被永久标为REMOTE_INCOMPATIBLE的连接恢复缺口，正在修复；显式重新connect已恢复同daemon。两端四插件ready、真实终端输入回显通过，新文件按设计出现INITIAL_SYNC_CHOICE_REQUIRED并显式选择本地。以上不等同完整到期验收。

官方控制台本次菜单没有立即Expire选项，只有启停key expiry和移除；节点key显示6个月。正核对SDK公开DebugSetExpireIn是否经官方控制面设置本节点到期，诊断构建与正式产品必须分别记录，不能冒充管理员网页Expire或自然auth key到期。


### m7启动恢复补验

基于bf2e645加本地node授权等待修复的719文件冻结源构建两平台m7，node/agent新差异独立复核通过。8项定向race、agent整包race37.341秒（68顶层、187含子项，0skip/fail）及vet通过。专用Mac正常停止m6并使用m7启动，同IP online且无需显式connect即恢复ready/tailscale-direct；应用daemon ID和既有凭据代次保持。最早一次采样已ready，不声称本轮捕获中间auth_required，确定性本地回归覆盖该竞态。VPS同身份候选更新后NodeControl同IP online，但Mac连接停在REMOTE_TRUST_OR_PROTOCOL_REJECTED，30秒自动恢复观察失败；保留该独立失败并继续调查，不将Mac侧成功扩称双端重启恢复。手动进程替换不计为安装器升级或主机重启。

提交格式更正：按用户要求，bf2e645重建为c83c95a，797092d重建为42eb93e；两组各自git tree完全相同，仅提交信息/父提交标识变化。历史CI仍绑定原OID，不能将其误写成新OID的实际运行。


### 官方节点key短期到期观察（2026-10-31 01:08–01:10 +08）

仅专用Mac使用source7诊断overlay调用SDK公开DebugSetExpireIn；调用链经该节点自己的官方RegisterRequest设置Expiry，不改tailnet全局策略或其他节点。overlay必须使用/private/tmp规范路径：首次/tmp逻辑路径映射未生效，部署前由二进制marker检查发现并纠正，未将该次构建当诊断。有效overlay的安全观测、持久单次claim、0600原子trigger及输入摘要独立保存；诊断未进入仓库或发行程序。

实际仅请求一次，01:08:08成功；SDK随后回读KeyExpiry从2027-04-28改为2026-10-31 01:09:08。到期后SDK为NeedsLogin、Self.Online=true、Self.Expired=false、已过去KeyExpiry、无AuthURL；这证明既有仅Self.Expired适配不足，NodeControl仍报needs_login/NODE_AUTH_REQUIRED。旧终端先实际回显，之后Canceled结束；旧loopback TCP先回显，01:09:15读到EOF且无额外字节。认证旧流回收已观察，expired分类修复及干净产品重新认证/业务恢复仍待；不能将SDK返回nil独自作为到期证据，也不把此次节点key测试等同独立预约的未使用auth key自然到期。

诊断期间另一次同版本VPS优雅重启后，新sessions RPC和自动同步可用，未重现先前m6→m7永久错误；该成功不解释或抹除此前失败。新的本地HTTP2/SDK边界测试仅提供错误分类证据，不将未定位的历史失败改记已修复。


### 到期组合分类修复与干净候选范围

到期分类现优先保留显式Self.Expired，否则仅同一SDK NeedsLogin观察、已知self公钥、非nil/非零KeyExpiry且严格超过该期限1秒才分类expired。额外1秒覆盖SDK秒级舍入；Running、控制失联、审批或单独本地时间均不能证明到期。11项定向race（node34.746秒、CLI9.695秒）及vet通过。CLI有效expired尝试沿原截止等待；现场m8另暴露SDK启动uninitialized使前端过早退出，同样只在有效attempt且reason为空时有界等待，最终CLI整包42顶层/183含子项race28.607秒、零skip/fail及vet通过，独立复核通过。

诊断agent已正常停止，干净source8 agent启动并核验无诊断marker；重启后的SDK未保留可读SelfExpiry，实际NodeControl为needs_login，不能记为干净候选外部显示expired。01:28仅原子替换source9 CLI，SHA256 157f406627408e6bcded4c6fd1a593abf5e494ca45130b58421be5816e45191a，agent8/VPS7未重启。前一attempt在Chrome Google登录页持续空白后按原截止LOGIN_EXPIRED；新显式attempt使用CLI9已正常等到URL。后续恢复见下；不重触发已发生的官方到期，不删除保留节点或身份。

调查中的SDK peer关闭、HTTP2 GOAWAY和共享pool probe取消测试各自通过，但不证明旧m6→m7 VPS错误已修复。认证证据位于隔离auth-m6-tybjjsuq/evidence，冻结source7/8/9与各构建摘要保留各自范围，预约自然auth key到期继续原CLI5/agent4。


### 干净产品重新认证与业务恢复（01:34 +08）

Chrome原Google标签持续空白，新标签继续同一OAuth流程后，官方页面显示Login successful及专用Mac hostname；NodeControl实际online、同IP100.123.245.42。没有再次connect、替换Token或重启agent，原server revision、credential revision及daemon_da4c559a0972fef4376d4df935a3d962保持，agent自动ready/tailscale-direct、attempts仅tsnet，未配置direct-TLS。

在原session新建附件并显式取得控制，发送新唯一字符串后收到对应AUTH_REPLY，正常Ctrl+Q退出0；历史屏幕重绘不当作新payload。原mapping既有共同基线文件本地改动在VPS1.466秒收敛，随后VPS修改在Mac1.003秒收敛；原forward自动重建监听，新TCP连接精确回显新payload且无额外字节。证据clean-reauth-business-recovery.json，实际版本CLI9/agent8/VPS7；终端为shell夹具，不代表Claude。结合前述官方单次到期与旧终端/TCP关闭，已取得到期后干净产品同身份恢复证据；干净agent重启后未再暴露旧KeyExpiry，不补造其expired现场分类。未知VPS旧更新失败与15:56未使用auth key自然到期仍独立待验。

最终本批文档对照和架构测试0.410秒通过；认证与CI分别按Conventional Commits提交bdab0f6、58806be，一次推送master触发新CI。保留所有仍有认证/安装依赖的专用节点、普通用户与身份，未清理受保护长测/预约或用户原有环境。


<a id="unused-auth-key-expiry-final-20261031"></a>
## 独立未使用Auth Key自然到期终态（2026-10-31）

原预约任务实际于北京时间15:56:05.028787启动，晚于保守not-before 15:55:52；15:56:06.202569调用一次node login，2.449517秒收到denied/INVALID_AUTH_KEY，无登录URL，15:56:08.716760完成。这里“一次”是runner仅调用一次CLI登录，不是抓包证明SDK只发一个HTTP请求；没有重新提交、重跑或拿已消费key替代自然过期。6份冻结非秘密输入hash保持，CLI5（99bee22）/agent4、Go1.26.10、Tailscale1.98.10、macOS27.0.1 arm64范围不追认为最新候选或公开alpha.2同源验收。

结果passed=true、submitted=true、secret_file_removed=true、agent_exit_code=0；根代理直接核对result和6份输入摘要，以及samples中的唯一login终态。key仅检查缺席，未读取内容；登记agent PID96424不存在，专用launchd label已卸载，目录无开放引用。官方节点库存本批未回读，因此不声称已独立确认管理台没有测试hostname；明确拒绝与没有URL是本地实际观察。

任务自清key/进程/job后仍留二进制和私有状态，本批按UID/dev/inode/摘要/无引用精确回收10项目标：20文件、7目录、72,890,138字节。8份验收证据摘要不变，result/manifest/schedule/activation/preflight、单次attempt.started、原验证脚本与审计/清理收据继续保留在`~/.shiki-tsnet-test/run-goeshw8f/next-unused-expired/`；此前输入二进制/配置/plist路径现为已清理历史输入。audit摘要9aaac0e7c99740d0704313445f3956ad71b429487676bb3d59cfecf7dc7f3ca9，cleanup-receipt摘要076bf5c40061abd81ce44a7ce528232f98353124de960918816f193420109bc0。没有操作正式Mac/VPS身份或签名备份。

本子项完成后不再等待自然到期，也不再要求用户提供同批key。完整认证项仍保留官方登录URL有效期（应用attempt到期不是替代）、审批拒绝实际可观测结果及最终契约核对。按11，SDK不能区分撤销与控制面失联时应报告原因未知；13已明确offline/NODE_CONTROL_UNAVAILABLE不能解释成凭据撤销。现有官方自身移除和两种控制面断网观察、原始错误不泄露与fail-closed行为可用于该回退核对，不把取得SDK不存在的细分信号设为永久门槛，也不借此省略尚无证据的场景。没有在本批扩展状态枚举或关闭整个Phase0条目。


## 认证矩阵及官方URL期限来源复核（2026-10-31）

固定Tailscale1.98.10的ipn/ipnlocal/local.go:4014明确注释“Server-side URLs expire after 7 days”，客户端仅复用不足6天23小时的URL。RegisterResponse/Status未提供公开URL到期字段；这是SDK源码说明，不是官方控制面本轮实际7天到期证据。Shiki的10分钟LoginExpiresAt是应用attempt截止，不能替代该门槛，也不再用短等重复证明它。来源与摘要已保存于`/private/tmp/shiki-alpha2-installer-a-a3af0oz0/sdk-url-validity-source.json`，不含认证URL。

自身撤销与两个官方控制面断网观察支持11/13的原因未知回退：NODE_CONTROL_UNAVAILABLE不被解释成撤销，明确节点授权失效仍撤回业务。此前待审批页面仅提供Approve/Remove，移除后仍拒绝业务、重新申请/批准恢复的范围保留，不伪造独立Reject动作或SDK分类。当前Chrome只读回读两台保留测试节点均Connected，没有更改审批、删除节点或产生新的Tailscale身份。

本批安装器中间态引导缺口已修，见[15](15-implementation-status.md#evidence-forward-installer-auth-fixes-20261031)；完整Phase0仍保留官方URL自然到期现场证据与最终矩阵，不把SDK源码或本地mock当实测。真实Claude与安装器组合正在独立推进。
