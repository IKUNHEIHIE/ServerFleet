//go:build !linux

package main

import "os"

func csrPrivateInfo(os.FileInfo, bool) bool { return false }
