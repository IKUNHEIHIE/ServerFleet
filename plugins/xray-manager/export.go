package xraymanager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrClientExport = errors.New("xray-manager: client export rejected or unavailable")
var ErrClientExportUnknown = errors.New("xray-manager: client export outcome unknown; no automatic retry")

type ClientExportRequest struct {
	RequestID, SubjectID, RecipientID, ApprovalID, Source, Format string
	NodeID, InstanceID, UserID                                    string
	ExpiresAt                                                     time.Time
}

// Snapshot comes from the current managed/applied config and persistent user
// record. Its bools/IDs are observations, not credentials or browser assertions.
type ClientExportSnapshot struct {
	NodeID, InstanceID, InboundID, UserID, IdentityRef               string
	TemplateID, TargetVersion, Host, ServerName                      string
	RealityPublicKey, ShortID, Label                                 string
	Port                                                             uint16
	ConfigRevision, UserRevision, PluginRevision, CredentialRevision uint64
	PluginEnabled, UserEnabled                                       bool
	ObservedAt                                                       time.Time
}
type ClientExportBinding struct {
	Request      ClientExportRequest
	Snapshot     ClientExportSnapshot
	DigestSHA256 string
}

func (s ClientExportSnapshot) String() string {
	return "xray-manager.ClientExportSnapshot{client-material:redacted}"
}
func (s ClientExportSnapshot) GoString() string { return s.String() }

func (b ClientExportBinding) String() string {
	return "xray-manager.ClientExportBinding{client-material:redacted}"
}
func (b ClientExportBinding) GoString() string { return b.String() }

type ClientExportReader func(context.Context, ClientExportRequest) (ClientExportSnapshot, error)
type ClientExportAdmission func(context.Context, ClientExportBinding) error

// Borrow MUST use the central credential service's current, persistent one-use
// fence plus complete reveal/confirmation/audit checks. It returns nil only
// after post-use authorization and audit commit. There is no fallback key read.
type ClientExportBorrow func(context.Context, ClientExportBinding, func([]byte) error) error

func validClientExport(req ClientExportRequest, s ClientExportSnapshot, now time.Time) bool {
	for _, id := range []string{req.RequestID, req.SubjectID, req.RecipientID, req.NodeID, req.InstanceID, req.UserID, s.InboundID, s.IdentityRef} {
		if !safeConfigID(id) {
			return false
		}
	}
	if req.RecipientID != req.SubjectID || req.Source != "web" || (req.Format != "vless-uri" && req.Format != "json") || req.ApprovalID != "" && !safeConfigID(req.ApprovalID) || !now.Before(req.ExpiresAt) || req.ExpiresAt.After(now.Add(2*time.Minute)) || s.NodeID != req.NodeID || s.InstanceID != req.InstanceID || s.UserID != req.UserID || !s.PluginEnabled || !s.UserEnabled || s.ConfigRevision == 0 || s.UserRevision == 0 || s.PluginRevision == 0 || s.CredentialRevision == 0 || s.TargetVersion != TestedXrayVersion || s.Port == 0 || s.ObservedAt.IsZero() || now.Before(s.ObservedAt) || now.Sub(s.ObservedAt) > 30*time.Second || !validDNS(s.ServerName) || len(s.Label) > 128 || !utf8.ValidString(s.Label) {
		return false
	}
	for _, r := range s.Label {
		if unicode.IsControl(r) {
			return false
		}
	}
	if ip, err := netip.ParseAddr(s.Host); err == nil {
		if ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() || ip.String() != s.Host {
			return false
		}
	} else if !validDNS(s.Host) {
		return false
	}
	switch s.TemplateID {
	case "vless-tls-vision":
		return s.RealityPublicKey == "" && s.ShortID == ""
	case "vless-reality-vision":
		key, err := base64.RawURLEncoding.DecodeString(s.RealityPublicKey)
		if err != nil || len(key) != 32 || base64.RawURLEncoding.EncodeToString(key) != s.RealityPublicKey || bytes.Equal(key, make([]byte, 32)) {
			return false
		}
		short, err := hex.DecodeString(s.ShortID)
		return err == nil && len(short) > 0 && len(short) <= 8 && hex.EncodeToString(short) == s.ShortID
	default:
		return false
	}
}
func stableExportSnapshot(s ClientExportSnapshot) ClientExportSnapshot {
	s.ObservedAt = time.Time{}
	return s
}
func renderClientExport(req ClientExportRequest, s ClientExportSnapshot, secret []byte) ([]byte, error) {
	if !canonicalVLESSID(secret) {
		return nil, ErrClientExport
	}
	security := "tls"
	if s.TemplateID == "vless-reality-vision" {
		security = "reality"
	}
	if req.Format == "vless-uri" {
		query := url.Values{"encryption": {"none"}, "flow": {"xtls-rprx-vision"}, "type": {"tcp"}, "security": {security}, "sni": {s.ServerName}}
		if security == "reality" {
			query.Set("fp", "chrome")
			query.Set("pbk", s.RealityPublicKey)
			query.Set("sid", s.ShortID)
		}
		u := url.URL{Scheme: "vless", User: url.User(string(secret)), Host: net.JoinHostPort(s.Host, strconv.Itoa(int(s.Port))), RawQuery: query.Encode(), Fragment: s.Label}
		return []byte(u.String()), nil
	}
	stream := map[string]any{"network": "raw", "security": security}
	if security == "tls" {
		stream["tlsSettings"] = map[string]any{"serverName": s.ServerName, "minVersion": "1.3"}
	} else {
		stream["realitySettings"] = map[string]any{"serverName": s.ServerName, "fingerprint": "chrome", "password": s.RealityPublicKey, "shortId": s.ShortID}
	}
	// Outbound config only: importing a client connection does not silently
	// create a local listener, expose an API or overwrite an existing profile.
	return json.Marshal(map[string]any{"outbounds": []any{map[string]any{"tag": "sf-xray-user-" + s.UserID, "protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": s.Host, "port": s.Port, "users": []any{map[string]any{"id": string(secret), "encryption": "none", "flow": "xtls-rprx-vision"}}}}}, "streamSettings": stream}}})
}

