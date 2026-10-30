# 原生完整产品平台验收

## 当前状态（2026-10-31）

四目标完整产品及实际WSL2验收已接入现有CI。首轮[41090262712](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41090262712)绑定原bf2e645；按用户要求改提交格式后对应c83c95a，git tree相同但不改写历史CI来源。第二轮[41093552523](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41093552523)绑定42eb93e。两轮已有失败，不能认定平台完成；Phase5复合项保持未勾选，路线图36/42。公开Homebrew四目标渠道验收见22，不能替代本矩阵。

| 环境 | 本批要求执行的范围 | 已取得结果（01:30 +08） |
|---|---|---|
| ubuntu-24.04 / amd64 | 完整race、真实双端插件、E2E、内嵌插件、终端语义、原生systemd及用户服务、活动升级/回滚 | 首轮race通过，E2E的TestCLIThreeProcessTLSLifecycle失败，后续未运行；第二轮race的obsolete-selection用例失败 |
| ubuntu-24.04-arm / arm64 | 同上，实际arm64内核/Go宿主 | 两轮产品/E2E/embedded/terminal通过，systemd维护失败 |
| macos-15 / arm64 | 完整产品/终端、实际LaunchAgent、上传下载活动升级 | 首轮race多个session/tmux/integration失败，第二轮尚未完成 |
| macos-15-intel / amd64 | 同上，拒绝Rosetta替代 | 两轮尚未完成 |
| windows-2025的WSL2 / amd64 | 独立Ubuntu、真实Microsoft内核、Linux文件系统、systemd PID1及产品/维护矩阵 | 首轮实际WSL身份/产品/E2E/embedded/terminal通过，systemd维护失败，专用发行版清理通过；第二轮agent与sync client各一项race失败 |

首轮四Homebrew及四unsigned构建成功不代表五个完整平台成功。旧摘要未保留原始Output，缺确切断言行的失败仍待诊断，不能凭耗时猜测超时。完全重复源的41093194046/41093197337取消请求已接受、随后不可查询，不记为验证通过或已核实的cancelled结论。

## 可复验约束

`platform-ci.py`在测试前记录git提交、全部受跟踪文件摘要及实际OS/架构/Go宿主身份，结束时再次核对。完整产品包括普通用户的build/lint、工具测试、全包race、真实跨进程插件、三宿主TLS/E2E和安装就绪场景；嵌入插件、终端及LaunchAgent必须出现具名PASS，跳过或仅wrapper退出0不足以通过。

Linux维护仅允许GitHub托管的可丢弃runner，使用新建普通用户和独立目录；复用既有真实systemd验收场景，不在Docker内冒充原生。系统级Shiki服务存在时拒绝执行，清理前核对本轮路径、UID、进程starttime和服务ExecStart；精确停止、删除本轮账户/组/linger/服务，不操作其他账户或用户Mac。旧Docker入口保留原默认布局与PID1检查。

WSL独立发行版以随机名称导入，只有该发行版被terminate/unregister，不调用全局shutdown。官方Ubuntu24.04.5镜像和Go1.26.10固定摘要来自官方发布清单，执行时实际下载校验；本地未下载完整包，不宣称已验证其包体。宿主不支持WSL2或需要重启时明确失败，不以普通Linux虚拟机代替。

仅上传固定公开字段和测试名称/结果；原始日志、配置、Token、认证URL及运行状态保持私有。所有必需阶段、最终源一致性及清理均成功才接受该平台。外部账号、真实Claude、物理重启/重新登录和自然网络切换仍是独立门槛。

## 本地检查与范围

最终17项Python工具检查、6份变更Python语法、actionlint及diff检查通过；覆盖身份/来源拒绝、安全证据过滤、具名PASS、超时TERM后清理、服务卸载后残留、资源探测和缓存预算。未在用户Mac执行原生安装、Homebrew操作或修改系统服务；c4双平台12小时与预约auth key到期继续原冻结源码。

## CI耗时优化及验证边界

旧配置原生矩阵max-parallel=2，Mac首轮排队约26分钟；首轮Linux amd64 build/lint236秒、全包race1023秒、E2E168秒，arm64相应297/1132/186秒。原生arm64维护首轮322.787秒、第二轮273.994秒失败，WSL首轮286.077秒失败；这些不是已证明的编译超时。

本轮将四原生任务并发槽增至4；预检按CPU亲和性、内存及当前cgroup-v2/可见祖先限制选择包并发：至少2核且6GiB才用p2，否则p1，探测失败也p1，GOMAXPROCS限制1–2。缓存来自runner既有GOCACHE，按目录与1MiB分块协作检查30秒/4GiB预算，完整条目复制到新UID自有私有目录；不共享可写源、不更改原缓存/HOME权限，部分复制可重建缺项，未完成条目删除。30秒不是阻塞文件系统操作硬deadline。

所有count=1、race、平台/维护阶段、具名PASS与清理要求保持。systemd失败时finally也导出安全固定结果；Go失败只公开受跟踪已知测试文件basename、合法行号、固定错误类别及有界elapsed，不含原始Output、Token或URL。独立七类审查通过。实际wallclock收益与剩余失败须由新CI确认，不能只凭调整参数宣称提速完成。

第二轮agent obsolete-selection失败已在source8复现：maintenance可先建立本地授权冷却，foreground Resolve合法返回Unavailable。测试现接受此现有竞态，同时加强auth_required/NODE_AUTH_REQUIRED、仅tsnet、无旧RemoteProtocol断言；20次race及7项相关回归通过，生产未改。WSL同步插件失败本地未复现，保持原断言等待安全行号。
