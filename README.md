# Shikigami（式神）

通过本地 CLI/TUI 和常驻 agent，使用远端 VPS 上的 tmux / Claude Code。核心链路采用 Connect/protobuf over HTTP/2；文件同步和 TCP 转发由双端插件执行。

**已提供公开 alpha，完整 Phase 0–5 尚未交付。** CLI、agent、daemon、真实 tmux 终端、双端同步/转发插件、监控、配置快照和 TUI 已有实现与本地验收。安装升级/回滚和内嵌产物已有隔离测试；原生macOS LaunchAgent和Linux容器真实systemd已通过限定生命周期验收，外部网络认证、原生VPS/重启登录持久化、完整平台与当前版本长期资源稳定仍待完成。证据见 [实施记录](docs/15-implementation-status.md) 和 [架构一致性检查](docs/16-architecture-conformance.md)，完整范围以 [路线图](docs/08-roadmap.md) 为准。

## 安装

当前公开预发布为 [v0.0.1-alpha.2](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-alpha.2)。通过 Homebrew 安装：

```bash
brew tap ddsrem-dev/shikigami https://github.com/DDSRem-Dev/Shikigami
brew install ddsrem-dev/shikigami/shikigami
```

后续版本由推送 tag 自动完成构建、签名、Release 和 Formula 更新；无需手改公式版本或校验值。Homebrew 只安装命令和完整 bundle，服务安装、升级与账号登录需显式操作，见 [Homebrew 使用与平台限制](docs/22-homebrew-distribution.md)。

## 开发

需要 Go ≥ 1.26.5；项目工具链固定为 Go 1.26.10，以满足 tsnet SDK 的最低补丁版本。真实终端测试需要 `tmux`；`make test` 中的验收采集器回归需要 Python 3.10+，并须以普通用户运行。源码中已经包含生成的 protobuf/Connect 代码，普通构建不需要 protoc。

```bash
make build
make test
make lint
```

`make build` 输出 `bin/shiki`、`bin/shiki-agent`、`bin/shiki-daemon`、短时运维 helper `bin/shiki-install`，以及两个插件和对应 manifest。这是开发构建；固定版本 bundle 的打包、验签及安装工具见 [工具说明](docs/18-release-readiness.md)，公开归档见 [GitHub Releases](https://github.com/DDSRem-Dev/Shikigami/releases)。`make test` 包含 race detector 和本机 tmux 测试；缺少 tmux 时对应测试会报告跳过，不能据此声称完成终端验证。

重新生成协议：

```bash
make tools
make generate-proto
```

生成器与 protoc 安装到仓库内忽略的 `.tools/`，版本和下载 SHA-256 固定；不会替换系统 protoc。详见 [构建约定](docs/07-project-structure.md)。

## 架构与实现

- `proto/`、`pkg/protocol/`：四份 schema 和生成绑定；仅插件通道保留原生 gRPC。
- `internal/connection/`：受信 UDS/TLS HTTP/2、UID 检查、协议准入、消息限额及取消。
- `internal/auth/`、`internal/server/storage/`：daemon 身份、JWT/撤销、事务快照和幂等记录。
- `internal/agent/`：单实例锁、本地握手、窗口归属、代理白名单和事件。
- `internal/server/`：项目/会话事务、认证 RPC、tmux/PTY、多观察者重绘及控制权。
- `pkg/config/`、`configs/`：严格 YAML 配置、迁移、条件保存与示例。
- `pkg/plugin/`、`internal/plugin/`：双协议插件 SDK 与宿主生命周期、路由。
- `plugins/sync/`、`plugins/port/`：独立插件入口和业务实现，分别以 `--side=client|server` 启动。
- `internal/node/`：内嵌 tsnet 与本地 NodeControl；真实本地控制面测试，外部部署认证待验收。

设计文档入口：[docs/README.md](docs/README.md)。代码仓库：[DDSRem-Dev/Shikigami](https://github.com/DDSRem-Dev/Shikigami)。公开版本、自动发布流程与安装方式见 [发布准备](docs/18-release-readiness.md)。

## 许可证

Copyright (c) 2026 Shikigami contributors。

当前项目采用 **GNU General Public License version 3 only（SPDX: GPL-3.0-only）**，完整正文见 [LICENSE](LICENSE)。软件按许可证条款提供，不附带任何担保。第三方依赖保留各自许可证。

从 `v0.0.1-alpha.2` 起的本项目发行采用 GPL-3.0-only；已发布的 `v0.0.1-alpha.1` 及其冻结源码保留当时的 MIT 许可证，历史归档不重新标注或覆盖。
