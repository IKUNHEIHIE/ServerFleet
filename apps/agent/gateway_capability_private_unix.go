//go:build !windows

package main

import (
	"os"
	"syscall"
)

// checkPrivateRoot requires the operator-owned directory to be owned by the
// current user and inaccessible to group/other. This is a Linux agent invariant,
// not an HTTP-supplied option.
func checkPrivateRoot(_ string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return errGatewayCapability
	}
	if info.Mode().Perm()&0o077 != 0 {
		return errGatewayCapability
	}
	return nil
}

func checkPrivateFile(_ string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return errGatewayCapability
	}
	if info.Mode().Perm()&0o022 != 0 {
		return errGatewayCapability
	}
	return nil
}
