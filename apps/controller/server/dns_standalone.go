package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"sync/atomic"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/dns"
)

// Standalone DNS console requests run outside the approval task engine, so they
// get their own borrow window: the intent is built from the DNS scope, pinned to
// the account's credential revision, and only that exact operation is allowed
// through the credential gate while the window is open.
type standaloneDNSBorrowKey struct{}

type standaloneDNSBorrowScope struct {
	server *Server
	op     auth.CredentialOperation
	active atomic.Bool
}

func (s *Server) borrowStandaloneDNS(ctx context.Context, use dns.SecretUse, consume func(context.Context, string) error) error {
	if s == nil || s.Credentials == nil || consume == nil {
		return auth.ErrDenied
	}
	if use.Module != "dns" || use.Purpose != "dns-provider" || use.SubjectID == "" || use.Ref == "" || use.ExpectedRevision == 0 {
		return auth.ErrDenied
	}
	digest := sha256.Sum256([]byte("dns-standalone:" + use.Ref + ":" +
		use.SubjectID + ":" + use.RequestID + ":" + use.Operation + ":" + strconv.FormatUint(use.ExpectedRevision, 10)))
	op := auth.CredentialOperation{
		SubjectID:    use.SubjectID,
		CredentialID: use.Ref,
		Module:       "dns",
		Source:       "web",
		OperationID:  "dns-use-" + newRequestID(),
		PlanDigest:   hex.EncodeToString(digest[:]),
		Action:       "use",
		ExpectedRevision: use.ExpectedRevision,
	}
	scope := &standaloneDNSBorrowScope{server: s, op: op}
	scope.active.Store(true)
	defer scope.active.Store(false)
	return s.Credentials.UseForService(context.WithValue(ctx, standaloneDNSBorrowKey{}, scope), op,
		func(current context.Context, secret []byte) error { return consume(current, string(secret)) })
}

func (s *Server) standaloneDNSGate(ctx context.Context, op auth.CredentialOperation) error {
	scope, ok := ctx.Value(standaloneDNSBorrowKey{}).(*standaloneDNSBorrowScope)
	if !ok || scope == nil || scope.server != s || !scope.active.Load() || scope.op != op {
		return auth.ErrDenied
	}
	return nil
}
