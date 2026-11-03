package dns

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// ACMEPublisherConfig binds one DNS zone and an authorized subject to the
// dns-01 challenge publisher. The subject must already hold dns.record.create and
// dns.record.delete; the publisher never bypasses the dns service authorizer.
type ACMEPublisherConfig struct {
	Service   *Service
	SubjectID string
	RequestID string
	ZoneID    string
	TTL       int
}

// ACMEChallengePublisher publishes `_acme-challenge` TXT values through the dns
// domain service and removes exactly the record it created.
type ACMEChallengePublisher struct {
	service   *Service
	subjectID string
	requestID string
	zoneID    string
	ttl       int
}

func NewACMEChallengePublisher(config ACMEPublisherConfig) (*ACMEChallengePublisher, error) {
	if config.Service == nil || !validToken(config.SubjectID, 128) ||
		!validText(config.RequestID, 128) || !validID(config.ZoneID) {
		return nil, ErrInvalidInput
	}
	ttl := config.TTL
	if ttl == 0 {
		ttl = 60
	}
	if ttl < 60 || ttl > 3600 {
		return nil, fmt.Errorf("%w: ttl", ErrInvalidInput)
	}
	return &ACMEChallengePublisher{
		service: config.Service, subjectID: config.SubjectID, requestID: config.RequestID,
		zoneID: config.ZoneID, ttl: ttl,
	}, nil
}

// Publish creates one TXT record. The cleanup function is idempotent and deletes
// only the record returned by this call. The provider record id is returned so a
// failed cleanup can be persisted by the caller.
func (p *ACMEChallengePublisher) Publish(ctx context.Context, fqdn, value string) (string, func(context.Context) error, error) {
	if p == nil || p.service == nil {
		return "", nil, ErrProviderUnavailable
	}
	if !validToken(fqdn, 255) || !strings.HasPrefix(fqdn, "_acme-challenge.") {
		return "", nil, fmt.Errorf("%w: challenge name", ErrInvalidInput)
	}
	if !validToken(value, 255) {
		return "", nil, fmt.Errorf("%w: challenge value", ErrInvalidInput)
	}
	record, err := p.service.CreateRecord(ctx, p.subjectID, p.requestID, p.zoneID, RecordInput{
		Type: RecordTXT, Name: fqdn, Content: value, TTL: p.ttl,
	})
	if err != nil {
		return "", nil, err
	}
	var (
		mu      sync.Mutex
		done    bool
		unknown error
	)
	return record.ID, func(cleanupCtx context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		if done {
			return nil
		}
		if unknown != nil {
			return unknown
		}
		err := p.service.DeleteRecord(cleanupCtx, p.subjectID, p.requestID, record.ID)
		if err == nil {
			done = true
			return nil
		}
		if errors.Is(err, ErrProviderOutcomeUnknown) {
			unknown = fmt.Errorf("%w: %v", ErrProviderOutcomeUnknown, err)
			return unknown
		}
		return err
	}, nil
}
