package main

// 测试专用 AI 上游：本进程内的 loopback TLS "OpenAI responses" SSE 端点。
// 它只服务 fixturehost 的验收宿主：校验 Bearer 秘密、回显请求中的 model，
// 并输出 internal/ai.decodeProviderStream 可解码的确定性终态文本。
// 这不是生产组件；生产代码绝不导入本包（依赖只允许 fixturehost → 生产）。

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const fixtureAssistantText = "fixture-assistant-terminal-ok"

// upstreamState 记录验收证据。Bearer 秘密只参与比较，不存储明文日志。
type upstreamState struct {
	mu           sync.Mutex
	bearerSecret string
	requestCount int
	lastBearerOK bool
	lastModel    string
}

func (u *upstreamState) setSecret(secret string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.bearerSecret = secret
}

func (u *upstreamState) snapshot() (int, bool, string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.requestCount, u.lastBearerOK, u.lastModel
}

type upstreamRequest struct {
	Model  string `json:"model"`
	Store  *bool  `json:"store"`
	Stream *bool  `json:"stream"`
}

type sseEvent struct {
	Type           string          `json:"type"`
	SequenceNumber int64           `json:"sequence_number"`
	Delta          string          `json:"delta,omitempty"`
	Item           *sseItem        `json:"item,omitempty"`
	Response       json.RawMessage `json:"response,omitempty"`
}

type sseItem struct {
	Type string `json:"type"`
}

type responseEnvelope struct {
	ID     string           `json:"id"`
	Object string           `json:"object"`
	Status string           `json:"status"`
	Model  string           `json:"model"`
	Store  bool             `json:"store"`
	Output []responseOutput `json:"output"`
}

type responseOutput struct {
	Type    string             `json:"type"`
	Role    string             `json:"role"`
	Status  string             `json:"status"`
	Content []responseTextPart `json:"content"`
}

type responseTextPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (u *upstreamState) serveTLS(address, certFile, keyFile string) error {
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("fixturehost: load ai upstream key pair: %w", err)
	}
	listener, err := tls.Listen("tcp", address, &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
	})
	if err != nil {
		return fmt.Errorf("fixturehost: ai upstream listen: %w", err)
	}
	server := &http.Server{
		Handler:           http.HandlerFunc(u.handle),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	go func() {
		_ = server.Serve(listener)
	}()
	return nil
}

func (u *upstreamState) handle(writer http.ResponseWriter, request *http.Request) {
	u.mu.Lock()
	u.requestCount++
	expected := u.bearerSecret
	u.mu.Unlock()
	if request.Method != http.MethodPost || request.URL.Path != "/v1/responses" {
		http.Error(writer, "not found", http.StatusNotFound)
		return
	}
	if expected == "" || request.Header.Get("Authorization") != "Bearer "+expected {
		u.mu.Lock()
		u.lastBearerOK = false
		u.mu.Unlock()
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(request.Body, 2<<20))
	if err != nil || len(raw) > 2<<20 {
		u.mu.Lock()
		u.lastBearerOK = true
		u.mu.Unlock()
		http.Error(writer, "invalid body", http.StatusBadRequest)
		return
	}
	_ = request.Body.Close()
	var parsed upstreamRequest
	if json.Unmarshal(raw, &parsed) != nil || parsed.Model == "" || parsed.Store == nil || *parsed.Store || parsed.Stream == nil || !*parsed.Stream {
		u.mu.Lock()
		u.lastBearerOK = true
		u.mu.Unlock()
		http.Error(writer, "invalid request", http.StatusBadRequest)
		return
	}
	u.mu.Lock()
	u.lastBearerOK = true
	u.lastModel = parsed.Model
	u.mu.Unlock()

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusOK)
	flusher, _ := writer.(http.Flusher)

	completed := responseEnvelope{
		ID:     "resp_fixture_0001",
		Object: "response",
		Status: "completed",
		Model:  parsed.Model,
		Store:  false,
		Output: []responseOutput{{
			Type: "message", Role: "assistant", Status: "completed",
			Content: []responseTextPart{{Type: "output_text", Text: fixtureAssistantText}},
		}},
	}
	inProgress := responseEnvelope{
		ID: completed.ID, Object: "response", Status: "in_progress",
		Model: parsed.Model, Store: false, Output: []responseOutput{},
	}
	completedJSON, err := json.Marshal(completed)
	if err != nil {
		return
	}
	inProgressJSON, err := json.Marshal(inProgress)
	if err != nil {
		return
	}

	events := []sseEvent{
		{Type: "response.created", SequenceNumber: 0, Response: inProgressJSON},
		{Type: "response.in_progress", SequenceNumber: 1, Response: inProgressJSON},
		{Type: "response.output_item.added", SequenceNumber: 2, Item: &sseItem{Type: "message"}},
		{Type: "response.content_part.added", SequenceNumber: 3},
		{Type: "response.output_text.delta", SequenceNumber: 4, Delta: fixtureAssistantText},
		{Type: "response.content_part.done", SequenceNumber: 5},
		{Type: "response.output_text.done", SequenceNumber: 6},
		{Type: "response.output_item.done", SequenceNumber: 7, Item: &sseItem{Type: "message"}},
		{Type: "response.completed", SequenceNumber: 8, Response: completedJSON},
	}
	for _, event := range events {
		payload, err := json.Marshal(event)
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event.Type, payload)
		if flusher != nil {
			flusher.Flush()
		}
	}
}
