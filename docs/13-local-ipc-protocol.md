# 本地 agent IPC 协议

本文定义 CLI/TUI/后续 GUI 与 shiki-agent 的进程边界及 `proto/agent.proto`。握手、代理、事件和本地资源 API 已有实现与本地测试，逐项验收和剩余门槛见 [08](./08-roadmap.md)/[15](./15-implementation-status.md)；远端与插件消息复用 [09](./09-protocol-definition.md)，不另造一套 Session/FileInfo。协议定义不替代实际业务和部署验收。

## 1. 传输、身份与启动

- 使用Connect 协议/protobuf over HTTP/2 prior knowledge（h2c）over Unix domain socket；默认 `~/.shiki/run/agent.sock`。目录 0700、socket 0600，Unix socket 路径长度在启动前检查。
- 公共 socket 名必须在 0600 权限和所有者确认后原子发布，不能让并发前端观察到 bind 后 chmod 前的中间权限。当前 Darwin/Linux 在同一私有目录以短随机名 bind，设定权限后用不覆盖目标的 hard link 发布，随后移除临时名；不修改进程全局 umask，发布/临时名清理失败即报错。Close 在宿主生命周期锁范围内检查 inode，仅移除本实例仍拥有的公共名字；不是抵御任意同 UID rename 的通用原子 CAS。
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

此外注册 `NodeControl`，用于内嵌 tsnet 的本地引导。daemon 在自己的 `/var/lib/shiki/run/admin.sock` 上注册 NodeControl 与独立只读 DaemonReadiness，安装器以 daemon 用户访问；不在该 socket 暴露 agent 业务或远端文件 RPC。NodeControl 独立协商，登录尚未完成也必须可调用。GetStatus 属于基础只读诊断，不要求 node.login.v1；关闭节点或身份状态不可用的端点仍以兼容协议与自己的 instance_id 提供实际状态。BeginLogin/CancelLogin 及需要登录的连接流程继续要求 node.login.v1，不能因诊断可读而开放登录。前端读取状态仍须校验同 UID socket、协议 major/minor、实例与截止，不能根据当前磁盘配置猜运行中节点状态。DaemonReadiness 不增加 NodeControl 方法，不注册在远端 listener 或 agent 代理。

前端先调用 Agent.Handshake，发送协议范围、所需能力和设备显示名称。响应包括协议交集、instance_id、frontend_id。其后 Agent 业务调用和代理调用必须携带：

| HTTP 请求头 | 规则 |
|---|---|
| `shiki-agent-instance-id` | 必须匹配当前进程，旧值返回 FailedPrecondition/AGENT_RESTARTED |
| `shiki-frontend-id` | Handshake 分配；用于窗口归属，未登记返回 FailedPrecondition/HANDSHAKE_REQUIRED |
| `shiki-server-id` | 仅 ShikiDaemon 代理必填；空、未知 ID 拒绝，不读取全局“当前服务器” |

Agent 业务操作的 server_id 来自请求自身；若还携带路由 header，必须与请求一致。NodeControl.Negotiate/GetStatus/BeginLogin/CancelLogin 使用独立 `shiki-node-control-instance-id`（Negotiate 返回），不依赖 agent frontend_id。所有字段都不能替代 OS peer 身份检查。

前端退出调用 ReleaseFrontend，关闭该 frontend 的终端流/订阅；后台 sync/listener 继续。崩溃时以流取消清理 attachment；不持有活动流、10 分钟无调用的 frontend 登记可回收，客户端重新 Handshake。

### daemon 本地安装就绪诊断

`DaemonReadiness.Negotiate/GetStatus` 只在 daemon 的私有 admin.sock 上工作。
每次调用校验同 UID、拒绝 Authorization；独立协商 `daemon.readiness.v1`，
后续调用必须携带唯一 `shiki-daemon-readiness-instance-id`。Negotiate 返回
当前 build、daemon_id 与每次进程启动生成的 instance_id；安装器验证当前选中
build，并校验 GetStatus 的 instance_id/daemon_id 与协商一致。此服务不提供
Token、登录 URL、会话、项目、文件或插件管理操作，也不扩大任何代理白名单。

