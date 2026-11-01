package updates

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// DirectoryReleaseSource supplies pre-provisioned signed artifacts from a
// private offline inbox. No network URL, shell or trust key comes from a client.
// The stager verifies signatures/platform/floor/size/hash before returning any
// executable handle. Trust rotation and feed acquisition belong to composition.
type DirectoryReleaseSource struct {
	root   *os.Root
	stager *ArtifactStager
}

func NewDirectoryReleaseSource(directory string, stager *ArtifactStager) (*DirectoryReleaseSource, error) {
	if stager == nil || !filepath.IsAbs(directory) {
		return nil, ErrArtifactRejected
	}
	i, e := os.Lstat(directory)
	if e != nil || !i.IsDir() || i.Mode()&os.ModeSymlink != 0 || runtime.GOOS == "linux" && i.Mode().Perm()&0077 != 0 {
		return nil, ErrArtifactRejected
	}
	r, e := os.OpenRoot(directory)
	if e != nil {
		return nil, ErrArtifactRejected
	}
	return &DirectoryReleaseSource{r, stager}, nil
}
func (s *DirectoryReleaseSource) Close() error { return s.root.Close() }
func (s *DirectoryReleaseSource) open(name string, max int64) (*os.File, error) {
	i, e := s.root.Lstat(name)
	if e != nil || !i.Mode().IsRegular() || i.Size() <= 0 || i.Size() > max {
		return nil, ErrArtifactRejected
	}
	f, e := s.root.Open(name)
	if e != nil {
		return nil, ErrArtifactRejected
	}
	opened, e := f.Stat()
	if e != nil || !os.SameFile(i, opened) {
		f.Close()
		return nil, ErrArtifactRejected
	}
	return f, nil
}
func (s *DirectoryReleaseSource) Stage(ctx context.Context, version, keyID string, policy ReleasePolicy) (*StagedArtifact, error) {
	if s == nil || ctx == nil || !versionPattern.MatchString(version) || policy.MaxBytes <= 0 {
		return nil, ErrArtifactRejected
	}
	info, err := s.root.Lstat(version)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrArtifactRejected
	}
	read := func(name string, max int64) ([]byte, error) {
		f, e := s.open(version+"/"+name, max)
		if e != nil {
			return nil, e
		}
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, max+1))
		if e != nil || int64(len(b)) > max {
			return nil, ErrArtifactRejected
		}
		return b, nil
	}
	raw, err := read("manifest.json", 16<<10)
	if err != nil {
		return nil, err
	}
	signature, err := read("signature.bin", 64)
	if err != nil {
		return nil, err
	}
	artifact, err := s.open(version+"/artifact", policy.MaxBytes)
	if err != nil {
		return nil, err
	}
	defer artifact.Close()
	staged, err := s.stager.Stage(ctx, raw, signature, keyID, artifact, policy)
	if err != nil {
		return nil, err
	}
	if staged.Manifest().Version != version {
		staged.Close()
		return nil, ErrArtifactRejected
	}
	return staged, nil
}
