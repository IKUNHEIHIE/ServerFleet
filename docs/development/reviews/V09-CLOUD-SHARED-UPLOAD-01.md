# V09-CLOUD-SHARED-UPLOAD-01

## 最终接收回执（2026-11-03）

本共享修正已作为PR #5合并：最终审查head fc9739971c06e3ecaa93513571deffc324b3ca72，main3a1aefb92082565282eb5e35991b975722a45d7b。原错误候选e982的历史SQL漂移已纠正，旧hash不变；正式Controller迁移26项，旧库升级/重放回归PASS。AM实现随后作为PR #3合入最终产品基线51e0ee57；GET/202相关实现已有接收，不再仅停留候选文档，契约仍未冻结。

最终main独立CI成功，范围与失败历史见[V09-CLOUD-R1-REVIEW](V09-CLOUD-R1-REVIEW.md)。以下prepared/not merged及“本PR尚未提供路由”描述属于提案时刻，不代表当前整体状态。B03/B04/B05/B06/G2/发布准入保持OPEN，不重放旧patch或改写已应用SQL。

## 提案时记录（保留历史）

2026-11-03 (Asia/Shanghai). Coordinator/session: chatgpt-review-01.
Branch: `contract/cloud-upload-task-review01`. Depends on shared prerequisite PR #4,
head `78e5e366f198379cf1cf1040d376cefe3b722259`; original main
`1ca79072232bfdcee2b9f4582ac020d4430d0c2b`.
Status: prepared, not accepted or merged. PR #3 remains independently reviewed.

## Shared schema and ownership

Select only `internal/files/migrations.go` and the four mechanical migration-count
assertions from PR #3 head `e423c40445dc09f4bfb009bc1f82cf416cdaf998`.
The coordinator reserves `ds/0041_file_upload_task` for the files domain. It sorts
after the existing `ds/0040_files` under the unchanged global lexical migrator.
The `ds/` prefix preserves historical numbering, not current author ownership.
The additive migration adds a default-empty task binding and a nonunique index;
it executes no file task and does not reinterpret existing upload status.
Controller composition becomes 26 migrations, the services-only test 14.
Acceptance requires this shared PR's checks and actual merge.

Rollback the application without removing the additive column/index; do not
rewrite a migration already recorded by the ledger. Test the existing database
upgrade/reapply and cross-domain backup/restore paths. No other schema, runtime
capability, dependency edge, generation, root lock or CI change is included.

## HTTP candidate and compatibility

New `api/openapi/revisions/v09-r8/files-candidate.yaml` describes the exact PR #3
GET/content/complete DTOs as a separate proposed revision, registered in Redocly.
The original three OpenAPI copies, 69 reference inputs, Proto/generated code and
compatibility baseline remain unchanged. This PR does not install the new routes;
those only become available with the separately accepted implementation.

Existing clients expecting only 200 from completion must explicitly handle 202,
`taskId` and `dispatching`. A staging digest or acceptance is not remote success.
There is no new cancellation endpoint or automatic side-effect retry. Owner-bound
status requires current files.write and can reconcile trusted terminal evidence;
its DTO currently omits failure reasons, so it is not a complete diagnostic API.
Contrary to the author's prose, GET does not require Idempotency-Key: the Controller
supplies its request correlation ID. Existing authorization, Origin/CSRF and
unified task/approval/audit remain mandatory. No schema or API freeze is claimed.

## Validation and limits

Required: fixed toolchain, source integrity, candidate lint/contracts, migration
upgrade/reapply/backup tests, full checks and security, and exact-head GitHub jobs.
Record FAIL/BLOCKED/SKIP without reinterpretation. The coordinator's root-only
container cannot certify ordinary-user actual product cases; use real runner
logs and preserve local skips. B03/B04/B05/B06, G2 and release admission stay open.

## Historical checksum correction (coordinator, 2026-11-03)

The first candidate copied whitespace edits inside the adopted ds/0040 SQL from
AM. This changes the raw migration checksum and rejects a real adopted database.
The four previous upgrade tests rebuilt their legacy sets using current SQL,
so their PASS did not establish compatibility with the actual older ledger.
The coordinator reproduced FAIL with an independent frozen fixture before fixing
it. The old SQL is now byte-for-byte preserved; only ds/0041 is appended.

`cloud_upload_history_test.go` embeds `testdata/ds0040-files-adopted.json`, extracted
from accepted main 1ca7907 (identical at cce2b29). It creates the adopted 25-entry
ledger and an old upload, applies/reapplies all 26 migrations, and verifies the
old checksum/status/default-empty binding remain unchanged. Adopted ds/0040 hash:
`a7b7ed891f79d592593e1eea435d1430df94965f075458dc222d1a435fe210f7`.
No checksum algorithm, migration version, historical ledger or safety check is
relaxed. The earlier candidate and its CI are superseded and must not be merged.
AM's later main synchronization must retain this exact migration definition.

The branch also normally merges reviewed main 8b10e32 and removes only trailing
blank EOF lines from two already merged WorkBuddy handoff documents, so a later
branch synchronization passes git diff --check. Historical evidence is preserved.
