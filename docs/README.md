# Shikigami 设计文档导航

**当前体验与优化：** 用户在正式alpha.9选择Smooth并暂停背景视频后反馈整体操作可用，但滚动低帧率仍未通过。后续源码保留noVNC1.7及Smooth默认；网络库升级候选因缺乏收益证据且模拟出现退化已暂缓，产品仍用原网络栈与Go1.26.11。见[实验结论与剩余门槛](15-implementation-status.md#evidence-tsnet104-deferred-20261122)。

**当前性能诊断批次（2026-11-23）：** 查看器新增本地屏幕数据速率、二进制传输块速率、最近采样最大到达间隔、最近数据年龄和本地发送缓冲区；指标明确不冒充远端 RTT、RFB 帧率或实际呈现帧率。24 项 viewer UI 回归及 browser client/server/runtime race 已通过；真实 alpha.9 当前路由为 `tailscale-relay`，doctor 的部分远端探测仍会超时，VPS 资源采样正常。诊断源码尚未发布或部署，真实滚动体验仍开放。

**此前严重卡顿的调查记录：** 用户确认5Mbps套餐；已完成分段与同页画质对照，源码已纳入noVNC1.7和Smooth默认，但尚未发布部署。最新体验以上方反馈为准，不能以此前短时功能通过代替滚动及长期性能通过。见[延迟归因与改进](15-implementation-status.md#evidence-viewer-memory-latency-batch-20261122)。

**alpha.9已发布（记录日期2026-11-22）：** 固定源`0b4847d`的[发布前CI](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44924752906)57/57通过，[发行流水线](https://github.com/DDSRem-Dev/Shikigami/actions/runs/44929424103)已成功，23资产于`2026-11-21T18:13:58Z`公开。现行下载入口为alpha.9，见[说明](releases/v0.0.1-alpha.9.md)与[实施记录](15-implementation-status.md)。用户两端已升级正式签名alpha.9，7插件ready、work为running/browse；原配置、Token/auth、密钥环、profile及daemon身份保持，见[两端升级证据](15-implementation-status.md#evidence-alpha9-upgrade-20261122)。12次CLI成功（0.180–0.211秒）仅为短时健康；新版真实viewer已完成自有页面点击/输入/滚动及5次显式重连回只读后接管；Balanced/Smooth各5次观测包含工具开销，不能严格对比旧夹具或证明卡顿已解决。用户人工验证后已进入claude.ai/login，助手截图复核正常登录表单；没有登录账号。本轮本机viewer、TaskSpace及10个临时文件已清理，远端Chrome登录页有意保留；这不承诺长期免挑战，见[alpha.9交互证据](15-implementation-status.md#evidence-alpha9-viewer-20261122)。历史alpha.8失败保留，beta门槛保持开放。

**本轮实机复验（2026-11-21）：** 默认预算下6次resolve（0.594–4.938秒）、端口目标拒绝恢复、浏览器输入/5次显式重连/接管/暂存文件功能通过；一次浏览器状态超时经手动Refresh恢复，临时转发后续端口重绑定原因未定。默认`/bin/sh`方向键、长稳、网络专项、Claude登录、物理重启仍开放。2026-11-19的alpha.7间歇超时、卡顿及keep-both部分完成历史证据保留，见[两端升级与实测](15-implementation-status.md#evidence-alpha8-upgrade-20261121)。

**beta 实测计划（2026-11-18）：** [30 用户实机测试手册](30-beta-user-acceptance.md)提供当前安装的详细命令、四插件检查、账号/网络恢复体验、助手分工和反馈模板；计划不等于测试通过或已经发布beta。

**alpha.6 后续修复：** alpha.7 纳入浏览器快速重连清理竞态和已有连接身份复核预算修补。安装反馈完整清单、连接故障证据、两端升级与开发文件清理状态见[29](29-alpha6-installation-followup.md)；公开发行与部署后验证分别记录。

**一键体验（2026-11-17）：** alpha.6 汇总安装输入、worker、配对和浏览器连接修复，延续 alpha.5 的控制终端、阶段进度和公共目录权限修复；VPS/Mac 命令、配置和插件说明见 [28 一键安装与首次使用](28-quickstart.md)，当前发行验证状态见[15](15-implementation-status.md)。公开发行与两端部署、账号和物理重启验收分别记录。

**状态：当前源码 Phase 0–5 验收清单已闭环（42/42）。** [当前支持声明](18-release-readiness.md#current-support)统一功能、平台、证据和未支持范围；[08 路线图](08-roadmap.md)、[15 实施记录](15-implementation-status.md)与[16 架构复核](16-architecture-conformance.md)记录关闭依据。认证生命周期和长稳、资源与性能按用户确认通过；真实 Claude 登录/推理、物理主机重启及生产 heap/object 归因仍保留未实测边界。当前公开安装入口为 v0.0.1-alpha.9，不是beta或稳定版。00–14保留设计契约，不能仅凭示例认定功能已通过所有平台验收。

历史alpha.7固定源`cbf3cdf`的发布前CI57/57通过；公开资产与签名验证见[alpha.7公开证据](15-implementation-status.md#evidence-public-alpha7-release-20261117)，两端部署另行记录。历史alpha.6固定源CI57/57通过，发行工作流60项成功、1项条件跳过（不计通过），自动发行及Homebrew更新完成；23公开资产、四目标验签/来源和公式的独立验证见[公开证据](15-implementation-status.md#evidence-public-alpha6-release-20261117)。[alpha.3历史证据](15-implementation-status.md#evidence-public-alpha3-release-20261115)、[beta候选证据](15-implementation-status.md#evidence-first-beta-candidate)及[beta发布门槛](18-release-readiness.md#beta-readiness)保留，不表示发布了beta。

**新增地区/浏览器扩展（2026-11-15）：** [27 地区环境与远程浏览器](27-region-browser-plugins.md)实现与授权范围内本地/隔离amd64容器验收已完成，并纳入公开alpha.3。新增功能不包含在历史42项或beta57项证据中；08/15/16单列精确源码、产物与发现/修复。新增原生体验、真实账号、物理重启及公网仍未验。

## 文档清单

共 15 份设计文档（00–14）、实施记录（15）、一致性检查（16）、原型复盘（17）、发布准备（18）、本地验收收敛（19）、资源审查（20）、密钥轮换（21）、Homebrew分发（22）、外部tsnet验收（23）、公开包升级兼容（24）、双主机验收（25）、原生平台验收（26）、地区/浏览器插件（27）、一键安装与首次使用（28）、alpha.6安装问题跟进（29）、beta用户实机测试（30）及本索引。

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

各候选的历史证据矩阵与验收边界：[19 本地验收收敛](./19-local-acceptance-closure.md)。完整长跑记录分析：[20 资源审查](./20-long-run-resource-review.md)。隔离签名演练：[21 密钥轮换与撤信](./21-release-key-rotation.md)。Homebrew渠道：[22 分发与安装](./22-homebrew-distribution.md)。官方网络与认证矩阵：[23 外部tsnet验收](./23-tsnet-external-acceptance.md)。

公开包升级、回滚与兼容证据：[24 公开包验收](24-public-package-compatibility.md)。双客户端与硬崩溃验收：[25 双主机验收](25-multihost-acceptance.md)。完整产品的原生平台及WSL矩阵：[26 原生平台验收](26-native-platform-acceptance.md)。

地区环境与远程 Chrome 的新增契约：[27 地区环境与远程浏览器插件](27-region-browser-plugins.md)。

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
| 命令 | `shiki-daemon token …`；`shiki @ <session>`（有空格）；插件名 `sync` / `port`，新增 `region` / `browser` 的范围与进度见27及08/15/16 |
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

实际 module 为 `github.com/DDSRem-Dev/Shikigami`，当前根目录采用[GPL-3.0-only](../LICENSE)，从v0.0.1-alpha.2起随发行包分发；已公开alpha.1及其冻结源码保留原MIT许可证。当前发行源为 [v0.0.1-alpha.9 GitHub Release](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-alpha.9)，公开问题反馈入口为 [GitHub Issues](https://github.com/DDSRem-Dev/Shikigami/issues)，不承诺响应时限；来源及下载验证结果见[公开发行及部署记录](./15-implementation-status.md)。文档中的 `yourusername`、`shiki.dev`、其余示例版本/下载地址、未关联22的Homebrew示例包名和邮箱仍是占位示例；真实Homebrew安装命令和平台边界见[22分发与安装](22-homebrew-distribution.md)，其余占位地址不得当作实际安装源。
