# 边界情况和错误处理

本文件定义异常行为和验收场景，配合 [10 实现约束](./10-implementation-details.md)。所有功能尚待实现；表内是要求，不是测试结果。

## 1. 资源与清理

| 情况 | 必须行为 |
|---|---|
| 并发创建达到上限 | 在同一临界区预留会话/Claude 配额，避免先检查后创建超限；返回 ResourceExhausted |
| CPU、内存、磁盘接近上限 | 显示实际采样，CPU 百分比定义明确；磁盘检查目标文件系统，包含状态目录和项目目录 |
| 磁盘写满或持久化失败 | 停止相关写入，不返回成功；保留原文件和可恢复状态，清理本次临时文件 |
| 长期无人连接 | 不能推断 Claude 空闲；只建议清理，默认不自动删除 |
| 用户开启自动清理 | 仅显式配置的 stopped 会话，先通知；不删除项目文件，不自动杀死仍运行的任务 |
| 内存/CPU 限额 | max_sessions/max_concurrent_claude 是准入限制；memory_per_session/cpu_per_session 只有 cgroup 实现并验证后才可声明硬限制 |

```yaml
resource_limits:
  max_sessions: 10
  max_concurrent_claude: 5
auto_cleanup:
  enabled: false
  idle_threshold: 168h
  warn_before_cleanup: 24h
```

时长使用统一解析规则（本设计示例用 Go duration 支持的 h/m/s，不能把 `7d` 直接交给 time.ParseDuration）。

## 2. 网络与长连接

- P2P 失败时 tsnet 可走 DERP；不要把 DERP 可用判作整个 Tailscale 失败。无可用策略要报告实际尝试列表，首版不包含 WebSocket。
- 网络错误最多重试 5 次并退避；取消立即结束。鉴权、权限、TLS 身份错误不通过换网络绕开。
- 网络切换后终端重新建立当前屏幕，旧输入不自动重发；原有 port TCP 流报错，应用重新连接。sync 执行清单对账。
- 慢终端客户端队列满时断开并标明需要重绘，不丢若干字节继续显示，否则 ANSI/UTF-8 会损坏。
- agent IPC 断开只关闭相应 attachment。agent 停止后本地转发/sync 停止，远端 tmux 继续；重启重新对账。
- 长流 Token 到期/撤销要取消；只在建流时鉴权不满足撤销语义。
- 使用滑动窗口记录断线时需并发保护，正确清空全部过期记录；单次 RTT 高只提示，不制造反复重连。

## 3. 文件同步

### 并发修改和删除

每路径比较共同基线 B、本地 L、远端 R（含存在/删除、内容 hash、受支持权限）：L=R 更新基线；L=B 仅远端变化则下载；R=B 仅本地变化则上传；其余为冲突。没有基线是首次关联，不能按两机时间判断新旧。

远端 stat/read 失败必须区分 NotFound、PermissionDenied、Unavailable；后两者不当作文件不存在。检查和写入之间也可能变化，必须做 revision 条件提交，失败重新对账。CLI 没有前台时保留冲突，不自动覆盖。

### watcher、文件大小和排除规则

- fsnotify 断流、overflow、新建目录、编辑器临时文件 rename、休眠恢复都触发重扫。
- 先 stat/限制读取大小再读取；自动同步上限 10 MiB，传输使用分块流，不能把 10 MiB 文件塞进 10 MiB protobuf 消息。
- 大文件手动同步需单独容量/超时配置。二进制不一概禁止，小图片等按大小与 ignore 规则处理；数据库等持续变化文件需暂停写入或排除。
- 默认忽略 .git、node_modules、.env 等；无 .shikiignore 时默认 matcher 也要初始化。规则变更导致“被忽略”不等于用户删除文件，不能传播删除。
- 文件权限只保留支持的普通权限位，拒绝 setuid/setgid、设备节点、FIFO。跨平台不支持的权限显式报告。

### 路径和符号链接

所有文件请求携带 project_id 和相对路径。拒绝绝对路径、`..`、NUL、分隔符混淆和项目外路径；路径比较使用目录组件，不能用字符串前缀判断。

首版跳过符号链接并提示；即使将来支持内部链接也必须从链接所在目录解析目标、处理链和循环，并在文件打开时防止 symlink 替换竞态。服务端使用 root-confined 文件访问设施（或等价 openat/no-follow 实现），不能只做一次 EvalSymlinks 后普通 Open。

