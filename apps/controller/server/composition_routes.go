package server

import (
	"encoding/json"
	"net/http"
	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/dns"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/gateway"
	"serverfleet.local/serverfleet/internal/monitoring"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/notifications"
	"serverfleet.local/serverfleet/internal/publishing"
	"serverfleet.local/serverfleet/internal/quota"
	"serverfleet.local/serverfleet/internal/servicegroups"
	"serverfleet.local/serverfleet/internal/tasks"
)

func (s *Server) registerRoutes() error {
	s.registerOperatorIncidentRoutes()
	s.registerBackupMaintenanceRoutes()
	s.mux.HandleFunc("GET /readyz", s.readiness)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "stage": "v0.9-wave5"})
	})

	for _, pattern := range []string{
		"/api/v1/auth/",
		"/api/v1/users",
		"/api/v1/users/",
		"/api/v1/roles",
		"/api/v1/permissions",
		"/api/v1/resource-grants",
		"/api/v1/resource-grants/",
		"/api/v1/credentials",
		"/api/v1/credentials/",
	} {
		s.mux.Handle(pattern, s.IdentityHTTP)
	}

	s.registerEnrollmentRoutes()
	if s.IdentityRenewalHTTP != nil {
		s.mux.Handle("/api/v1/agent/identity/renew", s.IdentityRenewalHTTP)
	}
	s.registerAIRoutes()
	if err := s.RegisterAIBudgetRoutes(); err != nil {
		return err
	}
	s.registerPluginRoutes()
	s.registerUpdateRecoveryRoutes()
	s.registerReleaseSourceRoutes()
	s.registerCertificateCSRRoutes()
	s.registerCertificateInstallRoutes()
	s.registerCertificateSetRoutes()
	s.registerPublicCertificateRoutes()
	s.registerCommandRoutes()
	s.registerResidentUpdateRoutes()
	s.registerRecoveryAuthorityRoutes()
	s.registerFileResumeRoutes()
	s.registerPublicationDNSRoutes()
	if s.AgentLinkHTTP != nil {
		s.mux.Handle("/api/v1/agent/link", s.AgentLinkHTTP)
	} else {
		s.mux.HandleFunc("/api/v1/agent/link", agentTransportUnavailable)
	}
	if s.EnrollmentHTTP != nil {
		s.mux.Handle("/api/v1/agent/enroll", s.EnrollmentHTTP)
	} else {
		s.mux.HandleFunc("/api/v1/agent/enroll", agentTransportUnavailable)
	}

	if err := nodes.RegisterHTTP(s.mux, s.Nodes, func(r *http.Request) (nodes.Principal, error) {
		sub, reqID, err := s.authenticate(r)
		return nodes.Principal{SubjectID: sub, RequestID: reqID}, err
	}); err != nil {
		return err
	}
	if err := servicegroups.RegisterHTTP(s.mux, s.ServiceGroups, func(r *http.Request) (servicegroups.Principal, error) {
		sub, reqID, err := s.authenticate(r)
		return servicegroups.Principal{SubjectID: sub, RequestID: reqID}, err
	}); err != nil {
		return err
	}
	if err := tasks.Register(
		s.mux,
		s.Tasks,
		func(r *http.Request) (tasks.Principal, error) {
			sub, reqID, err := s.authenticate(r)
			return tasks.Principal{SubjectID: sub, RequestID: reqID}, err
		},
		&composedGroupResolver{groups: s.GroupsRepo, serviceGroups: s.ServiceGroupsRepo},
		resolveOperationClass,
		&composedTaskReadGate{identity: s.Identity, tasks: s.TasksRepo},
	); err != nil {
		return err
	}
	if err := files.RegisterHTTP(s.mux, s.Files, func(r *http.Request) (files.Principal, error) {
		sub, reqID, err := s.authenticate(r)
		return files.Principal{SubjectID: sub, RequestID: reqID}, err
	}); err != nil {
		return err
	}
	if err := audit.Register(
		s.mux,
		s.Audit,
		func(r *http.Request) (audit.Principal, error) {
			sub, reqID, err := s.authenticate(r)
			return audit.Principal{SubjectID: sub, RequestID: reqID}, err
		},
		&composedAuditReadGate{identity: s.Identity},
	); err != nil {
		return err
	}
	if err := dns.RegisterHTTP(s.mux, s.DNS, func(r *http.Request) (dns.Principal, error) {
		sub, reqID, err := s.authenticate(r)
		return dns.Principal{SubjectID: sub, RequestID: reqID}, err
	}); err != nil {
		return err
	}
	if err := notifications.RegisterHTTP(
		s.mux,
		s.Notifications,
		func(r *http.Request) (notifications.Principal, error) {
			sub, reqID, err := s.authenticate(r)
			return notifications.Principal{SubjectID: sub, RequestID: reqID}, err
		},
		&composedNotificationsReadGate{identity: s.Identity},
	); err != nil {
		return err
	}
	if err := gateway.RegisterHTTP(s.mux, s.Gateway, func(r *http.Request) (gateway.Principal, error) {
		sub, reqID, err := s.authenticate(r)
		return gateway.Principal{SubjectID: sub, RequestID: reqID}, err
	}); err != nil {
		return err
	}
	if err := monitoring.RegisterHTTP(
		s.mux,
		s.Monitoring,
		&composedTrafficRiskSource{quota: s.Quota},
		func(r *http.Request) (monitoring.Principal, error) {
			sub, reqID, err := s.authenticate(r)
			return monitoring.Principal{SubjectID: sub, RequestID: reqID}, err
		},
	); err != nil {
		return err
	}
	if err := quota.RegisterHTTP(s.mux, s.Quota, func(r *http.Request) (quota.Principal, error) {
		sub, reqID, err := s.authenticate(r)
		return quota.Principal{SubjectID: sub, RequestID: reqID}, err
	}); err != nil {
		return err
	}
	if err := certificates.RegisterHTTP(
		s.mux,
		s.Certificates,
		&composedServiceGroupLookup{repo: s.ServiceGroupsRepo},
		func(r *http.Request) (certificates.Principal, error) {
			sub, reqID, err := s.authenticate(r)
			return certificates.Principal{SubjectID: sub, RequestID: reqID}, err
		},
	); err != nil {
		return err
	}
	if err := certificates.RegisterCleanupHTTP(s.mux, s.CertificateCleanup, func(r *http.Request) (certificates.CleanupPrincipal, error) {
		sub, id, err := s.authenticate(r)
		return certificates.CleanupPrincipal{SubjectID: sub, RequestID: id}, err
	}); err != nil {
		return err
	}
	if err := publishing.RegisterHTTP(s.mux, s.Publishing, func(r *http.Request) (publishing.Principal, error) {
		sub, reqID, err := s.authenticate(r)
		return publishing.Principal{SubjectID: sub, RequestID: reqID}, err
	}); err != nil {
		return err
	}
	if err := s.registerWorkflowRoutes(); err != nil {
		return err
	}
	return nil
}

