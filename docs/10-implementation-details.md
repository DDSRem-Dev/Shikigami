# 实现约束与验收

本文件给出关键模块的实现约束，替代原先无法直接编译、忽略错误和生命周期的长代码示例。仓库已开始实现；这些要求仍需逐项以源码和集成测试验证（见 [15](./15-implementation-status.md)）。消息结构以 [09](./09-protocol-definition.md) 为准。

## 1. 终端 I/O

### 客户端与本地 IPC

CLI 使用本地终端 raw mode，不创建无用途的本地 PTY，也不自行模拟 ANSI；宿主终端负责渲染。GUI 后续用 xterm.js。agent 传递原始 bytes，不经 UTF-8 字符串往返破坏多字节或控制序列。

1. 检查 stdin/stdout 是否 TTY，处理 MakeRaw/GetSize 错误，只有成功进入 raw mode 才 defer Restore。
2. 前端申请 attachment 后发送 Open 首帧；后续数据、resize、控制操作经一个有界队列由单一发送者序列化。
3. SIGWINCH 生命周期跟随前端 context，退出 signal.Stop；原始 stdin 读取必须可取消/唤醒，不能留下 goroutine 抢读下一个菜单输入。
4. Ctrl+Q detach 是可配置的前端快捷键；按字节状态机处理，不能假定一次 Read 恰好只有一个字节。需测试与粘贴/组合输入的边界，并提供关闭拦截的选项。
5. 任一方向失败取消上下文、停止监听、回收协程并恢复终端；流正常关闭与错误分别展示。
6. attach 期间插件日志与菜单不直接写进终端数据区域。通知进入 UI 状态/独立面板，需要交互时先安全暂停终端显示。

### 服务端 PTY 桥接

普通 `tmux -L shiki-<uid> attach-session -t shiki-<session_id>` 运行在 creack/pty 创建的 PTY；不用 `-CC`。命令和 session 都由可信控制器构造，保存 exec.Cmd 并 Wait 回收。

每个会话一个 PTY reader。Read 返回 n>0 时先复制 `buf[:n]` 再放入队列，不能复用底层 buffer 导致排队数据被覆盖；随后处理 err。输出/事件由同一个 attachment sender 发出，禁用多 goroutine 并发 Send。

慢客户端不能静默丢字节；队列到达上限取消它，其他客户端继续。资源上限、read/write 错误与 attachment cleanup 在会话级同步，网络 Send 不持有会话全局锁。

所有输入、resize、控制字符先检查控制权与 session.write。resize 限制为非零合理尺寸，调用 PTY Setsize 并使用同一 tmux socket 同步窗口；观察者尺寸不改变主会话。

终端控制字符通过 PTY 行规程送达前台作业，不用任意 PID 信号代替。显式 stop/restart Claude 走会话管理 RPC 和已验证进程身份。

### 重连和屏幕初始化

首版不实现输出日志回放。sequence 只用于同一次 attachment 的输出排序/诊断，不跨重连恢复。每次新连接必须获得完整当前屏幕，不能从一个正在变化的输出流中途加入。

按 [03](./03-session-management.md) 的桥接重附着策略，在当前尺寸下发送 RESET 并触发 tmux 重绘，输出流带新的 attachment_id；前端先重置终端再显示重绘。需要测试已有观察者与主客户端共同重绘的行为。alternate screen、尺寸切换、UTF-8 边界均属于交付门槛。

如果未来要无损历史回放，另行定义 stream epoch、持久化有界日志、ACK、水位、溢出与终端初始状态；仅增加 resume_from 字段无法实现。

## 2. 文件同步

### 数据模型与三方比较

本地保存每映射每路径的共同基线 B：存在状态、内容 hash、普通权限位和远端 revision。L/R 为当前本地/远端状态；mtime 只作扫描优化，不能跨机器比较判断冲突。

| 比较 | 动作 |
|---|---|
| L = R | 更新基线，无传输 |
| L = B，R ≠ B | 下载或应用远端删除 |
| R = B，L ≠ B | 上传或请求远端删除 |
| L ≠ B，R ≠ B，L ≠ R | 冲突，保留两端并暂停该路径 |
| 没有 B | 首次同步计划；显式选择方向并处理已有不同内容 |

删除是状态，不是缺少 stat 响应。watcher 错误、权限问题、断线不能生成删除。忽略规则变化也不生成删除。

### Watch 与协调

