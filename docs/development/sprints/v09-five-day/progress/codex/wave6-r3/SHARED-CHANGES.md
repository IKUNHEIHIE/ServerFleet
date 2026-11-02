# CX-04 trusted deployment assembly

Only the two NEW candidates apply to synchronized db3cf87: `controller-file-cli.patch` and `controller-product-updates.patch`. The five Wave6-R2 patches/24 migrations are already adopted and must not be replayed. CLI code is `server.LoadFileExecutionConfig(filename)` backed by `files.LoadDeployment`; the product code is `server.NewOwnedProductUpdates` plus `internal/updates/product_deployment*`.

Linux startup document (private current-UID 0600 regular file with one link, in a current-UID 0700 directory; spool must be private/current UID):

```json
{
  "version": 1,
  "spoolDir": "/private/controller/file-spool",
  "nodeRoots": [
    {"nodeId": "ACTUAL-ENROLLED-SOURCE-ID", "logicalRoot": "/source"},
    {"nodeId": "ACTUAL-ENROLLED-TARGET-ID", "logicalRoot": "/target"}
  ]
}
```

IDs must be actual enrolled inventory identities, provisioned by a trusted local operator. No runtime self-registration, wildcard/root inference or HTTP configuration route exists. Duplicate fields/IDs, unknown fields, root `/`, nonabsolute spool, insecure files or unsupported platforms reject. Stop Controller before changing the registry; loader snapshots the whole document once. Revocation still uses current resource grants each frame. Configuration cannot grant AI/plugin authority. Other platforms require a separate equivalent ACL implementation; programmatic FileExecution remains unchanged.

CLI candidate: existing management TLS arguments plus `-file-deployment /private/controller/files.json`. Error `files: invalid or insecure deployment configuration` aborts startup before local PKI/database setup, rather than starting a partially disabled runtime. Without this flag default behavior stays unchanged. Rollback disables this explicit flag after stopping the process, retaining the DB/migration/Agent ledger; never delete UNKNOWN reservations.

# CX-04 file conflict policies (third increment)

The controller-to-agent file parameters already carried `conflictPolicy` as a string; no Proto/gen change. `internal/transport` now admits the three reviewed values instead of only `skip`, and the agent implements them:

- `skip`: an existing non-symlink regular destination is reported `skipped` and is byte-for-byte unchanged.
- `overwrite`: only a non-symlink regular destination may be replaced; the verified partial is swapped in atomically (`renameat`) and re-opened with `NOFOLLOW` for identity/hash verification. Links and directories are rejected. A race with a same-UID writer ends UNKNOWN, never a follow.
- `rename`: at open the agent picks the first free sibling `name (N).ext` in the same explicit directory (bounded 32 attempts, never replaces an existing entry) and returns it as `FileOpenReply.relativePath`; the controller maps it to the transfer item destination. A resumed transfer reuses the path recorded in the private partial binding, so a reconnect cannot silently pick another name.
- A rename reply outside the requested directory, or any non-rename reply path that differs from the request, is rejected by the file client.
- The unified Controller worker (`apps/controller/server/file_worker.go`) now carries the policy in the frozen task params and rechecks it in `plan`/`decodeFileTask`/`live`/`Gate`/`open`. A rename reply is accepted only when both paths map to the same explicit destination directory, and the actual published path is written back to the transfer item `destinationPath`.
- The local (non-channel) transport still only asserts overwrite semantics; the coordinator keeps rejecting other policies before any bytes move. `FileOpenReply` loss/commit uncertainty remains UNKNOWN with no automatic replay.

Evidence: `TestActualWSSOverwriteAndRenameConflictPolicies` exercises overwrite replacement, two successive renames and unchanged skip over actual WSS; Windows/Linux `internal/files`, `internal/transport`, `apps/agent` suites, staticcheck and architecture checks pass, and the R3 development combination remains 2 PASS / 0 SKIP. Remote upload/multi-target distribution, broader partial-result matrices and the 16-item/64 MiB product limits remain open.

Cross-suite adjustment for integrator review: `tests/integration/v09/services/file_transport_blackbox_test.go` previously pinned overwrite/rename as "must keep failing closed until CX implements". It now asserts the three reviewed policies are admitted while every other unsafe/ambiguous request (unknown policy, createParents, bad digests, absolute/traversal/staging paths) still fails closed. The file is CX-relevant but sits in the services suite, so the integration owner should accept this expectation change explicitly.

# CX-04 owned product updates (second candidate)

`controller-product-updates.patch` adds only `Config.UpdateProduct *updates.ProductDeploymentOptions`, `Server.ProductUpdates`, startup registration and lifecycle. It refuses to combine `UpdateProduct` with an already-injected `UpdateExecution/UpdateApply/UpdateResolvePlan/UpdatePlan/UpdateCheck`. No HTTP field supplies paths, keys, argv, provenance or the node identity; the central source is not edited in this worktree.

Trusted Linux options (all required, fail closed):

