package server

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"os"
	"path/filepath"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/plugins"
	"serverfleet.local/serverfleet/internal/storage"
)

func (s *Server) setupPlugins(cfg Config) error {
	directory := filepath.Join(s.stagingDir, "plugins")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	vault, err := plugins.NewPackageVault(directory)
	if err != nil {
		return err
	}
	store, err := newPluginLifecycleStore(s.DB, s.Audit)
	if err != nil {
		return err
	}
	trustKeys := map[string]ed25519.PublicKey{}
	for id, key := range cfg.PluginTrustKeys {
		trustKeys[id] = append(ed25519.PublicKey(nil), key...)
	}
	trustSources := map[string]bool{}
	for source := range cfg.PluginTrustSources {
		trustSources[source] = true
	}
	trustCapabilities := map[string]bool{}
	for capability := range cfg.PluginTrustCapabilities {
		trustCapabilities[capability] = true
	}
	trust := func(context.Context) (plugins.PackageTrust, error) {
		keys := map[string]ed25519.PublicKey{}
		for id, key := range trustKeys {
			keys[id] = append(ed25519.PublicKey(nil), key...)
		}
		return plugins.PackageTrust{Keys: keys, Sources: cloneBoolMap(trustSources), Capabilities: cloneBoolMap(trustCapabilities)}, nil
	}
	gate := func(ctx context.Context, op plugins.LifecycleOperation) error {
		principal, ok := plugins.LifecyclePrincipalFromContext(ctx)
		if !ok || !principal.Administrator || principal.SubjectID != op.ActorID {
			return plugins.ErrPackageRejected
		}
		return nil
	}
	manager, err := plugins.NewLifecycleManager(store, vault, trust, gate, cfg.PluginRuntime)
	if err != nil {
		return err
	}
	repository, err := plugins.NewFileRepositoryStore(filepath.Join(directory, "repository.json"))
	if err != nil {
		return err
	}
	operations, err := plugins.NewFileOperationStore(filepath.Join(directory, "operations.json"))
	if err != nil {
		return err
	}
	management, err := plugins.NewManagementHTTP(manager, repository, operations, func(r *http.Request) (plugins.LifecyclePrincipal, error) {
		identity, requestID, err := s.authenticateIdentity(r)
		if err != nil {
			return plugins.LifecyclePrincipal{}, err
		}
		return plugins.LifecyclePrincipal{SubjectID: identity.SubjectID, RequestID: requestID, Administrator: identity.Administrator}, nil
	})
	if err != nil {
		return err
	}
	newXrayHTTP := plugins.NewXrayDeclarativeHTTP
	if cfg.XrayTaskHost != nil {
		newXrayHTTP = func(a plugins.PluginAuthenticator, state func(context.Context) (plugins.Installation, error), nodes func(context.Context) ([]plugins.XrayNodeView, error)) (*plugins.XrayDeclarativeHTTP, error) {
			return plugins.NewXrayHTTPWithTaskHost(a, state, nodes, *cfg.XrayTaskHost)
		}
	}
	xray, err := newXrayHTTP(func(r *http.Request) (plugins.LifecyclePrincipal, error) {
		identity, requestID, err := s.authenticateIdentity(r)
		if err != nil {
			return plugins.LifecyclePrincipal{}, err
		}
		return plugins.LifecyclePrincipal{SubjectID: identity.SubjectID, RequestID: requestID, Administrator: identity.Administrator}, nil
	}, func(ctx context.Context) (plugins.Installation, error) {
		return manager.Get(ctx)
	}, func(ctx context.Context) ([]plugins.XrayNodeView, error) {
		list, err := s.NodesRepo.List(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]plugins.XrayNodeView, 0, len(list))
		for _, node := range list {
			out = append(out, plugins.XrayNodeView{NodeID: node.ID, Name: node.Name, Online: node.Status == nodes.StatusOnline})
		}
		return out, nil
	})
	if err != nil {
		return err
	}
	s.Plugins = manager
	s.PluginVault = vault
	s.PluginRepository = repository
	s.PluginOperations = operations
	s.PluginHTTP = management
	s.XrayHTTP = xray
	return nil
}

func cloneBoolMap(input map[string]bool) map[string]bool {
	out := make(map[string]bool, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func newPluginLifecycleStore(db *storage.DB, recorder *audit.SQLRecorder) (*plugins.SQLLifecycleStore, error) {
	read := func(ctx context.Context, queryer storage.Queryer, query string, args ...any) ([]byte, error) {
		var payload []byte
		err := queryer.QueryRow(ctx, query, args...).Scan(&payload)
		return payload, err
	}
	backend := plugins.LifecycleSQLBackend{
		Read: func(ctx context.Context, query string, args ...any) ([]byte, error) {
			return read(ctx, db, query, args...)
		},
		InTx: func(ctx context.Context, fn func(plugins.LifecycleSQLStatements) error) error {
			return db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
				statements := plugins.LifecycleSQLStatements{
					Read: func(readCtx context.Context, query string, args ...any) ([]byte, error) {
						return read(readCtx, tx, query, args...)
					},
					Exec: func(execCtx context.Context, query string, args ...any) (int64, error) {
						result, err := tx.Exec(execCtx, query, args...)
						if err != nil {
							return 0, err
						}
						return result.RowsAffected()
					},
					AppendAudit: func(auditCtx context.Context, events []plugins.LifecycleAuditEvent) error {
						for _, event := range events {
							result := event.Result
							if result == "" {
								result = "success"
							}
							auditEvent := audit.Event{
								SubjectID: event.ActorID,
								Action:    event.Action,
								Result:    result,
								RequestID: newRequestID(),
								Source:    audit.SourcePlugin,
								At:        event.At.UTC(),
							}
							if event.TaskID != "" {
								auditEvent.TaskID = event.TaskID
							}
							if err := recorder.AppendTx(auditCtx, tx, auditEvent); err != nil {
								return err
							}
						}
						return nil
					},
				}
				return fn(statements)
			})
		},
	}
	return plugins.NewSQLLifecycleStore(backend)
}
