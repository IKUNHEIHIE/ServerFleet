package ai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var ErrModelUnavailable = errors.New("ai: model unavailable or disabled")
var ErrGenerationUnknown = errors.New("ai: generation outcome unknown; no automatic retry")

type MessageInput struct {
	Content         string `json:"content"`
	ClientMessageID string `json:"clientMessageId"`
	ScopeRevision   uint64 `json:"scopeRevision"`
}
type Message struct {
	MessageID       string    `json:"messageId"`
	SessionID       string    `json:"sessionId"`
	Role            string    `json:"role"`
	RedactedContent string    `json:"redactedContent"`
	CreatedAt       time.Time `json:"createdAt"`
	TaskIDs         []string  `json:"taskIds"`
}
type MessageEvent struct {
	SessionID string    `json:"sessionId"`
	Sequence  uint64    `json:"sequence"`
	Kind      string    `json:"kind"`
	MessageID string    `json:"messageId"`
	State     string    `json:"state"`
	At        time.Time `json:"at"`
}
type ModelProfile struct {
	ID, Model, CredentialRef string
	Revision                 uint64
}
type GenerationRequest struct {
	SubjectID, SessionID, MessageID, OperationID string
	ScopeRevision                                uint64
	Profile                                      ModelProfile
	Messages                                     []Message
}
type ConversationConfig struct {
	// Profile must read CURRENT owner configuration and reject disabled/missing
	// BYOK. CredentialRef is metadata; plaintext keys never enter this API.
	Profile func(context.Context, string) (ModelProfile, error)
	// Filter MUST apply the current scope/secret policy, both before persistence
	// and before provider/reader disclosure. There is no default pass-through.
	Filter func(context.Context, SessionView, string, string) (string, error)
	// Generate is a program-registered provider bridge. It borrows BYOK via the
	// credential service using OperationID, and honors ctx. Output is text only;
	// it cannot supply authoritative task IDs or run tools. Tool execution must
	// separately use AssessTool and the sole task/approval/audit path.
	Generate func(context.Context, GenerationRequest, func(string) error) error
}
type ConversationService struct {
	sessions *SessionService
	config   ConversationConfig
}
type generationRecord struct {
	ClientID, IdempotencyKey, InputSHA256 string
	ScopeRevision                         uint64
	Profile                               ModelProfile
	State, ResponseID                     string
}
type conversationRecord struct {
	Messages []Message
	Runs     map[string]generationRecord
	Events   []MessageEvent
}

