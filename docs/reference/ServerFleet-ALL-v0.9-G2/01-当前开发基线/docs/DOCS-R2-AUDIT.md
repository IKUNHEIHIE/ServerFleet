# docs-r2 自动化审计及交付边界

**日期：** 2026-10-29
**审查种类：** 文档勘误与契约一致性；**不是**真实 Controller/Agent、权限或 Xray 运行测试。

**主契约：** 119 paths / 151 operations / 132 schemas。
**Xray 契约：** 19 paths / 23 operations / 30 schemas。

## 自动检查结果

- 通过：现行 PRD 不再声称第一版 0 个获批插件
- 通过：明确唯一批准 Xray 插件（未实现）
- 通过：现行 PRD 无错误旧交付物引用
- 通过：一级功能章节 1–18 连续且无重复
- 通过：插件章节 11.1～11.5 连续且无重复
- 通过：AI 完全访问不豁免 root Shell 强制管理员确认
- 通过：官方仓库实际 URL 未虚构
- 通过：在线 URL 未误声称 v0.9 已部署
- 通过：全部内部 Markdown 相对链接有效（缺失=[]）
- 通过：主 OpenAPI 原始字节与 v0.9 一致，无接口语义改动
- 通过：Xray OpenAPI 原始字节与 v0.9 一致，无接口语义改动
- 通过：Xray 正式需求细节原文未被覆盖
- 通过：v0.9 交互原型原始字节保持不变
- 通过：Plugin 目录只有 Xray 设计清单相关条目
- 通过：Xray manifest 存在且可解析
- 通过：main 所有 operationId 唯一且非空
- 通过：main OpenAPI 内部 $ref 均可解析
- 通过：main OpenAPI 共享路径参数均声明为 required path 参数
- 通过：xray 所有 operationId 唯一且非空
- 通过：xray OpenAPI 内部 $ref 均可解析
- 通过：xray OpenAPI 共享路径参数均声明为 required path 参数
- 通过：主 API 数量与审查基线相同
- 通过：Xray API 数量与审查基线相同
- 通过：主契约 Xray 路径集与独立插件契约一致

## 文件改动说明

- v0.9 PRD：勘误与增补**非产品扩展**类文字，详见 `docs/DOCS-R2-CHANGELOG.md`；旧版文件留在原始 v0.9 档案中。
- OpenAPI：两份原始字节保留；主 SHA-256：`05fb9b826d305c28b73c7226692f1600346231045b25cc8868f50068c21e6db9`。

## 尚未解决、需要下一阶段批准

- ADR-PLUGIN 编译期内置后端 vs 插件按需热安装。
- ADR-DEPLOY 只接管 Nginx vs 新建网关自动安装。
- AI/PRIV 对任意 root Shell 的强制管理员确认与完全访问的具体策略映射。
- 认证/RBAC/审计、Agent Protobuf、插件统一 Schema 等 API 缺口。
- 当前线上预览地址的实际版本未核实。

## 不可宣称的事项

- 没有证明已部署真实主控/Agent 或 Xray 二进制。
- 没有进行运行中插件热加载的安全/性能验证。
- 没有进行真实节点端到端测试、外网可访问性检查或任务回滚测试。

## QA 总结

**结构检查：24/24 通过。** 无发现的结构性阻断项；G1/G2 仍待正式评审。
