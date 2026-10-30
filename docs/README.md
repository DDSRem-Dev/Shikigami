# Shikigami 设计文档导航

**状态：当前公开预发布 v0.0.1-alpha.2 已通过 tag 全自动构建、签名、发布并更新 Homebrew Formula；Phase 0–5 尚未完整交付。** 三宿主命令、终端闭环、双端同步/端口插件、监控、配置快照、TUI 与诊断已有实现及本地测试；安装事务和产物工具已有隔离验收，原生macOS LaunchAgent及Linux容器真实systemd生命周期已通过；官方tsnet登录、进程身份保持与真实P2P/DERP业务流已通过；四目标原生Homebrew和Mac/VPS双客户端/关键恢复验收亦已通过，路线图36/42；完整认证生命周期、主机重启登录、Claude与完整产品平台/长稳仍未完成。逐项进度见 [08 路线图](./08-roadmap.md)，证据与待完成门槛见 [15 实施记录](./15-implementation-status.md)。00–14 保留设计契约，不能仅凭示例认定功能已经可用。

## 文档清单

共 15 份设计文档（00–14）、实施记录（15）、一致性检查（16）、原型复盘（17）、发布准备（18）、本地验收收敛（19）、资源审查（20）、密钥轮换（21）、Homebrew分发（22）、外部tsnet验收（23）、公开包升级兼容（24）、双主机验收（25）及本索引。

| 文档 | 主要职责 |
|---|---|
| [00 项目概览](./00-project-overview.md) | 目标、范围、已确认决策 |
| [01 系统架构](./01-architecture.md) | 组件边界、数据流、进程归属 |
| [02 连接模型](./02-connection-model.md) | tsnet、TLS、认证、重连 |
| [03 会话管理](./03-session-management.md) | 项目、会话、控制权、持久化、配置快照 |
| [04 插件系统](./04-plugin-system.md) | 进程间桥接、双端插件、生命周期 |
| [05 用户体验](./05-user-experience.md) | 目标交互、命令和错误提示 |
| [06 边界情况](./06-edge-cases.md) | 异常处理与验收条件 |
| [07 项目结构](./07-project-structure.md) | 拟建目录、依赖方向、构建约定 |
| [08 开发路线图](./08-roadmap.md) | 交付顺序与阶段门槛，唯一排期来源 |
| [09 协议定义](./09-protocol-definition.md) | 远端 RPC、版本协商与完整内置插件协议 |
| [10 实现约束](./10-implementation-details.md) | 终端、同步、转发、认证的实现要求 |
| [11 自动化安装](./11-automated-installation.md) | 安装、引导登录、升级和卸载 |
| [12 客户端界面](./12-client-interface-design.md) | CLI/TUI 和后续 GUI 的展示设计 |
| [13 本地 IPC 协议](./13-local-ipc-protocol.md) | Unix socket、Agent/NodeControl、代理路由、前端生命周期 |
| [14 ConnectRPC 传输契约](./14-connectrpc-transport.md) | Connect-Go v2、HTTP/2、流式语义、错误与迁移验收 |

原型复盘与条件估算：[17 原型复盘](./17-prototype-reassessment.md)。公开预发布、自动发布流程与交付门槛：[18 发布准备](./18-release-readiness.md)。

当前候选的证据矩阵与待验条件：[19 本地验收收敛](./19-local-acceptance-closure.md)。完整长跑记录分析：[20 资源审查](./20-long-run-resource-review.md)。隔离签名演练：[21 密钥轮换与撤信](./21-release-key-rotation.md)。Homebrew渠道：[22 分发与安装](./22-homebrew-distribution.md)。官方网络与认证矩阵：[23 外部tsnet验收](./23-tsnet-external-acceptance.md)。

公开包升级、回滚与兼容证据：[24 公开包验收](24-public-package-compatibility.md)。双客户端与硬崩溃验收：[25 双主机验收](25-multihost-acceptance.md)。完整产品的原生平台及WSL矩阵：[26 原生平台验收](26-native-platform-acceptance.md)。

