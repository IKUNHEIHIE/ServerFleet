//go:build !linux

package bootstrap

import (
	"context"
	"io"
	"net/http"
	"os"
)

func installationEnvironment() []string                     { return nil }
func installationLock(*os.Root) (func(), error)             { return nil, errInstallation }
func installationVerifyFile(string, installationFile) error { return errInstallation }
func installationDownloadFile(context.Context, *http.Client, string, *os.Root, installationFile) error {
	return errInstallation
}
func installationServicePreflight(context.Context) error            { return errInstallation }
func installationUserService(context.Context, string, string) error { return errInstallation }
func installationReadToken() ([]byte, error)                        { return nil, errInstallation }
func runSystemInstallation(context.Context, string, string, bool, io.Writer, io.Writer) error {
	return errInstallation
}
