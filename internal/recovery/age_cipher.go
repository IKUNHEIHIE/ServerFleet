package recovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// The optional executable is operator-provisioned and hash-pinned. Only native
// age X25519 recipients/identities are accepted; plugins, SSH and passphrases
// are unavailable. Primary CLI contract: FiloSottile/age v1.3.2/doc/age.1.html.
const AgeVersion = "1.3.2"

var ErrCipherUnavailable = errors.New("recovery: pinned age cipher is unavailable or rejected")

type AgeCipher struct{ binary, digest string }

func NewAgeCipher(binary, digest string) (*AgeCipher, error) {
	if runtime.GOOS != "linux" || !filepath.IsAbs(binary) || filepath.Clean(binary) != binary || !validDigest(digest) {
		return nil, ErrCipherUnavailable
	}
	c := &AgeCipher{binary, digest}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var version bytes.Buffer
	if err := c.run(ctx, []string{"--version"}, nil, nil, &version); err != nil || strings.TrimSpace(version.String()) != AgeVersion {
		return nil, ErrCipherUnavailable
	}
	return c, nil
}
func validAgeRecipient(value string) bool {
	if len(value) != 62 || !strings.HasPrefix(value, "age1") {
		return false
	}
	const alphabet = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"
	for _, r := range value[4:] {
		if !strings.ContainsRune(alphabet, r) {
			return false
		}
	}
	return true
}
func (c *AgeCipher) openProgram() (*os.File, error) {
	info, err := os.Lstat(c.binary)
	if err != nil || !privateRecoveryProgram(info) || info.Size() <= 0 || info.Size() > 128<<20 {
		return nil, ErrCipherUnavailable
	}
	file, err := os.Open(c.binary)
	if err != nil {
		return nil, ErrCipherUnavailable
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !privateRecoveryProgram(opened) {
		file.Close()
		return nil, ErrCipherUnavailable
	}
	h := sha256.New()
	_, err = io.Copy(h, io.LimitReader(file, (128<<20)+1))
	if err != nil || hex.EncodeToString(h.Sum(nil)) != c.digest {
		file.Close()
		return nil, ErrCipherUnavailable
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, ErrCipherUnavailable
	}
	return file, nil
}

type discardCipherErrors struct{}

func (discardCipherErrors) Write(raw []byte) (int, error) { return len(raw), nil }
func (c *AgeCipher) run(ctx context.Context, args []string, identity *os.File, input io.Reader, output io.Writer) error {
	if c == nil || ctx == nil || ctx.Err() != nil || output == nil {
		return ErrCipherUnavailable
	}
	program, err := c.openProgram()
	if err != nil {
		return err
	}
	defer program.Close()
	command := exec.CommandContext(ctx, "/proc/self/fd/3", args...)
	command.ExtraFiles = []*os.File{program}
	if identity != nil {
		command.ExtraFiles = append(command.ExtraFiles, identity)
	}
	command.Env = []string{"PATH=/nonexistent", "LANG=C", "LC_ALL=C"}
	command.Stdin = input
	command.Stdout = output
	command.Stderr = discardCipherErrors{}
	command.WaitDelay = 2 * time.Second
	configureRecoveryProcess(command)
	if command.Run() != nil {
		return ErrCipherUnavailable
	}
	return nil
}
func (c *AgeCipher) Encrypt(ctx context.Context, recipient string, input io.Reader, output io.Writer) error {
	if !validAgeRecipient(recipient) || input == nil {
		return ErrCipherUnavailable
	}
	return c.run(ctx, []string{"--encrypt", "--recipient", recipient}, nil, input, output)
}

// DecryptOffline is invoked solely by an explicit local recovery CLI. The
// recovery identity is never an HTTP field, task parameter, archive or log.
func (c *AgeCipher) DecryptOffline(ctx context.Context, identityPath string, input io.Reader, output io.Writer) error {
	if !filepath.IsAbs(identityPath) || input == nil {
		return ErrCipherUnavailable
	}
	dir, err := openRecoveryDirectory(filepath.Dir(identityPath))
	if err != nil {
		return err
	}
	defer dir.Close()
	name := filepath.Base(identityPath)
	info, err := dir.Lstat(name)
	if err != nil || !privateRecoveryInfo(info, false) || info.Size() > 16<<10 {
		return ErrCipherUnavailable
	}
	identity, err := dir.Open(name)
	if err != nil {
		return ErrCipherUnavailable
	}
	defer identity.Close()
	after, err := identity.Stat()
	if err != nil || !os.SameFile(info, after) {
		return ErrCipherUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(identity, (16<<10)+1))
	if err != nil || len(raw) > 16<<10 {
		clear(raw)
		return ErrCipherUnavailable
	}
	keys := 0
	valid := true
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		if !bytes.HasPrefix(line, []byte("AGE-SECRET-KEY-1")) || len(line) > 256 {
			valid = false
			break
		}
		keys++
	}
	clear(raw)
	if !valid || keys != 1 {
		return ErrCipherUnavailable
	}
	if _, err = identity.Seek(0, io.SeekStart); err != nil {
		return ErrCipherUnavailable
	}
	return c.run(ctx, []string{"--decrypt", "--identity", "/proc/self/fd/4"}, identity, input, output)
}