权限错误提示具体文件、运行用户和所需权限；不建议无范围的 `sudo chown -R`。

## 4. 端口与监听

- net.Listen 的返回值是端口占用依据；冲突后重选/显示替代端口，不杀死陌生占用进程。
- 本地始终绑定 loopback，除非用户明确更改；远端地址也必须通过允许规则，拒绝任意主机代理。
- 同一主机的两个 tmux 会话共享网络命名空间，“两个会话都在相同地址开 3000”通常是远端绑定冲突，不是可由本地换端口解决的问题。
- 扫描器保留 IPv4/IPv6 绑定地址；只有端口号的 map 会合并不同 endpoint。无法识别 PID/session 不自动转发。
- TCP 半关闭必须继续接收另一方向数据；不能收到本地 EOF 就丢弃远端剩余响应。
- 读函数可能同时返回 n>0 和 EOF，要先传数据再处理 EOF。异常取消双向协程；正常半关闭等待另一侧结束。
- 插件关闭/网络失败清理 listener、conn、stream，计数减回，不能无界 goroutine 重试。

## 5. Claude 与 tmux

- 只检查服务端 Claude 可执行文件、支持版本和登录状态；本地不要求安装或版本一致。
- PID 存在不代表仍是原进程。核对 tmux pane、进程启动时间/身份，重启后重新发现；不按旧 YAML PID 杀进程。
- 五分钟无输出可能是正常等待，提示状态未知，不自动 SIGTERM/SIGKILL。
- 用户主动停止记 stopped，异常退出记 crashed，无法获取退出原因记 unknown。自动重启默认关闭，启用后有最大次数/退避/稳定窗口，重新启动监控。
- tmux 控制模式输出不是终端字节；普通 attach 的 PTY 关闭只结束 attach 客户端。
- daemon crash/restart 保活必须实测 systemd cgroup/停止策略；主机重启不保活。手动 tmux 操作需同一 UID、socket 和状态命名空间。
- Claude 对话能否继续由其自身持久化/官方恢复能力决定，Shiki 不把配置快照当作对话备份。

## 6. 权限和凭据

- unary/stream 使用同一认证入口和显式权限映射；未知 RPC 拒绝。
- 只读 attachment 无论连接顺序都不能输入、resize、发送控制字符或获得控制权；服务端检查，不能只靠 UI 隐藏按钮。
- 会话归属从 session.project_id 查 Project，再验证 daemon 单用户边界；不存在 Session 先返回 NotFound，不访问虚构的 Session.User 字段。
- 列表过滤 user 不是授权参数，禁止伪造 user 越权。普通用户运行 daemon，不按客户端请求 setuid。
- HS256 key、tsnet 状态和 Token 使用 0700 目录/0600 文件，日志脱敏；签名密钥持久化且管理命令与服务一致。
- 客户端本地 IPC 验证 peer UID；新窗口不能读取其他 OS 用户的 agent。
- go-plugin 不是恶意插件沙箱，安装插件等同于执行该用户权限的代码。

## 7. 持久化、恢复与升级

状态保存采用单写者原子快照，不能把带 stream/PTY 的 Session 直接 yaml.Marshal。若分文件保存，需要另外设计事务/日志协议，不能只对单文件 rename 就声称项目关联原子。

配置加载先解析到临时结构、做 schema/语义校验，再替换运行配置；保留损坏文件和已验证备份。无可用备份停止相关服务并说明恢复方式，不覆盖默认值丢失 Token 和映射。

重启从持久化配置与实际 tmux 状态双向核对；找不到进程保留会话配置，找不到记录保留孤儿供用户处理。启动/删除中断根据记录状态幂等恢复。

升级锁定单一版本集合（核心、插件、协议），下载临时产物、验证后原子切换；保留可回滚版本和状态备份，不覆盖配置/签名密钥。协议能力不匹配时禁用相关功能并说明，不能继续解析未知帧。

## 8. 必须覆盖的跨模块验收

