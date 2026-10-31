package dns

import "errors"

var (
	ErrMissingAuthorizer       = errors.New("dns: authorizer is required")
	ErrRequestIDRequired       = errors.New("dns: request id is required")
	ErrInvalidInput            = errors.New("dns: invalid input")
	ErrInvalidCredentialRef    = errors.New("dns: invalid credential reference")
	ErrAccountNotFound         = errors.New("dns: account not found")
	ErrAccountExists           = errors.New("dns: account already exists")
	ErrAccountDisabled         = errors.New("dns: account is disabled")
	ErrZoneNotFound            = errors.New("dns: zone not found")
	ErrZoneExists              = errors.New("dns: zone already exists")
	ErrZoneNameTaken           = errors.New("dns: zone name already exists for account")
	ErrZoneNotInProvider       = errors.New("dns: zone is not visible to the account")
	ErrRecordNotFound          = errors.New("dns: record not found")
	ErrRecordConflict          = errors.New("dns: record already exists")
	ErrProviderUnavailable     = errors.New("dns: provider is not configured")
	ErrProviderFailed          = errors.New("dns: provider request failed")
	ErrCredentialNotConfigured = errors.New("dns: account has no credential reference")
	ErrCredentialUnverified    = errors.New("dns: credential is not verified")
	ErrInvalidProviderResponse = errors.New("dns: invalid provider response")
)
