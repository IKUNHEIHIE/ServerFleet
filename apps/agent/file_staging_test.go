package main

import "testing"

func TestAgentStagingDirStaysInsideTheDestinationDirectory(t *testing.T) {
	for _, test := range []struct{ relative, want string }{
		{"hostname", ".sf-xfer-1"},
		{"tmp/hostname", "tmp/.sf-xfer-1"},
		{"data/site/a.txt", "data/site/.sf-xfer-1"},
	} {
		if got := agentStagingDir(test.relative, "xfer-1"); got != test.want {
			t.Fatalf("agentStagingDir(%q) = %q, want %q", test.relative, got, test.want)
		}
	}
}
