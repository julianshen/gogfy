// Package watch monitors a corpus root for filesystem changes and invokes a
// rebuild callback when relevant files change. Used by `gogfy watch <root>`
// to keep graph artifacts in sync with the source tree without manual reruns.
package watch

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Logger receives status lines from Run. Defaults to os.Stderr; tests
// override to capture output.
type Logger interface {
	io.Writer
}

// RebuildFunc is invoked after a debounce window when one or more files
// matching extensions have changed. The slice of changed paths is for
// reporting; the callback is responsible for rerunning the pipeline (which
// re-walks the corpus anyway, so the path list is informational).
type RebuildFunc func(changed []string) error

// Options configures a Run. Zero values are sensible defaults.
type Options struct {
	// Extensions filters which file changes count. Other paths are silently
	// ignored. Pass nil to react to every change (rare; usually you want
	// the same list as detect.CollectFiles).
	Extensions []string
	// Debounce is the quiet period the watcher waits after the last event
	// before invoking rebuild. Default 200ms.
	Debounce time.Duration
	// Logger receives status lines. Default os.Stderr.
	Logger io.Writer
}

// Run watches root recursively until ctx is closed (via err on the recv side
// of the returned chan) or rebuild returns an unrecoverable error. The
// caller signals shutdown by closing stop.
func Run(root string, opts Options, stop <-chan struct{}, rebuild RebuildFunc) error {
	if opts.Debounce <= 0 {
		opts.Debounce = 200 * time.Millisecond
	}
	if opts.Logger == nil {
		opts.Logger = os.Stderr
	}
	extSet := make(map[string]struct{}, len(opts.Extensions))
	for _, e := range opts.Extensions {
		extSet[e] = struct{}{}
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("watch: %w", err)
	}
	defer w.Close()

	if err := addRecursive(w, root); err != nil {
		return fmt.Errorf("watch %s: %w", root, err)
	}
	// Count watched paths so the startup line gives users a sense
	// of the scope. Silent watch mode previously left people
	// wondering "did this actually start watching anything?"
	watchedCount := w.WatchList()
	fmt.Fprintf(opts.Logger, "gogfy: watching %s (%d directories, debounce %v, extensions=%d)\n",
		root, len(watchedCount), opts.Debounce, len(extSet))

	pending := map[string]struct{}{}
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	// rebuilding=true while a rebuild goroutine is in flight; events keep
	// queueing into pending and the next flush picks them up after the
	// rebuild returns. Without this, a rebuild that runs longer than the
	// debounce window would block the event loop and overflow fsnotify's
	// internal buffer.
	rebuilding := false
	rebuildDone := make(chan struct{}, 1)

	flush := func() {
		if rebuilding || len(pending) == 0 {
			return
		}
		paths := make([]string, 0, len(pending))
		for p := range pending {
			paths = append(paths, p)
		}
		pending = map[string]struct{}{}
		fmt.Fprintf(opts.Logger, "gogfy: %d file(s) changed, rebuilding\n", len(paths))
		rebuilding = true
		go func() {
			// Track duration so users see whether the cache+manifest
			// path is actually saving time. Without this, a 30s
			// rebuild feels identical to a 0.3s no-op rebuild and
			// users can't tell whether the incremental machinery is
			// doing its job.
			start := time.Now()
			err := rebuild(paths)
			elapsed := time.Since(start)
			if err != nil {
				fmt.Fprintf(opts.Logger, "gogfy: rebuild failed in %v: %v\n", elapsed.Truncate(time.Millisecond), err)
			} else {
				fmt.Fprintf(opts.Logger, "gogfy: rebuild finished in %v\n", elapsed.Truncate(time.Millisecond))
			}
			rebuildDone <- struct{}{}
		}()
	}

	for {
		select {
		case <-stop:
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return errors.New("watcher closed")
			}
			// New directories must be added so we see events inside them.
			if ev.Has(fsnotify.Create) {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					_ = addRecursive(w, ev.Name)
				}
			}
			if !shouldNotify(ev, extSet) {
				continue
			}
			pending[ev.Name] = struct{}{}
			// Reset on every event so the debounce window is always relative
			// to the most-recent change ("trailing edge" semantics).
			timer.Reset(opts.Debounce)
		case err, ok := <-w.Errors:
			if !ok {
				return errors.New("watcher closed")
			}
			fmt.Fprintf(opts.Logger, "gogfy: watch error: %v\n", err)
		case <-timer.C:
			flush()
		case <-rebuildDone:
			rebuilding = false
			// Drain any events queued during the rebuild on the next flush.
			if len(pending) > 0 {
				timer.Reset(opts.Debounce)
			}
		}
	}
}

// shouldNotify decides whether a filesystem event is worth queueing a rebuild
// for. Chmod/access events are dropped; only writes/creates/renames/removes
// of files matching extSet survive.
func shouldNotify(ev fsnotify.Event, extSet map[string]struct{}) bool {
	if !ev.Has(fsnotify.Create) && !ev.Has(fsnotify.Write) &&
		!ev.Has(fsnotify.Remove) && !ev.Has(fsnotify.Rename) {
		return false
	}
	if len(extSet) == 0 {
		return true
	}
	ext := filepath.Ext(ev.Name)
	_, ok := extSet[ext]
	return ok
}

// heavyDirs are directory names that almost always contain machine-generated
// content unrelated to the corpus and that, on real-world repos, easily blow
// past the OS's inotify limit if we hand each subdirectory to fsnotify.
// Listed verbatim because .graphifyignore is a per-corpus convention while
// these are project-type conventions worth defaulting on.
var heavyDirs = map[string]struct{}{
	"node_modules": {}, "vendor": {}, "target": {}, "build": {}, "dist": {},
	".venv": {}, "venv": {}, "__pycache__": {}, ".gradle": {}, ".tox": {},
}

// addRecursive registers root and every directory beneath it with the
// watcher. Hidden directories (starting with `.`) and well-known heavy
// directories (node_modules, vendor, target, …) are skipped to avoid
// drowning the OS inotify queue and quickly tripping the per-process limit.
func addRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}
		if path != root {
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			if _, heavy := heavyDirs[base]; heavy {
				return filepath.SkipDir
			}
		}
		return w.Add(path)
	})
}
