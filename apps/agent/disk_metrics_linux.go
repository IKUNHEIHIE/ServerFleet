package main

import "golang.org/x/sys/unix"

func sampleDiskPercent() (float64, bool) {
	var stat unix.Statfs_t
	if unix.Statfs("/", &stat) != nil || stat.Blocks == 0 || stat.Bfree > stat.Blocks {
		return 0, false
	}
	return float64(stat.Blocks-stat.Bfree) / float64(stat.Blocks) * 100, true
}
