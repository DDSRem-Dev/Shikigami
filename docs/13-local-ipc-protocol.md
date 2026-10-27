# 本地 agent IPC 协议

本文定义 CLI/TUI/后续 GUI 与 shiki-agent 的进程边界，以及拟建 `proto/agent.proto`。这是协议设计，尚无服务实现；远端与插件消息复用 [09](./09-protocol-definition.md)，不另造一套 Session/FileInfo。

## 1. 传输、身份与启动

- 使用Connect 协议/protobuf over HTTP/2 prior knowledge（h2c）over Unix domain socket；默认 `~/.shiki/run/agent.sock`。目录 0700、socket 0600，Unix socket 路径长度在启动前检查。
- 服务端每次接受连接验证 OS peer UID（Linux SO_PEERCRED、macOS getpeereid 或等价方式）等于运行用户；客户端校验目录/socket 所有者、权限及 peer UID。不接受 TCP 回退、浏览器跨域或仅凭路径名建立信任。
- socket 内不另加 TLS，安全依赖同用户 IPC。它不是同一 UID 下恶意进程之间的隔离；frontend_id 只是窗口资源归属，不能作为认证凭据。
- `shiki agent start` 由 CLI 调用用户服务管理器或启动后台进程，不是向尚不存在的进程发送 RPC。所有竞争启动者使用同一个状态目录锁，最多一个实例可创建 tsnet 节点。
- stale socket 只有在确认属于本用户、没有活进程且成功持有实例锁后才可删除；不能仅凭 dial 失败 unlink。启动 10 秒内未 ready 返回明确错误，输出日志位置，不循环拉起。
- agent 启动生成新的 instance_id；磁盘存储 schema/凭据/节点损坏时仍可提供诊断，但不启动写文件或转发任务。

UDS transport 固定拨号该 socket，使用合成 base URL `http://shiki-agent` 只作 HTTP authority，不进行 DNS/TCP 解析；禁止环境代理及重定向。两端显式启用 unencrypted HTTP/2，客户端不得同时启用 HTTP/1；实现细则见 [14](./14-connectrpc-transport.md)。服务端在 listener 接受连接时校验 UID，通过 http.Server.ConnContext 把已验证身份绑定到该连接的请求，不相信自报 UID header。HTTP/2 连接复用不改变逐请求 frontend/instance 检查。

## 2. 服务与调用身份

同一个 agent socket 注册两个业务 service：

1. `shiki.agent.v1.Agent`：本地后台、服务器配置、映射、冲突、监听和插件管理。
2. `shiki.v1.ShikiDaemon`：**受限代理**，复用 09 的项目/会话/终端消息；agent 负责注入远端 Token 和连接管理。

此外注册 `NodeControl`，用于内嵌 tsnet 的本地引导。daemon 在自己的 `/var/lib/shiki/run/admin.sock` 上仅注册 NodeControl，安装器以 daemon 用户访问；不在该 socket 暴露 agent 业务或远端文件 RPC。NodeControl 独立协商，登录尚未完成也必须可调用。

前端先调用 Agent.Handshake，发送协议范围、所需能力和设备显示名称。响应包括协议交集、instance_id、frontend_id。其后 Agent 业务调用和代理调用必须携带：

| HTTP 请求头 | 规则 |
|---|---|
| `shiki-agent-instance-id` | 必须匹配当前进程，旧值返回 FailedPrecondition/AGENT_RESTARTED |
| `shiki-frontend-id` | Handshake 分配；用于窗口归属，未登记返回 FailedPrecondition/HANDSHAKE_REQUIRED |
| `shiki-server-id` | 仅 ShikiDaemon 代理必填；空、未知 ID 拒绝，不读取全局“当前服务器” |

Agent 业务操作的 server_id 来自请求自身；若还携带路由 header，必须与请求一致。NodeControl.Negotiate/GetStatus/BeginLogin/CancelLogin 使用独立 `shiki-node-control-instance-id`（Negotiate 返回），不依赖 agent frontend_id。所有字段都不能替代 OS peer 身份检查。

