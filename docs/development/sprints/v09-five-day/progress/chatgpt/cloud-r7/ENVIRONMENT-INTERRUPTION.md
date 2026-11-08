# Execution environment interruption — code-first package

Task V09-CLOUD-CODE-COMPLETE-01 / chatgpt-takeover-01. User instruction remains: implement the whole authorized code package, then one consolidated acceptance; no per-function/package test loops, no agents.

## Last confirmed durable code
- Branch feature/cloud-v09-code-completion-20261109.
- Product checkpoint 08916faef5ae8d83b5cc357860d8e47fcd9f6f71, verified by GitHub refs API after the environment disconnected.
- Main remains 9738648ecdf44546e4a2d3503f8c9b862c8a5cb3; this package is NOT merged, no PR/CI acceptance yet.
- Includes publishing probes/multi-gateway compensation, provisioned offline-Root/online-Intermediate PKI, typed Controller-local unified tasks, encrypted backups/configuration/challenge/download, quarantined restore/reconciliation, fixed signed HTTPS release acquisition and Web panels. R12/R13 candidates; 33 migrations expected.
- All code after the earlier accepted main is UNTESTED. No compilation, age execution, dependency installation or new test run was performed during this code-first package. Prior green CI does not validate it.

## Actual blocker
The execution server disconnected, then the environment was reported unavailable. A subsequent git-status attempt returned: No usable execution environment is available. No source reads/writes/commit/test can currently be performed in that workspace. This is not an approval rejection and no production/external account access was attempted.

## Uncommitted CSR work: inspect before editing after recovery
Workspace: /workspace/scratch/c499cac0c047/ServerFleet-completion. Last inspected branch head was 08916fa. Do not discard its dirty files or replace them from another checkout.

Written locally but NOT confirmed pushed:
- internal/transport/certificate_csr.go: strict public CSR request/result and P-256 proof, bounded canonical JSON. Key references were corrected to ASCII letters/digits/underscore/hyphen only; generic validBindingID is NOT filesystem-safe.
- internal/transport/certificate_csr_sql.go: proposed cx/0015_agent_certificate_csr (would be migration 34), public CSR receipt owner ports.
- apps/agent/certificate_csr.go and platform private-permission helpers: operator-enabled certificate-root outside file-root, private ordinary-user key creation, exclusive references, export only public CSR.
- apps/agent/runtime/runtime.go and connect.go: explicit certificate-root flag and registration.
- internal/tasks/dispatch_check.go: exact pending-target queued/running offer gate with current runtime/auth/approval; does not relax CheckExecution or queued artifact uploads.
- internal/tasks/service.go: distinct domain binding for new Controller task hashes; legacy node hashes retained.
- apps/controller/server/agent_links.go and server.go: CSR structural action/permission and last-mile offer checks.
- internal/certificates/acme.go: public-CSR ACME method and pre-side-effect CSR validation, no Controller website private key.
- apps/controller/server/certificate_csr.go: intended task admission/readback and public-output observer.

Important unfinished integration:
1. CSR server observer still references nonexistent tasksRepo.RecordCertificateCSR in the last successfully written version. Do not add certificate storage ownership to tasks. Instead inject a recordCSR callback into agentLinkBridge, from Controller enrollment composition, using transport.RecordCertificateCSRTx plus audit.AppendTx on the same DB transaction.
2. That callback/observer wiring, dynamic CSR grant, enrollment import updates, migration registration and route registration were in the command rejected before process creation. Their application is NOT confirmed; inspect actual files first.
3. The unfinished CSR server file may still have an unused storage import/placeholder. No compilation was performed.
4. Persist CSR output through the typed owner port only after current running task/node/plan/approval checks. General logs are not a typed CSR receipt. Expose completed results only under current admin/node read authorization.
5. Add R14 candidate/compatibility registration and actual Web support before claiming a product flow. No new Proto/gen edits are needed for this existing typed TaskOffer profile.
6. Reconcile the local branch with this documentation-only remote commit safely after inspecting dirty files; never reset/clean/overwrite to force synchronization.

## Still open
No actual fence/release, offsite SFTP/retention, full remote fleet updates, complete ACME/product publishing composition, normal/root execution admission, Xray Runtime/security, G2/release or external/ARM/physical/hard-1C1G acceptance is claimed complete. Preserve all historical FAIL/BLOCKED/SKIP. Resume implementation when execution is available, then one consolidated required acceptance and exact-head PR review before merge.
