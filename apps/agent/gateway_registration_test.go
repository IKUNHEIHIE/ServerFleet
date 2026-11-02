package main

import (
	"os"
	"path/filepath"
	"testing"
)

func newGatewayRegistrationFixture(t *testing.T) (string, string) {
	t.Helper()
	prefix := t.TempDir()
	if err := os.Chmod(prefix, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{filepath.Join(prefix, "conf"), filepath.Join(prefix, "artifacts")} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(prefix, "nginx.conf"),
		[]byte("events {}\nhttp { include conf/*.conf; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return prefix, binary
}

func TestGatewayRegistrationExposesTypedActions(t *testing.T) {
	prefix, binary := newGatewayRegistrationFixture(t)
	actions, err := newGatewayRegistration(gatewayRegistrationConfig{Prefix: prefix, NginxBinary: binary})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 2 {
		t.Fatalf("actions = %d, want 2", len(actions))
	}
	for _, action := range []string{gatewayApplyAction, gatewayRemoveAction} {
		if actions[action].Validate == nil || actions[action].Run == nil {
			t.Fatalf("action %s is not fully registered", action)
		}
	}
}

func TestGatewayRegistrationKeepsCapabilityClosedOnUnsafeConfiguration(t *testing.T) {
	prefix, binary := newGatewayRegistrationFixture(t)
	public := t.TempDir()
	if err := os.Chmod(public, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		config gatewayRegistrationConfig
	}{
		{name: "empty prefix", config: gatewayRegistrationConfig{NginxBinary: binary}},
		{name: "relative prefix", config: gatewayRegistrationConfig{Prefix: "gateway", NginxBinary: binary}},
		{name: "unclean prefix", config: gatewayRegistrationConfig{Prefix: prefix + "/conf/..", NginxBinary: binary}},
		{name: "group accessible prefix", config: gatewayRegistrationConfig{Prefix: public, NginxBinary: binary}},
		{name: "missing binary", config: gatewayRegistrationConfig{Prefix: prefix, NginxBinary: filepath.Join(prefix, "missing-nginx")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if actions, err := newGatewayRegistration(tc.config); err == nil || actions != nil {
				t.Fatalf("actions = %v, err = %v; want closed capability", actions, err)
			}
		})
	}
}
