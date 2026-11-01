package platform_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/gateway"
)

// This tests the rendered configuration against the installed executable in a
// private ordinary-user prefix. It does not test the not-yet-wired task executor.
func TestWave6IsolatedNginxProxy(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux isolated nginx")
	}
	if os.Geteuid() == 0 {
		t.Skip("ordinary user required")
	}
	nginx, err := exec.LookPath("nginx")
	if err != nil {
		nginx = "/usr/sbin/nginx"
		if _, err := os.Stat(nginx); err != nil {
			t.Skip("nginx not installed")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dir := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "wave6-nginx-isolated-ok") }))
	defer upstream.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	site, err := gateway.RenderNginxSite(gateway.NginxSiteRequest{
		GatewayID: "gw-wave6", Domain: "qa.example.invalid", Upstream: upstream.Listener.Addr().String(),
		HTTPListen: address, SiteDir: filepath.Join(dir, "sites"), ArtifactDir: filepath.Join(dir, "artifacts"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Dir(site.Path), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(site.Path, site.Config, 0600); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("worker_processes 1;\npid %s/nginx.pid;\nerror_log %s/error.log;\nevents { worker_connections 32; }\nhttp { access_log %s/access.log; client_body_temp_path %s/body; proxy_temp_path %s/proxy; include %s; }\n", dir, dir, dir, dir, dir, site.Path)
	configPath := filepath.Join(dir, "nginx.conf")
	if err = os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if raw, err := exec.CommandContext(ctx, nginx, "-t", "-p", dir+"/", "-c", configPath).CombinedOutput(); err != nil {
		t.Fatalf("isolated nginx -t: %v: %s", err, raw)
	}
	log, err := os.Create(filepath.Join(dir, "process.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd := exec.CommandContext(ctx, nginx, "-p", dir+"/", "-c", configPath, "-g", "daemon off;")
	cmd.Stdout, cmd.Stderr = log, log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(4 * time.Second):
			cmd.Process.Kill()
			<-done
		}
	}()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r, _ := http.NewRequestWithContext(ctx, "GET", "http://"+address+"/", nil)
		r.Host = "qa.example.invalid"
		response, err := client.Do(r)
		if err == nil {
			raw, e := io.ReadAll(response.Body)
			response.Body.Close()
			if e != nil || response.StatusCode != 200 || string(raw) != "wave6-nginx-isolated-ok" {
				t.Fatalf("isolated proxy = %d: %q (%v)", response.StatusCode, raw, e)
			}
			break
		}
		if time.Now().After(deadline) {
			raw, _ := os.ReadFile(filepath.Join(dir, "error.log"))
			t.Fatalf("isolated nginx readiness: %v: %s", err, raw)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
