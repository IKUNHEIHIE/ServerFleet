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
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/transport"
	"serverfleet.local/serverfleet/internal/updates"
	"strings"
	"time"
)

func RunSuperviseCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("controller supervise", flag.ContinueOnError)
	flags.SetOutput(stderr)
	filename := flags.String("config", "", "private operator-provisioned resident supervisor config")
	describe := flags.Bool("describe", false, "print public policy digest and component without starting a process")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return updates.ErrArtifactRejected
	}
	c, e := updates.LoadResidentSupervisor(*filename)
	if e != nil {
		return e
	}
	if *describe {
		return json.NewEncoder(stdout).Encode(map[string]string{"component": c.Component, "nodeId": c.NodeID, "policySha256": updates.ResidentPolicySHA(c)})
	}
	var hooks updates.ResidentSupervisorHooks
	var closeHook func() error
	var latest *transport.ResidentUpdateIntent
	var connection string
	if c.Component == "controller" {
		var latch *recovery.QuarantineLatch
		if c.RecoveryRuntime != "" {
			latch, e = openActivatedRecovery(ctx, c.RecoveryRuntime, c)
			if e != nil {
				return e
			}
			defer latch.Close()
		}
		hooks, closeHook, e = server.ControllerSupervisorHooksWithRecovery(ctx, c, latch)
		if e != nil {
			return e
		}
		defer closeHook()
		hooks.Ready = controllerResidentReady(c)
		if c.FenceConfiguration != "" {
			hooks.ServeFence = func(ctx context.Context, control updates.ResidentFenceControl) (func() error, error) {
				return serveRecoveryFence(ctx, c, control)
			}
		}
	} else if c.Component == "agent" {
		call := func(ctx context.Context, i transport.ResidentUpdateIntent, phase string, receipt *updates.InstallReceipt) (string, error) {
			cert, e := supervisorNodeCertificate(c)
			if e != nil {
				return "", e
			}
			roots, e := recovery.ReadPrivateFile(c.RootCertificate, 128<<10)
			if e != nil {
				return "", e
			}
			client, e := updates.PinnedHTTPSClient(updates.HTTPReleaseFeed{Origin: c.ControllerOrigin, Addresses: c.ControllerAddresses, RootPEM: roots}, &cert)
			if e != nil {
				return "", e
			}
			defer client.CloseIdleConnections()
			var nonce [16]byte
			if _, e = rand.Read(nonce[:]); e != nil {
				return "", e
			}
			text := hex.EncodeToString(nonce[:])
			body, _ := json.Marshal(struct {
				Intent       transport.ResidentUpdateIntent `json:"intent"`
				Phase        string                         `json:"phase"`
				Nonce        string                         `json:"nonce"`
				ConnectionID string                         `json:"connectionId,omitempty"`
				Receipt      *updates.InstallReceipt        `json:"receipt,omitempty"`
			}{i, phase, text, connection, receipt})
			req, e := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(c.ControllerOrigin, "/")+"/api/v1/agent/update-admission", bytes.NewReader(body))
			if e != nil {
				return "", e
			}
			req.Header.Set("Content-Type", "application/json")
			response, e := client.Do(req)
			if e != nil {
				return "", e
			}
			defer response.Body.Close()
			var reply map[string]string
			d := json.NewDecoder(io.LimitReader(response.Body, 4097))
			if response.StatusCode != 200 || d.Decode(&reply) != nil || len(reply) != 6 || reply["nonce"] != text || reply["phase"] != phase || reply["taskId"] != i.TaskID || reply["executionId"] != i.ExecutionID || reply["planHash"] != i.PlanSHA256 || !transport.FileID(reply["auditRecordId"]) {
				return "", updates.ErrApplyUnknown
			}
			return reply["auditRecordId"], nil
		}
		hooks.Current = func(ctx context.Context, i transport.ResidentUpdateIntent, phase string) error {
			if !i.Valid(time.Now()) {
				return updates.ErrApplyUnknown
			}
			latest = &i
			_, e := call(ctx, i, phase, nil)
			return e
		}
		hooks.Ready = updates.AgentProductReady(c.HealthFile, c.NodeID, func(ctx context.Context, node, id string) error {
			if node != c.NodeID || !transport.FileID(id) {
				return updates.ErrApplyUnknown
			}
			connection = id
			if latest == nil {
				return nil
			} // initial readiness still requires real Agent WSS bootstrap
			_, e := call(ctx, *latest, "ready", nil)
			return e
		})
		var state string
		hooks.BeforeStop = func(ctx context.Context, i transport.ResidentUpdateIntent) error {
			if hooks.Current(ctx, i, "before-stop") != nil {
				return updates.ErrApplyUnknown
			}
			digest, e := supervisorStateDigest(c.IdentityDir)
			if e != nil {
				return e
			}
			if state == "" {
				state = digest
			} else if state != digest {
				return updates.ErrApplyUnknown
			}
			return nil
		}
		hooks.RollbackSafe = func(ctx context.Context) error {
			digest, e := supervisorStateDigest(c.IdentityDir)
			if e != nil || state == "" || digest != state {
				return updates.ErrApplyUnknown
			}
			return ctx.Err()
		}
		hooks.Commit = func(ctx context.Context, i transport.ResidentUpdateIntent, r updates.InstallReceipt) (string, error) {
			id, e := call(ctx, i, "commit", &r)
			if e == nil {
				state = ""
			}
			return id, e
		}
	} else {
		return updates.ErrArtifactRejected
	}
	return updates.RunResidentSupervisor(ctx, c, hooks)
}
func supervisorNodeCertificate(c updates.ResidentSupervisorConfig) (tls.Certificate, error) {
	var zero tls.Certificate
	raw, e := recovery.ReadPrivateFile(filepath.Join(c.IdentityDir, "issued.json"), 64<<10)
	if e != nil {
		return zero, e
	}
	var issued transport.AgentEnrollmentResult
	if json.Unmarshal(raw, &issued) != nil || issued.State != "approved" || issued.NodeID != c.NodeID || issued.TrustGeneration == 0 || time.Until(issued.ExpiresAt) < time.Minute {
		return zero, updates.ErrArtifactRejected
	}
	key, e := recovery.ReadPrivateFile(filepath.Join(c.IdentityDir, "identity-key.pem"), 16<<10)
	if e != nil {
		return zero, e
	}
	defer clear(key)
	cert, e := tls.X509KeyPair([]byte(issued.CertificatePEM), key)
	if e != nil || len(cert.Certificate) < 2 {
		return zero, updates.ErrArtifactRejected
	}
	leaf, e := x509.ParseCertificate(cert.Certificate[0])
	if e != nil || leaf.IsCA || leaf.Subject.CommonName != c.NodeID {
		return zero, updates.ErrArtifactRejected
	}
	root, e := x509.ParseCertificate(cert.Certificate[len(cert.Certificate)-1])
	if e != nil || !root.IsCA || root.CheckSignatureFrom(root) != nil {
		return zero, updates.ErrArtifactRejected
	}
	sum := sha256.Sum256(root.RawSubjectPublicKeyInfo)
	if hex.EncodeToString(sum[:]) != c.RootPin {
		return zero, updates.ErrArtifactRejected
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	intermediates := x509.NewCertPool()
	for _, der := range cert.Certificate[1 : len(cert.Certificate)-1] {
		v, e := x509.ParseCertificate(der)
		if e != nil {
			return zero, e
		}
		intermediates.AddCert(v)
	}
	if _, e = leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); e != nil {
		return zero, e
	}
	return cert, nil
}
func supervisorStateDigest(directory string) (string, error) {
	root, e := os.OpenRoot(directory)
	if e != nil {
		return "", e
	}
	defer root.Close()
	info, e := root.Stat(".")
	if e != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return "", updates.ErrArtifactRejected
	}
	hash := sha256.New()
	count, total := 0, int64(0)
	e = filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			i, e := entry.Info()
			if e != nil || i.Mode().Perm() != 0700 {
				return updates.ErrArtifactRejected
			}
			return nil
		}
		info, e := entry.Info()
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return updates.ErrArtifactRejected
		}
		count++
		total += info.Size()
		if count > 10000 || total > 128<<20 {
			return updates.ErrArtifactRejected
		}
		name, e := filepath.Rel(directory, path)
		if e != nil {
			return e
		}
		raw, e := recovery.ReadPrivateFile(path, 16<<20)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(raw)
		clear(raw)
		b, _ := json.Marshal(struct {
			Name   string
			Size   int64
			Digest string
		}{filepath.ToSlash(name), info.Size(), hex.EncodeToString(sum[:])})
		hash.Write(b)
		return nil
	})
	if e != nil {
		return "", e
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func controllerResidentReady(c updates.ResidentSupervisorConfig) func(context.Context, updates.ReleaseManifest, updates.OwnedProcessObservation) error {
	return func(ctx context.Context, m updates.ReleaseManifest, p updates.OwnedProcessObservation) error {
		roots, e := recovery.ReadPrivateFile(c.RootCertificate, 128<<10)
		if e != nil {
			return e
		}
		client, e := updates.PinnedHTTPSClient(updates.HTTPReleaseFeed{Origin: c.ControllerOrigin, Addresses: c.ControllerAddresses, RootPEM: roots}, nil)
		if e != nil {
			return e
		}
		defer client.CloseIdleConnections()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			raw, e := recovery.ReadPrivateFile(c.HealthFile, 8192)
			var report struct {
				Version             string
				PID                 int
				Nonce, SchemaSHA256 string
				ObservedAt          time.Time
			}
			if e == nil && json.Unmarshal(raw, &report) == nil && report.PID == p.PID && report.Nonce == p.Nonce && !report.ObservedAt.Before(p.StartedAt) && time.Since(report.ObservedAt) >= 0 && time.Since(report.ObservedAt) < 3*time.Second {
				if report.Version != m.Version || report.SchemaSHA256 != c.SchemaSHA256 {
					return updates.ErrHealthUnhealthy
				}
				req, e := http.NewRequestWithContext(ctx, "GET", strings.TrimSuffix(c.ControllerOrigin, "/")+"/internal/resident-ready", nil)
				if e != nil {
					return e
				}
				req.Header.Set("X-SF-Resident-Nonce", p.Nonce)
				response, e := client.Do(req)
				if e == nil {
					_, re := io.Copy(io.Discard, io.LimitReader(response.Body, 8192))
					response.Body.Close()
					if response.StatusCode == 200 && re == nil {
						return nil
					}
				}
			}
			select {
			case <-ctx.Done():
				return updates.ErrApplyUnknown
			case <-ticker.C:
			}
		}
	}
}