GetStatus 总耗时有界，读取真实宿主/listener 状态，对已启用插件通过当前实例
执行有界 Health 调用，再复核当前状态；manifest 存在不代表启动成功。返回当前
能力、插件状态与实际探测的 Claude 版本。Claude 登录以当前运行用户的 HOME/PATH
执行固定 `auth status --json`，限制 3 秒和 4 KiB 输出，仅接受严格布尔 loggedIn，
丢弃其他字段和原始 stderr；成功退出返回 reported_logged_in/reported_logged_out，
已知 exit 1 加严格 false 也表示 reported_logged_out；其他失败、超时、
不支持或非规范响应保持 unknown。缺依赖、登录未报告成功或启用插件未 ready 返回 degraded。
`local_application_ready` 仅证明这些本地条件；CLI 报告登录不证明远端账号端到端
可用，`external_peer_acceptance=pending` 仍独立保留。
诊断不创建会话、不改业务文件、不初始化身份、不发行 Token，不允许把这个状态
重命名为整套部署已验收。协议新增运行态消息，不修改持久配置或认证状态 schema。

## 3. 代理白名单与终端

允许代理：项目 CreateProject/GetProject/ListProjects/DeleteProject/ListProjectDirectories；会话 ListProjectSessions/ListSessions/Create/Get/Update/Delete/TakeControl/StopClaude/RestartClaude/GetSessionScreen/GetSessionLogs；TerminalStream；四个配置快照 RPC；ListPorts/WatchPorts；GetSystemInfo/GetClaudeVersion/HealthCheck/GetStats/WatchStats。

不向前端开放代理 Read/Write/Delete/List/Stat/WatchFiles、ForwardPort、远端插件管理或 NegotiateProtocol。前端用本地冲突/转发/插件接口；agent 自行完成 daemon 协商，数据面由插件 CoreAPI 访问。未列方法返回 PermissionDenied。这个白名单是路由分工，同 UID 的前端本身不是沙箱。

`ListProjectDirectories` 仅为项目内工作目录选择提供受根限制、有界分页的目录名，要求 `file.read` 和 `project.directories.v1`；不是同步文件清单接口，不读取文件内容、不允许符号链接遍历。`GetSessionLogs` 要求 `session.attach` 和 `terminal.logs.v1`，仅返回已验证 pane 尚保留的有界纯文本尾部、截断/可用状态和已知退出码；不是持久日志或终端字节回放。消息、限制及行为详见09；两者均不扩大插件CoreAPI文件能力或开放任意tmux命令。

agent 剥离本地 header，不接受前端提供的远端 Authorization；使用该 server_id 关联的凭据。deadline/cancel 传递到子调用，业务 Connect 错误码经显式重建保留（v2 不能直接返回远端 error，见 14）；增加已脱敏的故障所在层，不无限重试非幂等请求。

TerminalStream 首帧/输出完全使用 09 的 TerminalInput/Output。agent 记录 `(instance_id, frontend_id, server_id, session_id, attachment_id)`，不与其他窗口复用同一远端 attachment。TakeControl 的目标只能属于调用前端，跨设备竞争由 daemon 的 control_revision 处理。

前端或 agent 流断开取消远端流，tmux 保留；重连要重新握手、重新打开终端并处理 RESET，绝不自动重发排队输入。原始 bytes 不经过 UTF-8 字符串往返。

## 4. proto/agent.proto

`ServerRecord.connection_path` 仅为运行态诊断：认证后的 TLS 路径为 `direct-tls`，tsnet 路径仅在 SDK 提供证据时为 `tailscale-direct` 或 `tailscale-relay`，其余为 `unknown`。它不写入服务器配置/资源快照，不携带凭据，也不表示已测得 RTT。连接选择、冷却和重连上限遵循 [02](./02-connection-model.md)；耗尽以 `RECONNECT_EXHAUSTED` 报告，后台定时器不能自动重置次数。

