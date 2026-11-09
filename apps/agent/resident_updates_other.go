//go:build !linux

package main

func registeredResidentUpdate(string, string) (agentAction, error) {
	return agentAction{}, errExecutionRejected
}
func residentUpdateBusy(string) bool { return true }
