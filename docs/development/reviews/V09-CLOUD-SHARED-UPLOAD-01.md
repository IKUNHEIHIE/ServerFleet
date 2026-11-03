# V09-CLOUD-SHARED-UPLOAD-01

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
The source's candidate comment is historical proposal provenance; acceptance
requires this shared PR's checks and actual merge.

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
