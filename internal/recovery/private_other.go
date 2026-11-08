//go:build !linux

package recovery

import (
	"os"
	"os/exec"
)

func privateRecoveryInfo(os.FileInfo, bool) bool { return false }
func privateRecoveryProgram(os.FileInfo) bool    { return false }
func configureRecoveryProcess(*exec.Cmd)         {}
