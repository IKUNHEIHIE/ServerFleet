package server

import "serverfleet.local/serverfleet/internal/ai"

func (s *Server) registerAIBudgetRoutes() error {
	handler, err := ai.NewBudgetHTTP(s.AIBudget, s.AIRuntime.Models, s.aiAuthenticate)
	if err != nil {
		return err
	}
	s.mux.Handle("GET /api/v1/ai/model-settings/{profileId}/budget", handler)
	s.mux.Handle("PUT /api/v1/ai/model-settings/{profileId}/budget", handler)
	return nil
}
