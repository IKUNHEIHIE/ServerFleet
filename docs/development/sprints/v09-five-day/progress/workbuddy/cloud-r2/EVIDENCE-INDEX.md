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

## E-06 工件字节校验（linux/amd64）—— 已完成

```
# 首段下载在 15,228,928/16,692,690 B 处停滞，用 HTTP Range(206) 补齐尾部
$ curl -H "Range: bytes=15228928-" … → 206，追加 1,463,762 B
$ stat -c%s ev/Xray-linux-64.zip → 16692690
$ sha256sum ev/Xray-linux-64.zip
e4bd9119f50ae7c41658b1abd0679ae06f98bd864a2907ff33f2f2cf7081b214  ev/Xray-linux-64.zip
# 与 GitHub asset.digest 完全一致 → PASS（仅完整性/来源字节层面）
$ unzip -l … → geosite.dat / LICENSE / README.md / geoip.dat / xray(35,151,996 B)
$ sha256sum ev/xray/xray
cc4b84d3069569f297a61f04b914f2bb37e9d202fd7b84cb50def1fc30fd40e1
$ go version -m ev/xray/xray → go1.27.3，github.com/xtls/xray-core (devel)
```
**未执行该二进制**（未 `run -test`、未启动、未监听）。

## E-06b 二进制漏洞扫描（v26.10.31 linux/amd64）

```
$ govulncheck -mode=binary ev/xray/xray   → EXIT 3
Vulnerability #1: GO-2026-6866  golang.org/x/net@v0.61.0    fixed v0.62.0
Vulnerability #2: GO-2026-6860  golang.org/x/net@v0.61.0    fixed v0.62.0
Vulnerability #3: GO-2026-5932  golang.org/x/crypto@v0.58.0 (openpgp)
→ "Your code is affected by 3 vulnerabilities from 2 modules."（44 行原始日志）
```
结论：**FAIL**（项目规则：扫描非零即不得作为安装目标）。符号为 `html.Parse*` 与 `openpgp/*`；可达性未评审，也不以“看似不可达”豁免。

## E-07 项目自检（本包改动后）

```
$ npm ci                                   → added 127 packages in 4s
$ npm run tools:install                    → protoc-gen-go/staticcheck/govulncheck@v1.9.0/actionlint OK
$ npm run check:web                        → Test Files 41 passed (41) | Tests 377 passed (377)
$ npm run check:security                   → npm audit: found 0 vulnerabilities
                                             govulncheck ./...: No vulnerabilities found
$ go test ./plugins/xray-manager/          → ok（44 项 PASS；3 项真实二进制测试 SKIP）
$ npm run check:go                         → FAIL（exit 1）：7 项环境性既有 FAIL，见 E-08
```

顶层统计（`reports/go-tests.log`，R2 返修后）：**910 PASS / 19 SKIP / 7 FAIL**（FAIL 集合与 CP1 完全一致，见 E-08）。

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
✓ src/features/files/upload-queue.test.ts        (21 tests)
✓ src/features/files/files-page.test.tsx         (6 tests)
✓ src/features/files/review-cloud-r2.test.tsx    (6 tests)
Tests 33 passed (33)
```
覆盖：202+`dispatching`+taskId 只显示“已受理（排队）/任务 task-9”且**不**显示“上传完成”；点击“读回真实状态”后 GET 返回 `completed` 才显示已完成；重复读回仍 `dispatching` 时保持未完成；403/409/413/503/摘要/UNKNOWN 文案；缓存 completed + 当前 403/5xx/404 一律降级；读回未落定前只有“待核验”；切节点后旧在途响应不得进入新面板。

## E-09b R2 返修证据（W1—W4，本 head 实测）

```
# Go：审查正文给出的三条回归 + 本包补充负向
$ go test ./plugins/xray-manager -run TestReviewR2 -count=1 -v
--- PASS: TestReviewR2QuotaResetRoundTripPreservesUsedBytes   (before=150 → after=151)
--- PASS: TestReviewR2QuotaRolloverChargesAllNewWindowBytes   (new window = 20)
--- PASS: TestReviewR2BatchApprovalRejectsChangedPolicy       (concurrency 1→16 被拒)
--- PASS: TestReviewR2QuotaResetRefusesWithoutZeroing         (缺来源/段不匹配/计数器倒退 → unknown 且不清零)
--- PASS: TestReviewR2QuotaDirectionIsHonouredOnReset         (uplink 只结算上行)
--- PASS: TestReviewR2QuotaRolloverRefusals                   (缺来源/跨段/未到期/未开启 ResetOnWindow)
--- PASS: TestReviewR2QuotaSegmentMismatchAndWarningOverflow   (used*100 溢出不再误报 80%)
--- PASS: TestReviewR2BatchApprovalRejectsMutatedPlan         (7 类字段漂移全部拒绝)
--- PASS: TestReviewR2BatchApprovalVerifiesWavesEvenWithRecomputedDigest
--- PASS: TestReviewR2BatchApprovalRejectsPastedIdentityStrings
ok  serverfleet.local/serverfleet/plugins/xray-manager  0.010s

# React：与审查正文同形的两条场景 + 补充负向
$ npx vitest run src/features/files/review-cloud-r2.test.tsx
✓ drops a cached completed status when the current read is forbidden (403)
✓ does not show completion before the first read-back settles
✓ maps a 5xx read failure to an unverified failure instead of a kept completion
✓ shows UNKNOWN when the server has no such upload session
✓ drops an in-flight response from the previous node after switching nodes
✓ keeps the new node's own records unverified rather than inheriting the old node's evidence
Tests 6 passed (6)

# 全量
$ npm run check:web        → Test Files 41 passed (41) | Tests 377 passed (377)
$ npm run check:policy     → PASS（branch / commit titles / module boundaries / actionlint）
$ npm run check:contracts  → PASS
$ npm run check:security   → npm audit 0；govulncheck ./... 无发现
$ npm run check:go         → FAIL（exit 1）：7 项既有环境 FAIL（E-08），本包新增用例全 PASS
```
原 head `3eacd28` 上这 10 条既有回归中的 5 条（审查给出的 3 Go + 2 React）实测 **FAIL**；本 head 全部 **PASS**。

## E-10 未执行（如实保留）

- 浏览器 Live/Stub 专项（Playwright/fixturehost）：**UNTESTED**（本检查点仅组件测试）。
- AC-01—24 严格矩阵重跑：**BLOCKED**（需最终组合 SHA + 非 root 环境；本环境 7 项既有 FAIL 使严格模式必然非零）。
- 任何 Xray 进程启动/安装/部署：**未发生**（Runtime 未准入）。
- arm64 工件下载/扫描/运行：**UNTESTED**（无 ARM 实机；不以交叉构建代替）。
- 漏洞可达性调用图评审：**未做**（需真实配置路径与安全负责人判定）。
