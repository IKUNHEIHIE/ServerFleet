// Package main 是 Wave6-R2 的测试专用验收宿主（V09-R6-GM-03）。它组合正式的
// apps/controller/server.New 与 internal/ai.LocalTLSProviderConfig、持久化
// AIBudget，让真实浏览器在当前 Cookie/CSRF/审批语义下完成 设置 → 消息 →
// 显式 generate → 真实 assistant 终态。依赖方向只允许本包导入生产包；
// 生产代码绝不导入本包。它不是产品 mock：组合与校验全部走真实代码路径，
// 宿主只提供程序级注入（LocalAIProvider）与预算配置动作（预算 HTTP 路由
// 尚未交付，Configure 的 admin gate 仍执行真实管理员/owner/profile 校验）。
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"serverfleet.local/serverfleet/apps/controller/localpki"
	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/ai"
)

type configureRequest struct {
	OwnerID   string `json:"ownerId"`
	ProfileID string `json:"profileId"`
	Secret    string `json:"secret"`
}

type fixtureResponse struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
	Count  int    `json:"requestCount,omitempty"`
	Bearer *bool  `json:"lastBearerOk,omitempty"`
	Model  string `json:"lastModel,omitempty"`
	Window string `json:"windowId,omitempty"`
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "composed API listen address (host:port)")
	origin := flag.String("origin", "", "trusted https:// browser origin (required)")
	dbPath := flag.String("db", "", "SQLite database path (required)")
	tlsCert := flag.String("tls-cert", "", "TLS server certificate path (required)")
	tlsKey := flag.String("tls-key", "", "TLS server key path (required)")
	stagingDir := flag.String("staging-dir", "", "optional staging directory override")
	aiUpstreamOrigin := flag.String("ai-upstream-origin", "", "loopback https:// origin of the fixture AI upstream (required)")
	aiUpstreamCA := flag.String("ai-upstream-ca", "", "PEM file with the CA that signed the AI upstream certificate (required)")
	aiUpstreamCert := flag.String("ai-upstream-cert", "", "AI upstream TLS certificate path (required)")
	aiUpstreamKey := flag.String("ai-upstream-key", "", "AI upstream TLS key path (required)")
	aiUpstreamListen := flag.String("ai-upstream-listen", "127.0.0.1:18444", "AI upstream TLS listen address")
	fixtureListen := flag.String("fixture-listen", "127.0.0.1:18445", "read-only fixture telemetry listen address")
	agentPKIDir := flag.String("agent-pki-dir", "", "optional local agent PKI directory; enables the enrollment and Agent WSS link")
	agentAuthority := flag.String("agent-authority", "", "agent-facing authority host:port when the agent link is enabled")
	agentServerName := flag.String("agent-server-name", "", "agent TLS server name when the agent link is enabled")
	secretFile := flag.String("ai-secret-file", "", "private synthetic upstream secret file")
	flag.Parse()
	controlHost, _, controlErr := net.SplitHostPort(*fixtureListen)
	if controlErr != nil || net.ParseIP(controlHost) == nil || !net.ParseIP(controlHost).IsLoopback() {
		log.Fatal("fixture telemetry must bind loopback")
	}
	secretInfo, secretErr := os.Lstat(*secretFile)
	if secretErr != nil || !secretInfo.Mode().IsRegular() || secretInfo.Mode().Perm()&0077 != 0 || secretInfo.Size() > 128 {
		log.Fatal("private fixture secret required")
	}
	secret, secretErr := os.ReadFile(*secretFile)
	if secretErr != nil || len(secret) < 16 {
		log.Fatal("invalid synthetic fixture secret")
	}

	if *origin == "" || *dbPath == "" || *tlsCert == "" || *tlsKey == "" ||
		*aiUpstreamOrigin == "" || *aiUpstreamCA == "" || *aiUpstreamCert == "" || *aiUpstreamKey == "" {
		log.Fatalf("fixturehost: -origin, -db, -tls-cert, -tls-key, -ai-upstream-origin, -ai-upstream-ca, -ai-upstream-cert and -ai-upstream-key are required")
	}
	roots := x509.NewCertPool()
	if caPEM, err := os.ReadFile(*aiUpstreamCA); err != nil || !roots.AppendCertsFromPEM(caPEM) {
		log.Fatalf("fixturehost: ai upstream CA PEM is unusable")
	}

	upstream := &upstreamState{}
	upstream.setSecret(string(secret))
	if err := upstream.serveTLS(*aiUpstreamListen, *aiUpstreamCert, *aiUpstreamKey); err != nil {
		log.Fatalf("%v", err)
	}
	log.Printf("fixturehost: ai upstream serving on %s", *aiUpstreamListen)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	composedConfig := server.Config{
		DBPath:     *dbPath,
		Origin:     *origin,
		StagingDir: *stagingDir,
		LocalAIProvider: &ai.LocalTLSProviderConfig{
			Origin: *aiUpstreamOrigin,
			Roots:  roots,
		},
	}
	var agentRoots *x509.CertPool
	agentMaterial, agentMaterialErr := func() (*localpki.Material, error) {
		if *agentPKIDir == "" {
			return nil, nil
		}
		return localpki.Ensure(*agentPKIDir)
	}()
	if agentMaterialErr != nil {
		log.Fatalf("fixturehost: local agent PKI: %v", agentMaterialErr)
	}
	if agentMaterial != nil {
		composedConfig.EnrollmentIssuer = agentMaterial.Issuer
		composedConfig.AgentAuthority = *agentAuthority
		composedConfig.AgentServerName = *agentServerName
		agentRoots = agentMaterial.Roots
	}
	composed, err := server.New(ctx, composedConfig)
	if err != nil {
		log.Fatalf("fixturehost: compose server: %v", err)
	}
	log.Printf("fixturehost: composed real server.New with LocalAIProvider %s and persistent AIBudget", *aiUpstreamOrigin)

	certificate, err := tls.LoadX509KeyPair(*tlsCert, *tlsKey)
	if err != nil {
		log.Fatalf("fixturehost: load api key pair: %v", err)
	}

	controlMux := http.NewServeMux()
	controlMux.HandleFunc("GET /fixture/health", func(writer http.ResponseWriter, request *http.Request) {
		writeFixtureJSON(writer, http.StatusOK, fixtureResponse{OK: true})
	})
	controlMux.HandleFunc("GET /fixture/upstream", func(writer http.ResponseWriter, request *http.Request) {
		count, bearerOK, model := upstream.snapshot()
		writeFixtureJSON(writer, http.StatusOK, fixtureResponse{OK: true, Count: count, Bearer: &bearerOK, Model: model})
	})
	controlServer := &http.Server{
		Handler:           controlMux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	controlListener, err := net.Listen("tcp", *fixtureListen)
	if err != nil {
		log.Fatalf("fixturehost: control plane listen: %v", err)
	}
	go func() {
		_ = controlServer.Serve(controlListener)
	}()
	log.Printf("fixturehost: control plane serving on %s", *fixtureListen)

	api := &http.Server{
		Addr:              *listen,
		Handler:           composed,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{certificate},
			ClientCAs:    agentRoots,
			ClientAuth:   tls.VerifyClientCertIfGiven,
		},
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = api.Shutdown(shutdownCtx)
		_ = controlServer.Shutdown(shutdownCtx)
	}()
	log.Printf("fixturehost: composed management API serving on https://%s", *listen)
	if err := api.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		log.Fatalf("fixturehost: serve: %v", err)
	}
}

func writeFixtureJSON(writer http.ResponseWriter, status int, value fixtureResponse) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
