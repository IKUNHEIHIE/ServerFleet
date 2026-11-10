package tasks

import (
	"encoding/json"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/transport"
)

// This narrowly typed public payload is not a general larger-param exception.
// Unknown fields, PEM private keys, alternate sources/classes and multi-targets
// are rejected before it can enter the frozen plan or a transport offer.
func publicChainParams(request SubmitRequest) (string, bool, error) {
	if request.ActionID != transport.CertificateInstallAction {
		return "", false, nil
	}
	if request.OperationClass != OperationClassModifying || !request.RequireApproval || request.Source != audit.SourceWeb || request.ScopeRevision != 0 || request.PluginID != "" || len(request.TargetNodeIDs) != 1 || request.CanonicalParams != nil && request.Params != nil {
		return "", true, ErrInvalidTask
	}
	raw := request.CanonicalParams
	if raw == nil {
		var err error
		raw, err = json.Marshal(request.Params)
		if err != nil {
			return "", true, ErrInvalidTask
		}
	}
	if _, _, err := transport.ParseCertificateInstallRequest(raw); err != nil {
		return "", true, ErrInvalidTask
	}
	var shape map[string]string
	if json.Unmarshal(raw, &shape) != nil {
		return "", true, ErrInvalidTask
	}
	canonical, err := json.Marshal(shape)
	if err != nil {
		return "", true, ErrInvalidTask
	}
	return string(canonical), true, nil
}
