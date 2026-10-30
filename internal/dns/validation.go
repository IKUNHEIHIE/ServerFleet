package dns

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func normalizeCreateAccountInput(input CreateAccountInput, now time.Time) (accountRecord, error) {
	if !validID(input.ID) {
		return accountRecord{}, fmt.Errorf("%w: account id", ErrInvalidInput)
	}
	if !validText(input.DisplayName, 128) {
		return accountRecord{}, fmt.Errorf("%w: display name", ErrInvalidInput)
	}
	if input.Note != "" && !validText(input.Note, 1024) {
		return accountRecord{}, fmt.Errorf("%w: note", ErrInvalidInput)
	}
	status := CredentialNotConfigured
	if input.Credential != "" {
		if err := input.Credential.Validate(); err != nil {
			return accountRecord{}, err
		}
		status = CredentialPendingVerification
	}
	return accountRecord{
		ID: input.ID, ProviderCode: ProviderCloudflare, DisplayName: input.DisplayName,
		Note: input.Note, Enabled: input.Enabled, IsDefault: input.MakeDefault,
		CredentialRef: input.Credential, CredentialStatus: status,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func normalizeUpdateAccountInput(input UpdateAccountInput) (UpdateAccountInput, error) {
	if !validText(input.DisplayName, 128) {
		return UpdateAccountInput{}, fmt.Errorf("%w: display name", ErrInvalidInput)
	}
	if input.Note != "" && !validText(input.Note, 1024) {
		return UpdateAccountInput{}, fmt.Errorf("%w: note", ErrInvalidInput)
	}
	if input.Credential != "" {
		if err := input.Credential.Validate(); err != nil {
			return UpdateAccountInput{}, err
		}
	}
	return input, nil
}

func normalizeZoneInput(input CreateZoneInput) error {
	if !validID(input.ID) {
		return fmt.Errorf("%w: zone id", ErrInvalidInput)
	}
	if !validProviderID(input.ProviderZoneID) {
		return fmt.Errorf("%w: provider zone id", ErrInvalidInput)
	}
	return nil
}

func normalizeProviderZone(info ZoneInfo) (ZoneInfo, ZoneStatus, error) {
	if !validProviderID(info.ProviderZoneID) {
		return ZoneInfo{}, "", ErrInvalidProviderResponse
	}
	if !validToken(info.Name, 255) {
		return ZoneInfo{}, "", ErrInvalidProviderResponse
	}
	return info, normalizeZoneStatus(info.Status), nil
}

func validateRecordInput(input RecordInput) (int, error) {
	if !input.Type.valid() {
		return 0, fmt.Errorf("%w: record type", ErrInvalidInput)
	}
	if !validToken(input.Name, 255) {
		return 0, fmt.Errorf("%w: name", ErrInvalidInput)
	}
	if !validText(input.Content, 4096) {
		return 0, fmt.Errorf("%w: content", ErrInvalidInput)
	}
	if input.TTL < 0 || input.TTL > 86400 {
		return 0, fmt.Errorf("%w: ttl", ErrInvalidInput)
	}
	if input.Proxied && !input.Type.proxiable() {
		return 0, fmt.Errorf("%w: proxied is not supported for this record type", ErrInvalidInput)
	}
	priority := 0
	if input.Type == RecordMX {
		if input.Priority == nil || *input.Priority < 0 || *input.Priority > 65535 {
			return 0, fmt.Errorf("%w: mx priority", ErrInvalidInput)
		}
		priority = *input.Priority
	} else if input.Priority != nil {
		return 0, fmt.Errorf("%w: priority is only valid for MX", ErrInvalidInput)
	}
	return priority, nil
}

func validID(value string) bool { return validToken(value, 128) }

func validProviderID(value string) bool { return validToken(value, 256) }

func validText(value string, max int) bool {
	if value == "" || len(value) > max || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	return !strings.ContainsFunc(value, func(ch rune) bool {
		return unicode.IsControl(ch) || unicode.In(ch, unicode.Cf, unicode.Zl, unicode.Zp)
	})
}

func validToken(value string, max int) bool {
	return validText(value, max) && !strings.ContainsFunc(value, unicode.IsSpace)
}
