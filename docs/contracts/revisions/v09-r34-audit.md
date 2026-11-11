# R34：审计、现行文档与兼容整理

基线 main8aa1ac0 / PR24，任务V09-CLOUD-AUDIT-01，主控chatgpt-audit-20261112。2026-11-12。

## 新增

现行功能矩阵、API安全/字段/状态说明、Go AST路由声明与OpenAPI候选匹配索引（精确/兼容别名分开标注）、自动漂移检查、项目结构和审计报告。独立runtime-additions-candidate补充原先未描述的令牌/工作流等14项method/path，不覆盖旧候选或冻结G2；旧别名链接既有规范操作。

## 修改

- 手动探测按服务端1—300秒目标上限，为该HTTP响应设置310秒期限，浏览器315秒；原目标校验、节点30秒限制、权限/配置/取消与UNKNOWN语义不变。SetWriteDeadline失败时拒绝探测；无deadline支持的测试记录器不设真实socket期限。普通请求期限保持。
- Web传输兼容解析拒绝冲突的path/sourcePath、state/status、bytes/bytesCopied，避免静默选择互相矛盾的结果；相同别名仍兼容、缺失数值不补零，旧无效可选bytes规则保持。
- Controller大入口按迁移/权限目录、路由、身份读门和适配器拆成同包文件。导出签名、调用顺序、HTTP路径、历史迁移SQL/checksum/原型/Proto/gen和依赖锁不变。

## 删除

无生产/测试调用的内部Web InlineError/FieldErrorList/StatusDot/useSearchParams/CHECK_UPDATE_MESSAGE/formatDate/formatCount/isoNow，以及composedNodeLookup.now/composedEligibility.now两项仅赋值的私有字段。没有删除用户功能、历史证据、兼容入口、公开Go端口或未准入插件。

## 审查与边界

安全关键路径人工抽查加全仓静态/自动检查；范围/发现和保留风险见[审计](../../audits/2026-11-12.md)。受限令牌Bearer读取与Xray生产执行缺口显式登记，不通过删除或默认放行来隐藏。最终精确head CI/COMMENT审查/正常合并回执写PR评论，禁止用旧基线绿灯覆盖本次变更。无新第三方依赖、运行时、生产/VPS/供应商访问或G2/发布认证。
