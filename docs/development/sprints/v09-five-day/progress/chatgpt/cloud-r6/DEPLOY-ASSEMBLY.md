# 正式控制台与核心构建装配

V09-CLOUD-DEPLOY-IMPLEMENT-01 / chatgpt-takeover-01；feature/cloud-deploy-assembly-20261108 / ServerFleet-deploy；IN PROGRESS。从#15精确候选b4305fed继续，main起点a8a093c；#15合入前属于明确候选依赖，不混称已由main提供。用户手动验证，主控优先实现，不启动其他Agent。

现有正式Controller仅提供API/Agent路由，不能把测试fixturehost当正式Web宿主。本包增加-web-dir私有构建资产装配，保留同源TLS、API与实时quarantine门；只读不可变有限资产，拒绝源树/秘密文件/链接/目录枚举/未知API回退。另补仓库外核心候选构建脚本，固定工具、干净源码、双架构实际构建、版本一致/规范待签manifest/字节清单；无密钥/签名/可选插件/部署行为，构建失败保留INCOMPLETE。详见PRODUCT-STARTUP。

本包集中后检查，不逐小改动重测；真实普通用户用原生CI，手动浏览器交用户。编译包不是验收/签名发行，ARM交叉编译不是实际运行。B03—B06/完整发布checker/ACME与本地CSR/可信更新批次/加密灾备与fence/G2/发布准入/外部/物理/硬1C1G仍OPEN；无生产/VPS/awskr/真实外部账号/云API授权。
