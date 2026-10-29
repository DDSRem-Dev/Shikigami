# Homebrew 分发与自动更新

更新：2026-10-30。首个 Homebrew 版本 [v0.0.1-alpha.2](https://github.com/DDSRem-Dev/Shikigami/releases/tag/v0.0.1-alpha.2) 已由 tag 工作流全自动发布并生成 [Formula/shikigami.rb](../Formula/shikigami.rb)。实际远端 tap 的 Linux arm64 安装、测试、重装和卸载已通过，结果与限制见下文；22公开资产及两平台独立暂存结果见[公开alpha.2证据](15-implementation-status.md#evidence-public-alpha2-release-20261030)；macOS完整brew安装仍受本机Xcode/CLT门槛阻止。完整交付门槛仍见 [08 路线图](08-roadmap.md)。

## 安装与使用边界

同一个仓库作为 tap，不需要额外仓库、维护账号或跨仓库 Secret。实际安装命令：

```bash
brew tap ddsrem-dev/shikigami https://github.com/DDSRem-Dev/Shikigami
brew install ddsrem-dev/shikigami/shikigami
```

Homebrew 7 如要求信任非官方 tap，可以仅信任此公式：

```bash
brew trust --formula ddsrem-dev/shikigami/shikigami
```

Formula 按当前 OS/架构选择固定版本的 Linux/macOS × arm64/amd64 归档，不从源码编译产品。四目标可供下载不等于全部原生平台均已验收。

Homebrew 只安装经过认证的完整 bundle：14 个发行成员保存在 Cellar 的 `libexec/bundle/`，`bin/` 链接 `shiki`、`shiki-agent`、`shiki-daemon`、`shiki-install`。双插件和 manifest 随 bundle 保留，未移动到宿主代码或以内嵌业务替代。Homebrew 不初始化账号、配置和 tsnet，不启动服务，不调用安装脚本的激活路径，也不提供 `brew services` 入口。

安装后先查看当前版本和服务安装说明：

```bash
shiki-install --version
shiki-install help install
brew info ddsrem-dev/shikigami/shikigami
```

`brew info` 的 caveats 给出当前固定版本、可信 checksum pin 和 bundle 路径组成的 `--stage-only` 命令。先检查生成的服务 review；需要激活时，按 [11 安装契约](11-automated-installation.md)显式移除 `--stage-only`，完成依赖和登录。不能把 `brew install` 成功写成应用 ready。

服务安装器会将受信 bundle 复制到自身受管的不可变版本目录，并链接匹配插件。用户服务不直接引用会被 Homebrew 清理的旧 Cellar 路径。因而：

- `brew upgrade` 更新下载的 bundle 和 Homebrew 命令；使用新 CLI 前，按 caveats 对既有受管安装显式执行 `shiki-install install` 升级。
- `brew uninstall` 仅移除 Homebrew 包，不停止独立受管服务或删除用户状态。
- 移除受管服务时，先按原 role/prefix 调用 `shiki-install uninstall`；配置、认证和项目数据的保留仍遵循文档11。
- Linux daemon 服务必须显式选择普通运行用户和部署路径；包管理器不代替此操作。

## 信任链与许可证

[scripts/homebrew-formula.py](../scripts/homebrew-formula.py)只消费已由可信仓库 verifier、独立公钥和固定 release 身份验证的22个文件。生成前再次核对所消费签名的大小/hash，并对四目标归档重新核对大小/SHA-256，从每个认证归档读取完整 `LICENSE`。

许可证识别采用完整文本摘要白名单，未知内容或四目标不一致立即失败。已发布 `v0.0.1-alpha.1` 的冻结归档是 MIT；`v0.0.1-alpha.2` 起的源码授权为 GPL-3.0-only。生成器按归档中的真实许可证生成 Homebrew SPDX 标签，不会用当前工作树的 LICENSE 把旧包改标。GPL 标准文本末尾的示例文字不改变项目 README 明确的 only 授权。

Formula 内嵌已认证签名信封和预先可信的 SPKI 公钥/指纹。Homebrew 在解包前验证固定 SHA-256；公式在安装前使用 Homebrew Ruby OpenSSL 验证实际 Ed25519 签名、版本/目标/归档名、归档大小/hash、`SHA256SUMS` pin 及每个 payload hash。信任不来自同一次下载旁的公钥。只有固定14成员会复制至 libexec/bundle；Homebrew 自动加入 staging 目录的 `.brew_home` 等工具文件不纳入 bundle，也不执行。Formula对libexec声明skip_clean，将完整bundle放入libexec/bundle并在keg根保留独立LICENSE副本，避免Homebrew4/7的顶层metafile整理把认证bundle里的LICENSE移走。

这不提供在线密钥撤销查询，不等同 Apple 代码签名或公证。密钥轮换、历史公钥及离线撤信边界见 [21 演练说明](21-release-key-rotation.md)与[发布流程](18-release-readiness.md#github-actions-自动构建签名与发布)。

## 后续 tag 全自动更新

公开 Release 成功后，发布工作流在同一固定 tag 来源上生成公式，核对公开资产来源和摘要，再通过 GitHub contents API 更新 master 的单个 `Formula/shikigami.rb`。该路径沿用发布 job 的仓库写权限，不增加私钥用途或另一个手动工作流。首次 Homebrew `alpha.2` 也走此自动步骤；tag 中不预先放置指向未发布归档的公式。

更新器读取当前 Formula 的唯一版本标记并按 SemVer 比较，使用文件 SHA 做 CAS；冲突时有界重新读取，同版本相同内容幂等，不同内容拒绝覆盖。旧 tag 不能将公式降级；尚无稳定版时预发布按版本推进，一旦已有稳定版，更高版本号的预发布也不会替换稳定渠道。已固定的发行 tag 不移动。

发布工作流只更新分发公式，不代表完成外部账号、原生平台、长稳或完整应用升级验收。若 Formula 更新失败，已有公开 Release 保留，重试匹配的运行可继续处理；不得手动上传其他字节替代已签名资产。

## 已执行验收与当前边界

准备阶段使用已公开、独立验签的 `alpha.1` 归档，仅作工具兼容 fixture，未将其发布为 Homebrew 首版。实际首发 `alpha.2` 已另用远端 tap 的自动公式完成限定安装验收，见本页最后一节；两批证据不合并为同一源码或同一归档验收。

- 生成器7项行为测试通过：四目标来源绑定、签名消费时被替换拒绝、认证元数据不一致拒绝、版本/仓库注入拒绝、完整MIT/GPL文本识别、未知/混合许可证拒绝、消费中归档路径替换拒绝。
- 本机 Homebrew 7.0.11 / Ruby OpenSSL 4.0.3 可验证实际 Ed25519 签名。直接执行真实 Formula 安装方法完成14成员认证/复制、四宿主命令链接，并使用实际包内 helper 首次/重复私有暂存；未激活服务。签名篡改负例使用独立公式副本，实际拒绝。
- 本机完整 `brew install` 在执行公式前被 Xcode/CLT 门禁拒绝：macOS27需要Xcode27/对应CLT，当前Xcode26.6与现有CLT不满足。未绕过检查或改动系统工具，不能称本机 Homebrew 全流程通过。
- 独立负例夹具首次尝试修改 Homebrew 冻结类常量失败；真实安装/暂存此前已通过。保留失败记录，改用新的公式副本只补负例，没有改真实签名或重复已通过负载。
- Linux 容器使用官方 `homebrew/brew` 原生arm64固定镜像，默认UID1000、无额外capability、无systemd/宿主Docker socket，有限CPU/内存。镜像中的Homebrew将Linux arm64标记为Tier2，结果不外推原生amd64或完整平台支持。

本批仅分发工具/公式/文档变化；无生产Go、module或协议变化。SDK/host/plugin依赖、RPC授权、状态事务、生命周期、文件/终端/网络语义及安装激活契约保持。实际包管理结果、失败与精确资源清理证据在本节补充，08/15/16由同批总记录同步。


### 本地准备批次最终结果

最终Linux原生arm64容器的Homebrew4.6.20完整链已通过：真实公开alpha.1归档的install → test →临时formula revision1 upgrade → test → reinstall → test → uninstall，七个命令均成功，三个test分别完成真实包内helper两次暂存，共六次；版本/manifest与helper完整14成员校验通过。revision仅用于隔离tap测试包管理升级，不是伪造新产品发行。未激活服务或生成标准用户状态，临时tap/Cellar已清除；精确容器缺席经独立inspect核验。

证据：`/tmp/shiki-homebrew-linux-evidence-20261030-5/verified-summary.json`、七份command/log与final-state记录；固定镜像为`homebrew/brew@sha256:8fce1421a053e662271310fadda9fa275d32e702cbf03d05d8aad7a1e4b4224e`。输入及日志后置hash已保存；该缓存镜像随后复用于实际alpha.2远端tap验收，未借用其他容器。

原生macOS最终nested-bundle Formula实际安装方法、签名/payload/成员校验、四命令链接、实际helper两次私有暂存与篡改签名拒绝均通过，证据`/var/folders/1n/q373cln10fqb0gl9z1ds5d8m0000gn/T/shiki-homebrew-native-final-kr6oldgl/`。它仍不等于被Xcode/CLT门禁阻止的完整brew安装通过。

实际失败与修复保留：镜像没有Python，首个Python入口未启动；Bash首轮误用macOS的tap目录层级，改为`brew --repository`；第二轮发现Homebrew加入`.brew_home`，改为只校验/复制固定成员；第三、四轮发现Homebrew自动移动顶层libexec/LICENSE，Homebrew7根许可证副本办法不适用于容器的Homebrew4，最终使用libexec/bundle保护完整认证包，并在根目录独立保留许可证。修复未删减helper断言、未篡改签名/校验pin，失败轮均精确清理，旧日志未改记通过。

alpha.1 Formula只是未提交的准备fixture；用户要求的首个Homebrew alpha.2现已由新tag工作流自动生成，实际公开与远端tap安装结果见下文。macOS工具环境、原生amd64/WSL、真实账号/外部部署与完整产品长稳门槛均保留。


## 首个公开alpha.2远端tap验收（2026-10-30）

[Release 40912019720](https://github.com/DDSRem-Dev/Shikigami/actions/runs/40912019720)最终成功，固定源码`fa35debd9c74d0ad44704e3a7897e0e0c1fcc3f2`的`v0.0.1-alpha.2`于2026-10-29 17:30:47 UTC公开（北京时间10月30日01:30:47）。公开发布后的同一工作流由`github-actions[bot]`自动创建Formula提交`85003591044fca15f82c9c220b2051766f1e47aa`；没有先手工发布alpha.1公式，也没有移动发行tag。

独立普通用户容器从真实公开URL执行`brew tap ddsrem-dev/shikigami https://github.com/DDSRem-Dev/Shikigami`，实际克隆HEAD恰为上述自动提交。克隆公式的SHA-256与预先从自动提交确认的值及本地只读副本均一致：`53fe01dcc5f9aa1d4c4dc5aab8d9d4ea05013631993835eb1ce9bb4d293f15ea`；marker为alpha.2，许可证为GPL-3.0-only。

真实Homebrew4.6.20 / Linux aarch64 / UID1000完成六步：tap → install → test → reinstall → test → uninstall，全部成功。安装/重装运行实际公式的Ed25519、归档hash、checksum pin与payload校验；独立检查安装后的libexec/bundle为14个普通文件，SHA256SUMS中13个payload逐一为OK，LICENSE完整文本SHA-256为`3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986`。两个brew test各执行版本断言和实际包内helper的首次/重复暂存，共四次；没有服务激活、current切换或标准配置/状态生成。此处是相同公开版本的重装，不冒充跨产品发行升级；准备阶段的临时revision升级结果单独保留。

容器限制为2 CPU、2GiB内存、128 PID、cap-drop ALL与no-new-privileges；仅只读挂载验收脚本和独立可写证据目录，没有宿主Docker socket或systemd。退出0、清理断言成功，tap和Cellar已删除，标准用户状态/服务文件未创建。Docker事件记录确认精确CID`a947b700fb80bf0544d9c2fe074687baa7703b8b7dfa8917788856d983a5e34b`的die exitCode为0并随后destroy；按完整CID再次inspect确认不存在，未清理其他容器。

证据目录`/tmp/shiki-homebrew-alpha2-public-20261030/`，包括`verified-summary.json`、实际`public-formula.rb`、tap提交、公式预期hash核验、完整六份命令/日志、归档成员与许可证核验、工具链/UID、Docker事件及最终清理回执；输入/日志后置hash已保存。核验脚本`/tmp/shiki-homebrew-alpha2-public.sh`不修改真实tap内容或远端仓库。临时目录不是永久证据存档；不可取得时须重新核验对应结论。

七类收尾复核：生产源码目录、SDK/host/plugin依赖未改；RPC/授权/能力声明未改；持久状态/revision/幂等未改；服务生命周期只走原stage-only路径，无新激活；文件/终端/网络语义未放宽，归档与完整bundle仍认证后消费；CLI/安装布局保持独立受管prefix与Homebrew包分离；证据声明区分当前alpha.2公开tap、旧alpha.1准备fixture、本机直接Formula方法及被工具前置阻止的macOS完整brew安装。Linux arm64的Homebrew Tier2提示、原生amd64/WSL、外部账号/网络、物理部署与长期资源门槛不因本次分发成功而关闭。
