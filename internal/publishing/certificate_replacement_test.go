package publishing_test

import (
	"context"
	"errors"
	"serverfleet.local/serverfleet/internal/publishing"
	"serverfleet.local/serverfleet/internal/storage"
	"testing"
)

func TestCertificateSuccessorPreservesRoutesDNSAndStopsDestructiveRollback(t *testing.T) {
	f := newPublicationFixture(t)
	ctx := context.Background()
	for _, m := range publishing.CertificateReplacementMigrations() {
		if err := f.db.InTx(ctx, func(c context.Context, tx storage.Tx) error {
			for _, q := range m.Statements {
				if _, e := tx.Exec(c, q); e != nil {
					return e
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	base, e := f.service.Create(ctx, "admin", "req-base", publishing.CreateInput{ID: "base-https", Name: "Site", Domain: "shop.example.com", ServiceGroupID: "sg1", ExpectedGroupRevision: 3, GatewayIDs: []string{"gw-1"}, ZoneID: "zone-1", CertificateID: "cert-1", HTTPS: true, TargetRevision: 2})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.Run(ctx, "admin", "run-base", base.ID); e != nil {
		t.Fatal(e)
	}
	// Both nodes may have captured this binding before either installs.
	frozenOtherNodeBase := base.ID
	next, e := f.service.ReplaceCertificate(ctx, "admin", "rotate-1", "successor-1", base.ID, "cert-1", "cert-2")
	if e != nil {
		t.Fatal(e)
	}
	if next.TargetRevision != 3 || next.CertificateID != "cert-2" || next.Domain != base.Domain || next.ZoneID != base.ZoneID {
		t.Fatalf("successor lost target binding: %+v", next)
	}
	root, e := publishing.CertificateRoutePlan(ctx, f.db, next.ID)
	if e != nil || root != base.ID {
		t.Fatalf("route identity %q: %v", root, e)
	}
	_, stages := f.plan(t, next.ID)
	if stages[0].Status != publishing.StageSucceeded || stages[0].Attempts != 1 {
		t.Fatal("DNS would be repeated", stages)
	}
	if _, e = f.service.LatestCertificatePlan(ctx, "admin", base.ID); !errors.Is(e, publishing.ErrStageAwaitingTask) {
		t.Fatal("pending successor must serialize later node rotation", e)
	}
	same, e := f.service.ReplaceCertificate(ctx, "admin", "rediscover", "successor-1", base.ID, "cert-1", "cert-2")
	if e != nil || same.ID != next.ID {
		t.Fatal("lost idempotency", e)
	}
	if _, e = f.service.ReplaceCertificate(ctx, "admin", "duplicate", "sibling", base.ID, "cert-1", "cert-3"); e == nil {
		t.Fatal("concurrent successor accepted")
	}
	for _, id := range []string{base.ID, next.ID} {
		if _, e = f.service.RollbackAll(ctx, "admin", "rollback", id); !errors.Is(e, publishing.ErrStageNotRollable) {
			t.Fatalf("destructive rollback %s: %v", id, e)
		}
	}
	if _, e = f.service.Run(ctx, "admin", "run-successor", next.ID); e != nil {
		t.Fatal(e)
	}
	latest, e := f.service.LatestCertificatePlan(ctx, "admin", frozenOtherNodeBase)
	if e != nil || latest.ID != next.ID {
		t.Fatal("did not follow immutable completed chain", e)
	}
	if _, e = f.service.ReplaceCertificate(ctx, "admin", "rotate-2", "successor-2", latest.ID, latest.CertificateID, "cert-3"); e != nil {
		t.Fatal(e)
	}
	root, e = publishing.CertificateRoutePlan(ctx, f.db, "successor-2")
	if e != nil || root != base.ID {
		t.Fatal("later rotation changed live route", e)
	}
}
