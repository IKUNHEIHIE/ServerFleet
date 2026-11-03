package platform_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// productBuilds caches actual product release binaries compiled once per test
// binary from the repository's formal apps/controller and apps/agent packages.
// Tests keep the explicit SF_* prebuilt-binary overrides; when those are absent
// the real products are built here so the unified flows are always exercised
// with production builds, never fixtures or mocks.
type productBuildCache struct {
	once       sync.Once
	err        error
	controller string
	agent      string
	agentV1    string
	agentV2    string
	logs       []string
}

var products productBuildCache

func productModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("module root not found from %s", dir)
		}
		dir = parent
	}
}

func buildProduct(dir, target, output, version string) (string, string, error) {
	args := []string{"build", "-trimpath", "-o", output}
	if version != "" {
		args = append(args, "-ldflags", "-X=main.agentBuildVersion="+version)
	}
	args = append(args, target)
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", string(out), fmt.Errorf("go build %s: %w: %s", target, err, string(out))
	}
	data, err := os.ReadFile(output)
	if err != nil {
		return "", "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), "", nil
}

// buildActualProducts compiles the four real release binaries. The controller
// and plain agent have no version stamp; agentV1/agentV2 are stamped 1.0.0 and
// 1.0.1 exactly as the update-flow tests expect the running products to report.
func buildActualProducts(t *testing.T) (controller, agent, agentV1, agentV2 string) {
	t.Helper()
	products.once.Do(func() {
		root, err := productModuleRoot()
		if err != nil {
			products.err = err
			return
		}
		dir := filepath.Join(root, "reports", "product-builds")
		if err = os.MkdirAll(dir, 0o755); err != nil {
			products.err = err
			return
		}
		products.controller = filepath.Join(dir, "controller")
		products.agent = filepath.Join(dir, "agent")
		products.agentV1 = filepath.Join(dir, "agent-1.0.0")
		products.agentV2 = filepath.Join(dir, "agent-1.0.1")
		for _, build := range []struct{ output, target, version string }{
			{products.controller, "./apps/controller", ""},
			{products.agent, "./apps/agent", ""},
			{products.agentV1, "./apps/agent", "1.0.0"},
			{products.agentV2, "./apps/agent", "1.0.1"},
		} {
			digest, buildLog, err := buildProduct(root, build.target, build.output, build.version)
			if err != nil {
				products.err = fmt.Errorf("%v: %s", err, buildLog)
				return
			}
			products.logs = append(products.logs, fmt.Sprintf(
				"go build -trimpath -o %s %s (version %q): sha256=%s", build.output, build.target, build.version, digest))
		}
	})
	if products.err != nil {
		t.Fatalf("actual product release builds failed: %v", products.err)
	}
	for _, line := range products.logs {
		t.Log(line)
	}
	return products.controller, products.agent, products.agentV1, products.agentV2
}

func explicitBinary(env, name string) (string, bool) {
	value := os.Getenv(env)
	if filepath.IsAbs(value) {
		if _, err := os.Stat(value); err == nil {
			return value, true
		}
	}
	return "", false
}
