//go:build !linux

package main

import "os"

func openAgentHealthDirectory(string) (*os.Root, error) { return nil, errAgentConnect }