前端退出调用 ReleaseFrontend，关闭该 frontend 的终端流/订阅；后台 sync/listener 继续。崩溃时以流取消清理 attachment；不持有活动流、10 分钟无调用的 frontend 登记可回收，客户端重新 Handshake。

## 3. 代理白名单与终端

允许代理：项目四个 RPC；会话 ListProjectSessions/ListSessions/Create/Get/Update/Delete/TakeControl/StopClaude/RestartClaude；TerminalStream；四个配置快照 RPC；ListPorts/WatchPorts；GetSystemInfo/GetClaudeVersion/HealthCheck/GetStats/WatchStats。

不向前端开放代理 Read/Write/Delete/List/Stat/WatchFiles、ForwardPort、远端插件管理或 NegotiateProtocol。前端用本地冲突/转发/插件接口；agent 自行完成 daemon 协商，数据面由插件 CoreAPI 访问。未列方法返回 PermissionDenied。这个白名单是路由分工，同 UID 的前端本身不是沙箱。

agent 剥离本地 header，不接受前端提供的远端 Authorization；使用该 server_id 关联的凭据。deadline/cancel 传递到子调用，业务 Connect 错误码经显式重建保留（v2 不能直接返回远端 error，见 14）；增加已脱敏的故障所在层，不无限重试非幂等请求。

TerminalStream 首帧/输出完全使用 09 的 TerminalInput/Output。agent 记录 `(instance_id, frontend_id, server_id, session_id, attachment_id)`，不与其他窗口复用同一远端 attachment。TakeControl 的目标只能属于调用前端，跨设备竞争由 daemon 的 control_revision 处理。

前端或 agent 流断开取消远端流，tmux 保留；重连要重新握手、重新打开终端并处理 RESET，绝不自动重发排队输入。原始 bytes 不经过 UTF-8 字符串往返。

## 4. proto/agent.proto

