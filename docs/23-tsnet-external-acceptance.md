# 官方 tsnet 外部验收

2026-10-30：用户已提供官方 tailnet、已登录的 Chrome，以及家庭宽带 Mac 与不同网络的 VPS。首次真实网络批次已关闭08的Phase 2首条；当前32/42，10项部分完成。其余认证矩阵和平台门槛不随此项关闭。后续官方节点移除暴露旧流残留，peer生命周期修复已有本地回归证据；第三次重新认证已online，随后不含诊断代码的最终构建重启及应用重连通过；官方再次移除已验证旧附件回收与主节点不受影响；自身状态分类与显式恢复的本地修复及分层回归已完成；旧63924ce诊断构建已重新认证并恢复应用连接，candidate4 clean重启同地址online。最终候选已完成两个独立的官方控制面受控断网场景：已有终端保持及控制恢复、无持有终端时显式connect复用授权。首轮混入主动连接重配的失败记录保留，完整认证矩阵未关闭。

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

仍需独立未使用key的撤销/自然过期、节点审批等待/拒绝/批准、URL自然过期及完整控制面离线分类。测试用已消费key不能替代这些场景。节点移除原失败、官方旧附件回收、后续重新认证和不同候选的证据见下节；完整自身撤销分类与最终候选再次移除后的恢复仍待；官方受控断网的两个限定场景已通过，不扩大为所有离线分类或自然网络切换通过。

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
