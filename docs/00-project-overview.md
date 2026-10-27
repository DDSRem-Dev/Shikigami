# Shikigami（式神）— 项目概览

**项目目标：让用户在本地终端流畅使用 VPS 上运行的 Claude Code。**

当前只有设计文档，尚无代码实现。本文描述目标范围，不代表功能已经交付。

## 设计原则

1. 单用户优化：一个远端 daemon 服务一个 OS 用户，允许其多个设备和多个会话。
2. 自定义应用协议：以 gRPC/protobuf 定义行为；复用 tsnet、tmux、go-plugin，不自行实现 NAT 穿透或终端多路复用。
3. 核心负责连接、认证、终端和会话；文件同步、端口转发由插件实现。
4. 网络断开不结束远端工作；删除、冲突解决和进程重启都有明确的资源边界。

## 核心链路

```text
CLI / TUI / 后续 GUI
  → 本地 shiki-agent（本地 IPC）
  → 内嵌 tsnet（P2P 或 DERP）上的 gRPC
  → 远端 shiki-daemon
  → tmux 普通 attach + PTY
  → Claude Code（启动命令 claude）
```

agent 是本机唯一的连接、同步及端口转发管理者。关闭 CLI 窗口不会停止 agent 或远端会话；用户可以显式停止 agent。

## 范围与已确认决策

- 内嵌 tsnet，客户端 agent 与 daemon 各自注册节点并持久化节点身份，无需系统 tailscaled。
- 首版 CLI/TUI；Wails GUI 为后续版本，共享 agent 能力。
- Project 是远端目录；Session 属于 Project，可使用其子目录。多个会话默认共享项目文件，不提供工作副本隔离。
- 会话配置快照只保存配置并创建新会话，不回滚项目文件或 Claude 对话。
- 多设备是同一用户多端使用；不是团队协作或安全隔离。

## 技术栈

Go、gRPC/protobuf、Tailscale tsnet、HashiCorp go-plugin、tmux；CLI/TUI 使用 Cobra/Bubble Tea。版本在实现时锁定，GUI 框架版本后续确认。

## 功能边界

核心：本地后台管理、连接与重连、认证授权、项目/会话管理、终端字节流、tmux 生命周期、诊断。

插件：项目级双向文件同步、TCP 端口发现与转发。插件未安装或未启用时，远端终端仍可独立使用。

后续：GUI、WebSocket 隧道、文件级快照、会话录制、多人协作和插件市场。无时间与性能保证，交付依赖验收。

## 文档入口

完整清单见 [文档导航](./README.md)。架构见 [01](./01-architecture.md)，唯一开发顺序见 [08](./08-roadmap.md)。