// WithClientExport delivers one user's URI/outbound JSON only AFTER the central
// credential loan has completed its durable audit/current checks. It never
// loads an Agent, credential DB, private key, HTTP route or QR runtime itself.
func WithClientExport(ctx context.Context, req ClientExportRequest, read ClientExportReader, admit ClientExportAdmission, borrow ClientExportBorrow, deliver func(context.Context, string, []byte, time.Time) error) (result error) {
	var output []byte
	defer func() {
		clear(output)
		if recover() != nil {
			result = ErrClientExportUnknown
		}
	}()
	if ctx == nil || ctx.Err() != nil || read == nil || admit == nil || borrow == nil || deliver == nil {
		return ErrClientExport
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	snapshot, err := read(bounded, req)
	if err != nil || !validClientExport(req, snapshot, time.Now()) {
		return ErrClientExport
	}
	raw, _ := json.Marshal(struct {
		Domain   string
		Request  ClientExportRequest
		Snapshot ClientExportSnapshot
	}{"serverfleet.xray.client-export.v1", req, stableExportSnapshot(snapshot)})
	hash := sha256.Sum256(raw)
	binding := ClientExportBinding{req, snapshot, hex.EncodeToString(hash[:])}
	current := func() error {
		observed, err := read(bounded, req)
		if err != nil || !validClientExport(req, observed, time.Now()) || stableExportSnapshot(observed) != stableExportSnapshot(snapshot) || admit(bounded, binding) != nil || bounded.Err() != nil {
			return ErrClientExport
		}
		return nil
	}
	if current() != nil {
		return ErrClientExport
	}
	var mu sync.Mutex
	live, called, poisoned := true, false, false
	borrowErr := borrow(bounded, binding, func(secret []byte) error {
		mu.Lock()
		defer mu.Unlock()
		if !live || called || bounded.Err() != nil {
			if live {
				poisoned = true
			}
			return ErrClientExport
		}
		called = true
		var err error
		output, err = renderClientExport(req, snapshot, secret)
		if err != nil || len(output) > 16<<10 {
			clear(output)
			output = nil
			return ErrClientExport
		}
		return nil
	})
	mu.Lock()
	live = false
	rendered := called && !poisoned && len(output) > 0
	mu.Unlock()
	if borrowErr != nil || !rendered || current() != nil {
		return ErrClientExportUnknown
	}
	if deliver(bounded, req.Format, output, req.ExpiresAt) != nil || current() != nil {
		return ErrClientExportUnknown
	}
	return nil
}
