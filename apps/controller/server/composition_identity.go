package server

import (
	"context"
	"errors"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/publishing"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"strings"
)

func newIdentityStore(db *storage.DB, recorder *audit.SQLRecorder) (*auth.SQLIdentityStore, error) {
	statements := func(q storage.Queryer) auth.IdentitySQLStatements {
		return auth.IdentitySQLStatements{
			Read: func(ctx context.Context, query string, args ...any) ([]byte, bool, error) {
				var payload []byte
				var initialized bool
				err := q.QueryRow(ctx, query, args...).Scan(&payload, &initialized)
				return payload, initialized, err
			},
			Exec: func(ctx context.Context, query string, args ...any) (int64, error) {
				result, err := q.Exec(ctx, query, args...)
				if err != nil {
					return 0, err
				}
				return result.RowsAffected()
			},
		}
	}
	backend := auth.IdentitySQLBackend{
		Read: statements(db).Read,
		InTx: func(ctx context.Context, fn func(auth.IdentitySQLStatements) error) error {
			return db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
				ops := statements(tx)
				ops.AppendAudit = func(auditCtx context.Context, events []auth.IdentityAuditEvent) error {
					for _, event := range events {
						source := audit.Source(event.Source)
						if source == "" {
							source = audit.SourceWeb
						}
						reqID := event.RequestID
						if reqID == "" {
							reqID = newRequestID()
						}
						if err := recorder.AppendTx(auditCtx, tx, audit.Event{
							SubjectID: event.SubjectID,
							Action:    event.Action,
							Result:    event.Result,
							RequestID: reqID,
							Source:    source,
							At:        event.At,
							NodeIDs:   event.NodeIDs,
						}); err != nil {
							return err
						}
					}
					return nil
				}
				return fn(ops)
			})
		},
	}
	backend.InTxContext = func(ctx context.Context, fn func(context.Context, auth.IdentitySQLStatements) error) error {
		return backend.InTx(ctx, func(ops auth.IdentitySQLStatements) error {
			return fn(ctx, ops)
		})
	}
	return auth.NewSQLIdentityStore(backend)
}

type composedAuthorizer struct {
	identity *auth.IdentityService
}

func (a *composedAuthorizer) Authorize(ctx context.Context, check auth.Check) error {
	if a == nil || a.identity == nil {
		return auth.ErrPolicyUnavailable
	}
	if len(check.NodeIDs) == 0 {
		if check.Action == nodes.ActionGroupsWrite ||
			strings.HasPrefix(check.Action, "dns.") ||
			strings.HasPrefix(check.Action, "notifications.") ||
			strings.HasPrefix(check.Action, "monitoring.") ||
			check.Action == certificates.ActionCertificateRead ||
			strings.HasPrefix(check.Action, "certificates.cleanup.") ||
			check.Action == publishing.ActionRead {
			return a.identity.AuthorizeSubjectAction(ctx, check.SubjectID, check.Action)
		}
		return auth.ErrInvalidCheck
	}
	err := a.identity.Authorize(ctx, check)
	if errors.Is(err, auth.ErrDenied) && (check.Action == "tasks.command" || check.Action == "host.patch") {
		return a.identity.Authorize(ctx, auth.Check{
			SubjectID: check.SubjectID,
			Action:    tasks.ActionSubmit,
			NodeIDs:   check.NodeIDs,
		})
	}
	return err
}

var _ auth.Authorizer = (*composedAuthorizer)(nil)

type composedTaskReadGate struct {
	identity *auth.IdentityService
	tasks    *tasks.Repo
}

func (g *composedTaskReadGate) AuthorizeTaskRead(ctx context.Context, subjectID, taskID string) error {
	if g == nil || g.identity == nil || g.tasks == nil {
		return auth.ErrPolicyUnavailable
	}
	if err := g.identity.AuthorizeSubjectAction(ctx, subjectID, "tasks.read"); err != nil {
		return err
	}
	if taskID == "" {
		return nil
	}
	task, err := g.tasks.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if task.TargetType == tasks.TargetTypeController {
		return g.identity.AuthorizeAdministrator(ctx, subjectID)
	}
	if len(task.Targets) == 0 {
		return nil
	}
	targetIDs := make([]string, 0, len(task.Targets))
	for _, target := range task.Targets {
		targetIDs = append(targetIDs, target.NodeID)
	}
	return g.identity.Authorize(ctx, auth.Check{
		SubjectID: subjectID,
		Action:    "tasks.read",
		NodeIDs:   targetIDs,
	})
}

type composedAuditReadGate struct {
	identity *auth.IdentityService
}

func (g *composedAuditReadGate) AuthorizeAuditRead(ctx context.Context, subjectID string) error {
	if g == nil || g.identity == nil {
		return auth.ErrPolicyUnavailable
	}
	return g.identity.AuthorizeSubjectAction(ctx, subjectID, "audit.read")
}

type composedNotificationsReadGate struct {
	identity *auth.IdentityService
}

func (g *composedNotificationsReadGate) AuthorizeNotificationsRead(ctx context.Context, subjectID string) error {
	if g == nil || g.identity == nil {
		return auth.ErrPolicyUnavailable
	}
	return g.identity.AuthorizeSubjectAction(ctx, subjectID, "notifications.read")
}
