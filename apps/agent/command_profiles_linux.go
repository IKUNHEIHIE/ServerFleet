package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

type commandProfile struct {
	ID               string   `json:"id"`
	Executable       string   `json:"executable"`
	SHA256           string   `json:"sha256"`
	Directory        string   `json:"directory"`
	PrefixArguments  []string `json:"prefixArguments"`
	MaximumArguments int      `json:"maximumArguments"`
	MaximumSeconds   int      `json:"maximumSeconds"`
}
type commandProfiles struct {
	Version  int              `json:"version"`
	NodeID   string           `json:"nodeId"`
	Profiles []commandProfile `json:"profiles"`
}

func loadCommandProfiles(filename, node string, root bool) (commandProfiles, string, error) {
	var policy commandProfiles
	raw, err := readCommandPrivateFile(filename, 64<<10, root)
	if err != nil {
		return policy, "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&policy) != nil || decoder.Decode(&struct{}{}) != io.EOF || policy.Version != 1 || policy.NodeID != node || len(policy.Profiles) < 1 || len(policy.Profiles) > 32 {
		return policy, "", errExecutionRejected
	}
	canonical, _ := json.Marshal(policy)
	if !bytes.Equal(bytes.TrimSpace(raw), canonical) {
		return policy, "", errExecutionRejected
	}
	seen := map[string]bool{}
	for _, profile := range policy.Profiles {
		if !executionIDPattern.MatchString(profile.ID) || seen[profile.ID] || !filepath.IsAbs(profile.Executable) || !filepath.IsAbs(profile.Directory) || !transport.FileDigest(profile.SHA256) || profile.MaximumArguments < 0 || profile.MaximumArguments > 32 || profile.MaximumSeconds < 1 || profile.MaximumSeconds > 300 || len(profile.PrefixArguments) > 32 {
			return policy, "", errExecutionRejected
		}
		seen[profile.ID] = true
		for _, arg := range profile.PrefixArguments {
			if len(arg) > 512 || strings.ContainsRune(arg, 0) {
				return policy, "", errExecutionRejected
			}
		}
	}
	digest := sha256.Sum256(canonical)
	return policy, hex.EncodeToString(digest[:]), nil
}
func readCommandPrivateFile(filename string, limit int64, root bool) ([]byte, error) {
	if !filepath.IsAbs(filename) {
		return nil, errExecutionRejected
	}
	if root && (filepath.Clean(filename) != filename || !rootCommandAncestors(filepath.Dir(filename))) {
		return nil, errExecutionRejected
	}
	directory, err := os.Lstat(filepath.Dir(filename))
	if err != nil || !commandFileOwned(directory, true, root) {
		return nil, errExecutionRejected
	}
	parent, err := os.OpenRoot(filepath.Dir(filename))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	anchor, e := parent.Stat(".")
	if e != nil || !os.SameFile(directory, anchor) || !commandFileOwned(anchor, true, root) {
		return nil, errExecutionRejected
	}
	info, err := parent.Lstat(filepath.Base(filename))
	if err != nil || !commandFileOwned(info, false, root) || info.Size() < 1 || info.Size() > limit {
		return nil, errExecutionRejected
	}
	file, err := parent.Open(filepath.Base(filename))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !os.SameFile(info, actual) || !commandFileOwned(actual, false, root) {
		return nil, errExecutionRejected
	}
	return io.ReadAll(io.LimitReader(file, limit+1))
}
func commandFileOwned(info os.FileInfo, directory, root bool) bool {
	if info == nil || info.Mode()&os.ModeSymlink != 0 || directory && !info.IsDir() || !directory && !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	uid := uint32(os.Geteuid())
	if root {
		uid = 0
	}
	return stat.Uid == uid && (!root || os.Geteuid() == 0)
}
func registeredCommands(filename, node string) (agentAction, error) {
	if os.Geteuid() == 0 {
		return agentAction{}, errExecutionRejected
	}
	_, digest, err := loadCommandProfiles(filename, node, false)
	if err != nil {
		return agentAction{}, err
	}
	validate := func(raw []byte) error {
		params, err := transport.ParseCommandParameters(raw)
		if err != nil || params.PolicySHA256 != digest {
			return errExecutionRejected
		}
		policy, current, err := loadCommandProfiles(filename, node, false)
		if err != nil || current != digest {
			return errExecutionRejected
		}
		for _, profile := range policy.Profiles {
			if profile.ID == params.ProfileID && len(params.Arguments) <= profile.MaximumArguments && params.TimeoutSeconds <= profile.MaximumSeconds {
				return nil
			}
		}
		return errExecutionRejected
	}
	return agentAction{Validate: validate, Run: func(ctx context.Context, raw []byte) executionResult {
		if validate(raw) != nil {
			return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_FAILED}
		}
		params, _ := transport.ParseCommandParameters(raw)
		policy, _, _ := loadCommandProfiles(filename, node, false)
		for _, profile := range policy.Profiles {
			if profile.ID == params.ProfileID {
				return runPinnedCommand(ctx, profile, params.Arguments, params.TimeoutSeconds, false)
			}
		}
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_FAILED}
	}}, nil
}
func runPinnedCommand(ctx context.Context, profile commandProfile, arguments []string, seconds int, root bool) executionResult {
	unknown := executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
	if ctx == nil || ctx.Err() != nil || root != (os.Geteuid() == 0) {
		return unknown
	}
	// A root-owned leaf beneath a replaceable ancestor is not a fixed path.
	// Check the program and working directory as well as private policy files.
	if root && (!filepath.IsAbs(profile.Executable) || filepath.Clean(profile.Executable) != profile.Executable || !rootCommandAncestors(filepath.Dir(profile.Executable)) || !rootCommandAncestors(profile.Directory)) {
		return unknown
	}
	info, err := os.Lstat(profile.Executable)
	if err != nil || !info.Mode().IsRegular() || !root && info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 || info.Mode().Perm()&0022 != 0 || info.Size() < 1 || info.Size() > 256<<20 {
		return unknown
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || root && stat.Uid != 0 {
		return unknown
	}
	file, err := os.Open(profile.Executable)
	if err != nil {
		return unknown
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return unknown
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, io.LimitReader(file, (256<<20)+1)); err != nil || hex.EncodeToString(hash.Sum(nil)) != profile.SHA256 {
		return unknown
	}
	directory, err := os.Lstat(profile.Directory)
	if err != nil || !commandFileOwned(directory, true, root) {
		return unknown
	}
	run, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(run, "/proc/self/fd/3", append(append([]string{}, profile.PrefixArguments...), arguments...)...)
	cmd.ExtraFiles = []*os.File{file}
	cmd.Dir = profile.Directory
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = time.Second
	if err = cmd.Start(); err != nil {
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_FAILED, ExitCode: -1}
	}
	err = cmd.Wait()
	if run.Err() != nil {
		return unknown
	}
	if err == nil {
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED}
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_FAILED, ExitCode: int32(exit.ExitCode())}
	}
	return unknown
}
