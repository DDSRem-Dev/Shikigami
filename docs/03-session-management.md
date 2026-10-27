# 会话管理设计

## 项目、会话和本地映射

Project 表示一个远端工作根目录，包含 ID、名称、规范化路径、创建时间和元数据。Session 属于一个 Project，工作目录只能是其根或子目录。各设备的本地路径、默认会话、子目录提示保存在本地 agent，不能写成远端 Project 的唯一 local_path。

CreateProject 校验路径与运行用户权限，存在的目录先确认关联，不清空内容；不存在则在允许的项目基目录内创建。项目根不允许互相嵌套造成同一文件被两个项目独立管理。

多个 Session 默认共享项目文件；并行 Claude 可能编辑同一文件。终端控制权只协调同一会话的输入，不提供跨会话文件隔离。需要独立工作副本时，用户创建独立目录/项目；自动 worktree 不在首版范围。

## 身份、命名与状态

- Project ID：`proj_<随机ID>`；Session ID：`sess_<随机ID>`，使用足够熵并在持久化层检查碰撞。
- 名称是可读标签：项目名在该 daemon 内唯一，会话名在所属项目内唯一；跨项目按名称连接时有歧义必须选择或使用 ID。
- tmux session 为 `shiki-<session_id>`，socket 名 `shiki-<uid>`（`-L` 接受名称，完整 socket 路径用 `-S`）。名称不可由未校验用户字符串拼接执行命令。
- Session 持久字段：id/project_id/name/working_dir/description/tags/metadata、创建时间、最后用户活动时间、期望 Claude 启动配置。
- 运行字段：attachment、stream、PTY、锁、取消函数、当前 PID、资源采样；不序列化这些对象。PID 仅作提示，不是重启后进程身份凭证。

生命周期：`creating → ready → deleting → deleted`，失败为 `failed`。Claude 状态独立为 `starting/running/stopped/crashed/unknown`。连接数派生 Idle/Active；detach 不把运行中的 Claude 标成 idle/stopped。

## 创建与删除

创建顺序：

1. 认证授权；校验 request_id、Project、名称唯一性、目录归属、资源配额。空 working_dir 使用远端项目根。
2. 在互斥事务中预留 ID/名称/配额并持久化 `creating` 与请求去重记录。
3. 使用同一 tmux socket 创建 session/pane；按 `auto_start_claude` 启动配置的 `claude` 可执行文件。该字段省略默认 true，显式 false 保留空 shell。
4. 配置 pane 退出状态保留，观测启动/退出，不能通过屏幕提示文本声称 Claude 已“语义就绪”。
5. 持久化 ready 后返回。任何一步失败需记录状态、回滚已创建资源并释放预留；不能忽略持久化错误后返回成功。

同一 request_id 的重试返回同一资源或同一失败结果；同 ID 不同参数拒绝。不能无条件重试创建。

删除只结束目标会话的已验证进程/PTY/tmux session 和配置记录，**不删除项目文件**。先标 deleting、停止新 attachment、取消已有流，再结束受控资源并完成持久化。不能对磁盘里旧 PID 无条件 SIGKILL；要核对 pane、进程启动标识与所属会话。

Project 删除要求没有会话或配置快照，否则返回 FailedPrecondition，不删除工作目录。文件删除使用独立文件操作；递归清理项目根是未来独立破坏性操作，不混入 DeleteSession/DeleteProject。

## 终端连接与多客户端

`TerminalStream` 是唯一 attach + 输入 + 输出的双向 RPC，握手见 [09](./09-protocol-definition.md)。每个前端创建一个 attachment，agent 负责桥接；server/session/attachment ID 不得串用。

- 第一位具备 `session.write` 且未请求只读的客户端成为控制者。其他 attachment 是观察者；只读 Token 即使第一个进入也不能成为控制者。
- 控制者独占输入、resize 和终端控制字符；观察者只接收屏幕与事件。
- 接管使用明确 RPC，要求 `session.control` 和目标 attachment 的可写资格。先检查目标存在，再在会话锁内原子转移，不能先降级旧控制者后才发现目标不存在。
- 控制者断开时清空控制权并通知剩余 attachment；具备资格的观察者可显式接管，不静默提升只读用户。
- 一个会话有一个 PTY 读取协程；每个 attachment 一个有界输出队列和唯一 Send 协程。多个 goroutine 不得同时 Send 同一 gRPC 流。
- RPC handler 等待流结束才返回，随后恰好一次 detach；不能启动 goroutine 后立即返回使流关闭。

