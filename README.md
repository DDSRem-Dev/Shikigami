# Shikigami（式神）

通过本地 CLI/TUI 和常驻 agent，使用远端 VPS 上的 tmux / Claude Code。核心链路采用 Connect/protobuf over HTTP/2；文件同步和 TCP 转发由双端插件执行。

**当前源码的 Phase 0–5 验收清单已闭环（42/42）。** 功能、已验平台、证据来源及未支持范围见[支持声明](docs/18-release-readiness.md#current-support)，逐项进度见[路线图](docs/08-roadmap.md)。认证生命周期及长稳、资源与性能按用户确认测试通过关闭；真实 Claude 登录/推理、物理主机重启和生产 heap/object 归因仍保留未实测边界。当前公开安装入口为 alpha.6，集中修复安装、配对和远程浏览器使用反馈，包含 VPS/Mac 一键安装向导及地区环境与远程浏览器插件；不是 beta 或稳定版。

alpha.6 固定源码的发布前 CI 为 57/57；发行工作流完成 60 项成功、1 项条件跳过（不计通过），自动构建、签名、公开发布和 Homebrew 更新成功。23 个公开资产、四目标签名与产物来源、自动公式已独立核验，见[发行证据](docs/15-implementation-status.md#evidence-public-alpha6-release-20261117)。两端已完成签名升级，部署后CLI采样和浏览器交互验证分别记录；真实账号授权与物理重启仍未实测，见[发行说明](docs/releases/v0.0.1-alpha.6.md)。[alpha.3 历史发行证据](docs/15-implementation-status.md#evidence-public-alpha3-release-20261115)保留，不扩大为新向导验收。

alpha.6 发布后的验收又发现一处浏览器快速重连清理竞态；追加修补尚未进入已发布的 alpha.6，源码与后续版本状态见[问题记录](docs/29-alpha6-installation-followup.md)。

## 安装

VPS 和 Mac 各执行一条命令，向导处理依赖、服务、登录和插件配置：

```bash
# VPS（Debian 12 / Ubuntu 24.04）
curl --proto '=https' --proto-redir '=https' -fsSL https://github.com/DDSRem-Dev/Shikigami/releases/download/v0.0.1-alpha.6/bootstrap.sh | sudo bash -s -- --quickstart --version=v0.0.1-alpha.6

# Mac（当前登录用户，不加 sudo）
curl --proto '=https' --proto-redir '=https' -fsSL https://github.com/DDSRem-Dev/Shikigami/releases/download/v0.0.1-alpha.6/bootstrap.sh | bash -s -- --quickstart --role=client --version=v0.0.1-alpha.6
```

按提示确认系统变更、完成账号授权，并将 VPS 显示的地址和 Token 填入 Mac 向导。默认配置全部插件；仅需终端、同步和端口转发时，两端均追加 `--components=core`。已有受管客户端可运行 `shiki setup`。重复同一命令可继续引导，详细行为、插件说明和恢复方式见[一键安装](docs/28-quickstart.md)。

当前公开预发布为 [v0.0.1-alpha.6](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-alpha.6)。也可通过 Homebrew 安装命令和完整 bundle：

```bash
brew tap ddsrem-dev/shikigami https://github.com/DDSRem-Dev/Shikigami
brew install ddsrem-dev/shikigami/shikigami
```

后续版本由推送 tag 自动完成构建、签名、Release 和 Formula 更新；无需手改公式版本或校验值。Homebrew 只安装命令和完整 bundle，服务安装、升级与账号登录需显式操作，见 [Homebrew 使用与平台限制](docs/22-homebrew-distribution.md)。

## 开发

需要 Go ≥ 1.26.5；项目工具链固定为 Go 1.26.11，以满足 tsnet SDK 的最低补丁版本。真实终端测试需要 `tmux`；`make test` 中的验收采集器回归需要 Python 3.10+，并须以普通用户运行。查看器行为测试需要 Node.js 22+。源码中已经包含生成的 protobuf/Connect 代码，普通构建不需要 protoc。

```bash
make build
make test
make lint
```

`make build` 输出 `bin/shiki`、`bin/shiki-agent`、`bin/shiki-daemon`、短时运维 helper `bin/shiki-install`，以及当前平台的内置插件和对应 manifest（macOS：sync/port/browser；Linux 另含 region 和两种系统支持程序）。这是开发构建；固定版本 bundle 的打包、验签及安装工具见 [工具说明](docs/18-release-readiness.md)，公开归档见 [GitHub Releases](https://github.com/DDSRem-Dev/Shikigami/releases)。`make test` 包含 race detector 和本机 tmux 测试；缺少 tmux 时对应测试会报告跳过，不能据此声称完成终端验证。

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
- `plugins/sync/`、`plugins/port/`：双端独立插件入口和业务实现；`plugins/region/` 提供 Linux 地区插件与受限 helper，`plugins/browser/` 提供远程浏览器及本地查看器，Linux runtime 由独立用户服务持有。
- `internal/node/`：内嵌 tsnet 与本地 NodeControl；真实本地控制面及官方网络认证生命周期测试已通过，平台和资源边界见文档。

设计文档入口：[docs/README.md](docs/README.md)。代码仓库：[DDSRem-Dev/Shikigami](https://github.com/DDSRem-Dev/Shikigami)。公开版本、自动发布流程与安装方式见 [发布准备](docs/18-release-readiness.md)。

## 许可证

Copyright (c) 2026 Shikigami contributors。

当前项目采用 **GNU General Public License version 3 only（SPDX: GPL-3.0-only）**，完整正文见 [LICENSE](LICENSE)。软件按许可证条款提供，不附带任何担保。第三方依赖保留各自许可证。

从 `v0.0.1-alpha.2` 起的本项目发行采用 GPL-3.0-only；已发布的 `v0.0.1-alpha.1` 及其冻结源码保留当时的 MIT 许可证，历史归档不重新标注或覆盖。
