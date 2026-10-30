package xraymanager

import (
	"context"
	"crypto/ecdh"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"path"
	"strconv"
	"strings"
	"time"
)

var ErrConfigurationRejected = errors.New("xray-manager: configuration rejected")

const TestedXrayVersion = "26.6.1"

type TemplateUser struct {
	ID, IdentityRef string
	Enabled         bool
}
type TemplateSpec struct {
	InstanceID, NodeID, TemplateID, TargetVersion, Listen, Domain, CertificateRef, RealityKeyRef, RealityTarget string
	Port, APIPort                                                                                               uint16
	ExpectedRevision                                                                                            uint64
	ServerNames, ShortIDs                                                                                       []string
	Users                                                                                                       []TemplateUser
	RealityFallbackAcknowledged                                                                                 bool
}

// PreflightFacts MUST come from current, controlled read operations, not a
// browser/model claim. Admission must bind them to task/actor/node/config plan.
type PreflightFacts struct {
	NodeID, InstanceID, OS, Architecture, Version                             string
	Revision, PluginRevision                                                  uint64
	PluginEnabled, PortAvailable, APIPortAvailable, Managed, AdoptionApproved bool
	ObservedAt                                                                time.Time
	CertificateDER                                                            []byte
	CertificateFile, PrivateKeyFile                                           string
	RealityTLS13Reachable                                                     bool
}
type ConfigAdmission func(context.Context, TemplateSpec, PreflightFacts) error

// MaterialResolver is the program-registered service bridge. It validates the
// current task, owner instance and purpose before borrowing secret bytes. There
// is no generic HTTP/AI/Host secret read method and no arbitrary OS/SSH call here.
type MaterialResolver func(context.Context, string, string, string, func([]byte) error) error

func safeConfigID(id string) bool {
	return id != "" && len(id) <= 128 && !strings.Contains(id, ">>>") && !strings.ContainsAny(id, "/\\*?[] \t\r\n\x00")
}
func validDNS(name string) bool {
	if name == "" || len(name) > 253 || strings.ToLower(name) != name || strings.ContainsAny(name, " /\\:@*?[]\r\n\x00") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}
func validateTemplate(spec TemplateSpec, facts PreflightFacts, now time.Time) error {
	if !safeConfigID(spec.InstanceID) || !safeConfigID(spec.NodeID) || spec.TargetVersion != TestedXrayVersion || facts.Version != spec.TargetVersion || facts.NodeID != spec.NodeID || facts.InstanceID != spec.InstanceID || facts.OS != "linux" || (facts.Architecture != "amd64" && facts.Architecture != "arm64") || spec.ExpectedRevision == 0 || facts.Revision != spec.ExpectedRevision || facts.PluginRevision == 0 || !facts.PluginEnabled || !facts.PortAvailable || !facts.APIPortAvailable || !facts.Managed && !facts.AdoptionApproved || facts.ObservedAt.IsZero() || now.Before(facts.ObservedAt) || now.Sub(facts.ObservedAt) > 30*time.Second || spec.Port == 0 || spec.APIPort == 0 || spec.APIPort == spec.Port || len(spec.Users) == 0 || len(spec.Users) > 1000 {
		return ErrConfigurationRejected
	}
	if addr, err := netip.ParseAddr(spec.Listen); err != nil || addr.IsMulticast() {
		return ErrConfigurationRejected
	}
	users := map[string]bool{}
	refs := map[string]bool{}
	enabled := 0
	for _, u := range spec.Users {
		if !safeConfigID(u.ID) || !safeConfigID(u.IdentityRef) || users[u.ID] || refs[u.IdentityRef] {
			return ErrConfigurationRejected
		}
		users[u.ID] = true
		refs[u.IdentityRef] = true
		if u.Enabled {
			enabled++
		}
	}
	if enabled == 0 {
		return ErrConfigurationRejected
	}
	switch spec.TemplateID {
	case "vless-tls-vision":
		if !validDNS(spec.Domain) || !safeConfigID(spec.CertificateRef) {
			return ErrConfigurationRejected
		}
		cert, err := x509.ParseCertificate(facts.CertificateDER)
		if err != nil || now.Before(cert.NotBefore) || !now.Add(time.Hour).Before(cert.NotAfter) || cert.VerifyHostname(spec.Domain) != nil {
			return ErrConfigurationRejected
		}
		prefix := "/var/lib/serverfleet/xray/" + spec.InstanceID + "/tls/"
		for _, file := range []string{facts.CertificateFile, facts.PrivateKeyFile} {
			if !strings.HasPrefix(file, prefix) || path.Clean(file) != file || strings.Contains(file, "\\") {
				return ErrConfigurationRejected
			}
		}
	case "vless-reality-vision":
		if !safeConfigID(spec.RealityKeyRef) || !spec.RealityFallbackAcknowledged || !facts.RealityTLS13Reachable || len(spec.ServerNames) == 0 || len(spec.ServerNames) > 16 || len(spec.ShortIDs) == 0 || len(spec.ShortIDs) > 16 {
			return ErrConfigurationRejected
		}
		host, port, err := net.SplitHostPort(spec.RealityTarget)
		number, e := strconv.Atoi(port)
		if err != nil || e != nil || number < 1 || number > 65535 || !validDNS(host) {
			return ErrConfigurationRejected
		}
		seen := map[string]bool{}
		for _, name := range spec.ServerNames {
			if !validDNS(name) || seen[name] {
				return ErrConfigurationRejected
			}
			seen[name] = true
		}
		seen = map[string]bool{}
		for _, id := range spec.ShortIDs {
			b, err := hex.DecodeString(id)
			if err != nil || len(b) == 0 || len(b) > 8 || hex.EncodeToString(b) != id || seen[id] {
				return ErrConfigurationRejected
			}
			seen[id] = true
		}
	default:
		return ErrConfigurationRejected
	}
	return nil
}
func canonicalVLESSID(secret []byte) bool {
	s := string(secret)
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	raw := strings.ReplaceAll(s, "-", "")
	b, err := hex.DecodeString(raw)
	return err == nil && len(b) == 16 && hex.EncodeToString(b) == raw && b[6]>>4 == 4 && b[8]>>6 == 2
}