先建立 watcher，再扫描/对账，并在扫描后处理积压事件；事件只提示“重新读取路径”，不能假定完整排序或恰好一次。新增目录递归注册，重命名重扫父目录，溢出和重连全量核对。

使用单一协调循环管理 debounce、待处理路径、重试和基线，避免 AfterFunc 重入并发上传同一文件。debounce 加最大等待上限，持续写入不能无限延迟。关闭 watcher channel 时退出，处理 Errors，Stop 时排空或明确保留未同步任务。

应用远端文件后用期望 hash/revision 抑制回声，不移除 watcher；如果本地文件已变成其他 hash，视为新的本地修改，不能按时间窗口忽略。

### 传输与提交

[09](./09-protocol-definition.md) 定义清单分页、ReadFile 服务端流、WriteFile 客户端流。每块最多 256 KiB，RPC 单条消息收发上限均为 1 MiB，自动同步文件上限默认 10 MiB。先限制大小再分块读取，不用完整 ReadFile 把未知大小文件装入内存。

首版同步普通文件及其必要父目录；不承诺复制空目录、符号链接或特殊文件。服务端创建父目录仍通过受根限制的路径操作，并保留已有目录内容。

每次写入首帧含 project_id/path、base_revision、内容 hash、总大小及普通权限位。服务端在项目根内创建私有临时文件，按累计大小/块大小限制接收，校验 hash，确认基线未变后原子替换。失败/取消删除临时文件；成功返回新 revision。删除也要求 expected revision。

revision 覆盖 daemon 已观察到的磁盘内容；在提交前重新读取/检查实际内容，不能仅相信 watcher 已更新索引。**Claude/编辑器是外部写者，普通文件系统没有可与任意写者协作的内容 CAS**：临近替换的并发写仍可能竞态。首版需保存被替换版本/冲突副本、提交后复核并提示，不能宣称“绝不丢任何外部同时写入”。更强保证需要写入协作或隔离工作副本，属于后续设计。备份本身需限制容量并明确保留策略。

客户端下载同样校验 expected local hash，避免读到远端后覆盖本地新修改。保留本地操作使用基于已展示远端 revision 的条件提交，不再次调用普通 uploadFile 触发递归冲突菜单；revision 变化则重新展示冲突。

keep_both 使用唯一副本名，记录原路径并避免副本再次触发冲突循环。show_diff 临时文件用 os.CreateTemp、0600、检查返回值，不能固定 `/tmp/` + basename。插件向宿主发送冲突事件，不能直接接管终端输入。

### 路径、权限和 ignore

协议路径使用 `/` 分隔的项目相对路径；本机到协议路径用 filepath.Rel + ToSlash，不能 TrimPrefix。拒绝绝对路径/逃逸；文件打开使用受项目根约束的 API，首版跳过 symlink。下载同样检查本地边界。

ignore matcher 在无配置文件时也用默认规则初始化；检查 Scanner.Err。gitignore 模式针对相对路径，支持的语法与默认规则需写测试。默认不同步凭据目录；忽略不等于删除。

## 3. TCP 转发

扫描器使用结构化信息或健壮解析 `ss`，保留绑定地址、IPv4/IPv6、端口、协议。`strings.Split(addr, ":")` 无法解析 IPv6；解析错误不把上次全部端口标关闭。端口号只能作为服务类型猜测，UI 标注推测。

每次流首帧 CONNECT 固定 endpoint/connection_id，鉴权和 allowlist 后使用带 context 的 Dial。成功返回 CONNECTED，失败返回 Connect 错误，禁止把一个流中途重用给其他目标。

- 数据块先检查上限；Read n>0 先发送再处理 err。
- 每方向只有一个发送者。TCP EOF 转成 HALF_CLOSE；接收方 CloseWrite，继续另一方向直到结束。
- 硬错误取消 context 并关闭 conn 使阻塞读写退出。正常半关闭不提前全关；最终等待两方向结束再回收。
- CloseSend 只由发送者调用；不能一边发送数据一边另一个协程 Send(CLOSE)。
- 统计用锁/原子计数，连接结束递减。不要依赖可变 Active bool 无锁终止循环。
- listener 真正绑定成功后登记，Stop 将 listener、活动流及计数一并回收。

RPC 已复制/序列化数据，不称为零拷贝；性能优化先测量吞吐、分配量和对终端延迟的影响。

## 4. tmux 与进程

所有操作复用同一 socket 参数，包括 resize、has-session、kill-session。启动 `claude` 使用经过配置验证的可执行文件/参数；如使用 shell wrapper 则严格引用，不拼用户输入到 shell。