```go
cfg.UpdateProduct = &updates.ProductDeploymentOptions{
    Directory: privateSlotDir, Inbox: privateSignedInbox, StageDir: privateStage,
    KeyID: "release-key", NodeID: enrolledNodeID,
    IdentityDir: privateAgentIdentity, ControllerOrigin: "https://localhost:PORT", RootCertificate: absoluteCAPath,
    Policy: updates.ReleasePolicy{Component: "agent", Architecture: runtime.GOARCH, MinimumSequence: 1, MaxBytes: 256 << 20, TrustedKeys: map[string]ed25519.PublicKey{"release-key": pub}},
    Initial: initialSignedManifest, SchemaVersion: 0, SchemaFingerprint: reviewedFingerprint,
    CandidateVersion: nextVersion,
}
composed, err := server.New(ctx, cfg) // binds RuntimeGate, Tasks.CheckExecution, current administrator and verified WSS link
```

Behaviour and boundaries:

- `Directory`, `Inbox`, `StageDir` must be private current-UID 0700 directories; the NodeID/KeyID must be actual identifiers.
- Provenance is established only when the signed inbox bytes and the actual `active` executable agree, inside one audited transaction. A missing row is never seeded from HTTP, a backup or a version string.
- `SchemaVersion` is compared with `PRAGMA user_version`, and `SchemaFingerprint` with a hash of sorted `sqlite_master` definitions. DDL changes that keep `user_version` constant are still rejected. The fingerprint is also carried into the frozen plan, so it is part of the approval digest; a plan approved under another pinned schema is refused, and plans persisted before the field existed fail closed.
- `Plan` persists a nonce plan file and a verified consistent SQLite backup (<=256 MiB, 0600, regular file) before audited `plan_prepared`. A definite revocation during the audit removes the unissued plan/backup; any other failure is UNKNOWN, never a silent retry.
- Applying requires current human approval through the sole task service. `awaiting_approval` never changes the product. Health requires the real owned PID/nonce, compiled-in `main.agentBuildVersion`, storage format, trust generation and the current verified WSS session, plus independent `/proc/PID/exe` SHA. Readiness reports are read through one bounded handle with `SameFile`; the directory must stay private.
- Version mismatch rolls back to the exact previous signed release (new owned PID) and records a failed parent result; journal provenance/floor/receipt/audit commit atomically. A pre-existing `update-lock.json` or `supervisor-reservation.json`, or an unknown prior effect, refuses startup/apply instead of taking over an external process.
- Without trusted release files `Check` reports source-unavailable; it never invents an online feed. No default AI/plugin grant is added.
- Schema drift after approval (tested by changing `PRAGMA user_version` behind an approved 1.0.3 plan) ends `unknown` without replacing the product, advancing the journal floor or auto-retrying. A leftover `update-lock.json` or `supervisor-reservation.json` makes `NewProductDeployment` return `ErrUpdateBusy`, so a restarted Controller never adopts an interrupted update or a foreign PID.

Reproduce `node tests/poc/v09/platform/prepare-wave6-r3-product.mjs` (it first runs the R3 CLI preparer; it never runs the R2 historical prepare), then `git apply --check .../controller-product-updates.patch`, `node tests/poc/v09/platform/run-wave6-r3.mjs --final`. The integrator must also adopt `controller-file-cli.patch`; the product test carries build tag `cx_r3_product` because it references the not-yet-adopted Config field. Only the two new bootstrap/lifecycle patches use overlays to compile the actual product executable; no shared source/index/Proto/gen/DDL/root config mutation.

Host/GM uses the SAME program entry:

```go
cfg.FileExecution, err = server.LoadFileExecutionConfig(privateRegistryFile)
if err != nil { return err } // before server.New, never ignored
composed, err := server.New(ctx, cfg) // normal issuer/authority and current RBAC
```

Start each actual resident Agent with its already issued identity and independent private Linux root: `-serve -state-dir ... -controller-url https://localhost:PORT -root-cert ... -file-root /private/agent/files`. Controller logical `/source/a` maps to Agent private relative `a`; root paths need not match across machines. Existing skip transfer API and Cookie/Origin/CSRF are unchanged. Unregistered paths/unsupported transfer strategies return 400 invalid_transfer; no worker/channel still reports its existing unavailable error. The root registry is not exposed by HTTP.

Reproduce `node tests/poc/v09/platform/prepare-wave6-r3-cli.mjs`; `git apply --check .../controller-file-cli.patch`; `node tests/poc/v09/platform/run-wave6-r3.mjs --final`. Tests seed the initial admin through a trusted local installer fixture before starting the actual CLI; subsequent enrollment, approvals, grants, transfers and states use HTTPS, with two actual Agent processes. This is local non-root protocol/deployment assembly evidence, not physical two-machine or production admission.

Current development evidence is DIRTY-DEVELOPMENT 2 PASS / 0 SKIP; the second test is a same-source local synthetic-signature/compiled-version product, not an external formal Release or browser/hardware admission. Fixed clean-SHA evidence must be produced after integrating both patches.