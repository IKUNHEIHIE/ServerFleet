package server

import (
	"errors"
	"strings"
	"testing"

	"serverfleet.local/serverfleet/internal/gateway"
)

func loopbackGatewayDeploymentDocument() []byte {
	return []byte(`{"version":1,"gateways":[{"gatewayId":"gw-one","siteDir":"/srv/fleet/nginx/conf","artifactDir":"/srv/fleet/nginx/artifacts","httpListen":"127.0.0.1:18080","httpsListen":"127.0.0.1:18443"}],"certificates":[]}`)
}

// G1: the trusted startup document must accept an explicit loopback host:port
// listener, matching the renderer's accepted syntax, and the value must reach
// the rendered site rather than being silently normalized away.
func TestReviewR2GatewayDeploymentAllowsLoopbackListener(t *testing.T) {
	deployment, err := parseGatewayDeployment(loopbackGatewayDeploymentDocument())
	if err != nil {
		t.Fatalf("loopback deployment rejected: %v", err)
	}
	got := deployment.Gateways["gw-one"]
	if got.HTTPListen != "127.0.0.1:18080" || got.HTTPSListen != "127.0.0.1:18443" {
		t.Fatalf("listen values not preserved: %+v", got)
	}
	site, err := gateway.RenderNginxSite(gateway.NginxSiteRequest{
		GatewayID: "gw-one", Domain: "app.example.com", Upstream: "127.0.0.1:19000",
		SiteDir: "/srv/fleet/nginx/conf", ArtifactDir: "/srv/fleet/nginx/artifacts",
		HTTPListen: got.HTTPListen,
	})
	if err != nil {
		t.Fatalf("loopback listen rejected by renderer: %v", err)
	}
	if !strings.Contains(string(site.Config), "listen 127.0.0.1:18080;") {
		t.Fatalf("loopback listener missing from rendered site:\n%s", site.Config)
	}
}

func TestReviewR2GatewayDeploymentRejectsInvalidListeners(t *testing.T) {
	for _, value := range []string{"0", "65536", "-1", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:80;evil", "8080 8080"} {
		document := []byte(`{"version":1,"gateways":[{"gatewayId":"gw-one","siteDir":"/srv/fleet/nginx/conf","artifactDir":"/srv/fleet/nginx/artifacts","httpListen":"` + value + `"}],"certificates":[]}`)
		if _, err := parseGatewayDeployment(document); !errors.Is(err, ErrGatewayDeployment) {
			t.Fatalf("listen %q accepted: %v", value, err)
		}
	}
}

// The real Agent file capability only accepts a missing final path component;
// every parent directory of a write must already exist. The artifact file must
// therefore live directly under the installed artifact directory (named by its
// content digest) rather than in an implicit per-gateway subdirectory.
func TestReviewR2GatewayArtifactLivesDirectlyUnderArtifactDir(t *testing.T) {
	site, err := gateway.RenderNginxSite(gateway.NginxSiteRequest{
		GatewayID: "gw-one", Domain: "app.example.com", Upstream: "127.0.0.1:19000",
		SiteDir: "/srv/fleet/nginx/conf", ArtifactDir: "/srv/fleet/nginx/artifacts",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "/srv/fleet/nginx/artifacts/" + site.Digest + ".conf"; site.ArtifactPath != want {
		t.Fatalf("artifact path = %q, want %q", site.ArtifactPath, want)
	}
}
