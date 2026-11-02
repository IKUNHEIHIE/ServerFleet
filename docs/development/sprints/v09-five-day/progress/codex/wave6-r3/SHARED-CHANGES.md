# CX-04 trusted deployment assembly

Only NEW `controller-file-cli.patch` applies to synchronized db3cf87. The five Wave6-R2 patches/24 migrations are already adopted and must not be replayed. New code is `server.LoadFileExecutionConfig(filename)` backed by `files.LoadDeployment`; CLI bootstrap owns only the flag and startup wiring.

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

Host/GM uses the SAME program entry:

```go
cfg.FileExecution, err = server.LoadFileExecutionConfig(privateRegistryFile)
if err != nil { return err } // before server.New, never ignored
composed, err := server.New(ctx, cfg) // normal issuer/authority and current RBAC
```

Start each actual resident Agent with its already issued identity and independent private Linux root: `-serve -state-dir ... -controller-url https://localhost:PORT -root-cert ... -file-root /private/agent/files`. Controller logical `/source/a` maps to Agent private relative `a`; root paths need not match across machines. Existing skip transfer API and Cookie/Origin/CSRF are unchanged. Unregistered paths/unsupported transfer strategies return 400 invalid_transfer; no worker/channel still reports its existing unavailable error. The root registry is not exposed by HTTP.

Reproduce `node tests/poc/v09/platform/prepare-wave6-r3-cli.mjs`; `git apply --check .../controller-file-cli.patch`; `node tests/poc/v09/platform/run-wave6-r3.mjs --final`. Only new CLI bootstrap uses an overlay to compile the actual product executable. No shared source/index/Proto/gen/DDL/root config mutation. Tests seed the initial admin through a trusted local installer fixture before starting the actual CLI; subsequent enrollment, approvals, grants, transfers and states use HTTPS, with two actual Agent processes. This is local non-root protocol/deployment assembly evidence, not physical two-machine/production admission.
