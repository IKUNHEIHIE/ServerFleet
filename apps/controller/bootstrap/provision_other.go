//go:build !linux

package bootstrap

import "os"

func provisionOwnerSupported() bool                                        { return false }
func newProvisionDirectory(string) (*os.Root, error)                       { return nil, errProvision }
func openProvisionPrivateDirectory(string) (*os.Root, error)               { return nil, errProvision }
func provisionExistingDatabase(string) bool                                { return false }
func openProvisionProgram(string) (*os.File, error)                        { return nil, errProvision }
func provisionWrite(*os.Root, string, []byte, os.FileMode) error           { return errProvision }
func copyProvisionProgram(*os.Root, string, *os.File, string, int64) error { return errProvision }
func syncProvisionDirectory(*os.Root, string) error                        { return errProvision }
