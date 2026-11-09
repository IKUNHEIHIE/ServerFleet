//go:build !linux

package main

import (
	"context"
	"io"
)

func registeredRootHelper(string, string) (agentAction, error) {
	return agentAction{}, errExecutionRejected
}
func runRootExecutor(context.Context, []string, io.Reader, io.Writer, io.Writer) int { return 2 }

func inspectCommandPolicy([]string, io.Writer, io.Writer) int { return 2 }
