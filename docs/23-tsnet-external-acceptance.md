# 官方 tsnet 外部验收

2026-10-30：用户已提供官方 tailnet、已登录的 Chrome，以及家庭宽带 Mac 与不同网络的 VPS。首次真实网络批次已关闭08的Phase 2首条；当前32/42，10项部分完成。其余认证矩阵和平台门槛不随此项关闭。后续官方节点移除暴露旧流残留，peer生命周期修复已有本地回归证据；第三次重新认证已online，随后不含诊断代码的最终构建重启及应用重连通过；官方撤销修复复验仍待。

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
- 登录修复阶段原生架构测试0.415秒通过；后续peer适配审查架构测试0.404秒通过，不重复成功检查。七类源码/文档复核见16。此前文档CI40923247799、40924428349均已确认成功，本次修改的远端CI需单独记录。

仍需独立未使用key的撤销/自然过期、节点审批等待/拒绝/批准、节点移除后的活动流撤权和重新认证、URL自然过期及控制面离线分类。测试用已消费key不能替代这些场景。节点移除原失败与本地修复范围见下节，官方修复复验尚未通过。

## peer撤销修复与复验边界

用户在最终操作时确认后，已通过Chrome移除专用一次性节点，当时控制台只剩主Mac/VPS两节点。原alpha.2移除后新请求5秒超时、旧终端最终canceled退出，路由offline/RECONNECT_EXHAUSTED且attempts仍仅tsnet；主Mac业务保持成功。但原agent仍显示缓存online，远端session附件计数仍为1，不能把最终网络超时当完整撤销验收通过。证据`node-removal-{start,observed,later,final}.json`；时间为采样上界，不宣称精确撤销时延。

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

**外部仍待：** 官方控制面的修复后节点撤销/附件回收尚未通过。第三次认证最初使用诊断overlay，之后正常停止诊断agent并换为不含诊断代码的最终构建；进程重启后同地址online，重新校验Token、原daemon ID和能力并恢复P2P应用连接。peer修复不凭空推断被移除节点自身缓存online应属于何种拒绝原因。此节的局部结果不扩大原Phase 2通过范围或提前关闭Phase 0完整认证矩阵。

完整Claude使用、主机开机/重新登录、自然网络变化、第二台物理客户端、其余原生平台、当前候选长期资源和macOS完整Homebrew仍待。当前不需要再次提供VPS密码或已有tailnet账户；新增凭据与影响账号的必要操作按具体步骤引导。

### 重新认证后的最终构建复核

四个Mac/Linux宿主从最终生产源码重新构建，版本`v0.0.1-alpha.2-local.tsnet.3`、Commit `unknown`、BuiltAt `2026-10-29T19:25:28Z`，不冒充已提交或已发布产物；参数与摘要见私有`candidate3/host-build.json`。双平台插件与manifest保持此前匹配版本，哈希未变。最终Linux daemon在原隔离service内替换，全部部署文件哈希与最终清单一致。Mac测试agent在原状态目录恢复，同地址online；专用节点与主Mac均通过Token认证、原daemon ID校验和能力协商，路径`tailscale-direct`、attempts仅`tsnet`。原tmux会话仍ready。证据`reauth-clean-restart.json`、`reauth-clean-connect.json`、`main-final-connect.json`及`reauth-session-list.json`。这不是主机重启、完整长稳或修复后再次撤销验收；专用节点目前保留在线认证状态。
