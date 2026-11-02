//go:build !linux

package updates

// No equivalent ACL/ownership primitive is implemented here, so an owned
// product readiness report cannot be trusted on this platform.
func healthDirectoryPrivate(string) bool { return false }
