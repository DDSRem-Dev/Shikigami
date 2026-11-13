# 插件系统设计

## 边界与部署

保留 HashiCorp go-plugin，每个插件是独立子进程；本地宿主为 shiki-agent，远端宿主为 shiki-daemon。CLI/TUI 不启动另一份插件。插件崩溃可隔离，插件仍具备同一 OS 用户的文件与进程权限，不是安全沙箱。

内置插件 ID 统一为 `sync`、`port`，二进制 `shiki-plugin-sync`、`shiki-plugin-port`。显示名称可描述为“文件同步”“端口转发”，不能把另一个名字作为配置 ID。

同一二进制显式使用 `--side=client|server` 启动；两端分别构建对应 OS/arch 的产物，不能把 macOS 插件复制到 Linux。非 Go 插件还需实现 go-plugin 的握手、启动协议和 RPC，只有同名 gRPC service 不够。

第三方包使用配置中的 `plugins_dir/<id>/manifest.json` 与相邻的 `shiki-plugin-<id>`；agent 只接纳 `client` side，daemon 只接纳 `server` side。第三方自身版本不必等于宿主版本，但必须与实际子进程 `GetInfo` 返回值逐字一致；manifest schema 1 使用 SDK protocol 1.0，协商还要求进程实现其声明的全部 capabilities。`sync` / `port` 保留独立的发行版/内嵌产物校验，不允许第三方同名目录覆盖。

用户已确认：无法替换的 go-plugin 内部通道继续使用原生 gRPC；可独立替换的远端通信、本地 IPC、CoreAPI 回调使用 ConnectRPC。go-plugin 的 GRPCServer/GRPCClient 接口绑定 `*grpc.Server` / `*grpc.ClientConn`，不能将 Connect handler 塞入这两个接口。

## 通信路径与协议边界

```text
同机控制/worker：宿主 → go-plugin 原生 gRPC → 插件生命周期/功能服务
同机回调：插件 → ConnectRPC over 私有 UDS → 宿主 CoreAPI
跨机：本地插件 → Connect CoreAPI → agent → Connect ShikiDaemon → daemon
                                → go-plugin gRPC 服务端插件 API → 文件/端口资源
```

Go 的 `CoreAPI` interface、handler 函数或 `*grpc.ClientConn` 不能直接跨进程传递。插件初始化只传序列化配置、side、协议版本和私有 CoreAPI 端点；宿主 SDK 构造 go-plugin gRPC stub，插件 SDK 构造 CoreAPI 的 Connect client，负责把 Subscribe 注册映射成事件流。两者只共享 protobuf 消息，不能互换流对象或客户端类型。

CoreAPI 必须包含项目映射查询、按 mapping_id 的资源访问、远端操作代理和结构化日志。旧的 `GetCurrentSession()` 只能在明确会话 scope 下成立；常驻 agent/daemon 不存在一个全局“当前会话”。调用必须携带 server_id/project_id/mapping_id 或 session_id。

CoreAPI 固定使用宿主提供的独立 Unix socket 上 Connect/h2c，不使用 GRPCBroker 回调；凭据是一次启动的短期能力凭据，不是用户 daemon Token。插件不能获取任意 daemon 连接或读取其他插件配置。宿主用调用 scope 和插件能力表检查每次转发。

远端 ShikiDaemon 中的文件/端口 RPC 是受认证的路由门面；实际 I/O 转交对应服务端插件。插件禁用返回 FailedPrecondition。未来新插件若要新增跨机数据面，必须声明、版本化并注册协议，不能宣称任意插件自动可远程调用。

## SDK 与协议

公共 SDK 放在 `pkg/plugin/`，管理器放在 `internal/plugin/`。公共 SDK 不暴露 grpc.ClientConn 或 connect.Client；内部 adapter 封装两种协议并提供类型化业务接口。公共 SDK 定义：

- 插件元信息、协议版本范围、side、capabilities。
- 生命周期 Init/Start/Stop/Health。
- 显式 scope 的 CoreAPI、订阅句柄、结构化事件和日志。
- sync/port 功能 stub 与取消语义。