| 场景 | 预期 |
|---|---|
| 两个 CLI 连接同一项目 | 一个 agent/tsnet 节点/同步映射；独立终端 attachment |
| 控制者与观察者 | 观察者控制操作被服务端拒绝；接管原子且有权限 |
| 断网后继续生成输出 | 远端工作继续；重连重绘，无输入重放 |
| daemon restart / agent restart / 主机 restart | 分别验证保活、重连、仅配置恢复的差异 |
| 本地编辑与远端删除同时发生 | 冲突保留两端内容，不静默删除 |
| 上传时远端 Claude 修改文件 | revision/外部写入检测触发冲突；不可确认的竞态见 10 |
| 删除 Session 与配置快照恢复 | 不改项目文件；恢复创建新 Session |
| Token 撤销时已有终端与转发流 | 流关闭，tmux 保留，重新授权后才能接入 |
| 清空或截断状态文件 | 保留损坏证据、从备份恢复或进入诊断，不清空项目 |

## 9. agent 崩溃、重启与恢复顺序

### 崩溃时

前端收到 IPC Unavailable/EOF 后立即停止写入，恢复 CLI raw mode、显示“本地后台中断，远端会话仍可能运行”。所有输入缓冲作废；不能在重连后重放。客户端不根据 IPC 失败断言 VPS 或 Claude 已停止。

agent 的插件宿主控制连接断开后，worker 必须取消所有 watcher、listener、网络流并退出；Linux 可辅以父进程死亡信号，macOS 使用父连接 EOF/watchdog。恢复前核对并回收旧 worker；无法确认其已停止则拒绝启动同一映射的新 writer，报告 ORPHAN_WORKER。单实例锁防止多个 CLI 同时启动多个 agent。

### 重启顺序

1. 获得同用户实例锁，安全清理 stale socket；生成新 instance_id，旧 frontend/attachment ID 全失效。
2. 验证配置、状态 schema、凭据目录和 worker 日志，不能以默认值覆盖损坏数据。版本比当前程序新则只提供诊断，禁止向下迁移或写入。
3. 启动本地 IPC 诊断与 NodeControl，再恢复 tsnet 节点身份；未登录时保持 auth_required，不删除目录制造新节点。
4. 按服务器分别建立连接，认证并协商协议。单个服务器失败不阻塞其他服务器或本地诊断；不兼容的功能保持停用。
5. 启动兼容 worker，读取已提交的任务状态。先重新扫描两端文件，处理未完成事务，再恢复已启用的 sync；已 paused 的映射保持 paused。
6. 重新绑定 enabled 的端口监听；端口已被其他程序占用时标 error，不能杀占用进程或悄悄改端口。旧 TCP 连接不恢复。
7. 前端重新 Handshake、订阅事件并获取快照；需要终端时重新打开流、等待 READY/RESET。旧控制权不能仅凭本地缓存恢复，需 daemon 判断。

worker 的文件日志区分 prepared/committed：收到远端提交结果但本地基线未持久化时，重启按当前 hash/revision 验证结果；未收到 ACK 时也先查询，不盲目重复覆盖。确认一致后写基线，否则保留冲突副本并产生新 conflict generation。

### 主动停止与反复崩溃

Shutdown 返回 stopping 后阻止新任务，持久化期望配置，取消输入/连接并等待 worker 退出。超过停止 deadline 由宿主监管回收 worker，不能遗留第二个 writer。状态文件保留；新一次显式 CLI 使用可以按启动策略重新启动，但服务管理器不能因正常退出 0 立即无限拉起。

同一版本 5 分钟内连续崩溃 3 次进入启动失败提示，用户查看日志再恢复；不要由每个 CLI 各自无限重启。崩溃次数窗口与网络重试次数是不同机制。验收包括上传中 SIGKILL、转发中重启、两个 CLI 同时重启、损坏配置、孤儿 worker、监听端口被抢占。

## 10. 四条版本边界

版本行为由 [09](./09-protocol-definition.md) 的 ProtocolOffer/Agreement 和 [13](./13-local-ipc-protocol.md) 的本地握手定义。构建版本不同不自动意味着不兼容；同版本也不能跳过能力检查。

