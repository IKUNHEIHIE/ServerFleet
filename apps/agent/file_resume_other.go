//go:build !linux

package main

import (
	"context"
	"os"
)

func agentResumeLock(*os.Root, string) (func(), error)   { return nil, errFileCapability }
func writeAgentResumeRecord(*os.Root, string, any) error { return errFileCapability }
func (*agentFileCapability) checkpointPrefix(context.Context, agentFileBinding, *os.File, uint64, []byte) error {
	return errFileCapability
}
func (*agentFileCapability) adoptResume(_ context.Context, b agentFileBinding, _ string) (agentFileBinding, error) {
	return b, errFileCapability
}
