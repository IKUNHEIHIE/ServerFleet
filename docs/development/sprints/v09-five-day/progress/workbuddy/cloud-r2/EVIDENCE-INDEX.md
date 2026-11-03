# 证据索引（CP1，2026-11-03 UTC 12:39–13:20）

所有条目均为本次实际执行。日期均为 UTC。命令可重放；未执行的项标注 UNTESTED，不补写。

## E-01 Xray 官方发布元数据

```
$ gh api repos/XTLS/Xray-core --jq '.full_name,.license.spdx_id,.default_branch,.archived'
XTLS/Xray-core
MPL-2.0
main
false

$ gh api "repos/XTLS/Xray-core/releases?per_page=8" --jq '.[]|[.tag_name,.published_at,(.prerelease|tostring)]|@tsv'
v26.10.31	2026-10-31T13:37:42Z	false
v26.10.21	2026-10-21T12:07:11Z	false
v26.9.30	2026-09-30T07:41:08Z	true
v26.9.9	2026-09-08T22:28:10Z	true
v26.7.28	2026-07-28T08:00:45Z	true
v26.7.11	2026-07-11T15:56:30Z	true
v26.6.27	2026-06-27T13:20:31Z	true
v26.6.22	2026-06-22T18:56:38Z	true

$ gh api repos/XTLS/Xray-core/releases/tags/v26.10.29
{"message":"Not Found",...}   # HTTP 404 —— 固定版本已不存在

$ gh api repos/XTLS/Xray-core/releases/tags/v26.6.1 --jq '.tag_name,.published_at,(.prerelease|tostring),(.assets|length)'
v26.6.1
2026-06-01T02:12:09Z
true          # 上游标记为 prerelease
64
```

## E-02 资产与真实 SHA256（GitHub `asset.digest`）

```
v26.10.31  Xray-linux-64.zip             16692690  sha256:e4bd9119f50ae7c41658b1abd0679ae06f98bd864a2907ff33f2f2cf7081b214
v26.10.31  Xray-linux-arm64-v8a.zip      15175093  sha256:1811c8f1da0c3b6dc707ebaf1c8390e987ba39859a8f0a7aadd8526157f2ec8d
v26.10.21  Xray-linux-64.zip             15100223  sha256:0eec3ed4163c273d8c8bf821f7d219e257921cf584aa3fb970715e458e631978
v26.6.1    Xray-linux-64.zip             21347411  sha256:136e822e99e616692550723e8b607cd8858c62a390aea5704938bc27930904ba
```
签名类资产枚举（`.sig/.asc/.pem/.minisign/.sbom/.spdx/.intoto.jsonl`）三个 release 均为 **NONE**。

## E-03 `.dgst` 内容（证明“只有校验和、没有签名”）

```
$ gh api .../assets?name=Xray-linux-64.zip.dgst → curl -H "Accept: application/octet-stream"
MD5= 9f7aaa60ba06e8f60481e019b427a1e2
SHA1= 54ef927c2c17d3fbed9bf6a36833574e0c94c280
SHA2-256= e4bd9119f50ae7c41658b1abd0679ae06f98bd864a2907ff33f2f2cf7081b214
SHA2-512= d507c15ae81f4d658b8bd70070c1e17085b46c0512f4f4030d0e35240bbbe18caecbe7911ecbce679da9a52ea8a410dad03a090ac437594d4feb235648df62ef
```
（299 B；v26.6.1 同结构）

## E-04 来源证明（attestation）

```
$ gh api repos/XTLS/Xray-core/attestations/sha256:e4bd9119...b214
{"message":"Not Found",...}   # HTTP 404，无 SLSA/in-toto
```

## E-05 官方安全说明（v26.10.31 release body 摘录）

> 紧急安全更新：[由于 Linux 内核刚被爆出监听 IPv4 MPTCP 极易被远程拿 root，且 Go 1.24+ 默认开了 MPTCP]，本版本默认关闭了 MPTCP 以显著减小攻击面（**v26.10.29 让位，已被撤版**），用户应尽快升级到该版本……

## E-06 工件字节校验（linux/amd64）

`artifact-sha256`：见 `ARTIFACT-ADMISSION.md` §3 与下方“下载结果”；仅当本地 `sha256sum` 与 E-02 一致才记 PASS。**不执行该二进制**（Runtime 未准入）。

下载结果：**未在本次会话窗口内完成**（16.7 MB，实测约 10–20 KB/s）。因此：
- `sha256sum` 本地比对：**UNTESTED（未完成）**
- 二进制 govulncheck（v26.10.31）：**UNTESTED（未完成）**

> 该项不得用“GitHub 提供 digest”推定本地字节一致；也不是“已扫描安全”。

## E-07 项目自检（本包改动后）

```
$ npm ci                                   → added 127 packages in 4s
$ npm run tools:install                    → protoc-gen-go/staticcheck/govulncheck@v1.9.0/actionlint OK
$ npm run check:web                        → Test Files 40 passed (40) | Tests 361 passed (361)
$ npm run check:security                   → npm audit: found 0 vulnerabilities
                                             govulncheck ./...: No vulnerabilities found
$ go test ./plugins/xray-manager/          → ok（新增 batch/quota 15 项 PASS；3 项真实二进制测试 SKIP）
$ npm run check:go                         → FAIL（exit 1）：7 项环境性既有 FAIL，见 E-08
```

顶层统计（`reports/go-tests.log`）：**899 PASS / 19 SKIP / 7 FAIL**；含子测试 **2113 PASS / 21 SKIP / 7 FAIL**。

## E-08 既有 FAIL 的基线复现（证明与本包无关）

```
$ git worktree add /tmp/pristine cbfde5e        # cbfde5e = 51e0ee5 快照的本地基线提交
$ cd /tmp/pristine && go test ./tests/integration/v09/platform/ \
    -run TestWave6IsolatedSQLiteRestoreKeepsQuarantineAndRevocations
--- FAIL: TestWave6IsolatedSQLiteRestoreKeepsQuarantineAndRevocations (0.10s)
    restore_executor_test.go:59: {  {0  {   0 0}}} recovery: reconciliation rejected 0

$ go test ./apps/agent/ -run TestAgentFileBytesResumeHashAndIndependentReadGate
--- FAIL: TestAgentFileBytesResumeHashAndIndependentReadGate (0.00s)
    file_capability_test.go:32: agent: file capability rejected or outcome unknown
```
结论：纯净基线同样失败 → 本容器 root/权限环境限制，非本包引入；未跳过、未删用例。

## E-09 Web 上传任务闭环（组件级）

```
$ npx vitest run src/features/files
✓ src/features/files/upload-queue.test.ts (11 tests)
✓ src/features/files/files-page.test.tsx  (6 tests)
Tests 17 passed (17)
```
覆盖：202+`dispatching`+taskId 只显示“已受理（排队）/任务 task-9”且**不**显示“上传完成”；点击“读回真实状态”后 GET 返回 `completed` 才显示已完成；重复读回仍 `dispatching` 时保持未完成；403/409/413/503/摘要/UNKNOWN 文案。

## E-10 未执行（如实保留）

- 浏览器 Live/Stub 专项（Playwright/fixturehost）：**UNTESTED**（本检查点仅组件测试）。
- AC-01—24 严格矩阵重跑：**BLOCKED**（需最终组合 SHA + 非 root 环境；本环境 7 项既有 FAIL 使严格模式必然非零）。
- 任何 Xray 进程启动/安装/部署：**未发生**（Runtime 未准入）。
