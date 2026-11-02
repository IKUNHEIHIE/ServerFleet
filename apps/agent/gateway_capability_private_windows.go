//go:build windows

package main

import "os"

// Windows ownership is enforced by the inherited private ACL on the configured
// prefix; the Agent already rejects reparse-point roots and non-regular files.
func checkPrivateRoot(string, os.FileInfo) error { return nil }

func checkPrivateFile(string, os.FileInfo) error { return nil }

func checkExecutableFile(string, os.FileInfo) error { return nil }
