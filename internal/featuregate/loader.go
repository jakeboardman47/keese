// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package featuregate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
)

// fileLoader watches a JSON file and pushes the parsed map to a
// callback. It tolerates the file being absent at startup (the
// projection ConfigMap may not exist yet on a fresh cluster) — in
// that case it watches the parent directory and waits for creation.
type fileLoader struct {
	path     string
	apply    func(map[string]bool)
	log      logr.Logger
	watcher  *fsnotify.Watcher
	debounce time.Duration

	mu     sync.Mutex
	closed bool
	cancel context.CancelFunc
}

func newFileLoader(path string, apply func(map[string]bool), log logr.Logger) (*fileLoader, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &fileLoader{
		path:     path,
		apply:    apply,
		log:      log,
		watcher:  w,
		debounce: 500 * time.Millisecond,
	}, nil
}

// start performs an initial best-effort load and spins up a
// background watcher. The supplied context cancels the watcher.
func (l *fileLoader) start(ctx context.Context) error {
	// Watch the parent directory rather than the file itself —
	// projected ConfigMaps replace the file via symlink swap, and
	// fsnotify on the file alone misses those events on Linux.
	dir := filepath.Dir(l.path)
	if err := l.watcher.Add(dir); err != nil {
		return err
	}
	l.loadOnce()

	wctx, cancel := context.WithCancel(ctx)
	l.cancel = cancel
	go l.run(wctx)
	return nil
}

func (l *fileLoader) close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.cancel != nil {
		l.cancel()
	}
	return l.watcher.Close()
}

// loadOnce parses the file and pushes the result. Errors (file
// missing, malformed JSON) are logged but non-fatal — the gate
// defaults remain in effect.
func (l *fileLoader) loadOnce() {
	data, err := os.ReadFile(l.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			l.log.V(1).Info("featuregate file absent; using defaults",
				"path", l.path)
			return
		}
		l.log.Error(err, "read featuregate file", "path", l.path)
		return
	}
	parsed := map[string]bool{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &parsed); err != nil {
			l.log.Error(err, "parse featuregate file", "path", l.path)
			return
		}
	}
	l.apply(parsed)
	l.log.V(1).Info("featuregate file loaded",
		"path", l.path, "gate_count", len(parsed))
}

// run consumes fsnotify events until the context cancels. Events
// are debounced because projected-ConfigMap updates produce a burst
// (rename + remove + create + chmod within milliseconds).
func (l *fileLoader) run(ctx context.Context) {
	timer := time.NewTimer(l.debounce)
	timer.Stop()
	pending := false
	target := filepath.Clean(l.path)

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-l.watcher.Events:
			if !ok {
				return
			}
			if filepath.Clean(ev.Name) != target {
				// Different file in the same directory; ignore.
				continue
			}
			pending = true
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(l.debounce)
		case err, ok := <-l.watcher.Errors:
			if !ok {
				return
			}
			l.log.Error(err, "fsnotify watcher error")
		case <-timer.C:
			if pending {
				pending = false
				l.loadOnce()
			}
		}
	}
}
