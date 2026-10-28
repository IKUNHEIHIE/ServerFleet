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
	At        time.Time
}

type Recorder interface {
	Append(context.Context, Event) error
}