```protobuf
syntax = "proto3";
package shiki.agent.v1;
option go_package = "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent";
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
  rpc GetStartupPolicy(shiki.v1.Empty) returns (StartupPolicy);
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
message StartupPolicy {
  string cli_auto_start = 1; // enabled/disabled/unknown; agent启动时加载的CLI按需配置，不是当前磁盘或登录启动
  string manager = 2; // launchd/systemd-user/unknown
  string managed_service = 3; // present/absent/unknown; 仅本产品标准受管服务
  string login_start = 4; // enabled/disabled/unknown
  string boot_start = 5; // enabled/disabled/unknown/not_applicable
  string reason_code = 6; // 有限诊断，不返回服务命令、环境或原始输出
  int64 observed_at = 7; // Unix ms；配置观察时间，不是实际重启验收
  string config_path = 8; // agent启动时加载的配置来源；无文件来源时为空
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
  string connection_path = 8; // 仅运行态：tailscale-direct/tailscale-relay/direct-tls/unknown
  repeated string connection_attempts = 9; // 最近一次路由选择实际factory顺序：tsnet/direct-tls，最多2；仅运行态，空表示未报告
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

// 同 UID、只读 daemon 安装诊断；仅注册在 daemon admin.sock。
service DaemonReadiness {
  rpc Negotiate(shiki.v1.ProtocolOffer) returns (ReadinessNegotiation);
  rpc GetStatus(shiki.v1.Empty) returns (DaemonReadinessStatus);
}
message ReadinessNegotiation {
  shiki.v1.ProtocolAgreement protocol = 1;
  string instance_id = 2;
}
message DaemonReadinessStatus {
  string instance_id = 1;
  string state = 2; // local_application_ready/degraded/stopping
  string reason_code = 3;
  shiki.v1.SystemInfo system = 4;
  shiki.v1.HealthStatus health = 5;
  repeated shiki.v1.PluginStatus plugins = 6;
  bool remote_listener_ready = 7;
  shiki.v1.ClaudeVersion claude_version = 8;
  string claude_login_state = 9; // reported_logged_in/reported_logged_out/unknown；仅 CLI 报告
  string external_peer_acceptance = 10; // pending；本地诊断不证明外部 peer 可达
}
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

`offline/NODE_CONTROL_UNAVAILABLE` 表示内嵌SDK尚不能确认当前控制面map连接，不等于凭据被撤销。仅缓存的Running/地址不能建立新的授权生命周期；先前已确认的有效peer通道按06继续工作，daemon与agent不得仅因本地诊断状态离线关闭它们。普通connect引导在此状态继续实际连接/认证探测，不自动发起重新登录；用户显式`node login`才开始新的认证尝试。新attempt必须有本次注册完成证据，旧Running不得提前完成；取消与认证完成在同一状态锁下裁决，已完成online不得被随后到达的取消覆盖。CLI对具有非空login_id和未来expires的`needs_login/NODE_AUTH_REQUIRED`、`offline/NODE_CONTROL_UNAVAILABLE`、`denied/NODE_APPROVAL_REQUIRED`、`expired/NODE_KEY_EXPIRED`或`uninitialized`且reason为空的状态，继续在原截止内等待当前attempt的URL/online；不重新BeginLogin、不自动Cancel、不延长父context或CLI截止。等待仅适配SDK启动、审批与重新认证的中间状态，待审批仍为denied、到期仍为expired且不得放行业务；`INVALID_AUTH_KEY`等明确拒绝不进入此等待。

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
| sessions / sessions snapshot / attach / @ | 带 server header 的 ShikiDaemon 白名单代理 |
| init --template | 前端加载模板并调用项目/会话代理；不绕过 agent |
| sync status/pause/resume/resolve | GetSyncState / PutMapping / ResolveConflict |
| ports list/forward/stop | ListForwards / CreateForward / StopForward |
| plugins list/enable/disable/status/config/logs/restart | Agent 插件接口，明确 LOCAL/REMOTE |
| config get/set/edit | 仅上述有类型的配置接口；编辑后校验 revision；凭据永不通过读接口导出 |
| doctor / stats / dashboard | Agent/NodeControl 状态 + 远端 SystemInfo/Stats 代理 + WatchEvents |

## 8. Phase 0 原型验收

- 编译本文件 agent.proto 与 09 三个 proto，生成所有 service stub；验证 imports、流方向、包路径。
- 在真实 Unix socket 上验证同 UID 成功、其他 UID 拒绝、stale socket/双启动无误删、旧 instance 请求拒绝。
- 验证明确 server 路由、Token 不回显、不支持的代理 RPC 被拒绝；两个前端流隔离，Release 不停止后台任务。
- 以最小桩 worker 验证取消、回调无锁死、事件溢出重查和 agent crash 后重建；之后再接真实文件系统/tsnet。
- 验证 major/minor/capability 不兼容失败、NodeControl 未登录仍可读状态，以及 [11](./11-automated-installation.md) 的 tsnet 引导原型。

通过 schema 编译不等于通过这些运行场景；故障恢复顺序见 [06](./06-edge-cases.md)。

Dashboard 的当前屏幕预览通过显式白名单 `GetSessionScreen` 代理，消息和权限见 09。该接口只读当前屏幕，沿用终端查看权限，不允许任意 tmux 命令、滚屏历史或输入注入。

ServerRecord.remote_protocol 中远端已认证协商返回的 daemon_id 与 granted_permissions 是运行期元数据；不把它们写入资源配置或当作前端可修改的授权。Agent 用协商中的 daemon_id 固定/复核目标身份；旧端无该字段时保留同连接 GetSystemInfo 的明确验证回退，不能因缺少 system.info 而对新端拒绝本来授权的 API。前端使用展开后的确切权限隐藏未授权变更/接管；旧端权限未知时仅保留可由服务端授权的只读观察请求，不推定写权限。服务端始终逐方法和逐流授权。普通业务 PermissionDenied 不表示连接失效，不取消其他已授权流。

## 最近一次路由选择诊断

`ServerRecord.connection_attempts = 9` 是 additive 运行态字段，不增加旧客户端连接必需能力。agent 仅在实际调用 route factory 前记录 `tsnet` 或 `direct-tls`，保留执行顺序且最多两项；失败策略、成功策略和取消前已经执行的策略均可出现。P2P/DERP 由 tsnet 自行选择，不能据此细分或声称实际拨号数量。无凭据读取成功前不记录策略，未尝试或旧端缺字段为空。

列表表示当前服务器代际最近一次发布的路由选择，不是完整重试历史或当前实际路径。代际验证失败的迟到结果丢弃；返回值使用独立切片，前端不可修改宿主观测。服务器持久记录及 request 去重结果剔除运行字段，持久校验拒绝这类运行元数据。CLI doctor/servers 与 TUI 诊断仅接受有限有序词汇，旧端空值或异常内容显示“未报告”，不从配置地址或 connection_path 补造尝试列表。当前路径和有界重连/信任拒绝规则保持独立，详见 02/06。

## 只读启动策略

`GetStartupPolicy` 是 additive 的本地 agent RPC，能力 `agent.startup.v1` 须在 Handshake 双方协商；绑定的 frontend 未取得该能力时拒绝。沿用同 UID、instance/frontend 生命周期、取消与无远端 server scope 的准入，不注册到 daemon 代理。旧 agent 未协商此能力时前端直接报告不支持，不盲调新 RPC；新增字段不改变 Preferences 可编辑 JSON。

`shiki config startup` 仅连接已经运行的 agent。`cli_auto_start` 与 `config_path` 来自该 agent 启动时加载的配置快照；它不是外部改写后的磁盘当前值。无文件来源时不能确认与服务绑定。服务状态实时只读观察，属于运行态，不写配置、receipt 或状态库。仅标准 Shiki 受管服务属于查询范围；absent 不排除自定义 OS 自动化，enabled 仅表示已确认的配置策略，不能当作实际重启/登录验证。systemd-user 的持久启用和 Linger 分开判断；LaunchAgent 的 boot 为 not_applicable。未知/drop-in/绑定不明/读取失败维持 unknown。具体来源与限制见 11。每个 agent 共享一个只读查询 permit，身份/能力检查先于准入；并发请求立即 ResourceExhausted/STARTUP_OBSERVATION_BUSY，不排队、不缓存，取消和所有终态释放。

诊断 reason_code 为有限集合：STARTUP_CONFIGURATION_UNAVAILABLE、STARTUP_MANAGER_UNAVAILABLE、STARTUP_MANAGER_OUTPUT_UNKNOWN、STARTUP_SERVICE_CONFIGURATION_UNKNOWN、STARTUP_MANAGED_SERVICE_ABSENT、STARTUP_MANAGED_SERVICE_DISABLED、STARTUP_LINGER_UNKNOWN、STARTUP_CONFIGURED_POLICY_OBSERVED、STARTUP_OBSERVATION_TIMEOUT。不会转发原始服务命令、环境或错误输出；调用者取消/截止仍作为 RPC 错误返回。
