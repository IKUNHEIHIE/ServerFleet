package server

import "serverfleet.local/serverfleet/internal/ai"

// RegisterAIBudgetRoutes is the program assembly port used by the central patch.
func (s *Server) RegisterAIBudgetRoutes() error {
	handler, err := ai.NewBudgetHTTP(s.AIBudget, s.AIRuntime.Models, s.aiAuthenticate)
	if err != nil {
		return err
	}
	s.mux.Handle("GET /api/v1/ai/model-settings/{profileId}/budget", handler)
	s.mux.Handle("PUT /api/v1/ai/model-settings/{profileId}/budget", handler)
	return nil
}
