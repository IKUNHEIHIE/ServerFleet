# V09-CLOUD-HW-01 · 交接记录

## 2026-11-03 检查点 1

- 分支：`feature/cloud-services-hw01`
- 起点：`1ca79072232bfdcee2b9f4582ac020d4430d0c2b`
- 检查点 HEAD：`4be0f32`
- 内容：修复真实 gateway CI 失败（Unix 下测试夹具使用相对 `C:/temp` 被私有根检查拒绝）。
- 证据：`STATUS.md` 的复现/修复/验证章节；本地 `apps/agent` 全包 PASS。
- 仍 OPEN：Foundation CI 的 `tests/integration/v09/platform` 三项失败不属于本任务写权限；Node/npm 版本尚未对齐；网关工件链与 ACME/cleanup 组合待继续。

## 2026-11-03 检查点 2

- 检查点内容：实际 `GatewayArtifactUploader`（`files.UploadTransport` 端口、重算 SHA、64KiB/绝对路径限制）与 Agent 侧 `newGatewayRegistration`（私有 prefix 推导、缺配置关闭）。
- 验证：新增 13 个用例全部 PASS；`go vet` 无输出；`apps/agent`、`apps/controller/server` 全包 PASS；gofmt 无差异。
- 未接线：`server.New` 默认组合、Agent runtime 参数、真实 `files.AgentFileChannel` 由主控/AgentMore 协调，已在 `SHARED-CHANGES.md` 列出精确接缝。

## 环境注意事项

- 本机以 `service`（uid 1000）执行 Go/Nginx 测试；`root` 仅用于创建私有目录、部署密钥和安装系统级工具，不作为安全验收依据。
- 通过 `ssh.github.com:443` 访问仓库；未在仓库内提交私钥或 token。
- 测试夹具全部在 `/home/service/.tools` 与 `os.TempDir()` 下生成，不共享其他作者缓存或旧 worktree。