// WithTemplate renders secrets only into a temporary configuration consumed by
// the controlled Agent operation. Never mount consume as an export endpoint or
// send raw config to the model/logs. The renderer neither installs nor tests or
// starts Xray; target-version run -test and health are separate real task stages.
func WithTemplate(ctx context.Context, spec TemplateSpec, facts PreflightFacts, admit ConfigAdmission, resolve MaterialResolver, consume func(context.Context, []byte) error) (result error) {
	defer func() {
		if recover() != nil {
			result = ErrConfigurationRejected
		}
	}()
	spec.Users = append([]TemplateUser{}, spec.Users...)
	spec.ServerNames = append([]string{}, spec.ServerNames...)
	spec.ShortIDs = append([]string{}, spec.ShortIDs...)
	facts.CertificateDER = append([]byte{}, facts.CertificateDER...)
	if ctx == nil || ctx.Err() != nil || admit == nil || resolve == nil || consume == nil || validateTemplate(spec, facts, time.Now()) != nil || admit(ctx, spec, facts) != nil {
		return ErrConfigurationRejected
	}
	users := []any{}
	for _, u := range spec.Users {
		if !u.Enabled {
			continue
		}
		err := resolve(ctx, spec.InstanceID, "xray-user", u.IdentityRef, func(secret []byte) error {
			if !canonicalVLESSID(secret) {
				return ErrConfigurationRejected
			}
			users = append(users, map[string]any{"id": string(secret), "email": "sf-user-" + u.ID, "flow": "xtls-rprx-vision", "level": 0})
			return nil
		})
		if err != nil {
			return ErrConfigurationRejected
		}
	}
	stream := map[string]any{"network": "raw"}
	if spec.TemplateID == "vless-tls-vision" {
		stream["security"] = "tls"
		stream["tlsSettings"] = map[string]any{"serverName": spec.Domain, "minVersion": "1.3", "certificates": []any{map[string]any{"certificateFile": facts.CertificateFile, "keyFile": facts.PrivateKeyFile}}}
	} else {
		stream["security"] = "reality"
		err := resolve(ctx, spec.InstanceID, "xray-reality", spec.RealityKeyRef, func(secret []byte) error {
			key, err := base64.RawURLEncoding.DecodeString(string(secret))
			if err != nil || len(key) != 32 {
				return ErrConfigurationRejected
			}
			defer clear(key)
			if _, err = ecdh.X25519().NewPrivateKey(key); err != nil {
				return ErrConfigurationRejected
			}
			stream["realitySettings"] = map[string]any{"show": false, "target": spec.RealityTarget, "xver": 0, "serverNames": spec.ServerNames, "privateKey": string(secret), "shortIds": spec.ShortIDs}
			return nil
		})
		if err != nil {
			return ErrConfigurationRejected
		}
	}
	raw, err := renderTemplate(spec, users, stream)
	if err != nil {
		return err
	}
	defer clear(raw)
	if ctx.Err() != nil || admit(ctx, spec, facts) != nil {
		return ErrConfigurationRejected
	}
	if consume(ctx, raw) != nil || ctx.Err() != nil {
		return ErrConfigurationRejected
	}
	return nil
}
func renderTemplate(spec TemplateSpec, users []any, stream map[string]any) ([]byte, error) {
	config := map[string]any{
		"log": map[string]any{"loglevel": "none"}, "stats": map[string]any{},
		"api":       map[string]any{"tag": "sf-api", "services": []string{"StatsService", "HandlerService"}},
		"policy":    map[string]any{"levels": map[string]any{"0": map[string]any{"statsUserUplink": true, "statsUserDownlink": true}}, "system": map[string]any{"statsInboundUplink": true, "statsInboundDownlink": true}},
		"inbounds":  []any{map[string]any{"tag": "sf-vless-" + spec.InstanceID, "listen": spec.Listen, "port": spec.Port, "protocol": "vless", "settings": map[string]any{"users": users, "decryption": "none"}, "streamSettings": stream}, map[string]any{"tag": "sf-api-in", "listen": "127.0.0.1", "port": spec.APIPort, "protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1"}}},
		"outbounds": []any{map[string]any{"tag": "sf-direct", "protocol": "freedom"}, map[string]any{"tag": "sf-block", "protocol": "blackhole"}},
		// Loopback binding alone does not protect an unauthenticated local API
		// from proxy clients. Only the dedicated local inbound can reach it.
		"routing": map[string]any{"rules": []any{
			map[string]any{"type": "field", "inboundTag": []string{"sf-api-in"}, "outboundTag": "sf-api"},
			map[string]any{"type": "field", "port": strconv.Itoa(int(spec.APIPort)), "outboundTag": "sf-block"},
			map[string]any{"type": "field", "ip": []string{"127.0.0.0/8", "::1/128"}, "outboundTag": "sf-block"},
		}},
	}
	raw, err := json.Marshal(config)
	if err != nil || len(raw) > 1<<20 {
		return nil, ErrConfigurationRejected
	}
	return raw, nil
}
