package bootstrap

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"serverfleet.local/serverfleet/apps/controller/localpki"
	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/updates"
)

// This listener belongs to the independent parent, never the Controller child.
// Its narrowly provisioned mTLS peer can present offline Root authority to fence
// ONLY that parent's owned child. It has no command/path/PID execution API.
type recoveryFenceServer struct {
	Version                 int    `json:"version"`
	Listen                  string `json:"listen"`
	Certificate             string `json:"certificate"`
	Key                     string `json:"key"`
	ClientRoot              string `json:"clientRoot"`
	ClientCertificateSHA256 string `json:"clientCertificateSha256"`
	RootCertificate         string `json:"rootCertificate"`
	RootPin                 string `json:"rootPin"`
	AuthorizationDirectory  string `json:"authorizationDirectory"`
	MinimumTrustGeneration  uint64 `json:"minimumTrustGeneration"`
}
type recoveryFenceClient struct {
	Origin                  string   `json:"origin"`
	Addresses               []string `json:"addresses"`
	RootCertificate         string   `json:"rootCertificate"`
	RootSHA256              string   `json:"rootSha256"`
	ServerCertificateSHA256 string   `json:"serverCertificateSha256"`
	Certificate             string   `json:"certificate"`
	Key                     string   `json:"key"`
}
type recoveryFenceCall struct {
	Nonce         string                        `json:"nonce"`
	Authorization json.RawMessage               `json:"authorization,omitempty"`
	Request       *updates.ResidentFenceRequest `json:"request,omitempty"`
}
type recoveryFenceReply struct {
	Nonce      string                      `json:"nonce"`
	Record     updates.ResidentFenceRecord `json:"record"`
	ObservedAt time.Time                   `json:"observedAt"`
}