```protobuf
syntax = "proto3";
package shiki.agent.v1;
option go_package = "github.com/yourusername/shikigami/pkg/protocol/agent";
import "types.proto";
import "plugin.proto";

service Agent {
  rpc Handshake(HandshakeRequest) returns (HandshakeResponse);
  rpc GetStatus(shiki.v1.Empty) returns (AgentStatus);
  rpc ReleaseFrontend(shiki.v1.Empty) returns (shiki.v1.Empty);
  rpc Shutdown(ShutdownRequest) returns (AgentStatus);
  rpc ListServers(shiki.v1.Empty) returns (ServerList);
  rpc UpsertServer(UpsertServerRequest) returns (ServerRecord);
  rpc RemoveServer(RemoveServerRequest) returns (shiki.v1.Empty);
  rpc SetServerCredential(SetServerCredentialRequest) returns (ServerRecord);
  rpc GetPreferences(shiki.v1.Empty) returns (Preferences);
  rpc SetPreferences(SetPreferencesRequest) returns (Preferences);
  rpc ListMappings(ListMappingsRequest) returns (shiki.plugin.v1.MappingList);
  rpc PutMapping(PutMappingRequest) returns (shiki.plugin.v1.Mapping);
  rpc RemoveMapping(RemoveMappingRequest) returns (shiki.v1.Empty);
  rpc GetSyncState(shiki.plugin.v1.Scope) returns (shiki.plugin.v1.SyncState);
  rpc ListConflicts(shiki.plugin.v1.Scope) returns (shiki.plugin.v1.ConflictList);
  rpc ResolveConflict(shiki.plugin.v1.ResolveConflictRequest) returns (shiki.plugin.v1.SyncState);
  rpc ReadConflictFile(shiki.plugin.v1.ReadConflictFileRequest) returns (stream shiki.v1.FileData);
  rpc ListForwards(shiki.v1.Empty) returns (shiki.plugin.v1.ForwardList);
  rpc CreateForward(CreateForwardRequest) returns (shiki.plugin.v1.ForwardState);
  rpc StopForward(StopForwardRequest) returns (shiki.v1.Empty);
  rpc ListPlugins(PluginTarget) returns (shiki.v1.ListPluginsResponse);
  rpc ManagePlugin(ManagePluginRequest) returns (shiki.v1.PluginStatus);
  rpc WatchPluginLogs(PluginLogsRequest) returns (stream shiki.v1.LogRecord);
  rpc WatchEvents(shiki.v1.Empty) returns (stream AgentEvent);
}
message HandshakeRequest {
  shiki.v1.ProtocolOffer protocol = 1;
  string client_build = 2;
  string device_name = 3;
}
message HandshakeResponse {
  shiki.v1.ProtocolAgreement protocol = 1;
  string instance_id = 2;
  string frontend_id = 3;
  AgentStatus status = 4;
}
message AgentStatus {
  string instance_id = 1;
  string state = 2; // starting/ready/degraded/stopping
  string reason_code = 3;
  uint32 active_terminals = 4;
  uint32 active_sync_tasks = 5;
  uint32 active_forwards = 6;
}
message ShutdownRequest {
  string expected_instance_id = 1;
  string request_id = 2;
}
message ServerConfig {
  string id = 1;
  string name = 2;
  string tailscale_address = 3;
  string direct_tls_address = 4;
  string tls_server_name = 5;
  string tls_ca_file = 6;
  string daemon_id = 7; // 首次验证后固定目标身份；变更须重新确认关联
}
message ServerRecord {
  ServerConfig config = 1;
  string revision = 2;
  bool credential_present = 3;
  string credential_revision = 4;
  string connection_state = 5; // offline/connecting/ready/auth_required/incompatible
  string reason_code = 6;
  shiki.v1.ProtocolAgreement remote_protocol = 7;
}
message ServerList { repeated ServerRecord servers = 1; }
message UpsertServerRequest {
  ServerConfig config = 1; // 新建 id 为空，agent 分配
  string expected_revision = 2; // 新建为空
  string request_id = 3;
}
message RemoveServerRequest {
  string server_id = 1;
  string expected_revision = 2;
  string request_id = 3;
}
message SetServerCredentialRequest {
  string server_id = 1;
  bytes token = 2; // write-only；禁止日志、事件或响应返回
  string expected_credential_revision = 3;
  string request_id = 4;
}
message Preferences {
  string default_server_id = 1;
  bool no_color = 2;
  string detach_key = 3;
  string revision = 4;
}
message SetPreferencesRequest {
  Preferences values = 1; // 完整替换上述非敏感字段，revision 必须匹配
  string request_id = 2;
}
message ListMappingsRequest { string server_id = 1; }
message PutMappingRequest {
  shiki.plugin.v1.Mapping mapping = 1;
  string expected_revision = 2; // 创建为空；mapping.revision 由 agent 分配
  string request_id = 3;
}
message RemoveMappingRequest {
  string mapping_id = 1;
  string expected_revision = 2;
  string request_id = 3;
  bool archive_pending = 4; // 显式放弃继续同步并归档未处理状态，不删除源文件
}
message CreateForwardRequest {
  shiki.plugin.v1.ForwardSpec spec = 1; // id/revision 由 agent 分配
  string request_id = 2;
}
message StopForwardRequest {
  string forward_id = 1;
  string expected_revision = 2;
  string request_id = 3;
}
message PluginTarget {
  enum Side { SIDE_UNSPECIFIED = 0; LOCAL = 1; REMOTE = 2; }
  Side side = 1;
  string server_id = 2; // REMOTE 必填，LOCAL 为空
}
message ManagePluginRequest {
  PluginTarget target = 1;
  shiki.v1.ManagePluginRequest request = 2;
}
message PluginLogsRequest {
  PluginTarget target = 1;
  shiki.v1.PluginLogsRequest request = 2;
}
message AgentEvent {
  string instance_id = 1;
  uint64 sequence = 2;
  string type = 3; // rescan_required/server/mapping/sync/conflict/forward/plugin/node
  string resource_id = 4;
  string server_id = 5;
  string revision = 6; // 状态提示，具体内容经 Get/List 查询
}

// 同机认证引导：agent 和 daemon 各在自己的本地 socket 实现。
service NodeControl {
  rpc Negotiate(shiki.v1.ProtocolOffer) returns (NodeNegotiation);
  rpc GetStatus(shiki.v1.Empty) returns (NodeStatus);
  rpc BeginLogin(BeginLoginRequest) returns (NodeStatus);
  rpc CancelLogin(CancelLoginRequest) returns (NodeStatus);
}
message NodeNegotiation {
  shiki.v1.ProtocolAgreement protocol = 1;
  string instance_id = 2;
}
message NodeStatus {
  string state = 1; // uninitialized/needs_login/authenticating/online/expired/denied/offline
  string reason_code = 2;
  string login_id = 3;
  string login_url = 4; // 仅同 UID 本地管理响应；不进入日志/远端诊断
  int64 login_expires_at = 5;
  repeated string addresses = 6; // 当前内嵌节点地址，不是系统 Tailscale 地址
}
message BeginLoginRequest {
  oneof method {
    bool interactive = 1; // 必须 true
    bytes auth_key = 2; // 从受限文件读取后仅在本机传入，不持久化
  }
  string request_id = 3;
}
message CancelLoginRequest { string login_id = 1; }
```

