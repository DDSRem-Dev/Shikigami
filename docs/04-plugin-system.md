# 插件系统设计

## 边界与部署

采用 HashiCorp go-plugin，每个插件是独立子进程；本地宿主为 shiki-agent，远端宿主为 shiki-daemon。CLI/TUI 不启动另一份插件。插件崩溃可隔离，插件仍具备同一 OS 用户的文件与进程权限，不是安全沙箱。

内置插件 ID 统一为 `sync`、`port`，二进制 `shiki-plugin-sync`、`shiki-plugin-port`。显示名称可描述为“文件同步”“端口转发”，不能把另一个名字作为配置 ID。

同一二进制显式使用 `--side=client|server` 启动；两端分别构建对应 OS/arch 的产物，不能把 macOS 插件复制到 Linux。非 Go 插件还需实现 go-plugin 的握手、启动协议和 RPC，只有同名 gRPC service 不够。

## 两种通信路径

```text
同机控制：宿主 → go-plugin gRPC → 插件生命周期/配置
同机回调：插件 → CoreAPI（受限本地端点）→ 宿主
跨机：本地插件 → agent CoreAPI → daemon ShikiDaemon RPC
                                → 服务端插件 API → 文件/端口资源
```

Go 的 `CoreAPI` interface、handler 函数或 `*grpc.ClientConn` 不能直接跨进程传递。插件初始化只传序列化配置、side、协议版本和私有 CoreAPI 端点；插件 SDK 在本进程构造 gRPC stub，负责把 Subscribe 注册映射成事件流。

CoreAPI 必须包含项目映射查询、按 mapping_id 的资源访问、远端操作代理和结构化日志。旧的 `GetCurrentSession()` 只能在明确会话 scope 下成立；常驻 agent/daemon 不存在一个全局“当前会话”。调用必须携带 server_id/project_id/mapping_id 或 session_id。

CoreAPI 端点使用 Unix socket/进程私有通道或 GRPCBroker；凭据是一次启动的短期能力凭据，不是用户 daemon Token。插件不能获取任意 daemon 连接或读取其他插件配置。宿主用调用 scope 和插件能力表检查每次转发。

远端 ShikiDaemon 中的文件/端口 RPC 是受认证的路由门面；实际 I/O 转交对应服务端插件。插件禁用返回 FailedPrecondition。未来新插件若要新增跨机数据面，必须声明、版本化并注册协议，不能宣称任意插件自动可远程调用。

## SDK 与协议

公共 SDK 放在 `pkg/plugin/`，管理器放在 `internal/plugin/`。公共 SDK 定义：

- 插件元信息、协议版本范围、side、capabilities。
- 生命周期 Init/Start/Stop/Health。
- 显式 scope 的 CoreAPI、订阅句柄、结构化事件和日志。
- sync/port 功能 stub 与取消语义。

[09](./09-protocol-definition.md) 提供生命周期 protobuf 草案；完整回调和双端转发 schema 必须在 Phase 0/插件阶段补齐并做真实进程集成测试。文中的接口用途不能冒充已生成或可调用的协议。

事件采用可序列化 schema，包含 event_id/type/source/server_id/project_id/session_id/mapping_id 与按事件类型校验的 payload。日志字段不输出 Token。慢订阅者使用有界队列，断流后获取快照重建状态，不能无界缓存。

## 生命周期

1. 校验名称、manifest、协议版本、side、路径和发布校验信息；禁止路径穿越或从可被其他用户替换的位置执行。
2. 启动进程并在超时内完成握手、Dispense、安全类型断言和 Init。任何失败均 Kill 并回收进程，不泄漏半初始化客户端。
3. Start 返回代表后台运行已就绪；插件任务 context 来自插件运行生命周期，不能使用已经结束的 Start RPC context。
4. 每个插件/映射串行启停，幂等 Start/Stop；Stop 取消 watcher/流/listener、等待协程结束、超时强制结束并 Wait。
5. 插件失败关闭相关请求并更新健康状态；有界退避重启。sync 重启必须重新对账；port 重启只能恢复监听，不能恢复旧 TCP 会话。
6. 不持有 manager 全局锁调用插件或网络，也不向外暴露可变 LoadedPlugin 指针。状态快照与操作生命周期分开。

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

每条本地 TCP 连接对应一条 ForwardPort gRPC 流；首帧 CONNECT 固定目标，服务端验证、拨号成功后回复 CONNECTED。其后只传 DATA/HALF_CLOSE/CLOSE，禁止中途改目的地。远端限于允许的本机地址，不提供任意内网代理。

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
```

插件管理始终明确 local/remote；远端管理需 plugin.manage。第三方分发、插件市场、自动更新签名信任根为后续工作，未具备前不提供“自动下载并执行任意插件”的承诺。
