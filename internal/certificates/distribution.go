package certificates

import (
	"context"
	"errors"
	"fmt"

	"serverfleet.local/serverfleet/internal/storage"
)

func (s *Service) CreateInternalDistribution(ctx context.Context, subjectID, requestID string, input CreateInternalDistributionInput) (DistributionResult, error) {
	if err := s.requireRequest(requestID); err != nil {
		return DistributionResult{}, err
	}
	normalized, err := normalizeCreateInput(input)
	if err != nil {
		return DistributionResult{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionDistributionCreate, requestID, normalized.NodeIDs, "certificate_distribution_create"); err != nil {
		return DistributionResult{}, err
	}
	if _, err := s.repo.getCertificate(ctx, s.db, normalized.ID); err == nil {
		return DistributionResult{}, ErrCertificateExists
	} else if !errors.Is(err, ErrCertificateNotFound) {
		return DistributionResult{}, err
	}
	if s.groupRevisions == nil {
		return DistributionResult{}, ErrGroupRevisionsUnavailable
	}
	revision, err := s.groupRevisions.Revision(ctx, normalized.ServiceGroupID)
	if err != nil {
		return DistributionResult{}, ErrRevisionCheckFailed
	}
	if revision != normalized.ExpectedGroupRevision {
		return DistributionResult{}, ErrRevisionConflict
	}
	if s.issuer == nil {
		return DistributionResult{}, ErrIssuerUnavailable
	}
	if s.tasks == nil {
		return DistributionResult{}, ErrTaskSubmitterUnavailable
	}
	issued, err := s.issuer.Issue(ctx, IssueRequest{
		CommonName: normalized.CommonName,
		Kind:       KindInternal,
		KeyRef:     normalized.KeyRef,
		NotBefore:  normalized.Validity.NotBefore,
		NotAfter:   normalized.Validity.NotAfter,
	})
	if err != nil {
		return DistributionResult{}, ErrIssuerFailed
	}
	issued, err = normalizeIssued(issued, normalized.KeyRef, normalized.Validity)
	if err != nil {
		return DistributionResult{}, err
	}
	now := s.now()
	record := certificateRecord{
		ID: normalized.ID, Kind: KindInternal, CommonName: normalized.CommonName,
		Status: StatusActive, IssuerRef: issued.IssuerRef, KeyRef: issued.KeyRef,
		Serial: issued.Serial, NotBefore: issued.NotBefore, NotAfter: issued.NotAfter,
		CreatedAt: now, UpdatedAt: now,
	}
	taskID, err := s.tasks.Submit(ctx, DistributionTask{
		SubjectID: subjectID, CertificateID: normalized.ID,
		ServiceGroupID: normalized.ServiceGroupID,
		NodeIDs:        append([]string(nil), normalized.NodeIDs...),
	})
	if err != nil {
		return DistributionResult{}, ErrTaskSubmitFailed
	}
	if !validInputToken(taskID, 128) {
		return DistributionResult{}, ErrInvalidTaskID
	}
	distributions := make([]DistributionView, 0, len(normalized.NodeIDs))
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.createCertificate(txCtx, tx, record); err != nil {
			return err
		}
		views := make([]DistributionView, 0, len(normalized.NodeIDs))
		for _, nodeID := range normalized.NodeIDs {
			view := DistributionView{
				ID: distributionID(record.ID, nodeID), CertificateID: record.ID,
				ServiceGroupID: normalized.ServiceGroupID, NodeID: nodeID,
				Status: DistributionPending, TaskID: taskID, CreatedAt: now, UpdatedAt: now,
			}
			if err := s.repo.createDistribution(txCtx, tx, view); err != nil {
				return err
			}
			views = append(views, view)
		}
		event := s.event(subjectID, ActionDistributionCreate, "success", requestID, taskID, normalized.NodeIDs, "")
		if err := s.audit.AppendTx(txCtx, tx, event); err != nil {
			return err
		}
		distributions = views
		return nil
	})
	if err != nil {
		return DistributionResult{}, err
	}
	return DistributionResult{
		Certificate: record.view(), Distributions: distributions, TaskID: taskID,
	}, nil
}

func distributionID(certificateID, nodeID string) string {
	return fmt.Sprintf("%d:%s:%s", len(certificateID), certificateID, nodeID)
}
