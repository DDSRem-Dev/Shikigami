# 连接模型设计

## 传输和节点归属

应用数据统一使用 **Connect 协议 + protobuf 二进制编码 + HTTP/2**，Go 实现基线为 Connect-Go v2.0.2（库要求 Go ≥ 1.26.0；项目固定 tsnet v1.98.10 后要求 Go ≥ 1.26.5，工具链为 1.26.10）。保留 protobuf 的 service/message 定义，替换 RPC 运行库、生成器与 HTTP 传输。所有业务链路的具体约束见 [14 ConnectRPC 传输契约](./14-connectrpc-transport.md)，不通过 SSH。性能是否优于 SSH 需要测量，不作为选型事实。

采用内嵌 **tsnet**：本地 agent 和远端 daemon 各持有一个长生命周期 `tsnet.Server`，使用私有持久化目录保存节点身份。同一目录只能由一个进程打开；CLI 不自行创建节点。多台设备需不同节点名/身份。

```go
// API 示意：实现时锁定 tsnet 版本并检查每一步错误。
node := &tsnet.Server{Hostname: hostname, Dir: stateDir}
// daemon: node.Listen("tcp", ":9527")
// agent:  node.Dial(ctx, "tcp", "shiki-vps:9527")
// node.Close() 由 agent/daemon 的 shutdown 调用，不能在单次 Dial 返回前 defer。
```

tsnet 的首次认证通过 13 的本地 NodeControl 引导，具体 SDK 行为须先通过 11 的 Phase 0 原型。首次认证可能产生登录 URL 或使用一次性 auth key；登录完成并确认节点 online 后才显示可连接地址。地址必须来自内嵌节点，不能调用系统 `tailscale ip` 代替。用户可使用官方控制面或兼容的自建控制面，兼容性单独验收。

## 连接优先级

1. tsnet 连接：P2P 与 DERP 由 Tailscale 内部自动切换，Shiki 不分别重拨两个策略。
2. 可选公网/私网直连：用户显式配置独立 TLS listener 和可信服务器身份后启用。
3. WebSocket 隧道：后续版本提案，首版不得显示为可用回退。

每个策略必须完成有 deadline 的连接与已认证探测，不能把 `connect.NewClient` 或生成客户端构造成功当作已建立网络连接。只有可恢复的网络失败触发回退；`Unauthenticated`、`PermissionDenied`、证书错误应停止并报告。

agent 通过 `ServerRecord.connection_attempts` 报告最近一次已发布路由选择真正进入构造器的顺序，值仅为 `tsnet` / `direct-tls`、最多两项。进入构造器后即使因节点未授权或信任配置错误在拨号前失败，也属于该策略的尝试；这不是成功连接证明、实际 TCP 拨号次数、重连累计历史，也不把 P2P/DERP 分成独立策略。认证拒绝仍立即停止回退，未执行的候选不会出现。无凭据/未选择或旧 agent 缺字段时为空，界面显示未报告，不能推断从未尝试。

该列表仅在连接配置和凭据代际复核通过后发布到运行态；过期结果不能污染新代，重新选择时不累积历史。已有路线的认证复核不改变最近一次选择诊断。当前实际路径仍独立读取 `connection_path`，不能从最后一项推断；列表不含地址、原始错误或凭据，不写入服务器配置或去重结果。


状态显示 `tailscale-direct / tailscale-relay / direct-tls / unknown`，无法取得路径信息时显示 unknown，不能一律打印 P2P。单次延迟高不主动切换线路；连续失败、冷却时间和用户配置共同决定重连，防止抖动。

## 配置契约

```yaml
# daemon.yaml（语义示意；加载时校验未知键）
run_user: rem
state_dir: /var/lib/shiki
server:
  port: 9527                 # tsnet 内 listener
  direct_tls:
    enabled: false
    address: ""             # 例如 vps.example.com:9528
    cert_file: ""
    key_file: ""
tailscale:
  enabled: true
  hostname: shiki-vps
  state_dir: /var/lib/shiki/tailscale
  control_url: https://controlplane.tailscale.com
# auth key 在引导阶段读取，不长期写入常规配置。
```

```yaml
# ~/.shiki/config.yaml
agent:
  socket: ~/.shiki/run/agent.sock
  state_dir: ~/.shiki/agent
tailscale:
  hostname: shiki-macbook
  state_dir: ~/.shiki/tailscale
servers:
  my-vps:
    tailscale_address: shiki-vps:9527
    token_ref: my-vps-macbook    # 私有凭据存储的引用
    direct_tls_address: ""     # 默认无公网回退
    tls_server_name: ""
```

配置中 `~` 必须由加载器显式展开。凭据可先存入独立 0600 文件，GUI 后续接入系统钥匙串。导出配置、日志、诊断报告不包含 Token/auth key。

## 加密与认证

仅当 TCP 字节明确经过受控 tsnet 连接时，可依赖 WireGuard 加密，在其内部使用 HTTP/2 prior knowledge（h2c）。该连接构造器必须固定目标、封装 tsnet dialer、禁用环境代理和重定向，不得把明文 HTTP transport 用于普通网卡连接。公网直连强制 TLS，验证 CA 和服务端名称或显式受信 pin；证书错误不回退为明文。

显式配置的 TLS CA、证书和私钥分别限制为非空、最多 1 MiB 的普通文件；打开和读取必须有界，FIFO/设备不能阻塞连接管理。证书路径允许显式 symlink（如系统 CA 或证书轮换目录），验证已打开文件的类型与大小；这不放宽项目文件同步的 no-follow 边界。

