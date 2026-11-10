//go:build linux

package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfflineActivationRecordsRemainExclusiveAndPathBounded(t *testing.T) {
	dir := privateQuarantineDirectory(t)
	value := map[string]string{"state": "synthetic-authorized-receipt"}
	for _, name := range []string{"activation-attempt.json", "activation.json"} {
		if e := WriteOfflineRecord(dir, name, value); e != nil {
			t.Fatal(name, e)
		}
		if e := WriteOfflineRecord(dir, name, value); e == nil {
			t.Fatal("overwrote", name)
		}
	}
	for _, name := range []string{"hold.json", "restored.db", "../activation.json", "nested/activation.json", "activation.json/", "authorization-" + strings.Repeat("a", 64) + ".json"} {
		if e := WriteOfflineRecord(dir, name, value); e == nil {
			t.Fatal("unsafe or misbound record", name)
		}
	}
	// Valid signed revocation lists can exceed the 16KiB receipt limit. Only
	// content-addressed authorization documents get the bounded 128KiB limit.
	raw, e := json.Marshal(map[string]string{"syntheticDocument": strings.Repeat("x", 17<<10)})
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(raw)
	name := "authorization-" + hex.EncodeToString(sum[:]) + ".json"
	if e = WriteOfflineRecord(dir, name, json.RawMessage(raw)); e != nil {
		t.Fatal(e)
	}
	if saved, e := os.ReadFile(filepath.Join(dir, name)); e != nil || string(saved) != string(raw) {
		t.Fatal("authorization bytes changed", e)
	}
	if e = WriteOfflineRecord(dir, "reconciled.json", json.RawMessage(raw)); e == nil {
		t.Fatal("large ordinary receipt accepted")
	}
	raw, _ = json.Marshal(map[string]string{"oversized": strings.Repeat("x", 128<<10)})
	sum = sha256.Sum256(raw)
	if e = WriteOfflineRecord(dir, "authorization-"+hex.EncodeToString(sum[:])+".json", json.RawMessage(raw)); e == nil {
		t.Fatal("oversized authorization accepted")
	}
}
