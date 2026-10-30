# Mac 与 VPS 双客户端验收

2026-10-30进行中；本页尚不表示整项通过。按08原条目验收多设备路径映射、终端接管、后台冲突及06§9/12的恢复边界；不新增或拆小原42个checkbox。

## 隔离环境与来源

A为原生macOS27.0.1 arm64、UID501；B为已有Debian VPS原生amd64上的新临时普通用户，独立HOME/配置/凭据/本地目录。B同时运行专用daemon，项目目录与B本地目录分开。daemon只监听VPS loopback TLS，A通过专用SSH TCP转发连接并验证localhost证书与应用Token；B直接访问同一个TLS服务。SSH仅调度与承载加密字节，不转发agent UDS或冒充本地UID；不复用既有tsnet身份、不改系统防火墙、不安装服务或系统软件。

初始六程序/双插件manifest固定于`c4c5a56fe6fe104f9cb94df5a6bb3b918412c1f5`构建，版本`v0.0.1-alpha.2-local.multihost.1`。双端测试使用其中CLI/agent/daemon及插件，短时installer不部署。源码690文件清单与两平台二进制hash独立记录；这是候选构建，不是已公开alpha.2。终端提示缺陷修复后，仅CLI替换为固定源码的`v0.0.1-alpha.2-local.multihost.2`，其余产品程序仍为上述c4产物；不得把新CLI结果追认为旧公开包或长期运行包含该修复。

[test/diagnostics/multihost](../test/diagnostics/multihost/)为显式验收工具，业务均经过真实CLI/agent/daemon RPC。配置所有可写路径须在各自0700根目录；只读sync.snapshot仅提取指定mapping/path的baseline/operation及staging摘要，拒绝凭据路径。helper按UID/PID/启动身份/可执行文件核验自己启动的进程，不停止共享tmux服务器。controller清理与操作系统账户/目录/SSH清理分别核验；前者成功不代表后者完成，也不代替所有历史worker退出检查。

使用明确标记的shell fixture验证终端，匹配`MULTIHOST_REPLY:`防止把输入回显误当实际远端执行。本批不验证真实Claude、tsnet P2P/DERP、物理主机重启、Intel Mac或WSL完整平台，不把VPS称为裸金属设备。

## 已遇到的问题与修正

| 轮次 | 实际结果 | 处理 |
|---|---|---|
| 1 | 12.40秒失败：连续terminal.open触发既有100ms重绘节流；两个helper正常清理 | 仅ResourceExhausted按150ms最多5次重试，其他错误不重试；不放宽产品限额 |
| 2 | 27.37秒失败：真实CLI显式接管成功后仍显示read-only observer；正常清理 | 修复产品接管模式提示，见下文 |
| 3 | 45.14秒失败：修复后的真实CLI观察/接管提示及Ctrl+Q均通过，Dashboard夹具过早发q，最终超时；正常清理 | 改为等待真实界面/目标面板/所需内容，再发送退出；不使用固定延迟猜测就绪 |
| 4 | 35.61秒失败：双端Sessions与终端阶段通过；CAS负例误用不支持的ctrl-x先被参数校验拒绝 | 改为契约允许的ctrl-]，继续使用旧revision验证Aborted；不更改产品错误优先级 |

每轮重新初始化专用状态并发行新的独立测试Token，保留失败结论；不在失败后重用已撤销凭据。原始临时证据位于本轮closeout目录，各轮events单独保存；最终清理后可用性以清理记录为准。

## 终端接管提示修复

原`internal/client.Terminal`在TakeControl返回成功后，仍把READY中的observer状态传入BeforeRaw，导致真实CLI模式提示错误。现在等待同一attachment有序输出中的CONTROL_CHANGED及新的control_revision，再报告controller；unary成功本身不直接放行输入。确认前输出最多64帧/1MiB，随后保持原顺序交给既有事件循环，输入代次、旧输入作废和当前TTY尺寸同步保持。消费后释放缓存引用；READY和控制确认超时回调在Stop失败时join，避免迟到回调取消健康终端。确认等待最多5秒且可取消，错误/EOF/崩溃/错序/重复READY/旧revision/溢出不进入raw。

真实PTY+Connect回归先红后绿，覆盖RPC早返回、延迟控制事件、前置输出顺序、提前输入不重放及窗口尺寸同步。最终客户端整包race17.257秒；CLI24.145秒、UI2.859秒、架构1.375秒race通过，相应vet通过。两平台CLI实际构建并部署；第三轮真实CLI已复验两种模式提示、退出0及原pane保持。完整双主机矩阵及最终CI仍需继续完成。

## 有限验收矩阵

尚待整轮实际结果确认：独立身份/映射、同机配置CAS与重叠拒绝、同/异路径条件提交和同名新建、A离线/B写入、修改与删除、相同内容采纳、第三次修改失效旧generation、预览与KEEP_LOCAL/REMOTE/BOTH、冲突副本碰撞保护、无前台冲突持久化、真实Sessions/Sync/Ports展示、同时接管唯一胜者及只读拒绝、重连新attachment、独立loopback转发、仅A撤权而B继续工作。

两类硬崩溃要求先观察远端真实部分临时文件与仍完整的公开目标、本地prepared记录/完整staging及旧baseline，再SIGKILL agent，确认旧host/worker退出、旧listener不可连接，B终端/TCP保持。新owner扫描恢复后核对完整内容、远端revision对应新baseline且无pending operation。缓冲可能使commit_state为possible，不强行称incomplete；不把该部分上传场景冒充提交后ACK丢失，后者沿用既有四场景syncack实测范围。
