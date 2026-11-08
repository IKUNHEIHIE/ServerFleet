//go:build !linux

package server

import "os"

func gatewayDeploymentPrivate(os.FileInfo, bool) bool { return false }
