package server

import "serverfleet.local/serverfleet/internal/files"

// LoadFileExecutionConfig is the trusted CLI/host startup port. A private,
// owner-checked registry is loaded once. It never adds a ScopeGate; non-Web
// execution remains denied. The caller must fail startup if this returns error.
func LoadFileExecutionConfig(filename string) (*FileExecutionConfig, error) {
	paths, spool, err := files.LoadDeployment(filename)
	if err != nil {
		return nil, err
	}
	return &FileExecutionConfig{Paths: paths, SpoolDir: spool}, nil
}
