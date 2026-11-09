//go:build !linux

package updates

import "context"

func RunResidentSupervisor(context.Context, ResidentSupervisorConfig, ResidentSupervisorHooks) error {
	return ErrArtifactRejected
}
