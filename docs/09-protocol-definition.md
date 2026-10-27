# Protocol 定义

本文档是远端 gRPC 接口草案的唯一来源，尚未生成源码或实现服务。下列三个 protobuf 块分别对应拟建的 shiki.proto、types.proto、plugin.proto；Go 示例只描述调用流程。

## 协议行为

- 单用户、多设备；user 字段仅展示或过滤同一配置用户，不能用于身份切换。所有 RPC（含 stream）必须认证授权。
- 项目、会话 ID 不可变；时间为 UTC Unix 秒，CPU 使用率以一个逻辑核为 100%（可以大于 100），内存/文件大小为 bytes。
- CreateProject/CreateSession/RestoreSnapshot 的 request_id 由调用者生成；同请求幂等，参数不同返回 InvalidArgument，去重结果随资源状态持久化。
- TerminalStream 首帧且仅首帧为 open，服务端回复 READY 后开始数据；一次流绑定一个会话和 attachment。只有控制者可发送 data/resize/control。RESET 后重绘当前屏幕；首版无 resume_from 或历史重放。
- 接管先检查新控制者资格后原子更新，旧控制者收到 CONTROL_CHANGED。流取消即 detach，不需要第二条无绑定关系的 AttachSession/DetachSession RPC。
- DeleteSession/DeleteProject 不删除项目文件；Project 有会话或配置快照时返回 FailedPrecondition。UpdateSession 使用 FieldMask 区分“不修改”和“清空”。
- 文件路径为 project_id 范围内的相对路径，使用 `/`，根目录清单/watch 使用空 path。FileEvent 是重扫提示，不是可靠变更日志。
- 文件块 ≤256 KiB，消息上限 ≥1 MiB；自动同步默认文件上限 10 MiB。WriteFile 仅首帧含 header，后续仅 chunk，CloseSend 表示上传完成，校验大小/hash/revision 后返回 FileInfo；流取消不提交。
- revision 是不透明内容版本；空 base_revision 表示预期不存在；DeleteFile 必须匹配现存 revision。外部文件写入竞态限制及版本备份见 10。ListFiles 返回分页清单，扫描期间变化需对账复核，不承诺事务快照。
- ReadFile 的第一帧为 info，随后 chunk；下载必须检查 hash/revision，变化或读错返回错误。客户端接收完成前不替换本地文件。
- ForwardPort 一流一 TCP 连接，CONNECT → CONNECTED → DATA/HALF_CLOSE → CLOSE；目标固定、loopback allowlist 校验，不支持 UDP。connection_id 在流内唯一并保持不变。
- 配置快照只保存会话配置，不保存文件/环境密钥/Claude 对话；恢复使用新的 request_id 创建新会话。
- 稳定错误语义：InvalidArgument 参数错误，NotFound 资源不存在，AlreadyExists 名称冲突，FailedPrecondition 插件未就绪/状态不符，Aborted 版本冲突，ResourceExhausted 配额或队列限制，Unavailable 暂时不可达。
- 已发布字段号不得复用；删除字段使用 reserved；版本/能力由 GetSystemInfo 返回。当前尚未发布，不表示需要兼容错误的早期草稿。

本地 agent IPC、插件管理 RPC、资源统计订阅及完整插件数据面仍需独立 schema；见文末缺口列表，不能由 UX 命令存在推断协议齐全。

## proto/shiki.proto - 核心服务

### ShikiDaemon 服务

