//go:build !linux

package runtime

func readEnrollmentTokenFile(string) (string, error) { return "", errRuntime }
