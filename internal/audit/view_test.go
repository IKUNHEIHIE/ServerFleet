package audit_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
)

func sampleViewEvents() []audit.Event {
	base := auditNow.Add(-time.Hour)
	return []audit.Event{
		{SubjectID: "user-a", Action: "nodes.read", Result: "success", RequestID: "req-1", At: base.Add(time.Minute)},
		{SubjectID: "user-a", Action: "tasks.submit", Result: "success", RequestID: "req-2", At: base.Add(3 * time.Minute)},
		{SubjectID: "user-b", Action: "nodes.read", Result: "denied", RequestID: "req-1", At: base.Add(2 * time.Minute)},
		{SubjectID: "user-a", Action: "nodes.read", Result: "denied", RequestID: "req-2", At: base.Add(3 * time.Minute)},
	}
}

func summaries(items []audit.EventView) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.SubjectID+"|"+item.Action+"|"+item.Result+"|"+item.RequestID)
	}
	return result
}

func TestQueryCorrelatesAllEventsForRequestID(t *testing.T) {
	page, err := audit.NewView(sampleViewEvents()).Query(audit.Query{RequestID: "req-1"})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	want := []string{
		"user-b|nodes.read|denied|req-1",
		"user-a|nodes.read|success|req-1",
	}
	if got := summaries(page.Items); !slices.Equal(got, want) {
		t.Fatalf("request events = %v, want %v", got, want)
	}
	if page.Total != 2 {
		t.Fatalf("total = %d, want 2", page.Total)
	}
}

