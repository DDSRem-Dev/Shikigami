# Protocol 定义

本文档是远端 ConnectRPC 与插件进程间接口草案的唯一来源（插件 CoreAPI 用 Connect；go-plugin 生命周期/worker 保留 gRPC），尚未生成源码或实现服务。下列三个 protobuf 块分别对应拟建的 shiki.proto、types.proto、plugin.proto；调用流程不代表已有可执行服务。

## 协议行为

- 单用户、多设备；user 字段仅展示或过滤同一配置用户，不能用于身份切换。所有 RPC（含 stream）必须认证授权。
- 项目、会话 ID 不可变；时间为 UTC Unix 秒，CPU 使用率以一个逻辑核为 100%（可以大于 100），内存/文件大小为 bytes。
- CreateProject/CreateSession/RestoreSnapshot 的 request_id 由调用者生成；同请求幂等，参数不同返回 InvalidArgument，去重结果随资源状态持久化。
- TerminalStream 首帧且仅首帧为 open，服务端回复 READY 后开始数据；一次流绑定一个会话和 attachment。只有控制者可发送 data/resize/control。RESET 后重绘当前屏幕；首版无 resume_from 或历史重放。
- 接管先检查新控制者资格与 expected_control_revision 后原子更新，竞争失败返回 Aborted；同一 request_id 在该 attachment 生命周期内去重，旧控制者收到 CONTROL_CHANGED。每次控制者更换或断开都产生新 control_revision，daemon 重启重置运行 epoch，旧 attachment 请求拒绝。流取消即 detach，不需要第二条无绑定关系的 AttachSession/DetachSession RPC。
- DeleteSession/DeleteProject 不删除项目文件；Project 有会话或配置快照时返回 FailedPrecondition。UpdateSession 使用 FieldMask 区分“不修改”和“清空”。
- 文件路径为 project_id 范围内的相对路径，使用 `/`，根目录清单/watch 使用空 path。FileEvent 是重扫提示，不是可靠变更日志。
- 文件块 ≤256 KiB，消息上限 ≥1 MiB；自动同步默认文件上限 10 MiB。WriteFile 仅首帧含 header，后续仅 chunk，CloseSend 表示上传完成，校验大小/hash/revision 后返回 FileInfo；流取消不提交。
- revision 是不透明内容版本；空 base_revision 表示预期不存在；DeleteFile 必须匹配现存 revision。外部文件写入竞态限制及版本备份见 10。ListFiles 返回分页清单，扫描期间变化需对账复核，不承诺事务快照。
- ReadFile 的第一帧为 info，随后 chunk；下载必须检查 hash/revision，变化或读错返回错误。客户端接收完成前不替换本地文件。
- ForwardPort 一流一 TCP 连接，CONNECT → CONNECTED → DATA/HALF_CLOSE → CLOSE；目标固定、loopback allowlist 校验，不支持 UDP。connection_id 在流内唯一并保持不变。
- 配置快照只保存会话配置，不保存文件/环境密钥/Claude 对话；恢复使用新的 request_id 创建新会话。
- 稳定错误语义：InvalidArgument 参数错误，NotFound 资源不存在，AlreadyExists 名称冲突，FailedPrecondition 插件未就绪/状态不符，Aborted 版本冲突，ResourceExhausted 配额或队列限制，Unavailable 暂时不可达。
- 已发布字段号不得复用；删除字段使用 reserved；版本/能力由 GetSystemInfo 返回。当前尚未发布，不表示需要兼容错误的早期草稿。

本地 agent IPC 的独立 schema 见 [13 本地 IPC 协议](./13-local-ipc-protocol.md)。本文补齐远端插件管理、资源订阅、CoreAPI 和两端 worker 接口；协议定义与实现交付仍是两件事。

## proto/shiki.proto - 核心服务

### ShikiDaemon 服务

