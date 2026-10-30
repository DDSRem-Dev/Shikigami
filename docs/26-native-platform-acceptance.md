# 原生完整产品平台验收

## 当前状态（2026-10-31）

当前[CI41115363744](https://github.com/DDSRem-Dev/Shikigami/actions/runs/41115363744)绑定896b986：原生Linux双架构与WSL2完整通过；Mac Intel在登录准备有一个新失败，Mac ARM仍运行。此前e2f的Mac双架构与WSL完整通过，两Linux产品失败已在896b正向复验。另已确认并本地修复旧gVisor的ARM64 race CAS缺陷，固定Go1.26兼容的最早上游Go快照，完整平台待新CI。Phase5复合项保持未勾选，路线图36/42；物理环境、主机重启登录与外部账号仍独立验收。

| 环境 | 本批要求执行的范围 | 已取得结果（截至本批） |
|---|---|---|
| ubuntu-24.04 / amd64 | 完整race、真实双端插件、E2E、内嵌插件、终端语义、原生systemd及用户服务、活动升级/回滚 | 首轮E2E失败、第二轮agent用例失败保留；588及c108全产品/E2E/embedded/terminal通过；c108首次安装/e2f退出事件旧失败保留；896b全产品/E2E/terminal及systemd维护、数据保持和清理完整通过 |
| ubuntu-24.04-arm / arm64 | 同上，实际arm64内核/Go宿主 | 896b产品及systemd维护完整通过，真实HTTP拒绝用例通过；c108首次安装/e2f认证health失败历史保留，588的useradd60秒超时原因仍未知 |
| macos-15 / arm64 | 完整产品/终端、实际LaunchAgent、上传下载活动升级 | c108与e2f完整产品及LaunchAgent全部通过，实际tmux3.8及p1/GOMAX2；896b当前运行，旧失败保留 |
| macos-15-intel / amd64 | 同上，拒绝Rosetta替代 | bf2e645/588/c108/e2f完整产品及LaunchAgent通过；896b唯一peer-lifetime登录准备超时，后续维护未执行，原CI原因未复现 |
| windows-2025的WSL2 / amd64 | 独立Ubuntu、真实Microsoft内核、Linux文件系统、systemd PID1及产品/维护矩阵 | c108/e2f/896b完整产品及systemd维护通过，13生命周期操作、双角色四个活动升级/回滚边界及清理通过；历史失败保留 |

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

可选缓存预算为build8GiB/modules4GiB、25万文件、15秒协作stat扫描，保持源内容和权限；恢复3分钟、保存5分钟以及准备/lookup1分钟步骤上限均为非阻断，缓存失效时正常构建。16项缓存工具检查、18项平台工具检查、8项构建元数据检查、AST/actionlint/diff及独立七类审查通过。c108轮四compile初次模块/编译缓存均miss，prepare/baseline/plan成功且四目标warm实际保存；Linux amd64于18:14:54Z保存共享modules，另外两任务迟到lookup命中跳过重复上传，另一并发reserve竞争被安全拒绝且任务成功。c108两Linux失败后native partial实际保存，新增缓存尾部约15.1/11.5秒；保存成功不覆盖安装失败。跨轮恢复命中/partial提升仍待，不把首存通过当提速证据。WSL继续独立发行版，不复用跨Windows缓存。


58806be原生Intel后续终态同样通过，source_unchanged=true、incomplete_stages为空，4CPU/14GiB、p2/GOMAX2；build/lint578.146秒、全包race1055.335秒、E2E287.879秒、embedded54.098秒、terminal16.053秒、LaunchAgent生命周期159.038秒、上传137.744秒、下载162.144秒。首轮1451.806秒的全包race本轮降至1055.335秒；不同源/runner波动保留，不能当作c108修复后的完整结果。


### c10852f 平台结果与剩余安装诊断

实际WSL2全阶段通过：全包race535.945秒、E2E178.547秒、systemd227.259秒；13项服务生命周期、agent/daemon两角色共四个活动升级/回滚边界、卸载数据保持及独占发行版清理通过。该结果覆盖本轮冻结源的WSL产品与服务，不外推物理设备重启、真实Claude或外部认证。

Linux amd64/arm64全包race574.450/530.638秒、E2E187.125/177.919秒及embedded/terminal均通过；systemd分别73.282/92.822秒失败，固定诊断为activate-daemon、test-systemd.py初次install的RuntimeError。两端账户创建及清理通过，arm64的useradd为37.682秒；cache3.709/3.671GB均完整复制。/tmp祖先权限冲突已修正且WSL完整维护通过，但不能据此把native剩余错误归结为编译、缓存或超时。原始stderr不上传。后续独占容器已复现/opt0777的明确安装拒绝，见下述诊断；本轮原生宿主尚无实际目录mode记录，不把机制复现当成修复后原生通过。


### 托管镜像安装祖先权限冲突复现

读取GitHub官方runner-images的`images/ubuntu/scripts/build/configure-system.sh`，当前blob `829f338bcdeb6093e493790d9e3856efdddd2c93`明确执行`chmod -R 777 /opt`。本轮日志记录的Ubuntu amd64/arm64镜像分别为20261025.353.1/20261025.165.1；对应amd64镜像tag API返回404，因此这里不声称取得本轮镜像的冻结配置源码或实际宿主mode。

独占Linux arm64真实systemd容器冻结c108源，在容器内部将/opt由0755模拟为0777；真实首次installer退出1，明确类别`system_prefix_ancestor_unprotected`，与生产ensureSystemPrefix的保护拒绝对应。未改用户主机/opt，未读取或公开认证材料；按CID与label核对后销毁诊断容器并清理专属镜像tag。该反例确认/opt并非可假定安全的CI父目录，前次只从/tmp移到/opt的修复不完整。

本次仅调整验收脚本：GitHub专属根移到`/var/lib/shiki-platform-systemd-*`，在建账户、复制缓存及构建之前逐层检查父目录为真实目录、root所有且无group/world写入；公开目录诊断仅允许`/`、`/var`、`/var/lib`、`/opt`固定路径及固定布尔/类别，不输出stderr。生产安装安全检查、权限、超时和全部维护场景保持。新布局的原生正向验收由下一轮CI提供；WSL已通过证据仍绑定c108旧布局，不能跨源追认。


本次诊断含构建/清理109.055秒，初次installer没有单独计时；专属CID与image tag缺席已独立记录。21项平台工具回归1.078秒及四脚本AST/diff通过，覆盖任意深度祖先权限/owner/symlink拒绝、拒绝时零账户/缓存/目录创建、结果保留和固定诊断不泄露原始错误。架构/协议检查通过；新布局实际平台结果保持待验。


### e2f7f41 实际缓存恢复

修复提交`fix(ci): validate protected systemd acceptance paths`已推送master，CI41109911804于18:47:35Z触发。四目标compile完成；Linux amd64实际日志确认modules exact hit后恢复3.672秒、compile/linux-amd64/CGO0旧源warm fallback恢复6.478秒，随后新源warm于18:48:32.781Z保存成功。此为真实下载/提取和构建结果，不只API cache lookup命中；native运行中日志端点404/503，partial恢复与完整平台仍待实际证据。


### c10852f Mac双架构完整终态

两个Mac最终artifact均state=passed、source_unchanged=true、incomplete_stages为空，完整LaunchAgent生命周期与活动上传/下载升级均有具名PASS。arm64实际3CPU/7GiB、p1/GOMAX2，tmux3.8；build/lint336.485秒、全包race1505.223秒、E2E262.351秒、embedded36.763秒、terminal17.614秒、LaunchAgent生命周期154.793秒、上传130.567秒、下载137.406秒。此前空pane PID和GOMAX1下节点期限问题已有这轮实际平台正向验证。

Intel为4CPU/14GiB、p2/GOMAX2；build/lint502.576秒、全包race886.896秒、E2E282.515秒、embedded83.209秒、terminal19.880秒、LaunchAgent生命周期181.984秒、上传133.896秒、下载167.273秒。旧源码失败和正常预算保持，不把arm64从失败轮到成功轮的耗时差直接归因为单一并发参数。Mac arm64整包race约25分钟仍是当前主要耗时，后续缓存实际复用效果单独记录；不以降低覆盖制造提速。

GitHub API部分job.steps滞后、运行中日志404/503，Chrome页面也出现加载超时；根代理使用Chrome读取的阶段日志仅作为进度观察，完整结论以随后下载并核对的artifact为准，不用页面计时器替代stage精确seconds。c108最终为Mac双架构和WSL全通过，两Linux维护失败；e2f新源的/var/lib正向验收继续等待。


### e2f7f41 原生partial恢复与新产品失败

两Linux实际命中c108的native/对应架构/CGO1/partial：amd64于18:47:50.213→18:48:03.450完成恢复13.237秒，arm64于18:47:50.550→18:48:04.449完成13.898秒；非lookup推断。build/lint从c108的217.624/143.132秒降至16.766/12.900秒，覆盖未减；之后失败仍保存新源partial，没有错误提升为warm。整轮产品测试发生失败，不能将370.640/416.506秒与全通过轮直接比较来声称整体测试提速。

本轮两原生Linux没有执行E2E或维护：amd64唯一失败为TestObservedExitEventsWithoutAutomaticRestart/kill_-TERM_$$，crash_events_test.go107先记录未知退出原因、152把随后CLAUDE_CRASHED判为重复；arm64唯一失败为TestAuthKeyHTTPRejectionCompletesLogin/health-event，auth_rejection_test.go25、15.25秒。原生/var/lib正向验收因此仍未发生。初查确认session测试把允许的未知→已知首次原因升级当成重复；node旧测试本机10次race均通过，原CI瞬时失败未复现。后续测试夹具修复与验证见下一节，保留原准入/事件/截止契约。


### e2f7f41 WSL新布局正向验收与后续测试修复

实际WSL2全阶段state=passed、source_unchanged=true、incomplete_stages为空；全包race665.037秒、E2E195.992秒、systemd255.314秒。13个生命周期操作、两角色四个活动升级/回滚边界、卸载数据保持及精确清理通过。/var/lib、/var、/均root-owned且无group/world写入，WSL的/opt也满足保护。本证据仅覆盖e2f新布局实际WSL，原生Linux两架构因产品测试失败尚未进入维护。

下一批仅修测试与调度：真实tmux事件测试接受unknown→首次known，仍检查唯一crash、scope/顺序/正确原因和原超时；确定性补测不要求实际tmux必须提供信号。认证夹具改为真实Noise HTTP400，在LocalAPI订阅完成后放行，或先经独立SDK health observer确认再启动Node watcher；旧needs_login不足以作为认证结果屏障。原CI认证瞬时失败未直接复现，不能据此宣称已定位所有SDK时序原因。session最终专项race5.753秒、node最终两种时序包4.764秒及vet通过；更早初稿全包与20次认证重复另按实际范围记录于15。

Linux普通产品失败后允许独立systemd维护；必须先通过preflight/build-lint/tool-tests/platform-tool-tests，再核对OS/arch/machine/Go/kernel/WSL与完整冻结源码。源码摘要覆盖tracked及非忽略untracked，新增Go输入/同提交变更均会拒绝，忽略生成物不算自定义摘要覆盖。信号取消/负退出码/130/143拒绝继续，固定blocked原因不当PASS；最终所有原必需阶段保持，失败不能被维护成功覆盖，Mac仍按成功链。27项平台工具检查1.200秒、actionlint/AST及三段WSL Bash检查通过。下一轮完整平台结果待新提交。


### 896b986 Linux完整闭环、Mac新失败与依赖性能定位

两原生Linux最终state=passed、source_unchanged=true、incomplete/failures均为空。amd64/arm64产品385.477/400.338秒、E2E176.416/168.868秒、build/lint17.717/13.500秒、systemd239.720/245.831秒。新auth health-event0.09/0.57秒、before-watch均PASS；session TERM0.71/0.70秒，exit0/exit7/intentional-stop通过。实际/opt两台均writable_by_others=true，/var/lib、/var、/均root所有且不可组/他人写；新根13服务操作、四活动升级/回滚边界、数据保持及cleanup通过。e2f partial真实恢复12.708/15.550秒并保存896b warm。独立维护准入通过；因产品本轮成功，不宣称已在真实runner演练“产品失败但维护继续”。

WSL896b全阶段PASS，来源未变且无未完成项：build205.913秒、产品566.848秒、E2E185.237秒、embedded21.441秒、terminal8.044秒、systemd247.212秒，13操作/四活动边界/数据保持/清理全通过。Mac Intel本轮build65.562秒、产品642.911秒唯一失败为TestPeerRemovalClosesEstablishedIdleStreams/outbound/removed，15.89秒；安全行43/49指向left的loginInteractive，未进入撤销业务断言，不能从安全日志判断是URL还是online阶段。Mac ARM实际排队约10分10秒，19:37:21Z开始，不把排队当测试执行时长。

旧e2f Mac终态也已完整取得：arm64 build292.232、产品1391.744、E2E244.342、embedded32.245、terminal13.826、Launch144.770/上传120.498/下载117.580秒；Intel547.226/900.658/249.957/65.391/23.246/172.875/167.196/152.176秒，全部PASS且来源未变。旧run41109911804完整结论failure来自两Linux产品测试；上述新run的两Linux正向结果不改写旧失败。

耗时独立定位发现旧gVisor ARM64 race CAS错误：预期old值被new覆盖、存储寄存器未初始化；固定模块zip与已编译指令一致，最小直接调用和新增CAS回归均确定性失败。上游PR14585于09-09合并，选择第一份含修复的可构建Go快照v0.0.0-20260909230408-9b1144b679cb，保持Go1.26.10/Tailscale1.98.10。默认非race走标准sync/atomic，不据此宣称默认产品同样忙循环；新网络栈版本还包含其他变更，完整平台需重验。

本机同GOMAX2/p1/race的原连接用例32.48→1.81秒；后者未插入phase overlay，仅报告原用例总时长。候选node整包44顶层/132含子项51.200秒零fail/skip，主树新增CAS回归1.533秒通过（旧依赖0.612秒红）；全包build39.847/vet22.515/模块校验3.356/漏洞扫描4.029秒通过。Intel旧失败本机未复现，候选两次phase诊断通过仅属局部证据；预算与重试不变，下一轮须实际通过Intel与其余完整矩阵。进度仍36/42，长稳与外部环境不跨新依赖追认。
