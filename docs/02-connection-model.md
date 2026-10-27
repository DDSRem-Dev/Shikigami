# 连接模型设计

## 传输和节点归属

应用数据使用原生 gRPC（HTTP/2），不通过 SSH。性能是否优于 SSH 需要测量，不作为选型事实。

采用内嵌 **tsnet**：本地 agent 和远端 daemon 各持有一个长生命周期 `tsnet.Server`，使用私有持久化目录保存节点身份。同一目录只能由一个进程打开；CLI 不自行创建节点。多台设备需不同节点名/身份。

```go
// API 示意：实现时锁定 tsnet 版本并检查每一步错误。
node := &tsnet.Server{Hostname: hostname, Dir: stateDir}
// daemon: node.Listen("tcp", ":9527")
// agent:  node.Dial(ctx, "tcp", "shiki-vps:9527")
// node.Close() 由 agent/daemon 的 shutdown 调用，不能在单次 Dial 返回前 defer。
```

tsnet 的首次认证可能产生登录 URL 或使用一次性 auth key；登录完成并确认节点 online 后才显示可连接地址。地址必须来自内嵌节点，不能调用系统 `tailscale ip` 代替。用户可使用官方控制面或兼容的自建控制面，兼容性单独验收。

## 连接优先级

1. tsnet 连接：P2P 与 DERP 由 Tailscale 内部自动切换，Shiki 不分别重拨两个策略。
2. 可选公网/私网直连：用户显式配置独立 TLS listener 和可信服务器身份后启用。
3. WebSocket 隧道：后续版本提案，首版不得显示为可用回退。

每个策略必须完成有 deadline 的连接与已认证探测，不能把惰性的 `grpc.NewClient`/`Dial` 返回当作连接成功。只有可恢复的网络失败触发回退；`Unauthenticated`、`PermissionDenied`、证书错误应停止并报告。

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

仅当 TCP 字节明确经过受控 tsnet 连接时，可依赖 WireGuard 加密省略应用 TLS。该连接构造器必须封装 dialer，不能把 `insecure.NewCredentials()` 泛化到普通网卡连接。公网直连强制 TLS，验证 CA 和服务端名称或显式受信 pin；证书错误不回退为明文。

Go gRPC 的 `PerRPCCredentials.RequireTransportSecurity()` 只识别 gRPC transport credentials，不会自动识别 WireGuard。tsnet 构造器使用仅限该路径的 metadata 注入；TLS 构造器可使用要求 TLS 的 PerRPCCredentials，不能因这一冲突把所有路径的安全要求关闭。

Token 格式为 `shiki_<JWT>`，metadata 为 `authorization: Bearer shiki_<JWT>`。JWT 使用 HS256、持久化随机 256 位密钥、标准 `iat/exp/iss/aud/jti` 与应用 `user/permissions`；`aud` 绑定稳定 daemon ID。验证签名算法、签发者、受众、过期、未来签发时间及撤销状态。

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
- 客户端与服务端 keepalive/enforcement 配套配置。建议先使用 gRPC 默认值，测量后共同调整，避免单端 10 秒 ping 被服务器拒绝。
- 新连接仍需认证。终端重新打开 TerminalStream，发送 Open 首帧；不是调用另一条 AttachSession 输出流。
- 首版恢复同一远端会话并重绘当前屏幕，不承诺断线期间所有输出逐字节补齐。无持久化回放日志时，不接受 `resume_from` 伪承诺。
- 不自动重放终端输入、TCP 数据或非幂等创建请求。创建操作使用 request_id 去重并持久化结果；失败未知时先查询。
- 文件恢复按共同基线与当前清单协调，不按最后网络包续传猜测内容。

## 代理与后续 WebSocket

HTTP CONNECT 与 SOCKS5 是不同协议，配置必须明确类型；保留 TLS 端到端验证，并验证实际代理可达性。DERP 可覆盖一部分受限网络，但不能保证适用所有企业代理。

原先 `grpcweb.WrapServer` + `grpc.Dial("ws://…")` 的示例不成立：原生 Go gRPC 不会自动变成 gRPC-Web；底层 WebSocket socket 也不是自动解帧后的字节流。后续若做 WSS 隧道，需要两端明确的 HTTP/2 字节封装适配、鉴权、帧大小限制、关闭语义和代理测试。没有适配实现前不声明与 9527 同端口复用，也不把此功能纳入首版验收。
