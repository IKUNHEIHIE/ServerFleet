# Server Fleet：核心代码运行说明

五项核心功能的正式源码、Web入口与本机部署工具一并交付；实际联调、找bug与故障演练由操作者执行。程序在Linux amd64/arm64普通用户下运行。发布签名、Root权限和恢复激活仍有各自的显式授权边界，不因代码交付自动放行。

## 构建与目录

准备Go1.27.3、Node24.22.1、npm11，在干净且已提交的源码目录执行：

```sh
npm ci
node scripts/build-release.mjs --version=0.9.0 --sequence=1 --output=/home/serverfleet/release
```

输出两个架构的controller、agent、正式web、规范manifest及本说明/示例；不会签名、启动或安装服务。输出父目录需已存在；runtime安装父目录由运行用户所有、0700，配置/CA/令牌文件0600。runtime必须是新目录，路径为规范绝对路径，仅含字母/数字/下划线/点/斜杠/横杠，以供生成的shell和用户服务安全引用。示例中的域名、IP、nodeId、公钥和指纹均须替换。

## 首次信任与CA

在离线Root机器用`controller pki init-offline -directory /private/offline-root -offline-ack`建立Root；在线主控用`pki request-intermediate -directory /home/serverfleet/pki -authority-id fleet-controller-01`生成CSR。只转移公开CSR，离线机器执行`pki sign-intermediate -root-directory /private/offline-root -request-directory /private/public-request -directory /private/public-bundle -authority-id fleet-controller-01 -generation 1 -offline-ack`。带回公开bundle后执行`pki install-intermediate -directory /home/serverfleet/pki -bundle-directory /private/public-bundle -root-pin 实际指纹`。Root私钥留在离线机器。

Controller的TLS fullchain/private-key用于HTTPS，内部Agent身份由Intermediate签发。两套CA可以不同：provision的rootCertificate是HTTPS根，rootPin始终是内部Agent Root的SPKI SHA256。HTTPS根文件是单个有效自签CA PEM，不能用叶证书/fullchain替代。Agent新增-controller-ca指定HTTPS根，-root-cert/-root-pin继续固定内部身份Root；未提供-controller-ca时保持原同CA模式。Agent域名须直连mTLS，不能由不转发客户端证书的代理终止连接。

## 离线发行签名

在离线发行机器普通用户私有目录执行：

```sh
controller release keygen -directory /private/release-key -key-id release-2026 -offline-ack
```

只分发release-public.json中的公钥，填入两个配置示例的releaseKeys；release-private.key留在离线机器，不放入主控/节点/runtime/仓库或备份。它与Root CA密钥互不混用。

更新版本提高version和sequence，针对组件/架构签署构建工件：

```sh
controller release sign -key /private/release-key/release-private.key -key-id release-2026 -manifest /private/build/agent.manifest.json -artifact /private/build/agent -output /private/signed/0.9.1 -offline-ack
```

新输出目录末级名必须等于版本，父目录0700。工具核对规范manifest、实际长度/摘要、签名与平台，manifest最后发布。整个版本目录预置到对应runtime/inbox/VERSION，或使用固定HTTPS feed。不能混用组件/架构、重写manifest沿用旧签名或降低sequence。首次复制来自本机操作者核对的构建；后续替换仍须签名与审批。

## 主控与首次管理员

复制examples/controller.json到私有配置目录，填真实路径/信任信息；用构建包controller执行：

```sh
controller provision -config /home/serverfleet/config/controller.json
/home/serverfleet/runtime-controller/start
```

provision创建新runtime：独立supervisor副本、slot/active、mailbox/inbox/stage/health、初始化DB/keyring、实际schema摘要、supervisor.json、start和用户服务文件。它不接管已有进程/数据库，不自动启动或安装系统服务。prepared.json是成功准备回执；中途失败保留provision-attempt.json和部分目录，不覆盖重试。

初始化令牌只落入runtime-controller/bootstrap-token.txt，10分钟内在正式网页“首次初始化”使用。stdout/服务日志不打印明文。过期可在本机显式生成新文件：

```sh
controller bootstrap-token -config /home/serverfleet/runtime-controller/supervisor.json -output /home/serverfleet/runtime-controller/bootstrap-token-2.txt
```

仅未初始化身份库可用；不能重置已有管理员、解除恢复/隔离/维护门或覆盖旧文件。建立管理员后，通过用户管理再建立第二位真人管理员用于独立审批；自我审批仍拒绝。使用后由操作者移除令牌文件。

## 注册节点与常驻运行

网页“添加节点”创建短期令牌，放到节点0700私有目录内的0600文件，避免进程参数和shell历史泄露：

```sh
agent -controller-url https://fleet.example.com:8443 -state-dir /home/serverfleet/identity -root-cert /home/serverfleet/trust/agent-root.pem -root-pin 实际内部Root指纹 -controller-ca /home/serverfleet/trust/https-ca.pem -enroll-token-file /home/serverfleet/config/enroll-token.txt
agent -status -state-dir /home/serverfleet/identity -root-pin 实际内部Root指纹
```

审批入网先在网页批准，再显式重新执行注册；已有身份不会自动替换。令牌文件使用后自行删除。填examples/agent.json中的实际nodeId，identityDir指向刚注册的原身份目录，fileRoot/certificateRoot分别预建独立0700目录，证书私钥目录不能进入文件传输根。随后：