[09](./09-protocol-definition.md) 提供生命周期、完整 CoreAPI、客户端 worker 和服务端数据面的 protobuf 草案；本篇末尾定义双端路由。Phase 0 落盘生成并做真实进程集成测试，定义存在不代表服务已经实现。

事件采用可序列化 schema，包含 event_id/type/source/server_id/project_id/session_id/mapping_id 与按事件类型校验的 payload。日志字段不输出 Token。慢订阅者使用有界队列，断流后获取快照重建状态，不能无界缓存。

## 生命周期

1. 校验名称、manifest、协议版本、side、路径和发布校验信息；禁止路径穿越或从可被其他用户替换的位置执行。
2. 先建立当前插件实例的私有 CoreAPI listener、生成短期能力凭据，再启动进程并在超时内完成 go-plugin 握手、Dispense、安全类型断言、GetInfo 与 Init。Init 通过 go-plugin gRPC 发送 CoreAPI 端点/凭据，插件验证并建立 Connect 回调。任何失败均 Kill/Wait、撤销凭据和关闭 listener，不泄漏半初始化客户端。
3. Start 返回代表后台运行已就绪；插件任务 context 来自插件运行生命周期，不能使用已经结束的 Start RPC context。
4. 每个插件/映射串行启停，幂等 Start/Stop；Stop 取消 watcher/流/listener、等待协程结束、超时强制结束并 Wait。
5. 插件失败关闭相关请求并更新健康状态；有界退避重启。sync 重启必须重新对账；port 重启只能恢复监听，不能恢复旧 TCP 会话。
6. 不持有 manager 全局锁调用插件或网络，也不向外暴露可变 LoadedPlugin 指针。状态快照与操作生命周期分开。

管理器将每个已发现 ID 的 enabled、config 和 revision 写入宿主私有状态，`ManagePlugin` 使用 expected_revision 与持久 request_id 去重。`ListPlugins` / `ManagePlugin` 会刷新当前配置目录，因此 agent/daemon 无需重启便可发现新包；新的第三方包默认为 disabled。包被移走、损坏或不再兼容时，刷新立即撤销旧实例准入及回调凭据，然后等待进程清理；不继续提供旧可运行槽位。原 enabled/config/revision 不因文件缺失而改写，缺失包仍可显式 disable，已 disabled 的期望状态在重启和包重新出现后仍保持 disabled。更新/重启按新实例重新验证，既不从失效包回退到内置实现，也不从安装事务日志擅自启用插件。每次实际 launch 都重新验证路径、所有权、权限、manifest 与 binary 的精确摘要，go-plugin SecureConfig 再核对可执行文件。

### 保留 gRPC 的最小范围

- Plugin.GetInfo/Init/Start/Stop/Health，以及按 side 注册的 SyncWorker、PortWorker、FileService、PortService，使用 go-plugin 原生 gRPC。go-plugin 自身的握手、健康/控制流继续由库负责；不在 tsnet、公网或 agent 前端 socket 注册这些 gRPC 服务。
- CoreAPI 独立注册为 Connect service，仅插件进程可按当前实例 scope 调用；不把生成的 gRPC CoreAPI 服务注册到 go-plugin，也不向 worker 注册 Connect 的同名服务。plugin.proto 因此生成两套绑定，实际开放端点由服务白名单决定。
- 基线 go-plugin v1.8.0，AllowedProtocols 仅允许 ProtocolGRPC，启用 AutoMTLS，将 UnixSocketConfig.TempDir 指向宿主私有运行目录（0700）；首版拒绝意外 TCP/跨用户端点。magic cookie 用于启动协议识别，不能替代 mTLS 或能力授权。AutoMTLS 不支持 Reattach，恢复按启动新实例处理。
- CoreAPI 的独立 UDS 使用 0700 目录/0600 socket、双向 peer UID 校验与实例 Bearer 凭据。凭据通过 Init 提供，不放命令行/日志；插件退出、宿主重启或实例替换即失效，凭据到期取消相关回调流，由宿主按后台恢复流程受控重启实例并签发新凭据；现有协议没有在线刷新 RPC，不能假设 Init 可在运行中重复调用，也不能静默变成永久凭据。
- 插件的 ready 状态与协议能力必须同时依据当前实例和有效凭据。凭据已失效、实例结束或停止中，即使监管循环尚在等待进程退出，也不能继续广告 ready/能力或接纳新调用；该等待窗口报告 `failed / PLUGIN_INSTANCE_ENDED`，不改持久化 enabled/revision，后续状态由监管恢复流程更新。
- CoreAPI listener 必须在 Init 前 ready，调用 worker 时不得持有 host 状态锁，避免“Init/Start 等回调、回调等宿主锁”的死锁。go-plugin 管理的父子进程监管仍负责崩溃清理，不能把 Connect 池某条空闲连接的关闭当作父进程死亡。
- go-plugin gRPC 的 server/client 分别设置 1 MiB 的消息收发限制（MaxRecvMsgSize/MaxSendMsgSize 与 MaxCallRecvMsgSize/MaxCallSendMsgSize），不依赖 Connect 的配置自动作用到 gRPC。同样要求每方向单 sender、有界队列和 context 取消。协议适配详见 14。

