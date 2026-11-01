//go:build !linux && !windows

package auth

import "os"

// Fail closed until an OS-specific owner/ACL validator has been reviewed.
func credentialPrivateOwner(os.FileInfo, bool, string) bool { return false }
func makeCredentialDirectory(string) error                  { return ErrIdentityUnavailable }
func publishCredentialFile(*os.Root, string, bool) error    { return ErrIdentityUnavailable }
