package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/dns"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

const actionManagedWebsiteIssue = "certificates.website.issue"

type managedWebsiteWorker struct{ manager *publicCertificateExecution }

func (m *publicCertificateExecution) managedIssueParams(site managedWebsite) map[string]string {
	return map[string]string{"domain": site.Domain, "zoneId": site.ZoneID, "profileSha256": site.Profile, "baseOrderId": site.OrderID}
}

func (m *publicCertificateExecution) managedIssueSite(ctx context.Context, task tasks.Task) (managedWebsite, error) {
	var p map[string]string
	if task.TargetType != tasks.TargetTypeController || task.TargetResourceID != m.authorityID || task.OperationClass != tasks.OperationClassModifying || json.Unmarshal([]byte(task.Params), &p) != nil {
		return managedWebsite{}, certificates.ErrACMEConfig
	}
	site, err := m.managedWebsite(ctx, p["domain"])
	if err != nil || site.Owner != task.SubmitterID || site.IssueKey != task.IdempotencyKey || site.IssueSource != task.Source || site.Profile != m.profileSHA {
		return managedWebsite{}, certificates.ErrACMEConfig
	}
	expected, _ := json.Marshal(m.managedIssueParams(site))
	if string(expected) != task.Params {
		return managedWebsite{}, certificates.ErrACMEConfig
	}
	return site, nil
}

func (m *publicCertificateExecution) submitManagedIssue(ctx context.Context, site managedWebsite) (tasks.Task, error) {
	source := audit.Source(site.IssueSource)
	return m.s.Tasks.SubmitController(ctx, tasks.SubmitRequest{
		SubjectID: site.Owner, RequestID: site.IssueKey, Name: "签发网站证书 " + websiteTaskDomain(site.Domain),
		ActionID: actionManagedWebsiteIssue, OperationClass: tasks.OperationClassModifying,
		Source: source, Params: m.managedIssueParams(site), IdempotencyKey: site.IssueKey,
		RequireApproval: source == audit.SourceWeb, ApprovalSatisfied: source == audit.SourceSystem,
	})
}

func websiteTaskDomain(domain string) string {
	if len(domain) > 75 {
		return domain[:75] + "…"
	}
	return domain
}

func (w managedWebsiteWorker) Execute(ctx context.Context, task tasks.Task) (map[string]tasks.WorkerResult, error) {
	m := w.manager
	rejected := map[string]tasks.WorkerResult{m.authorityID: {Code: tasks.NodeResultFailed, Message: "website issue plan rejected"}}
	site, err := m.managedIssueSite(ctx, task)
	if err != nil || task.Source == string(audit.SourceSystem) && (!site.AutoRenew || site.OrderID == "" || time.Now().UnixMilli() < site.RenewAtMS) {
		return rejected, nil
	}
	const timeout = 6 * time.Minute
	scope := m.taskScope(task, site.ZoneID)
	ctx, cancel := context.WithTimeout(m.scope(ctx, scope), timeout+time.Minute)
	defer cancel()
	if m.current(ctx, scope) != nil {
		return rejected, nil
	}
	// Website keys are independent of the ACME account and internal PKI.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return rejected, nil
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return rejected, nil
	}
	defer clear(keyDER)
	sealID, ciphertext, err := m.s.Credentials.SealWebsiteKey(task.ID, site.Owner, keyDER)
	if err != nil {
		return rejected, nil
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: site.Domain}, DNSNames: []string{site.Domain}, SignatureAlgorithm: x509.ECDSAWithSHA256}, key)
	if err != nil {
		return rejected, nil
	}
	order := certificates.PublicOrder{TaskID: task.ID, Domain: site.Domain, KeyRef: task.ID, ZoneID: site.ZoneID, ProfileSHA256: site.Profile, PlanSHA256: task.PlanHash, Status: "running", UpdatedAt: time.Now().UTC()}
	err = m.s.DB.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		if err := certificates.ReservePublicOrderTx(ctx, tx, order); err != nil {
			return err
		}
		return m.s.Audit.AppendTx(ctx, tx, audit.Event{SubjectID: site.Owner, Action: task.ActionID, Result: "allowed", RequestID: task.RequestID, TaskID: task.ID, Source: audit.Source(task.Source), ReasonCode: "controller_website_order_reserved", At: order.UpdatedAt})
	})
	if err != nil {
		return map[string]tasks.WorkerResult{m.authorityID: {Code: tasks.NodeResultUnknown, Message: "website order reservation uncertain"}}, nil
	}
	publisher, err := dns.NewACMEChallengePublisher(dns.ACMEPublisherConfig{Service: m.s.DNS, SubjectID: site.Owner, RequestID: task.RequestID, ZoneID: site.ZoneID, TTL: 60})
	if err != nil {
		return m.finish(ctx, task, order, nil, err, nil, nil)
	}
	tracked := &trackedCertificateChallenges{manager: m, scope: scope, next: publisher, pending: map[string]certificateChallengeState{}}
	client, err := certificates.NewACMEClient(certificates.ACMEConfig{
		DirectoryURL: m.o.DirectoryURL, Client: m.acmeClient, AccountKey: "account", Signer: privateACMEAccountSigner{manager: m, scope: scope},
		Challenge: tracked, TrustRoots: m.acmeRoots, Timeout: timeout,
	})
	if err != nil {
		return m.finish(ctx, task, order, nil, err, tracked, nil)
	}
	issued, issueErr := client.IssueDNS01WithCSR(ctx, certificates.ACMERequest{CommonName: site.Domain, KeyRef: certificates.KeyRef(task.ID)}, csr)
	return m.finish(ctx, task, order, &issued, issueErr, tracked, func(ctx context.Context, tx storage.Tx, finished certificates.PublicOrder) error {
		chain, err := transport.ParsePublicCertificateChain(finished.ChainPEM)
		if err != nil {
			return err
		}
		if site.OrderID != "" {
			old, err := certificates.ReadPublicOrder(ctx, tx, site.OrderID)
			if err != nil || !finished.NotAfter.After(old.NotAfter) {
				return errors.New("renewed certificate does not extend validity")
			}
		}
		if _, err = tx.Exec(ctx, `INSERT INTO cx_managed_website_keys(order_id,key_id,sealed_key) VALUES(?,?,?)`, task.ID, sealID, ciphertext); err != nil {
			return err
		}
		result, err := tx.Exec(ctx, `UPDATE cx_managed_websites
			SET order_id=?,generation=generation+1,renew_at_ms=?,delivery_key='',delivery_source=''
			WHERE domain=? AND order_id=? AND issue_key=?`,
			task.ID, managedRenewAt(chain[0].NotBefore, finished.NotAfter).UnixMilli(), site.Domain, site.OrderID, site.IssueKey)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return certificates.ErrACMEConfig
		}
		return nil
	})
}