func recoveryDigest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func strictRecoveryJSON(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(&struct{}{}) != io.EOF {
		return recovery.ErrRecoveryRejected
	}
	return nil
}
func loadFenceServer(filename string) (recoveryFenceServer, error) {
	var c recoveryFenceServer
	raw, e := recovery.ReadPrivateFile(filename, 32<<10)
	if e != nil || strictRecoveryJSON(raw, &c) != nil || c.Version != 1 || len(c.ClientCertificateSHA256) != 64 || c.MinimumTrustGeneration == 0 {
		return c, recovery.ErrRecoveryRejected
	}
	host, _, e := net.SplitHostPort(c.Listen)
	if e != nil || net.ParseIP(host) == nil {
		return c, recovery.ErrRecoveryRejected
	}
	return c, nil
}
func serveRecoveryFence(ctx context.Context, c updates.ResidentSupervisorConfig, control updates.ResidentFenceControl) (func() error, error) {
	f, e := loadFenceServer(c.FenceConfiguration)
	if e != nil {
		return nil, e
	}
	raw, e := recovery.ReadPrivateFile(c.FenceConfiguration, 32<<10)
	if e != nil || recoveryDigest(raw) != c.FenceConfigurationSHA256 {
		return nil, recovery.ErrRecoveryRejected
	}
	certPEM, e := recovery.ReadPrivateFile(f.Certificate, 128<<10)
	if e != nil {
		return nil, e
	}
	key, e := recovery.ReadPrivateFile(f.Key, 128<<10)
	if e != nil {
		return nil, e
	}
	defer clear(key)
	pair, e := tls.X509KeyPair(certPEM, key)
	if e != nil {
		return nil, e
	}
	rootsPEM, e := recovery.ReadPrivateFile(f.ClientRoot, 128<<10)
	if e != nil {
		return nil, e
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootsPEM) {
		return nil, recovery.ErrRecoveryRejected
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reject := func() { http.Error(w, "recovery fence rejected", 403) }
		if r.Method != "POST" || (r.URL.Path != "/v1/fence" && r.URL.Path != "/v1/fence/current") || r.URL.RawQuery != "" || r.TLS == nil || r.TLS.Version < tls.VersionTLS13 || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 || recoveryDigest(r.TLS.PeerCertificates[0].Raw) != f.ClientCertificateSHA256 || r.Header.Get("Content-Type") != "application/json" {
			reject()
			return
		}
		for _, name := range []string{"Origin", "Cookie", "Authorization", "Proxy-Authorization", "Referer"} {
			if len(r.Header.Values(name)) > 0 {
				reject()
				return
			}
		}
		body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 128<<10))
		var call recoveryFenceCall
		if e != nil || strictRecoveryJSON(body, &call) != nil || len(call.Nonce) != 32 {
			reject()
			return
		}
		if b, e := hex.DecodeString(call.Nonce); e != nil || len(b) != 16 {
			reject()
			return
		}
		bounded, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var request updates.ResidentFenceRequest
		var record updates.ResidentFenceRecord
		if r.URL.Path == "/v1/fence" {
			entries, e := os.ReadDir(f.AuthorizationDirectory)
			if e != nil || len(entries) > 32 {
				reject()
				return
			} // bound the authenticated immutable-document cache
			if len(call.Authorization) == 0 || call.Request != nil {
				reject()
				return
			}
			path := filepath.Join(f.AuthorizationDirectory, "authorization-"+recoveryDigest(call.Authorization)+".json")
			// The directory is provisioned privately by the operator. No request path.
			if _, e := os.Lstat(path); errors.Is(e, os.ErrNotExist) {
				if recovery.WriteOfflineRecord(f.AuthorizationDirectory, filepath.Base(path), json.RawMessage(call.Authorization)) != nil {
					reject()
					return
				}
			}
			saved, e := recovery.ReadPrivateFile(path, 128<<10)
			if e != nil || !bytes.Equal(saved, call.Authorization) {
				reject()
				return
			}
			authority, e := localpki.VerifyRecoveryAuthority(f.RootCertificate, path, f.RootPin)
			if e != nil {
				reject()
				return
			}
			p := authority.Payload()
			if p.OldAuthorityID != c.NodeID || p.TrustGeneration < f.MinimumTrustGeneration {
				reject()
				return
			}
			request = updates.ResidentFenceRequest{AuthorizationSHA256: authority.SHA256(), OldAuthorityID: p.OldAuthorityID, NewAuthorityID: p.NewAuthorityID, TrustGeneration: p.TrustGeneration}
			record, e = control.Fence(bounded, request)
		} else {
			if len(call.Authorization) != 0 || call.Request == nil {
				reject()
				return
			}
			request = *call.Request
			record, e = control.Current(bounded, request)
		}
		if e != nil || bounded.Err() != nil {
			http.Error(w, "owned process fence unknown", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(recoveryFenceReply{call.Nonce, record, time.Now().UTC()})
	})
	listener, e := net.Listen("tcp", f.Listen)
	if e != nil {
		return nil, e
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 6 * time.Second, WriteTimeout: 6 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 8192, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert}}
	go func() { _ = srv.Serve(tls.NewListener(listener, srv.TLSConfig)) }()
	// Shutdown is tied to the actual parent lifecycle, including failed startup.
	go func() { <-ctx.Done(); _ = srv.Close() }()
	return srv.Close, nil
}
func callRecoveryFence(ctx context.Context, c recoveryFenceClient, request updates.ResidentFenceRequest, authorization []byte) (updates.ResidentFenceRecord, error) {
	var zero updates.ResidentFenceRecord
	roots, e := recovery.ReadPrivateFile(c.RootCertificate, 128<<10)
	if e != nil || recoveryDigest(roots) != c.RootSHA256 || len(c.ServerCertificateSHA256) != 64 {
		return zero, recovery.ErrQuarantined
	}
	pem, e := recovery.ReadPrivateFile(c.Certificate, 128<<10)
	if e != nil {
		return zero, e
	}
	key, e := recovery.ReadPrivateFile(c.Key, 128<<10)
	if e != nil {
		return zero, e
	}
	defer clear(key)
	cert, e := tls.X509KeyPair(pem, key)
	if e != nil {
		return zero, e
	}
	client, e := updates.PinnedHTTPSClient(updates.HTTPReleaseFeed{Origin: c.Origin, Addresses: c.Addresses, RootPEM: roots}, &cert)
	if e != nil {
		return zero, e
	}
	defer client.CloseIdleConnections()
	// In addition to the configured CA/IP policy, pin this independent parent's
	// leaf. An old Controller or a different witness cannot answer in its place.
	tr, ok := client.Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil {
		return zero, recovery.ErrQuarantined
	}
	tc := tr.TLSClientConfig.Clone()
	previous := tc.VerifyConnection
	tc.VerifyConnection = func(s tls.ConnectionState) error {
		if previous != nil {
			if e := previous(s); e != nil {
				return e
			}
		}
		if len(s.PeerCertificates) == 0 || recoveryDigest(s.PeerCertificates[0].Raw) != c.ServerCertificateSHA256 {
			return recovery.ErrQuarantined
		}
		return nil
	}
	tr.TLSClientConfig = tc
	var nonce [16]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		return zero, e
	}
	call := recoveryFenceCall{Nonce: hex.EncodeToString(nonce[:]), Request: &request}
	path := "/v1/fence/current"
	if len(authorization) > 0 {
		call.Request = nil
		call.Authorization = authorization
		path = "/v1/fence"
	}
	b, _ := json.Marshal(call)
	req, e := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(c.Origin, "/")+path, bytes.NewReader(b))
	if e != nil {
		return zero, e
	}
	req.Header.Set("Content-Type", "application/json")
	started := time.Now().UTC()
	response, e := client.Do(req)
	if e != nil {
		return zero, e
	}
	defer response.Body.Close()
	body, e := io.ReadAll(io.LimitReader(response.Body, 8193))
	var reply recoveryFenceReply
	if e != nil || len(body) > 8192 || response.StatusCode != 200 || strictRecoveryJSON(body, &reply) != nil || reply.Nonce != call.Nonce || reply.Record.Request != request || len(reply.Record.SupervisorPolicySHA256) != 64 || len(reply.Record.ImageSHA256) != 64 || reply.Record.ConfirmedAt.IsZero() || reply.Record.ConfirmedAt.After(reply.ObservedAt) || reply.ObservedAt.Before(started.Add(-2*time.Second)) || reply.ObservedAt.After(time.Now().Add(2*time.Second)) {
		return zero, recovery.ErrQuarantined
	}
	return reply.Record, nil
}