```protobuf
syntax = "proto3";
package shiki.v1;

option go_package = "github.com/yourusername/shikigami/pkg/protocol";

import "types.proto";

// ShikiDaemon 是核心服务接口
service ShikiDaemon {
  // 经应用 Token 认证后协商，不能用构建版本字符串推断兼容性。
  rpc NegotiateProtocol(ProtocolOffer) returns (ProtocolAgreement);
  rpc ListPlugins(ListPluginsRequest) returns (ListPluginsResponse);
  rpc ManagePlugin(ManagePluginRequest) returns (PluginStatus);
  rpc WatchPluginLogs(PluginLogsRequest) returns (stream LogRecord);
  rpc GetStats(StatsRequest) returns (StatsSnapshot);
  rpc WatchStats(StatsRequest) returns (stream StatsSnapshot);

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
import "google/protobuf/struct.proto";

// Empty 表示空消息
message Empty {}

// 版本协商：相同 major、minor 区间交集，选择交集最大值。
message ProtocolOffer {
  uint32 major = 1;
  uint32 min_minor = 2;
  uint32 max_minor = 3;
  repeated string capabilities = 4;
  repeated string required_capabilities = 5;
}
message ProtocolAgreement {
  uint32 major = 1;
  uint32 minor = 2;
  repeated string capabilities = 3; // 双方可用能力的交集
  string build_version = 4;        // 仅用于诊断，不用于代替协议版本
}
message ListPluginsRequest {}
message ListPluginsResponse { repeated PluginStatus plugins = 1; }
message PluginStatus {
  string plugin_id = 1;
  string build_version = 2;
  bool enabled = 3;        // 已持久化的期望状态
  string state = 4;        // stopped/starting/ready/failed/incompatible
  string revision = 5;     // 配置和期望状态 revision
  string reason_code = 6;
  ProtocolAgreement protocol = 7;
}
message ManagePluginRequest {
  enum Action {
    ACTION_UNSPECIFIED = 0;
    ENABLE = 1;
    DISABLE = 2;
    RESTART = 3;
    SET_CONFIG = 4;
  }
  string plugin_id = 1;
  Action action = 2;
  google.protobuf.Struct config = 3; // 仅 SET_CONFIG，按插件 schema 验证
  string expected_revision = 4;
  string request_id = 5; // 包括 restart 的去重，不能重复重启
}
message PluginLogsRequest {
  string plugin_id = 1;
  uint32 tail = 2; // 0=仅后续，最大 1000 行
  bool follow = 3;
}
message LogRecord {
  int64 timestamp = 1;
  string level = 2;
  string message = 3; // 宿主脱敏后的有界内容
  map<string, string> fields = 4;
}
message StatsRequest {
  string session_id = 1; // 空=系统与全部有权查看的会话
  uint32 interval_seconds = 2; // WatchStats 默认 5，允许 1–60
}
message SessionStats {
  string session_id = 1;
  optional double cpu_percent = 2;
  optional int64 memory_bytes = 3;
}
message StatsSnapshot {
  int64 timestamp = 1;
  optional double cpu_percent = 2; // 一个逻辑核为 100%
  optional int64 memory_used_bytes = 3;
  optional int64 disk_free_bytes = 4;
  repeated SessionStats sessions = 5;
}


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
  string control_revision = 22;     // 当前控制权版本
  string controller_attachment_id = 23; // 无控制者时为空
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
  string expected_control_revision = 3; // READY/CONTROL_CHANGED 返回的水位
  string request_id = 4;
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
    READY = 6;                      // data 含 mode/controller_attachment_id/control_revision
    RESET = 7;                      // 清空终端状态，后续为当前屏幕重绘
    CONTROL_CHANGED = 8;            // data 含 controller_attachment_id/control_revision
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

此 schema 覆盖内置 sync/port 插件的宿主回调、客户端 worker 和服务端数据面。`CoreAPI` 由 agent/daemon 提供，其他五个 service 由插件按 side 提供。第三方插件的新能力需另行版本化，不能复用任意字节的通用远程执行入口。

```protobuf
syntax = "proto3";
package shiki.plugin.v1;
option go_package = "github.com/yourusername/shikigami/pkg/protocol/plugin";
import "types.proto";
import "google/protobuf/struct.proto";