## 文件同步插件

同步单位是项目映射，不是 terminal attachment。两个会话共享同一映射只启动一份 watcher；全部终端 detach 不停止用户已启用的后台同步。

首次同步：扫描两端清单，形成计划；没有共同基线时不按 mtime 决定谁覆盖谁。用户选择方向后仍需展示目标端已有不同内容、删除和冲突；不默认清空目标目录。

持续同步采用每路径共同基线、内容 hash、存在状态和远端 revision。任一端删除也必须与修改比较；详细算法见 [10](./10-implementation-details.md)。冲突暂停该路径并通知前端；其他文件继续。无前端时保持待处理，不自动选择远端。

fsnotify 是变更提示而非可靠日志：监听目录、递归注册新目录、处理 watcher overflow/error、定期/重连后全量核对。rename 后重扫涉及目录；按最终文件状态合并事件，而不是盲目执行 Remove。

上传和下载经私有临时文件、大小/校验/版本验证后原子替换。应用远端变更仍保留监听，用预期 hash/revision 抑制回声；不能 Remove 文件 watcher 来“暂停”父目录的事件。路径由 project_id + 相对路径定位，禁止远端传入绝对路径任意写盘。

## 端口转发插件

首版仅 TCP；UDP 需要不同的报文边界和会话协议，不能用 TCP 字节流接口宣称支持。服务端扫描的是主机网络命名空间；tmux 不隔离网络，同一地址/端口无法被两个独立服务正常同时独占。

端口记录包含绑定地址、端口、协议、进程信息和可选 session_id。仅能通过可靠进程归属识别会话；未知则保留 unknown，不把主机所有端口归给当前会话。扫描错误不等于端口列表为空。

自动转发需同时满足：当前用户可访问、已确认的会话/项目归属、允许规则、不在保护端口列表；未知端口先请求用户开启。9527、SSH、数据库等敏感服务默认不自动转发。端口大于 30000 不代表动态分配，仅作为提示，不作为授权依据。

本地默认绑定 `127.0.0.1`，IPv6 loopback 可显式增加；不能绑定 `:port` 暴露给局域网。真正 net.Listen 成功才算获取端口，不能用“先检查可用”代替原子绑定。冲突可选择备用端口并显示实际映射。

每条本地 TCP 连接对应一条逻辑 ForwardPort 通道（CoreAPI/远端用 Connect，daemon→worker 用 gRPC，逐跳桥接）；首帧 CONNECT 固定目标，服务端验证、拨号成功后回复 CONNECTED。其后只传 DATA/HALF_CLOSE/CLOSE，禁止中途改目的地。远端限于允许的本机地址，不提供任意内网代理。

StopForward 和 ListForwards 是本地 agent 的监听管理操作，不是远端 daemon RPC。关闭窗口不停止监听；显式 stop 取消其活动连接并关闭 listener。

## 打包、配置与命令

内嵌插件是可选构建方式：按目标平台预构建插件，存入带版本/平台的 0700 私有缓存；校验后原子落盘再执行。升级按内容/版本校验，不能只判断目录是否存在。客户端 agent 和远端 daemon 都需要匹配产物。`//go:embed` 文件应隔离在构建标签文件中，开发版不依赖不存在的生成目录。

