package certificates

import (
	"context"

	"serverfleet.local/serverfleet/internal/storage"
)

func (s *Service) Renew(ctx context.Context, subjectID, requestID, certificateID string, input RenewInput) (CertificateView, error) {
	if err := s.requireRequest(requestID); err != nil {
		return CertificateView{}, err
	}
	current, err := s.repo.getCertificate(ctx, s.db, certificateID)
	if err != nil {
		return CertificateView{}, err
	}
	nodes, err := s.certificateNodes(ctx, certificateID)
	if err != nil {
		return CertificateView{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionCertificateRenew, requestID, nodes, "certificate_renew"); err != nil {
		return CertificateView{}, err
	}
	if current.Status == StatusRevoked {
		return CertificateView{}, ErrCertificateRevoked
	}
	if s.issuer == nil {
		return CertificateView{}, ErrIssuerUnavailable
	}
	validity, err := normalizeValidity(input.Validity)
	if err != nil {
		return CertificateView{}, err
	}
	issued, err := s.issuer.Renew(ctx, RenewRequest{
		CertificateID: current.ID, CommonName: current.CommonName,
		IssuerRef: current.IssuerRef, Serial: current.Serial, KeyRef: current.KeyRef,
		NotBefore: validity.NotBefore, NotAfter: validity.NotAfter,
	})
	if err != nil {
		return CertificateView{}, ErrIssuerFailed
	}
	issued, err = normalizeIssued(issued, current.KeyRef, validity)
	if err != nil {
		return CertificateView{}, err
	}
	updated := current
	updated.Status = StatusActive
	updated.IssuerRef = issued.IssuerRef
	updated.Serial = issued.Serial
	updated.NotBefore = issued.NotBefore
	updated.NotAfter = issued.NotAfter
	updated.UpdatedAt = s.now()
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.updateCertificate(txCtx, tx, updated); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionCertificateRenew, "success", requestID, "", nodes, ""))
	})
	if err != nil {
		return CertificateView{}, err
	}
	return updated.view(), nil
}

func (s *Service) Revoke(ctx context.Context, subjectID, requestID, certificateID string) (CertificateView, error) {
	if err := s.requireRequest(requestID); err != nil {
		return CertificateView{}, err
	}
	current, err := s.repo.getCertificate(ctx, s.db, certificateID)
	if err != nil {
		return CertificateView{}, err
	}
	nodes, err := s.certificateNodes(ctx, certificateID)
	if err != nil {
		return CertificateView{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionCertificateRevoke, requestID, nodes, "certificate_revoke"); err != nil {
		return CertificateView{}, err
	}
	if current.Status == StatusRevoked {
		return CertificateView{}, ErrCertificateRevoked
	}
	if s.issuer == nil {
		return CertificateView{}, ErrIssuerUnavailable
	}
	if err := s.issuer.Revoke(ctx, RevokeRequest{
		CertificateID: current.ID, IssuerRef: current.IssuerRef, Serial: current.Serial,
		KeyRef: current.KeyRef,
	}); err != nil {
		return CertificateView{}, ErrIssuerFailed
	}
	now := s.now()
	updated := current
	updated.Status = StatusRevoked
	updated.UpdatedAt = now
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.updateCertificate(txCtx, tx, updated); err != nil {
			return err
		}
		if err := s.repo.revokeDistributions(txCtx, tx, certificateID, now); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionCertificateRevoke, "success", requestID, "", nodes, ""))
	})
	if err != nil {
		return CertificateView{}, err
	}
	return updated.view(), nil
}

