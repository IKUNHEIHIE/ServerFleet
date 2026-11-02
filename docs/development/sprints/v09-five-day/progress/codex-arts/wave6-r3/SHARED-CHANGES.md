# Codex Arts · Wave6-R3 共享变更

本分支只写 `apps/web/src/**`、`tests/browser/v09/**` 与本进度目录。

## 已提交

- 无公共 OpenAPI/Proto/gen/DDL/根依赖/锁/CI/TASK-BOARD 改动。
- AI 预算界面只消费已接收的 `GET/PUT /api/v1/ai/model-settings/{profileId}/budget`，未新增或改写后端契约。

## 需要总控协调的事项

- 暂无。若后续 fixturehost 需要生产侧新的程序注入端口，将在此登记精确 patch、最小失败用例与兼容/安全说明。