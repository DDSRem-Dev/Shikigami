# 开发路线图

**状态：所有实现任务尚未完成。** 本文是排期与阶段范围的唯一来源。原 14 周是未经验证的粗估，新增本地 agent 后不能保留为交付承诺；以验收门槛推进，完成 Phase 0 的原型后再估算工期。05/12 的完整体验不代表 MVP 已具备插件或 GUI。

## 阶段概览

| 阶段 | 交付范围 | 依赖 |
|---|---|---|
| Phase 0 | 工程、协议、本地 IPC/agent 设计和高风险原型 | 已确认范围 |
| Phase 1 | CLI + agent + daemon、TLS 直连、Project/Session、tmux 终端 | Phase 0 |
| Phase 2 | 内嵌 tsnet、双端插件、基础双向文件同步 | 核心生命周期稳定 |
| Phase 3 | TCP 转发、监控诊断、后台运行体验 | 插件路由和身份明确 |
| Phase 4 | TUI、配置快照、模板、多设备打磨、安装升级 | 关键故障场景验证 |
| Phase 5 | 首个正式版本验证与发布 | 全部首版门槛 |
| 后续 | GUI、WSS、文件快照等独立迭代 | 首版稳定及再次评审 |

## Phase 0：初始化和可行性验证

- [ ] 建立 go.mod、源码目录、LICENSE、Makefile、CI，确认实际 module/release 地址。
- [ ] 将 09 的三个 protobuf 块和 13 的 agent.proto 落成文件，锁定 protoc/生成器版本，生成后 go build。
- [ ] 按 09/13 已定义的 IPC、CoreAPI/worker、路由和能力协商生成真实 stub，验证 UID、代理白名单、旧 instance 拒绝和取消；不能停留在 Go interface。
- [ ] 验证 14 的 Connect-Go v2 生成、注册/客户端、HTTP/2 over UDS/tsnet/TLS 四种 RPC 形态、HTTP/1 拒绝、下游错误重建、超限/取消/半关闭/最终状态及协议准入；不以 unary 成功代替双向流验证。
- [ ] 定义状态 schema、Token 撤销表、request_id 幂等与配置迁移。
- [ ] 终端原型：普通 tmux attach/PTY、多观察者屏幕初始化、resize、退出回收。
- [ ] 服务保活原型：daemon crash/restart 不杀 tmux，插件不留孤儿。
- [ ] 插件原型：真实 go-plugin gRPC 子进程 Init/Start/Stop + 独立 Connect CoreAPI 回调；验证 AutoMTLS、实例凭据、context 生命周期、跨协议错误/半关闭适配和失败回收。
- [ ] 完成 11 的 tsnet 登录/一次性 key/身份持久化/认证失败/NodeControl 原型，记录 SDK/OS 版本与脱敏结果。
- [ ] 验证 06 的 agent 崩溃恢复、四层版本边界及多设备条件提交/控制权竞争。
- [ ] 根据原型结果重估工期，登记不能满足的假设。

验收：协议可编译，关键进程生命周期实测，接口没有未指定的跨进程指针传递。

## Phase 1：CLI 核心闭环

- [ ] 本地 agent 单实例、受限 IPC、状态/凭据管理，CLI 自动发现/启动。
- [ ] daemon 普通用户运行、完整 unary/stream 认证、Token 创建/撤销、显式权限表。
- [ ] 先建立 Project 模型及持久化，再实现依赖它的 Session 创建，避免先实现无项目会话再返工。
- [ ] Connect/protobuf over HTTP/2 TLS 直连测试通道，证书验证、取消、deadline、重连；公网不可明文。
- [ ] 终端 TerminalStream 首帧/控制权、PTY、屏幕重绘、只读观察者、慢消费者处理。
- [ ] 创建/删除幂等、停止/重启 Claude、断线保活、状态恢复和错误提示。
- [ ] CLI connect/attach/@/sessions/agent、目录映射、基本菜单。

验收：两窗口通过一个 agent 使用同一项目；断网重连恢复当前屏幕，无输入重放；删除会话不改文件；观察者控制操作被拒绝；Token 撤销关闭流。

