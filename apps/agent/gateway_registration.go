package main

import (
	"errors"
	"path/filepath"
	"strings"
)

var errGatewayRegistration = errors.New("agent: gateway registration rejected")

// gatewayRegistrationConfig is the operator-owned gateway runtime identity.
// Prefix must be an absolute private directory owned by the current user; the
// well-known site/artifact/main-config children are derived, never supplied by
// a task.
type gatewayRegistrationConfig struct {
	Prefix      string
	NginxBinary string
}

// newGatewayRegistration validates one private gateway prefix and returns the
// two typed capability actions. Any missing or unsafe input keeps the capability
// closed; there is no default /etc/nginx or system-service takeover.
func newGatewayRegistration(config gatewayRegistrationConfig) (map[string]agentAction, error) {
	prefix := strings.TrimSpace(config.Prefix)
	if prefix == "" || !filepath.IsAbs(prefix) || filepath.Clean(prefix) != prefix {
		return nil, errGatewayRegistration
	}
	capability, err := newGatewayCapability(gatewayCapabilityConfig{
		Prefix:      prefix,
		SiteDir:     filepath.Join(prefix, "conf"),
		ArtifactDir: filepath.Join(prefix, "artifacts"),
		MainConfig:  filepath.Join(prefix, "nginx.conf"),
		NginxBinary: strings.TrimSpace(config.NginxBinary),
	}, execGatewayRunner{})
	if err != nil {
		return nil, errGatewayRegistration
	}
	return capability.Actions(), nil
}