```yaml
plugins:
  sync:
    enabled: true
    config:
      max_auto_sync_size: 10485760 # 10 MiB，流式分块传输
  port:
    enabled: true
    config:
      auto_forward: false         # 配置允许规则后才启用
      auto_allow_ports: []          # client 自动策略显式允许端口；server 另配 allow_ports
      bind_address: 127.0.0.1
```

```bash
shiki plugins list
shiki plugins enable sync --side local
shiki plugins enable sync --side remote --server my-vps
shiki plugins disable port --side local
shiki plugins status sync
shiki plugins logs sync --tail 50
shiki plugins restart sync
shiki plugins install /path/to/plugin.tar.gz
shiki plugins install example --market https://market.example/index.json
shiki plugins update example
shiki plugins fixture show --json --flag suffix=hello
shiki plugins sync status --json
shiki plugins action invoke sync get-sync-status --json
```

第三方插件分发通过静态 HTTPS marketplace index 完成；index 和每个归档都使用相同格式的 Ed25519 signed envelope。客户端只信任本地 0600 trust store 中的 publisher key，按 `key_id` 选择根并校验 index、插件 ID、版本、OS/arch、归档文件名、大小和 SHA-256；任何签名、平台或清单不匹配都拒绝且不执行。`shiki plugins install <url-or-path>` 是明确的手工信任入口，不能冒充 marketplace 信任；`shiki plugins install <id> --market <index-url>` 与显式 `shiki plugins update [id]` 只走签名 index/归档，更新沿用已记录的 market URL、版本和 key。没有周期后台更新器。上述 example URL 是格式示例，不是公开可用的 marketplace。CLI 安装/更新/卸载仅作用于当前用户的本地配置目录，生命周期复用 ListPlugins/ManagePlugin IPC，不创建通用远程执行或远程安装 RPC。插件仍以当前 OS 用户权限运行而非沙箱。


## Manifest 与 UI 注入

### manifest.json 格式

每个插件在二进制同目录提供一个 `manifest.json` 文件，宿主在进程启动**前**读取并缓存。字段说明：

安装器还在包目录生成私有 `.artifact-integrity.json`，记录精确安装字节的 `manifest_sha256` 与 `binary_sha256`。宿主要求此收据与普通、不可被其他用户替换的文件和目录；它是本地完整性记录，不是签名或额外信任根。手工安装也生成收据，表示用户明确接受该包；marketplace 的 publisher 签名、平台和分发信任仍归安装器验证。只有 `manifest.json` 而缺少收据的旧手工目录不能执行，须经显式安装入口重新发布，宿主不会临时计算摘要来“修复”收据。

| 字段 | 类型 | 说明 |
|---|---|---|
| schema_version | string | 当前为 `"1"` |
| id | string | 插件唯一 ID（如 `sync`、`port`） |
| version | string | 插件自身版本，与 GetInfo 精确一致；第三方不要求等于宿主 build |
| display_name | string | 展示名 |
| description | string | 插件描述 |
| binary | string | 相邻可执行文件名，必须精确为 `shiki-plugin-<id>`，不允许路径分隔符 |
| capabilities | []string | 非空、无重复的能力列表，要求 `core.scope.v1`；声明 CLI action 必须含 `ui.action.v1` |
| sides | []string | `["client"]` / `["server"]` / `["client","server"]` |
| binary_flags | []string | 额外启动参数，支持 `${side}`；宿主统一注入一份 `--side=client|server`，不得指定其他 side 或终止参数解析 |
| config_schema | []ConfigField | 配置字段声明，宿主用于预校验 SetConfig |
| ui | UIContribution | UI 注入声明 |