## 5. 操作与持久化语义

| 操作 | 确定行为 |
|---|---|
| UpsertServer / SetPreferences | 校验/持久化后返回；地址变更取消受影响连接并重新协商，验证固定的 daemon_id；不得沿用旧项目映射连接到不同身份 |
| SetServerCredential | 独立 0600 凭据存储；替换后取消旧凭据相关流并重新验证，响应只有 credential_present/revision |
| RemoveServer | 有映射、转发记录或活动终端时 FailedPrecondition；用户先停止/移除关联，不级联删文件 |
| PutMapping | 规范化本地根并验证远端项目；ID 与 server/project/local_root 创建后不可变，修改需新建映射；enabled 控制后台同步 |
| Pause/Resume | CLI 通过 PutMapping 改 sync_enabled 并携带 expected_revision；宿主调用 SyncWorker.ConfigureMapping，resume 先对账 |
| RemoveMapping | 必须 paused；有未处理状态时默认拒绝，只有显式 archive_pending 才归档基线/冲突后移除；停止 worker，不删两端文件/冲突副本 |
| CreateForward | 校验 endpoint/权限；保存期望配置后调用 PortWorker，绑定失败保留 error 状态与 ID供诊断，不显示 listening |
| StopForward | 先持久化 disabled，再取消连接/监听并等退出，最后移除该转发配置并保留请求去重结果；重启途中也不会恢复该监听 |
| ManagePlugin | LOCAL 调 agent manager；REMOTE 调选定 daemon 的 ManagePlugin 并要求 plugin.manage；禁用先停止任务，启用重新对账 |
| ResolveConflict | 必须持有最新 generation；agent 将请求交 SyncWorker，过期决定返回 Aborted；文件写入结果未知先对账 |
| Shutdown | 只有 instance 匹配才接受；响应 stopping 后在默认 10 秒内关闭任务/连接并退出 0；不删状态、不结束远端 tmux |
| BeginLogin | 同一节点最多一个登录 attempt；相同 request_id 幂等，其他 attempt 返回 AlreadyExists；无网络时保留状态，不清节点身份 |
| CancelLogin | 取消指定登录等待并失效登录链接，不退出已 online 的节点、不撤销其他设备 |

有状态的本地变更 request_id 按 OS 用户、方法和请求内容去重，结果与配置事务持久化；相同 ID 不同内容返回 InvalidArgument。密码/Token/auth key 不进入通用请求日志或明文去重记录；登录 auth_key 仅在内存内完成当前 attempt，进程重启后原 attempt 标为中断，用户重启登录。

