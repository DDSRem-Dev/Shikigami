# 原生完整产品平台验收

## 当前状态（2026-10-31）

四目标完整产品及实际 WSL2 验收已接入现有 CI，尚待首次实际执行。工具检查不能代替平台通过，Phase 5 平台复合条目保持未勾选，路线图仍为36/42。公开 Homebrew 四目标渠道验收见22，不能用来替代本矩阵。

| 环境 | 本批要求执行的范围 | 当前结果 |
|---|---|---|
| ubuntu-24.04 / amd64 | 完整race、真实双端插件、E2E、内嵌插件、终端语义、原生systemd及用户服务、活动升级/回滚 | 待CI |
| ubuntu-24.04-arm / arm64 | 同上；必须实际arm64内核和Go宿主 | 待CI |
| macos-15 / arm64 | 完整产品和终端验收、实际LaunchAgent生命周期、上传/下载活动升级 | 待CI |
| macos-15-intel / amd64 | 同上；拒绝Rosetta替代原生Intel | 待CI |
| windows-2025中的WSL2 / amd64 | 独立Ubuntu发行版、真实Microsoft内核、Linux文件系统、systemd PID1及相同产品/维护矩阵 | 待CI |

## 可复验约束

`platform-ci.py`在测试前记录git提交、全部受跟踪文件摘要及实际OS/架构/Go宿主身份，结束时再次核对。完整产品包括普通用户的build/lint、工具测试、全包race、真实跨进程插件、三宿主TLS/E2E和安装就绪场景；嵌入插件、终端及LaunchAgent必须出现具名PASS，跳过或仅wrapper退出0不足以通过。

Linux维护仅允许GitHub托管的可丢弃runner，使用新建普通用户和独立目录；复用既有真实systemd验收场景，不在Docker内冒充原生。系统级Shiki服务存在时拒绝执行，清理前核对本轮路径、UID、进程starttime和服务ExecStart；精确停止、删除本轮账户/组/linger/服务，不操作其他账户或用户Mac。旧Docker入口保留原默认布局与PID1检查。

WSL独立发行版以随机名称导入，只有该发行版被terminate/unregister，不调用全局shutdown。官方Ubuntu24.04.5镜像和Go1.26.10固定摘要来自官方发布清单，执行时实际下载校验；本地未下载完整包，不宣称已验证其包体。宿主不支持WSL2或需要重启时明确失败，不以普通Linux虚拟机代替。

仅上传固定公开字段和测试名称/结果；原始日志、配置、Token、认证URL及运行状态保持私有。所有必需阶段、最终源一致性及清理均成功才接受该平台。外部账号、真实Claude、物理重启/重新登录和自然网络切换仍是独立门槛。

## 本地检查与范围

9项Python工具检查、Python语法、actionlint及diff空白检查通过，覆盖身份/来源拒绝、证据过滤、具名通过要求、超时TERM后清理及已卸载但仍加载的服务清理。未在用户Mac执行原生安装、Homebrew操作或修改系统服务；正在运行的c4双平台12小时和独立auth key到期观察保持原冻结源码。
