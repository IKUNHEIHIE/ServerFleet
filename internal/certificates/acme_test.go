package certificates_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/certificates"
)

type testKeySigner struct {
	mu   sync.Mutex
	keys map[certificates.KeyRef]*ecdsa.PrivateKey
	err  error
}

func newTestKeySigner(t *testing.T, refs ...certificates.KeyRef) *testKeySigner {
	t.Helper()
	signer := &testKeySigner{keys: map[certificates.KeyRef]*ecdsa.PrivateKey{}}
	for _, ref := range refs {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		signer.keys[ref] = key
	}
	return signer
}

func (s *testKeySigner) PublicKey(_ context.Context, ref certificates.KeyRef) (crypto.PublicKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	key, ok := s.keys[ref]
	if !ok {
		return nil, errors.New("unknown key")
	}
	return &key.PublicKey, nil
}

func (s *testKeySigner) SignDigest(_ context.Context, ref certificates.KeyRef, digest []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	key, ok := s.keys[ref]
	if !ok {
		return nil, errors.New("unknown key")
	}
	r, sig, err := ecdsa.Sign(rand.Reader, key, digest)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 64)
	copy(out[32-len(r.Bytes()):32], r.Bytes())
	copy(out[64-len(sig.Bytes()):], sig.Bytes())
	return out, nil
}

func (s *testKeySigner) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

type acmePublisher struct {
	mu       sync.Mutex
	fake     *fakeACMEServer
	fail     bool
	cleanErr error
	records  map[string]string
	cleaned  int
}

func newACMEPublisher(fake *fakeACMEServer) *acmePublisher {
	return &acmePublisher{fake: fake, records: map[string]string{}}
}

func (p *acmePublisher) Publish(_ context.Context, fqdn, value string) (func(context.Context) error, error) {
	if p.fail {
		return nil, errors.New("dns provider offline")
	}
	p.mu.Lock()
	p.records[fqdn] = value
	p.fake.setPublished(fqdn, value)
	p.mu.Unlock()
	return func(context.Context) error {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.cleaned++
		delete(p.records, fqdn)
		return p.cleanErr
	}, nil
}

func (p *acmePublisher) record(fqdn string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	value, ok := p.records[fqdn]
	return value, ok
}

type fakeACMEServer struct {
	t      *testing.T
	server *httptest.Server
	caCert *x509.Certificate
	caKey  *ecdsa.PrivateKey
	caPEM  []byte

	mu                 sync.Mutex
	nonces             map[string]bool
	accountPublic      *ecdsa.PublicKey
	accountURL         string
	token              string
	orderStatus        string
	challengeStatus    string
	published          map[string]string
	badNonceOnce       bool
	nonceRejections    int
	rejectChallenge    bool
	failFinalize       bool
	finalizeProcessing bool
	certificatePEM     []byte
	certificateChain   []byte
	serial             *big.Int
	notBefore          time.Time
	notAfter           time.Time
}

func newFakeACMEServer(t *testing.T) *fakeACMEServer {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(100),
		Subject:               pkix.Name{CommonName: "Fake ACME CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeACMEServer{
		t: t, caCert: caCert, caKey: caKey,
		caPEM:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		nonces: map[string]bool{}, published: map[string]string{},
		orderStatus: "pending", challengeStatus: "pending",
	}
	token := make([]byte, 16)
	_, _ = rand.Read(token)
	fake.token = base64.RawURLEncoding.EncodeToString(token)
	mux := http.NewServeMux()
	mux.HandleFunc("/directory", fake.directory)
	mux.HandleFunc("/new-nonce", fake.newNonce)
	mux.HandleFunc("/new-account", fake.newAccount)
	mux.HandleFunc("/new-order", fake.newOrder)
	mux.HandleFunc("/authz", fake.authorization)
	mux.HandleFunc("/challenge", fake.challenge)
	mux.HandleFunc("/finalize", fake.finalize)
	mux.HandleFunc("/order", fake.order)
	mux.HandleFunc("/certificate", fake.certificate)
	fake.server = httptest.NewTLSServer(mux)
	t.Cleanup(fake.server.Close)
	fake.accountURL = fake.server.URL + "/account/1"
	return fake
}

func (f *fakeACMEServer) setPublished(fqdn, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published[fqdn] = value
}

