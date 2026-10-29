# ConnectRPC 传输契约

本文规定远端 RPC、本地 agent IPC 和插件 RPC 的传输实现；消息与方法分别以 [09](./09-protocol-definition.md)、[13](./13-local-ipc-protocol.md) 为准。当前已有传输实现与部分跨进程测试，完整验收状态见 [15](./15-implementation-status.md)。资料核对日期：2026-10-27；实现基线为 **connectrpc.com/connect/v2 v2.0.2**，库自身要求 Go ≥ 1.26.0；项目为满足固定 tsnet SDK 使用 **Go ≥ 1.26.5、工具链 1.26.10**，不能照搬 Connect-Go v1 示例。

## 1. 选型与协议范围

ConnectRPC 是 RPC 生态与库；Connect 是具体线协议；protobuf 是 IDL 和消息编码。Connect-Go 能处理 Connect、gRPC、gRPC-Web 三种协议，不意味着本项目同时提供三种协议。

- 可独立替换的链路统一使用 **Connect 协议 + protobuf 二进制消息 + HTTP/2**：agent⇄daemon、前端⇄agent、NodeControl 和插件 CoreAPI 回调。不保留远端/前端 gRPC 兼容入口。用户确认仅 go-plugin 所要求的宿主→插件生命周期/worker 继续使用原生 gRPC，见第 7 节。已有业务 proto 的字段编号、service 名、流方向和权限边界不因迁移改变。
- Connect Go 客户端使用 connecthttp 默认 Connect/protobuf，不启用 WithGRPC、WithGRPCWeb、WithProtoJSON 或 WithHTTPGet。
- Connect 服务端不能仅依赖库默认值：HTTP 准入层仅接受 POST、HTTP/2，以及 `application/proto`（unary）或 `application/connect+proto`（streaming）。用 MIME parser 解析 Content-Type，再由 handler 校验其是否匹配方法形态；拒绝 gRPC/gRPC-Web、JSON 请求和 GET。JSON **错误响应及 EndStream 控制帧**仍属于 Connect 协议，不能一并禁用。
- connecthttp.Mount 使用 WithRequireConnectProtocolHeader；流请求同时在准入层检查 `Connect-Protocol-Version: 1`。这个 header 不是身份认证，也不是 Shiki 应用协议版本。拒绝重复/歧义值，不自行设置保留的 Connect-/Grpc- 业务头。
- 全部 RPC 响应禁止缓存（Cache-Control: no-store），反向代理不得缓存、重试写操作或缓冲流。Connect 入口默认不启用 CORS、cookie 认证、反射或额外健康服务（go-plugin 内部健康/控制服务由库管理）；应用健康查询仍按权限表授权。

Connect 的 unary 可以运行在 HTTP/1.1，Go 也支持客户端流和服务端流；**双向流必须端到端 HTTP/2**。为了避免“列表可用、终端失败”，首版所有 RPC 入口统一要求 HTTP/2，不提供自动降级。

## 2. 传输矩阵与连接所有权

| 链路 | HTTP 传输 | 身份与所有者 |
|---|---|---|
| CLI/TUI/GUI Go 后端 → agent | UDS 上 h2c prior knowledge | 双向 peer UID 校验；frontend/instance header；agent 单实例 |
| agent → daemon（默认） | tsnet TCP 上 h2c prior knowledge | WireGuard + tailnet 准入 + 应用 Token；agent 持有节点和连接池 |
| agent → daemon（可选直连） | HTTPS + ALPN h2 | CA/名称或受信 pin + 应用 Token；显式启用 |
| 插件 → 宿主 CoreAPI | 独立私有 UDS 上 Connect/h2c | 同 UID + 实例能力凭据，启动契约见 04 |
| 宿主 → 插件生命周期/worker | go-plugin 原生 gRPC/HTTP2（AutoMTLS） | go-plugin 私有 Unix socket 与父子进程管理；不经过 Connect HTTP 准入层 |
| 安装器/本地管理 → NodeControl | 私有 UDS 上 h2c prior knowledge | daemon/agent 对应 OS 用户；不向远端公开 |

