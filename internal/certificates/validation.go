package certificates

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

func normalizeCreateInput(input CreateInternalDistributionInput) (CreateInternalDistributionInput, error) {
	for _, value := range append([]string{input.ID, input.CommonName, input.ServiceGroupID}, input.NodeIDs...) {
		if containsPrivateKeyMaterial(value) {
			return CreateInternalDistributionInput{}, ErrKeyMaterialRejected
		}
	}
	if !validInputToken(input.ID, 128) {
		return CreateInternalDistributionInput{}, fmt.Errorf("%w: certificate id", ErrInvalidInput)
	}
	if !validInputToken(input.CommonName, 255) {
		return CreateInternalDistributionInput{}, fmt.Errorf("%w: common name", ErrInvalidInput)
	}
	if !validInputToken(input.ServiceGroupID, 128) {
		return CreateInternalDistributionInput{}, fmt.Errorf("%w: service group id", ErrInvalidInput)
	}
	nodes, err := normalizeNodeIDs(input.NodeIDs)
	if err != nil {
		return CreateInternalDistributionInput{}, err
	}
	if input.ExpectedGroupRevision == 0 {
		return CreateInternalDistributionInput{}, ErrInvalidRevision
	}
	if err := input.KeyRef.Validate(); err != nil {
		return CreateInternalDistributionInput{}, err
	}
	if input.CredentialType == "" {
		input.CredentialType = CredentialTrustBundle
	}
	if !input.CredentialType.valid() {
		return CreateInternalDistributionInput{}, fmt.Errorf("%w: credential type", ErrInvalidInput)
	}
	validity, err := normalizeValidity(input.Validity)
	if err != nil {
		return CreateInternalDistributionInput{}, err
	}
	if !input.Confirm {
		return CreateInternalDistributionInput{}, ErrConfirmationRequired
	}
	input.NodeIDs = nodes
	input.Validity = validity
	return input, nil
}

func normalizeNodeIDs(ids []string) ([]string, error) {
	if len(ids) == 0 || len(ids) > 500 {
		return nil, fmt.Errorf("%w: node ids", ErrInvalidInput)
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !validInputToken(id, 128) {
			return nil, fmt.Errorf("%w: node id", ErrInvalidInput)
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func normalizeValidity(validity Validity) (Validity, error) {
	if validity.NotBefore.IsZero() || validity.NotAfter.IsZero() {
		return Validity{}, ErrInvalidValidity
	}
	before := validity.NotBefore.UTC()
	after := validity.NotAfter.UTC()
	if !after.After(before) {
		return Validity{}, ErrInvalidValidity
	}
	return Validity{NotBefore: before, NotAfter: after}, nil
}

func normalizeIssued(issued IssuedCertificate, fallbackKey KeyRef, validity Validity) (IssuedCertificate, error) {
	if !validInputToken(issued.Serial, 128) || !validInputToken(issued.IssuerRef, 256) {
		return IssuedCertificate{}, ErrInvalidIssuerResponse
	}
	if issued.KeyRef == "" {
		issued.KeyRef = fallbackKey
	}
	if err := issued.KeyRef.Validate(); err != nil {
		return IssuedCertificate{}, ErrInvalidIssuerResponse
	}
	if issued.NotBefore.IsZero() || issued.NotAfter.IsZero() {
		issued.NotBefore = validity.NotBefore
		issued.NotAfter = validity.NotAfter
	}
	if !issued.NotAfter.After(issued.NotBefore) {
		return IssuedCertificate{}, ErrInvalidIssuerResponse
	}
	issued.NotBefore = issued.NotBefore.UTC()
	issued.NotAfter = issued.NotAfter.UTC()
	return issued, nil
}

// containsPrivateKeyMaterial rejects any text that could carry PEM key bytes,
// including RSA, EC and OpenSSH variants.
func containsPrivateKeyMaterial(value string) bool {
	return strings.Contains(strings.ToUpper(value), "PRIVATE KEY")
}

func validInputToken(value string, max int) bool {
	return validToken(value, max) && !containsPrivateKeyMaterial(value)
}

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