首版不提供字节历史回放。重连/新观察者加入时由桥接层在会话锁内重新建立普通 tmux attach，并在当前控制者尺寸下向所有客户端发送 RESET 后转发 tmux 重绘输出。已有客户端也需重绘；tmux 中的 Claude 继续运行。实现必须验证 alternate screen、光标、颜色与尺寸，不用 capture-pane 文本冒充完整终端状态。高频加入需限流。若该策略在验收中无法可靠恢复，需先完成终端状态快照方案再交付多客户端，不能静默降级成残缺屏幕。

最后一个 attachment 离开时关闭 attach PTY、回收对应 attach 子进程；保留独立 tmux server/session。attach 子进程退出不等于 Claude 退出。重连恢复当前界面，不重放未确认的输入。

## 智能连接

```yaml
# 客户端配置片段
projects:
  /Users/rem/projects/myapp:
    server: my-vps
    project_id: proj_abc123
    default_session: main
    session_hints:
      frontend: frontend
      backend: backend
```

将 cwd 与映射根规范化后按目录边界匹配，取最长祖先根；再对相对路径按最长子目录提示匹配。不能使用 HasSuffix（frontend/src 会漏匹配）或字符串 HasPrefix（myapp2 会误匹配）。不存在映射进入设置流程。

优先级：显式 session ID/名称 → 子目录提示 → 默认会话 → 选择菜单。排序先按连接状态分组，再按最近用户活动降序，最后按 ID 稳定排序；比较器必须满足严格弱序。取消菜单不作为索引访问。

只有 GetSession 返回 NotFound 才移除失效映射；超时、无权限、服务器离线均保留映射。同步不是 attach 的强制前置条件；发生冲突时显示待处理状态，用户可以继续终端工作。

## 配置快照与模板

用户已确认首版只做**会话配置快照**：保存 project_id、源 session 标识/名称、相对工作目录、description/tags/允许的 metadata、auto_start_claude 和 schema_version。快照不包含项目文件内容、文件列表“版本”、进程内存、Token、环境密钥或 Claude 对话目录。

恢复先验证原 Project 仍存在且工作目录仍有效，再按快照配置和新名称创建新 Session。两个会话继续看到当前项目文件。源 Session 被删除不影响快照；原 Project 被删除时恢复失败，不能猜测路径或自动覆盖另一个项目。快照属于 Project，必须先单独删除其配置快照，再删除 Project。

```bash
shiki sessions snapshot main --description "前端会话配置"
shiki sessions snapshots
shiki sessions restore <snapshot-id> --name main-copy
# 提示：创建同配置的新会话；项目文件和 Claude 对话不会回滚。
```

模板按远端 project.path 解析相对目录，不拿客户端路径发送给服务器。模板的 auto_start 只控制启动 Claude，不自动依次 attach（attach 会阻塞）。批量创建报告逐项成功/失败，不默默跳过；完成后用户选择一个会话连接。

## Claude 生命周期

默认启动命令 `claude`；版本、安装方式和登录状态在服务端检查。本地无需安装 Claude。停止、重启分别对应明确 RPC，带会话授权，主动停止不记作崩溃。用户选择重启不保证恢复对话；若将来集成 Claude 官方恢复能力，需要独立能力检测和验收。

无输出可能是等待输入或长任务，不自动判为卡死。自动重启默认关闭；启用时限制次数和退避，记录退出原因。每次成功重启重新挂接监控，避免只监控第一个进程。

## 持久化与恢复

采用一个带 schema_version 的状态快照保存 Projects、Sessions、配置快照和请求去重结果，避免多个 YAML 更新中途失败破坏关联。单写者串行化：同目录唯一临时文件 → 写入 → fsync → 原子 rename → fsync 目录；保留上一个有效版本，所有错误上报。

会话列表按 project_id 派生，不独立维护可能不一致的 sessions 数组。启动时清空连接状态，再用 socket、tmux session/pane 标识及进程启动信息重建运行状态。

- 有记录但无 tmux：标 stopped/unknown，保留配置和快照，不静默删除。
- 有受管 tmux 但无记录：列为待认领孤儿，不默认杀死。
- 权限不足、tmux 命令失败与“确实不存在”分开处理。
- 状态损坏：保留损坏文件，尝试验证后的备份；无备份则进入诊断/只读模式，不覆盖默认配置启动。
- 主机重启只能恢复配置，不能声称恢复原进程。daemon 重启保活条件见 [11](./11-automated-installation.md)。