UDS 不通过 TCP 代理；tsnet 只通过对应 node.Dial。每个受信目标/凭据域独立构建 http.Transport、http.Client、Connect client；可在其内部复用 HTTP/2 连接，不把“一台服务器一个逻辑连接”理解为必须只有一条 TCP 连接。文件/端口并发与队列有限，必要时独立连接池保护终端延迟。

### h2c 构造要求

Go 1.26 的 net/http 原生支持 unencrypted HTTP/2，无需额外 h2c wrapper。示意如下，省略的 dialer 必须由连接模块提供并已验证目标/UID；这不是完整安全初始化代码：

```go
protocols := new(http.Protocols)
protocols.SetUnencryptedHTTP2(true)
transport := &http.Transport{
    Protocols: protocols, // h2c 客户端不要同时 SetHTTP1(true)
    Proxy: nil,
    DialContext: dialTrustedEndpoint, // UDS 固定路径，或固定目标的 tsnet.Dial
}
httpClient := &http.Client{
    Transport: transport,
    // Timeout 保持 0；每个 RPC 单独携带可取消 context/deadline。
    CheckRedirect: func(*http.Request, []*http.Request) error {
        return errors.New("RPC redirects are disabled")
    },
}
```

UDS base URL 如 `http://shiki-agent`、`http://shiki-core` 是合成 authority；dialer 不解析它们为公网地址。tsnet base URL 对应已固定节点/端口，dialer 拒绝其他目标。服务器在 UDS 或 node.Listen 返回的 listener 上调用 http.Server.Serve，并显式设置 Protocols.SetUnencryptedHTTP2(true)；不启用 HTTP/1。

TLS 路径单独构建 HTTPS transport/server，显式启用 HTTP2、禁用 HTTP1，配置 TLS 信任与名称，验证协商 ALPN 为 h2；不得 InsecureSkipVerify 或降级 h2c。经过代理时，客户端到代理、代理到服务端都必须满足 HTTP/2 双向流要求；只有 HTTP CONNECT 字节隧道可透明保留端到端 TLS，普通 HTTP/1 反代不满足要求。

库客户端构造通常不拨号；连接 ready 需实际完成认证后的 NegotiateProtocol。Phase 0 还必须验证双向首帧互通，不能用一次 unary 响应证明代理支持全双工。

## 3. v2 生成、注册和调用

