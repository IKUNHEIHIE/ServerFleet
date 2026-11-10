package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

func openRecoveryDirectory(directory string) (*os.Root, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, ErrRecoveryRejected
	}
	info, err := os.Lstat(directory)
	if err != nil || !privateRecoveryInfo(info, true) {
		return nil, ErrRecoveryRejected
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, ErrRecoveryRejected
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, ErrRecoveryRejected
	}
	return root, nil
}
func readRecoveryFile(root *os.Root, name string, limit int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil || !privateRecoveryInfo(info, false) || info.Size() < 0 || info.Size() > limit {
		return nil, ErrRecoveryRejected
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, ErrRecoveryRejected
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !privateRecoveryInfo(opened, false) {
		return nil, ErrRecoveryRejected
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(raw)) != info.Size() {
		clear(raw)
		return nil, ErrRecoveryRejected
	}
	return raw, nil
}
func writeRecoveryFile(root *os.Root, name string, raw []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrRecoveryRejected
	}
	_, err = file.Write(raw)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return ErrRecoveryRejected
	}
	return syncRecoveryRoot(root)
}

// ReadPrivateFile never follows a caller-selected archive path. It is used for
// trusted startup and explicit offline CLI files, not HTTP filesystem access.
func ReadPrivateFile(path string, limit int64) ([]byte, error) {
	if !filepath.IsAbs(path) || limit <= 0 {
		return nil, ErrRecoveryRejected
	}
	root, err := openRecoveryDirectory(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return readRecoveryFile(root, filepath.Base(path), limit)
}
func FreezeBackupFile(name, path string) (BackupFileSource, error) {
	if !allowedBundleName(name) || name == "controller.db" {
		return BackupFileSource{}, ErrBackupUnavailable
	}
	raw, err := ReadPrivateFile(path, 128<<10)
	if err != nil {
		return BackupFileSource{}, err
	}
	defer clear(raw)
	hash := sha256.Sum256(raw)
	return BackupFileSource{Name: name, Path: path, SHA256: hex.EncodeToString(hash[:]), MaxBytes: 128 << 10}, nil
}
func CheckBackupFile(file BackupFileSource) error {
	if len(file.Inline) > 0 {
		hash := sha256.Sum256(file.Inline)
		if file.Name != "configuration/controller.json" || !json.Valid(file.Inline) || file.Path != "" || hex.EncodeToString(hash[:]) != file.SHA256 {
			return ErrBackupUnavailable
		}
		return nil
	}
	current, err := FreezeBackupFile(file.Name, file.Path)
	if err != nil || current.SHA256 != file.SHA256 {
		return ErrBackupUnavailable
	}
	return nil
}

func syncRecoveryRoot(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return ErrRecoveryRejected
	}
	defer directory.Close()
	if directory.Sync() != nil {
		return ErrRecoveryRejected
	}
	return nil
}

func FreezeControllerConfiguration(raw []byte) (BackupFileSource, error) {
	if len(raw) == 0 || len(raw) > 128<<10 || !json.Valid(raw) {
		return BackupFileSource{}, ErrBackupUnavailable
	}
	hash := sha256.Sum256(raw)
	return BackupFileSource{Name: "configuration/controller.json", Inline: append([]byte{}, raw...), SHA256: hex.EncodeToString(hash[:]), MaxBytes: 128 << 10}, nil
}
