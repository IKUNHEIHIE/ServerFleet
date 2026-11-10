package certificates_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

func TestPublicRenewalCASRetainsCycleBinding(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDB(t, certificates.PublicRenewalMigrations())
	now := time.Now().UTC()
	p := certificates.PublicRenewal{ID: "policy", OwnerID: "admin", Revision: 1, Enabled: true, OrderTaskID: "issued", InstallTaskID: "installed", CSRTaskID: "csr", NodeID: "node", Domain: "example.com", KeyRef: "website", ZoneID: "zone", ProfileSHA256: strings.Repeat("a", 64), NotAfter: now.Add(80 * 24 * time.Hour), RenewBeforeDays: 30, UpdatedAt: now}
	write := func(p certificates.PublicRenewal, expected uint64) error {
		return db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
			return certificates.SavePublicRenewalTx(ctx, tx, p, expected)
		})
	}
	if err := write(p, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		return certificates.BindPublicRenewalTaskTx(ctx, tx, p.ID, 1, "renew-task")
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		return certificates.BindPublicRenewalTaskTx(ctx, tx, p.ID, 1, "different-task")
	}); err == nil {
		t.Fatal("different task replaced cycle binding")
	}
	stored, err := certificates.ReadPublicRenewal(ctx, db, p.ID)
	if err != nil || stored.TaskID != "renew-task" || stored.Revision != 1 {
		t.Fatalf("binding: %+v, %v", stored, err)
	}
	// A newer policy revokes the prior execution. The task binding is retained.
	stored.Revision = 2
	stored.Enabled = false
	if err = write(stored, 1); err != nil {
		t.Fatal(err)
	}
	if err = write(stored, 1); err == nil {
		t.Fatal("stale policy revision accepted")
	}
	if err = db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		return certificates.BindPublicRenewalTaskTx(ctx, tx, p.ID, 1, "renew-task")
	}); err == nil {
		t.Fatal("disabled/stale cycle was rebound")
	}
	items, err := certificates.ListPublicRenewals(ctx, db)
	if err != nil || len(items) != 1 || items[0].Enabled || items[0].TaskID != "renew-task" {
		t.Fatalf("list: %+v, %v", items, err)
	}
}

func TestPublicRenewalOwnerSlotAuditRollbackAndRestore(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDB(t, certificates.PublicRenewalMigrations())
	now := time.Now().UTC()
	p := certificates.PublicRenewal{ID: "policy", OwnerID: "admin", Revision: 1, Enabled: true, OrderTaskID: "issued", InstallTaskID: "installed", CSRTaskID: "csr", NodeID: "node", Domain: "example.com", KeyRef: "website", ZoneID: "zone", ProfileSHA256: strings.Repeat("a", 64), NotAfter: now.Add(80 * 24 * time.Hour), RenewBeforeDays: 30, UpdatedAt: now}
	if err := db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error { return certificates.SavePublicRenewalTx(ctx, tx, p, 0) }); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*certificates.PublicRenewal){func(p *certificates.PublicRenewal) { p.OwnerID = "other" }, func(p *certificates.PublicRenewal) { p.NodeID = "other" }, func(p *certificates.PublicRenewal) { p.KeyRef = "other" }} {
		q := p
		q.Revision = 2
		change(&q)
		if err := db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error { return certificates.SavePublicRenewalTx(ctx, tx, q, 1) }); err == nil {
			t.Fatal("owner/slot mutation accepted")
		}
	}
	q := p
	q.ID = "duplicate"
	if err := db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error { return certificates.SavePublicRenewalTx(ctx, tx, q, 0) }); err == nil {
		t.Fatal("duplicate key slot accepted")
	}
	failure := errors.New("audit commit rejected")
	q = p
	q.Revision = 2
	q.Enabled = false
	if err := db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		if e := certificates.SavePublicRenewalTx(ctx, tx, q, 1); e != nil {
			return e
		}
		return failure
	}); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	before, err := certificates.ReadPublicRenewal(ctx, db, p.ID)
	if err != nil || !before.Enabled || before.Revision != 1 {
		t.Fatalf("write escaped failed audit tx: %+v %v", before, err)
	}
	if err = db.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		return certificates.InvalidateRestoredRenewalsTx(ctx, tx, now)
	}); err != nil {
		t.Fatal(err)
	}
	after, err := certificates.ReadPublicRenewal(ctx, db, p.ID)
	if err != nil || after.Enabled || after.Revision != 2 {
		t.Fatalf("restored policy retained execution authority: %+v %v", after, err)
	}
}
