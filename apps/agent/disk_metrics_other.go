//go:build !linux

package main

func sampleDiskPercent() (float64, bool) { return 0, false }