```sh
controller provision -config /home/serverfleet/config/agent.json
/home/serverfleet/runtime-agent/start
```

节点parent使用controller的supervise命令，child仍是Agent。生成器从已验证身份链提取内部公开Root，自动生成同策略摘要的agent-mailbox.json，不复制节点私钥或重做注册。运行版本来自各个实际构建，不固定旧-agent-version参数。

前台停止用SIGTERM/Ctrl+C。要开机常驻，把生成的serverfleet-controller.service或serverfleet-agent.service复制到同一普通用户的~/.config/systemd/user/，执行systemctl --user daemon-reload及enable --now对应服务；无人登录运行需主机管理员启用该用户linger。不要同时运行前台与服务，不把Agent整体改为root。服务Restart=no，异常/未知更新先检查保留记录再人工启动。

## 文件、命令和Root配置

主控默认生成runtime/file-deployment.json、0700 file-spool和显式allowEmpty:true的空表，对任何节点文件操作均拒绝。节点注册后，参照examples/file-deployment.json，在原nodeRoots填实际nodeId和logicalRoot（如/files），保留原spoolDir和固定文件路径，再停启同一监督程序。/files是网页逻辑根，映射到该Agent的实际fileRoot，不访问Controller上的/files，也不自动给予用户/AI权限。已有实例不要更换supervisor路径/argv、用provision覆盖原目录或沿用旧摘要伪称策略未变。

普通命令参照紧凑examples/command-profiles.json：固定程序、真实sha256sum、工作目录、前缀参数和数量/超时上限；填nodeId并保持字段顺序，文件0600/父0700。用`agent command-policy -file /private/command-profiles.json -node 实际nodeId`取得公开policySha256，Agent配置增加commandProfiles路径；网页只提交profile/公开摘要/argv/超时。

Root是显式可选配置。管理员将独立Agent副本安装为/usr/local/libexec/serverfleet-agent-root（Root所有0755，祖先均Root所有且普通用户不可写）；/etc/serverfleet和/var/lib/serverfleet-root为Root所有0700。按照root-executor/root-command-profiles示例设置Root侧0600配置、内部Root、HTTPS根、同一已注册nodeId的完整客户端证书链及匹配私钥。私钥由管理员本机处理，不在线上传或经对话传递。示例仅固定df -h；实际profile逐一限制程序/摘要/工作目录/前缀参数和上限。

Root环境运行安装的helper的command-policy子命令取得Root policySha256；填root-helper-registration.json中的sudo/helper真实SHA和该摘要，把注册表放到Agent用户私有目录，Agent配置增加rootHelper路径。Root侧可选controllerCa也须Root所有0600并通过不可替换祖先检查。管理员通过visudo只允许精确命令，例如：

```sudoers
serverfleet ALL=(root) NOPASSWD: /usr/local/libexec/serverfleet-agent-root root-executor -config /etc/serverfleet/root-executor.json
```

不授予ALL/shell/通配参数。每次Root执行仍读Root侧策略、精确任务/参数/真人审批与mTLS实时复权；更换程序或策略必须重新核对摘要。

## 五项功能的实际入口

| 核心功能 | 操作入口 |
|---|---|
| 普通命令/受控Root | 正式任务页按profile/摘要创建任务，由独立真人管理员审批。 |
| 文件及执行续传 | 正式文件页传输；中断后对已确认前缀创建新恢复审批任务，原UNKNOWN不改成功。 |
| Agent灰度更新 | 正式更新页填版本/keyId/发行摘要及prepared.json中的policySha256；先1台再5台，非成功停止扩批。批内同平台/同策略，目录与参数一致才能共享摘要。 |
| 主控自更新 | 用独立supervisor运行，签名Controller版本放入其inbox/feed，正式更新页审批；备份、停机、schema/健康及失败回滚门保持。 |
| 灾备接管 | 既有recovery prove-key/stage/reconcile/activate/status，使用新目录/Intermediate/权威、离线授权和真实旧parent fence。provision不能覆盖恢复目录或绕过hold。 |

可选feed字段为`{"origin":"https://releases.example.com","addresses":["192.0.2.20"],"rootCertificate":"/private/feed-ca.pem"}`；路径/releases/{component}/linux/{architecture}/{version}/{manifest.json,signature.bin,artifact}，无代理/重定向/DNS回退。初装即需灾备隔离端时，Controller JSON根增加fenceConfiguration指向原私有fence服务配置，生成器冻结SHA；完整字段和恢复流程见仓库docs/development/CORE-PRIORITY-STARTUP.md。不要在绑定后随意改supervisor策略。

代码与联调验收分别交付。准备命令不访问网络，运行后的任务继续受权限、独立审批、审计、签名、防降级、UNKNOWN不重放及实时隔离门约束；真实提权/供应商/故障/物理双机验证由操作者执行。

## 异地备份与人工处置

R29支持在正式备份页配置S3兼容／WebDAV HTTPS密文上传、定时备份和保留清理。先配置上述加密备份部署与离线密钥确认，再添加 `backup_storage_secret` 凭据（模块 `backups`）；策略只保存引用。详见[操作说明](../docs/development/BACKUP-OPERATIONS.md)。自动清理选项创建独立审批任务；UNKNOWN操作通过只读核验与任务中心人工处置闭环，不自动重试。
