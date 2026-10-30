# 原生完整产品平台验收

## 当前状态（2026-10-31）

四目标完整产品及实际WSL2验收已接入现有CI。首轮[41090262712](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41090262712)绑定原bf2e645；按用户要求改提交格式后对应c83c95a，git tree相同但不改写历史CI来源。第二轮[41093552523](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41093552523)绑定42eb93e。两轮已有失败，不能认定平台完成；Phase5复合项保持未勾选，路线图36/42。公开Homebrew四目标渠道验收见22，不能替代本矩阵。

| 环境 | 本批要求执行的范围 | 已取得结果（02:10 +08） |
|---|---|---|
| ubuntu-24.04 / amd64 | 完整race、真实双端插件、E2E、内嵌插件、终端语义、原生systemd及用户服务、活动升级/回滚 | 首轮E2E失败、第二轮agent用例失败保留；588全产品/E2E/embedded/terminal通过，systemd安装路径失败，修复待新CI |
| ubuntu-24.04-arm / arm64 | 同上，实际arm64内核/Go宿主 | 三轮产品/E2E/embedded/terminal通过；588在useradd 60秒超时，原因待核验，新固定诊断已补 |
| macos-15 / arm64 | 完整产品/终端、实际LaunchAgent、上传下载活动升级 | 三轮有tmux/session失败；588另有GOMAX1下node期限失败，兼容性和资源策略已本地修复，待新CI |
| macos-15-intel / amd64 | 同上，拒绝Rosetta替代 | 首轮bf2e645完整原生产品及LaunchAgent维护通过，tmux3.7c；42eb/588结果尚待，不追认新源码 |
| windows-2025的WSL2 / amd64 | 独立Ubuntu、真实Microsoft内核、Linux文件系统、systemd PID1及产品/维护矩阵 | 首轮实际WSL身份通过；588全产品/E2E/embedded/terminal通过，systemd安装路径失败、清理通过；前轮agent/sync失败保留 |

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

本轮将四原生任务并发槽增至4；预检按CPU亲和性、内存及当前cgroup-v2/可见祖先限制选择包并发：最终策略至少4核且6GiB才用p2，2/3核保持p1/GOMAX2，否则p1，探测失败也p1，GOMAXPROCS限制1–2。缓存来自runner既有GOCACHE，按目录与1MiB分块协作检查30秒/4GiB预算，完整条目复制到新UID自有私有目录；不共享可写源、不更改原缓存/HOME权限，部分复制可重建缺项，未完成条目删除。30秒不是阻塞文件系统操作硬deadline。

所有count=1、race、平台/维护阶段、具名PASS与清理要求保持。systemd失败时finally也导出安全固定结果；Go失败只公开受跟踪已知测试文件basename、合法行号、固定错误类别及有界elapsed，不含原始Output、Token或URL。独立七类审查通过。实际wallclock收益与剩余失败须由新CI确认，不能只凭调整参数宣称提速完成。

第二轮agent obsolete-selection失败已在source8复现：maintenance可先建立本地授权冷却，foreground Resolve合法返回Unavailable。测试现接受此现有竞态，同时加强auth_required/NODE_AUTH_REQUIRED、仅tsnet、无旧RemoteProtocol断言；20次race及7项相关回归通过，生产未改。WSL同步插件失败本地未复现，保持原断言等待安全行号。


### 优化轮已测启动耗时

[CI41099611238](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41099611238)精确58806be于17:31:48Z创建；Linux amd64/arm64实际17:31:51/53Z启动，Mac Intel17:31:53Z、Mac arm64 17:34:15Z启动，WSL也已运行。Mac Intel等待仅5秒，arm64为2分27秒，已消除旧两槽串行造成的约26分钟等待；runner可用性仍影响实际排队。整轮执行、包并发及私有缓存的收益须等最终阶段摘要，不由启动时间推算。GitHub overview仍可显示queued而部分job已运行，状态以逐job为准。


### tmux 3.8 已退出pane兼容修复

官方3.8相对3.7b的format_cb_pane_pid仅在pane仍有fd时输出PID，已退出pane返回空字符串；现有解析器无条件Atoi，导致保留的dead pane整体解析失败。确定性回归修前四场景红，现仅dead且空PID保留为0，live空PID或非数字仍拒绝，退出0/非零/信号/unknown独立保留；不降级tmux或伪造存活/退出状态。消费者复核确认PID0不作为进程信号目标或活跃身份，操作仍使用精确tmux session/window/pane ID。

隔离TMUX_TMPDIR内以本机3.7b执行tmux/session整包race65顶层，8.512/21.461秒，零fail/skip；vet及独立七类复核通过。官方3.8固定源码已核对GitHub release摘要及tag来源，本地独立编译因缺所要求的jemalloc停在configure，未安装依赖或改用户Homebrew，不称3.8实际运行通过；须由采用3.8的Mac CI补验。旧Mac失败与此已知格式变化吻合，其他无安全行号失败继续保持待定位。


### 58806be 实测结果与下一轮修复范围

