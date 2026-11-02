//go:build !linux

package files

import "os"

func deploymentPrivate(os.FileInfo, bool) bool { return false }
func spoolOwned(os.FileInfo) bool              { return true } // Existing platform semantics; deployment file remains unsupported.
