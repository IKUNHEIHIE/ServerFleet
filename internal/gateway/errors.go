// Package gateway owns managed Nginx gateways, their routes and provisioning
// reconciliation. It never executes commands itself: provisioning is delegated to
// the unified task pipeline through an injected TaskSubmitter.
package gateway

import "errors"

var (
	ErrMissingAuthorizer = errors.New("gateway: authorizer is required")
	ErrRequestIDRequired = errors.New("gateway: request id is required")
	ErrInvalidRevision   = errors.New("gateway: expected revision must be positive")

	ErrInvalidGateway   = errors.New("gateway: invalid gateway")
	ErrGatewayNotFound  = errors.New("gateway: gateway not found")
	ErrGatewayExists    = errors.New("gateway: gateway already exists")
	ErrRevisionConflict = errors.New("gateway: revision conflict")

	ErrNodeNotFound     = errors.New("gateway: node not found")
	ErrNodeLookupFailed = errors.New("gateway: node lookup failed")

	ErrServiceGroupNotFound     = errors.New("gateway: service group not found")
	ErrServiceGroupLookupFailed = errors.New("gateway: service group lookup failed")

	ErrInvalidRoute     = errors.New("gateway: invalid route")
	ErrRouteNotFound    = errors.New("gateway: route not found")
	ErrRouteExists      = errors.New("gateway: route already exists")
	ErrRouteDomainTaken = errors.New("gateway: route domain already exists")

	ErrRouteCheckFailed   = errors.New("gateway: route health check failed")
	ErrRoutePortConflict  = errors.New("gateway: route port conflict")
	ErrCheckerUnavailable = errors.New("gateway: route checker is not configured")

	ErrTakeoverApprovalRequired = errors.New("gateway: takeover approval is required")

	ErrInvalidProvision         = errors.New("gateway: invalid provision update")
	ErrProvisionNotFound        = errors.New("gateway: provision not found")
	ErrProvisionExists          = errors.New("gateway: provision already exists for task")
	ErrProvisionInProgress      = errors.New("gateway: a provision is already in progress")
	ErrProvisionTerminal        = errors.New("gateway: provision is terminal")
	ErrProvisionTransition      = errors.New("gateway: invalid provision transition")
	ErrProvisionConflict        = errors.New("gateway: provision changed concurrently")
	ErrTaskSubmitterUnavailable = errors.New("gateway: task submitter is not configured")
	ErrTaskSubmitFailed         = errors.New("gateway: task submission failed")
)
