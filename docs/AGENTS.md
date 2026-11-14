# 文档维护范围

继承根 AGENTS；先读 [文档导航](README.md)与[交接入口](development/HANDOFF.md)。

现行状态在 `development/CURRENT-STATE.md` 汇总，产品行为在 `product/FEATURES.md`，API 入口在 `api/README.md`。更新相关入口，避免另写一个冲突的“当前完成情况”。

原始 reference、批准 ADR、候选、历史验收/审计/任务条目保留字节与当时结论；不得全仓库替换版本、迁移数或历史 FAIL/SKIP。历史文件通过导航说明适用范围；已有可变进度索引可加导航，保留历史正文。归档中的 AGENTS 是来源材料，不给现行源码分配任务。

纯 Markdown 整理只核对文档，不运行完整 CI；代码/配置/生成器变化按影响验证。生成 API 索引须用生成器，不能手调数量。
