//go:build !linux

package main

import "runtime"

type agentHostFacts struct {
	CPUModel    string
	CPUCores    uint32
	MemoryBytes uint64
	DiskBytes   uint64
}

func collectHostFacts(string) agentHostFacts {
	return agentHostFacts{CPUCores: uint32(runtime.NumCPU())}
}
