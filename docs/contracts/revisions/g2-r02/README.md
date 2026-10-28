# G2-R01 · g2-r02 独立评审候选

日期：2026-10-29。状态：**REVIEW-CANDIDATE / G2 NOT FROZEN**。作者会话：`codex-01`。

## 任务与边界

- 任务 G2-R01；分支 `contract/g2-review-cx01`；起始提交 `15814c67beddad0172fac2440fc3f45f502bfcad`。
- 实际目录 `D:\admin\desktop\codex\myproject\ks.worktrees\contract--g2-review-cx01`。用户指定路径优先于任务建议中的 `<root>/.worktrees/`。开始时该路径/分支不存在，已从指定提交创建并核对；未在 main 写业务文件。
- 写入范围仅 `docs/contracts/revisions/g2-r02/`、`api/openapi/revisions/g2-r02/`、`tests/contracts/g2r02/`。安装依赖、报告和工具缓存为本 Worktree 忽略文件。
- 审查人：Codex 安全审查人、通信/任务 owner；产品/安全决定由项目负责人批准；仅集成人可合并。作者不是独立安全审批人。未启动其他 Agent。
- 停止条件：兼容矩阵、独立候选、正反测试、实际证据及待决事项齐备后创建本地提交并停止；不合并、不推送、不开发业务、不宣告冻结。

## 交付导航

| 文件 | 用途 |
|---|---|
| [COMPATIBILITY.md](COMPATIBILITY.md) | 认证/错误/分页/幂等/任务/revision 的兼容、迁移与回退 |
| [REVIEW-C01-C07.md](REVIEW-C01-C07.md) | C01～C07 结论、来源和阻塞事项；区分 P1-Cxx 与 G2-Cxx |
| [OPERATIONS.md](OPERATIONS.md)、[operations.json](operations.json) | 全部旧151项及新34项的逐操作核对；成功schema传递变化、认证前后、错误状态、前置条件和哈希 |
| [PROTO-REVIEW.md](PROTO-REVIEW.md) | P1 M0-P1-r1 源/生成代码/线格式/安全缺口审查 |
| [VALIDATION.md](VALIDATION.md) | 命令、实际结果、失败修复及未验证范围 |
| [shared-file-changes.patch](shared-file-changes.patch) | 提交总控的具体共享文件差异，**尚未应用** |
| [独立 OpenAPI](../../../../api/openapi/revisions/g2-r02/merged-candidate.yaml) | 147路径/185操作，可独立 lint；全部仅候选 |
| [隔离检查入口](../../../../tests/contracts/g2r02/run.mjs) | 显式 lint/ref/ID/路径/兼容样例/Proto 检查，不依赖旧脚本自动发现 |

## 候选依赖与使用条件

来源为字节保留的三份工作候选、现行原始 `ServerFleet-API-Contract-v0.9.yaml`、G1 Accepted 和有效历史 BOOT/TASK/AI 条款。原始 `g2/G2-OpenAPI-ChangeSet.json` 的151项与实际源操作逐项对照；Xray 23项已经包含其中，不重复计数。修订不是新的插件包。

新文件完整描述一种**显式选择**的 `g2-r02` profile；它有认证/前置条件/错误及状态的破坏性差异，不能原地替换现有候选，也没有生产路由实现。兼容 profile 的安全门槛、旧任务读取和关键缺失 ADR 尚未批准，不能据本文件开启真实安全闭环。

auth/tasks/nodes/audit/transport 的现有临时端口已只读检查：仅有最小 SubjectID/ActionID/NodeIDs/Event/Envelope 签名，没有会话、审批版本、计划摘要、幂等持久化和状态存储能力。不能把候选 DTO 硬塞进这些端口后声称已统一授权。所需端口扩展必须另交 owner 审查。

## 集成和回退

本提交仅新增隔离工件，回退可由集成人不采纳本提交，原候选和实现仍保持基线。未来若采纳 profile，须先做支持探测、客户端显式升级和真实服务端拒绝测试，禁止自动降级重试写操作。已创建新状态任务后不能通过回退数据库/压缩状态恢复旧客户端；保留唯一任务记录和只读兼容适配。共享文件 patch 待总控应用和复验。
