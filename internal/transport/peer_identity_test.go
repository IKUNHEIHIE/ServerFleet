package transport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"
)

func testPeerCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey, *x509.CertPool) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "ephemeral-test-ca"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	ca, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return ca, key, pool
}
func testPeerCert(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, server bool) tls.Certificate {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	eku := x509.ExtKeyUsageClientAuth
	serial := int64(2)
	if server {
		eku = x509.ExtKeyUsageServerAuth
		serial = 3
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(serial), DNSNames: []string{"fleet.test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(30 * time.Minute), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{eku}}
	der, e := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if e != nil {
		t.Fatal(e)
	}
	leaf, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

func TestPeerRealLocalTLSAndLiveRevocation(t *testing.T) {
	ca, key, roots := testPeerCA(t)
	serverCert := testPeerCert(t, ca, key, true)
	clientCert := testPeerCert(t, ca, key, false)
	h := sha256.Sum256(clientCert.Certificate[0])
	fingerprint := hex.EncodeToString(h[:])
	var mu sync.Mutex
	revoked := false
	v, _ := NewPeerVerifier(func(context.Context, string) (AgentIdentity, error) {
		mu.Lock()
		defer mu.Unlock()
		return AgentIdentity{"node-a", fingerprint, "2", true, revoked, 1}, nil
	})
	serverConfig, e := v.ControllerTLSConfig(serverCert, roots)
	if e != nil {
		t.Fatal(e)
	}
	clientConfig, e := AgentTLSConfig(clientCert, roots, "fleet.test")
	if e != nil {
		t.Fatal(e)
	}
	listener, e := tls.Listen("tcp", "127.0.0.1:0", serverConfig)
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	states := make(chan tls.ConnectionState, 1)
	errs := make(chan error, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			errs <- e
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		c := conn.(*tls.Conn)
		e = c.Handshake()
		if e == nil {
			states <- c.ConnectionState()
		}
		errs <- e
	}()
	client, e := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", listener.Addr().String(), clientConfig)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	if e = <-errs; e != nil {
		t.Fatal(e)
	}
	state := <-states
	if _, e = v.Verify(context.Background(), state, "node-a"); e != nil {
		t.Fatal(e)
	}
	if _, e = v.Verify(context.Background(), state, "node-b"); e != ErrPeerIdentity {
		t.Fatal("message identity substituted", e)
	}
	mu.Lock()
	revoked = true
	mu.Unlock()
	if _, e = v.Verify(context.Background(), state, "node-a"); e != ErrPeerIdentity {
		t.Fatal("live connection retained revoked identity", e)
	}
}

func TestPeerWrongCANameAndMissingClientCertificate(t *testing.T) {
	ca, key, roots := testPeerCA(t)
	serverCert := testPeerCert(t, ca, key, true)
	clientCert := testPeerCert(t, ca, key, false)
	h := sha256.Sum256(clientCert.Certificate[0])
	fingerprint := hex.EncodeToString(h[:])
	v, _ := NewPeerVerifier(func(context.Context, string) (AgentIdentity, error) {
		return AgentIdentity{"node-a", fingerprint, "2", true, false, 1}, nil
	})
	_, _, wrongRoots := testPeerCA(t)
	for _, scenario := range []string{"ca", "name", "missing", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			serverConfig, _ := v.ControllerTLSConfig(serverCert, roots)
			clientConfig, _ := AgentTLSConfig(clientCert, roots, "fleet.test")
			switch scenario {
			case "ca":
				clientConfig.RootCAs = wrongRoots
			case "name":
				clientConfig.ServerName = "wrong.test"
			case "missing":
				clientConfig.Certificates = nil
			case "revoked":
				serverConfig.VerifyConnection = func(tls.ConnectionState) error { return ErrPeerIdentity }
			}
			listener, e := tls.Listen("tcp", "127.0.0.1:0", serverConfig)
			if e != nil {
				t.Fatal(e)
			}
			defer listener.Close()
			errs := make(chan error, 1)
			go func() {
				conn, e := listener.Accept()
				if e == nil {
					defer conn.Close()
					_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
					e = conn.(*tls.Conn).Handshake()
				}
				errs <- e
			}()
			client, e := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", listener.Addr().String(), clientConfig)
			if client != nil {
				_ = client.Close()
			}
			serverError := <-errs
			if e == nil && serverError == nil {
				t.Fatal("invalid peer accepted")
			}
		})
	}
}
