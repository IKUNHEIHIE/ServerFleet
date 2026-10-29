// Package audit owns redacted append-only application events and read views.
package audit

import (
	"context"
	"time"
)

type Event struct {
	SubjectID string
	Action    string
	Result    string
	RequestID string

	// TaskID links the event to the unified task pipeline. Empty means the action
	// was not task-scoped.
	TaskID string

	// NodeIDs records the exact node snapshot involved in the action.
	NodeIDs []string

	// Source is the trusted origin of the action. The SQL recorder requires it so
	// web, AI, plugin, agent and system actions stay distinguishable; the offline
	// memory recorder tolerates the empty value for legacy tests.
	Source Source

	// ReasonCode is a short machine code for denial/failure causes. It must not
	// contain user text.
	ReasonCode string

	At time.Time
}

// Source identifies which trusted boundary produced an event.
type Source string

const (
	SourceWeb    Source = "web"
	SourceAI     Source = "ai"
	SourcePlugin Source = "plugin"
	SourceAgent  Source = "agent"
	SourceSystem Source = "system"
)

// AllowedSources returns a sorted copy of accepted source values.
func AllowedSources() []string {
	return []string{string(SourceAgent), string(SourceAI), string(SourcePlugin), string(SourceSystem), string(SourceWeb)}
}

type Recorder interface {
	Append(context.Context, Event) error
}
