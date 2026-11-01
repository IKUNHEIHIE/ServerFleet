//go:build !linux

package auth

import "os"

// Fail closed until an OS-specific owner/ACL validator has been reviewed.
func credentialPrivateOwner(os.FileInfo, bool) bool { return false }