```protobuf
syntax = "proto3";
package shiki.v1;

option go_package = "github.com/yourusername/shikigami/pkg/protocol";

import "types.proto";

// ShikiDaemon 是核心服务接口
service ShikiDaemon {
  // ============================================
  // 项目管理
  // ============================================

  // 列出所有项目
  rpc ListProjects(ListProjectsRequest) returns (ListProjectsResponse);

  // 创建新项目
  rpc CreateProject(CreateProjectRequest) returns (Project);

  // 获取项目信息
  rpc GetProject(GetProjectRequest) returns (Project);

  // 删除项目
  rpc DeleteProject(DeleteProjectRequest) returns (Empty);

  // ============================================
  // 会话管理
  // ============================================

  // 列出项目的所有会话
  rpc ListProjectSessions(ListProjectSessionsRequest) returns (ListProjectSessionsResponse);

  // 列出所有会话（跨项目）
  rpc ListSessions(ListSessionsRequest) returns (ListSessionsResponse);

  // 创建新会话
  rpc CreateSession(CreateSessionRequest) returns (Session);

  // 获取会话信息
  rpc GetSession(GetSessionRequest) returns (Session);

  // 更新会话元数据
  rpc UpdateSession(UpdateSessionRequest) returns (Session);

  // 删除会话
  rpc DeleteSession(DeleteSessionRequest) returns (Empty);

  // 原子转移终端控制权
  rpc TakeControl(TakeControlRequest) returns (Empty);
  rpc StopClaude(SessionActionRequest) returns (Session);
  rpc RestartClaude(SessionActionRequest) returns (Session);

  // ============================================
  // 终端 I/O（双向流）
  // ============================================

  // 终端数据流（双向）
  rpc TerminalStream(stream TerminalInput) returns (stream TerminalOutput);

  // ============================================
  // 文件操作
  // ============================================

  // 读取文件
  rpc ReadFile(ReadFileRequest) returns (stream FileData);

  // 写入文件
  rpc WriteFile(stream WriteFileRequest) returns (FileInfo);
  rpc ListFiles(ListFilesRequest) returns (ListFilesResponse);

  // 删除文件
  rpc DeleteFile(DeleteFileRequest) returns (Empty);

  // 监听文件变化（服务端流）
  rpc WatchFiles(WatchFilesRequest) returns (stream FileEvent);

  // 获取文件状态
  rpc StatFile(StatFileRequest) returns (FileInfo);

  // ============================================
  // 端口管理
  // ============================================

  // 列出开放的端口
  rpc ListPorts(ListPortsRequest) returns (ListPortsResponse);

  // 监听端口变化（服务端流）
  rpc WatchPorts(WatchPortsRequest) returns (stream PortEvent);

  // 转发端口数据（双向流）
  rpc ForwardPort(stream PortData) returns (stream PortData);

  // ============================================
  // 系统信息
  // ============================================

  // 获取系统信息
  rpc GetSystemInfo(Empty) returns (SystemInfo);

  // 获取 Claude Code 版本
  rpc GetClaudeVersion(Empty) returns (ClaudeVersion);

  // 健康检查
  rpc HealthCheck(Empty) returns (HealthStatus);

  // ============================================
  // 会话快照
  // ============================================

  // 创建会话快照
  rpc CreateSnapshot(CreateSnapshotRequest) returns (Snapshot);

  // 列出快照
  rpc ListSnapshots(ListSnapshotsRequest) returns (ListSnapshotsResponse);

  // 从快照恢复
  rpc RestoreSnapshot(RestoreSnapshotRequest) returns (Session);

  // 删除快照
  rpc DeleteSnapshot(DeleteSnapshotRequest) returns (Empty);
}
```

## proto/types.proto - 数据类型

### 基础消息类型

