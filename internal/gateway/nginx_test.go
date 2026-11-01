package gateway_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"serverfleet.local/serverfleet/internal/gateway"
)

func directHTTPRequest() gateway.NginxSiteRequest {
	return gateway.NginxSiteRequest{
		GatewayID: "gw-1", Domain: "App.Example.com", Upstream: "10.0.0.5:8080",
	}
}

func TestRenderNginxSiteHTTPUpstream(t *testing.T) {
	site, err := gateway.RenderNginxSite(directHTTPRequest())
	if err != nil {
		t.Fatal(err)
	}
	config := string(site.Config)
	for _, expected := range []string{
		"# Managed by ServerFleet; do not edit.",
		"server_name app.example.com;",
		"proxy_pass http://10.0.0.5:8080;",
		"listen 80;",
		"location /.well-known/acme-challenge/",
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("config missing %q:\n%s", expected, config)
		}
	}
	if strings.Contains(config, "ssl_certificate") || strings.Contains(config, "listen 443") {
		t.Fatalf("HTTP site contains TLS directives:\n%s", config)
	}
	if site.Path != "/etc/nginx/conf.d/serverfleet-gw-1-app.example.com.conf" {
		t.Fatalf("path = %q", site.Path)
	}
	sum := sha256.Sum256(site.Config)
	if site.Digest != hex.EncodeToString(sum[:]) {
		t.Fatalf("digest = %s", site.Digest)
	}
	if !strings.HasSuffix(site.ArtifactPath, site.Digest+".conf") {
		t.Fatalf("artifact path = %q", site.ArtifactPath)
	}
	again, err := gateway.RenderNginxSite(directHTTPRequest())
	if err != nil || !bytes.Equal(again.Config, site.Config) || again.Digest != site.Digest {
		t.Fatalf("rendering is not deterministic: %v", err)
	}
}

func TestRenderNginxSiteHTTPSGroup(t *testing.T) {
	site, err := gateway.RenderNginxSite(gateway.NginxSiteRequest{
		GatewayID: "gw-1", Domain: "shop.example.com", ServiceGroupID: "sg-1", ServiceGroupRevision: 3,
		UpstreamHosts: []string{"10.0.0.2:9000", "10.0.0.1:9000", "10.0.0.2:9000"},
		HTTPS:         true, CertificatePath: "/etc/serverfleet/certs/shop/fullchain.pem",
		PrivateKeyPath: "/etc/serverfleet/certs/shop/privkey.pem",
	})
	if err != nil {
		t.Fatal(err)
	}
	config := string(site.Config)
	for _, expected := range []string{
		"upstream sf_", "least_conn;", "server 10.0.0.1:9000;", "server 10.0.0.2:9000;",
		"listen 443 ssl;", "ssl_certificate /etc/serverfleet/certs/shop/fullchain.pem;",
		"ssl_certificate_key /etc/serverfleet/certs/shop/privkey.pem;",
		"return 301 https://$host$request_uri;", "Strict-Transport-Security",
		"proxy_pass http://sf_",
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("config missing %q:\n%s", expected, config)
		}
	}
	if strings.Count(config, "server 10.0.0.2:9000;") != 1 {
		t.Fatalf("duplicate upstream host was rendered:\n%s", config)
	}
	if strings.Contains(config, "BEGIN") {
		t.Fatal("private key material appeared in the config")
	}
}

func TestRenderNginxSiteTakeoverGate(t *testing.T) {
	request := directHTTPRequest()
	request.ExistingSite = true
	if _, err := gateway.RenderNginxSite(request); !errors.Is(err, gateway.ErrNginxTakeover) {
		t.Fatalf("no approval err = %v", err)
	}
	request.TakeoverApproved = true
	if _, err := gateway.RenderNginxSite(request); !errors.Is(err, gateway.ErrNginxTakeover) {
		t.Fatalf("no approval ref err = %v", err)
	}
	request.ApprovalRef = "approval-1"
	if _, err := gateway.RenderNginxSite(request); err != nil {
		t.Fatalf("approved takeover err = %v", err)
	}
}

func TestRenderNginxSiteRejectsInjection(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*gateway.NginxSiteRequest)
	}{
		{"domain newline", func(r *gateway.NginxSiteRequest) { r.Domain = "app.example.com\ninclude /etc/passwd" }},
		{"domain semicolon", func(r *gateway.NginxSiteRequest) { r.Domain = "app.example.com;}" }},
		{"upstream injection", func(r *gateway.NginxSiteRequest) { r.Upstream = "10.0.0.5:8080; include /etc/passwd" }},
		{"upstream no port", func(r *gateway.NginxSiteRequest) { r.Upstream = "10.0.0.5" }},
		{"cert relative", func(r *gateway.NginxSiteRequest) {
			r.HTTPS = true
			r.CertificatePath = "certs/fullchain.pem"
			r.PrivateKeyPath = "/etc/serverfleet/privkey.pem"
		}},
		{"key newline", func(r *gateway.NginxSiteRequest) {
			r.HTTPS = true
			r.CertificatePath = "/etc/serverfleet/fullchain.pem"
			r.PrivateKeyPath = "/etc/serverfleet/privkey.pem\ninclude /etc/passwd"
		}},
		{"https missing cert", func(r *gateway.NginxSiteRequest) { r.HTTPS = true }},
		{"http with cert", func(r *gateway.NginxSiteRequest) {
			r.CertificatePath = "/etc/serverfleet/fullchain.pem"
			r.PrivateKeyPath = "/etc/serverfleet/privkey.pem"
		}},
		{"site dir traversal", func(r *gateway.NginxSiteRequest) { r.SiteDir = "/etc/nginx/conf.d/../../etc" }},
		{"group without hosts", func(r *gateway.NginxSiteRequest) {
			r.Upstream = ""
			r.ServiceGroupID = "sg-1"
			r.ServiceGroupRevision = 2
		}},
		{"group without revision", func(r *gateway.NginxSiteRequest) {
			r.Upstream = ""
			r.ServiceGroupID = "sg-1"
			r.UpstreamHosts = []string{"10.0.0.1:80"}
		}},
		{"both targets", func(r *gateway.NginxSiteRequest) {
			r.ServiceGroupID = "sg-1"
			r.ServiceGroupRevision = 1
			r.UpstreamHosts = []string{"10.0.0.1:80"}
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := directHTTPRequest()
			test.mutate(&request)
			if _, err := gateway.RenderNginxSite(request); err == nil {
				t.Fatalf("request accepted: %+v", request)
			}
		})
	}
}

