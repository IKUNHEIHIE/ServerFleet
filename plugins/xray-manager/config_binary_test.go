package xraymanager

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in local Windows evidence only. It does not install on a node,
// claim Linux compatibility, or include the optional binary in core Release.
func TestPinnedXrayActualConfigTestAndLoopbackStart(t *testing.T) {
	binary := os.Getenv("SF_XRAY_LOCAL_TEST_BINARY")
	if binary == "" {
		t.Skip("UNTESTED: explicit pinned local Xray binary not supplied")
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Fatal("this artifact pin is for Windows amd64 only")
	}
	file, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	_, err = io.Copy(h, file)
	_ = file.Close()
	if err != nil || hex.EncodeToString(h.Sum(nil)) != "9bd2d873d78b5c39303ab16db630a131f9f33836e98d96455f80a8092fecdcd5" {
		t.Fatal("unrecognized test binary", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	version, err := exec.CommandContext(ctx, binary, "version").Output()
	if err != nil || !strings.HasPrefix(string(version), "Xray 26.6.1 ") {
		t.Fatal("wrong target version")
	}
	reservePort := func() uint16 {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := uint16(listener.Addr().(*net.TCPAddr).Port)
		_ = listener.Close()
		return port
	}
	for _, template := range []string{"vless-tls-vision", "vless-reality-vision"} {
		t.Run(template, func(t *testing.T) {
			spec, facts, resolve := templateFixture(t)
			spec.TemplateID = template
			spec.Port = reservePort()
			spec.APIPort = reservePort()
			var config []byte
			if err := WithTemplate(ctx, spec, facts, allowTemplate, resolve, func(_ context.Context, b []byte) error { config = bytes.Clone(b); return nil }); err != nil {
				t.Fatal(err)
			}
			defer clear(config)
			dir := t.TempDir()
			if template == "vless-tls-vision" {
				// Windows-only file location mapping; the Xray config schema is unchanged.
				// Linux target paths and Linux execution remain separate pending evidence.
				key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				cert, _ := x509.ParseCertificate(facts.CertificateDER)
				template := &x509.Certificate{SerialNumber: cert.SerialNumber, DNSNames: cert.DNSNames, NotBefore: cert.NotBefore, NotAfter: cert.NotAfter, KeyUsage: cert.KeyUsage, ExtKeyUsage: cert.ExtKeyUsage}
				der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
				if err != nil {
					t.Fatal(err)
				}
				facts.CertificateDER = der
				clear(config)
				if err = WithTemplate(ctx, spec, facts, allowTemplate, resolve, func(_ context.Context, b []byte) error { config = bytes.Clone(b); return nil }); err != nil {
					t.Fatal(err)
				}
				private, _ := x509.MarshalPKCS8PrivateKey(key)
				certFile := filepath.Join(dir, "cert.pem")
				keyFile := filepath.Join(dir, "key.pem")
				if os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600) != nil || os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600) != nil {
					t.Fatal("local material write failed")
				}
				clear(private)
				var value map[string]any
				if json.Unmarshal(config, &value) != nil {
					t.Fatal("invalid rendered config")
				}
				tls := value["inbounds"].([]any)[0].(map[string]any)["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)
				tls["certificates"] = []any{map[string]any{"certificateFile": certFile, "keyFile": keyFile}}
				config, _ = json.Marshal(value)
			}
			configFile := filepath.Join(dir, "config.json")
			if err = os.WriteFile(configFile, config, 0600); err != nil {
				t.Fatal(err)
			}
			// Never print the tool's raw config/secret-bearing diagnostic output.
			if err = exec.CommandContext(ctx, binary, "run", "-test", "-config", configFile).Run(); err != nil {
				t.Fatal("target-version configuration test failed", err)
			}
			command := exec.CommandContext(ctx, binary, "run", "-config", configFile)
			if err = command.Start(); err != nil {
				t.Fatal(err)
			}
			exited := make(chan error, 1)
			go func() { exited <- command.Wait() }()
			defer func() {
				_ = command.Process.Kill()
				select {
				case <-exited:
				case <-ctx.Done():
				}
			}()
			deadline := time.Now().Add(3 * time.Second)
			started := false
			for time.Now().Before(deadline) {
				select {
				case err := <-exited:
					t.Fatal("Xray exited before loopback health", err)
				default:
				}
				connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(spec.Port))), 50*time.Millisecond)
				if err == nil {
					_ = connection.Close()
					started = true
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if !started {
				t.Fatal("actual process did not open loopback inbound")
			}
			if api, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(spec.APIPort))), time.Second); err != nil {
				t.Fatal("loopback management API unavailable")
			} else {
				_ = api.Close()
			}
		})
	}
}
