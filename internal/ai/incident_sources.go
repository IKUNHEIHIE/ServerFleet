package ai

import (
	"context"
	"slices"
)

// Only operational identifiers and terminal state cross this trusted monitor
// port. Private conversation text, tool results and native reasoning never do.
type OperatorIncidentSource struct {
	OwnerID, SessionID, RunID, State, TaskID, Reason string
	Revision                                         uint64
}

func (s *SessionService) OperatorIncidentSources(ctx context.Context) ([]OperatorIncidentSource, error) {
	out := []OperatorIncidentSource{}
	e := s.store.View(ctx, func(raw []byte) error {
		st, e := decodeSessions(raw)
		if e != nil {
			return e
		}
		for session, r := range st.Sessions {
			for _, a := range r.AgentRuns {
				v := a.View
				if !slices.Contains([]string{"unknown", "failed", "limit_reached"}, v.State) {
					continue
				}
				task := ""
				if len(v.Steps) > 0 {
					task = v.Steps[len(v.Steps)-1].TaskID
				}
				out = append(out, OperatorIncidentSource{r.Scope.SubjectID, session, v.ID, v.State, task, v.StopReason, v.Revision})
				if len(out) > 1000 {
					return ErrSessionUnavailable
				}
			}
		}
		return nil
	})
	return out, e
}
func (s *SessionService) CheckOperatorIncidentSource(ctx context.Context, owner, session, run, state string, revision uint64) error {
	return s.store.View(ctx, func(raw []byte) error {
		st, e := decodeSessions(raw)
		if e != nil {
			return e
		}
		r := st.Sessions[session]
		a, ok := r.AgentRuns[run]
		if !ok || r.Scope.SubjectID != owner || a.View.Revision != revision || a.View.State != state || !agentTerminal(state) {
			return ErrToolDenied
		}
		return nil
	})
}
