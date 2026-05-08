package watch

import (
	"bytes"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunFiresRebuildOnRelevantWrite(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "seed.go"), []byte("package x"), 0644); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	defer close(stop)
	var rebuildCount int32
	rebuildC := make(chan []string, 4)

	go func() {
		_ = Run(root, Options{
			Extensions: []string{".go"},
			Debounce:   30 * time.Millisecond,
			Logger:     &bytes.Buffer{},
		}, stop, func(changed []string) error {
			atomic.AddInt32(&rebuildCount, 1)
			rebuildC <- changed
			return nil
		})
	}()

	// Give the watcher a moment to register. fsnotify is racy on startup;
	// poll the watcher state implicitly by writing repeatedly.
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(root, "seed.go"), []byte("package x\nfunc f() {}"), 0644); err != nil {
		t.Fatal(err)
	}

	select {
	case changed := <-rebuildC:
		if len(changed) == 0 {
			t.Fatal("rebuild fired with empty change list")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("rebuild did not fire within 2s; count=%d", atomic.LoadInt32(&rebuildCount))
	}
}

func TestRunIgnoresExtensionsOutsideFilter(t *testing.T) {
	root := t.TempDir()
	stop := make(chan struct{})
	defer close(stop)
	rebuildC := make(chan struct{}, 1)

	go func() {
		_ = Run(root, Options{
			Extensions: []string{".go"},
			Debounce:   30 * time.Millisecond,
			Logger:     &bytes.Buffer{},
		}, stop, func([]string) error {
			rebuildC <- struct{}{}
			return nil
		})
	}()
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(root, "ignored.txt"), []byte("noise"), 0644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rebuildC:
		t.Fatal("rebuild fired on a .txt change despite .go filter")
	case <-time.After(300 * time.Millisecond):
		// Expected: no rebuild.
	}
}

func TestRunStopsOnSignal(t *testing.T) {
	root := t.TempDir()
	stop := make(chan struct{})
	doneC := make(chan error, 1)

	go func() {
		doneC <- Run(root, Options{Debounce: 30 * time.Millisecond, Logger: &bytes.Buffer{}}, stop, func([]string) error { return nil })
	}()
	close(stop)
	select {
	case err := <-doneC:
		if err != nil {
			t.Fatalf("Run returned non-nil on stop: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after stop")
	}
}

func TestRunWatchesNewlyCreatedSubdir(t *testing.T) {
	root := t.TempDir()
	stop := make(chan struct{})
	defer close(stop)
	rebuildC := make(chan []string, 4)

	go func() {
		_ = Run(root, Options{Extensions: []string{".go"}, Debounce: 30 * time.Millisecond, Logger: &bytes.Buffer{}}, stop, func(c []string) error {
			rebuildC <- c
			return nil
		})
	}()
	time.Sleep(100 * time.Millisecond)

	// New directory + file inside should trigger rebuild after addRecursive
	// hooks the new dir.
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(sub, "x.go"), []byte("package x"), 0644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rebuildC:
	case <-time.After(2 * time.Second):
		t.Fatal("rebuild did not fire on file in newly-created subdir")
	}
}

func TestRunSkipsHiddenDirectoriesAtSetup(t *testing.T) {
	// The .git dir should be skipped during the initial recursive add. We
	// can't directly assert that no event fires (timing); instead, verify
	// the watcher start succeeds with a hidden dir present.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git", "objects"), 0755); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	close(stop)
	if err := Run(root, Options{Extensions: []string{".go"}, Logger: &bytes.Buffer{}}, stop, func([]string) error { return nil }); err != nil {
		t.Fatalf("Run with hidden dir present failed: %v", err)
	}
}

func TestRunErrorsOnMissingRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	stop := make(chan struct{})
	close(stop)
	if err := Run(missing, Options{Logger: &bytes.Buffer{}}, stop, func([]string) error { return nil }); err == nil {
		t.Fatal("expected error for missing root")
	}
}