该阶段使用开发产物和受控 TLS 测试环境。尚不提供一键 tsnet 安装、自动同步或端口转发，不把未发布 Homebrew/download URL 作为验收命令。

## Phase 2：tsnet 与文件同步

- [ ] agent/daemon 内嵌 tsnet 的首次登录、状态持久化、节点名和 P2P/DERP 状态展示。
- [ ] 直连 TLS 显式回退，认证失败不换路线绕过；同一用户多个 CLI 不重复创建节点。
- [ ] 插件 manifest/version/side、CoreAPI 能力 scope、加载/失败清理/重启、两端管理命令。
- [ ] 项目映射级 sync，共同基线/hash/revision、首次同步计划、冲突积压、删除冲突。
- [ ] 分块上传下载、清单分页、受根限制文件访问、原子替换、冲突副本/恢复。
- [ ] watcher 递归/溢出/重扫、ignore、后台运行、pause/resume、无需前台也保留冲突。

验收：双方修改、修改与删除、断线补齐、权限错误、symlink 逃逸、大文件限制、多窗口共享 watcher 均验证。首版先完整文件分块传输；增量 diff/三方自动合并后续优化，不能与安全冲突检测混为一件事。

## Phase 3：端口转发与诊断

- [ ] 服务端发现 TCP endpoint、进程归属、保护端口及允许规则。
- [ ] 本地 agent 持有 loopback listener，冲突重新绑定与实际地址展示。
- [ ] ForwardPort 握手、半关闭、双向取消、限额、统计和孤儿连接清理。
- [ ] 明确手动/自动转发策略；关闭所有 CLI 后用户已启用的监听继续。
- [ ] 系统/会话资源 RPC、插件状态、网络诊断与 stats/doctor/logs。
- [ ] Claude 进程退出检测、可选有界自动重启；不靠“无输出”自动杀进程。

验收：TCP EOF 后仍收到完整响应，断线不泄漏连接，默认无 LAN 暴露，未知进程端口不自动转发，扫描失败不清空所有映射。

## Phase 4：首版体验和运维

- [ ] TUI Dashboard、非交互模式、友好错误、通知不污染终端字节流。
- [ ] 会话配置快照与模板；恢复新会话且不回滚文件或对话。
- [ ] 多设备路径映射、显式终端控制权接管、后台同步冲突展示。
- [ ] 按 11 实现安装器、Linux systemd 服务、macOS LaunchAgent/Linux 用户服务。
- [ ] 重复安装/升级/回滚、自定义 config、非交互登录、依赖检查和卸载保留数据。
- [ ] 文档和命令帮助与 capability 保持一致。

## Phase 5：正式发布

- [ ] macOS Intel/ARM、Linux、WSL 客户端与 Linux daemon 兼容性验证。
- [ ] 长时间后台运行、资源泄漏、故障注入、安全边界与性能回归。
- [ ] 代码审查、依赖扫描、关键恢复/认证/数据操作分支测试；覆盖率数字不能替代行为验收。
- [ ] 产物签名/校验、版本固定、插件匹配、真实安装源和 Homebrew 等分发渠道。
- [ ] 发布说明、升级限制、故障排除、许可证、真实支持入口。
- [ ] 经验证后才能把“设计目标”改成“已支持”，不得预先勾选完成。

## 后续迭代

1. GUI：Wails + React/TypeScript + xterm.js，通过本地 agent；版本、平台打包、托盘和通知需专门验证。见 12，不计入首版工期。
2. 企业代理/WSS 隧道：专用双端适配和代理测试，不能以 Connect-Web/gRPC-Web wrapper 冒充原生双向流。
3. 同步增量优化、三方合并、独立目录/worktree 文件快照与恢复。
4. 终端历史录制/回放、Claude 官方对话恢复集成。
5. 团队多租户、访客沙箱、插件市场、云配置等另行设计安全边界。

后续功能无既定发布日期，不因 UI 草图或协议预留就承诺已可用。
