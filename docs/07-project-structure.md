# 项目结构与依赖

仓库正在实施 Phase 0–5，实现状态见 [15](./15-implementation-status.md)。下列是最终目标目录，不代表全部已存在；完整开发顺序见 [08](./08-roadmap.md)。

```text
shikigami/
├── README.md / LICENSE / go.mod / go.sum / Makefile
├── .github/workflows/                 # CI / release
├── docs/                              # 00–14设计 + 实施/验收/发布准备记录
├── proto/
│   ├── shiki.proto                    # 远端 RPC
│   ├── types.proto                    # 远端共享消息
│   ├── plugin.proto                   # 生命周期与 CoreAPI
│   └── agent.proto                    # 本地 IPC，定义见文档 13
├── pkg/
│   ├── protocol/                      # shiki/types 生成包
│   │   ├── shiki.pb.go / types.pb.go
│   │   ├── protocolconnect/shiki.connect.go
│   │   ├── plugin/                    # 独立 go_package，不能混写
│   │   │   ├── plugin.pb.go / plugin_grpc.pb.go # 仅 go-plugin 使用 gRPC
│   │   │   └── pluginconnect/plugin.connect.go
│   │   └── agent/                     # agent.pb.go + agentconnect/agent.connect.go
│   ├── plugin/                        # 外部插件可导入的 SDK
│   │   ├── interface.go / host_client.go / events.go
│   │   ├── sync.go / port.go           # 类型化 stub 与 scope
│   │   └── state/ / config/ / limits/ / buildinfo/ # 插件可用的持久化、配置契约和构建信息
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
│   ├── releaseauth/                   # 固定版本归档签名/验签与显式HTTPS下载，不执行或安装归档
│   ├── install/                       # 短时事务安装、校验、备份、回滚与服务生成
│   ├── strictjson/                    # 拒绝重复键、尾随值和未知字段
│   ├── server/
│   │   ├── daemon.go / rpc.go / auth.go
│   │   ├── project/                    # 项目管理
│   │   ├── session/                    # 生命周期、attachment、配置快照
│   │   ├── tmux/                       # 统一 socket / PTY / 进程控制
│   │   ├── storage/                    # 状态快照、迁移、请求去重
│   │   └── monitor/                    # 资源与诊断
│   └── plugin/                        # 宿主实现，依赖 pkg/plugin
│       ├── manager.go / loader.go / grpc_adapter.go
│       ├── core_api.go / connect_adapter.go / routes.go
│       └── builtin*.go / embedded-assets/ # 可选内嵌加载；assets 仅构建时生成
├── plugins/
│   ├── sync/                          # 单一入口 + client/server 模式
│   │   ├── main.go
│   │   └── client/ / server/ / filetree/ / reconcile/ # 监听、对账、冲突及受根限制 I/O
│   └── port/
│       ├── main.go
│       └── client/ / server/ / tunnel/ / config/ # 扫描、监听、TCP 流与配置
├── cmd/
│   ├── shiki/                         # CLI 命令，仅参数和展示
│   ├── shiki-agent/                   # 本地后台入口
│   ├── shiki-daemon/                  # serve/init/token 等入口
│   └── shiki-install/                 # 短时安装/运维 helper，不是常驻宿主
├── apps/shiki-gui/                     # 后续，同仓同 Go module
│   ├── main.go / app.go
│   └── frontend/                      # Wails + React/TypeScript
├── configs/
│   ├── client.example.yaml / daemon.example.yaml
│   ├── systemd/                       # daemon、agent 用户服务
│   └── launchd/                       # macOS agent
├── scripts/
│   ├── install.sh / install-client.sh / uninstall.sh
│   ├── generate-proto.sh / build.sh / dev-setup.sh
│   └── build-embedded.sh / release.sh / release.go / release-trust.go / test-linux.sh
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

插件公共 interface 放 pkg/plugin，不能仅定义在 internal/plugin 后又让第三方导入。内置插件的入口和业务实现全部位于 `plugins/<id>/`；宿主不导入该目录的实现，配置验证通过 `pkg/plugin/config` 的公开契约共享。插件自己的持久化使用 `pkg/plugin/state`，daemon 的 Project/Session 资源约束仍留在 `internal/server/storage`。`test/architecture` 自动检查这条源码依赖边界。核心业务默认 internal，只有稳定 SDK/协议公开；不为所有内部工具建立公共兼容承诺。

GUI 位于同仓 Go module 内，合法复用 internal/client；独立外部仓库不能直接导入 internal。GUI 不直接连接远端或独立运行 tsnet，以免绕开本地 agent 的资源归属。

grpc-go 的直接依赖限定在 go-plugin 适配层、插件服务实现及 plugin 生成包；远端 connection/agent IPC 不使用 grpc.ClientConn 或 gRPC stub。生成代码按 go_package 输出：plugin 在 pkg/protocol/plugin，agent 在 pkg/protocol/agent。protoc 以 module 参数生成，命令见 [09](./09-protocol-definition.md)。

## 模块职责

| 模块 | 所有权 |
|---|---|
| agent | 本地凭据、节点、连接、映射、转发注册、前端生命周期 |
| server/project + session | 远端资源身份、状态转换、权限后的业务操作 |
| server/storage | 单写者原子状态、恢复、schema 迁移、幂等请求记录 |
| server/tmux | 所有 tmux 命令和 PID/PTY 资源管理，不散落在 RPC handler |
| plugin host | 进程启动、超时/重启、CoreAPI、双端路由 |
| install helper | 本地受信 bundle 校验、不可变版本目录、安装事务、私有备份、服务生成与本地节点探测；不是常驻宿主 |
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
shiki config get|set|edit|startup
shiki version
shiki-daemon serve|init|version
shiki-daemon token create|list|revoke|refresh
shiki-install install|rollback|uninstall|status|probe|version|help
```