func (f *fakeACMEServer) directory(w http.ResponseWriter, _ *http.Request) {
	writeACMEMap(w, map[string]string{
		"newNonce": f.server.URL + "/new-nonce", "newAccount": f.server.URL + "/new-account",
		"newOrder": f.server.URL + "/new-order",
	})
}

func (f *fakeACMEServer) newNonce(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	nonce := f.newNonceLocked()
	f.mu.Unlock()
	w.Header().Set("Replay-Nonce", nonce)
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeACMEServer) newNonceLocked() string {
	buffer := make([]byte, 16)
	_, _ = rand.Read(buffer)
	nonce := base64.RawURLEncoding.EncodeToString(buffer)
	f.nonces[nonce] = true
	return nonce
}

func (f *fakeACMEServer) newAccount(w http.ResponseWriter, r *http.Request) {
	protected, _, public, ok := f.verifyJWS(w, r, true)
	if !ok {
		return
	}
	_ = protected
	f.mu.Lock()
	f.accountPublic = public
	f.mu.Unlock()
	w.Header().Set("Location", f.accountURL)
	writeACMEMap(w, map[string]string{"status": "valid"})
}

func (f *fakeACMEServer) newOrder(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := f.verifyJWS(w, r, false); !ok {
		return
	}
	w.Header().Set("Location", f.server.URL+"/order")
	writeACMEMap(w, map[string]any{
		"status": "pending", "authorizations": []string{f.server.URL + "/authz"},
		"finalize": f.server.URL + "/finalize",
	})
}

func (f *fakeACMEServer) authorization(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := f.verifyJWS(w, r, false); !ok {
		return
	}
	f.mu.Lock()
	status := f.challengeStatus
	f.mu.Unlock()
	writeACMEMap(w, map[string]any{
		"status": status, "identifier": map[string]string{"type": "dns", "value": "app.example.com"},
		"challenges": []map[string]string{{
			"type": "dns-01", "url": f.server.URL + "/challenge", "token": f.token,
		}},
	})
}

func (f *fakeACMEServer) challenge(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := f.verifyJWS(w, r, false); !ok {
		return
	}
	f.mu.Lock()
	expected := f.expectedTXT()
	actual := f.published["_acme-challenge.app.example.com"]
	f.challengeStatus = "valid"
	if f.rejectChallenge || actual != expected {
		f.challengeStatus = "invalid"
	}
	f.mu.Unlock()
	writeACMEMap(w, map[string]string{"status": "processing"})
}

func (f *fakeACMEServer) expectedTXT() string {
	public := f.accountPublic
	jwk := canonicalJWK(public)
	thumb := sha256.Sum256(jwk)
	keyAuth := f.token + "." + base64.RawURLEncoding.EncodeToString(thumb[:])
	sum := sha256.Sum256([]byte(keyAuth))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (f *fakeACMEServer) finalize(w http.ResponseWriter, r *http.Request) {
	_, payload, _, ok := f.verifyJWS(w, r, false)
	if !ok {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failFinalize {
		w.Header().Set("Replay-Nonce", f.newNonceLocked())
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"type":"urn:ietf:params:acme:error:badCSR","status":400}`)
		return
	}
	var body struct {
		CSR string `json:"csr"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		w.WriteHeader(400)
		return
	}
	csr, err := base64.RawURLEncoding.DecodeString(body.CSR)
	if err != nil {
		w.WriteHeader(400)
		return
	}
	if err := f.issueCertificate(csr); err != nil {
		w.WriteHeader(500)
		return
	}
	if f.finalizeProcessing {
		f.orderStatus = "processing"
		writeACMEMap(w, map[string]any{"status": "processing", "finalize": f.server.URL + "/finalize"})
		return
	}
	f.orderStatus = "valid"
	writeACMEMap(w, map[string]any{
		"status": "valid", "finalize": f.server.URL + "/finalize",
		"certificate": f.server.URL + "/certificate",
	})
}

func (f *fakeACMEServer) issueCertificate(csrDER []byte) error {
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return err
	}
	serial := big.NewInt(0).SetBytes([]byte{0x01, 0x23, 0x45, 0x67})
	notBefore := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	notAfter := notBefore.Add(90 * 24 * time.Hour)
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: csr.Subject.CommonName},
		DNSNames:     append([]string(nil), csr.DNSNames...),
		NotBefore:    notBefore, NotAfter: notAfter,
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, f.caCert, csr.PublicKey, f.caKey)
	if err != nil {
		return err
	}
	f.serial = serial
	f.notBefore = notBefore
	f.notAfter = notAfter
	f.certificatePEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	f.certificateChain = append(append([]byte(nil), f.certificatePEM...), f.caPEM...)
	return nil
}

