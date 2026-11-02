# V09-R6-CX-04 / codex-06

审查 codex-review-02；原分支/目录已核对，干净起点 db3cf87d44022be79bbec55eb4012b95f307dac7。已读 R3、R2-SHARED 和本轮总控审查；24项迁移/五份旧patch均已正式接收，不再重放。沿用已授权目录，不写 DS gateway 或 GM Web/宿主；无 main合并/推送/SSH/awskr/云/真实外部秘密/其他Agent/消息。

第一增量：internal/files/deployment*、spool owner检查、新增server/file_deployment.go和实际产品CLI组合验证。启动文件严格version/schema/重复字段及重复nodeID拒绝；Linux配置0600/单硬链接、父目录0700/当前UID、非symlink/打开句柄身份核对，私有spool owner/权限校验。非Linux缺等效ACL证据明确拒绝此加载入口。映射只读一次，非HTTP配置，不提供默认AI/plugin ScopeGate。

中央bootstrap.go只交controller-file-cli.patch：新增-file-deployment，管理TLS模式必需，配置失败在PKI/DB打开前终止，不能退回关闭worker后继续启动。新脚本prepare-wave6-r3-cli只生成这个新候选；不执行R2旧prepare。run-wave6-r3.mjs --final要求干净且不变HEAD。

开发组合PASS（不是固定clean SHA最终证据）：本机WSL UID1000实际Controller CLI +两个实际Agent CLI，HTTP入网/审批/资源授权后重启加载已登记nodeID，65,000目标字节及SHA、父task两节点/transfer完成、第二次skip、未登记路径400和不安全配置启动失败。初始fixture把节点详情路由写成不存在的路径，后又误用state；两次404原始输出已保留，最后核对正式diagnostics路径后PASS。未修改产品路由/放宽断言。files/架构/server模块Windows检查PASS；Linux模块和固定提交复验随后执行。

剩余：本次新CLI共享patch待总控接收（已运行真实候选CLI，不阻塞其他工作）；完整文件策略/上传分发、真实产品更新来源/版本健康与恢复权威/fence、Xray安全工件及正式准入继续推进。程序端口缺来源不再作为全包停止理由。