`config_schema` 中每个 `ConfigField` 包含 `key`、`type`（`string`/`int`/`float`/`bool`/`object`/`array`）、`label`、`description`、`default_json`、`required`、`enum_values`、`constraints`（最小/最大/长度/正则）及 `children`。object 的 children 声明嵌套字段，array 的单个 child 声明元素类型（可继续嵌套 object/array）。宿主使用 `pkg/plugin.ValidateConfig` 检查未知字段、必填、有限数字、整数、枚举、约束及嵌套结构；错误直接返回 `InvalidArgument`，不启动进程、不修改 config/revision。默认值声明也必须通过 schema 校验；`default_json` 是描述信息，不会绕过 required 或静默改写用户配置，插件实现仍负责自身未提供字段的运行默认值。`pkg/plugin.ValidateManifest` 在发现时校验 schema、side、能力、启动参数与 CLI flag 声明。

### UIContribution

`manifest.json` 的 `ui` 字段声明插件向宿主注入的所有 UI 节点：

```json
{
  "ui": {
    "status_slots": [{
      "slot_id": "primary",
      "label": "文件同步",
      "event_type": "plugin.status.sync.v1"
    }],
    "dashboard_panels": [{
      "panel_id": "sync-overview",
      "title": "同步状态",
      "event_type": "plugin.panel.sync.v1"
    }],
    "settings_section": {
      "section_id": "sync",
      "title": "文件同步"
    },
    "cli_commands": [{
      "name": "list-conflicts",
      "description": "列出当前冲突",
      "action_id": "list-conflicts",
      "flags": [{"name": "json", "type": "bool", "default": "false", "description": "以 JSON 格式输出"}]
    }]
  }
}
```

宿主在插件 ready 后向 `SettingsRegistry` 注册其真实静态 metadata；第三方的已验证 manifest 在 enable 前也可查询。直接 CLI 语法为 `shiki plugins <id> <command> [--json] [--flag key=value]`，agent 按当前 manifest 的 `ui.cli_commands` 将 command_name 映射成 action_id，并校验声明的 flag 类型及默认值；不是启动 CLI 时连接 agent 动态重写 Cobra 命令树，也不承诺离线 help 列出每个注入命令。固定管理命令保留优先级；原始 action 调用 `shiki plugins action invoke <id> <action-id> [--json] [--flag key=value]` 是独立脚本接口。新发行版的内置 artifact 携带由真实源 manifest 生成并校验的嵌套 metadata；旧 alpha.2 没有此可选字段仍可装载，但不能虚构其 command-name metadata。

### StatusPayload 与 NotifyPayload

插件可通过 CoreAPI `PublishEvent` 发布以下 payload。它们是协议和 metadata 契约；首版 TUI 的通用插件 status-slot、dashboard、settings/form 与通知渲染器仍待实现，GUI 也未交付，不得把下面的声明视为已有可见界面：

```
// 状态槽更新：event_type = "plugin.status.<id>.v1"
StatusPayload { text, state, icon, detail string }

// 通知横幅：event_type = "plugin.notify.<id>.v1"
NotifyPayload { level, title, message, action_id string }
```

`state` 字段使用有限词汇（如 `ok`、`warning`、`error`、`syncing`），前端仅展示已知状态的对应图标；未知状态降级为默认图标。

### InvokeAction 调用链

manifest 中的 `cli_commands` 声明 action_id；dashboard/status/settings 当前仅声明 metadata，不会自动形成交互界面。已实现的本地 action 调用链：

```text
shiki plugins <id> <command> --flag key=value [--json]
  → agent IPC InvokePluginAction（AgentActions Connect service）
  → agent 读取当前已验证 manifest，将 command_name 解析为 action_id
  → internal/plugin/Supervisor.Call(ctx, id, "ui.action.v1", fn)
  → fn 通过 lifecycleClient.InvokeAction 发送生成的 Plugin.InvokeAction gRPC 到插件进程
  → 插件 ActionImplementation.InvokeAction 处理并返回 ActionResult
```

`ActionResult` 的 `kind` 字段决定结果处理方式：

- `ActionResultText` (0)：直接打印文本
- `ActionResultForm` (1)：输出结构化 `ActionForm`（含字段和 submit action_id），不提供交互式表单渲染
- `ActionResultJSON` (2)：格式化 JSON 输出
- `ActionResultNotify` (3)：输出 `NotifyPayload` 描述，不自动形成 TUI 通知