service Plugin {
  rpc GetInfo(shiki.v1.Empty) returns (PluginInfo);
  rpc Init(InitRequest) returns (shiki.v1.Empty);
  rpc Start(shiki.v1.Empty) returns (shiki.v1.Empty);
  rpc Stop(shiki.v1.Empty) returns (shiki.v1.Empty);
  rpc Health(shiki.v1.Empty) returns (shiki.v1.HealthStatus);
}
message PluginInfo {
  string name = 1;
  string version = 2;
  string description = 3;
  string side = 4; // client/server/both
  shiki.v1.ProtocolOffer protocol = 5;
}
message CoreAPIEndpoint {
  string address = 1; // 宿主独立 CoreAPI Unix socket 的绝对路径；Connect/h2c，非 URL/broker
  string token = 2;   // 当前进程实例的短期能力凭据
}
message InitRequest {
  google.protobuf.Struct config = 1;
  CoreAPIEndpoint core_api = 2;
  string side = 3;
  shiki.v1.ProtocolAgreement protocol = 4;
  string plugin_instance_id = 5;
  string host_instance_id = 6;
  string state_dir = 7; // 本实例独占的私有任务状态目录
}
message Scope {
  string server_id = 1; // agent 的服务器记录 ID；不会跨网络传给 daemon
  string project_id = 2;
  string session_id = 3;
  string mapping_id = 4;
}
message Mapping {
  string id = 1;
  string server_id = 2;
  string project_id = 3;
  string local_root = 4; // 宿主规范化；插件不能自行改根
  string revision = 5;
  bool sync_enabled = 6;
  string default_session = 7;
  map<string, string> session_hints = 8;
}
message MappingList { repeated Mapping mappings = 1; }
message ConfigRequest { Scope scope = 1; }
message PluginConfig {
  google.protobuf.Struct values = 1;
  string revision = 2;
}
message SetConfigRequest {
  Scope scope = 1;
  google.protobuf.Struct values = 2;
  string expected_revision = 3;
  string request_id = 4;
}
message SubscribeRequest {
  Scope scope = 1;
  repeated string event_types = 2;
}
message Event {
  string event_id = 1;
  string type = 2;
  Scope scope = 3;
  int64 timestamp = 4;
  string source_instance_id = 5;
  oneof payload {
    SyncState sync_state = 6;
    Conflict conflict = 7;
    ForwardState forward_state = 8;
    shiki.v1.SessionEvent session_event = 9;
    bool rescan_required = 10;
  }
}
message PublishEventRequest {
  Event event = 1;
  string expected_host_instance_id = 2;
}

