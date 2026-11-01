//go:build !linux

package main

import "os"

func agentFileOpen(*os.Root, string, int, os.FileMode) (*os.File, error) {
	return nil, errFileCapability
}
func agentFilePublish(*os.Root, string, string) error { return errFileCapability }
