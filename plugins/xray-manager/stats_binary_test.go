package xraymanager

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Explicit native Windows evidence. The fixture adds one precise local redirect
// and egress allowance ONLY to reach its own loopback HTTP target, keeps API
// guards, and uses a fresh test CA verified by the real VLESS TLS+Vision client.
func TestPinnedXrayActualVisionTrafficStatsAndProcessReset(t *testing.T) {
	binary := pinnedLocalXray(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	var received atomic.Int64
	responseBody := bytes.Repeat([]byte("local-response"), 2048)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, io.LimitReader(r.Body, 128<<10))
		if err != nil {
			http.Error(w, "read failed", 400)
			return
		}
		received.Add(n)
		w.Header().Set("Connection", "close")
		_, _ = w.Write(responseBody)
	}))
	defer target.Close()
	_, targetPort, _ := net.SplitHostPort(strings.TrimPrefix(target.URL, "http://"))
	used := map[int]bool{}
	port := func() int {
		for {
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			n := l.Addr().(*net.TCPAddr).Port
			_ = l.Close()
			if !used[n] && strconv.Itoa(n) != targetPort {
				used[n] = true
				return n
			}
		}
	}
	serverPort, apiPort, socksPort := port(), port(), port()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(10), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(2 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(11), DNSNames: []string{"fleet.test"}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(private)
	caFile, certFile, keyFile := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "server.pem"), filepath.Join(dir, "server-key.pem")
	for file, raw := range map[string][]byte{caFile: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), certFile: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), keyFile: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})} {
		if err := os.WriteFile(file, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	spec, facts, resolve := templateFixture(t)
	spec.Port, spec.APIPort = uint16(serverPort), uint16(apiPort)
	facts.CertificateDER = leafDER
	var serverConfig []byte
	if err = WithTemplate(ctx, spec, facts, allowTemplate, resolve, func(_ context.Context, raw []byte) error { serverConfig = bytes.Clone(raw); return nil }); err != nil {
		t.Fatal(err)
	}
	defer clear(serverConfig)
	var config map[string]any
	if json.Unmarshal(serverConfig, &config) != nil {
		t.Fatal("invalid rendered server")
	}
	tls := config["inbounds"].([]any)[0].(map[string]any)["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)
	tls["certificates"] = []any{map[string]any{"certificateFile": certFile, "keyFile": keyFile}}
	// The fixture redirects only to its own target, never the management port.
	config["outbounds"].([]any)[0].(map[string]any)["settings"] = map[string]any{"redirect": strings.TrimPrefix(target.URL, "http://"), "finalRules": []any{map[string]any{"action": "allow", "ip": []string{"127.0.0.1"}, "port": targetPort}}}
	serverConfig, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var uuid string
	if err = resolve(ctx, spec.InstanceID, "xray-user", spec.Users[0].IdentityRef, func(secret []byte) error { uuid = string(secret); return nil }); err != nil {
		t.Fatal(err)
	}
	clientConfig := map[string]any{"log": map[string]any{"loglevel": "none"}, "inbounds": []any{map[string]any{"tag": "local-socks", "listen": "127.0.0.1", "port": socksPort, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}}, "outbounds": []any{map[string]any{"tag": "vision-client", "protocol": "vless", "settings": map[string]any{"address": "127.0.0.1", "port": serverPort, "id": uuid, "flow": "xtls-rprx-vision", "encryption": "none"}, "streamSettings": map[string]any{"network": "raw", "security": "tls", "tlsSettings": map[string]any{"serverName": "fleet.test", "minVersion": "1.3", "disableSystemRoot": true, "certificates": []any{map[string]any{"usage": "verify", "certificateFile": caFile}}}}}}}
	clientRaw, err := json.Marshal(clientConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(clientRaw)
	writeConfig := func(name string, raw []byte) string {
		file := filepath.Join(dir, name)
		if err := os.WriteFile(file, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if err := exec.CommandContext(ctx, binary, "run", "-test", "-config", file).Run(); err != nil {
			t.Fatal("pinned target rejected local Stats fixture", name, err)
		}
		return file
	}
	serverFile, clientFile := writeConfig("server.json", serverConfig), writeConfig("client.json", clientRaw)
	type child struct {
		command *exec.Cmd
		exited  chan error
		stopped bool
	}
	start := func(file string, listenPort int) *child {
		command := exec.CommandContext(ctx, binary, "run", "-config", file)
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		c := &child{command: command, exited: make(chan error, 1)}
		go func() { c.exited <- command.Wait() }()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-c.exited:
				t.Fatal("real Xray exited before health")
			default:
			}
			conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(listenPort)), 50*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				return c
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("real Xray loopback health timed out")
		return c
	}
	stop := func(c *child) {
		if c == nil || c.stopped {
			return
		}
		c.stopped = true
		_ = c.command.Process.Kill()
		select {
		case <-c.exited:
		case <-ctx.Done():
		}
	}
	server := start(serverFile, serverPort)
	defer func() { stop(server) }()
	client := start(clientFile, socksPort)
	defer stop(client)
	dial := func(ctx context.Context, _, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPort)))
		if err != nil {
			return nil, err
		}
		ok := false
		defer func() {
			if !ok {
				_ = conn.Close()
			}
		}()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err = conn.Write([]byte{5, 1, 0}); err != nil {
			return nil, err
		}
		var hello [2]byte
		if _, err = io.ReadFull(conn, hello[:]); err != nil || hello != [2]byte{5, 0} {
			return nil, fmt.Errorf("SOCKS greeting failed")
		}
		host, p, err := net.SplitHostPort(address)
		if err != nil || len(host) > 255 {
			return nil, fmt.Errorf("bad local request endpoint")
		}
		number, err := strconv.Atoi(p)
		if err != nil {
			return nil, err
		}
		request := append([]byte{5, 1, 0, 3, byte(len(host))}, []byte(host)...)
		request = append(request, byte(number>>8), byte(number))
		if _, err = conn.Write(request); err != nil {
			return nil, err
		}
		var response [4]byte
		if _, err = io.ReadFull(conn, response[:]); err != nil || response[0] != 5 || response[1] != 0 {
			return nil, fmt.Errorf("SOCKS request refused")
		}
		skip := 0
		switch response[3] {
		case 1:
			skip = 6
		case 4:
			skip = 18
		case 3:
			var n [1]byte
			if _, err = io.ReadFull(conn, n[:]); err != nil {
				return nil, err
			}
			skip = int(n[0]) + 2
		default:
			return nil, fmt.Errorf("SOCKS address rejected")
		}
		if _, err = io.CopyN(io.Discard, conn, int64(skip)); err != nil {
			return nil, err
		}
		ok = true
		return conn, nil
	}
	transport := &http.Transport{DialContext: dial, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: 8 * time.Second}
	transfer := func(body []byte) {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://traffic.fixture.invalid/probe", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response, err := httpClient.Do(request)
		if err != nil {
			t.Fatal("actual VLESS TLS/Vision request failed", err)
		}
		defer response.Body.Close()
		content, err := io.ReadAll(io.LimitReader(response.Body, 128<<10))
		if err != nil || response.StatusCode != 200 || !bytes.Equal(content, responseBody) {
			t.Fatal("actual proxy target response mismatch", response.StatusCode, err)
		}
	}
	bindings := []StatsBinding{{CounterKey{spec.InstanceID, "user", spec.Users[0].ID}, []string{"user>>>sf-user-" + spec.Users[0].ID + ">>>traffic"}}, {CounterKey{spec.InstanceID, "inbound", "inbound-one"}, []string{"inbound>>>sf-vless-" + spec.InstanceID + ">>>traffic"}}, {CounterKey{spec.InstanceID, "instance", spec.InstanceID}, []string{"inbound>>>sf-vless-" + spec.InstanceID + ">>>traffic"}}}
	segment := func(c *child) string {
		return fmt.Sprintf("process-%d-%d", c.command.Process.Pid, time.Now().UnixNano())
	}
	process := segment(server)
	query := func() []TrafficSample {
		raw, err := exec.CommandContext(ctx, binary, "api", "statsquery", "-s", net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort)), "-t", "2", "-pattern", "").Output()
		if err != nil {
			t.Fatal("actual non-resetting StatsService query failed", err)
		}
		samples, err := DecodeStatsJSON(raw, StatsObservation{spec.InstanceID, process, time.Now()}, bindings)
		if err != nil {
			t.Fatal("actual StatsService JSON rejected", err)
		}
		return samples
	}
	transfer([]byte("warmup"))
	before := query()
	states := make([]CounterState, len(bindings))
	for i, sample := range before {
		if !sample.Available {
			t.Fatal("actual configured counter unavailable", i)
		}
		states[i], _, err = Accumulate(CounterState{}, sample)
		if err != nil {
			t.Fatal(err)
		}
	}
	payload := bytes.Repeat([]byte("upload-body"), 4096)
	transfer(payload)
	after := query()
	for i, sample := range after {
		next, delta, err := Accumulate(states[i], sample)
		if err != nil || !delta.Known || delta.Uplink == 0 || delta.Downlink == 0 {
			t.Fatal("actual proxy traffic not reflected", i, delta, err)
		}
		states[i] = next
		t.Logf("actual TLS/Vision Stats delta %s: uplink=%d downlink=%d", sample.Key.Dimension, delta.Uplink, delta.Downlink)
	}
	if after[0].Uplink-before[0].Uplink < uint64(len(payload)) || after[2].Uplink != after[1].Uplink || after[2].Downlink != after[1].Downlink || received.Load() != int64(len(payload)+len("warmup")) {
		t.Fatal("actual counter direction/instance aggregation mismatch")
	}
	again := query()
	if again[0].Uplink < after[0].Uplink || again[0].Downlink < after[0].Downlink {
		t.Fatal("query unexpectedly reset real user counters")
	}
	stop(server)
	if err := exec.CommandContext(ctx, binary, "api", "statsquery", "-s", net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort)), "-t", "1").Run(); err == nil {
		t.Fatal("stopped API falsely reported successful sampling")
	}
	server = start(serverFile, serverPort)
	process = segment(server)
	transfer([]byte("after restart"))
	restarted := query()
	for i, sample := range restarted {
		old := states[i]
		next, delta, err := Accumulate(old, sample)
		if err != nil || !delta.Reset || delta.Known || next.TotalUplink != old.TotalUplink || next.TotalDownlink != old.TotalDownlink {
			t.Fatal("real process reset lost history or invented interval", i, delta, err)
		}
		t.Logf("actual process reset %s: prior totals preserved (%d/%d)", sample.Key.Dimension, next.TotalUplink, next.TotalDownlink)
	}
}
