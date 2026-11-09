//go:build !linux

package main

func registeredCommands(string, string) (agentAction, error) {
	return agentAction{}, errExecutionRejected
}
