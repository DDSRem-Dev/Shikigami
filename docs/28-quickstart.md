# 一键安装与首次使用

用户要求（2026-11-16）：普通用户在 VPS 和 Mac 各执行一条命令，由工具完成依赖、服务、登录引导和插件配置，不再手工下载公钥、拼接 bundle/pin、编辑 YAML 或串联管理命令。

**alpha.8已发布并完成公开资产独立验签：** 本页下载命令现指向alpha.8，作为下一轮修复测试目标。发行与验证结果见[15](15-implementation-status.md#evidence-public-alpha8-release-20261120)，修复内容见[alpha.8说明](releases/v0.0.1-alpha.8.md)。用户VPS与Mac已于2026-11-21升级正式alpha.8，见[两端升级与实测](15-implementation-status.md#evidence-alpha8-upgrade-20261121)。默认预算下6次resolve（0.594–4.938秒）、端口目标拒绝恢复、浏览器输入/5次显式重连/接管/暂存文件功能通过；浏览器一次状态超时经手动Refresh恢复，临时转发后续端口重绑定原因未定。默认`/bin/sh`方向键尚未修复，长稳/网络专项/Claude登录/物理重启仍开放。

**版本边界：本页使用 v0.0.1-alpha.8 修复入口，延续 alpha.4–alpha.7 的安装与连接修补，新增同步复合预算/副本保护、端口目标拒绝隔离及浏览器控制竞态修补。发行状态与验证见[15](15-implementation-status.md)。alpha.3 不含 `shiki-install quickstart` 或 `shiki setup`，历史归档不改写；真实完整双机首装、账号授权和物理重启仍按外部门槛登记。**

历史alpha.7已纳入alpha.6发布后发现的旧attachment本地清理竞态与已有连接身份复核预算修补；alpha.8继续保留这些修补。历史公开发行和两端部署后验收分别记录，见[29](29-alpha6-installation-followup.md)。连接失败时不要把更换 Token 当作通用恢复步骤。

## 用户入口

从受信源码 checkout 启动，VPS（Debian 12 / Ubuntu 24.04）：

```bash
sudo bash scripts/bootstrap.sh --quickstart
```

Mac 当前登录用户，不加 sudo：

```bash
bash scripts/bootstrap.sh --quickstart --role=client
```

直接使用以下 v0.0.1-alpha.8 完整命令。用户无需克隆仓库或另行下载文件：

```bash
# VPS
curl --proto '=https' --proto-redir '=https' -fsSL https://github.com/DDSRem-Dev/Shikigami/releases/download/v0.0.1-alpha.8/bootstrap.sh | sudo bash -s -- --quickstart --version=v0.0.1-alpha.8

# Mac
curl --proto '=https' --proto-redir '=https' -fsSL https://github.com/DDSRem-Dev/Shikigami/releases/download/v0.0.1-alpha.8/bootstrap.sh | bash -s -- --quickstart --role=client --version=v0.0.1-alpha.8
```

同一版本 Release 的 `bootstrap.sh` 也支持管道执行，交互通过 `/dev/tty`，不从脚本输入流读取回答。预发布必须使用明确的 Release/tag 下载 URL，不以 stable-only `latest/download` 推断预发布。加 `--version=v0.0.1-alpha.8` 可固定归档；省略时选择最近公开 Release，旧版本没有向导时明确失败，不退回半套安装冒充完成。脚本应从可信发布/源码取得，归档仍先验证固定发行身份和 Ed25519 签名，再执行包内代码。

默认安装全部四种插件的支持；只需终端、同步和转发时加 `--components=core`。`--user=<普通用户>` 可覆盖 VPS 默认专用账号 `shiki`。向导不接受自定义布局或 `--non-interactive`，高级/无人值守部署继续使用原安装参数契约。

已安装客户端直接运行 `shiki setup`；可选 `--server my-vps --components=all`。重复同一命令继续未完成的引导，已有地址、插件配置及 profile 默认保留；失效凭据在向导内明确确认后更新，旧凭据文件保留。Mac 自动为受支持 shell 添加 PATH，遇到自定义链接则保留并显示绝对命令路径，继续连接向导。

## 自动完成与显式选择

| 环节 | 向导责任与边界 |
|---|---|
| 验签工具 | bootstrap 在必要时说明并请求安装 APT 基础工具或官方 Homebrew/OpenSSL；原高级模式不自动装包 |
| VPS 基础环境 | 普通账号创建、tmux、缺失 Claude 的官方安装、服务配置；Claude 安装和运行均使用普通 UID |
| 地区/浏览器支持 | 复用受签名保护的 setup，bootstrap 自动传递 archive/signature/public key/pin；不要求用户查找这些文件 |
| 系统变更 | 计划说明包来源、整机 Chrome 策略、linger、受限 helper、加密密钥环和 host-key 边界；确认后执行 |
| 密钥环 | 新专用密钥环隐藏输入口令；已有 enrollment 复用，不重置已有密钥环 |
| 节点登录 | 显示真实 URL、有界等待且可取消；账号授权和设备审批由用户完成 |
| 连接凭据 | 在私有终端交付地址/设备 Token，允许 Claude 待授权时先配对打开远程浏览器；不降低原应用 ready 判定 |
| 客户端连接 | 隐藏输入 Token；已登记服务器复用地址/凭据，设置默认服务器 |
| 插件 | 经 agent/远端 RPC 启用所选双端插件，核对 worker ready 和 region helper；不直接运行插件或写状态文件 |
| 端口 | 用户可输入允许端口；明确完整配置替换并确认，默认留空保留，重跑也可显式补配置；插件 ready 不等于转发已建立 |
| 浏览器 | 新 profile 选择继承/JP/明确 US 时区，启动并打开本机查看器；既有实例不自动替换，恢复需显式选择 |
| 项目 | 在项目目录执行现有 `shiki` 智能入口创建/映射和选择会话；首次差异仍需显式冲突选择 |
| 终态 | 区分已安装、节点在线、插件就绪、浏览器/端口配置和 Claude 待授权；跳过或失败不记通过 |

不会隐式选择整机国家、DNS、出口、浏览器开机目标或文件同步方向。Chrome 托管策略影响同机全部官方 Chrome：禁用密码保存、Chrome 同步和 Chrome 账号登录，网站账号登录仍可用。实际官方授权仍由用户在远程浏览器完成；查看器仍需 Take control 才能输入。

## 安装时看到的提示

安装器显示当前步骤与耗时，耗时超过10秒的已覆盖非交互操作会持续提示仍在运行。APT、Chrome 下载和语言包生成可能需要数分钟；耗时提示不是下载百分比，也不代表检查已经成功。完成或失败会明确显示，Ctrl+C 仍可取消。

`Keyring password (not echoed)` 要求设置专用浏览器加密密钥环口令，不是 root、SSH 或 Claude 密码。输入时不显示字符或星号；新建时输入两次。保存口令，后续恢复沿用同一个；正常运行采用已明确批准的 systemd 加密凭据自动解锁。

alpha.4 曾因公共目录被 `umask 077` 限制而出现 `203/EXEC Permission denied`。本版在批准安装后修复已知公共程序/策略目录，不递归放宽私密凭据或密钥环权限。无需手工 chmod；直接执行上方新版本命令继续。

## 恢复与验收

在 Mac 上检查现有连接时，将下面的 `my-vps` 换成向导中保存的服务器标签（不是端口号）：

```bash
shiki --server my-vps projects list
shiki plugins --side local list
shiki --server my-vps plugins --side remote list
shiki --server my-vps browser status work
```

本机应有 sync、port、browser，远端另有 region；仅安装 core 的用户不要求 region/browser 就绪。项目列表成功表示一次实际远端请求完成，插件列表仍需检查各项状态；浏览器 `running` 不代表查看器已取得控制权，也不代表 Claude 已登录。

若原有连接报告 `RECONNECT_EXHAUSTED`，网络恢复后执行 `shiki connect my-vps`，使用已保存的地址和凭据重新连接，再运行项目查询。若报告身份或凭据拒绝，按具体诊断核对原服务器；不要用重新登录节点或重签 Token 作为所有故障的通用修复。`shiki` 不在 PATH 时，可暂用 `~/.local/bin/shiki`；不要为写入 PATH 而接管受保护的 shell 文件。

浏览器画面可用但鼠标无法操作时，先点击 **Take control** 并等待 **Controller**。画面冻结或断开时点击 **Reconnect screen**；新连接从只读开始，必要时再次接管。关闭查看器不停止远端 Chrome。系统支持升级若明确要求停止现有实例，可执行 `shiki --server my-vps browser stop work --yes`，完成升级后执行 `shiki --server my-vps browser start work --yes`；保留 profile，但停止前应保存网页中未提交的内容。

重复执行沿用安装事务、版本和身份。同源受信系统支持跳过包更新和密钥环重登记，运行 Chrome 保留；正常中断的密钥环登记继续使用原密码和加密 store。未提交的密文、受保护文件被外部修改等冲突保留人工复核，不自动覆盖。中断事务要求原有显式 rollback，不抹除 receipt。停止向导不等于卸载已完成的服务；安装包/系统依赖不是一个可完整回滚的大事务。

systemd/LaunchAgent 存活、节点 online 或 doctor 输出均不单独代表应用 ready。Claude 未授权时先交付连接凭据解除“需要浏览器登录，却必须先登录才能连接”的引导循环，状态仍为 pending。

本地隔离测试不访问真实账号，不安装宿主包，不修改用户系统服务。公开发行和资产验证见[15](15-implementation-status.md)；历史 alpha.6 已使用公开签名包完成现有 VPS/Mac 升级；alpha.7部署与交互验证以[29](29-alpha6-installation-followup.md)的实际记录为准。alpha.8已于2026-11-21完成两端正式升级及部分现场功能复验，范围和遗留问题见[两端升级与实测](15-implementation-status.md#evidence-alpha8-upgrade-20261121)；不是全部测试通过。首次完整向导、官方 Claude 登录、物理重启及本批实机回滚仍单列外部门槛。11/27 的签名、普通 UID、权限、revision、取消和就绪要求保持，细分命令继续用于高级配置和故障处理。

## alpha.6 已发布的体验改进

客户端把服务器标签与地址分开提示，纯端口号会被拒绝并重新询问；已保存地址会显示，可用`shiki setup --server <标签> --tailscale-address <IP或主机名>`显式修正，保留凭据和身份pin。确认支持y/yes/n/no；Token提示说明不回显、Enter提交和Ctrl+C取消。控制面暂时离线不会要求重新授权或误换Token。

快速安装批准计划后，仅移除所选普通用户真实home目录的组/其他用户写位，保留已有读/执行位及全部子文件；不更改所有者。受保护shell启动文件逐个保留和报告，其他安全文件仍可写入PATH块。显式安装region支持会写入受管daemon专用NoNewPrivileges=false drop-in，使既有六动作sudo授权可以执行；core-only仍保持true。若运行daemon仍继承旧限制，仅重启daemon，独立tmux/Claude/Chrome保持运行。

查看器显示Controller后才能操作。画面冻结时可以显式点击Reconnect screen，新连接仍从只读开始，需要重新Take control；不会重放此前鼠标/键盘输入。部署与实测结果见[29](29-alpha6-installation-followup.md)。
