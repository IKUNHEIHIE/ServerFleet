package server

import (
	"context"
	"errors"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/gateway"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/transport"
)

type gatewayWiring struct {
	deployment   *GatewayDeployment
	registry     *gatewayArtifactRegistry
	stage        *GatewayArtifactStageSlot
	artifactGate *gatewayArtifactGate
	uploader     GatewayArtifactUploader
	dispatcher   *gatewayTaskDispatcher
}

func newGatewayWiring(ctx context.Context, db *storage.DB, deployment *GatewayDeployment) (*gatewayWiring, error) {
	if ctx == nil || db == nil || deployment == nil || len(deployment.Gateways) == 0 {
		return nil, ErrGatewayDeployment
	}
	stage := &GatewayArtifactStageSlot{}
	gate, err := NewGatewayArtifactGate(db, stage)
	if err != nil {
		return nil, ErrGatewayDeployment
	}
	return &gatewayWiring{
		deployment: deployment, registry: newGatewayArtifactRegistry(),
		stage: stage, artifactGate: gate,
	}, nil
}

func (w *gatewayWiring) validate(ctx context.Context, repo *gateway.Repo, queryer storage.Queryer, paths files.PathMapper) error {
	if w == nil || w.deployment == nil || repo == nil || queryer == nil || paths == nil {
		return ErrGatewayDeployment
	}
	for gatewayID, install := range w.deployment.Gateways {
		record, err := repo.GetGateway(ctx, queryer, gatewayID)
		if errors.Is(err, gateway.ErrGatewayNotFound) {
			// The trusted document may be installed before the gateway record is
			// created. The first publication still requires the record and the
			// mapped roots, so an absent record cannot bypass the boundary.
			continue
		}
		if err != nil {
			return ErrGatewayDeployment
		}
		if _, err := paths.RelativePath(ctx, record.NodeID, install.SiteDir); err != nil {
			return ErrGatewayDeployment
		}
		if _, err := paths.RelativePath(ctx, record.NodeID, install.ArtifactDir); err != nil {
			return ErrGatewayDeployment
		}
	}
	return nil
}

// composeGatewayFileGate routes only a persisted gateway artifact reservation to
// the gateway gate; every other file frame keeps the reviewed file worker gate.
func composeGatewayFileGate(gatewayGate *gatewayArtifactGate, delegate transport.FileFrameGate) transport.FileFrameGate {
	return func(ctx context.Context, observation transport.LinkObservation, offer *agentv1.TaskOffer, envelope *agentv1.Envelope) error {
		if delegate == nil || gatewayGate == nil {
			return files.ErrTaskBindingRequired
		}
		if offer != nil && offer.ActionId == transport.FileCapability {
			matched, err := gatewayGate.Matches(ctx, offer.ExecutionId)
			if err != nil {
				return err
			}
			if matched {
				return gatewayGate.Authorize(ctx, observation, offer, envelope)
			}
		}
		return delegate(ctx, observation, offer, envelope)
	}
}
