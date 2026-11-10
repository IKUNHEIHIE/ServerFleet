//go:build !linux

package localpki

import "os"

func privatePKIInfo(os.FileInfo, bool) bool { return false }