09 定义四个 proto 的生成命令和 Go package。protoc-gen-connect-go v2.0.2 与运行库锁定同版，protoc-gen-go 使用 v1.36.11，protoc 编译器基线为 31.1，发行工具的校验值在 Phase 0 记录。生成 *.pb.go 消息及 *connect/*.connect.go 服务代码。仅 plugin.proto 额外使用 protoc-gen-go-grpc v1.6.2 生成 plugin_grpc.pb.go（grpc-go v1.85.0、go-plugin v1.8.0），供必须保留的插件通道使用；不为远端 shiki.proto 或 agent.proto 生成 gRPC stub。

```go
// 片段：authInterceptor、实现对象和 httpClient 由各自模块提供。
server := connect.NewServer(authInterceptor)
protocolconnect.RegisterShikiDaemonHandler(server, daemon)
mux := http.NewServeMux()
connecthttp.Mount(mux, server,
    connecthttp.WithRequireConnectProtocolHeader(),
    connecthttp.WithCompressors(), // 首版禁用压缩
    connecthttp.WithReadMaxBytes(1<<20),
    connecthttp.WithSendMaxBytes(1<<20),
)
// mux 外包 HTTP 协议准入层，再交给已配置的 http.Server。
client := protocolconnect.NewShikiDaemonClient(connect.NewClient(
    connecthttp.NewTransport(httpClient, baseURL,
        connecthttp.WithCompressors(),
        connecthttp.WithReadMaxBytes(1<<20),
        connecthttp.WithSendMaxBytes(1<<20),
    ),
    credentialInterceptor,
))
```

unary 接口直接接收/返回 protobuf 指针，不使用 v1 的 connect.Request/Response。v2 用 connect.NewClientContext 得到每次调用独立的 CallInfo，通过 RequestHeader().Set 写入业务 header；服务端 connect.CallInfoForServerContext 必须检查第二个返回值。HTTP/TLS 细节通过 connecthttp 的 context API 获取，不放入 protobuf 请求伪装为可信事实。

使用 `connect.ClientInterceptor` / `connect.ServerInterceptor` 函数统一覆盖 unary 和 streaming。ServerInterceptor 通过 Spec.Procedure 对照完整方法路径权限表，在读取业务消息前认证；handler 再验证资源、scope 和流首帧。客户端拦截器只包建流过程，统计完整耗时/终态必须包装 stream 的 Receive/Close 等操作，不能把“创建 stream 无错误”记为调用成功。

## 4. 四种 RPC 与流结束

| RPC 类型 | 项目例子 | v2 调用约定 |
|---|---|---|
| unary | CreateSession、Handshake、Init | protobuf 指针 → 指针/error；结果未知按 request_id 查询，不盲目重放 |
| server streaming | ReadFile、WatchEvents、WatchFiles | Receive() → (消息, error)，读取到 io.EOF 才是正常结束；其他错误为失败 |
| client streaming | WriteFile、RemoteWriteFile | 单 sender 发送 header/chunks，CloseAndReceive() 关闭请求并获取唯一响应及最终状态 |
| bidirectional | TerminalStream、ForwardPort、RemoteForwardPort | Send 与 Receive 分属两个协程；每方向仅一个操作序列，CloseSend 后继续读响应，退出时 Close |

Connect-Go v2 生成方法专用 stream 类型；不要混入 grpc 的 Recv、SendMsg/RecvMsg 或 v1 Receive() bool 写法。`io.EOF` 用 errors.Is 判断；Send 端失败不等于已得到服务端最终错误，在可继续接收且 context 有效时读取响应终态。取消或严重 I/O 故障时取消整条代理调用链，回收两方向协程和资源。

- TerminalStream 的 Open/READY/RESET、attachment 与控制权协议不变；网络重连新建流，不重放输入。输出 bytes 不经文本编码往返。
- WriteFile 必须在所有块完成并获成功终态后记录提交结果；已提交但响应丢失按 revision/hash 对账，不盲目再写。ReadFile 收到完整字节但最终状态失败也不能提交本地文件。
- ForwardPort 的 HALF_CLOSE 是应用层 TCP 半关闭消息，不是 RPC 整体成功。发送该帧后客户端可 CloseSend，服务端仍继续发送剩余数据；服务端用 HALF_CLOSE 表达自身写方向结束，最终 handler 返回才结束 RPC。代理逐跳保持这一语义，不能收到一个方向 EOF 就取消另一方向。
- Connect streaming 的 HTTP 200 只说明进入流响应，最终结果位于 EndStream envelope；缺少合法终帧/网络截断不能当成功。trailer/终态由 SDK 解析，不手写帧解码或把它当作业务 protobuf。

## 5. 跨代理错误、header 与授权

Connect 错误代码与 gRPC 同名同义，所以现有 FailedPrecondition、Aborted 等领域约定保留；Go 使用 connect.CodeOf、connect.NewError(code, message)。不要仅从 HTTP 状态判断错误：FailedPrecondition/InvalidArgument 都可能是 400，流式错误可能仍是 HTTP 200。

**v2 将收到的错误标记为 remote；handler 直接返回该错误时，对外会变为 Internal。** 每个 Connect 出口边界（agent、daemon、CoreAPI）必须重建本地 connect.Error；go-plugin 的 gRPC 出口另按第 7 节重建 status.Error：

1. 区分本地取消、网络错误和已收到的下游业务错误；本地 deadline/cancel 保留对应 code。
2. 已知业务错误保留 code，例如 PermissionDenied、Aborted、NotFound、ResourceExhausted；插件不可用按已定义的 FailedPrecondition，传输故障用 Unavailable；未知内部错误不泄露底层细节。
3. 从固定消息模板或允许的 reason_code 生成安全文案；不复制任意下游 Message、Cause、header/trailer。WithCause 仅保留本机诊断原因，不在网络序列化。
4. 结构化详情只透传白名单类型及字段；可使用 google.rpc.ErrorInfo 的 reason/domain 和受限故障层字段，通过 connectproto.NewErrorDetail + WithDetail（返回新对象）构造。未识别的详情不转发。
5. 日志记录安全的 procedure、故障层和 code；Token、auth key、终端内容、文件内容及登录 URL 不进入通用访问日志。指标以 RPC 终态统计。

当前版本诊断白名单限于 FailedPrecondition 下 `google.rpc.ErrorInfo` 的 `shiki.protocol` domain。`PROTOCOL_INCOMPATIBLE` 只接收六个规范 uint32 版本区间字段、最多 64 字节构建标识、最多八个各 96 字节能力名及 agent/daemon/plugin/node 组件名；`REMOTE_CAPABILITY_UNAVAILABLE` 使用独立 schema，仅允许 daemon 组件与一个能力名。只检查前八个详情、单个编码不超过 2048 字节；未知字段、原因、domain、非法字符和超限值不转发，重建时丢弃 protobuf 未知字段和 debug。Connect 与 go-plugin gRPC 共用同一校验，绝不把任意 metadata 转为可显示诊断。

远端 Authorization 由 agent 从 server_id 凭据存储注入；前端的身份/路由 header 不转发到 daemon。daemon 到插件使用 go-plugin AutoMTLS 的受信实例通道及已验证 scope，不传用户 Token；插件回调 CoreAPI 使用独立短期实例凭据。每一跳新建 header 集合，deadline 继承但不得延长，取消向下传播；不可把整份 HTTP header 透明复制。

RPC 建立后持续追踪 jti 的撤销/过期；取消关联上下文并终止读写。仅修改未来请求的客户端 header 不会使既有流重新认证；凭据变更需要取消旧流并重新协商。连接复用不允许跳过每次 RPC 的认证。

## 6. 资源限制、超时与退出

- 客户端 NewTransport 和服务端 Mount 都显式设置 WithReadMaxBytes/WithSendMaxBytes 为 **1 MiB/消息**；按解压后消息限制验收，不依赖库默认值。文件/端口 DATA 块上限 **256 KiB**，自动同步文件累计默认上限 **10 MiB**；限制是三个不同层次。
- 首版客户端和服务端均用 WithCompressors() 显式禁用压缩（库默认支持 gzip），压缩请求由 SDK 拒绝。以后启用压缩需测试解压后大小限制，并测量 CPU 与交互延迟；不能仅关闭请求压缩却误以为响应也不会自动压缩。
- 不给整个终端/转发流套固定 http.MaxBytesHandler 或短 ReadTimeout/WriteTimeout；分别控制消息、并发流、每身份资源、文件累计大小和有界队列。请求头大小/读取时间、TLS 握手和连接建立使用有限上限。
  - 当前文件准入策略：同一daemon单OS用户共享metadata32（List/Stat/Delete）、transfer16（Read/Write）、watch32三类独立计数，不按Token/连接/handler重新分配。授权后、读取首帧前占用，超限立即ResourceExhausted，不排无限队列；worker另设相同限额。宿主文件RPC首个完整请求消息、worker上传首header默认5秒，保留更早父截止，收到后清除该首帧限制，不给Watch整流设5秒超时。这些数值是实现策略，不把消息/文件大小上限当并发限制。
  - 当前端口元数据准入策略：同一daemon单OS用户共享metadata32（ListPorts/CheckPort）、watch32两类独立计数，授权后、首个完整请求消息前占用，跨Token/连接/handler不重置；超限立即ResourceExhausted。首请求默认5秒并保留更早父截止，成功接收后清除，不限制正常Watch整流时长。port worker分别限制短查询32、Watch32，ForwardPort另按max_connections准入并等待有界CONNECT；这些默认值为实现策略，短查询不会被长Watch占满。私有AutoMTLS gRPC首message解码并非此远端HTTP2预算的保护点；远端typed请求完整接收后才调用worker。
- http.Client.Timeout 与 Server.ReadTimeout/WriteTimeout 覆盖整个流，长流路径保持 0；短 unary 默认 5 秒，启停按 13，文件按大小设 deadline，所有长期流都绑定可取消任务 context。
  - 当前后台同步实现的文件预算为 `30秒 + ceil(size / 64KiB)秒`（默认10MiB为190秒），覆盖打开流、首帧、传输及最终ACK，保留更早父截止；这是整次操作预算，收到进度不重置。下载先用默认5秒StatFile取得当前大小并核验revision；上传从本地阶段文件首header取大小。WatchFiles/终端/端口长期流不套此文件预算。预算超时后提交结果未知的写入仍按10的日志恢复规则重观测，不盲目重放。
- HTTP/2 PING 与 IdleTimeout 是连接级机制，不代表业务输出心跳。闲置终端仍合法；HTTP/2 流控不能代替应用队列上限或取消逻辑。具体 HTTP2Config 参数须在锁定 Go 版本上通过丢包/休眠/DERP 测试后设定，不复制 grpc-go keepalive 配置。
- 优雅退出先阻止新任务、取消长期流与 worker，再对 http.Server.Shutdown 设有限 deadline，超时 Close 并 Wait 回收；只调用 Shutdown 会等待长期流而无法退出。关闭空闲连接池不能终止所有活跃 RPC，仍需取消其 context。tmux 保活规则不变。

## 7. go-plugin 的 gRPC 例外与适配

不以 connecthttp.WithGRPC 伪装成 go-plugin 集成：go-plugin 要求注册原生 `*grpc.Server`、从 `*grpc.ClientConn` 构造客户端，并管理启动握手、健康和关闭。保留这些机制；所有远端和前端接口仍由 Connect-Go 承载。

| service | 唯一运行传输 | 监听归属 |
|---|---|---|
| ShikiDaemon、Agent、NodeControl | Connect | 各自远端或本地专用 listener，按 13 分别注册 |
| CoreAPI | Connect | agent/daemon 为每个插件实例建立的私有 UDS |
| Plugin、SyncWorker、PortWorker、FileService、PortService | go-plugin 原生 gRPC | 插件进程中 go-plugin 管理的私有 listener，按 side/能力白名单注册 |

同一个 plugin.proto 生成两套绑定是生成器以文件为粒度的结果；未使用的 service 不注册。grpc-go 依赖仍会进入宿主/插件产物，这是明确保留的例外。非 Go 插件必须同时实现 go-plugin 的 gRPC 启动契约以及 CoreAPI Connect 客户端，不能声称只有一套 Connect stub 就可以运行。

适配层仅转换消息、状态和生命周期，不在远端接收原生 gRPC：

- 同一 protobuf 消息可以复用，grpc stream 和 Connect stream 不可互换。gRPC 使用 Send/Recv；Connect v2 使用 Send/Receive。gRPC 客户端上传结束调用 CloseAndRecv，Connect 客户端使用 CloseAndReceive。gRPC 服务端上传成功用 SendAndClose，Connect handler 返回 protobuf 指针；都必须等待下游最终结果后再向上游报告成功。
- 双向流每方向单发送者，HALF_CLOSE 原样保留；下游 gRPC CloseSend 后继续 Recv 直到最终状态，再完成 Connect handler。任一跳取消都要停止下游调用并唤醒阻塞读写；不能把半关闭当整体取消。
- gRPC→Connect：用 status.FromError/status.Code 分辨状态，显式映射 codes.* → connect.Code*，安全重建 connect.NewError；io.EOF 是成功流结束，不按 Unknown 转发。context 错误先单独识别，非 gRPC 内部错误不泄露文本。
- Connect→gRPC：插件回调 CoreAPI 失败后，若需要从 worker RPC 返回，按同名代码显式映射并用 status.Error/WithDetails 重建；直接返回 *connect.Error 会丢失 gRPC code。任一方向都不依赖枚举强转或复制原始错误字符串。
- ErrorInfo 等详情只转发共同白名单的已校验字段；转换失败保留安全 code，不把整份 metadata/trailer 传到另一协议。用户 Token 永不进入插件，实例凭据也不穿越远端链路。
- 两套运行库分别配置限额、拦截器和日志：Connect interceptor 不会保护 grpc.Server；原生 gRPC 同时安装 unary/stream 校验，并验证受信 host 的实例/scope。库内健康/控制方法按 go-plugin 协议单独放行，不按业务权限表误拦截，也不能匿名公开到外部。

插件本身的隔离、AutoMTLS、CoreAPI endpoint 和重启策略见 [04](./04-plugin-system.md)。Phase 0 要验证真实进程的“Connect CoreAPI → agent → Connect daemon → gRPC worker”链路；协议编译或内存中的转换测试不能替代进程验收。

## 8. Phase 0 与后续验收

以下是必须完成的实现验收，不代表本仓库已有测试通过：

- 四份 proto 同时生成消息和 v2 stub，plugin.proto 另生成 gRPC stub 并一起编译；真实 UDS/tsnet/TLS 上验证 unary、server/client/bidi streaming。
- 终端“客户端尚未 CloseSend 就收到 READY/输出”；文件 CloseAndReceive 得到提交或冲突；端口半关闭后仍收齐剩余数据；所有接收方读取 RPC 终态。
- tsnet 禁止普通网络拨号、TLS 校验失败停止、HTTP/1/GET/JSON/gRPC/gRPC-Web/重定向被拒绝；无需开放 CORS 或公网 h2c。
- go-plugin 双协议适配验证 Init 前 CoreAPI ready、凭据失效、回调无死锁，以及 gRPC/Connect 双向错误和四种流形态转换。
- 每条代理链验证 Unauthenticated、PermissionDenied、Aborted、ResourceExhausted 不被错误转换成 Internal；不泄漏下游敏感消息/headers；缺少终帧不能成功。
- UID/instance/能力/权限拒绝、Token 撤销、超限消息、慢消费者、长时间静默、取消、进程死亡和优雅退出均不遗留 writer/协程。
- Wails Go 后端原型调用 agent，GUI 不直接使用 Connect-Web 承载终端。纯 Web 客户端属于后续独立设计。

Phase 1 使用已验证的 TLS 核心链路，Phase 2 接入真实 tsnet 和插件，后续按 [08](./08-roadmap.md) 推进；传输库升级不能自动改变应用 capability 或交付状态。

## 9. 官方资料与版本核对

- [Connect 协议规范](https://connectrpc.com/docs/protocol/)：HTTP 方法、编码、envelope、终态和错误映射。
- [Go 入门](https://connectrpc.com/docs/go/getting-started/)与 [v2 迁移](https://connectrpc.com/docs/go/migrating-to-v2/)：生成、注册、客户端、API 差异。
- [流式 RPC](https://connectrpc.com/docs/go/streaming/)与 [部署](https://connectrpc.com/docs/go/deployment/)：HTTP/2、h2c、超时与浏览器限制。
- [拦截器](https://connectrpc.com/docs/go/interceptors/)、[headers/trailers](https://connectrpc.com/docs/go/headers-and-trailers/)、[错误](https://connectrpc.com/docs/go/errors/)：统一拦截器、CallInfo、远端错误重建。
- [Connect-Web 协议选择](https://connectrpc.com/docs/web/choosing-a-protocol/)：fetch transport 的能力边界，不提供 UDS 访问。
- [Connect-Go v2.0.2 源码](https://github.com/connectrpc/connect-go/tree/v2.0.2)及其 go.mod/connecthttp/option.go：最低 Go 版本、具体选项和版本锁定依据。网站可能随新版本变化，实现以锁定源码为准。
- [HashiCorp go-plugin](https://github.com/hashicorp/go-plugin)：仅提供 net/rpc 与 gRPC 原生传输，不是 Connect 插件宿主。