func (f *fakeACMEServer) order(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := f.verifyJWS(w, r, false); !ok {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	body := map[string]any{"status": f.orderStatus, "finalize": f.server.URL + "/finalize"}
	if f.orderStatus == "valid" {
		body["certificate"] = f.server.URL + "/certificate"
	}
	writeACMEMap(w, body)
}

func (f *fakeACMEServer) certificate(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := f.verifyJWS(w, r, false); !ok {
		return
	}
	f.mu.Lock()
	chain := append([]byte(nil), f.certificateChain...)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/pem-certificate-chain")
	_, _ = w.Write(chain)
}

func (f *fakeACMEServer) verifyJWS(w http.ResponseWriter, r *http.Request, allowJWK bool) (map[string]any, []byte, *ecdsa.PublicKey, bool) {
	f.mu.Lock()
	if f.badNonceOnce {
		f.badNonceOnce = false
		f.nonceRejections++
		nonce := f.newNonceLocked()
		f.mu.Unlock()
		w.Header().Set("Replay-Nonce", nonce)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"type":"urn:ietf:params:acme:error:badNonce","status":400}`)
		return nil, nil, nil, false
	}
	f.mu.Unlock()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(400)
		return nil, nil, nil, false
	}
	var jws struct {
		Protected string `json:"protected"`
		Payload   string `json:"payload"`
		Signature string `json:"signature"`
	}
	if err := json.Unmarshal(body, &jws); err != nil {
		w.WriteHeader(400)
		return nil, nil, nil, false
	}
	protectedJSON, err1 := base64.RawURLEncoding.DecodeString(jws.Protected)
	signature, err2 := base64.RawURLEncoding.DecodeString(jws.Signature)
	payload, err3 := base64.RawURLEncoding.DecodeString(jws.Payload)
	if err1 != nil || err2 != nil || err3 != nil {
		w.WriteHeader(400)
		return nil, nil, nil, false
	}
	var protected struct {
		Alg   string          `json:"alg"`
		Nonce string          `json:"nonce"`
		URL   string          `json:"url"`
		KID   string          `json:"kid"`
		JWK   json.RawMessage `json:"jwk"`
	}
	if err := json.Unmarshal(protectedJSON, &protected); err != nil || protected.Alg != "ES256" {
		w.WriteHeader(400)
		return nil, nil, nil, false
	}
	expectedURL := "https://" + r.Host + r.URL.RequestURI()
	if protected.URL != expectedURL {
		f.t.Errorf("jws url = %q, want %q", protected.URL, expectedURL)
		w.WriteHeader(400)
		return nil, nil, nil, false
	}
	f.mu.Lock()
	validNonce := f.nonces[protected.Nonce]
	delete(f.nonces, protected.Nonce)
	f.mu.Unlock()
	if !validNonce {
		w.WriteHeader(400)
		return nil, nil, nil, false
	}
	var public *ecdsa.PublicKey
	if len(protected.JWK) > 0 {
		if !allowJWK {
			w.WriteHeader(400)
			return nil, nil, nil, false
		}
		public, err = parseJWK(protected.JWK)
		if err != nil {
			w.WriteHeader(400)
			return nil, nil, nil, false
		}
	} else {
		if protected.KID != f.accountURL {
			w.WriteHeader(400)
			return nil, nil, nil, false
		}
		f.mu.Lock()
		public = f.accountPublic
		f.mu.Unlock()
	}
	if public == nil || len(signature) != 64 {
		w.WriteHeader(400)
		return nil, nil, nil, false
	}
	digest := sha256.Sum256([]byte(jws.Protected + "." + jws.Payload))
	rInt := new(big.Int).SetBytes(signature[:32])
	sInt := new(big.Int).SetBytes(signature[32:])
	if !ecdsa.Verify(public, digest[:], rInt, sInt) {
		f.t.Errorf("invalid jws signature for %s", r.URL.Path)
		w.WriteHeader(400)
		return nil, nil, nil, false
	}
	var decodedProtected map[string]any
	_ = json.Unmarshal(protectedJSON, &decodedProtected)
	f.mu.Lock()
	nonce := f.newNonceLocked()
	f.mu.Unlock()
	w.Header().Set("Replay-Nonce", nonce)
	return decodedProtected, payload, public, true
}

func parseJWK(raw json.RawMessage) (*ecdsa.PublicKey, error) {
	var jwk struct {
		Crv string `json:"crv"`
		Kty string `json:"kty"`
		X   string `json:"x"`
		Y   string `json:"y"`
	}
	if err := json.Unmarshal(raw, &jwk); err != nil || jwk.Crv != "P-256" || jwk.Kty != "EC" {
		return nil, errors.New("unsupported jwk")
	}
	x, err := base64.RawURLEncoding.DecodeString(jwk.X)
	if err != nil {
		return nil, err
	}
	y, err := base64.RawURLEncoding.DecodeString(jwk.Y)
	if err != nil {
		return nil, err
	}
	point := make([]byte, 0, 65)
	point = append(point, 4)
	point = append(point, x...)
	point = append(point, y...)
	return ecdsa.ParseUncompressedPublicKey(elliptic.P256(), point)
}

func canonicalJWK(public *ecdsa.PublicKey) []byte {
	raw, err := public.Bytes()
	if err != nil || len(raw) != 65 {
		return nil
	}
	encoded, _ := json.Marshal(struct {
		Crv string `json:"crv"`
		Kty string `json:"kty"`
		X   string `json:"x"`
		Y   string `json:"y"`
	}{Crv: "P-256", Kty: "EC", X: base64.RawURLEncoding.EncodeToString(raw[1:33]), Y: base64.RawURLEncoding.EncodeToString(raw[33:])})
	return encoded
}

func writeACMEMap(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func newACMEClient(t *testing.T, fake *fakeACMEServer, signer *testKeySigner, publisher *acmePublisher) *certificates.ACMEClient {
	t.Helper()
	client, err := certificates.NewACMEClient(certificates.ACMEConfig{
		DirectoryURL: fake.server.URL + "/directory", Client: fake.server.Client(),
		AccountKey: "account-key", Signer: signer, Challenge: publisher,
		PollInterval: 10 * time.Millisecond, Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestACMEDNS01IssuanceWithVerifiedJWS(t *testing.T) {
	fake := newFakeACMEServer(t)
	signer := newTestKeySigner(t, "account-key", "cert-key")
	publisher := newACMEPublisher(fake)
	client := newACMEClient(t, fake, signer, publisher)
	issued, err := client.IssueDNS01(context.Background(), certificates.ACMERequest{
		CommonName: "app.example.com", KeyRef: "cert-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(issued.CertificatePEM) == 0 || len(issued.ChainPEM) <= len(issued.CertificatePEM) {
		t.Fatalf("issued = %+v", issued)
	}
	if issued.Serial != fake.serial.String() || !issued.NotAfter.Equal(fake.notAfter) {
		t.Fatalf("issued metadata = %+v", issued)
	}
	leaf, err := parseLeaf(issued.CertificatePEM)
	if err != nil {
		t.Fatal(err)
	}
	if leaf.Subject.CommonName != "app.example.com" || len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "app.example.com" {
		t.Fatalf("leaf = %+v", leaf)
	}
	fake.mu.Lock()
	published := fake.published["_acme-challenge.app.example.com"]
	fake.mu.Unlock()
	if published == "" {
		t.Fatal("dns-01 value was never published")
	}
	if publisher.cleaned != 1 {
		t.Fatalf("cleanup count = %d", publisher.cleaned)
	}
	if fake.challengeStatus != "valid" {
		t.Fatalf("challenge status = %s", fake.challengeStatus)
	}
}

func TestACMEDNS01RetriesBadNonce(t *testing.T) {
	fake := newFakeACMEServer(t)
	fake.badNonceOnce = true
	signer := newTestKeySigner(t, "account-key", "cert-key")
	client := newACMEClient(t, fake, signer, newACMEPublisher(fake))
	if _, err := client.IssueDNS01(context.Background(), certificates.ACMERequest{CommonName: "app.example.com", KeyRef: "cert-key"}); err != nil {
		t.Fatal(err)
	}
	if fake.nonceRejections != 1 {
		t.Fatalf("nonce rejections = %d", fake.nonceRejections)
	}
}

func TestACMEDNS01WrongValueFails(t *testing.T) {
	fake := newFakeACMEServer(t)
	fake.rejectChallenge = true
	signer := newTestKeySigner(t, "account-key", "cert-key")
	publisher := newACMEPublisher(fake)
	client := newACMEClient(t, fake, signer, publisher)
	_, err := client.IssueDNS01(context.Background(), certificates.ACMERequest{CommonName: "app.example.com", KeyRef: "cert-key"})
	if !errors.Is(err, certificates.ErrACMEChallenge) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := publisher.record("_acme-challenge.app.example.com"); ok {
		t.Fatal("challenge record was not cleaned up")
	}
}

func TestACMEDNS01FinalizeFailure(t *testing.T) {
	fake := newFakeACMEServer(t)
	fake.failFinalize = true
	signer := newTestKeySigner(t, "account-key", "cert-key")
	client := newACMEClient(t, fake, signer, newACMEPublisher(fake))
	_, err := client.IssueDNS01(context.Background(), certificates.ACMERequest{CommonName: "app.example.com", KeyRef: "cert-key"})
	if !errors.Is(err, certificates.ErrACMEProtocol) && !errors.Is(err, certificates.ErrACMEIssuance) {
		t.Fatalf("err = %v", err)
	}
}

func TestACMEDNS01PollsProcessingOrder(t *testing.T) {
	fake := newFakeACMEServer(t)
	fake.finalizeProcessing = true
	signer := newTestKeySigner(t, "account-key", "cert-key")
	client := newACMEClient(t, fake, signer, newACMEPublisher(fake))
	// Let the fake transition processing -> valid on the first order poll.
	go func() {
		time.Sleep(50 * time.Millisecond)
		fake.mu.Lock()
		fake.orderStatus = "valid"
		fake.mu.Unlock()
	}()
	if _, err := client.IssueDNS01(context.Background(), certificates.ACMERequest{CommonName: "app.example.com", KeyRef: "cert-key"}); err != nil {
		t.Fatal(err)
	}
}

func TestACMEDNS01RejectsConfigAndSignerFailures(t *testing.T) {
	fake := newFakeACMEServer(t)
	signer := newTestKeySigner(t, "account-key", "cert-key")
	publisher := newACMEPublisher(fake)
	for _, config := range []certificates.ACMEConfig{
		{AccountKey: "account-key", Signer: signer},
		{DirectoryURL: "http://insecure.example/directory", AccountKey: "account-key", Signer: signer, Challenge: publisher},
		{DirectoryURL: fake.server.URL + "/directory", Signer: signer, Challenge: publisher},
	} {
		if _, err := certificates.NewACMEClient(config); !errors.Is(err, certificates.ErrACMEConfig) {
			t.Fatalf("config %+v err = %v", config, err)
		}
	}
	signer.setError(errors.New("secret store offline"))
	client := newACMEClient(t, fake, signer, publisher)
	if _, err := client.IssueDNS01(context.Background(), certificates.ACMERequest{CommonName: "app.example.com", KeyRef: "cert-key"}); !errors.Is(err, certificates.ErrACMESigner) {
		t.Fatalf("signer err = %v", err)
	}
}

func TestACMEDNS01RedirectRefused(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
	}))
	defer target.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()
	signer := newTestKeySigner(t)
	client, err := certificates.NewACMEClient(certificates.ACMEConfig{
		DirectoryURL: source.URL + "/directory", Client: source.Client(),
		AccountKey: "account-key", Signer: signer,
		Challenge: &acmePublisher{fake: &fakeACMEServer{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.IssueDNS01(context.Background(), certificates.ACMERequest{CommonName: "app.example.com", KeyRef: "cert-key"}); !errors.Is(err, certificates.ErrACMERedirect) {
		t.Fatalf("err = %v", err)
	}
	if redirected.Load() != 0 {
		t.Fatalf("redirect target requests = %d", redirected.Load())
	}
}

func parseLeaf(payload []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(payload)
	if block == nil {
		return nil, errors.New("no certificate")
	}
	return x509.ParseCertificate(block.Bytes)
}
