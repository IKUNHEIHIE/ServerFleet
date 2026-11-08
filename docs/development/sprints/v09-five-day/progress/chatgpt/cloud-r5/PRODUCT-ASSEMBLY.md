# 正式产品装配连续实施

任务V09-CLOUD-PRODUCT-IMPLEMENT-01，唯一chatgpt-takeover-01，feature/cloud-product-assembly-20261108，ServerFleet-product；起点main a8a093ccfaecc35c1a58cadbef3a675b57fdf035，独立main CI42480445288已核验success。用户负责手动验证，主控优先实现；必要自动检查按整包集中运行，不逐小修改重跑。尚无本轮合并/完整产品通过结论。

## 本包实现

- 用户上传三策略：API/持久冻结/统一任务worker/正式页面一致；skip不改旧文件、不伪造远端内容；rename受限同目录并保存实际路径；202仍非完成。显式R11 bindingVersion=2，原R8/R10形状与旧overwrite计划保持。29项迁移；真实旧28升级与旧25fixture保留历史SQL/hash。
- checksum拒绝：事务内状态和审计提交成功再删暂存，审计失败保留字节；成功/skip/取消清理遵循同一提交顺序。无可靠skip/rename回执只得到task completed时UNKNOWN、保留暂存，不重放。
- cleanup正式Controller装配DNS cleaner/后台循环/关闭生命周期与quarantine门，当前原owner权限，不建高权限服务账号；严格TXT/ID/FQDN/值匹配。仅明确未执行的错误才有界重试，其他错误/超时/lease失联UNKNOWN。HTTP取消优先、claim不重放保持。
- 可信更新增加私有启动配置与-update-deployment，注入现有ProductUpdates签名inbox/实际进程所有权/schema/健康/回滚链。校验路径/来源/公钥/版本与重复JSON/未知字段；不从Web接收执行路径/密钥。不等于在线发行、Controller更新、canary/batch或B05全验收。

启动字段见[PRODUCT-STARTUP](../../../../../PRODUCT-STARTUP.md)，接口差异见[R11](../../../../../../contracts/revisions/v09-r11-upload-policies.md)。不更改原文/旧候选/Proto/gen/根锁/CI，不添加外部依赖。Context7未提供，本包为内部装配与现有固定标准库使用，无新SDK/CLI引入。

## 批量检查记录

npm ci、tools:install已执行成功；Go1.27.3/Node24.22.1/npm11.9.0（项目要求npm11，说明历史记录11.19.1，不冒称完全相同）。本地只有UID0，不将非root实际程序专项SKIP计为通过；由普通用户原生CI执行已有实际Controller/Agent/Nginx专项。手动验收交用户，不虚构浏览器/外部供应商证据。

首批集中模块检查发现脚本编辑误改了分发方法签名以及Field children渲染约定，导致Go编译失败/TS失败/Web397PASS+21FAIL；已修复具体原因，不删除用例。该批在65c636f加未提交修改状态运行，未捕获完整dirty树，不能冒称精确commit可重现的最终证据。日志压缩保留在evidence/initial；最终结果以最后精确head CI及审查记录为准。

## 继续项和关卡

Standalone发布checker/完整部署、node-local CSR/ACME、更新固定可信发行与canary批次、加密一致备份/一次性恢复CLI/新权威fence、普通任务真实降UID/GID/清附加组、Xray安全工件与Runtime仍未全部完成。恢复的四ADR原文已在#13接收，不再误称缺失，但实施差异继续OPEN。

B03/B04/B05/B06、G2/D01—D07/P1-C10/发布准入、真实外部/物理双机/ARM运行/硬1C1G/ENV-W6-01不因换平台/CI/手动验收安排关单。不访问生产/VPS/awskr/真实供应商/云API，不启动其他Agent，不承诺原定交付时间或完成百分比。


第二批本地整包：policy/contracts/git PASS；security PASS（npm audit 0、govulncheck无已知漏洞），Go/Web FAIL。迁移计数/旧fixture排除表遗漏新ds/0043和三个Web精确URL断言未带v2参数已修复，保留原断言语义。Go另有6项Agent文件及1项恢复测试在本地UID0被生产普通用户门拒绝，未删除/改SKIP/放宽；实际Controller/Agent/Linux仍须原生普通用户CI。Web418PASS/3FAIL（新增三策略选择用例已通过）。该批在aea5f90加Web未提交改动运行，精确dirty树未捕获；最终证据只认最终head原生CI。原输出见evidence/local-batch。
