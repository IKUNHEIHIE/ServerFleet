package files

import "testing"

// Console-facing entry paths must be directly reusable as request paths
// (delete, size, transfer source, navigation). The root listing used to emit
// "//name", which every consumer then rejected.
func TestEntryPathIsReusableAsRequestPath(t *testing.T) {
	// The Agent echoes the request path, so dir is always "/"-rooted here; those
	// are the cases every consumer reuses as a request path.
	for _, tc := range []struct{ dir, name, want string }{
		{"/", "a.txt", "/a.txt"},
		{"/", "logs", "/logs"},
		{"/logs", "sample.log", "/logs/sample.log"},
		{"/a/b", "c.bin", "/a/b/c.bin"},
	} {
		got := entryPath(tc.dir, tc.name)
		if got != tc.want {
			t.Fatalf("entryPath(%q, %q) = %q; want %q", tc.dir, tc.name, got, tc.want)
		}
		if err := ValidateRemotePath(got); err != nil {
			t.Fatalf("entryPath(%q, %q) = %q is not a valid request path: %v", tc.dir, tc.name, got, err)
		}
	}
	// Defensive relative forms stay relative.
	for _, tc := range []struct{ dir, name, want string }{
		{"", "a.txt", "a.txt"},
		{".", "a.txt", "a.txt"},
	} {
		if got := entryPath(tc.dir, tc.name); got != tc.want {
			t.Fatalf("entryPath(%q, %q) = %q; want %q", tc.dir, tc.name, got, tc.want)
		}
	}
}
