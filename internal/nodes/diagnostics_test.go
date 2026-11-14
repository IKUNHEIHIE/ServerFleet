package nodes_test

import (
	"context"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/nodes"
	"testing"
	"time"
)

func TestConnectionDiagnosticsAreBoundedPersistentAndResourceScoped(t *testing.T) {
	ctx := context.Background()
	h := newServiceFixture(t, authorizerFunc(func(context.Context, auth.Check) error { return nil }), readPolicy("reader", []string{"n1", "n2"}, []string{"n1"}), nodes.ServiceActions()...)
	seedNode(t, h.db, h.repo, "n1", "Visible")
	seedNode(t, h.db, h.repo, "n2", "Hidden")
	at := time.Now().UTC()
	o := nodes.ConnectionObservation{AgentVersion: "0.9.1-test", Capabilities: []string{"probe.service.v1"}, ConnectionID: "connection-one", ObservedAt: at, CertificateExpiresAt: at.Add(12 * time.Hour)}
	if e := h.repo.RecordConnection(ctx, "n1", o); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 110; i++ {
		if e := h.repo.RecordDisconnect(ctx, "n1", at.Add(time.Duration(i)*time.Second)); e != nil {
			t.Fatal(e)
		}
	}
	fresh, _ := nodes.NewRepo(h.db)
	record, e := fresh.ConnectionDiagnostics(ctx, "n1")
	if e != nil || !record.Recorded || record.AgentVersion != o.AgentVersion || len(record.History) != 100 {
		t.Fatal(record, e)
	}
	if _, e = h.svc.ConnectionDiagnostics(ctx, "reader", "n2"); e == nil {
		t.Fatal("diagnostics leaked hidden node")
	}
}

func TestRecordRejectionIsQueryableAndValidated(t *testing.T) {
	ctx := context.Background()
	h := newServiceFixture(t, authorizerFunc(func(context.Context, auth.Check) error { return nil }), readPolicy("reader", []string{"n1"}, []string{"n1"}), nodes.ServiceActions()...)
	seedNode(t, h.db, h.repo, "n1", "Visible")
	at := time.Now().UTC()
	if e := h.repo.RecordRejection(ctx, "n1", at, "INVALID_PROTOCOL"); e != nil {
		t.Fatal(e)
	}
	record, e := h.repo.ConnectionDiagnostics(ctx, "n1")
	if e != nil {
		t.Fatal(e)
	}
	if record.Recorded {
		t.Fatal("rejection must not fabricate an authenticated observation")
	}
	if len(record.History) != 1 || record.History[0].Code != "rejected:INVALID_PROTOCOL" {
		t.Fatalf("rejection not queryable: %+v", record.History)
	}
	if _, e = h.svc.ConnectionDiagnostics(ctx, "reader", "n1"); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []struct {
		node, code string
		at         time.Time
	}{
		{"", "INVALID_PROTOCOL", at},
		{"n1", "", at},
		{"n1", "bad code", at},
		{"n1", "INVALID_PROTOCOL", time.Time{}},
	} {
		if e := h.repo.RecordRejection(ctx, bad.node, bad.at, bad.code); e == nil {
			t.Fatalf("accepted invalid rejection %+v", bad)
		}
	}
}
