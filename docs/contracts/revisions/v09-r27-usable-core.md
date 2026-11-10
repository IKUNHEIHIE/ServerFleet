# R27：核心代码可运行交付

2026-11-10；V09-CLOUD-CODE-COMPLETE-01 / chatgpt-resume-20261110，从9e96f47接续。用户明确本人负责联调和找bug；本轮只补可用源码/启动入口、必要编译及静态检查，不新增或反复执行联调套件。维护清理后已按原Git SHA恢复全部143项差异与16个提交；新增源码已单独保存于feature/cloud-usable-core-20261110，避免只留工作目录。

## 新增本机入口

- `controller provision -config /private/spec.json`：Linux普通用户、新目录/私有父目录、明确公钥/内部Root pin/HTTPS来源及初始工件/manifest。同FD核对长度与摘要，O_EXCL/fsync，不覆盖旧DB/未知安装。生成独立supervisor/slot/mailbox、真实DB schema、固定参数、start和用户service；准备阶段不运行工件、联网、安装服务、审批任务或释放恢复隔离。
- Controller初次准备生成原有10分钟setup token，只写0600文件；`controller bootstrap-token -config ... -output 新文件`显式重新准备。仅未初始化身份库可用，原审计/权威/恢复/维护门保持；不重置管理员，不在线暴露令牌，不打印明文。
- `controller release keygen/sign ... -offline-ack`：离线Ed25519发行密钥与Root CA分开；只分发公钥。沿用原域分隔/规范manifest，实际工件校验后最后发布manifest。更新签名、统一任务、审批、防降级和UNKNOWN门保持。
- `agent -enroll-token-file`：Linux当前用户0700父目录/0600单链接普通文件、同inode读取、有界ASCII；与原明文参数互斥，不把令牌放入进程参数，不自动重试/替换身份或删除文件。

## 信任与配置兼容

可选Agent `-controller-ca`及Root broker的root-owned `controllerCa`只配置HTTPS根。内部`-root-cert/-root-pin`、身份验链、trust generation和节点证书仍独立固定；HTTPS CA来自操作员本机显式配置，单个有效自签CA，保留TLS1.3/hostname/已验证根指纹、无代理/重定向限制。未提供时沿用原同CA行为。没有HTTP响应提供新信任或私钥。

file-deployment新增可选`allowEmpty`，默认false；仅显式true接受0节点表，所有节点/路径仍拒绝，通用NewRootPathMapper空表拒绝保持。provision生成该拒绝式初始表与spool。注册真实nodeId后在同一固定文件中加逻辑根，重启同一parent，不更改已绑定argv/策略，不自动赋予用户/AI权限。原节点上限、重复JSON/节点、路径和所有权检查保持。

构建包附deployment说明和七种模板。Agent参数不冻结旧版本，使用每个签名工件的实际编译版本。固定feed和Controller fence可初装时显式配置；不能用provision覆盖恢复目录或替代activate。

## 交付状态

五项核心主路径代码和正式产品入口已存在；待用户联调不继续混称缺少源码。公开REST/Proto/UI字段、40项迁移/历史SQL、权限和独立审批无变化。本轮未新增/执行测试套件，仅记录实际编译与静态结果；不把编译通过等同实际业务、Root或灾备验证。较低优先级、真实供应商/物理双机/硬1C1G/ARM实跑、Runtime PoC、G2与发布准入仍按各自范围处理。
