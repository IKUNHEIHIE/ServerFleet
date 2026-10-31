package auth

import (
	"encoding/json"
	"errors"
	"time"
)

var ErrIdempotencyConflict = errors.New("auth: idempotency conflict")

type identityReceipt struct {
	Digest   string
	EntityID string
	Password passwordRecord
	Expires  time.Time
}

func receiptKey(subject, operation, key string) string {
	return credentialDigest(subject + "\x00" + operation + "\x00" + key)
}
func requestDigest(input any) string { b, _ := json.Marshal(input); return credentialDigest(string(b)) }
func loadReceipt(st *identityState, subject, operation, key, digest string, now time.Time) (identityReceipt, bool, error) {
	if key == "" {
		return identityReceipt{}, false, nil
	}
	if !validPolicyID(key) || len(key) < 16 || len(key) > 128 {
		return identityReceipt{}, false, ErrInvalidCheck
	}
	for k, receipt := range st.Receipts {
		if !now.Before(receipt.Expires) {
			delete(st.Receipts, k)
		}
	}
	receipt, ok := st.Receipts[receiptKey(subject, operation, key)]
	if ok && receipt.Digest != digest {
		return identityReceipt{}, false, ErrIdempotencyConflict
	}
	if !ok && len(st.Receipts) >= 5000 {
		return identityReceipt{}, false, ErrIdentityUnavailable
	}
	return receipt, ok, nil
}
func saveReceipt(st *identityState, subject, operation, key, digest, id string, pw passwordRecord, now time.Time) {
	if key != "" {
		st.Receipts[receiptKey(subject, operation, key)] = identityReceipt{digest, id, pw, now.Add(time.Hour)}
	}
}