// 插件 → 同机宿主。Remote* 仅 agent 提供；daemon 端调用会拒绝。
service CoreAPI {
  rpc GetProject(Scope) returns (shiki.v1.Project);
  rpc ListProjects(Scope) returns (shiki.v1.ListProjectsResponse);
  rpc GetSession(Scope) returns (shiki.v1.Session);
  rpc ListSessions(Scope) returns (shiki.v1.ListSessionsResponse);
  rpc GetMapping(Scope) returns (Mapping);
  rpc ListMappings(Scope) returns (MappingList);
  rpc SubscribeEvents(SubscribeRequest) returns (stream Event);
  rpc PublishEvent(PublishEventRequest) returns (shiki.v1.Empty);
  rpc Log(shiki.v1.LogRecord) returns (shiki.v1.Empty);
  rpc GetConfig(ConfigRequest) returns (PluginConfig);
  rpc SetConfig(SetConfigRequest) returns (PluginConfig);
  rpc RemoteListFiles(RemoteListFilesRequest) returns (shiki.v1.ListFilesResponse);
  rpc RemoteStatFile(RemoteStatFileRequest) returns (shiki.v1.FileInfo);
  rpc RemoteReadFile(RemoteReadFileRequest) returns (stream shiki.v1.FileData);
  rpc RemoteWriteFile(stream RemoteWriteFrame) returns (shiki.v1.FileInfo);
  rpc RemoteDeleteFile(RemoteDeleteFileRequest) returns (shiki.v1.Empty);
  rpc RemoteWatchFiles(RemoteWatchFilesRequest) returns (stream shiki.v1.FileEvent);
  rpc RemoteListPorts(RemoteListPortsRequest) returns (shiki.v1.ListPortsResponse);
  rpc RemoteWatchPorts(RemoteWatchPortsRequest) returns (stream shiki.v1.PortEvent);
  rpc RemoteForwardPort(stream RemotePortFrame) returns (stream shiki.v1.PortData);
}
message RemoteListFilesRequest { Scope scope = 1; shiki.v1.ListFilesRequest request = 2; }
message RemoteStatFileRequest { Scope scope = 1; shiki.v1.StatFileRequest request = 2; }
message RemoteReadFileRequest { Scope scope = 1; shiki.v1.ReadFileRequest request = 2; }
message RemoteDeleteFileRequest { Scope scope = 1; shiki.v1.DeleteFileRequest request = 2; }
message RemoteWatchFilesRequest { Scope scope = 1; shiki.v1.WatchFilesRequest request = 2; }
message RemoteListPortsRequest { Scope scope = 1; shiki.v1.ListPortsRequest request = 2; }
message RemoteWatchPortsRequest { Scope scope = 1; shiki.v1.WatchPortsRequest request = 2; }
message RemoteWriteOpen { Scope scope = 1; shiki.v1.WriteFileHeader header = 2; }
message RemoteWriteFrame {
  oneof payload { RemoteWriteOpen open = 1; bytes chunk = 2; }
}
message RemotePortOpen { Scope scope = 1; shiki.v1.PortData connect = 2; }
message RemotePortFrame {
  oneof payload { RemotePortOpen open = 1; shiki.v1.PortData data = 2; }
}

// 宿主 → server-side 插件，消息与远端门面复用，业务操作不会回调 Remote*。
service FileService {
  rpc ListFiles(shiki.v1.ListFilesRequest) returns (shiki.v1.ListFilesResponse);
  rpc StatFile(shiki.v1.StatFileRequest) returns (shiki.v1.FileInfo);
  rpc ReadFile(shiki.v1.ReadFileRequest) returns (stream shiki.v1.FileData);
  rpc WriteFile(stream shiki.v1.WriteFileRequest) returns (shiki.v1.FileInfo);
  rpc DeleteFile(shiki.v1.DeleteFileRequest) returns (shiki.v1.Empty);
  rpc WatchFiles(shiki.v1.WatchFilesRequest) returns (stream shiki.v1.FileEvent);
}
service PortService {
  rpc ListPorts(shiki.v1.ListPortsRequest) returns (shiki.v1.ListPortsResponse);
  rpc WatchPorts(shiki.v1.WatchPortsRequest) returns (stream shiki.v1.PortEvent);
  rpc ForwardPort(stream shiki.v1.PortData) returns (stream shiki.v1.PortData);
}

