package server

import (
	"context"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/updates"
)

// NewOwnedProductUpdates binds trusted offline product configuration to the
// actual Controller authority. Caller paths/keys never come from HTTP. The
// central patch registers its worker and owns Start/Close before returning New.
func (s *Server) NewOwnedProductUpdates(config updates.ProductDeploymentOptions) (updates.ProductDeployment, error) {
	if s == nil || s.Links == nil || s.runtimeGate == nil {
		return nil, updates.ErrArtifactRejected
	}
	config.DB, config.Audit = s.DB, s.Audit
	config.Current = func(ctx context.Context, id, plan string) (tasks.Task, error) {
		return s.Tasks.CheckExecution(ctx, id, plan)
	}
	config.Administrator = s.Identity.AuthorizeAdministrator
	config.RuntimeCurrent = s.runtimeGate
	config.NodeCurrent = func(ctx context.Context, node, connection string) error {
		observed, err := s.Links.CurrentConnection(ctx, node)
		if err != nil || observed.ConnectionID != connection {
			return updates.ErrArtifactRejected
		}
		return nil
	}
	return updates.NewProductDeployment(config)
}
