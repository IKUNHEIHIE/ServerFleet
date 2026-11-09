package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

// setupAgentTransport builds the enrollment service, the management HTTP surface
// and the Agent WSS link. It returns a dispatcher backed by the live link; the
// caller binds the task service after construction.
func (s *Server) setupAgentTransport(cfg Config, nodesService *nodes.Service, tasksRepo *tasks.Repo) (*agentLinkBridge, error) {
	if cfg.EnrollmentIssuer == nil || strings.TrimSpace(cfg.AgentAuthority) == "" || strings.TrimSpace(cfg.AgentServerName) == "" {
		return nil, nil
	}
	store, err := newEnrollmentStore(s.DB, s.Audit)
	if err != nil {
		return nil, err
	}
	gate := func(ctx context.Context, op transport.EnrollmentOperation) error {
		principal, ok := transport.EnrollmentPrincipalFromContext(ctx)
		if !ok || !principal.Administrator || principal.SubjectID != op.ActorID {
			return transport.ErrEnrollmentRejected
		}
		return nil
	}
	service, err := transport.NewEnrollmentService(store, cfg.EnrollmentIssuer, gate, gate)
	if err != nil {
		return nil, err
	}
	frameBytes := cfg.AgentFrameBytes
	if frameBytes == 0 {
		frameBytes = 64 << 10
	}
	if frameBytes < 1024 || frameBytes > 1<<20 {
		return nil, errors.New("controller: invalid agent frame limit")
	}
	enrollmentHTTP, err := transport.NewEnrollmentHTTP(service, transport.EnrollmentHTTPConfig{
		Authority:       cfg.AgentAuthority,
		ServerName:      cfg.AgentServerName,
		MaxConcurrent:   8,
		MaxIPs:          1024,
		PerIPPerMinute:  60,
		GlobalPerMinute: 600,
		Current: func(ctx context.Context) error {
			var one int
			if err := s.DB.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
				return transport.ErrEnrollmentUnavailable
			}
			return nil
		},
		Rejected: func(ctx context.Context, rejection transport.EnrollmentHTTPRejection) error {
			return s.recordEnrollmentRejection(ctx, rejection)
		},
	})
	if err != nil {
		return nil, err
	}
	management, err := transport.NewEnrollmentManagementHTTP(service, s.authenticateEnrollmentPrincipal, s.ensureEnrolledNode)
	if err != nil {
		return nil, err
	}
	verifier, err := transport.NewPeerVerifier(service.Lookup)
	if err != nil {
		return nil, err
	}
	bridge := newAgentLinkBridge(s.NodesRepo, nodesService, tasksRepo, s.now)
	bridge.commandCurrent = s.authorizeCommandTask
	bridge.readCSR = func(ctx context.Context, id string) (transport.CertificateCSRReceipt, error) {
		return transport.ReadCertificateCSR(ctx, s.DB, id)
	}
	bridge.recordCSR = func(ctx context.Context, receipt transport.CertificateCSRReceipt) error {
		return s.DB.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
			if err := transport.RecordCertificateCSRTx(txCtx, tx, receipt); err != nil {
				return err
			}
			return s.Audit.AppendTx(txCtx, tx, audit.Event{SubjectID: receipt.NodeID, Action: transport.CertificateCSRAction, Result: "success", RequestID: "csr-" + receipt.TaskID, TaskID: receipt.TaskID, NodeIDs: []string{receipt.NodeID}, Source: audit.SourceAgent, ReasonCode: "public_csr_observed_no_private_key", At: receipt.ObservedAt})
		})
	}

	bridge.readChain = func(ctx context.Context, id string) (transport.CertificateChainReceipt, error) {
		return transport.ReadCertificateChain(ctx, s.DB, id)
	}
	bridge.recordChain = func(ctx context.Context, receipt transport.CertificateChainReceipt) error {
		return s.DB.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
			if err := transport.RecordCertificateChainTx(txCtx, tx, receipt); err != nil {
				return err
			}
			return s.Audit.AppendTx(txCtx, tx, audit.Event{SubjectID: receipt.NodeID, Action: transport.CertificateInstallAction, Result: "success", RequestID: "chain-" + receipt.TaskID, TaskID: receipt.TaskID, NodeIDs: []string{receipt.NodeID}, Source: audit.SourceAgent, ReasonCode: "node_public_chain_installed_observed", At: receipt.ObservedAt})
		})
	}
	semantic, minor := agentValidationLimits(), uint32(0)
	if cfg.AgentFileGate != nil {
		semantic.AllowFileFrames = true
		minor = 1
	}
	links, err := transport.NewControllerLinks(verifier, transport.ControllerLinkConfig{
		RuntimeGate:       s.runtimeGate,
		ProtocolMajor:     1,
		ProtocolMinor:     minor,
		Semantic:          semantic,
		Frames:            agentFrameLimits(),
		HeartbeatInterval: 15 * time.Second,
		MaxConnections:    100,
		Handshake:         bridge.handshake,
		Admit:             bridge.admit,
		Event:             bridge.event,
		Disconnected:      bridge.disconnected,
	})
	if err != nil {
		return nil, err
	}
	bridge.links = links
	if cfg.AgentFileGate != nil {
		client, err := transport.NewFileClient(links, cfg.AgentFileGate, 32)
		if err != nil {
			return nil, err
		}
		bridge.files, s.AgentFiles = client, client
	}
	linkHTTP, err := transport.NewAgentLinkHTTP(links, cfg.AgentAuthority, cfg.AgentServerName, transport.WebSocketConfig{MaxFrameBytes: frameBytes})
	if err != nil {
		return nil, err
	}
	s.Enrollment = service
	s.EnrollmentHTTP = enrollmentHTTP
	s.EnrollmentManagement = management
	s.Links = links
	s.AgentLinkHTTP = linkHTTP
	s.agentBridge = bridge
	return bridge, nil
}

