# 项目结构与依赖

当前实际仓库只有 docs/。下列均是拟建目录，不代表已存在；完整开发顺序见 [08](./08-roadmap.md)。

```text
shikigami/
├── README.md / LICENSE / go.mod / go.sum / Makefile
├── .github/workflows/                 # CI / release
├── docs/                              # 00–12 + README
├── proto/
│   ├── shiki.proto                    # 远端 RPC
│   ├── types.proto                    # 远端共享消息
│   ├── plugin.proto                   # 生命周期与 CoreAPI
│   └── agent.proto                    # 本地 IPC，Phase 0 补齐
├── pkg/
│   ├── protocol/                      # shiki/types 生成包
│   │   ├── shiki.pb.go / shiki_grpc.pb.go / types.pb.go
│   │   ├── plugin/                    # 独立 go_package，不能混写
│   │   │   └── plugin.pb.go / plugin_grpc.pb.go
│   │   └── agent/                     # 本地 IPC 生成包
│   ├── plugin/                        # 外部插件可导入的 SDK
│   │   ├── interface.go / host_client.go / events.go
│   │   └── sync.go / port.go           # 类型化 stub 与 scope
│   ├── config/                        # 配置 schema / 验证 / 迁移
│   └── errors/                        # 共享错误映射
├── internal/
│   ├── agent/                         # 本机单实例后台
│   │   ├── agent.go / ipc.go / lock.go
│   │   ├── mappings.go / sync.go / forwards.go
│   │   └── connections.go / credentials.go
│   ├── client/                        # 前端通用 API，通过 IPC 调 agent
│   │   ├── client.go / terminal.go / events.go
│   │   └── ui/                        # CLI/TUI 菜单、格式、进度
│   ├── connection/                    # tsnet/TLS/重连，宿主持有
│   ├── auth/                          # JWT、撤销、权限表
│   ├── server/
│   │   ├── daemon.go / grpc.go / auth.go
│   │   ├── project/                    # 项目管理
│   │   ├── session/                    # 生命周期、attachment、配置快照
│   │   ├── tmux/                       # 统一 socket / PTY / 进程控制
│   │   ├── storage/                    # 状态快照、迁移、请求去重
│   │   └── monitor/                    # 资源与诊断
│   └── plugin/                        # 宿主实现，依赖 pkg/plugin
│       ├── manager.go / loader.go / broker.go
│       └── core_api.go / routes.go
├── plugins/
│   ├── sync/                          # 单一入口 + client/server 模式
│   │   └── main.go / watcher.go / reconcile.go / transfer.go / ignore.go
│   └── port/
│       └── main.go / scanner.go / forward.go / registry.go
├── cmd/
│   ├── shiki/                         # CLI 命令，仅参数和展示
│   ├── shiki-agent/                   # 本地后台入口
│   └── shiki-daemon/                  # serve/init/token 等入口
├── apps/shiki-gui/                     # 后续，同仓同 Go module
│   ├── main.go / app.go
│   └── frontend/                      # Wails + React/TypeScript
├── configs/
│   ├── client.example.yaml / daemon.example.yaml
│   ├── systemd/                       # daemon、agent 用户服务
│   └── launchd/                       # macOS agent
├── scripts/
│   ├── install.sh / install-client.sh / uninstall.sh
│   └── generate-proto.sh / build.sh / dev-setup.sh
└── test/
    ├── integration/                   # 跨进程/插件/协议测试
    ├── e2e/                           # 真实 tmux/网络/恢复场景
    └── fixtures/                      # 测试用数据，不含凭据
```

## 依赖约束

```text
CLI / GUI → internal/client → agent IPC
agent / daemon → internal 的业务模块 → pkg/protocol、pkg/plugin
插件 → pkg/plugin、生成的 protocol；不依赖 internal
```

插件公共 interface 放 pkg/plugin，不能仅定义在 internal/plugin 后又让第三方导入。核心业务默认 internal，只有稳定 SDK/协议公开；不为所有内部工具建立公共兼容承诺。

GUI 位于同仓 Go module 内，合法复用 internal/client；独立外部仓库不能直接导入 internal。GUI 不直接连接远端或独立运行 tsnet，以免绕开本地 agent 的资源归属。

生成代码按 go_package 输出：plugin 在 pkg/protocol/plugin，agent 在 pkg/protocol/agent。protoc 以 module 参数生成，命令见 [09](./09-protocol-definition.md)。

## 模块职责

| 模块 | 所有权 |
|---|---|
| agent | 本地凭据、节点、连接、映射、转发注册、前端生命周期 |
| server/project + session | 远端资源身份、状态转换、权限后的业务操作 |
| server/storage | 单写者原子状态、恢复、schema 迁移、幂等请求记录 |
| server/tmux | 所有 tmux 命令和 PID/PTY 资源管理，不散落在 RPC handler |
| plugin host | 进程启动、超时/重启、CoreAPI、双端路由 |
| sync plugin | 文件监听、对账、版本/冲突、文件传输 |
| port plugin | 端口发现、监听、TCP 转发；状态上报给宿主 |
| frontend | 输入和显示，不能自行修改后台状态文件 |

## 命令契约

```text
shiki                         智能连接
shiki connect                 首次服务器配置
shiki attach <session>        连接；别名 shiki @ <session>
shiki agent start|stop|status
shiki sessions list|create|info|edit|delete
shiki sessions stop-claude|restart-claude
shiki sessions snapshot|snapshots|restore
shiki servers list|add|remove|switch|update
shiki plugins list|enable|disable|status|logs|restart
shiki sync status|pause|resume|resolve
shiki ports list|forward|stop
shiki dashboard|doctor|stats|logs
shiki init --template <name>
shiki config get|set|edit
shiki version
shiki-daemon serve|init|version
shiki-daemon token create|list|revoke|refresh
```

服务器参数与项目作用域需统一（`--server` / `--project`）；名称歧义时不任意选择。破坏性操作非交互需明确参数，输入不足报错，不能读取流水线数据当回答。命令到 RPC 的映射/尚待补齐协议见 09。版本可提供 `--version` 与 `version` 一致别名。

## 构建与质量门槛

拟建 Makefile 目标：generate-proto、build（client/agent/daemon/plugins）、build-embedded、test、test-integration、test-e2e、test-all、lint、fmt、vet、release、clean。每个目标只有在脚本实际存在且 CI 验证后才作为用户可运行指令。

Go module 路径和发布组织尚未设置；`github.com/yourusername/shikigami` 为占位符。版本锁定依赖和生成器，避免 @latest 使结果不确定。单文件内嵌构建按平台生成插件，开发构建不依赖该目录。

日志使用统一结构化接口，context 私有类型键；错误映射统一 gRPC 状态码。测试文件与模块邻近，跨进程集成放 test/；关键恢复/安全测试属于对应阶段交付，不推迟到发布前补做。
