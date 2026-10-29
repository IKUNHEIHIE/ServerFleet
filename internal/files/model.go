package files

import (
	"errors"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalidPath       = errors.New("files: invalid remote path")
	ErrSymlinkForbidden  = errors.New("files: following symlinks is not allowed")
	ErrInvalidTransfer   = errors.New("files: invalid transfer request")
	ErrTransferNotFound  = errors.New("files: transfer not found")
	ErrUploadNotFound    = errors.New("files: upload not found")
	ErrUploadClosed      = errors.New("files: upload is not open")
	ErrOffsetConflict    = errors.New("files: upload offset conflict")
	ErrUploadTooLarge    = errors.New("files: upload exceeds size limit")
	ErrChecksumMismatch  = errors.New("files: checksum mismatch")
	ErrSubjectMismatch   = errors.New("files: upload belongs to another subject")
	ErrTransportAbsent   = errors.New("files: transport port is not configured")
	ErrTaskSubmitter     = errors.New("files: task submitter is not configured")
	ErrRequestIDRequired = errors.New("files: request id is required")
)

// ConflictPolicy matches the candidate contract.
type ConflictPolicy string

const (
	ConflictSkip      ConflictPolicy = "skip"
	ConflictOverwrite ConflictPolicy = "overwrite"
	ConflictRename    ConflictPolicy = "rename"
)

func (p ConflictPolicy) Valid() bool {
	switch p {
	case ConflictSkip, ConflictOverwrite, ConflictRename:
		return true
	default:
		return false
	}
}

// TransferStatus mirrors the API lifecycle.
type TransferStatus string

const (
	TransferQueued         TransferStatus = "queued"
	TransferRunning        TransferStatus = "running"
	TransferCompleted      TransferStatus = "completed"
	TransferPartialFailure TransferStatus = "partial_failure"
	TransferFailed         TransferStatus = "failed"
	TransferCancelling     TransferStatus = "cancelling"
	TransferCanceled       TransferStatus = "cancelled"
)

func (s TransferStatus) Valid() bool {
	switch s {
	case TransferQueued, TransferRunning, TransferCompleted, TransferPartialFailure,
		TransferFailed, TransferCancelling, TransferCanceled:
		return true
	default:
		return false
	}
}

func (s TransferStatus) Terminal() bool {
	switch s {
	case TransferCompleted, TransferPartialFailure, TransferFailed, TransferCanceled:
		return true
	default:
		return false
	}
}

// ItemStatus is one source path result reported by the agent.
type ItemStatus string

const (
	ItemQueued    ItemStatus = "queued"
	ItemCopying   ItemStatus = "copying"
	ItemCopied    ItemStatus = "copied"
	ItemSkipped   ItemStatus = "skipped"
	ItemFailed    ItemStatus = "failed"
	ItemCancelled ItemStatus = "cancelled"
)

func (s ItemStatus) Valid() bool {
	switch s {
	case ItemQueued, ItemCopying, ItemCopied, ItemSkipped, ItemFailed, ItemCancelled:
		return true
	default:
		return false
	}
}

type TransferItem struct {
	Sequence        int
	SourcePath      string
	DestinationPath string
	Status          ItemStatus
	BytesCopied     int64
	ErrorCode       string
	ErrorMessage    string
}

type Transfer struct {
	ID                string
	TaskID            string
	SubmitterID       string
	SourceNodeID      string
	DestinationNodeID string
	DestinationDir    string
	ConflictPolicy    ConflictPolicy
	FollowSymlinks    bool
	DryRun            bool
	Status            TransferStatus
	Progress          float64
	TotalBytes        *int64
	CopiedBytes       int64
	ErrorMessage      string
	RequestID         string
	Items             []TransferItem
	CreatedAt         time.Time
	UpdatedAt         time.Time
	StartedAt         time.Time
	CompletedAt       time.Time
}

// CreateTransferInput is the trusted request. Checksums are optional expected
// SHA-256 digests per source path.
type CreateTransferInput struct {
	SubjectID         string
	RequestID         string
	SourceNodeID      string
	DestinationNodeID string
	SourcePaths       []string
	DestinationDir    string
	ConflictPolicy    ConflictPolicy
	FollowSymlinks    bool
	DryRun            bool
	Checksums         map[string]string
}

type Upload struct {
	ID              string
	SubjectID       string
	NodeID          string
	DestinationPath string
	Status          string
	BytesReceived   int64
	ExpectedSize    *int64
	SHA256Expected  string
	SHA256Actual    string
	ErrorCode       string
	StagingName     string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidateRemotePath enforces an absolute POSIX file path without traversal,
// duplicate separators, NUL/control characters, backslashes or trailing slashes.
func ValidateRemotePath(value string) error {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) {
		return ErrInvalidPath
	}
	if !strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || strings.Contains(value, "\x00") {
		return ErrInvalidPath
	}
	if strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
	}) {
		return ErrInvalidPath
	}
	if value == "/" {
		return nil
	}
	if strings.HasSuffix(value, "/") {
		return ErrInvalidPath
	}
	segments := strings.Split(value, "/")
	for index, segment := range segments {
		if index == 0 {
			continue
		}
		if segment == "" || segment == "." || segment == ".." {
			return ErrInvalidPath
		}
	}
	return nil
}

// ValidateDirectoryPath is ValidateRemotePath for a directory (root allowed).
func ValidateDirectoryPath(value string) error {
	return ValidateRemotePath(value)
}

// JoinRemotePath joins a validated directory and a single file name. It never
// escapes the directory because the name is validated as one path segment.
func JoinRemotePath(directory, name string) (string, error) {
	if err := ValidateDirectoryPath(directory); err != nil {
		return "", err
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") ||
		!utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl) {
		return "", ErrInvalidPath
	}
	return path.Join(directory, name), nil
}

func baseName(value string) string {
	return path.Base(value)
}
