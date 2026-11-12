package server

import (
	"context"
	"path/filepath"
	"serverfleet.local/serverfleet/internal/audit"
	"testing"
	"time"
)

func TestIdentityLifecycleEventsAcceptedByProductionAuditCatalog(t *testing.T) {
	ctx := context.Background()
	s, e := New(ctx, Config{DBPath: filepath.Join(t.TempDir(), "audit.db"), Origin: "https://fleet.test"})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for _, action := range []string{"agent.identity.renew", "agent.identity.activate"} {
		if e = s.Audit.Append(ctx, audit.Event{SubjectID: "synthetic-node", Action: action, Result: "success", RequestID: "synthetic-renewal-request", NodeIDs: []string{"synthetic-node"}, Source: audit.SourceAgent, At: time.Now().UTC()}); e != nil {
			t.Fatal("production catalog rejected identity lifecycle", action, e)
		}
	}
}