创建 shell 再 SendKeys 时，字面输入用 `send-keys -l`，Enter 单独发送；更建议让受控启动包装器记录实际退出码和 pane 身份。`claude code` 不是默认启动命令。

tmux 的 session/window/pane ID 用于定位，PID 加启动标识验证；进程结束必须 Wait，主动停止等待退出后才考虑升级信号，不能固定 sleep 后无条件 kill 一个可能已复用的 PID。

配置快照只恢复启动配置。daemon 重启与 tmux 生存关系依赖 [11](./11-automated-installation.md) 的服务模型，需故障注入确认。

## 5. 认证与授权

使用 jwt/v5 显式限制 HS256、验证 issuer/audience/exp/iat/jti 和撤销集合。要求 exp 存在，不能允许没有期限的 Token 悄悄通过默认解析；密钥长度和熵在初始化时检查。nonce/jti 不宣称防重放。

Connect-Go v2 用统一 connect.ServerInterceptor 包装所有 unary/stream，在读取业务消息前认证；按 connect.Spec.Procedure 查权限表，context 用私有类型键。通过 connect.CallInfoForServerContext 读取请求头并检查存在标志；Authorization 必须只有一个规范 Bearer 值；Token 无效用 Unauthenticated，有效但权限不足用 PermissionDenied。

方法映射采用显式表，举例：

| RPC | 权限 |
|---|---|
| NegotiateProtocol | 只要求有效 Token，不要求业务权限 |
| ListPlugins / ManagePlugin / WatchPluginLogs | plugin.manage |
| GetStats / WatchStats | system.stats |
| ListProjects / GetProject | project.list |
| CreateProject / DeleteProject | project.create / project.delete |
| ListSessions / ListProjectSessions / GetSession | session.list |
| CreateSession / UpdateSession / DeleteSession | session.create / session.update / session.delete |
| TerminalStream | session.attach；每个输入/resize/control 再检查 session.write + 控制权 |
| TakeControl | session.control + session.write |
| StopClaude / RestartClaude | session.control |
| ReadFile / StatFile / ListFiles | file.read |
| WriteFile / DeleteFile | file.write |
| WatchFiles | file.watch |
| ListPorts / WatchPorts / ForwardPort | port.list / port.list / port.forward |
| GetSystemInfo / GetClaudeVersion / HealthCheck | system.info |
| CreateSnapshot / RestoreSnapshot / ListSnapshots / DeleteSnapshot | session.create / session.create / session.list / session.delete |

本地 Agent 与 NodeControl 的权限边界见 [13](./13-local-ipc-protocol.md)，插件 CoreAPI 的能力白名单见 [09](./09-protocol-definition.md)。

新增 RPC 未进映射则拒绝，不能从 CamelCase 推导反序权限字符串。公网不匿名暴露完整健康详情；安装器可通过同用户本地管理通道检查 ready。

流建立时验证权限、资源归属，绑定 Token jti；到期定时取消，撤销主动取消该 jti 的全部流。TakeControl 等跨 attachment 操作验证同一会话及目标资格。实现不能只给 unary RPC 加拦截器。

## 6. ConnectRPC 实现边界

具体 transport、v2 API、错误重建与验收见 [14](./14-connectrpc-transport.md)。HTTP middleware 负责协议/方法/HTTP 版本准入；Connect interceptor 负责身份及方法权限；handler 负责资源/首帧/每帧授权。每层均不得把 HTTP 200 当作流成功。go-plugin 原生 gRPC adapter 单独配置 unary/stream 拦截器、1 MiB 收发限制及 status/Connect 错误转换；Connect 配置不自动覆盖它。

宿主转发每一跳独立创建客户端 context/header，deadline 只收紧；不透传 Authorization、Connect-*、Grpc-* 或下游全部 trailers。取消时同时唤醒阻塞的 Send/Receive；正常文件提交必须读到 Connect CloseAndReceive / gRPC CloseAndRecv 成功，端口半关闭保留另一方向直到最终 RPC 状态。

## 7. 具体技术验证清单

实现后需编译生成的 protobuf，运行真实进程级 go-plugin 测试和 tsnet/直连 TLS 测试；仅通过伪代码或 mock 不算端到端验收。重点覆盖：终端重绘/慢消费者/退出回收、双向文件冲突/竞态恢复/路径逃逸、TCP 半关闭/残余数据、Token 撤销流、systemd 重启保活、多 CLI 的单 agent 归属。
