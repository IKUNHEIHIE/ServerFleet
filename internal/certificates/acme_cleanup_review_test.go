package certificates_test

import (
	"context"
	"errors"
	"testing"

	"serverfleet.local/serverfleet/internal/certificates"
)

func TestR1ACMEFailurePreservesCleanupFailureAndRecord(t *testing.T) {
	for _, phase := range []string{"challenge", "finalize"} {
		t.Run(phase, func(t *testing.T) {
			fake := newFakeACMEServer(t)
			fake.rejectChallenge = phase == "challenge"
			fake.failFinalize = phase == "finalize"
			publisher := newACMEPublisher(fake)
			publisher.cleanErr = errors.New("synthetic cleanup failure")
			client := newACMEClient(t, fake, newTestKeySigner(t, "account-key", "cert-key"), publisher)
			issued, err := client.IssueDNS01(context.Background(), certificates.ACMERequest{CommonName: "app.example.com", KeyRef: "cert-key"})
			if !errors.Is(err, certificates.ErrACMEDNSCleanup) {
				t.Fatalf("cleanup failure lost after %s: %v", phase, err)
			}
			if phase == "challenge" && !errors.Is(err, certificates.ErrACMEChallenge) || phase == "finalize" && !errors.Is(err, certificates.ErrACMEProtocol) {
				t.Fatalf("original %s error lost: %v", phase, err)
			}
			if len(issued.CertificatePEM) != 0 || len(issued.DNS01Records) != 1 || issued.DNS01Records[0].RecordID != "record-1" || publisher.cleaned != 1 {
				t.Fatalf("failed issuance must retain cleanup identity without a certificate: records=%+v, cleanups=%d", issued.DNS01Records, publisher.cleaned)
			}
		})
	}
}
