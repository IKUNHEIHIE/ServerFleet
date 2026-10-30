package certificates

import "errors"

var (
	ErrMissingAuthorizer         = errors.New("certificates: authorizer is required")
	ErrRequestIDRequired         = errors.New("certificates: request id is required")
	ErrInvalidInput              = errors.New("certificates: invalid input")
	ErrInvalidKeyRef             = errors.New("certificates: invalid key reference")
	ErrKeyMaterialRejected       = errors.New("certificates: private key material is not accepted")
	ErrInvalidValidity           = errors.New("certificates: invalid validity window")
	ErrConfirmationRequired      = errors.New("certificates: explicit confirmation is required")
	ErrInvalidRevision           = errors.New("certificates: expected group revision must be positive")
	ErrRevisionConflict          = errors.New("certificates: service group revision mismatch")
	ErrGroupRevisionsUnavailable = errors.New("certificates: group revision port is not configured")
	ErrRevisionCheckFailed       = errors.New("certificates: group revision check failed")
	ErrCertificateNotFound       = errors.New("certificates: certificate not found")
	ErrCertificateExists         = errors.New("certificates: certificate already exists")
	ErrCertificateRevoked        = errors.New("certificates: certificate is revoked")
	ErrDistributionExists        = errors.New("certificates: distribution already exists")
	ErrIssuerUnavailable         = errors.New("certificates: issuer is not configured")
	ErrIssuerFailed              = errors.New("certificates: issuer request failed")
	ErrTaskSubmitterUnavailable  = errors.New("certificates: task submitter is not configured")
	ErrTaskSubmitFailed          = errors.New("certificates: task submission failed")
	ErrInvalidTaskID             = errors.New("certificates: task submitter returned an invalid task id")
	ErrInvalidIssuerResponse     = errors.New("certificates: invalid issuer response")
)