func (s *Service) Rotate(ctx context.Context, subjectID, requestID, certificateID string, input RotateInput) (CertificateView, error) {
	if err := s.requireRequest(requestID); err != nil {
		return CertificateView{}, err
	}
	if err := input.NewKeyRef.Validate(); err != nil {
		return CertificateView{}, err
	}
	current, err := s.repo.getCertificate(ctx, s.db, certificateID)
	if err != nil {
		return CertificateView{}, err
	}
	nodes, err := s.certificateNodes(ctx, certificateID)
	if err != nil {
		return CertificateView{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionCertificateRotate, requestID, nodes, "certificate_rotate"); err != nil {
		return CertificateView{}, err
	}
	if current.Status == StatusRevoked {
		return CertificateView{}, ErrCertificateRevoked
	}
	if s.issuer == nil {
		return CertificateView{}, ErrIssuerUnavailable
	}
	validity := input.Validity
	if validity.NotBefore.IsZero() && validity.NotAfter.IsZero() {
		validity = Validity{NotBefore: current.NotBefore, NotAfter: current.NotAfter}
	}
	validity, err = normalizeValidity(validity)
	if err != nil {
		return CertificateView{}, err
	}
	issued, err := s.issuer.Renew(ctx, RenewRequest{
		CertificateID: current.ID, CommonName: current.CommonName,
		IssuerRef: current.IssuerRef, Serial: current.Serial, KeyRef: input.NewKeyRef,
		NotBefore: validity.NotBefore, NotAfter: validity.NotAfter,
	})
	if err != nil {
		return CertificateView{}, ErrIssuerFailed
	}
	issued, err = normalizeIssued(issued, input.NewKeyRef, validity)
	if err != nil {
		return CertificateView{}, err
	}
	updated := current
	updated.Status = StatusActive
	updated.KeyRef = input.NewKeyRef
	updated.IssuerRef = issued.IssuerRef
	updated.Serial = issued.Serial
	updated.NotBefore = issued.NotBefore
	updated.NotAfter = issued.NotAfter
	updated.UpdatedAt = s.now()
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.updateCertificate(txCtx, tx, updated); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionCertificateRotate, "success", requestID, "", nodes, "key_rotation"))
	})
	if err != nil {
		return CertificateView{}, err
	}
	return updated.view(), nil
}

func (s *Service) List(ctx context.Context, subjectID string) ([]CertificateView, error) {
	records, err := s.repo.listCertificates(ctx)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		if err := s.authorizeRead(ctx, subjectID, nil); err != nil {
			return nil, err
		}
		return []CertificateView{}, nil
	}
	visible := make([]CertificateView, 0, len(records))
	for _, record := range records {
		nodes, err := s.certificateNodes(ctx, record.ID)
		if err != nil {
			return nil, err
		}
		authErr := s.authorizeRead(ctx, subjectID, nodes)
		if authErr == nil {
			visible = append(visible, record.view())
			continue
		}
		if isPureDenial(authErr) {
			continue
		}
		return nil, authErr
	}
	return visible, nil
}

func (s *Service) Get(ctx context.Context, subjectID, certificateID string) (CertificateDetail, error) {
	record, err := s.repo.getCertificate(ctx, s.db, certificateID)
	if err != nil {
		return CertificateDetail{}, err
	}
	distributions, err := s.repo.listDistributions(ctx, s.db, certificateID)
	if err != nil {
		return CertificateDetail{}, err
	}
	nodes := make([]string, 0, len(distributions))
	for _, distribution := range distributions {
		nodes = append(nodes, distribution.NodeID)
	}
	if err := s.authorizeRead(ctx, subjectID, nodes); err != nil {
		return CertificateDetail{}, err
	}
	return CertificateDetail{Certificate: record.view(), Distributions: distributions}, nil
}

// ListDetails returns every certificate the subject may read together with its
// distribution rows. Visibility is decided once by List; management metadata
// never includes key references.
func (s *Service) ListDetails(ctx context.Context, subjectID string) ([]CertificateDetail, error) {
	views, err := s.List(ctx, subjectID)
	if err != nil {
		return nil, err
	}
	details := make([]CertificateDetail, 0, len(views))
	for _, view := range views {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		distributions, err := s.repo.listDistributions(ctx, s.db, view.ID)
		if err != nil {
			return nil, err
		}
		details = append(details, CertificateDetail{Certificate: view, Distributions: distributions})
	}
	return details, nil
}
