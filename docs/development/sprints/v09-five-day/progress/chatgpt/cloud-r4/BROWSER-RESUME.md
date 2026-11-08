# 正式Web持久续传检查点

V09-CLOUD-BROWSER-RESUME-01 / chatgpt-takeover-01；基于已接收main f1d715b，独立CI42458157478 success。实现/批量检查进行中，最终接收依PR精确head及实际CI，不提前宣称完整产品验收。

- 只保存账号/节点隔离的待核验文件身份，不保存成功、偏移、字节或认证秘密；重新选择原文件校验完整摘要，再显式读取当前服务端绑定和整数偏移。1MiB上限分块，确认精确下一偏移后才继续。
- 已受理/终态/未知只读回，不重复complete/另造任务；网络/409不自动重试。每次await和写入前重检当前scope/权限；捕获确切node/upload id，重复选择锁定同一操作，旧世代结果不可覆盖新403。
- R10显式resumeBinding=true才返回原始目标/摘要；缺省/false与PUT保持R8封闭响应，旧候选/69原文/Proto/gen/锁/CI/迁移SQL不改。新增字段只在owner/当前节点权限通过后返回；响应no-store。
- 首批类型检查及文件Web模块66PASS。HTTP模块首轮79顶层PASS/1FAIL/1SKIP：新专项误把已有owner mismatch的409当403；实际无数据泄露。修正断言遵守原语义，撤权仍严格403，并新增缺省响应不变、重复/无效参数拒绝。未删用例/改既有错误分类。
- 会话续接曾使本地回到旧快照，尚未提交改动需恢复；已改为尽早推送工作分支检查点。检查点不触发每处修改的产品全套，集中模块复验后仅最终head完整CI。
- 本地只有UID0、没有Chrome/Chromium可执行程序；DOM夹具不是实际浏览器/独立Agent证明。普通用户实际产品组合交原生CI；真实浏览器/跨Controller重启组合及远端执行续传仍须后续证据，不因此关闭G2/B03—B06/外部/ARM/硬1C1G等关卡。

兼容性审查发现R8的additionalProperties=false：改为显式query opt-in返回绑定，默认/false/PUT响应保持旧形状；新增无效/重复参数和默认不增字段的HTTP验证。待确认任务缓存也按账号隔离，旧匿名缓存不自动迁移；原403/乱序/旧节点测试仅更新夹具账号键，原触发和断言不削弱。

一次合同检查的工具回收被自动审批拒绝，原因是Redocly遥测可能向未信任第三方发送私有项目元数据；不绕过拦截。已核对仓库TOOLCHAIN及固定安装版本的官方CLI代码，REDOCLY_TELEMETRY=off与resolvedConfig.telemetry=off均关闭遥测；以环境开关重跑同一校验，并在现有redocly.yaml设telemetry: off保证后续CI也不发送。没有关闭lint规则、改CI或降低门槛。

## 最终本地集中验证

- 固定Go1.27.3、Node24.22.1、npm11.9.0；npm ci/tools:install完成，无新增依赖/锁漂移。
- npm run check:web：44文件/411PASS，类型检查与Vite生产构建成功（已有体积警告保留，不调大阈值隐藏）。新增模块/DOM覆盖重新挂载后的断线恢复、精确1MiB偏移、错文件/绑定/摘要、403/网络/409、重复选择、旧节点/账号隔离和已受理不重放；DOM不冒称真实浏览器。
- Go go test ./internal/files -json -count=1：80顶层PASS/1既有SKIP/0FAIL；真实HTTP绑定、撤权403、owner mismatch409、不泄露、缺省/false旧投影及重复/无效参数均验证；staticcheck ./internal/files退出0。
- check:policy、REDOCLY_TELEMETRY=off check:contracts、check:security、R10独立Redocly lint均退出0；npm audit0、govulncheck无已知漏洞；原69文件与3字节候选校验通过，git diff --check干净。自动审批遥测拒绝记录保留，关闭遥测后同一校验成功，不绕过门槛。
- 最终head的完整普通用户CI与逐项代码复核待PR进行，未提前标合入/全量发布通过。README/架构/进度已同步上一轮#12/#13接收与本轮实现边界，不因文档更新关闭发布缺口。

## 代码复核追加的实际安全修正

最终候选4fcf163的CI已经启动后，逐项复核发现既有用户上传的conflictPolicy被解析却忽略，worker实际上固定overwrite，旧Web却发skip。本轮合入前修正UI明确覆盖、后台拒绝本入口尚未实现的skip/rename/未知策略；独立传输的三策略保持。这是实际数据安全问题，不为旧head碰绿灯或删用例；新head须重审并复验。旧默认请求不改，新HTTP/DOM回归证明不静默改策略。同步R10/产品差异，完整上传三策略仍登记缺口。另修sessionStorage.getItem故障返回未核验空列表而非令页面崩溃，增加故障专项。

复核补丁集中复验：全Web412PASS/44文件，类型/生产构建成功；Go文件模块81顶层PASS/1SKIP/0FAIL；R10独立lint、staticcheck及diff检查通过。新策略HTTP专项包含skip/rename/unknown明确拒绝、overwrite/省略默认仍创建，DOM核对实际请求overwrite及覆盖文案。原Full-CI候选4fcf163不是最终接收依据；此次追加修复有实际安全依据，新head再进行完整CI，所有旧运行/取消状态保留。

## 完整CI实际失败与最小修正

b03b737 / run42477852601：dependencies success；Go973顶层PASS/7SKIP/0FAIL、Web412PASS；foundation因新增R10的InvalidRequest内联description含逗号却未引号，YAML将其解析成额外属性，Redocly struct规则报2错。该新增契约格式错误已修为带引号字符串；不改规则/产品代码，不删除失败。后续生成/锁漂移检查和交叉构建因前步失败跳过，不冒称已执行。只针对变更补完整契约检查，本地已通过Go/Web不重复；新head完整CI仍须通过后才能接收。
