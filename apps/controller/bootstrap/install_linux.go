//go:build linux

package bootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"serverfleet.local/serverfleet/internal/recovery"
)

func installationEnvironment() []string {
	env := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	for _, key := range []string{"HOME", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	return env
}
func installationLock(root *os.Root) (func(), error) {
	f, e := root.OpenFile("install.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	i, e := f.Stat()
	if e != nil || !i.Mode().IsRegular() || i.Mode().Perm() != 0600 {
		f.Close()
		return nil, errInstallation
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("installation is already running")
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
func installationVerifyFile(filename string, want installationFile) error {
	before, e := os.Lstat(filename)
	if e != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 || before.Size() != want.Bytes {
		return errInstallation
	}
	expected := os.FileMode(0600)
	if want.Executable {
		expected = 0700
	}
	if before.Mode().Perm() != expected {
		return errInstallation
	}
	st, ok := before.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 {
		return errInstallation
	}
	f, e := os.Open(filename)
	if e != nil {
		return e
	}
	defer f.Close()
	after, e := f.Stat()
	if e != nil || !os.SameFile(before, after) {
		return errInstallation
	}
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, want.Bytes+1))
	if e != nil || n != want.Bytes || hex.EncodeToString(h.Sum(nil)) != want.SHA256 {
		return errInstallation
	}
	return nil
}
func installationDownloadFile(ctx context.Context, client *http.Client, url string, root *os.Root, want installationFile) error {
	req, e := http.NewRequestWithContext(ctx, "GET", url, nil)
	if e != nil {
		return e
	}
	req.Header.Set("Accept-Encoding", "identity")
	response, e := client.Do(req)
	if e != nil {
		return errInstallation
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.ContentLength > want.Bytes || response.Header.Get("Content-Encoding") != "" || response.Header.Get("Set-Cookie") != "" {
		return errInstallation
	}
	tmp := want.Path + ".download"
	if i, e := root.Lstat(tmp); e == nil {
		st, ok := i.Sys().(*syscall.Stat_t)
		if !ok || !i.Mode().IsRegular() || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 {
			return errInstallation
		}
		if e = root.Remove(tmp); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	f, e := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(response.Body, want.Bytes+1))
	if e != nil || n != want.Bytes || hex.EncodeToString(h.Sum(nil)) != want.SHA256 {
		f.Close()
		return errors.New("download hash/length rejected; no executable was run")
	}
	if want.Executable {
		e = f.Chmod(0700)
	}
	se, ce := f.Sync(), f.Close()
	if e != nil || se != nil || ce != nil {
		return errInstallation
	}
	if _, e = root.Lstat(want.Path); !os.IsNotExist(e) {
		return errInstallation
	}
	if e = root.Rename(tmp, want.Path); e != nil {
		return e
	}
	return syncProvisionDirectory(root, filepath.Dir(want.Path))
}
func installationSystemctl(ctx context.Context, userMode bool, args ...string) error {
	prefix := []string{}
	if userMode {
		prefix = append(prefix, "--user")
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/systemctl", append(prefix, args...)...)
	cmd.Env = installationEnvironment()
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return errors.New("systemd operation failed; inspect service status, existing installation retained")
	}
	return nil
}
func installationServicePreflight(ctx context.Context) error {
	if e := installationSystemctl(ctx, true, "show-environment"); e != nil {
		return errors.New("systemd user manager unavailable; use root with -system-user serverfleet for a persistent system service")
	}
	return nil
}
func installationUserService(ctx context.Context, directory, component string) error {
	name := "serverfleet-" + component + ".service"
	if e := installationSystemctl(ctx, true, "link", filepath.Join(directory, name)); e != nil {
		return e
	}
	if e := installationSystemctl(ctx, true, "daemon-reload"); e != nil {
		return e
	}
	return installationSystemctl(ctx, true, "enable", "--now", name)
}
func installationReadToken(ctx context.Context) ([]byte, error) {
	tty, e := os.OpenFile("/dev/tty", os.O_RDWR|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, errors.New("private token file required when no interactive terminal is available")
	}
	defer tty.Close()
	var old syscall.Termios
	if _, _, n := syscall.Syscall(syscall.SYS_IOCTL, tty.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&old))); n != 0 {
		return nil, errInstallation
	}
	next := old
	next.Lflag &^= syscall.ECHO
	if _, _, n := syscall.Syscall(syscall.SYS_IOCTL, tty.Fd(), syscall.TCSETS, uintptr(unsafe.Pointer(&next))); n != 0 {
		return nil, errInstallation
	}
	defer func() {
		_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, tty.Fd(), syscall.TCSETS, uintptr(unsafe.Pointer(&old)))
		_, _ = fmt.Fprintln(tty)
	}()
	_, _ = fmt.Fprint(tty, "Paste the one-time enrollment token (hidden): ")
	raw := []byte{}
	buf := make([]byte, 256)
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		n, e := syscall.Read(int(tty.Fd()), buf)
		if n > 0 {
			raw = append(raw, buf[:n]...)
			if len(raw) > 4096 {
				return nil, errInstallation
			}
			if bytes.ContainsRune(raw, '\n') {
				raw = bytes.TrimSpace(raw)
				if len(raw) < 16 {
					return nil, errInstallation
				}
				return raw, nil
			}
		}
		if e != nil && e != syscall.EAGAIN && e != syscall.EINTR {
			return nil, errInstallation
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
func runSystemInstallation(ctx context.Context, config, name string, check bool, stdout, stderr io.Writer) error {
	if os.Geteuid() != 0 || len(name) < 3 || len(name) > 32 || name[0] < 'a' || name[0] > 'z' || name == "root" {
		return errors.New("-system-user requires root and an explicit dedicated account")
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return errInstallation
		}
	}
	d, _, e := installationReadConfig(config)
	if e != nil {
		return e
	}
	home := "/var/lib/" + name
	if filepath.Dir(d.Directory) != home {
		return errors.New("system installation directory must be a direct child of /var/lib/<service-user>")
	}
	if e = installationSystemctl(ctx, false, "show-environment"); e != nil {
		return e
	}
	if check {
		return json.NewEncoder(stdout).Encode(map[string]any{"state": "SYSTEM_PREFLIGHT_OK", "component": d.Provision.Component, "user": name, "changesMade": false})
	}
	u, e := user.Lookup(name)
	if _, missing := e.(user.UnknownUserError); missing {
		cmd := exec.CommandContext(ctx, "/usr/sbin/useradd", "--system", "--user-group", "--home-dir", home, "--create-home", "--shell", "/usr/sbin/nologin", name)
		cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/bin", "LANG=C.UTF-8"}
		if cmd.Run() != nil {
			return errors.New("cannot create dedicated service account; no existing account was modified")
		}
		u, e = user.Lookup(name)
		if e == nil {
			if e = os.Chmod(home, 0700); e != nil {
				return e
			}
		}
	}
	if e != nil {
		return e
	}
	uid, e := strconv.ParseUint(u.Uid, 10, 32)
	if e != nil || uid == 0 || u.HomeDir != home {
		return errInstallation
	}
	gid, e := strconv.ParseUint(u.Gid, 10, 32)
	if e != nil {
		return e
	}
	i, e := os.Lstat(home)
	if e != nil || !i.IsDir() || i.Mode().Perm() != 0700 || i.Mode()&os.ModeSymlink != 0 {
		return errors.New("dedicated service home must be owned by its service account with mode 0700")
	}
	st, ok := i.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(uid) {
		return errInstallation
	}
	anchor := "/var/lib/serverfleet-installers"
	if e = os.Mkdir(anchor, 0711); e == nil {
		if e = os.Chmod(anchor, 0711); e != nil {
			return e
		}
	} else if !os.IsExist(e) {
		return e
	}
	anchorInfo, e := os.Lstat(anchor)
	if e != nil || !anchorInfo.IsDir() || anchorInfo.Mode().Perm() != 0711 || anchorInfo.Sys().(*syscall.Stat_t).Uid != 0 {
		return errInstallation
	}
	inputs := filepath.Join(anchor, name+"-"+d.Provision.Component)
	if e = os.Mkdir(inputs, 0700); e == nil {
		if e = os.Chown(inputs, int(uid), int(gid)); e != nil {
			return e
		}
	} else if !os.IsExist(e) {
		return e
	}
	inputInfo, e := os.Lstat(inputs)
	if e != nil || !inputInfo.IsDir() || inputInfo.Mode().Perm() != 0700 || inputInfo.Sys().(*syscall.Stat_t).Uid != uint32(uid) {
		return errInstallation
	}
	root, e := os.OpenRoot(inputs)
	if e != nil {
		return e
	}
	defer root.Close()
	ownedWrite := func(filename string, raw []byte, mode os.FileMode) error {
		if old, e := root.OpenFile(filename, os.O_RDONLY|syscall.O_NOFOLLOW, 0); e == nil {
			defer old.Close()
			i, e := old.Stat()
			if e != nil || !i.Mode().IsRegular() || i.Mode().Perm() != mode {
				return errInstallation
			}
			st, ok := i.Sys().(*syscall.Stat_t)
			if !ok || st.Uid != uint32(uid) || st.Nlink != 1 {
				return errInstallation
			}
			b, e := io.ReadAll(io.LimitReader(old, int64(len(raw))+1))
			if e != nil || !bytes.Equal(b, raw) {
				return errInstallation
			}
			return nil
		} else if !os.IsNotExist(e) {
			return e
		}
		f, e := root.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if e != nil {
			return e
		}
		defer f.Close()
		if e = f.Chown(int(uid), int(gid)); e != nil {
			return e
		}
		if _, e = f.Write(raw); e != nil {
			return e
		}
		return f.Sync()
	}
	if d.Provision.Component == "agent" {
		tokenPath := filepath.Join(inputs, "enroll-token.txt")
		if _, e = root.Lstat("enroll-token.txt"); os.IsNotExist(e) {
			var token []byte
			if d.EnrollmentTokenFile != "" {
				token, e = recovery.ReadPrivateFile(d.EnrollmentTokenFile, 4096)
			} else {
				token, e = installationReadToken(ctx)
			}
			if e != nil {
				return e
			}
			defer clear(token)
			if e = ownedWrite("enroll-token.txt", token, 0600); e != nil {
				return e
			}
		}
		d.EnrollmentTokenFile = tokenPath
	}
	d.ServiceMode = "external"
	raw, _ := json.Marshal(d)
	if e = ownedWrite("install.json", raw, 0600); e != nil {
		return e
	}
	self, e := os.Executable()
	if e != nil {
		return e
	}
	f, e := openProvisionProgram(self)
	if e != nil {
		return e
	}
	b, e := io.ReadAll(io.LimitReader(f, 256<<20))
	f.Close()
	if e != nil {
		return e
	}
	if e = ownedWrite("controller-installer", b, 0700); e != nil {
		return e
	}
	cmd := exec.CommandContext(ctx, filepath.Join(inputs, "controller-installer"), "install", "-config", filepath.Join(inputs, "install.json"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{uint32(gid)}}}
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + home, "LANG=C.UTF-8"}
	var result bytes.Buffer
	cmd.Stdout = &result
	cmd.Stderr = stderr
	if e = cmd.Run(); e != nil {
		return errInstallation
	}
	var state struct {
		State string `json:"state"`
	}
	if result.Len() > 32768 || json.Unmarshal(result.Bytes(), &state) != nil {
		return errInstallation
	}
	if state.State == "WAITING_FOR_NODE_APPROVAL" {
		_, e = stdout.Write(result.Bytes())
		return e
	}
	if state.State != "PREPARED_SYSTEM_SERVICE_REQUIRED" {
		return errInstallation
	}
	runtimeDir := filepath.Join(d.Directory, "runtime")
	unitName := "serverfleet-" + d.Provision.Component + ".service"
	unit := []byte("[Unit]\nDescription=Server Fleet " + d.Provision.Component + " owned supervisor\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nUser=" + name + "\nGroup=" + u.Gid + "\nWorkingDirectory=" + runtimeDir + "\nExecStart=" + runtimeDir + "/start\nUMask=0077\nNoNewPrivileges=yes\nRestart=no\nTimeoutStopSec=15\n\n[Install]\nWantedBy=multi-user.target\n")
	// Never replace an unrelated existing unit or touch firewall/sudoers.
	unitDir, e := os.OpenRoot("/etc/systemd/system")
	if e != nil {
		return e
	}
	defer unitDir.Close()
	if i, e := unitDir.Lstat(unitName); e == nil {
		st, ok := i.Sys().(*syscall.Stat_t)
		if !i.Mode().IsRegular() || i.Mode().Perm() != 0644 || !ok || st.Uid != 0 || st.Nlink != 1 {
			return errInstallation
		}
		old, e := unitDir.ReadFile(unitName)
		if e != nil || !bytes.Equal(old, unit) {
			return errors.New("service name already exists with different configuration; it was not overwritten")
		}
	} else if os.IsNotExist(e) {
		f, e := unitDir.OpenFile(unitName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if e != nil {
			return e
		}
		if e = f.Chmod(0644); e != nil {
			f.Close()
			return e
		}
		_, we := f.Write(unit)
		se, ce := f.Sync(), f.Close()
		if we != nil || se != nil || ce != nil {
			return errInstallation
		}
	} else {
		return e
	}
	if e = installationSystemctl(ctx, false, "daemon-reload"); e != nil {
		return e
	}
	if e = installationSystemctl(ctx, false, "enable", "--now", unitName); e != nil {
		return e
	}
	verify := exec.CommandContext(ctx, filepath.Join(inputs, "controller-installer"), "install", "-config", filepath.Join(inputs, "install.json"), "-verify-health")
	verify.SysProcAttr = cmd.SysProcAttr
	verify.Env = cmd.Env
	verify.Stdout = stdout
	verify.Stderr = stderr
	if e = verify.Run(); e != nil {
		return errInstallation
	}
	return nil
}

func installationProcessMatches(pid int, digest string) bool {
	if pid <= 1 || syscall.Kill(pid, 0) != nil {
		return false
	}
	f, e := os.Open("/proc/" + strconv.Itoa(pid) + "/exe")
	if e != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, (256<<20)+1))
	return e == nil && n > 0 && n <= 256<<20 && hex.EncodeToString(h.Sum(nil)) == digest
}
