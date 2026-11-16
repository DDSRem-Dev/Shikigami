# 一键安装与首次使用

用户要求（2026-11-16）：普通用户在 VPS 和 Mac 各执行一条命令，由工具完成依赖、服务、登录引导和插件配置，不再手工下载公钥、拼接 bundle/pin、编辑 YAML 或串联管理命令。

**版本边界：本页描述已发布 v0.0.1-alpha.4 的新入口。alpha.3 不含 `shiki-install quickstart` 或 `shiki setup`，不会改写其归档。固定源 CI、公开发行和资产验证见[15](15-implementation-status.md#evidence-public-alpha4-release-20261116)；真实新向导 VPS/Mac 首装、账号授权与物理重启仍未实测。**

## 用户入口

从受信源码 checkout 启动，VPS（Debian 12 / Ubuntu 24.04）：

```bash
sudo bash scripts/bootstrap.sh --quickstart
```

Mac 当前登录用户，不加 sudo：

```bash
bash scripts/bootstrap.sh --quickstart --role=client
```

直接使用以下 v0.0.1-alpha.4 完整命令。用户无需克隆仓库或另行下载文件：

```bash
# VPS
curl --proto '=https' --proto-redir '=https' -fsSL https://github.com/DDSRem-Dev/Shikigami/releases/download/v0.0.1-alpha.4/bootstrap.sh | sudo bash -s -- --quickstart --version=v0.0.1-alpha.4

# Mac
curl --proto '=https' --proto-redir '=https' -fsSL https://github.com/DDSRem-Dev/Shikigami/releases/download/v0.0.1-alpha.4/bootstrap.sh | bash -s -- --quickstart --role=client --version=v0.0.1-alpha.4
```

同一版本 Release 的 `bootstrap.sh` 也支持管道执行，交互通过 `/dev/tty`，不从脚本输入流读取回答。预发布必须使用明确的 Release/tag 下载 URL，不以 stable-only `latest/download` 推断预发布。加 `--version=v0.0.1-alpha.4` 可固定归档；省略时选择最近公开 Release，旧版本没有向导时明确失败，不退回半套安装冒充完成。脚本应从可信发布/源码取得，归档仍先验证固定发行身份和 Ed25519 签名，再执行包内代码。

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

## 恢复与验收

重复执行沿用安装事务、版本和身份。同源受信系统支持跳过包更新和密钥环重登记，运行 Chrome 保留；正常中断的密钥环登记继续使用原密码和加密 store。未提交的密文、受保护文件被外部修改等冲突保留人工复核，不自动覆盖。中断事务要求原有显式 rollback，不抹除 receipt。停止向导不等于卸载已完成的服务；安装包/系统依赖不是一个可完整回滚的大事务。

systemd/LaunchAgent 存活、节点 online 或 doctor 输出均不单独代表应用 ready。Claude 未授权时先交付连接凭据解除“需要浏览器登录，却必须先登录才能连接”的引导循环，状态仍为 pending。

本地隔离测试不访问真实账号，不安装宿主包，不修改用户系统服务。公开发行和资产验证见[15](15-implementation-status.md#evidence-public-alpha4-release-20261116)；真实 VPS/Mac 向导首装、官方 Claude 登录、物理重启及公开包完整服务升级/回滚仍单列外部门槛。11/27 的签名、普通 UID、权限、revision、取消和就绪要求保持，细分命令继续用于高级配置和故障处理。