`--json` 输出保留 `{kind, done, json|text|form|notify}` envelope；JSON action 的业务对象位于 `json` 字段，而不是文本 envelope。未协商 `ui.action.v1` 的插件会在宿主准入时返回 FailedPrecondition；如果进程声明了该能力却未实现 `ActionImplementation`，SDK InvokeAction 返回 Unimplemented，宿主传递错误而不崩溃或回退。

该能力只在实际实现 ActionImplementation 的内置 client sync/port 上协商；server 内置插件不声明 `ui.action.v1`。远程 `InvokePluginAction` 明确返回 Unimplemented，第三方安装也不会自动获得跨机 action/data-plane 路由。`ActionResultForm`/Notify 可以结构化输出，但没有通用 TUI 表单或通知渲染闭环。

### PluginRegistry 扫描路径

`internal/plugin/registry.go` 只扫描当前宿主配置的 `plugins_dir` 一层；默认 agent 为 `~/.shiki/plugins/`，daemon 为其配置的插件目录。不额外扫描另一个用户或硬编码“系统优先级”目录：

- `<id>/manifest.json`、`.artifact-integrity.json` 与 `shiki-plugin-<id>` 必须共同通过校验，manifest ID 必须等于目录名；不接纳包目录/manifest/binary 符号链接、特殊文件或可被其他用户替换的位置。
- `.builtin`、`.embedded`、`.plugin-txn-*` 等私有目录和 `sync`/`port` 第三方目录不会注册。新安装的 plugins_dir 是保留第三方数据的私有真实目录，内置文件通过 `.builtin` 目录指针选择不可变发行版；宿主验证该指针本身、原路径与目标的所有权/权限链，manifest/binary 叶节点仍须为普通文件。指针存在但无效时不回退。指针不存在才读取旧的 flat `<id>.manifest.json` 或匹配 build 的内嵌缓存；旧安装的整个 plugins_dir 管理符号链接仍可只读装载，安装器必须合法迁移后才可写入第三方包。
- 每次刷新以新扫描结果替换注册表，损坏/移除的包不会留下旧缓存条目。缺失目录的持久 desired intent 留存但不能执行；未知或无效包不能被 ENABLE/RESTART/SET_CONFIG 接纳，DISABLE 始终可撤销已有期望状态。
- marketplace index 与归档签名绑定 `id/version/os/arch/url-independent archive name/size/SHA-256`，trust root 由用户显式导入。显式 update 拒绝未知 key、降级、平台不匹配或校验失败；手工安装不因此获得 marketplace 更新资格。


## 完整 RPC 分工与双端路由

[09 的 plugin.proto](./09-protocol-definition.md) 是以下接口的 schema 来源。协议已定义，Phase 0 需落盘生成和跨进程验证，不再把 CoreAPI 留作未定义的 Go interface。

| 调用方向 | Service / 方法 | 作用与限制 |
|---|---|---|
| host → 插件 | Plugin.GetInfo/Init/Start/Stop/Health | 协议协商、side、instance、私有状态目录和生命周期 |
| 插件 → host | CoreAPI.GetProject/ListProjects/GetSession/ListSessions | 授权范围内只读资源；服务端文件插件从 host 获取项目根 |
| client sync → agent | CoreAPI.GetMapping/ListMappings | 本机映射，不把 local_root 上传给 daemon |
| 插件 → host | CoreAPI.GetConfig/SetConfig/Log | 自身配置的 revision 更新、脱敏日志，不访问用户 Token |
| 插件 ⇄ host | CoreAPI.SubscribeEvents/PublishEvent | 订阅取消、状态回调、冲突持久化通知；来源由 host 绑定 |
| client sync → agent → daemon | CoreAPI.RemoteListFiles/RemoteStatFile/RemoteReadFile/RemoteWriteFile/RemoteDeleteFile/RemoteWatchFiles | 携带并核对 scope，复用远端文件 RPC，支持分块流 |
| client port → agent → daemon | CoreAPI.RemoteListPorts/RemoteWatchPorts/RemoteForwardPort | endpoint 授权、TCP 双向流，不暴露 daemon Connect client |
| daemon → server sync | FileService.ListFiles/StatFile/ReadFile/WriteFile/DeleteFile/WatchFiles | 真正执行远端文件操作；插件服务仅通过私有通道提供 |
| daemon → server port | PortService.ListPorts/WatchPorts/ForwardPort | 扫描与拨号；真正的数据面 |
| agent → client sync | SyncWorker.ConfigureMapping/GetSyncState/ListConflicts/ResolveConflict/ReadConflictFile | 本地任务和冲突管理，pause/resume 使用映射期望状态 |
| agent → client port | PortWorker.StartForward/StopForward/ListForwards | 本机 listener；由 host 指定稳定任务 ID |

