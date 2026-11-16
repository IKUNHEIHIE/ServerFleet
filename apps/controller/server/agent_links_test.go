package server

import (
	"testing"

	"serverfleet.local/serverfleet/internal/transport"
)

func TestAgentFrameLimitsFitADirectoryListing(t *testing.T) {
	limits := agentFrameLimits()
	// Each FileEntry contributes several fields plus the reply envelope itself.
	if want := transport.MaxFileOpEntries*5 + 16; limits.MaxFields < want {
		t.Fatalf("MaxFields=%d cannot carry %d directory entries", limits.MaxFields, transport.MaxFileOpEntries)
	}
}