| 边界 | 发现方式 | 不匹配时行为 |
|---|---|---|
| CLI/GUI → agent | Agent.Handshake | major 不同、minor 无交集或必需能力缺失：拒绝业务调用；显示双方版本与需升级组件，不自动停止其他窗口使用的 agent |
| agent → daemon | 认证后 NegotiateProtocol | 禁用该服务器不兼容能力；核心不兼容则不 attach/不写文件；其他服务器继续 |
| host → plugin | GetInfo 的 offer + Init 的 agreement | 拒绝启动不兼容插件，标 incompatible；终端核心继续工作；依赖此插件的任务不降级为另一个实现 |
| 程序 → 磁盘状态 | schema_version 检查 | 只迁移明确支持的旧 schema，先备份；新 schema 不向下读取并覆写，提供诊断/回滚说明 |

NegotiateProtocol 不存在（Unimplemented）视为不支持协商，不猜测旧协议可用；旧 daemon 不收到新格式的写请求。可选 sync/port/stats 缺失只禁用对应入口。ready、capability、权限是三个条件，都满足才执行。

已有连接遇到服务重启/协议改变必须重新协商，不能在活跃流中热换消息语义。升级先停止相关宿主与 worker、备份状态，再切换兼容产物；状态已迁移时回滚程序需匹配备份，不能把旧程序直接指向新状态。双方实际版本、协商值和缺失能力可进入诊断，Token 不可进入。

## 11. tsnet 认证失败与离线

认证属于持有 tsnet.Server 的 agent/daemon；安装器和 CLI 只通过本地 NodeControl 引导。先打开 IPC 再等登录，避免“未入网所以拿不到登录 URL”的启动死锁。

| 状态/原因 | 处理与恢复 |
|---|---|
| needs_login | 显示该实例的有效 login_url；用户完成或提供受限 key 后重查；所有依赖节点的任务暂停 |
| 无效/过期/已使用的一次性 auth key | 标 denied/INVALID_AUTH_KEY，停止反复用同一 key 登录，清除内存 key；用户提供新的 key 或改交互登录 |
| 用户取消/URL 过期 | 使该 attempt 失效，保留节点状态；新的显式 BeginLogin 生成新 attempt，不复用过期 URL |
| 管理员待审批/设备策略拒绝 | 显示可核实的原因及人工操作，不自行删节点重新注册绕过策略 |
| 节点登录失效或被撤销 | 标 expired/denied，暂停依赖任务并取消使用旧节点的流；重新认证后重新检查 daemon Token/能力并对账 |
| 控制面暂时不可达 | 区别于凭据无效；保留节点身份，已可用的 peer 通道可继续，无可用通道时按网络退避重试 |
| ACL 阻止访问 peer | 显示受限 peer/端口，不误报为 key 失效；不自动换明文或新身份 |
| 节点状态目录损坏/权限错误 | 停止该节点并给出路径/权限诊断，不自动重建身份或覆盖目录 |

tsnet API 未提供足够信息时状态为 unknown/offline，不能从一次 dial 超时猜测 INVALID_AUTH_KEY。需要以锁定 SDK 的实际状态/错误映射验证；URL/key 都不进入日志或远端诊断报告。

明确认证拒绝、节点撤销或 ACL 拒绝时停止自动回退，避免绕过用户网络策略；仅普通网络不可达可使用已显式配置且验证身份的 direct-TLS。重新认证成功必须先确认同一目标 daemon ID，再恢复任务。客户端节点和 VPS 节点可能分别失效，诊断要说明故障所在端；客户端无法访问时需在 VPS 本地查看 NodeControl。

## 12. 多设备冲突的完整处理

### 文件冲突：两台设备 A/B

每个设备/映射有自己的共同基线，`device_id` 不充当权限。daemon 串行化通过 Shiki 写入的同一路径提交，并以当前 revision 校验 base_revision；不依赖两台设备时钟。外部 Claude/编辑器写入的竞态仍按 10 保留副本和复核，不能声称拥有通用文件锁。

