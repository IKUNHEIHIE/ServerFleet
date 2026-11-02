//go:build !linux

package main

import "os"

func agentFileOpen(*os.Root, string, int, os.FileMode) (*os.File, error) {
	return nil, errFileCapability
}
func agentFilePublishNoReplace(*os.Root, string, string) error { return errFileCapability }
func agentFilePublishReplace(*os.Root, string, string) error   { return errFileCapability }
func agentFileDestination(*os.Root, string) (agentFileKind, error) {
	return agentDestinationOther, errFileCapability
}
func agentFileSpace(*os.Root, int64) error { return errFileCapability }