```protobuf
syntax = "proto3";
package shiki.v1;

option go_package = "github.com/yourusername/shikigami/pkg/protocol";

import "google/protobuf/field_mask.proto";

// Empty 表示空消息
message Empty {}

// ============================================
// 项目相关
// ============================================

message Project {
  string id = 1;                    // 项目 ID (proj_abc123)
  string name = 2;                  // 项目名称
  string path = 3;                  // 远端路径
  string user = 4;                  // 所属用户
  int64 created_at = 5;             // 创建时间（Unix 时间戳）
  repeated string session_ids = 6;  // 会话 ID 列表
  map<string, string> metadata = 7; // 元数据
}

message ListProjectsRequest {
  string user = 1;                  // 可选：按用户过滤
}

message ListProjectsResponse {
  repeated Project projects = 1;
}

message CreateProjectRequest {
  string name = 1;                  // 项目名称
  string path = 2;                  // 远端路径
  map<string, string> metadata = 3;
  string request_id = 4;
}

message GetProjectRequest {
  string id = 1;                    // 项目 ID
}

message DeleteProjectRequest {
  string id = 1;                    // 项目 ID
  reserved 2;
  reserved "delete_files";            // 删除管理记录不删除项目文件
}

// ============================================
// 会话相关
// ============================================

message Session {
  // 基本信息
  string id = 1;                    // 会话 ID (sess_main_001)
  string project_id = 2;            // 所属项目 ID
  string name = 3;                  // 会话名称 (main, frontend, backend)
  string working_dir = 4;           // 工作目录

  // 时间信息
  int64 created_at = 5;             // 创建时间
  int64 last_active = 6;            // 最后活跃时间

  // tmux 信息
  string tmux_session = 7;          // tmux 会话名称
  string tmux_window = 8;           // tmux 窗口
  string tmux_pane = 9;             // tmux 窗格

  // Claude Code 状态
  string claude_status = 10;        // starting, running, crashed, stopped, unknown
  int32 claude_pid = 11;            // Claude Code 进程 ID
  string claude_context = 12;       // 用户备注，不推断 Claude 内部状态

  // 连接信息
  int32 client_count = 13;          // 当前连接的客户端数量
  bool is_connected = 14;           // 派生值：client_count > 0

  // 元数据
  string description = 15;          // 描述
  repeated string tags = 16;        // 标签
  map<string, string> metadata = 17;

  // 资源使用
  double cpu_usage = 18;            // CPU 使用率
  int64 memory_usage = 19;          // 内存使用（字节）
  string lifecycle = 20;            // creating, ready, failed, deleting
  optional bool auto_start_claude = 21; // 期望启动配置
}

message ListProjectSessionsRequest {
  string project_id = 1;            // 项目 ID
}

message ListProjectSessionsResponse {
  repeated Session sessions = 1;
}

message ListSessionsRequest {
  string user = 1;                  // 可选：按用户过滤
  string status = 2;                // 可选：按状态过滤
  repeated string tags = 3;         // 可选：按标签过滤
}

message ListSessionsResponse {
  repeated Session sessions = 1;
}

message CreateSessionRequest {
  string project_id = 1;            // 必须：所属项目
  string name = 2;                  // 会话名称
  string working_dir = 3;           // 工作目录
  string description = 4;           // 描述
  repeated string tags = 5;         // 标签
  map<string, string> metadata = 6;
  optional bool auto_start_claude = 7; // 省略默认 true，显式 false 不启动
  string request_id = 8;
}

message GetSessionRequest {
  string id = 1;                    // 会话 ID
}

message UpdateSessionRequest {
  string id = 1;
  string description = 2;
  repeated string tags = 3;
  map<string, string> metadata = 4;
  google.protobuf.FieldMask update_mask = 5; // 仅允许 description/tags/metadata
}

message DeleteSessionRequest {
  string id = 1;                    // 会话 ID
  reserved 2;
  reserved "delete_files";            // 删除管理记录不删除项目文件
}

message TakeControlRequest {
  string session_id = 1;
  string attachment_id = 2; // 已连接、非只读且具备 write 权限的目标
}

message SessionActionRequest {
  string session_id = 1;
}

// ============================================
// 终端 I/O
// ============================================

message TerminalOpen {
  string session_id = 1;
  bool read_only = 2;
  ResizeEvent size = 3;
  string device_name = 4;
}

message TerminalInput {
  reserved 1;
  reserved "session_id";
  oneof payload {
    bytes data = 2;
    ResizeEvent resize = 3;
    ControlCommand control = 4;
    TerminalOpen open = 5;
  }
}

message TerminalOutput {
  string session_id = 1;
  oneof payload {
    bytes data = 2;
    SessionEvent event = 5;
  }
  uint64 sequence = 3; // 仅本次 attachment 内单调递增；不用于重放
  int64 timestamp = 4;
  string attachment_id = 6; // 服务端 READY 首帧分配
}

message ResizeEvent {
  uint32 rows = 1;                  // 行数
  uint32 cols = 2;                  // 列数
}

message ControlCommand {
  enum Type {
    INTERRUPT = 0;                  // Ctrl+C
    QUIT = 1;                       // Ctrl+\
    SUSPEND = 2;                    // Ctrl+Z
  }
  Type type = 1;
}

message SessionEvent {
  enum Type {
    CLIENT_CONNECTED = 0;           // 客户端连接
    CLIENT_DISCONNECTED = 1;        // 客户端断开
    CLAUDE_STARTED = 2;             // Claude Code 启动
    CLAUDE_STOPPED = 3;             // Claude Code 停止
    CLAUDE_CRASHED = 4;             // Claude Code 崩溃
    SESSION_DELETING = 5;           // 会话正在删除
    READY = 6;                      // data 中含 mode=controller|observer
    RESET = 7;                      // 清空终端状态，后续为当前屏幕重绘
    CONTROL_CHANGED = 8;            // data 中含 controller_attachment_id
  }
  Type type = 1;
  string message = 2;
  map<string, string> data = 3;
}

// ============================================
// 文件操作
// ============================================

message ReadFileRequest {
  string path = 1;
  string project_id = 2;
  string expected_revision = 3; // 必须匹配清单/stat 的版本
}

message FileData {
  oneof payload {
    bytes chunk = 1;
    FileInfo info = 2; // 仅首帧
  }
}

message WriteFileHeader {
  string project_id = 1;
  string path = 2;
  string base_revision = 3; // 空表示预期不存在
  string content_hash = 4;  // SHA-256 十六进制
  int64 size = 5;
  uint32 mode = 6;
}

message WriteFileRequest {
  oneof payload {
    WriteFileHeader header = 1;
    bytes chunk = 2;
  }
}

message DeleteFileRequest {
  string path = 1;
  string project_id = 2;
  string expected_revision = 3;
}

message ListFilesRequest {
  string project_id = 1;
  string path = 2;
  uint32 page_size = 3; // 服务端有最大值
  string page_token = 4;
}

message ListFilesResponse {
  repeated FileInfo files = 1;
  string next_page_token = 2;
}

message WatchFilesRequest {
  string path = 1;
  repeated string patterns = 2;
  repeated string ignore = 3;
  string project_id = 4;
}

message FileEvent {
  enum EventType {
    CREATED = 0;
    MODIFIED = 1;
    DELETED = 2;
    RENAMED = 3;
    RESCAN_REQUIRED = 4;
  }
  EventType type = 1;
  string path = 2;
  string old_path = 3;
  int64 timestamp = 4;
  int64 size = 5;
  string project_id = 6;
  string revision = 7;
}

message StatFileRequest {
  string path = 1;
  string project_id = 2;
}

message FileInfo {
  string name = 1;
  int64 size = 2;
  uint32 mode = 3;
  int64 mod_time = 4;
  bool is_dir = 5;
  string path = 6; // 项目相对路径
  string content_hash = 7;
  string revision = 8;
}

// ============================================
// 端口管理
// ============================================

message ListPortsRequest {
  string session_id = 1;            // 可选：按会话过滤
}

message ListPortsResponse {
  repeated OpenPort ports = 1;
}

message OpenPort {
  int32 port = 1;                   // 端口号
  string protocol = 2;              // 首版仅 tcp
  string service = 3;               // 服务类型（http, postgres, redis）
  string process_name = 4;          // 进程名称
  int32 pid = 5;                    // 进程 ID
  string session_id = 6;            // 所属会话；无法验证时为空
  string bind_address = 7;          // 被观测到的监听地址
}

message WatchPortsRequest {
  string session_id = 1;            // 可选：只监听特定会话
}

message PortEvent {
  enum EventType {
    OPENED = 0;                     // 端口开启
    CLOSED = 1;                     // 端口关闭
  }
  EventType type = 1;
  OpenPort port = 2;
}

message PortData {
  enum Type {
    CONNECT = 0;                    // 建立连接
    DATA = 1;                       // 数据传输
    CLOSE = 2;                      // 关闭连接
    CONNECTED = 3;                  // 目标连接已建立
    HALF_CLOSE = 4;                 // 本方向 EOF，另一方向仍可读
  }
  Type type = 1;
  int32 port = 2;                   // 端口号
  bytes data = 3;                   // 数据
  string connection_id = 4;         // 一条流对应一个 TCP 连接
  string remote_address = 5;        // CONNECT 时使用，必须是授权的本机地址
}

// ============================================
// 系统信息
// ============================================

message SystemInfo {
  string os = 1;                    // 操作系统
  string arch = 2;                  // 架构
  int32 cpu_cores = 3;              // CPU 核心数
  int64 total_memory = 4;           // 总内存（字节）
  int64 free_memory = 5;            // 可用内存
  int64 total_disk = 6;             // 总磁盘空间
  int64 free_disk = 7;              // 可用磁盘空间
  string daemon_version = 8;        // Daemon 版本
  int64 uptime = 9;                 // 运行时间（秒）
  string protocol_version = 10;
  repeated string capabilities = 11;
  string daemon_id = 12;
}

message ClaudeVersion {
  string version = 1;               // Claude Code 版本
  string path = 2;                  // Claude Code 路径
}

message HealthStatus {
  enum Status {
    HEALTHY = 0;                    // 健康
    DEGRADED = 1;                   // 降级
    UNHEALTHY = 2;                  // 不健康
  }
  Status status = 1;
  string message = 2;
  map<string, string> details = 3;
}

// ============================================
// 会话快照
// ============================================

message Snapshot {
  string id = 1;
  string session_id = 2;
  string session_name = 3;
  int64 created_at = 4;
  string description = 5; // 快照说明
  string working_dir = 6; // 项目相对目录，恢复时重新验证
  reserved 7, 8, 9;
  reserved "claude_context_path", "files", "environment";
  string project_id = 10;
  repeated string tags = 11;
  map<string, string> metadata = 12; // 仅允许的非敏感配置
  optional bool auto_start_claude = 13;
  uint32 schema_version = 14;
  string session_description = 15;
}

message CreateSnapshotRequest {
  string session_id = 1;
  string description = 2;
  reserved 3;
  reserved "include_files"; // 首版只做配置快照
}

message ListSnapshotsRequest {
  string project_id = 1;            // 可选：按项目过滤
  string session_id = 2;            // 可选：按会话过滤
}

message ListSnapshotsResponse {
  repeated Snapshot snapshots = 1;
}

message RestoreSnapshotRequest {
  string snapshot_id = 1;           // 快照 ID
  string new_session_name = 2;      // 新会话名称
  string request_id = 3;
}

message DeleteSnapshotRequest {
  string snapshot_id = 1;           // 快照 ID
}
```

