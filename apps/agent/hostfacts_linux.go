//go:build linux

package main

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// agentHostFacts is static host information reported once per handshake. Every
// field is best-effort: zero or empty means "not reported", never a guess.
type agentHostFacts struct {
	CPUModel    string
	CPUCores    uint32
	MemoryBytes uint64
	DiskBytes   uint64
}

func collectHostFacts(fileRoot string) agentHostFacts {
	facts := agentHostFacts{CPUCores: uint32(runtime.NumCPU())}
	if f, err := os.Open("/proc/cpuinfo"); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if key, value, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(key) == "model name" {
				facts.CPUModel = strings.TrimSpace(value)
				break
			}
		}
		_ = f.Close()
	}
	if f, err := os.Open("/proc/meminfo"); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 2 && fields[0] == "MemTotal:" {
				if kb, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
					facts.MemoryBytes = kb * 1024
				}
				break
			}
		}
		_ = f.Close()
	}
	root := fileRoot
	if root == "" {
		root = "/"
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(root, &stat); err == nil {
		facts.DiskBytes = stat.Blocks * uint64(stat.Bsize)
	}
	return facts
}
