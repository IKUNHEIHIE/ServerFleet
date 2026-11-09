package server

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/gateway"
	"serverfleet.local/serverfleet/internal/publishing"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

const nodeCertificatePrefix = "nodecert-"

type nodeCertificatePublication struct {
	db         *storage.DB
	tasks      *tasks.Repo
	gateways   *gateway.Service
	authorizer auth.Authorizer
	roots      map[string]string
	now        func() time.Time
}

func (p *nodeCertificatePublication) proof(ctx context.Context, subject, id string) (transport.CertificateChainReceipt, error) {
	if p == nil || p.db == nil || p.tasks == nil || p.authorizer == nil || !strings.HasPrefix(id, nodeCertificatePrefix) {
		return transport.CertificateChainReceipt{}, publishing.ErrProductionCertificate
	}
	taskID := strings.TrimPrefix(id, nodeCertificatePrefix)
	if !deploymentID.MatchString(taskID) {
		return transport.CertificateChainReceipt{}, publishing.ErrProductionCertificate
	}
	t, err := p.tasks.Get(ctx, taskID)
	if err != nil || t.ActionID != transport.CertificateInstallAction || t.Status != tasks.StatusCompleted || len(t.Targets) != 1 || t.Targets[0].State != tasks.TargetSucceeded {
		return transport.CertificateChainReceipt{}, publishing.ErrProductionCertificate
	}
	receipt, err := transport.ReadCertificateChain(ctx, p.db, taskID)
	if err != nil || receipt.PlanSHA256 != t.PlanHash || receipt.NodeID != t.Targets[0].NodeID || !p.now().Before(receipt.Result.NotAfter) {
		return transport.CertificateChainReceipt{}, publishing.ErrProductionCertificate
	}
	if err = p.authorizer.Authorize(ctx, auth.Check{SubjectID: subject, Action: certificates.ActionCertificateRead, NodeIDs: []string{receipt.NodeID}}); err != nil {
		return transport.CertificateChainReceipt{}, err
	}
	root, ok := p.roots[receipt.NodeID]
	if !ok || !validCertificatePath(root) {
		return transport.CertificateChainReceipt{}, publishing.ErrProductionCertificate
	}
	return receipt, nil
}
func (p *nodeCertificatePublication) paths(ctx context.Context, subject, id, gatewayID string) (string, string, error) {
	receipt, err := p.proof(ctx, subject, id)
	if err != nil {
		return "", "", err
	}
	if gatewayID != "" {
		record, e := p.gateways.GetGateway(ctx, subject, gatewayID)
		if e != nil || record.NodeID != receipt.NodeID {
			return "", "", publishing.ErrProductionCertificate
		}
	}
	root := p.roots[receipt.NodeID]
	return filepath.Join(root, receipt.Result.KeyRef, "chain-"+receipt.Result.LeafSHA256+".pem"), filepath.Join(root, receipt.Result.KeyRef, "website-key.pem"), nil
}
func (p *nodeCertificatePublication) inspect(ctx context.Context, subject, id, domain string) (string, error) {
	receipt, err := p.proof(ctx, subject, id)
	if err != nil || !certificateMatchesDomain(receipt.Result.Domain, domain) {
		return "", publishing.ErrProductionCertificate
	}
	return fmt.Sprintf("node=%s;leaf_sha256=%s;not_after=%s", receipt.NodeID, receipt.Result.LeafSHA256, receipt.Result.NotAfter.UTC().Format(time.RFC3339)), nil
}
