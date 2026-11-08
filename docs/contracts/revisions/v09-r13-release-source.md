# R13 可信在线Agent工件获取候选

V09-CLOUD-CODE-COMPLETE-01，UNTESTED：按用户要求整包写码后统一验收，不以历史CI替代。原候选/Proto/gen/根锁不改；R12新增Controller目标的schema复用。

- 私有启动 `-release-source`仅在provisioned PKI与既有可信-update-deployment下启用。固定HTTPS origin、明确允许的IP、可选公开CA文件；版本、组件/架构/公钥/序号下限/收件箱/暂存目录来自更新启动配置，HTTP不给URL/令牌/路径/密钥/脚本。
- GET source只读当前管理员可见公共版本/配置SHA/host，不访问网络。POST acquire只接受幂等编号，SubmitController创建统一任务；AI/plugin/一般task-create不准本地目标。没有第二调度器/Agent SSH执行或默认源。
- 实际下载只在worker中执行，TLS1.3、固定地址/Host/SNI、无环境proxy/redirect/compression/cookie/authorization；元数据先验Ed25519/严格canonical manifest，再流式实际字节/长度/hash，暂存后再次验证并写入版本专属新目录。manifest最后发布，目录/文件均EXCL；已存在/部分版本不覆盖。当前权限/审批/运行门/计划/序号随每次IO核对。
- 获取与安装分开：工件可用不构成安装、版本健康或回滚成功。安装仍需既有已批准冻结计划和实际Agent进程。没有把本地Controller管理的单Agent更新称作完整远程多Agent批次。
- 追加cx/0014_release_acquisition，预计正式33项。未知发布/提交/重启保留unknown和私有材料，不自动重放；未把真实外部网络访问授权带入本次开发。

最终集中验收：无部署/非管理员/AI/一般HTTP本地目标拒绝；来源替换/redirect/代理/DNS/IP/SNI/CA/压缩/超长/旧序号/签名/字节不符拒绝；撤权/取消/重启和部分目录不覆盖；正常实际本地TLS源→统一任务→签名收件箱→单独批准安装，Web对应真实状态。外部发行源/签名运营与完整B05继续开放。