1. A/B 最初都与远端 revision R0 一致。A 离线编辑得到 LA，B 在线编辑并成功条件提交生成 R1。
2. A 上线先扫描并获取 R1：若 LA 与 R1 内容一致，只更新 A 基线；若仅远端变动则下载；若 LA、R1 都不同于 A 的基线，则生成冲突 C1，保留 A 和远端内容，暂停该路径。
3. UI 或后台状态列出 local/base/remote 的存在状态、hash 和 generation。无前台时持久保留冲突，其他路径继续；远端删除与本地修改同样进入冲突。
4. 用户预览通过 ReadConflictFile 指定 C1 generation；选择 KEEP_LOCAL/REMOTE/BOTH 后，worker 重新验证两端版本。若 B 又提交 R2 或 A 文件又变化，返回 Aborted，生成 C2，要求重新审阅；不套用旧选择。
5. 条件提交/下载完成后校验实际内容，持久化 A 的新基线与完成状态，再将 C1 标 resolved。B 接收事件后用自己的基线对账，不被强制采纳 A 的解决方式。
6. 保留两份时用唯一名字记录冲突副本；不覆盖已有副本。手动合并先由用户编辑，再刷新到最新 generation，以 KEEP_LOCAL 条件提交；不提供无版本约束的 force。
7. 处理过程中任一端崩溃，按第 9 节的事务日志和实际 hash/revision恢复；服务器收到提交但 ACK 丢失时先读取确认。删除历史无法从旧基线判断时重新扫描并保留冲突，不把不存在的 stat 响应当删除。

跨设备不存在一个全局“最后同步时间”。暂停设备回来后必须扫描，WatchFiles 事件丢失或缓存过期不能导致覆盖。两个设备创建相同新文件：一方条件创建成功，另一方失败后比较内容，相同则采纳，不同则冲突。

### 同一终端控制权

READY/CONTROL_CHANGED 携带 `control_revision` 与 controller_attachment_id。A/B 同时以 revision C0 请求 TakeControl，daemon 在会话锁中至多让一个提交成功并产生 C1，另一个返回 Aborted；它刷新后可再次明确接管。禁止以“最后一个请求获胜”反复夺权。只读设备即使提供正确 revision 仍不能接管。

断线检测前旧控制者可能仍暂时存在；重连设备以新的 attachment 进入观察/控制状态并按协议显式接管，不复用旧 ID。停止/删除会话使相关接管请求失败，不能在已删除会话上恢复旧控制权。

### 本地资源与设备隔离

同一设备上两个窗口写 mapping/config 使用 expected_revision，冲突返回 Aborted；agent 不接受两个活动映射管理重叠本地目录，避免重复 watcher 写同一文件。不同设备的 local_root 和本地端口各自独立，不互相覆盖设置。

端口监听只在各设备 loopback 上；A 停止自己的 forward 不影响 B。会话删除、远端服务退出或 Token 撤销导致相应远端流结束，各 agent 单独更新状态；不能让一个设备的端口 UI 显示另一个设备的真实 listener。

验收至少覆盖：A 离线/B 写入、修改与删除、双方同时创建、展示冲突后第三次写入、解决中途崩溃、两个设备同时接管、同机两个窗口同时改配置、一个设备凭据撤销而另一个继续工作。


## 13. ConnectRPC 传输故障

| 场景 | 必须行为 |
|---|---|
| HTTP/1 代理或 TLS 未协商 h2 | 标明 HTTP2_REQUIRED；不将 unary 可用判为终端可用，不回退明文或 gRPC-Web |
| Connect 客户端创建成功但未拨号 | 仍为 connecting；实际认证/协商成功后才 ready |
| HTTP 200 后 EndStream 返回错误或流截断 | 以最终 RPC 状态失败处理；文件不提交、不更新同步基线，终端重绘、TCP 由应用重连 |
| agent/daemon 收到下游远端 error | 按 14 重建本地 Connect error，保留安全业务 code；直接 return 导致 Internal 属实现缺陷 |
| 超限消息或压缩请求 | 首版拒绝压缩请求，每跳限制消息 1 MiB；将来启用压缩仍按解压后限制；数据块另限 256 KiB；取消下游并清理临时文件 |
| 终端长时间无输出 | 保持有效流；不套覆盖整条流的短 HTTP timeout，不误判 Claude 崩溃 |
| HTTP 重定向或未知目标 | 拒绝，不把 Authorization/插件凭据带往新地址 |
| graceful shutdown 遇到长期流 | 先取消任务再限时 Shutdown，超时 Close/Wait；保留远端 tmux |

协议准入拒绝发生在 HTTP 层时，SDK 推导的 Connect code 不等于领域错误；诊断结合 HTTP 版本/安全状态，禁止解析原始 HTML 错误页面当作业务消息。代理的错误详情、headers 和 trailers 都按白名单重建。具体接口、标准流关闭和测试要求见 [14](./14-connectrpc-transport.md)。
