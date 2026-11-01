package auth

import (
	"context"
	"time"
)

// AuthorizeAdministrator is a read-only backend gate for already authenticated
// operator operations. SubjectID is trusted composition data, never proof of a
// browser session/Origin/CSRF, node permission or root execution approval.
// Unlike Authenticate it opens no writer, so a same-DB coordinator transaction
// can repeat it before committing budget configuration without nested writers.
func (s *IdentityService) AuthorizeAdministrator(ctx context.Context, subject string) error {
	if !validPolicyID(subject) {
		return ErrDenied
	}
	return s.read(ctx, func(st *identityState, _ time.Time) error {
		u, ok := st.Users[subject]
		if !ok || u.Disabled || !u.Administrator {
			return ErrDenied
		}
		return nil
	})
}