// 宿主 → client-side 插件：管理本地任务，不是跨网络暴露的 RPC。
service SyncWorker {
  rpc ConfigureMapping(Mapping) returns (SyncState);
  rpc GetSyncState(Scope) returns (SyncState);
  rpc ListConflicts(Scope) returns (ConflictList);
  rpc ResolveConflict(ResolveConflictRequest) returns (SyncState);
  rpc ReadConflictFile(ReadConflictFileRequest) returns (stream shiki.v1.FileData);
}
message SyncState {
  string mapping_id = 1;
  string mapping_revision = 2;
  string state = 3; // paused/reconciling/active/conflicted/offline/auth_required/error
  uint64 pending_files = 4;
  uint64 conflict_count = 5;
  string reason_code = 6;
}
message FileVersion {
  bool exists = 1;  // false=已删除；读错误不能转换为 false
  string content_hash = 2;
  uint32 mode = 3;
  string revision = 4; // 远端不透明 revision；本地由宿主/worker 计算的观测版本
}
message Conflict {
  string id = 1;
  string mapping_id = 2;
  string path = 3;
  FileVersion base = 4;
  FileVersion local = 5;
  FileVersion remote = 6;
  string generation = 7; // 任一侧版本变化时替换，旧选择不再有效
}
message ConflictList { repeated Conflict conflicts = 1; }
message ResolveConflictRequest {
  enum Choice {
    CHOICE_UNSPECIFIED = 0;
    KEEP_LOCAL = 1;
    KEEP_REMOTE = 2;
    KEEP_BOTH = 3;
  }
  string mapping_id = 1;
  string conflict_id = 2;
  string expected_generation = 3;
  Choice choice = 4;
  string request_id = 5;
}
message ReadConflictFileRequest {
  enum Side { SIDE_UNSPECIFIED = 0; LOCAL = 1; REMOTE = 2; }
  string mapping_id = 1;
  string conflict_id = 2;
  string expected_generation = 3;
  Side side = 4;
}
service PortWorker {
  rpc StartForward(ForwardSpec) returns (ForwardState);
  rpc StopForward(ForwardID) returns (shiki.v1.Empty);
  rpc ListForwards(shiki.v1.Empty) returns (ForwardList);
}
message ForwardSpec {
  string id = 1; // agent 分配，重复启动同 ID 同配置幂等
  string server_id = 2;
  string session_id = 3; // 可选，不能替代目标授权
  string remote_address = 4;
  uint32 remote_port = 5;
  string local_address = 6; // 默认 127.0.0.1
  uint32 local_port = 7; // 0 表示让 OS 选空闲端口
  bool enabled = 8;
  string revision = 9;
}
message ForwardID { string id = 1; }
message ForwardState {
  ForwardSpec spec = 1;
  uint32 bound_port = 2; // 实际 listener 地址，未绑定时为 0
  string state = 3; // starting/listening/stopped/offline/auth_required/error
  uint64 connections = 4;
  uint64 bytes_transferred = 5;
  string reason_code = 6;
}
message ForwardList { repeated ForwardState forwards = 1; }
```

### CoreAPI 权限与流规则

- Init 的短期凭据绑定 plugin_instance_id、side、插件 ID 和允许的 scopes；host_instance_id 由宿主启动时生成并通过 Init 提供。daemon Token 永不传给插件。
- Get/ListProject、Get/ListSession 只读受限项目/会话；server FileService 通过 GetProject 获取可信项目根，不能相信请求中的绝对路径；Get/ListMapping 仅 client-side sync 可查询已授权映射。daemon 不存在本地目录映射，返回 FailedPrecondition。
- Remote* 只接受 agent 中 client-side 的对应能力：sync 可用六个文件方法，port 可用三个端口方法。默认不授权任意其他方法；daemon CoreAPI 一律拒绝 Remote* 防止路由递归。
- 文件 scope 的 server/project/mapping 必须匹配宿主持久记录，内部 request.project_id 必须相同；port scope 与连接请求的 session/目标绑定，所有路径/端点仍由服务端复核。未授权 scope 返回 PermissionDenied，不靠插件自报来源。
- RemoteWriteFrame 首帧 open 固定 scope/header，后续只能 chunk；host 变换为 WriteFile 的 header/chunk。RemotePortFrame 首帧 open.connect 必须是 CONNECT，后续只允许 DATA/HALF_CLOSE/CLOSE；目标不得改变。
- 宿主→server worker 的请求通过私有 go-plugin gRPC/AutoMTLS 连接，宿主转发身份/项目授权上下文；FileService/PortService 不注册到公网或 tsnet listener。worker 验证路径/目标，不把任意来源都当已授权。
- PublishEvent 只允许该实例拥有的 task 的状态、冲突和进度；source/time/ID 由宿主核验或赋值，不准发布伪造的 session/control 事件。宿主确认持久化必要状态后 ACK；丢失 ACK 可按 event_id 幂等重发。
- Get/SetConfig 限于当前插件的 schema 和 scope，不得读写 Token、其他插件配置或任意核心配置；Set 使用 expected_revision，响应前持久化。Log 限额并脱敏。
- SubscribeEvents 是状态提示流：开始发送 rescan_required，消费者查询快照；有界队列溢出以 ResourceExhausted 结束，重订阅后重查状态，不宣称无损事件日志。取消流即取消订阅，无需独立 Unsubscribe RPC。
- host/plugin 退出使两侧 RPC context 取消。短 RPC 默认 5 秒，数据流继承宿主任务的可取消 context；每方向单 sender、有界缓冲。host 等待子调用时不持有状态锁，worker 回调 CoreAPI 不会形成同步死锁。

### worker 状态与宿主持久化

agent 拥有 mapping/forward 的期望配置、共同基线、冲突及 request_id 去重结果的持久存储；sync worker 拥有协调算法，port worker 拥有实际 listener。worker 通过宿主分配的私有任务状态目录读写基线日志，宿主只在 worker 停止后迁移/恢复该日志，禁止双写。

ConfigureMapping 以 revision 幂等应用配置：sync_enabled=false 取消任务并返回 paused；同 revision 不重复启动 watcher；先向 CoreAPI.GetMapping 核对当前 revision，不匹配即拒绝，宿主以当前完整配置重新下发。移除映射先 ConfigureMapping(false)、等待任务停止，再删除配置；未处理状态按 13 默认拒绝或显式归档，不删本地/远端项目文件。

StartForward 接收宿主已保存的期望配置，只有实际 bind 成功才返回 listening；重复 ID 不重复监听；StopForward 幂等关闭监听与活动流。重启仅恢复期望监听，不恢复连接或终端输入。

ReadConflictFile 是只读预览，严格匹配 generation 和文件当前版本，流结束前再次检查；不存在的一侧返回 NotFound。ResolveConflict 不自动接受文件的新版本：状态改变返回 Aborted 并发布新 generation。手动合并由用户编辑本地文件后刷新冲突，再以最新 generation 选择 KEEP_LOCAL；不提供绕过版本检查的 force。

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

Connect 客户端通过 [14](./14-connectrpc-transport.md) 的受控 HTTP transport 建立 Connect 客户端。v2 生成方法使用 protobuf 指针和各方法专用 stream 类型；接收使用 Receive() 返回消息与 error。WriteFile 用 CloseAndReceive() 获取最终提交结果；双向流 CloseSend() 后继续 Receive 到最终状态，退出时 Close()。代理必须显式转换下游远端错误，不能直接 return err。

## 生成与验证

必须先安装并锁定 protoc 31.1、protoc-gen-go v1.36.11 和 protoc-gen-connect-go v2.0.2（路径 connectrpc.com/connect/v2/cmd/protoc-gen-connect-go）。Go 运行库使用 connectrpc.com/connect/v2 v2.0.2，最低 Go 1.26.0；禁止混用 v1 的 Request/Response wrapper 或生成器。protoc 发行工具的来源与校验值需在 Phase 0 工具清单固定，不能仅锁 Go 插件。插件通道另锁定 protoc-gen-go-grpc v1.6.2、grpc-go v1.85.0 和 go-plugin v1.8.0。以下命令在本文三个文件、13 中的 agent.proto 及 go.mod 建立后使用；module 占位符需在实现初始化时统一替换。

```bash
protoc -I proto \
  --go_out=. --go_opt=module=github.com/yourusername/shikigami \
  --connect-go_out=. --connect-go_opt=module=github.com/yourusername/shikigami \
  proto/shiki.proto proto/types.proto proto/plugin.proto proto/agent.proto

