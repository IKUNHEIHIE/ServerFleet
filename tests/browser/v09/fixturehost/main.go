// Package main 是 Wave6-R2 的测试专用验收宿主（V09-R6-GM-03）。它组合正式的
// apps/controller/server.New 与 internal/ai.LocalTLSProviderConfig、持久化
// AIBudget，让真实浏览器在当前 Cookie/CSRF/审批语义下完成 设置 → 消息 →
// 显式 generate → 真实 assistant 终态。依赖方向只允许本包导入生产包；
// 生产代码绝不导入本包。它不是产品 mock：组合与校验全部走真实代码路径，
// 宿主只提供程序级注入（LocalAIProvider）；预算配置通过正式的、带当前
// Cookie/Origin/CSRF 和管理员/owner/profile 校验的 HTTP 路由完成。
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
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"serverfleet.local/serverfleet/apps/controller/localpki"
	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/ai"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/updates"
)

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
	fileSpool := flag.String("file-spool", "", "private execution spool directory (enables the unified file worker)")
	fileRoots := flag.String("file-roots", "", "private 0600 JSON registry mapping enrolled nodeID to its logical absolute root")
	productUpdateConfig := flag.String("product-update-config", "", "private 0600 JSON enabling the trusted local Agent product update assembly")
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
	// 统一文件执行只在测试程序显式提供私有 spool 和受控 nodeID 注册表时开启。
	// 注册表是本地私有文件（0600），fixturehost 不暴露任何修改它的 HTTP 入口；
	// 没有 Agent PKI/受控链路时拒绝开启，避免“配置了一个不会执行的 worker”。
	if (*fileSpool == "") != (*fileRoots == "") {
		log.Fatal("fixturehost: -file-spool and -file-roots must be provided together")
	}
	if *fileSpool != "" {
		if agentMaterial == nil || !filepath.IsAbs(*fileSpool) || !filepath.IsAbs(*fileRoots) {
			log.Fatal("fixturehost: file execution requires -agent-pki-dir and absolute -file-spool/-file-roots paths")
		}
		if err := files.CheckExecutionSpool(*fileSpool); err != nil {
			log.Fatalf("fixturehost: file spool is not private: %v", err)
		}
		rootsInfo, err := os.Lstat(*fileRoots)
		if err != nil || !rootsInfo.Mode().IsRegular() || rootsInfo.Mode()&os.ModeSymlink != 0 || (runtime.GOOS != "windows" && rootsInfo.Mode().Perm()&0o077 != 0) || rootsInfo.Size() > maxFileRootsBytes {
			log.Fatal("fixturehost: -file-roots must be a private regular file owned by the test program")
		}
		composedConfig.FileExecution = &server.FileExecutionConfig{
			Paths:    registryPathMapper{path: *fileRoots},
			SpoolDir: *fileSpool,
		}
		log.Printf("fixturehost: unified file worker enabled with controlled node registry %s", *fileRoots)
	}
	// Agent 组件本地签名更新：只在显式给出私有配置时装配。SchemaFingerprint 必须
	// 来自真实迁移后的库定义，因此先用同一 DB 组合一次正式 Server 读取指纹，关闭后
	// 再以真实指纹重新组合；配置文件中声明的指纹一律忽略。
	if *productUpdateConfig != "" {
		if agentMaterial == nil {
			log.Fatal("fixturehost: product update requires -agent-pki-dir and the Agent WSS link")
		}
		productFile, loadErr := loadProductUpdateFile(*productUpdateConfig)
		if loadErr != nil {
			log.Fatalf("fixturehost: product update config: %v", loadErr)
		}
		probe, probeErr := server.New(ctx, composedConfig)
		if probeErr != nil {
			log.Fatalf("fixturehost: compose for schema fingerprint: %v", probeErr)
		}
		fingerprint, fpErr := updates.SQLiteSchemaFingerprint(ctx, probe.DB)
		if closeErr := probe.Close(); closeErr != nil {
			log.Fatalf("fixturehost: close fingerprint probe: %v", closeErr)
		}
		if fpErr != nil {
			log.Fatalf("fixturehost: read schema fingerprint: %v", fpErr)
		}
		productOptions, optErr := productFile.options(fingerprint)
		if optErr != nil {
			log.Fatalf("fixturehost: product update options: %v", optErr)
		}
		composedConfig.UpdateProduct = &productOptions
		log.Printf("fixturehost: trusted agent product update enabled; slot %s inbox %s key %s node %s candidate %s schema fingerprint %s",
			productOptions.Directory, productOptions.Inbox, productOptions.KeyID, productOptions.NodeID, productOptions.CandidateVersion, fingerprint)
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
	serveErr := api.ListenAndServeTLS("", "")
	// 生命周期必须闭合：正式的 Server.Close 会停止自有 Agent 进程并清理 supervisor
	// 保留记录。缺少这一步时，下一次启动会被“installation reserved”拒绝（这是产品
	// 的正确行为，不是宿主可以绕过的东西）。
	if closeErr := composed.Close(); closeErr != nil {
		log.Printf("fixturehost: close composed server: %v", closeErr)
	}
	if serveErr != nil && serveErr != http.ErrServerClosed {
		log.Fatalf("fixturehost: serve: %v", serveErr)
	}
}

func writeFixtureJSON(writer http.ResponseWriter, status int, value fixtureResponse) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