### 本地文件上传

```text
client sync
  → CoreAPI.RemoteWriteFile(open{scope, header}, chunks)
  → agent 验证 mapping + plugin_instance + scope
  → agent 注入远端 Token，去掉本地 scope/凭据
  → daemon ShikiDaemon.WriteFile(header, chunks)
  → daemon 验证 file.write、项目归属、插件状态
  → server sync FileService.WriteFile(header, chunks)
  → 校验 revision、提交文件 → FileInfo 沿原调用链返回
```

远端鉴权以设备 Token 为准，客户端 plugin_instance_id/server_id/mapping_id 不作为远端授权参数。daemon 不转发用户 Token 给 server plugin；只在受信私有通道传递已验证 scope，文件插件仍进行项目路径/条件写检查。

### 远端变更返回本地

```text
client sync 发起 RemoteWatchFiles(scope)
  → agent 发起 daemon.WatchFiles
  → daemon 发起 server sync.FileService.WatchFiles

server sync 发现变化
  → 同一条已建立的服务端流返回 FileEvent
  → daemon → agent → client sync 的订阅接收循环
  → client sync 重新读取版本、对账、必要时下载
  → CoreAPI.PublishEvent(sync_state/conflict)
  → agent 持久记录并通过本地 WatchEvents 提示前端
```

这就是内置 sync 的反向通信路径；daemon 不对客户端打开入站连接，不直接调用任意本地文件写入函数。port 的响应同理沿已建立的 ForwardPort 双向流返回；远端端口发现沿 WatchPorts 返回。远端不能通过 generic CallPlugin 要求 agent 执行任意插件方法。

首次同步不依赖一定收到每个事件；WatchFiles 断流/溢出后重新扫描。服务端流发送错误沿链返回，所有代理取消下游；同一流每方向一个 sender，不在 host 锁内等待 worker 回调。

### 后台恢复与能力

本地 Agent 接口见 [13](./13-local-ipc-protocol.md)。host 持久化期望配置，worker 执行并回报实际状态；停止/重启按 [06](./06-edge-cases.md) 的顺序完成。插件进程死亡先取消路由，再启动兼容实例并下发当前配置；旧实例的 PublishEvent 因 instance 不匹配拒绝。

内置插件 capability 必须与 side 匹配。client sync 需要 `core.scope.v1` 和 `sync.worker.v1`；server sync 实现 FileService 并声明 `sync.revision.v1`；port 两端分别实现 PortWorker 和 PortService。缺少其中一端不能显示“同步/转发可用”，即使插件可执行文件已经存在。

### SDK 文件接收与停止完成

Go SDK 的 `FileWorkerService` 用于宿主到插件的传输实现，`Services.Files` 要求 `WriteFile` 的接收回调显式接受 `context.Context`。本地/客户端同步协调所用 `FileService` 保留同步本地reader形状；两者共享读操作契约。第三方worker源码在重建到当前尚未发行的SDK时须适配此接口，protobuf/原生gRPC方法和capability未变，不提供不支持取消的传输回退。

SDK每次上传只有一个transport reader，取消或终态后不再次读取。真正的gRPC阻塞Recv在handler返回终止该流后退出；不能先在handler内等待Recv结束而形成死锁。worker在接收首帧前登记活动及并发名额，停止取消并等待实际活动完成；Stop超时不表示已经停止，SDK及worker重试均继续等待原清理完成，只有实现成功后才记录stopped。准入默认值与首帧预算见14，行为证据见[文件准入验收](15-implementation-status.md#evidence-file-admission-validation)。