服务器参数与项目作用域需统一（`--server` / `--project`）；名称歧义时不任意选择。破坏性操作非交互需明确参数，输入不足报错，不能读取流水线数据当回答。本地命令到 RPC 的映射见 13，远端及插件协议见 09。版本可提供 `--version` 与 `version` 一致别名。

## 构建与质量门槛

Makefile 已提供 generate-proto、build（client/agent/daemon/短时 installer/plugins）、build-embedded、test、test-tools、test-integration、test-e2e、test-linux、test-all、lint、fmt、vet、release、clean 等目标。test先通过test-tools执行采集器Python回归，要求Python3.10+及普通用户，避免以root跳过生命周期检查；随后执行Go race检查。本地执行证据与外部 CI、真实服务管理器及发行签名门槛分开记录，见 [15](./15-implementation-status.md)；release 默认生成本地包与摘要；显式配置 SHIKI_SIGNING_KEY 时追加离线 Ed25519 签名，release-trust 从可信源码配合独立公钥验签后生成私有归档副本；fetch显式接收HTTPS归档/签名地址，先验签再取归档，固定版本与目标必须匹配。该工具不建立正式公钥身份或分发渠道，不代表公开发行。

CI 的 macOS/Linux 测试任务在常规检查后另调用 `scripts/test-terminal-state.sh`，用私有临时证据目录构建独立 VT analyzer 并执行终端语义专项；普通 `go test ./...` 缺少 analyzer 时的跳过不计为该专项通过。此入口只运行 `test/integration/terminalstate`，保留原有测试与超时，不重复整套或依赖生成。独立 analyzer 的依赖留在自身 module，不进入产品依赖。入口的本地验证与远端 CI 实际执行分开记录，见 [终端 CI 验收](15-implementation-status.md#evidence-terminal-ci-validation)。

CI 的四目标编译任务使用固定 `v0.0.0-ci` 执行既有 `make release`：`build.sh` 首先保留全包 `go build ./...`，再构建六程序及双插件清单，宿主工具生成目标归档。明确清除 `SHIKI_SIGNING_KEY`，检查归档 SHA-256 sidecar 和归档内 `SHA256SUMS` 的 pin；只从归档读取清单，不解包或运行目标程序，也不上传、签名或发布。原生 macOS/Linux 测试任务仍显式测试和 vet 两个 `go:build ignore` 的 release 工具；新增入口补充的是实际构建到打包的四目标集成。固定 CI 版本与摘要仅用于本地完整性验证，不建立正式版本/签名身份；本地 Linux arm64 交叉打包及远端 Ubuntu CI 执行分别记录，见 [打包 CI 验收](15-implementation-status.md#evidence-release-ci-validation)。

依赖扫描分别覆盖根 module 和独立终端 analyzer module；根目录的 `./...` 不包含嵌套 module。CI 保持原根扫描，另在 analyzer 目录以固定 `govulncheck v1.1.4` 执行 package 级扫描。记录数据库日期、实际目标及已导入包与仅模块公告的区别；静态目标扫描不作为目标平台运行或无漏洞证明。该入口的本地证据与远端 CI 执行分开记录，见 [analyzer 依赖扫描](15-implementation-status.md#evidence-terminal-analyzer-security-validation)。

Go module 路径为 `github.com/DDSRem-Dev/Shikigami`，正式发行尚未发布；旧设计中的 `github.com/yourusername/shikigami` 为占位符。Connect-Go 基线为 connectrpc.com/connect/v2 v2.0.2，库最低要求 Go ≥ 1.26.0；项目因 tsnet v1.98.10 将最低版本固定为 1.26.5，使用 1.26.10 工具链；protoc-gen-go 基线 v1.36.11。仅插件适配层保留 go-plugin v1.8.0、grpc-go v1.85.0 和 protoc-gen-go-grpc v1.6.2；CoreAPI 回调用 Connect。运行库与 Connect 生成器锁定同版，protoc 也固定版本（见 09）。避免 @latest 使结果不确定。单文件内嵌构建按平台生成插件，开发构建不依赖该目录。

日志使用统一结构化接口，context 私有类型键；错误映射统一 Connect 错误码。测试文件与模块邻近，跨进程集成放 test/；关键恢复/安全测试属于对应阶段交付，不推迟到发布前补做。

CI第三方Action按用户选择使用常规稳定版本标签：`actions/checkout@v7.0.1`、`actions/setup-go@v7.0.0`，为2026-10-29查询的官方latest Release。两者使用Node24，现有GitHub托管Ubuntu/macOS任务参数已核对；不以Action升级或静态检查代替远端CI执行结果。
