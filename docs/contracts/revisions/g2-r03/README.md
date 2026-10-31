# G2-R02A · 节点只读契约决策包

日期：2026-10-29；会话 `codex-01`；审查人 `codex-review-01`。**PROPOSED / G2 未冻结**。

> 集成复审更新：总控已接收8165057评审工件，将r03接入根check:contracts，并按作者提交的具体差异修正两处Web来源注释。原固定生成器只更新provenance的webAdapter哈希，候选YAML/MATRIX字节未变；作者原provenance单独归档。D08技术协调完成，D01–D07和P1-C10仍OPEN，不批准运行时profile或冻结。下文保留作者交付时的工作目录/独立运行记录；最新结果见[总控复审](../../../development/reviews/G2-R02A-REVIEW.md)。

实际工作目录：`D:\admin\desktop\codex\myproject\ks.worktrees\contract--nodes-read-cx01`。
实际分支：`contract/nodes-read-cx01`；开始时 HEAD：`0fef8ba9d42bc50d442721b4ab382af961604e72`，工作区干净。
依据本目录根 AGENTS、README、架构索引、模块归属/依赖规则、TASK-BOARD、第五批审查以及主工作目录第六批任务单。仅在三个授权 r03 目录新增文件。

结论：Merged **已经**有 `GET /api/v1/nodes` / `listNodesForTransfer`，并有可空 CPU/内存字段；Core 只有详情。保留既有列表的路径、operationId、`onlineOnly` 查询、`{items:[...]}` 包裹及必填 `id/name/online`，提议增加可选的权威状态、revision、平台/版本/lastSeen 和采样时间。详情保留 `nodeId/displayName/status/revision` 的必填与原字段类型，仅提议可选指标。旧响应缺失的信息不能通过默认值变成新 DTO。

这是一份只含两个已有 GET 的独立投影，**不能与 Merged 直接拼接加载**，否则重复路径/operationId。若批准，契约 owner 应在新的统一修订中修改原操作的对应声明；本轮没有修改旧候选、r02、Web/Go 实现、ports、Proto/生成物、公共配置、脚本、依赖或锁文件。没有增加运行时 profile、登录或网络访问，没有启动其他 Agent。

| 交付 | 内容 |
|---|---|
| [MATRIX.md](MATRIX.md)、[provenance.json](provenance.json) | 原始/Merged/Core/r02、Web 与 Go 来源哈希；操作、字段与错误矩阵，测试直接重算 |
| [COMPATIBILITY.md](COMPATIBILITY.md) | 字段语义、旧消费者迁移、可见集合与错误/缓存边界、当前实现限制 |
| [DECISIONS.md](DECISIONS.md) | 逐项待批准决定及明确接线阻塞 |
| [独立候选](../../../../api/openapi/revisions/g2-r03/nodes-read-candidate.yaml) | 两个已有 GET，保留原认证与错误媒体差异，可单独 lint |
| [检查入口](../../../../tests/contracts/g2r03/run.mjs) | 直接读取候选、真实示例 schema 校验、真实负向 lint、当前 Go 离线边界调用 |
| [VALIDATION.md](VALIDATION.md)、[RESULTS.json](RESULTS.json) | 固定版本、实际命令、结果、首次失败与修正、未验证项 |
| [shared-file-changes.patch](shared-file-changes.patch) | 提交总控的 Web 注释及根检查接线具体差异，尚未应用 |

独立运行（先使用项目固定 Node/Go 环境，再 `npm ci`、`npm run tools:install`）：

```powershell
node tests/contracts/g2r03/run.mjs
npm exec -- redocly lint --config tests/contracts/g2r03/redocly.yaml api/openapi/revisions/g2-r03/nodes-read-candidate.yaml --format=summary
```

根 `npm run check:contracts` 仅检查原候选和显式接入的 r02，**尚未接入 r03**。新目录通过与否须看上面的独立结果。`build-candidate.mjs --write` 只重建本任务的候选、provenance 和 MATRIX，不改共享文件。

本地提交后停止，交总控/节点/Web/产品/安全复审。不自行合并、推送、开发 HTTP 或宣布冻结。回退方式是总控不采纳这组新增工件，原基线仍可独立使用；没有运行时数据迁移或降级重试行为。