Connect 的 HTTP header 本身不证明传输安全。凭据仅由受控 tsnet/TLS transport 所绑定的客户端拦截器注入；公网路径验证 TLS/ALPN h2，tsnet 路径只允许 node.Dial。IPC、插件和远端分别建客户端，不能跨目标复用带凭据的 transport，也不提供接受任意 URL 的带 Token 客户端。

Token 格式为 `shiki_<JWT>`，HTTP 请求头为 `Authorization: Bearer shiki_<JWT>`。JWT 使用 HS256、持久化随机 256 位密钥、标准 `iat/exp/iss/aud/jti` 与应用 `user/permissions`；`aud` 绑定稳定 daemon ID。验证签名算法、签发者、受众、过期、未来签发时间及撤销状态。

`jti` 用于撤销和设备追踪，不让 Bearer Token 自动具备防重放能力。传输加密、限制有效期和撤销才是当前防护。生成 Token 的本地管理命令必须读取 daemon 的同一配置和密钥；服务重启不能换密钥。

```bash
shiki-daemon token create --user rem --expires 720h
shiki-daemon token list                    # 仅显示 token ID，不能显示原始凭据
shiki-daemon token revoke <token-id>
shiki-daemon token refresh <token-id> --expires 720h
```

`--user` 只能匹配配置的运行用户。refresh 签发新 JWT/jti 并撤销旧 ID；不得延长旧 JWT 字节的 exp，也不得用已过期 Token 自助续期。撤销和过期关闭关联 RPC 流（包括插件代理），远端 tmux 会话继续运行。终端只读权限仍可读取敏感输出，不是匿名访客功能。

## 权限

所有 unary 和 stream RPC 都需要认证；使用显式 method → permission 表，未知方法默认拒绝，不能把 CreateSession 自动转成 `create.session`。

- 项目：`project.list/create/delete`。
- 会话：`session.list/create/update/delete/attach/write/control`；`attach` 只授予查看，写入还需 `write`，接管还需 `control`。
- 文件：`file.read/write/watch`；所有路径限制在已授权项目根内。
- 端口：`port.list/forward`；权限不等于任意目标地址的访问权。
- 系统：`system.info/stats`；插件状态管理：`plugin.manage`（本地配置另由 agent 权限保护）。

`*` 和 `<namespace>.*` 可表示管理者权限。只读角色不含 `write/control`。Token 权限限制 API 入口，不是拥有终端写权限用户的安全沙箱。

## 超时、重连和流恢复

- 短 RPC 默认 5 秒；文件传输按大小设 deadline；终端流使用可取消 context 与 Token 有效期，不用永久 Background。
- 默认最多 5 次重连：1 秒起、倍增、最大 30 秒，加抖动。等待可由用户取消；只有网络类错误重试。
- HTTP/2 连接健康检查、空闲连接回收与业务流存活分别配置；不沿用 grpc-go keepalive 参数。终端允许长期无输出，不能将静默当故障。禁用覆盖整条长流的 http.Client.Timeout 和 Server.ReadTimeout/WriteTimeout；短 RPC 用 context deadline，握手/请求头另设有界超时，见 14。
- 新连接仍需认证并按 09 的 NegotiateProtocol 协商版本/能力；不匹配处理见 06。终端重新打开 TerminalStream，发送 Open 首帧；不是调用另一条 AttachSession 输出流。
- 首版恢复同一远端会话并重绘当前屏幕，不承诺断线期间所有输出逐字节补齐。无持久化回放日志时，不接受 `resume_from` 伪承诺。
- 不自动重放终端输入、TCP 数据或非幂等创建请求。创建操作使用 request_id 去重并持久化结果；失败未知时先查询。
- 文件恢复按共同基线与当前清单协调，不按最后网络包续传猜测内容。

## 代理与后续 WebSocket

HTTP CONNECT 与 SOCKS5 是不同协议，配置必须明确类型；保留 TLS 端到端验证，并验证实际代理可达性。DERP 可覆盖一部分受限网络，但不能保证适用所有企业代理。

macOS 的内嵌 tsnet 使用纯 userspace netstack，不创建内核 TUN 或系统路由。其控制面、DERP 和其他外层承载 socket 应遵循操作系统现有路由，允许已有 VPN/代理隧道承载；不得强制绑物理网卡而绕过解析所得虚拟地址的实际出口。固定 SDK 的平台适配须在节点启动前选择与其 `userspace-networking` 模式一致的进程级网络策略，不修改系统 Tailscale、全局代理或路由，也不随单个 Node 关闭恢复进程级开关。此策略仅针对当前无真实 TUN/Router 的 macOS 嵌入方式；未来引入内核接口或升级 SDK 必须重验。

这不改变业务数据面的隔离：`Node.Dial` 仍须经过授权与 peer 身份检查并直接进入 userspace TCP，禁止回退系统 TCP；OS 的外层隧道不能代替 Shiki 自己的 tailnet/Token 授权。SDK 的 HTTP(S) 环境代理与应用业务 transport 分开，后者继续禁用环境代理。macOS 纯 userspace 策略不提供 `ALL_PROXY` 隐式 SOCKS 包装；未实现并实测的代理组合不得声明支持。当前修复与现场验收状态见[24](24-public-package-compatibility.md#external-installer-20261031)。

Connect 协议不自动提供 WebSocket 传输；Connect-Web/gRPC-Web 也不让浏览器获得终端所需的全双工能力。底层 WebSocket socket 不是自动解帧后的字节流。后续若做 WSS 隧道，需要两端明确的 HTTP/2 字节封装适配、鉴权、帧大小限制、关闭语义和代理测试。没有适配实现前不声明与 9527 同端口复用，也不把此功能纳入首版验收。