## proto/plugin.proto - 插件接口

```protobuf
syntax = "proto3";
package shiki.plugin.v1;

option go_package = "github.com/yourusername/shikigami/pkg/protocol/plugin";

// Plugin 是所有插件必须实现的接口
service Plugin {
  // 获取插件信息
  rpc GetInfo(Empty) returns (PluginInfo);

  // 初始化插件
  rpc Init(InitRequest) returns (Empty);

  // 启动插件
  rpc Start(Empty) returns (Empty);

  // 停止插件
  rpc Stop(Empty) returns (Empty);

  // 健康检查
  rpc Health(Empty) returns (HealthStatus);
}

message Empty {}

message PluginInfo {
  string name = 1;                  // 插件名称
  string version = 2;               // 版本
  string description = 3;           // 描述
  string type = 4;                  // client, server, both
  string protocol_version = 5;
  repeated string capabilities = 6;
}

message InitRequest {
  map<string, string> config = 1;   // 配置
  CoreAPIEndpoint core_api = 2;     // 进程私有 CoreAPI 端点
  string side = 3;                  // client/server
  string protocol_version = 4;
}

message CoreAPIEndpoint {
  string address = 1;               // gRPC 地址
  string token = 2;                 // 本次插件启动的短期凭据，不是用户 Token
}

message HealthStatus {
  enum Status {
    HEALTHY = 0;
    DEGRADED = 1;
    UNHEALTHY = 2;
  }
  Status status = 1;
  string message = 2;
  map<string, string> details = 3;
}

// CoreAPI 是核心提供给插件的 API
service CoreAPI {
  // 获取显式 scope 的会话
  rpc GetSession(ScopeRequest) returns (Session);

  // 列出会话
  rpc ListSessions(ScopeRequest) returns (ListSessionsResponse);

  // 订阅事件
  rpc SubscribeEvents(SubscribeRequest) returns (stream Event);

  // 发送日志
  rpc Log(LogEntry) returns (Empty);

  // 获取配置
  rpc GetConfig(GetConfigRequest) returns (ConfigValue);

  // 设置配置
  rpc SetConfig(SetConfigRequest) returns (Empty);
}

message ScopeRequest {
  string server_id = 1;
  string project_id = 2;
  string session_id = 3;
  string mapping_id = 4;
}

message Session {
  string id = 1;
  string name = 2;
  string project_id = 3;
  string working_dir = 4;
  string local_path = 5;            // 本地路径（客户端）
  string remote_path = 6;           // 远端路径（服务端）
}

message ListSessionsResponse {
  repeated Session sessions = 1;
}

message SubscribeRequest {
  repeated string event_types = 1;  // 订阅的事件类型
  ScopeRequest scope = 2;
}

message Event {
  string type = 1;                  // 事件类型
  string source = 2;                // 事件源
  map<string, string> data = 3;     // 事件数据
  int64 timestamp = 4;              // 时间戳
  ScopeRequest scope = 5;
  string event_id = 6;
}

message LogEntry {
  enum Level {
    DEBUG = 0;
    INFO = 1;
    WARN = 2;
    ERROR = 3;
  }
  Level level = 1;
  string message = 2;
  map<string, string> fields = 3;
  int64 timestamp = 4;
}

message GetConfigRequest {
  string key = 1;
}

message ConfigValue {
  oneof value {
    string string_value = 1;
    int64 int_value = 2;
    bool bool_value = 3;
    double double_value = 4;
  }
}

message SetConfigRequest {
  string key = 1;
  ConfigValue value = 2;
}
```