实施进展：[15 实施与验收记录](./15-implementation-status.md)。每批架构检查：[16 架构与文档一致性](./16-architecture-conformance.md)。

## 建议阅读顺序

快速了解：00 → 01 → 05。开始实现：00 → 01 → 07 → 08 → 09 → 13 → 14，再按模块阅读 02/03/04/10；测试设计同时参考 06。部署参考 11，界面参考 12。

## 已确认的设计基线

- 单用户 VPS；多个设备属于同一使用者。多租户、访客权限隔离后续另行设计。
- Go ≥ 1.26.5 + Connect-Go v2.0.2，Connect 协议/protobuf over HTTP/2；内嵌 tsnet 管理 Tailscale 节点，不安装系统 tailscaled。
- 本地 `shiki-agent` 常驻，CLI/TUI 和未来 GUI 通过受限本地 IPC 共用连接、同步和端口转发。
- 远端 `shiki-daemon` 以指定普通用户运行；tmux 中的 Claude Code 与网络连接生命周期分离。
- 首版 CLI/TUI，GUI 后续交付；快照只恢复会话配置，不恢复文件、进程或对话历史。
- 文件同步和端口转发由双端 go-plugin 插件执行；宿主→插件保留原生 gRPC，CoreAPI 回调用 Connect。远端和本地 IPC 均不保留 gRPC 入口；核心负责认证、路由和生命周期。

## 统一约定

| 项目 | 约定 |
|---|---|
| 身份 | 一个 daemon 对应一个 OS 用户；Token 不得切换 OS 用户 |
| 资源 ID | `proj_<随机ID>`、`sess_<随机ID>`；不可变，不编码显示名 |
| tmux | socket 名 `shiki-<uid>`，会话名 `shiki-<session_id>`；所有操作使用同一 socket |
| 命令 | `shiki-daemon token …`；`shiki @ <session>`（有空格）；插件名 `sync` / `port` |
| 客户端配置 | `~/.shiki/config.yaml`，0600；本地路径映射仅存在客户端 |
| 本地后台 | `~/.shiki/run/agent.sock`（Unix，0600），状态目录 0700 |
| 服务端配置 | `/etc/shiki/daemon.yaml`；包含运行用户和显式状态目录 |
| 服务端状态 | `/var/lib/shiki/`，由运行用户所有，0700 |
| 插件目录 | 本地 `~/.shiki/plugins/`；远端 `/var/lib/shiki/plugins/` |
| 端口 | tsnet 内 ConnectRPC 9527；公网监听默认关闭，启用必须配置 TLS |
| 重连 | 最多 5 次，1 秒起、指数退避、上限 30 秒，带抖动且可取消 |
| 同步 | 项目级，每路径保存共同基线；冲突暂停该文件，禁止静默覆盖 |
| 转发 | TCP、默认仅本地 `127.0.0.1`；发现端口不等于授权转发 |

09 是远端与插件消息形状的唯一来源，13 是本地 agent IPC 的来源；02/03/04/10 规定行为。修改接口时必须同步相关行为、UX 和验收。09 的代码块可用于协议语法验证，但未表示服务已经实现。待实现能力必须列在路线图，不能通过界面示例宣称完成。

实际 module 为 `github.com/DDSRem-Dev/Shikigami`，当前根目录采用[GPL-3.0-only](../LICENSE)，从v0.0.1-alpha.2起随发行包分发；已公开alpha.1及其冻结源码保留原MIT许可证。当前发行源为 [v0.0.1-alpha.2 GitHub Release](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-alpha.2)，公开问题反馈入口为 [GitHub Issues](https://github.com/DDSRem-Dev/Shikigami/issues)，不承诺响应时限；来源及下载验证结果见[公开发行证据](./15-implementation-status.md#evidence-public-alpha2-release-20261030)。文档中的 `yourusername`、`shiki.dev`、其余示例版本/下载地址、未关联22的 Homebrew 示例包名和邮箱仍是占位示例；真实 Homebrew 安装命令和平台边界见[22 分发与安装](22-homebrew-distribution.md)，其余占位地址不得当作实际安装源。
