# V09-CLOUD-SHARED-CI-01

Date: 2026-11-03 (Asia/Shanghai). Coordinator/session: chatgpt-review-01.
Base: `1ca79072232bfdcee2b9f4582ac020d4430d0c2b`.
Branch: `integration/cloud-ci-prerequisites-review01`.
Status: prepared for independent checks and a shared PR; not merged.

## Scope and provenance

This shared prerequisite selects only six reviewed test files from the author PRs.
It does not adopt their business changes, change production configuration, register
new migrations, change public contracts, or modify CI/locks/generated files.

- PR #2, `261ed168ae432b150aa60bd3503c2eddb70d02b1`: the two existing
  `apps/agent/gateway_capability*_test.go` files. Windows drive-letter roots are
  considered only on Windows; real Nginx fixtures wait for the actual PID file.
- PR #3, `e423c40445dc09f4bfb009bc1f82cf416cdaf998`: platform
  `productbuild_test.go`, `file_cli_r3_test.go`, `file_flow_r2_test.go`, and
  `update_product_r3_test.go`. Missing prebuilt overrides trigger real Controller
  and Agent builds from the current checkout, including version-stamped Agent
  products. Build errors remain fatal and artifact SHA-256 values are logged.

The ordinary-user/Linux prerequisites, actual byte/digest/state assertions,
signature checks and unknown-result semantics are unchanged. The change removes
one missing-prebuild SKIP on eligible Linux hosts rather than introducing a skip.
Formal Controller migrations remain 25 in this shared prerequisite.

## Validation and limits

Required checks before merge: `npm ci`, `npm run tools:install`, `npm run check`,
`npm run check:security`, and the corresponding GitHub Actions jobs/logs on the
exact reviewed head. Original failed author runs remain historical evidence.

The coordinator container maps only UID 0. Ordinary-user product execution cannot
be independently certified there; those cases remain BLOCKED locally. Any root
run's SKIP results must be preserved. GitHub runner evidence must be read before
accepting this shared baseline; a green dependency scan alone is insufficient.

PR #1/#2/#3 remain separately reviewed, unmerged Drafts while this prerequisite is
prepared. Their new production behavior, shared migration/API proposals and
remaining browser/gateway/recovery acceptance are not accepted by this document.
B03/B04/B05/B06, G2 and production release admission remain open.
