package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestFileNoFollowRejectsConcurrentParentSymlinkSwap(t *testing.T) {
	dir := privateAgentDirectory(t)
	os.Mkdir(filepath.Join(dir, "permitted"), 0700)
	os.Mkdir(filepath.Join(dir, "sibling"), 0700)
	os.WriteFile(filepath.Join(dir, "permitted", "data"), []byte("authorized"), 0600)
	os.WriteFile(filepath.Join(dir, "sibling", "data"), []byte("forbidden sibling"), 0600)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if os.Rename(filepath.Join(dir, "permitted"), filepath.Join(dir, "hold")) == nil {
				os.Symlink("sibling", filepath.Join(dir, "permitted"))
				os.Remove(filepath.Join(dir, "permitted"))
				os.Rename(filepath.Join(dir, "hold"), filepath.Join(dir, "permitted"))
			}
		}
	}()
	defer func() { close(stop); wg.Wait() }()
	for i := 0; i < 1000; i++ {
		file, e := agentFileOpen(root, "permitted/data", os.O_RDONLY, 0)
		if e != nil {
			continue
		}
		var data [64]byte
		n, _ := file.Read(data[:])
		file.Close()
		if string(data[:n]) != "authorized" {
			t.Fatal("symlink race redirected source bytes")
		}
	}
}
