# 正式控制台与核心构建装配

V09-CLOUD-DEPLOY-IMPLEMENT-01 / chatgpt-takeover-01；feature/cloud-deploy-assembly-20261108 / ServerFleet-deploy；IN PROGRESS。从#15精确候选b4305fed继续，main起点a8a093c；#15合入前属于明确候选依赖，不混称已由main提供。用户手动验证，主控优先实现，不启动其他Agent。

现有正式Controller仅提供API/Agent路由，不能把测试fixturehost当正式Web宿主。本包增加-web-dir私有构建资产装配，保留同源TLS、API与实时quarantine门；只读不可变有限资产，拒绝源树/秘密文件/链接/目录枚举/未知API回退。另补仓库外核心候选构建脚本，固定工具、干净源码、双架构实际构建、版本一致/规范待签manifest/字节清单；无密钥/签名/可选插件/部署行为，构建失败保留INCOMPLETE状态，成功需正向完成回执。详见PRODUCT-STARTUP。

本包集中后检查，不逐小改动重测；真实普通用户用原生CI，手动浏览器交用户。编译包不是验收/签名发行，ARM交叉编译不是实际运行。B03—B06/完整发布checker/ACME与本地CSR/可信更新批次/加密灾备与fence/G2/发布准入/外部/物理/硬1C1G仍OPEN；无生产/VPS/awskr/真实外部账号/云API授权。


#15已接收：main6031c1f9d9b2b840ef5a477520aa937799bb3ba5，精确head b4305fed，CI42485042788 foundation/dependencies success；Go983顶层/1301子项PASS、7SKIP、0FAIL，Web422PASS。实际三策略上传/真实路径/字节、29迁移与安全负向已核验；COMMENT5818107752并expected-head合并。当前分支已正常merge共享main，未强推/直接推main。

首批模块编译发现新测试多余net/http import，已删除。首次候选构建与主线同步启动顺序未等待，造成manifest源SHA与后三个程序VCS记录不一致；该包明确补INCOMPLETE拒绝接收，原始输出保留。构建器增加每个程序前后与最终源head/dirty检查，并核对每个真实程序go version -m的vcs.revision/modified，杜绝漂移伪称同一发行源。没有通过改manifest/SHA或忽略失败来接收旧包；后续使用独立新输出目录集中复验。


第二批集中检查：policy通过；构建来源保护及双架构候选生成通过（源82cb843），但Go组合专项发现GET /与既有methodless API mux模式冲突导致启动panic，未接收该运行包。改为仅对明确静态路径在RuntimeHTTP内选取静态handler，原API mux和方法/404语义不改。补旧health POST=405回归，原panic输出保留；修复后使用新目录再构建，旧目录保留INCOMPLETE拒绝使用。


第三批模块PASS，源931702f的双架构程序/Web已实际构建，但跨命令检查仍观察到INCOMPLETE负面标记，未靠忽略标记宣布包可用。构建器改为始终保存build-status.json，只有真实完成后写入BUILT_UNSIGNED_UNVERIFIED正向回执，并绑定源SHA和manifest摘要；不以“某文件不存在”作为成功证据。最终新目录复验后再记录候选。
