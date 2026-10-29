package audit

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

var ErrInvalidQuery = errors.New("audit: invalid query")

const (
	DefaultPageLimit = 50
	MaxPageLimit     = 200
)

// EventView is the restricted read projection. It contains only explicit event fields and
// never carries payloads, messages, details or upstream error text.
type EventView struct {
	SubjectID string
	Action    string
	Result    string
	RequestID string
	At        time.Time
}

// Query filters events by explicit fields. Empty string fields and zero times are unbounded.
// Since is inclusive and Until is exclusive. Offset and Limit page the sorted result.
type Query struct {
	SubjectID string
	Action    string
	Result    string
	RequestID string
	TaskID    string
	Source    Source
	Since     time.Time
	Until     time.Time
	Offset    int
	Limit     int
}

// Page is a restricted read result. Limit reports the resolved page size.
type Page struct {
	Items   []EventView
	Offset  int
	Limit   int
	Total   int
	HasMore bool
}

// View is an immutable snapshot for read-only queries. Records are validated by the append
// path; the view only projects already-stored events and never mutates them.
type View struct {
	events []Event
}

// NewView copies the event slice so later caller mutations cannot change the view.
func NewView(events []Event) *View {
	copied := make([]Event, len(events))
	copy(copied, events)
	return &View{events: copied}
}

// Query filters, orders newest first, and pages without modifying the underlying records.
// Equal timestamps keep their append order.
func (v *View) Query(q Query) (Page, error) {
	if q.Offset < 0 {
		return Page{}, invalidQuery("offset must not be negative")
	}
	if q.Limit < 0 {
		return Page{}, invalidQuery("limit must not be negative")
	}
	limit := q.Limit
	if limit == 0 {
		limit = DefaultPageLimit
	}
	if limit > MaxPageLimit {
		return Page{}, invalidQuery("limit exceeds maximum")
	}
	if !q.Since.IsZero() && !q.Until.IsZero() && q.Until.Before(q.Since) {
		return Page{}, invalidQuery("until must not be before since")
	}

	matched := make([]Event, 0, len(v.events))
	for _, event := range v.events {
		if matchesQuery(event, q) {
			matched = append(matched, event)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].At.After(matched[j].At)
	})

	total := len(matched)
	start := q.Offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	items := make([]EventView, 0, end-start)
	for _, event := range matched[start:end] {
		items = append(items, project(event))
	}
	return Page{
		Items:   items,
		Offset:  q.Offset,
		Limit:   limit,
		Total:   total,
		HasMore: end < total,
	}, nil
}

func matchesQuery(event Event, q Query) bool {
	if q.SubjectID != "" && event.SubjectID != q.SubjectID {
		return false
	}
	if q.Action != "" && event.Action != q.Action {
		return false
	}
	if q.Result != "" && event.Result != q.Result {
		return false
	}
	if q.RequestID != "" && event.RequestID != q.RequestID {
		return false
	}
	if q.TaskID != "" && event.TaskID != q.TaskID {
		return false
	}
	if q.Source != "" && event.Source != q.Source {
		return false
	}
	if !q.Since.IsZero() && event.At.Before(q.Since) {
		return false
	}
	if !q.Until.IsZero() && !event.At.Before(q.Until) {
		return false
	}
	return true
}

func project(event Event) EventView {
	return EventView{
		SubjectID: event.SubjectID,
		Action:    event.Action,
		Result:    event.Result,
		RequestID: event.RequestID,
		At:        event.At,
	}
}

func invalidQuery(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidQuery, reason)
}