func (s *Server) registerEnrollmentRoutes() {
	patterns := []string{
		"/api/v1/enrollment-tokens",
		"/api/v1/node-enrollment-tokens",
		"/api/v1/enrollments",
		"/api/v1/enrollments/",
		"/api/v1/node-enrollments",
		"/api/v1/node-enrollments/",
	}
	for _, pattern := range patterns {
		if s.EnrollmentManagement != nil {
			s.mux.Handle(pattern, s.EnrollmentManagement)
			continue
		}
		s.mux.HandleFunc(pattern, agentTransportUnavailable)
	}
}

func (s *Server) registerAIRoutes() {
	if s.AIAgentHTTP != nil {
		for _, pattern := range []string{"GET /api/v1/ai/sessions/{sessionId}/runs", "POST /api/v1/ai/sessions/{sessionId}/runs", "GET /api/v1/ai/sessions/{sessionId}/runs/{runId}", "POST /api/v1/ai/sessions/{sessionId}/runs/{runId}/advance", "POST /api/v1/ai/sessions/{sessionId}/runs/{runId}/cancel"} {
			s.mux.Handle(pattern, s.AIAgentHTTP)
		}
	}
	if s.AIManagement != nil {
		for _, pattern := range []string{
			"GET /api/v1/ai/security-policy",
			"POST /api/v1/ai/sessions",
			"GET /api/v1/ai/sessions/{sessionId}",
			"GET /api/v1/ai/sessions/{sessionId}/approval-policy",
			"PATCH /api/v1/ai/sessions/{sessionId}/approval-policy",
			"POST /api/v1/ai/sessions/{sessionId}/grants",
			"DELETE /api/v1/ai/sessions/{sessionId}/grants/{grantId}",
			"POST /api/v1/ai/sessions/{sessionId}/approvals",
			"POST /api/v1/ai/sessions/{sessionId}/approvals/{approvalId}/decision",
			"POST /api/v1/ai/tools/execute",
			"POST /api/v1/ai/sessions/{sessionId}/resource-recommendations",
		} {
			s.mux.Handle(pattern, s.AIManagement)
		}
	}
	if s.AIModelsHTTP != nil {
		s.mux.Handle("GET /api/v1/ai/model-settings", s.AIModelsHTTP)
		s.mux.Handle("PUT /api/v1/ai/model-settings", s.AIModelsHTTP)
	}
	if s.AIGenerationHTTP != nil {
		s.mux.Handle("POST /api/v1/ai/sessions/{sessionId}/messages/{messageId}/generate", s.AIGenerationHTTP)
	}
	if s.AIMessagesHTTP != nil {
		s.mux.Handle("GET /api/v1/ai/sessions/{sessionId}/messages", s.AIMessagesHTTP)
		s.mux.Handle("POST /api/v1/ai/sessions/{sessionId}/messages", s.AIMessagesHTTP)
	}
	if s.AIEventsHTTP != nil {
		s.mux.Handle("GET /api/v1/ai/sessions/{sessionId}/events", s.AIEventsHTTP)
	}
}

func (s *Server) registerPluginRoutes() {
	if s.PluginHTTP != nil {
		for _, pattern := range []string{
			"GET /api/v1/plugins",
			"GET /api/v1/plugins/repository",
			"PUT /api/v1/plugins/repository",
			"GET /api/v1/plugins/catalog",
			"POST /api/v1/plugins/catalog/refresh",
			"POST /api/v1/plugins/{pluginId}/install",
			"PATCH /api/v1/plugins/{pluginId}/activation",
			"PUT /api/v1/plugins/{pluginId}/activation",
			"POST /api/v1/plugins/{pluginId}/update",
			"DELETE /api/v1/plugins/{pluginId}",
			"GET /api/v1/plugins/{pluginId}",
			"GET /api/v1/plugin-operations/{operationId}",
		} {
			s.mux.Handle(pattern, s.PluginHTTP)
		}
	}
	if s.XrayHTTP != nil {
		for _, pattern := range s.XrayHTTP.Patterns() {
			s.mux.Handle(pattern, s.XrayHTTP)
		}
	}
}

func agentTransportUnavailable(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "about:blank", "title": http.StatusText(http.StatusServiceUnavailable),
		"status": http.StatusServiceUnavailable, "code": "AGENT_TRANSPORT_UNAVAILABLE",
		"requestId": newRequestID(),
	})
}