func NewConversationService(sessions *SessionService, cfg ConversationConfig) (*ConversationService, error) {
	if sessions == nil || cfg.Profile == nil || cfg.Filter == nil || cfg.Generate == nil {
		return nil, ErrModelUnavailable
	}
	return &ConversationService{sessions, cfg}, nil
}
func validMessageContent(text string) bool {
	return utf8.ValidString(text) && utf8.RuneCountInString(text) <= 32000 && !strings.ContainsRune(text, '\x00') && strings.TrimSpace(text) != ""
}
func validModelProfile(p ModelProfile) bool {
	return exactID(p.ID) && exactID(p.Model) && exactID(p.CredentialRef) && p.Revision > 0
}
func validateConversation(id string, c *conversationRecord) error {
	if c == nil {
		return nil
	}
	if c.Runs == nil || len(c.Messages) == 0 || len(c.Events) == 0 || len(c.Messages) > 512 || len(c.Runs) > 256 || len(c.Events) > 1024 {
		return ErrSessionUnavailable
	}
	messages := map[string]Message{}
	for _, m := range c.Messages {
		if !exactID(m.MessageID) || m.SessionID != id || !validMessageContent(m.RedactedContent) || m.CreatedAt.IsZero() || len(m.TaskIDs) != 0 || m.Role != "user" && m.Role != "assistant" || messages[m.MessageID].MessageID != "" {
			return ErrSessionUnavailable
		}
		messages[m.MessageID] = m
	}
	clients, keys, responses := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for messageID, run := range c.Runs {
		digest, err := hex.DecodeString(run.InputSHA256)
		if messages[messageID].Role != "user" || !exactID(run.ClientID) || !exactID(run.IdempotencyKey) || clients[run.ClientID] || keys[run.IdempotencyKey] || run.ScopeRevision == 0 || !validModelProfile(run.Profile) || err != nil || len(digest) != 32 || hex.EncodeToString(digest) != run.InputSHA256 {
			return ErrSessionUnavailable
		}
		if messageHash(MessageInput{ClientMessageID: run.ClientID, ScopeRevision: run.ScopeRevision}, run.Profile, messages[messageID].RedactedContent) != run.InputSHA256 {
			return ErrSessionUnavailable
		}
		clients[run.ClientID], keys[run.IdempotencyKey] = true, true
		switch run.State {
		case "queued", "generating", "unknown":
			if run.ResponseID != "" {
				return ErrSessionUnavailable
			}
		case "succeeded":
			if messages[run.ResponseID].Role != "assistant" || responses[run.ResponseID] {
				return ErrSessionUnavailable
			}
			responses[run.ResponseID] = true
		default:
			return ErrSessionUnavailable
		}
	}
	for _, m := range c.Messages {
		if m.Role == "user" && c.Runs[m.MessageID].ClientID == "" || m.Role == "assistant" && !responses[m.MessageID] {
			return ErrSessionUnavailable
		}
	}
	for i, event := range c.Events {
		if event.SessionID != id || event.Sequence != uint64(i+1) || messages[event.MessageID].MessageID == "" || event.At.IsZero() || event.Kind != "ai.message.created" && event.Kind != "ai.generation.state" {
			return ErrSessionUnavailable
		}
		if !slices.Contains([]string{"queued", "generating", "succeeded", "unknown"}, event.State) {
			return ErrSessionUnavailable
		}
	}
	return nil
}
func messageHash(input MessageInput, profile ModelProfile, sanitized string) string {
	encoded, _ := json.Marshal(struct {
		Content, ClientID string
		Scope             uint64
		Profile           ModelProfile
	}{sanitized, input.ClientMessageID, input.ScopeRevision, profile})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
func appendMessageEvent(c *conversationRecord, id, kind, messageID, state string, now time.Time) error {
	if len(c.Events) >= 1024 {
		return ErrSessionUnavailable
	}
	c.Events = append(c.Events, MessageEvent{id, uint64(len(c.Events) + 1), kind, messageID, state, now})
	return nil
}
func (c *ConversationService) audit(st *sessionState, record sessionRecord, action, messageID string, now time.Time) error {
	if err := c.sessions.audit(st, record, action, now); err != nil {
		return err
	}
	event := &st.Outbox[len(st.Outbox)-1]
	event.MessageID = messageID
	if action == "ai.messages.unknown" {
		event.Result = "unknown"
	}
	return nil
}
func (c *ConversationService) profile(ctx context.Context, owner string) (out ModelProfile, result error) {
	defer func() {
		if recover() != nil {
			out, result = ModelProfile{}, ErrModelUnavailable
		}
	}()
	p, err := c.config.Profile(ctx, owner)
	if err != nil || !validModelProfile(p) || ctx.Err() != nil {
		return ModelProfile{}, ErrModelUnavailable
	}
	return p, nil
}
func (c *ConversationService) filter(ctx context.Context, view SessionView, role, text string) (out string, result error) {
	defer func() {
		if recover() != nil {
			out, result = "", ErrToolDenied
		}
	}()
	out, err := c.config.Filter(ctx, view, role, text)
	if err != nil || !validMessageContent(out) || ctx.Err() != nil {
		return "", ErrToolDenied
	}
	return out, nil
}

// Submit persists an accepted user message and queued inference, not a pretend
// model answer. Generate must be explicitly invoked; no second task executor or
// ambient background goroutine is started by this service.
func (c *ConversationService) Submit(ctx context.Context, id, idempotencyKey string, input MessageInput) (Message, error) {
	if c == nil || !exactID(idempotencyKey) || !exactID(input.ClientMessageID) || input.ScopeRevision == 0 || !validMessageContent(input.Content) {
		return Message{}, ErrToolDenied
	}
	actor, err := c.sessions.actor(ctx, "ai.messages.create", id, true)
	if err != nil {
		return Message{}, err
	}
	view, err := c.sessions.Get(ctx, id)
	if err != nil {
		return Message{}, err
	}
	if view.ScopeRevision != input.ScopeRevision || view.Status != "active" {
		return Message{}, ErrScopeConflict
	}
	profile, err := c.profile(ctx, actor.SubjectID)
	if err != nil {
		return Message{}, err
	}
	text, err := c.filter(ctx, view, "user", input.Content)
	if err != nil {
		return Message{}, err
	}
	hash := messageHash(input, profile, text)
	var out Message
	err = c.sessions.change(ctx, func(st *sessionState, now time.Time) error {
		if _, err := c.sessions.recheck(ctx, actor, "ai.messages.create", id, true); err != nil {
			return err
		}
		r, exists := st.Sessions[id]
		if !exists || r.Closed || r.Scope.SubjectID != actor.SubjectID {
			return ErrToolDenied
		}
		if r.Scope.Revision != input.ScopeRevision {
			return ErrScopeConflict
		}
		if r.Conversation == nil {
			r.Conversation = &conversationRecord{Runs: map[string]generationRecord{}}
		}
		conversation := r.Conversation
		for messageID, run := range conversation.Runs {
			if run.ClientID == input.ClientMessageID || run.IdempotencyKey == idempotencyKey {
				if run.ClientID != input.ClientMessageID || run.IdempotencyKey != idempotencyKey || run.InputSHA256 != hash {
					return ErrScopeConflict
				}
				for _, message := range conversation.Messages {
					if message.MessageID == messageID {
						out = message
						return nil
					}
				}
				return ErrSessionUnavailable
			}
		}
		if len(conversation.Runs) >= 256 || len(conversation.Messages) >= 511 {
			return ErrSessionUnavailable
		}
		messageID, err := sessionID()
		if err != nil {
			return err
		}
		out = Message{messageID, id, "user", text, now, []string{}}
		conversation.Messages = append(conversation.Messages, out)
		conversation.Runs[messageID] = generationRecord{ClientID: input.ClientMessageID, IdempotencyKey: idempotencyKey, InputSHA256: hash, ScopeRevision: input.ScopeRevision, Profile: profile, State: "queued"}
		if appendMessageEvent(conversation, id, "ai.message.created", messageID, "queued", now) != nil {
			return ErrSessionUnavailable
		}
		st.Sessions[id] = r
		return c.audit(st, r, "ai.messages.create", messageID, now)
	})
	if err != nil {
		return Message{}, err
	}
	return out, nil
}

// Events contains committed metadata only. Consumers reauthenticate each poll
// and before SSE flush. Cursor is per session; retention overflow is rejected.
func (c *ConversationService) Events(ctx context.Context, id string, after uint64, limit int) ([]MessageEvent, error) {
	if c == nil || limit < 1 || limit > 100 {
		return nil, ErrToolDenied
	}
	actor, err := c.sessions.actor(ctx, "ai.events.read", id, false)
	if err != nil {
		return nil, err
	}
	out := []MessageEvent{}
	err = c.sessions.change(ctx, func(st *sessionState, _ time.Time) error {
		if _, err := c.sessions.recheck(ctx, actor, "ai.events.read", id, false); err != nil {
			return err
		}
		r, exists := st.Sessions[id]
		if !exists || r.Scope.SubjectID != actor.SubjectID {
			return ErrToolDenied
		}
		length := 0
		if r.Conversation != nil {
			length = len(r.Conversation.Events)
		}
		if after > uint64(length) {
			return ErrScopeConflict
		}
		if length > 0 {
			end := min(length, int(after)+limit)
			out = slices.Clone(r.Conversation.Events[int(after):end])
		}
		return nil
	})
	return out, err
}

func (c *ConversationService) Messages(ctx context.Context, id string, after int, limit int) ([]Message, error) {
	if c == nil || after < 0 || limit < 1 || limit > 100 {
		return nil, ErrToolDenied
	}
	actor, err := c.sessions.actor(ctx, "ai.messages.read", id, false)
	if err != nil {
		return nil, err
	}
	view, err := c.sessions.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	out := []Message{}
	err = c.sessions.change(ctx, func(st *sessionState, _ time.Time) error {
		if _, err := c.sessions.recheck(ctx, actor, "ai.messages.read", id, false); err != nil {
			return err
		}
		r, exists := st.Sessions[id]
		if !exists || r.Scope.SubjectID != actor.SubjectID {
			return ErrToolDenied
		}
		length := 0
		if r.Conversation != nil {
			length = len(r.Conversation.Messages)
		}
		if after > length {
			return ErrScopeConflict
		}
		if length > 0 {
			out = slices.Clone(r.Conversation.Messages[after:min(length, after+limit)])
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for i, m := range out {
		out[i].RedactedContent, err = c.filter(ctx, view, m.Role, m.RedactedContent)
		out[i].TaskIDs = []string{}
		if err != nil {
			return nil, err
		}
	}
	fresh, err := c.sessions.Get(ctx, id)
	if err != nil || fresh.ScopeRevision != view.ScopeRevision || !slices.Equal(fresh.AllowedNodeIDs, view.AllowedNodeIDs) {
		return nil, ErrToolDenied
	}
	return out, nil
}

func (c *ConversationService) markUnknown(ctx context.Context, id, messageID string) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = c.sessions.change(cleanup, func(st *sessionState, now time.Time) error {
		r, exists := st.Sessions[id]
		if !exists || r.Conversation == nil {
			return ErrSessionUnavailable
		}
		run, exists := r.Conversation.Runs[messageID]
		if !exists || run.State != "generating" {
			return nil
		}
		run.State = "unknown"
		r.Conversation.Runs[messageID] = run
		if appendMessageEvent(r.Conversation, id, "ai.generation.state", messageID, "unknown", now) != nil {
			return ErrSessionUnavailable
		}
		st.Sessions[id] = r
		return c.audit(st, r, "ai.messages.unknown", messageID, now)
	})
}

// Generate durably fences the inference before contacting the provider. A
// crash/error/timeout/revocation never replays a generating/unknown request.
// Invocation after restart needs a fresh explicit user message, not an auto retry.
func (c *ConversationService) Generate(ctx context.Context, id, messageID string) (out Message, result error) {
	if c == nil || !exactID(messageID) {
		return Message{}, ErrToolDenied
	}
	actor, err := c.sessions.actor(ctx, "ai.messages.generate", id, false)
	if err != nil {
		return Message{}, err
	}
	view, err := c.sessions.Get(ctx, id)
	if err != nil || view.Status != "active" {
		return Message{}, ErrToolDenied
	}
	profile, err := c.profile(ctx, actor.SubjectID)
	if err != nil {
		return Message{}, err
	}
	request := GenerationRequest{SubjectID: actor.SubjectID, SessionID: id, MessageID: messageID, OperationID: "ai-generation-" + messageID, ScopeRevision: view.ScopeRevision, Profile: profile}
	reserved := false
	defer func() {
		if recover() != nil {
			out, result = Message{}, ErrGenerationUnknown
		}
		if reserved && result != nil {
			c.markUnknown(ctx, id, messageID)
		}
	}()
	err = c.sessions.change(ctx, func(st *sessionState, now time.Time) error {
		if _, err := c.sessions.recheck(ctx, actor, "ai.messages.generate", id, false); err != nil {
			return err
		}
		r, exists := st.Sessions[id]
		if !exists || r.Closed || r.Scope.SubjectID != actor.SubjectID || r.Conversation == nil {
			return ErrToolDenied
		}
		run, exists := r.Conversation.Runs[messageID]
		if !exists {
			return ErrToolDenied
		}
		if run.State == "generating" || run.State == "unknown" {
			return ErrGenerationUnknown
		}
		if run.State != "queued" {
			return ErrScopeConflict
		}
		if r.Scope.Revision != view.ScopeRevision || run.ScopeRevision != view.ScopeRevision || run.Profile != profile {
			return ErrScopeConflict
		}
		for _, message := range r.Conversation.Messages {
			if message.Role != "user" {
				continue
			}
			if message.MessageID == messageID {
				request.Messages = append(request.Messages, message)
				break
			}
			prior := r.Conversation.Runs[message.MessageID]
			if prior.State == "generating" || prior.State == "queued" {
				return ErrScopeConflict
			}
			if prior.State == "succeeded" {
				request.Messages = append(request.Messages, message)
				for _, response := range r.Conversation.Messages {
					if response.MessageID == prior.ResponseID {
						request.Messages = append(request.Messages, response)
						break
					}
				}
			}
		}
		historyBytes := 0
		for _, message := range request.Messages {
			historyBytes += len(message.RedactedContent)
		}
		if len(request.Messages) > 64 || historyBytes > 256000 {
			return ErrSessionUnavailable // No silent context truncation or call.
		}
		if len(r.Conversation.Events) > 1021 || len(r.Conversation.Messages) >= 512 {
			return ErrSessionUnavailable
		}
		run.State = "generating"
		r.Conversation.Runs[messageID] = run
		if appendMessageEvent(r.Conversation, id, "ai.generation.state", messageID, "generating", now) != nil {
			return ErrSessionUnavailable
		}
		st.Sessions[id] = r
		return c.audit(st, r, "ai.messages.generate", messageID, now)
	})
	if err != nil {
		return Message{}, err
	}
	reserved = true
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for i, message := range request.Messages {
		request.Messages[i].RedactedContent, err = c.filter(bounded, view, message.Role, message.RedactedContent)
		request.Messages[i].TaskIDs = []string{}
		if err != nil {
			return Message{}, ErrGenerationUnknown
		}
	}
	var output bytes.Buffer
	check := func() error {
		if _, err := c.sessions.recheck(bounded, actor, "ai.messages.generate", id, false); err != nil {
			return err
		}
		fresh, err := c.sessions.Get(bounded, id)
		if err != nil || fresh.ScopeRevision != view.ScopeRevision || fresh.Status != "active" || !slices.Equal(fresh.AllowedNodeIDs, view.AllowedNodeIDs) {
			return ErrToolDenied
		}
		p, err := c.profile(bounded, actor.SubjectID)
		if err != nil || p != profile {
			return ErrModelUnavailable
		}
		return nil
	}
	if check() != nil {
		return Message{}, ErrGenerationUnknown
	}
	providerContext, closeObservation, err := observeGeneration(bounded, request, view.AllowedNodeIDs)
	if err != nil {
		return Message{}, ErrGenerationUnknown
	}
	defer closeObservation()
	var emitMu sync.Mutex
	finished, emitFailed, chunks := false, false, 0
	defer func() { emitMu.Lock(); finished = true; emitMu.Unlock() }()
	providerErr := c.config.Generate(providerContext, request, func(chunk string) error {
		emitMu.Lock()
		defer emitMu.Unlock()
		chunks++
		if finished {
			return ErrToolDenied
		}
		if emitFailed || chunks > 1024 || check() != nil || !utf8.ValidString(chunk) || output.Len()+len(chunk) > 128000 {
			emitFailed = true
			return ErrToolDenied
		}
		_, err := output.WriteString(chunk)
		return err
	})
	closeObservation()
	emitMu.Lock()
	finished = true
	outputText := output.String()
	outputRejected := emitFailed
	emitMu.Unlock()
	if providerErr != nil || outputRejected || bounded.Err() != nil || check() != nil {
		return Message{}, ErrGenerationUnknown
	}
	current, err := c.sessions.Get(bounded, id)
	if err != nil {
		return Message{}, ErrGenerationUnknown
	}
	text, err := c.filter(bounded, current, "assistant", outputText)
	if err != nil {
		return Message{}, ErrGenerationUnknown
	}
	err = c.sessions.change(bounded, func(st *sessionState, now time.Time) error {
		if _, err := c.sessions.recheck(bounded, actor, "ai.messages.generate", id, false); err != nil {
			return err
		}
		r := st.Sessions[id]
		if r.Closed || r.Scope.SubjectID != actor.SubjectID || r.Scope.Revision != view.ScopeRevision || r.Conversation == nil {
			return ErrToolDenied
		}
		run := r.Conversation.Runs[messageID]
		if run.State != "generating" || run.Profile != profile {
			return ErrScopeConflict
		}
		responseID, err := sessionID()
		if err != nil {
			return err
		}
		out = Message{responseID, id, "assistant", text, now, []string{}}
		r.Conversation.Messages = append(r.Conversation.Messages, out)
		run.State, run.ResponseID = "succeeded", responseID
		r.Conversation.Runs[messageID] = run
		if appendMessageEvent(r.Conversation, id, "ai.message.created", responseID, "succeeded", now) != nil || appendMessageEvent(r.Conversation, id, "ai.generation.state", messageID, "succeeded", now) != nil {
			return ErrSessionUnavailable
		}
		st.Sessions[id] = r
		return c.audit(st, r, "ai.messages.completed", messageID, now)
	})
	if err != nil {
		return Message{}, ErrGenerationUnknown
	}
	return out, nil
}