func newEnrollmentStore(db *storage.DB, recorder *audit.SQLRecorder) (*transport.SQLEnrollmentStore, error) {
	read := func(ctx context.Context, queryer storage.Queryer, query string, args ...any) ([]byte, error) {
		var payload []byte
		err := queryer.QueryRow(ctx, query, args...).Scan(&payload)
		return payload, err
	}
	backend := transport.EnrollmentSQLBackend{
		Read: func(ctx context.Context, query string, args ...any) ([]byte, error) {
			return read(ctx, db, query, args...)
		},
		InTx: func(ctx context.Context, fn func(context.Context, transport.EnrollmentSQLStatements) error) error {
			return db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
				statements := transport.EnrollmentSQLStatements{
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
					AppendAudit: func(auditCtx context.Context, events []transport.EnrollmentAuditEvent) error {
						for _, event := range events {
							source := audit.SourceWeb
							if event.ActorID == "enrolling-agent" {
								source = audit.SourceAgent
							}
							nodeIDs := []string{}
							if event.NodeID != "" {
								nodeIDs = []string{event.NodeID}
							}
							at := time.UnixMilli(event.AtUnixMilli).UTC()
							if event.AtUnixMilli <= 0 {
								at = time.Now().UTC()
							}
							if err := recorder.AppendTx(auditCtx, tx, audit.Event{
								SubjectID: event.ActorID,
								Action:    event.Action,
								Result:    "success",
								RequestID: newRequestID(),
								NodeIDs:   nodeIDs,
								Source:    source,
								At:        at,
							}); err != nil {
								return err
							}
						}
						return nil
					},
				}
				return fn(txCtx, statements)
			})
		},
	}
	return transport.NewSQLEnrollmentStore(backend)
}

func (s *Server) recordEnrollmentRejection(ctx context.Context, rejection transport.EnrollmentHTTPRejection) error {
	code := sanitizeAuditCode(rejection.ReasonCode)
	if code == "" {
		code = "enrollment_rejected"
	}
	requestID := rejection.RequestID
	if requestID == "" || len(requestID) > 128 || strings.ContainsAny(requestID, " \t\r\n\x00") {
		requestID = newRequestID()
	}
	return s.Audit.Append(ctx, audit.Event{
		SubjectID:  "enrollment-source",
		Action:     "enrollment.rejected",
		Result:     "denied",
		RequestID:  requestID,
		NodeIDs:    []string{},
		Source:     audit.SourceAgent,
		ReasonCode: code,
		At:         s.now().UTC(),
	})
}

func sanitizeAuditCode(code string) string {
	if code == "" || len(code) > 64 {
		return ""
	}
	out := make([]rune, 0, len(code))
	for _, r := range strings.ToLower(code) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return ""
	}
	return string(out)
}

func (s *Server) authenticateEnrollmentPrincipal(r *http.Request) (transport.EnrollmentPrincipal, error) {
	identity, requestID, err := s.authenticateIdentity(r)
	if err != nil {
		return transport.EnrollmentPrincipal{}, err
	}
	return transport.EnrollmentPrincipal{SubjectID: identity.SubjectID, RequestID: requestID, Administrator: identity.Administrator}, nil
}

// ensureEnrolledNode creates the offline inventory record for an approved
// enrollment in the same trusted composition path that issued the identity.
func (s *Server) ensureEnrolledNode(ctx context.Context, principal transport.EnrollmentPrincipal, view transport.EnrollmentView) error {
	if view.Status != "approved" || view.NodeID == "" {
		return nil
	}
	if _, err := s.NodesRepo.Get(ctx, view.NodeID); err == nil {
		return nil
	} else if !errors.Is(err, nodes.ErrNodeNotFound) {
		return err
	}
	name := strings.TrimSpace(view.Name)
	if name == "" {
		name = "node-" + view.NodeID
		if len(name) > 128 {
			name = name[:128]
		}
	}
	return s.DB.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		_, err := s.NodesRepo.Create(txCtx, tx, nodes.NodeCreateInput{ID: view.NodeID, Name: name, Status: nodes.StatusOffline})
		if err != nil {
			if storage.IsUniqueViolation(err) {
				return nil
			}
			return err
		}
		return s.Audit.AppendTx(txCtx, tx, audit.Event{
			SubjectID: principal.SubjectID,
			Action:    nodes.ActionNodeCreate,
			Result:    "success",
			RequestID: principal.RequestID,
			NodeIDs:   []string{view.NodeID},
			Source:    audit.SourceWeb,
			At:        s.now().UTC(),
		})
	})
}
