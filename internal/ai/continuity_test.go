package ai

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestChatMemoryReachesExecutionAndExecutionReturnsToChat(t *testing.T) {
	ctx := context.Background()
	loop, view, _, _ := agentLoopFixture(t, false, false)
	var memory string
	loop.conversation.config.Generate = func(_ context.Context, r GenerationRequest, emit func(string) error) error {
		if r.Agent != nil {
			memory = r.Agent.Memory
			return emit("{\"text\":\"checked actual evidence\",\"items\":[]}")
		}
		return emit("Acknowledged")
	}
	message, err := loop.conversation.Submit(ctx, view.ID, "memory-chat", MessageInput{Content: "Only use /srv/application on port 18765; preserve /etc/example.conf", ClientMessageID: "memory-chat", ScopeRevision: view.ScopeRevision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = loop.conversation.Generate(ctx, view.ID, message.MessageID); err != nil {
		t.Fatal(err)
	}
	run, err := loop.Create(ctx, view.ID, AgentRunInput{Goal: "Follow the previous plan", ClientRunID: "memory-run", ScopeRevision: view.ScopeRevision})
	if err != nil {
		t.Fatal(err)
	}
	run, err = loop.Advance(ctx, view.ID, run.ID, run.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(memory, "/etc/example.conf") || !strings.Contains(memory, "18765") {
		t.Fatal("chat constraints absent from execution context", memory)
	}
	var chatContext string
	loop.conversation.config.Generate = func(_ context.Context, r GenerationRequest, emit func(string) error) error {
		for _, m := range r.Messages {
			chatContext += m.RedactedContent
		}
		return emit("summary")
	}
	message, err = loop.conversation.Submit(ctx, view.ID, "memory-followup", MessageInput{Content: "What happened?", ClientMessageID: "memory-followup", ScopeRevision: view.ScopeRevision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = loop.conversation.Generate(ctx, view.ID, message.MessageID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(chatContext, run.ID) || !strings.Contains(chatContext, "checked actual evidence") {
		t.Fatal("execution evidence absent from chat")
	}
}
func TestCompactionPreservesLateConstraints(t *testing.T) {
	ctx := context.Background()
	loop, view, _, _ := agentLoopFixture(t, false, false)
	loop.conversation.config.Generate = func(_ context.Context, _ GenerationRequest, emit func(string) error) error {
		return emit("Acknowledged")
	}
	text := strings.Repeat("Detailed background. ", 80) + "Never delete /srv/data. Use port 28443. policySha256=abcdef."
	for i, key := range []string{"compact-one", "compact-two"} {
		content := text
		if i == 1 {
			content = "Continue with those constraints."
		}
		message, err := loop.conversation.Submit(ctx, view.ID, key, MessageInput{Content: content, ClientMessageID: key, ScopeRevision: view.ScopeRevision})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = loop.conversation.Generate(ctx, view.ID, message.MessageID); err != nil {
			t.Fatal(err)
		}
	}
	if err := loop.sessions.change(ctx, func(st *sessionState, _ time.Time) error {
		record := st.Sessions[view.ID]
		record.ContextSummary = "旧版截断摘要"
		record.ContextCutoff = 2
		st.Sessions[view.ID] = record
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := loop.conversation.Compact(ctx, view.ID, 2); err != nil {
		t.Fatal(err)
	}
	err := loop.sessions.inspect(ctx, func(st *sessionState, _ time.Time) error {
		summary := st.Sessions[view.ID].ContextSummary
		for _, value := range []string{"/srv/data", "28443", "policySha256=abcdef"} {
			if !strings.Contains(summary, value) {
				t.Errorf("lost constraint %s", value)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestContinueBudgetNeverReplaysCompletedTool(t *testing.T) {
	ctx := context.Background()
	loop, view, _, executions := agentLoopFixture(t, false, false)
	run, err := loop.Create(ctx, view.ID, AgentRunInput{Goal: "inspect", ClientRunID: "continue-run", ScopeRevision: view.ScopeRevision, MaxSteps: 1})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		run, err = loop.Advance(ctx, view.ID, run.ID, run.Revision)
		if err != nil {
			t.Fatal(err)
		}
	}
	if run.State != "limit_reached" {
		t.Fatal(run)
	}
	oldRevision := run.Revision
	run, err = loop.Continue(ctx, view.ID, run.ID, oldRevision, 8, 900)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = loop.Continue(ctx, view.ID, run.ID, oldRevision, 8, 900); err == nil {
		t.Fatal("stale continuation accepted")
	}
	run, err = loop.Advance(ctx, view.ID, run.ID, run.Revision)
	if err != nil || run.State != "completed" || *executions != 1 {
		t.Fatal("completed side effect replayed", run, err, *executions)
	}
}
func TestContinueUnknownIsRejected(t *testing.T) {
	ctx := context.Background()
	loop, view, _, _ := agentLoopFixture(t, false, true)
	run, err := loop.Create(ctx, view.ID, AgentRunInput{Goal: "inspect", ClientRunID: "unknown-continue", ScopeRevision: view.ScopeRevision})
	if err != nil {
		t.Fatal(err)
	}
	run, err = loop.Advance(ctx, view.ID, run.ID, run.Revision)
	if err != nil {
		t.Fatal(err)
	}
	run, err = loop.Advance(ctx, view.ID, run.ID, run.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = loop.Continue(ctx, view.ID, run.ID, run.Revision, 8, 900); err == nil {
		t.Fatal("unknown replay admitted")
	}
}
func TestPublicSummaryPersistedAndPreviewTentative(t *testing.T) {
	ctx := context.Background()
	loop, view, _, _ := agentLoopFixture(t, false, false)
	loop.conversation.config.Generate = func(ctx context.Context, _ GenerationRequest, emit func(string) error) error {
		if err := generationPreviewFrom(ctx)("summary", "Check current service status first."); err != nil {
			return err
		}
		if err := generationPreviewFrom(ctx)("text", "Tentative"); err != nil {
			return err
		}
		return emit("Verified final text")
	}
	message, err := loop.conversation.Submit(ctx, view.ID, "preview-one", MessageInput{Content: "inspect", ClientMessageID: "preview-one", ScopeRevision: view.ScopeRevision})
	if err != nil {
		t.Fatal(err)
	}
	var previews []string
	live := withGenerationPreview(ctx, func(kind, text string) error { previews = append(previews, kind+":"+text); return nil })
	result, err := loop.conversation.Generate(live, view.ID, message.MessageID)
	if err != nil || result.ReasoningSummary != "Check current service status first." || result.RedactedContent != "Verified final text" || len(previews) != 2 {
		t.Fatal(result, previews, err)
	}
}

func TestLateGenerationPreviewRejected(t *testing.T) {
	ctx := context.Background()
	loop, view, _, _ := agentLoopFixture(t, false, false)
	var late generationPreview
	loop.conversation.config.Generate = func(ctx context.Context, _ GenerationRequest, emit func(string) error) error {
		late = generationPreviewFrom(ctx)
		return emit("Final")
	}
	message, err := loop.conversation.Submit(ctx, view.ID, "late-preview", MessageInput{Content: "inspect", ClientMessageID: "late-preview", ScopeRevision: view.ScopeRevision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = loop.conversation.Generate(ctx, view.ID, message.MessageID); err != nil {
		t.Fatal(err)
	}
	if err = late("text", "late bytes"); err == nil {
		t.Fatal("late provider preview accepted after commit")
	}
}

func TestFrozenMemoryStopsWhenOnlyToolPermissionChanges(t *testing.T) {
	ctx := context.Background()
	loop, view, models, executions := agentLoopFixture(t, false, false)
	run, err := loop.Create(ctx, view.ID, AgentRunInput{Goal: "inspect", ClientRunID: "permission-memory", ScopeRevision: view.ScopeRevision})
	if err != nil {
		t.Fatal(err)
	}
	original := loop.sessions.authority
	loop.sessions.authority = func(ctx context.Context, owner, action string) (CurrentAuthority, error) {
		current, err := original(ctx, owner, action)
		current.AllowedTools = slices.DeleteFunc(slices.Clone(current.AllowedTools), func(s string) bool { return s == "tasks.command" })
		return current, err
	}
	run, err = loop.Advance(ctx, view.ID, run.ID, run.Revision)
	if err != nil || run.State != "failed" || run.StopReason != "memory_permissions_changed" || *models != 0 || *executions != 0 {
		t.Fatal("stale tool memory authority used", run, err)
	}
}
func TestHistoricalEvidenceExcludedAfterToolRevocation(t *testing.T) {
	ctx := context.Background()
	loop, view, _, _ := agentLoopFixture(t, false, false)
	run, err := loop.Create(ctx, view.ID, AgentRunInput{Goal: "inspect", ClientRunID: "evidence-before", ScopeRevision: view.ScopeRevision})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		run, err = loop.Advance(ctx, view.ID, run.ID, run.Revision)
		if err != nil {
			t.Fatal(err)
		}
	}
	original := loop.sessions.authority
	loop.sessions.authority = func(ctx context.Context, owner, action string) (CurrentAuthority, error) {
		current, err := original(ctx, owner, action)
		current.AllowedTools = slices.DeleteFunc(slices.Clone(current.AllowedTools), func(s string) bool { return s == "tasks.command" })
		return current, err
	}
	next, err := loop.Create(ctx, view.ID, AgentRunInput{Goal: "what next", ClientRunID: "evidence-after", ScopeRevision: view.ScopeRevision})
	if err != nil {
		t.Fatal(err)
	}
	record, err := loop.readRecord(ctx, view.ID, next.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(record.Memory, "actual-result") {
		t.Fatal("revoked tool evidence reintroduced through memory")
	}
}

func TestCompactionRejectsConcurrentNewMessage(t *testing.T) {
	ctx := context.Background()
	loop, view, _, _ := agentLoopFixture(t, false, false)
	loop.conversation.config.Generate = func(_ context.Context, _ GenerationRequest, emit func(string) error) error { return emit("ack") }
	for _, key := range []string{"before-one", "before-two"} {
		message, err := loop.conversation.Submit(ctx, view.ID, key, MessageInput{Content: "preserve original constraints", ClientMessageID: key, ScopeRevision: view.ScopeRevision})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = loop.conversation.Generate(ctx, view.ID, message.MessageID); err != nil {
			t.Fatal(err)
		}
	}
	original := loop.conversation.config.Filter
	added := false
	loop.conversation.config.Filter = func(ctx context.Context, view SessionView, role, text string) (string, error) {
		if !added && strings.Contains(text, "[用户目标与约束") {
			added = true
			if _, err := loop.conversation.Submit(ctx, view.ID, "concurrent-message", MessageInput{Content: "New constraint: never change port 18443", ClientMessageID: "concurrent-message", ScopeRevision: view.ScopeRevision}); err != nil {
				return "", err
			}
		}
		return original(ctx, view, role, text)
	}
	if _, err := loop.conversation.Compact(ctx, view.ID, 2); err != ErrScopeConflict {
		t.Fatal("concurrent compaction overwrite accepted", err)
	}
	if err := loop.sessions.inspect(ctx, func(st *sessionState, _ time.Time) error {
		if st.Sessions[view.ID].ContextCutoff != 0 {
			t.Error("failed compaction altered cutoff")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestInternalAgentMessageSummaryUsesRunBoundary(t *testing.T) {
	ctx := context.Background()
	loop, view, _, _ := agentLoopFixture(t, false, false)
	loop.conversation.config.Generate = func(ctx context.Context, _ GenerationRequest, emit func(string) error) error {
		if err := generationPreviewFrom(ctx)("summary", "internal step summary"); err != nil {
			return err
		}
		return emit(`{"text":"finished","items":[]}`)
	}
	run, err := loop.Create(ctx, view.ID, AgentRunInput{Goal: "inspect", ClientRunID: "internal-summary", ScopeRevision: view.ScopeRevision})
	if err != nil {
		t.Fatal(err)
	}
	run, err = loop.Advance(ctx, view.ID, run.ID, run.Revision)
	if err != nil || run.State != "completed" {
		t.Fatal(run, err)
	}
	messages, _, err := loop.conversation.MessagesPage(ctx, view.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) == 0 {
		t.Fatal("missing run references")
	}
	for _, message := range messages {
		if message.AgentRunID != "" && message.ReasoningSummary != "" {
			t.Fatal("internal message bypassed run filtering", message)
		}
	}
}
