# 总控接回与 awskr 实机基线验证

日期2026-11-01（Asia/Shanghai）；会话codex-review-02；代码基线2da203cac40ccc2bfc4c7bb644978005cd99dab1。负责人明确由本会话重新总控，并授权使用本机SSH别名awskr作Controller测试节点。独立完成审查已接收，实际业务缺口未因此关闭。

## 已完成

1. 严格已知主机校验、非交互SSH成功；只使用已保存认证，不读/复制/打印私钥。主机Debian13.6/Linux amd64、2CPU、约4GB内存、约70GB可用空间；Go1.27.3、Node24.22.1、Python3.13.5、systemd与cgroup v2可用。
2. 发现已有serverfleet-controller/serverfleet-agent、nginx、MySQL等服务及80/443/8443等监听；已有18443健康接口返回v0.9-wave5。本次不替换它们，不读取既有数据库/秘密，不改服务/站点/防火墙，不创建AWS资源或操作其他SSH别名。
3. 在普通UID1000的私有`~/.local/share/serverfleet-qa/control-20261101-cx02-bd9cc2c8`复制固定代码SHA的范围源包和Linux测试二进制，传输后逐文件核验大小/SHA256。源码只含apps/internal/gen/go、go.mod/go.sum及platform测试，未上传SSH密钥或真实业务凭据。
4. 在该独立目录运行真实`TestWave5ControllerAndAgentCLIFullLoopWSSAndTask`，**PASS，103.80秒**（外层记录103.84秒，含首次构建）。实际启动临时Controller与Agent，完成初始化/登录、注册审批/签发、WSS+mTLS、心跳/指标、固定self-check任务执行和结果/审计。监听动态127.0.0.1端口，临时数据库/PKI/账号为测试生成；Go源依赖使用独立缓存、GOMAXPROCS=2/GOMEMLIMIT=256MiB控制构建压力，未关闭校验。
5. 测试后既有Controller/Agent/nginx/MySQL仍active；测试启动的应用由测试清理，QA源包/日志保留复验。该103秒结果不是压力/P95数据，不证明1核1GB、arm64、跨机文件、常驻Agent、通用/root运维、Xray、更新或灾备完成。
6. 已明确新授权边界、唯一总控、Wave6三线任务及各自全新目录。旧设计/作者/接任者历史全部保留，未强推/重置/自动stash/启动其他Agent或代发消息。

## 失败与修正记录

- 接收审查分支时TASK-BOARD新增行发生冲突，逐行保留“本次接管”和“独立审查完成”两条任务后提交，不整体覆盖任一侧。
- 一个只读SSH探针的本地PowerShell嵌套引号解析失败，尚未执行远程命令；改用Python从标准输入读取脚本，未更改SSH配置或削弱主机校验。
- 首次远程解包校验拒绝tar根目录条目（apps/internal/gen/tests未带尾部斜杠），在任何提取/测试执行前退出；补齐这四个明确目录名并要求已有src仍为空，保留原拒绝日志。后续仍拒绝链接、绝对路径、上级逃逸及非指定源目录。
- 首次tools:install使用不含完整sumdb记录的应用缓存，访问sum.golang.org超时。改为现有工具专用缓存重跑，未关闭GOSUMDB或安装脚本策略；业务门禁使用已完整下载的应用缓存。实际结果以RESULTS.json与原始日志为准。

## 交接

本次本地门禁已实际完成：npm ci、固定tools:install（缓存重试后）、npm run check五组及check:security均PASS；Go636顶层+1134子测试、Web329项，0FAIL，Windows仍有9项SKIP。awskr实际Linux CLI测试PASS单列，不将其扩成全量Linux或全量产品验收。原资料69/候选3份校验保持，未改业务代码/依赖/原契约。

[授权与机器边界](../sprints/v09-five-day/CONTROL-RESUME.md)、[Wave6三线任务](../sprints/v09-five-day/WAVE6.md)已登记。CX优先安全密钥/Agent能力与平台执行器，DS优先真实文件与供应商/发布链，GM补全量正常成功路径及AC证据。apps/controller与共享契约由总控单写；作者不直接操作awskr，提供精确SHA/脚本由总控统一验证。

三作者新目录先从3bfb9e6准备，最终交接时仅在仍干净时快进到本轮已验证main，给共同精确SHA；不会覆盖旧作者未提交内容。此轮主要新增接管资料与实机证据，没有在本次修改业务源码，独立审查发现的硬编码密钥等仍是第一优先待修项。
