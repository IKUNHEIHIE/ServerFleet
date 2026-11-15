package files

import "testing"

// Transfer staging directories live inside the Agent file root but are
// machinery, not operator content; the console must not list them.
func TestStagingEntryNameHiddenFromListing(t *testing.T) {
	for _, name := range []string{".sf-file-38b5c3740da095ef1708e050a5b8e0d70653f523", ".sf-x"} {
		if !stagingEntryName(name) {
			t.Fatalf("%q must be recognised as staging", name)
		}
	}
	for _, name := range []string{"node-142-test.txt", "logs", ".sfx", "sf-file", ".hidden", "a.sf-file"} {
		if stagingEntryName(name) {
			t.Fatalf("%q must not be treated as staging", name)
		}
	}
}