# 仅 go-plugin 服务需要 gRPC 绑定；不要给 shiki/agent 生成 gRPC 服务。
protoc -I proto \
  --go-grpc_out=. --go-grpc_opt=module=github.com/yourusername/shikigami \
  proto/plugin.proto
```

消息生成到 pkg/protocol、pkg/protocol/plugin、pkg/protocol/agent；Connect stub 分别在其 protocolconnect、pluginconnect、agentconnect 子包（*.connect.go）。只有 plugin 包额外生成 plugin_grpc.pb.go；不生成 shiki_grpc.pb.go 或 agent_grpc.pb.go。不能使用 paths=source_relative 把不同 Go package 的文件混写进 proto/。校验语法可用 protoc 的 descriptor 输出；完整 Go 编译仍需生成器和依赖。

同一个 plugin.proto 同时生成两套 service 绑定是工具粒度限制，不代表所有 service 开放两种协议：CoreAPI 只注册 Connect，其他五个 service 只注册 go-plugin gRPC。proto import 的 types.proto 仅需消息生成，不需要为远端服务生成 gRPC；运行库和 adapter 规则见 [14](./14-connectrpc-transport.md)。

## 协议版本与交付状态

Connect 的线协议版本（Connect-Protocol-Version: 1）、Go 模块 v2 与以下应用协议版本互相独立，不得混用。协议 major=1，初始 minor=0；包名 `.v1` 标识 major。CLI→agent 用 13 的 Handshake，agent→daemon 用 NegotiateProtocol，宿主→插件先 GetInfo 后在 Init 确认协商结果。协商要求 major 相同、minor 区间有交集、双方 required_capabilities 均被交集满足；任一不满足返回 FailedPrecondition，不静默以“旧版模式”继续写数据。

初始能力按边界公布：本地 agent 为 `agent.control.v1`/`agent.events.v1`，NodeControl 为 `node.login.v1`，插件回调为 `core.scope.v1`、客户端 worker 为 `sync.worker.v1`/`port.worker.v1`；远端及对应 server worker 为 `terminal.v1`、`session-config-snapshot.v1`、`sync.revision.v1`、`port.tcp-half-close.v1`、`plugin.manage.v1`、`stats.v1`。只公布已实现且满足协议语义的能力；插件 ready 与否另查 PluginStatus，capability 存在不保证运行健康。可选功能不足时禁用该功能，不阻止兼容的终端核心。

NegotiateProtocol 已认证但不要求业务权限，只返回版本/能力，避免只读 Token 无法协商。后续调用仍逐个授权；协商结果不是权限。双方部署新版本后重建连接并协商；不能改变既有流的消息语义。

| 定义 | 文档来源 | 实现与验收 |
|---|---|---|
| 远端会话/文件/端口/快照/插件管理/统计 | 本文 shiki.proto + types.proto | 按 08 对应阶段实现，所有新增方法进入权限表 |
| 插件生命周期、CoreAPI、FileService/PortService、SyncWorker/PortWorker | 本文 plugin.proto；路由语义见 04 | Phase 0 编译/真实进程回调原型，Phase 2/3 完成功能 |
| 本地管理 RPC + 远端代理路由 | 13 的 agent.proto + 本文远端 schema | Phase 0 握手/UID/取消原型，Phase 1 核心接入 |

以上为已定义、尚未实现的协议草案，不能作为功能完成证明；Phase 0 不再以“缺少 IPC/CoreAPI schema”为待定项，而是将草案落盘生成代码并验证真实进程行为。
