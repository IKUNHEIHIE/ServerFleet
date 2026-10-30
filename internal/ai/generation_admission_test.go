package ai

import (
	"context"
	"strings"
	"testing"

	"serverfleet.local/serverfleet/internal/auth"
)

func generationCredentialOperation(t *testing.T, r GenerationRequest) auth.CredentialOperation {
	t.Helper()
	digest, err := generationDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	return auth.CredentialOperation{SubjectID: r.SubjectID, CredentialID: r.Profile.CredentialRef, Module: "ai", Source: "ai", OperationID: r.OperationID, PlanDigest: digest, Action: "use", ExpectedRevision: 1}
}

func TestGenerationCredentialGateRequiresPrivateLiveObservationAndFullBinding(t *testing.T) {
	ctx := context.Background()
	s, _, _, _, _ := sessionFixture(t)
	v, err := s.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var chat *ConversationService
	var savedContext context.Context
	var savedOperation auth.CredentialOperation
	chat, _ = conversationFixture(t, s, func(ctx context.Context, r GenerationRequest, emit func(string) error) error {
		op := generationCredentialOperation(t, r)
		if err := chat.CheckCredentialOperation(ctx, op); err != nil {
			t.Fatal("durable generating request rejected", err)
		}
		if err := chat.CheckCredentialOperation(context.Background(), op); err != ErrToolDenied {
			t.Fatal("request fields supplied authority", err)
		}
		if _, err := s.AddGrant(ctx, v.ID, v.ScopeRevision, ResourceGrantInput{"node", "node-a", []string{"nodes.read"}}); err != ErrToolDenied {
			t.Fatal("inference context expanded human grants", err)
		}
		if _, err := s.SetPolicy(ctx, v.ID, v.ScopeRevision, FullAccess, 15); err != ErrToolDenied {
			t.Fatal("inference context elevated approval mode", err)
		}
		for _, change := range []func(*auth.CredentialOperation){
			func(o *auth.CredentialOperation) { o.SubjectID = "other" }, func(o *auth.CredentialOperation) { o.CredentialID = "other" }, func(o *auth.CredentialOperation) { o.OperationID = "other" }, func(o *auth.CredentialOperation) { o.PlanDigest = strings.Repeat("f", 64) }, func(o *auth.CredentialOperation) { o.Module = "dns" }, func(o *auth.CredentialOperation) { o.Source = "system" }, func(o *auth.CredentialOperation) { o.Action = "revoke" }, func(o *auth.CredentialOperation) { o.ChangeID = "forged" }, func(o *auth.CredentialOperation) { o.ExpectedRevision = 0 },
		} {
			bad := op
			change(&bad)
			if err := chat.CheckCredentialOperation(ctx, bad); err != ErrToolDenied {
				t.Fatal("partial operation binding accepted", err)
			}
		}
		savedContext = context.WithoutCancel(ctx)
		savedOperation = op
		return emit("plain answer")
	})
	message, err := chat.Submit(ctx, v.ID, "request-one", MessageInput{"question", "client-one", 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = chat.Generate(ctx, v.ID, message.MessageID); err != nil {
		t.Fatal(err)
	}
	if err = chat.CheckCredentialOperation(savedContext, savedOperation); err != ErrToolDenied {
		t.Fatal("retained context survived generation", err)
	}
}

func TestGenerationCredentialGateRejectsCurrentRevocationAndUnknown(t *testing.T) {
	for _, failure := range []string{"actor", "profile-disabled", "resources", "scope", "closed", "unknown"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			s, _, actor, current, _ := sessionFixture(t)
			v, err := s.Create(ctx)
			if err != nil {
				t.Fatal(err)
			}
			v, err = s.AddGrant(ctx, v.ID, v.ScopeRevision, ResourceGrantInput{"node", "node-a", []string{"nodes.read"}})
			if err != nil {
				t.Fatal(err)
			}
			var chat *ConversationService
			var enabled *bool
			humanContext := ctx
			chat, enabled = conversationFixture(t, s, func(ctx context.Context, r GenerationRequest, _ func(string) error) error {
				op := generationCredentialOperation(t, r)
				if err := chat.CheckCredentialOperation(ctx, op); err != nil {
					t.Fatal(err)
				}
				switch failure {
				case "actor":
					actor.SubjectID = "another"
				case "profile-disabled":
					*enabled = false
				case "resources":
					current.AllowedNodes = []string{}
				case "scope":
					_, err = s.SetPolicy(humanContext, v.ID, v.ScopeRevision, LowRisk, 0)
				case "closed":
					_, err = s.Close(humanContext, v.ID, v.ScopeRevision)
				case "unknown":
					chat.markUnknown(ctx, v.ID, r.MessageID)
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := chat.CheckCredentialOperation(ctx, op); err != ErrToolDenied {
					t.Fatal("live change ignored", err)
				}
				return ErrToolDenied
			})
			m, err := chat.Submit(ctx, v.ID, "request-one", MessageInput{"question", "client-one", v.ScopeRevision})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = chat.Generate(ctx, v.ID, m.MessageID); err != ErrGenerationUnknown {
				t.Fatal("revoked inference succeeded", err)
			}
		})
	}
}
