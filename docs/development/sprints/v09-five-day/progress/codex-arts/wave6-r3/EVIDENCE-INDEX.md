# Codex Arts · 证据索引（Wave6-R3）

所有原始输出位于本工作区被 Git 忽略的 `reports/`。提交只含源码与说明，不含截图、数据库、私钥或二进制。

| 检查点 | 证据 | 结果 |
|---|---|---|
| CP-1 AI 预算界面 | `reports/web-tests.log`、`reports/web-junit.xml`、`reports/web-typecheck.log`、`reports/web-build.log` | `check:web` PASS；Web 345/345（含新增 4 个预算用例）；`tsc --noEmit` PASS |
| CP-2 B01 | `reports/positive-b01.log`、`reports/positive-w6r3-b01/positive-b01-transfer-completed.png` | 23 PASS / 4 BLOCKED / 0 FAIL；B01 与 P20 PASS |
| CP-2 构建 | `reports/fixturehost.exe`、`reports/linux-fixturehost`、`reports/linux-agent`、`reports/linux-controller` | Windows 与 linux/amd64 构建均 exit 0；`go vet` exit 0 |
| 失败保留 | 本轮第一次正向运行（协议 minor 不一致）输出保留在 `reports/positive-b01.log` 的滚动历史与对话记录中；未删除失败 | 失败原因：Controller minor=1 而 Agent 未声明文件能力 |

## 判定口径

- 500/401/403/错误 shape/超时按 FAIL；明确预期拒绝只在对应负向步骤通过。
- 202/queued/preview/check 不算成功；B01 只认 transfer=completed、任务 completed、双方执行 succeeded、目标 SHA 与源一致。