## 调用流程示例

```text
前端 → agent：Attach(server_id, session_id)
agent → daemon：TerminalStream（受认证的可取消 context）
agent 首帧：TerminalInput.open(session_id, read_only, size)
daemon 首帧：READY(attachment_id, mode)
daemon：RESET → tmux 当前屏幕输出
agent：输入/resize → 一个 sender；接收输出 → 对应前端
前端关闭：取消该流 → daemon detach；保留远端会话
```

客户端通过统一连接管理器建立连接；不在使用示例中用普通网络的 insecure gRPC 发送 Token。生成的类型化 stream 提供 Send/Recv；通用 grpc.ClientStream/ServerStream 只有 SendMsg/RecvMsg，不能混用。

## 生成与验证

必须先安装并锁定 protoc、protoc-gen-go 和 protoc-gen-go-grpc 版本（不是只有后两者）。以下命令在三个文件及 go.mod 建立后使用；module 占位符需在实现初始化时统一替换。

```bash
protoc -I proto \
  --go_out=. --go_opt=module=github.com/yourusername/shikigami \
  --go-grpc_out=. --go-grpc_opt=module=github.com/yourusername/shikigami \
  proto/shiki.proto proto/types.proto proto/plugin.proto
```

这样生成 pkg/protocol 及 pkg/protocol/plugin，不能使用 paths=source_relative 把不同 Go package 的文件混写进 proto/。校验语法可用 protoc 的 descriptor 输出；完整 Go 编译仍需生成器和依赖。

## 实现前必须补齐的协议

| 协议 | 所需定义与交付门槛 |
|---|---|
| agent 本地 IPC | 本地身份验证、服务/映射选择、终端桥接、agent start/stop/status、同步冲突决策、监听状态和取消；Phase 0 定义，Phase 1 实现 |
| CoreAPI 代理与插件数据面 | 带 scope 的映射、Read/Write/Watch/ListFiles、ForwardPort、插件侧 server handler；本页生命周期不是完整插件 API，插件功能开发前必须编译并跨进程验证 |
| 插件管理 | 两端独立的 list/enable/disable/status/logs/restart；远端需 plugin.manage；Phase 2 交付 |
| 监控 | 会话/系统资源采样、订阅及 capability；GetSystemInfo 仅静态/当前信息，不能替代实时统计；Phase 3 交付 |

首版已列远端消息是可编译草案，以上缺口是明确的开发任务，不再宣称协议“覆盖所有功能”。只有能力被服务端/agent 宣告且相应插件就绪，UI 才启用功能。