func TestRenderNginxSiteWildcardName(t *testing.T) {
	request := directHTTPRequest()
	request.Domain = "*.example.com"
	site, err := gateway.RenderNginxSite(request)
	if err != nil {
		t.Fatal(err)
	}
	if site.Path != "/etc/nginx/conf.d/serverfleet-gw-1-wildcard.example.com.conf" {
		t.Fatalf("path = %q", site.Path)
	}
	if !strings.Contains(string(site.Config), "server_name *.example.com;") {
		t.Fatalf("config:\n%s", site.Config)
	}
}

func TestBuildNginxTasksUseArtifactReference(t *testing.T) {
	site, err := gateway.RenderNginxSite(directHTTPRequest())
	if err != nil {
		t.Fatal(err)
	}
	input := gateway.NginxTaskInput{
		SubjectID: "admin", RequestID: "req-1", GatewayID: "gw-1", NodeID: "node-1",
		IdempotencyKey: "idem-1", ExistingSite: true, TakeoverApproved: true, ApprovalRef: "approval-1",
	}
	request, err := gateway.BuildNginxApplyTask(site, input)
	if err != nil {
		t.Fatal(err)
	}
	if request.ActionID != gateway.NginxActionApply || request.ResourceID != "gw-1" || len(request.NodeIDs) != 1 {
		t.Fatalf("request = %+v", request)
	}
	if request.Params["operation"] != "apply" || request.Params["config_sha256"] != site.Digest ||
		request.Params["artifact_path"] != site.ArtifactPath || request.Params["site_path"] != site.Path {
		t.Fatalf("params = %+v", request.Params)
	}
	if _, ok := request.Params["config"]; ok {
		t.Fatal("task params carry the raw config instead of an artifact reference")
	}
	if request.Params["takeover_approved"] != "true" || request.Params["approval_ref"] != "approval-1" {
		t.Fatalf("takeover params = %+v", request.Params)
	}
	for key, value := range request.Params {
		if len(value) > 1024 || strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			t.Fatalf("param %q is not task-safe: %q", key, value)
		}
	}
	remove, err := gateway.BuildNginxRemoveTask(site, input)
	if err != nil {
		t.Fatal(err)
	}
	if remove.ActionID != gateway.NginxActionRemove || remove.Params["operation"] != "remove" ||
		remove.Params["config_sha256"] != site.Digest {
		t.Fatalf("remove request = %+v", remove)
	}
	if _, ok := remove.Params["artifact_path"]; ok {
		t.Fatal("remove plan should not reference a new artifact")
	}
}

func TestBuildNginxTaskRejectsMismatchedIdentity(t *testing.T) {
	site, err := gateway.RenderNginxSite(directHTTPRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.BuildNginxApplyTask(site, gateway.NginxTaskInput{GatewayID: "gw-2", NodeID: "node-1"}); err == nil {
		t.Fatal("mismatched gateway accepted")
	}
	if _, err := gateway.BuildNginxApplyTask(site, gateway.NginxTaskInput{GatewayID: "gw-1"}); err == nil {
		t.Fatal("missing node accepted")
	}
}

func TestRenderNginxSiteCustomListenForIsolation(t *testing.T) {
	request := directHTTPRequest()
	request.HTTPListen = "127.0.0.1:18080"
	site, err := gateway.RenderNginxSite(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(site.Config), "listen 127.0.0.1:18080;") {
		t.Fatalf("config:\n%s", site.Config)
	}
	request.HTTPS = true
	request.CertificatePath = "/etc/serverfleet/fullchain.pem"
	request.PrivateKeyPath = "/etc/serverfleet/privkey.pem"
	request.HTTPListen = "18080"
	request.HTTPSListen = "127.0.0.1:18443"
	site, err = gateway.RenderNginxSite(request)
	if err != nil {
		t.Fatal(err)
	}
	config := string(site.Config)
	if !strings.Contains(config, "listen 18080;") || !strings.Contains(config, "listen 127.0.0.1:18443 ssl;") {
		t.Fatalf("config:\n%s", config)
	}
	for _, listen := range []string{"80; include", "127.0.0.1:18080;}", "0", "70000"} {
		request.HTTPListen = listen
		if _, err := gateway.RenderNginxSite(request); err == nil {
			t.Fatalf("listen %q accepted", listen)
		}
	}
}
