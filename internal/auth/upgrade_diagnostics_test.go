package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type upgradeReadOnlyPayload []byte

func (s upgradeReadOnlyPayload) View(_ context.Context, fn func([]byte) error) error {
	return fn([]byte(s))
}
func (s upgradeReadOnlyPayload) Update(context.Context, func([]byte) ([]byte, error)) error {
	return errors.New("preflight must not write")
}

func TestUpgradeDiagnosticsIdentifyVersionAndRecordWithoutEchoingSecrets(t *testing.T) {
	for _, sample := range []struct{ raw, code string }{
		{`{"Version":99,"Password":"do-not-echo-secret"}`, "IDENTITY_VERSION_UNSUPPORTED"},
		{`{"Version":1,"Initialized":false,"Sessions":{"do-not-echo-secret":{}}}`, "IDENTITY_SESSION_RECORD_INVALID"},
		{`{"Version":1,"Initialized":true,"Users":{"do-not-echo-secret":{}}}`, "IDENTITY_USER_RECORD_INVALID"},
		{`{"Version":1,`, "IDENTITY_PAYLOAD_JSON_INVALID"},
	} {
		_, e := CheckIdentityUpgrade(context.Background(), upgradeReadOnlyPayload(sample.raw), "/unused")
		var diagnostic *UpgradeError
		if !errors.As(e, &diagnostic) || diagnostic.Code != sample.code || strings.Contains(e.Error(), "do-not-echo-secret") {
			t.Fatal(sample.code, e)
		}
	}
}