已有 mapping/终端/forward 关联时不允许替换 ServerConfig.daemon_id；先明确移除关联，再重新登记目标。首次目标身份由受信 tsnet/TLS 连接和应用认证确认并固定，不能仅按显示名认领另一台 VPS。

配置持久化与 worker I/O 不能组成单个跨进程事务：成功保存期望状态后响应表示“已接受”，SyncState/ForwardState 表示实际结果。失败保留 error/reason 并允许修正；暂停/停止的响应只有在运行任务确实退出后才返回。不得让 UI 把 sync_enabled=true 当成“已经同步完成”。

## 6. 事件、背压与重连

WatchEvents 首帧发送 rescan_required，随后发送有序状态提示；客户端先订阅，再 List/Get 当前快照，并处理排队提示重新读取受影响资源。sequence 仅在本实例内递增，没有跨重启重放语义；instance 变化丢弃旧序号和缓存。

队列默认最多 256 个提示；溢出结束为 ResourceExhausted，前端重订阅并重查。终端另用 TerminalStream，文件预览另用 ReadConflictFile；二者不进入状态事件队列。普通消息上限 1 MiB、文件块 256 KiB；列表超过 1 MiB 返回 ResourceExhausted，首版按该有界容量验收，扩容需版本化分页。错误详情只暴露可诊断的 reason_code，不暴露 Token/登录 key。

短 RPC 默认 5 秒，启动/停止调用 15 秒，登录完成是异步状态，不用一个无限时长 unary 等浏览器。数据流继承前端/worker 可取消 context；任一代理断流取消下游，禁止遗留第二个 writer。重连只恢复订阅与终端当前屏幕，不重放输入或旧 TCP 字节。

明确错误：AGENT_RESTARTED/HANDSHAKE_REQUIRED/PROTOCOL_INCOMPATIBLE 用 FailedPrecondition；scope 越界用 PermissionDenied；revision/generation 竞争用 Aborted；节点未认证用 FailedPrecondition/NODE_AUTH_REQUIRED；远端 Token 无效保留 Unauthenticated。

## 7. CLI 到 RPC 映射

| CLI | 路径 |
|---|---|
| agent start | 用户服务管理器/单实例启动，不走 RPC |
| agent status/stop | Handshake → GetStatus/Shutdown |
| connect / servers add/update | UpsertServer + write-only SetServerCredential；必要时 NodeControl 登录 |
| servers switch | Get/SetPreferences，显式服务器参数仍优先 |
| sessions / attach / @ / snapshot | 带 server header 的 ShikiDaemon 白名单代理 |
| init --template | 前端加载模板并调用项目/会话代理；不绕过 agent |
| sync status/pause/resume/resolve | GetSyncState / PutMapping / ResolveConflict |
| ports list/forward/stop | ListForwards / CreateForward / StopForward |
| plugins list/enable/disable/status/logs/restart | Agent 插件接口，明确 LOCAL/REMOTE |
| config get/set/edit | 仅上述有类型的配置接口；编辑后校验 revision；凭据永不通过读接口导出 |
| doctor / stats / dashboard | Agent/NodeControl 状态 + 远端 SystemInfo/Stats 代理 + WatchEvents |

## 8. Phase 0 原型验收

- 编译本文件 agent.proto 与 09 三个 proto，生成所有 service stub；验证 imports、流方向、包路径。
- 在真实 Unix socket 上验证同 UID 成功、其他 UID 拒绝、stale socket/双启动无误删、旧 instance 请求拒绝。
- 验证明确 server 路由、Token 不回显、不支持的代理 RPC 被拒绝；两个前端流隔离，Release 不停止后台任务。
- 以最小桩 worker 验证取消、回调无锁死、事件溢出重查和 agent crash 后重建；之后再接真实文件系统/tsnet。
- 验证 major/minor/capability 不兼容失败、NodeControl 未登录仍可读状态，以及 [11](./11-automated-installation.md) 的 tsnet 引导原型。

通过 schema 编译不等于通过这些运行场景；故障恢复顺序见 [06](./06-edge-cases.md)。
