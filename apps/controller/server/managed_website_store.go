package server

import (
	"context"
	"time"

	"serverfleet.local/serverfleet/internal/storage"
)

func managedWebsiteMigrations() []storage.Migration {
	return []storage.Migration{{Version: "cx/0025_managed_websites", Name: "Controller website certificate inventory and encrypted keys", Statements: []string{
		`CREATE TABLE cx_managed_websites (
			domain TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL,
			zone_id TEXT NOT NULL,
			profile_sha256 TEXT NOT NULL,
			auto_renew INTEGER NOT NULL CHECK(auto_renew IN (0,1)),
			order_id TEXT NOT NULL DEFAULT '',
			generation INTEGER NOT NULL DEFAULT 0 CHECK(generation>=0),
			renew_at_ms BIGINT NOT NULL DEFAULT 0,
			issue_key TEXT NOT NULL,
			issue_source TEXT NOT NULL,
			targets_json TEXT NOT NULL DEFAULT '[]',
			delivery_key TEXT NOT NULL DEFAULT '',
			delivery_source TEXT NOT NULL DEFAULT '',
			redeploy INTEGER NOT NULL DEFAULT 0 CHECK(redeploy IN (0,1))
		)`,
		`CREATE TABLE cx_managed_website_keys (
			order_id TEXT PRIMARY KEY,
			key_id TEXT NOT NULL,
			sealed_key BLOB NOT NULL CHECK(length(sealed_key)<=4096)
		)`,
	}}}
}

type managedWebsite struct {
	Domain, Owner, ZoneID, Profile       string
	OrderID, IssueKey, IssueSource       string
	Targets, DeliveryKey, DeliverySource string
	Generation                           uint64
	RenewAtMS                            int64
	AutoRenew, Redeploy                  bool
}

const managedWebsiteColumns = `domain,owner_id,zone_id,profile_sha256,auto_renew,order_id,generation,renew_at_ms,issue_key,issue_source,targets_json,delivery_key,delivery_source,redeploy`

func scanManagedWebsite(row interface{ Scan(...any) error }) (managedWebsite, error) {
	var site managedWebsite
	var enabled, redeploy int
	err := row.Scan(&site.Domain, &site.Owner, &site.ZoneID, &site.Profile, &enabled, &site.OrderID, &site.Generation, &site.RenewAtMS, &site.IssueKey, &site.IssueSource, &site.Targets, &site.DeliveryKey, &site.DeliverySource, &redeploy)
	site.AutoRenew, site.Redeploy = enabled == 1, redeploy == 1
	return site, err
}

func (m *publicCertificateExecution) managedWebsite(ctx context.Context, domain string) (managedWebsite, error) {
	return scanManagedWebsite(m.s.DB.QueryRow(ctx, `SELECT `+managedWebsiteColumns+` FROM cx_managed_websites WHERE domain=?`, domain))
}

func (m *publicCertificateExecution) managedWebsites(ctx context.Context, owner string) ([]managedWebsite, error) {
	query := `SELECT ` + managedWebsiteColumns + ` FROM cx_managed_websites`
	var args []any
	if owner != "" {
		query += ` WHERE owner_id=?`
		args = append(args, owner)
	}
	rows, err := m.s.DB.Query(ctx, query+` ORDER BY domain`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sites := []managedWebsite{}
	for rows.Next() {
		site, err := scanManagedWebsite(rows)
		if err != nil {
			return nil, err
		}
		sites = append(sites, site)
	}
	return sites, rows.Err()
}

func managedRenewAt(notBefore, notAfter time.Time) time.Time {
	return notAfter.Add(-min(30*24*time.Hour, notAfter.Sub(notBefore)/3))
}