优化轮Linux amd64/arm64和WSL完整全包race分别563.519/559.562/569.749秒，E2E分别179.382/176.031/183.821秒；三者产品、release工具、内嵌插件与终端阶段均通过。首轮两Linux全包race为1023/1132秒，本轮约9.4/9.3分钟；这是实际运行对照，不保证所有runner固定收益，也不等于整个平台已通过。三者systemd维护失败但清理均通过：amd64/WSL已定位activate-daemon、test-systemd.py 285→275；arm64为useradd的60秒调用超时。

Mac arm64实际3CPU/7GiB被选为p2/GOMAX1，全包race590.757秒失败，包含已知tmux兼容问题及两个45秒节点用例失败；旧42eb对应node用例曾通过。相同源码、原45秒预算下，本地GOMAX1两项分别45.52/45.24秒失败，GOMAX2则30.35/31.38秒通过，包63.21秒、零skip；已将策略修为至少4CPU且6GiB才p2，两/三核保持p1/GOMAX2。不能把减少单进程CPU造成的新失败当成优化成功。

本轮私有systemd缓存副本实际为amd64 4,290,401,201字节/18,557文件、8.071秒后触及4GiB上限；WSL3,708,416,479字节/18,751文件、19.644秒完整复制。缓存复制成功不覆盖后续安装失败。

产品构建参数对照已经通过：固定58806be源、独立空GOCACHE、p1/GOMAX2，原build.sh185.545秒，对齐全包-trimpath后152.838秒，减少32.707秒（约17.6%）；六程序SHA256及双manifest完全一致，实际版本/Commit/BuiltAt/trimpath和清单均核验，原有8项元数据测试通过。仅构建脚本及对应argv期望变化，不将这一本机对照幅度承诺为远端CI结果。初次验证前置Go1.26.3不满足go.mod、随后读取buildinfo的验证脚本假设错误均保留；实际构建使用固定Go1.26.10，未重跑已成功的before。


首轮原生Intel终态已取得（实际tmux3.7c）：bf2e645（树同c83c95a）的Mac amd64全部阶段PASS，source_unchanged=true、incomplete_stages为空；build/lint566.825秒、全包race1451.806秒、E2E320.003秒、embedded102.498秒、terminal26.252秒、LaunchAgent生命周期232.723秒、活动上传213.338秒、活动下载176.370秒。此为首轮固定源完整原生Intel证据，不追认新HEAD、Mac arm64或Linux维护通过；其他首轮失败保留。

原生systemd测试布局存在已确认契约冲突：commands在/tmp下，而生产systemBinDirectory要求所有祖先归root且不可被组/他人写入。现仅将GitHub专用根移至/opt/shiki-platform-systemd-*，继续独占账户/路径和精确清理，不削弱安装器保护。冻结源码的短Linux arm64容器实际调用生产校验，/tmp拒绝、/opt通过，两个子项0.012秒；不启动服务，不等于完整systemd已通过。账户创建增加lookup-account、lookup-group、create-group、create-user固定阶段/结果/耗时，允许公开字段严格过滤，用户创建60秒预算未变且超时原因仍待。资源/布局/诊断相关18项工具检查、语法与diff检查通过。


### 分离缓存与失败轮复用

官方setup-go v7.0.0的默认缓存键仅含runner/Go/go.mod，未含目标GOOS/GOARCH或race，且post-if为success、精确命中后不更新；旧compile四目标与native Linux amd64存在同键竞争，缺少的编译变体不能补存。旧日志有真实cache hit，不笼统声称全部冷编译；究竟哪个历史job首次占位不作推定。

现关闭内置缓存，固定使用官方稳定actions/cache/restore、save@v6.1.0。GOMODCACHE按实际宿主/Go、根及嵌套module锁和工具版本键共享；GOCACHE额外隔离native/compile、runner镜像、目标、CGO及源码内容。源码摘要覆盖受跟踪和非忽略输入含非Go资产，仅已知顶层文档排除于跨轮键；根/docs Go包嵌入文档时保守纳入。受控embedded-assets为忽略的生成物，其生成脚本/插件源受摘要保护，Go自身仍校验实际嵌入字节；不称任意忽略文件都被自定义摘要覆盖。

失败且实际产生编译条目时可保存partial，成功完整执行后保存warm；优先同源warm，再同源partial和同角色/宿主/目标/依赖的旧源缓存。精确命中及保存前lookup避免重复上传，不使用每轮run_id强制重传。保存前再次核对包括文档在内的全部来源不变；取消不保存，测试/安装失败不被缓存动作改写，count=1与实际PASS门槛保持。只缓存go env的独立Go目录，不包含运行状态、Token、证据或产物。

可选缓存预算为build8GiB/modules4GiB、25万文件、15秒协作stat扫描，保持源内容和权限；恢复3分钟、保存5分钟以及准备/lookup1分钟步骤上限均为非阻断，缓存失效时正常构建。16项缓存工具检查、18项平台工具检查、8项构建元数据检查、AST/actionlint/diff及独立七类审查通过。尚未取得新设计真实远端命中/保存时间，不把上述本地验证写成缓存收益实测。WSL继续独立发行版，不复用跨Windows缓存。
