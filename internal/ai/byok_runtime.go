package ai

import (
	"context"

	"serverfleet.local/serverfleet/internal/auth"
)

type GenerationQuota func(context.Context, GenerationRequest) error

// BYOKRuntime binds the existing persistent settings, conversation and HTTP
// provider. Composition must register Conversation.CheckCredentialOperation in
// the credential gate. A mandatory quota adapter admits this exact operation;
// nil is unavailable, never unlimited. Filter remains mandatory.
type BYOKRuntime struct {
	Models       *ModelSettingsService
	Conversation *ConversationService
	provider     *OpenAITextProvider
}

func NewBYOKRuntime(sessions *SessionService, credentials *auth.CredentialService, filter func(context.Context, SessionView, string, string) (string, error), quota GenerationQuota) (*BYOKRuntime, error) {
	if credentials == nil || quota == nil || filter == nil {
		return nil, ErrModelUnavailable
	}
	models, err := NewModelSettingsService(sessions, credentials.ServiceMetadata)
	if err != nil {
		return nil, err
	}
	provider, err := NewOpenAIStreamingTextProvider(credentials, models.ResolveCredential)
	if err != nil {
		return nil, err
	}
	conversation, err := NewConversationService(sessions, ConversationConfig{Profile: models.Profile, Filter: filter, Generate: func(ctx context.Context, r GenerationRequest, emit func(string) error) error {
		if quota(ctx, r) != nil || ctx.Err() != nil {
			return ErrToolDenied
		}
		return provider.Generate(ctx, r, emit)
	}})
	if err != nil {
		provider.Close()
		return nil, err
	}
	return &BYOKRuntime{Models: models, Conversation: conversation, provider: provider}, nil
}
func (r *BYOKRuntime) Close() {
	if r != nil {
		r.provider.Close()
	}
}