func TestQueryFiltersExplicitFieldsAndTimeWindow(t *testing.T) {
	base := auditNow.Add(-time.Hour)
	view := audit.NewView(sampleViewEvents())
	for _, tc := range []struct {
		name  string
		query audit.Query
		want  []string
	}{
		{
			name:  "subject",
			query: audit.Query{SubjectID: "user-a"},
			want: []string{
				"user-a|tasks.submit|success|req-2",
				"user-a|nodes.read|denied|req-2",
				"user-a|nodes.read|success|req-1",
			},
		},
		{
			name:  "action",
			query: audit.Query{Action: "nodes.read"},
			want: []string{
				"user-a|nodes.read|denied|req-2",
				"user-b|nodes.read|denied|req-1",
				"user-a|nodes.read|success|req-1",
			},
		},
		{
			name:  "result",
			query: audit.Query{Result: "denied"},
			want: []string{
				"user-a|nodes.read|denied|req-2",
				"user-b|nodes.read|denied|req-1",
			},
		},
		{
			name:  "action and result",
			query: audit.Query{Action: "nodes.read", Result: "denied"},
			want: []string{
				"user-a|nodes.read|denied|req-2",
				"user-b|nodes.read|denied|req-1",
			},
		},
		{
			name:  "since is inclusive",
			query: audit.Query{Since: base.Add(2 * time.Minute)},
			want: []string{
				"user-a|tasks.submit|success|req-2",
				"user-a|nodes.read|denied|req-2",
				"user-b|nodes.read|denied|req-1",
			},
		},
		{
			name:  "until is exclusive",
			query: audit.Query{Until: base.Add(2 * time.Minute)},
			want:  []string{"user-a|nodes.read|success|req-1"},
		},
		{
			name:  "bounded window",
			query: audit.Query{Since: base.Add(2 * time.Minute), Until: base.Add(3 * time.Minute)},
			want:  []string{"user-b|nodes.read|denied|req-1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := view.Query(tc.query)
			if err != nil {
				t.Fatalf("Query returned error: %v", err)
			}
			if got := summaries(page.Items); !slices.Equal(got, tc.want) {
				t.Fatalf("items = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestQueryOrdersNewestFirstAndKeepsTieOrder(t *testing.T) {
	page, err := audit.NewView(sampleViewEvents()).Query(audit.Query{})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	want := []string{
		"user-a|tasks.submit|success|req-2",
		"user-a|nodes.read|denied|req-2",
		"user-b|nodes.read|denied|req-1",
		"user-a|nodes.read|success|req-1",
	}
	if got := summaries(page.Items); !slices.Equal(got, want) {
		t.Fatalf("ordered items = %v, want %v", got, want)
	}
}

func TestQueryPaginationBounds(t *testing.T) {
	view := audit.NewView(sampleViewEvents())
	for _, tc := range []struct {
		name      string
		query     audit.Query
		want      []string
		wantLimit int
		wantTotal int
		wantMore  bool
	}{
		{
			name:      "first page",
			query:     audit.Query{Limit: 2},
			want:      []string{"user-a|tasks.submit|success|req-2", "user-a|nodes.read|denied|req-2"},
			wantLimit: 2,
			wantTotal: 4,
			wantMore:  true,
		},
		{
			name:      "second page",
			query:     audit.Query{Offset: 2, Limit: 2},
			want:      []string{"user-b|nodes.read|denied|req-1", "user-a|nodes.read|success|req-1"},
			wantLimit: 2,
			wantTotal: 4,
			wantMore:  false,
		},
		{
			name:      "offset at end",
			query:     audit.Query{Offset: 4, Limit: 2},
			want:      []string{},
			wantLimit: 2,
			wantTotal: 4,
			wantMore:  false,
		},
		{
			name:      "offset beyond end",
			query:     audit.Query{Offset: 100, Limit: 2},
			want:      []string{},
			wantLimit: 2,
			wantTotal: 4,
			wantMore:  false,
		},
		{
			name:      "default limit",
			query:     audit.Query{},
			wantLimit: audit.DefaultPageLimit,
			wantTotal: 4,
			wantMore:  false,
			want: []string{
				"user-a|tasks.submit|success|req-2",
				"user-a|nodes.read|denied|req-2",
				"user-b|nodes.read|denied|req-1",
				"user-a|nodes.read|success|req-1",
			},
		},
		{
			name:      "maximum limit",
			query:     audit.Query{Limit: audit.MaxPageLimit},
			wantLimit: audit.MaxPageLimit,
			wantTotal: 4,
			wantMore:  false,
			want: []string{
				"user-a|tasks.submit|success|req-2",
				"user-a|nodes.read|denied|req-2",
				"user-b|nodes.read|denied|req-1",
				"user-a|nodes.read|success|req-1",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := view.Query(tc.query)
			if err != nil {
				t.Fatalf("Query returned error: %v", err)
			}
			if got := summaries(page.Items); !slices.Equal(got, tc.want) {
				t.Fatalf("items = %v, want %v", got, tc.want)
			}
			if page.Limit != tc.wantLimit || page.Total != tc.wantTotal || page.HasMore != tc.wantMore {
				t.Fatalf("limit/total/hasMore = %d/%d/%t, want %d/%d/%t",
					page.Limit, page.Total, page.HasMore, tc.wantLimit, tc.wantTotal, tc.wantMore)
			}
		})
	}
}

func TestQueryRejectsInvalidBounds(t *testing.T) {
	view := audit.NewView(sampleViewEvents())
	for _, tc := range []struct {
		name  string
		query audit.Query
	}{
		{name: "negative offset", query: audit.Query{Offset: -1}},
		{name: "negative limit", query: audit.Query{Limit: -1}},
		{name: "limit over maximum", query: audit.Query{Limit: audit.MaxPageLimit + 1}},
		{
			name:  "reversed time window",
			query: audit.Query{Since: auditNow, Until: auditNow.Add(-time.Minute)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := view.Query(tc.query); !errors.Is(err, audit.ErrInvalidQuery) {
				t.Fatalf("error = %v, want ErrInvalidQuery", err)
			}
		})
	}
}

func TestQueryDoesNotModifyRecords(t *testing.T) {
	events := sampleViewEvents()
	view := audit.NewView(events)
	before := mustAuditJSON(t, events)
	if _, err := view.Query(audit.Query{RequestID: "req-1"}); err != nil {
		t.Fatalf("first query error: %v", err)
	}
	if after := mustAuditJSON(t, events); string(before) != string(after) {
		t.Fatal("query mutated the source events")
	}

	page, err := view.Query(audit.Query{RequestID: "req-1"})
	if err != nil {
		t.Fatal(err)
	}
	page.Items[0].Action = "tampered"
	page.Items[0].RequestID = "tampered"
	again, err := view.Query(audit.Query{RequestID: "req-1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"user-b|nodes.read|denied|req-1", "user-a|nodes.read|success|req-1"}
	if got := summaries(again.Items); !slices.Equal(got, want) {
		t.Fatalf("items after projection mutation = %v, want %v", got, want)
	}

	events[0].Action = "changed-after-view"
	again, err = view.Query(audit.Query{RequestID: "req-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := summaries(again.Items); !slices.Equal(got, want) {
		t.Fatalf("view was not isolated from source mutation: %v", got)
	}
}

func TestEventViewExposesOnlyAllowedFields(t *testing.T) {
	allowed := map[string]bool{
		"SubjectID": true,
		"Action":    true,
		"Result":    true,
		"RequestID": true,
		"At":        true,
	}
	typ := reflect.TypeOf(audit.EventView{})
	if typ.NumField() != len(allowed) {
		t.Fatalf("EventView has %d fields, want %d", typ.NumField(), len(allowed))
	}
	for i := 0; i < typ.NumField(); i++ {
		if name := typ.Field(i).Name; !allowed[name] {
			t.Fatalf("EventView exposes unexpected field %q", name)
		}
	}
}

func TestQueryOnEmptyView(t *testing.T) {
	page, err := audit.NewView(nil).Query(audit.Query{})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if page.Items == nil {
		t.Fatal("items must be a non-nil empty slice")
	}
	if len(page.Items) != 0 || page.Total != 0 || page.HasMore || page.Limit != audit.DefaultPageLimit {
		t.Fatalf("unexpected empty page: %+v", page)
	}
}

func mustAuditJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}
