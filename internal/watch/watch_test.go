package watch

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

func TestRunSkipsHeavyDirsAtSetup(t *testing.T) {
	// node_modules/ + vendor/ etc. are project-scale machine-generated trees
	// that easily exceed the OS inotify limit. Setup must skip them.
	root := t.TempDir()
	for _, d := range []string{"node_modules", "vendor", "target"} {
		if err := os.MkdirAll(filepath.Join(root, d, "deep"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, d, "deep", "noisy.go"), []byte("package x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	stop := make(chan struct{})
	defer close(stop)
	rebuildC := make(chan struct{}, 1)
	go func() {
		_ = Run(root, Options{Extensions: []string{".go"}, Debounce: 30 * time.Millisecond, Logger: &bytes.Buffer{}}, stop, func([]string) error {
			rebuildC <- struct{}{}
			return nil
		})
	}()
	time.Sleep(100 * time.Millisecond)
	// Modify a file inside node_modules — should NOT trigger rebuild.
	if err := os.WriteFile(filepath.Join(root, "node_modules", "deep", "noisy.go"), []byte("package x // updated"), 0644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rebuildC:
		t.Fatal("rebuild fired on a node_modules change despite heavy-dir skip")
	case <-time.After(300 * time.Millisecond):
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

func TestRunLogsRebuildDuration(t *testing.T) {
	// The "rebuild finished in Xms" / "rebuild failed in Xms ..."
	// log line gives users a feel for whether the cache machinery is
	// saving time. A regression that drops the duration would make
	// silent-watch UX worse.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "seed.go"), []byte("package x"), 0644); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	defer close(stop)
	var log bytes.Buffer
	done := make(chan struct{})
	go func() {
		_ = Run(root, Options{
			Extensions: []string{".go"},
			Debounce:   30 * time.Millisecond,
			Logger:     &log,
		}, stop, func(changed []string) error {
			defer func() { close(done) }()
			return nil
		})
	}()
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(root, "seed.go"), []byte("package x\nfunc f() {}"), 0644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("rebuild did not fire within 2s")
	}
	// Give the goroutine a moment to flush the post-rebuild log line.
	time.Sleep(50 * time.Millisecond)
	if !bytes.Contains(log.Bytes(), []byte("rebuild finished in")) {
		t.Errorf("expected 'rebuild finished in <duration>' in log, got:\n%s", log.String())
	}
}

func TestStartupLogIncludesDirectoryCount(t *testing.T) {
	// The startup line should advertise the directory count and
	// extension filter size — gives the user immediate feedback
	// that the watcher actually started and what it's watching.
	root := t.TempDir()
	stop := make(chan struct{})
	var log bytes.Buffer
	go func() {
		_ = Run(root, Options{
			Extensions: []string{".go", ".py"},
			Debounce:   30 * time.Millisecond,
			Logger:     &log,
		}, stop, func(changed []string) error { return nil })
	}()
	time.Sleep(150 * time.Millisecond)
	close(stop)
	output := log.String()
	if !strings.Contains(output, "watching") {
		t.Errorf("missing 'watching' verb in startup line: %s", output)
	}
	if !strings.Contains(output, "directories") {
		t.Errorf("missing directory count in startup line: %s", output)
	}
	if !strings.Contains(output, "extensions=2") {
		t.Errorf("missing extension count in startup line: %s", output)
	}
}
