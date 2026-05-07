package reporter

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// ScreenshotsWriter owns screenshots.md for the duration of a screenshot pass.
// Workers Push the latest accumulated ScreenshotResult; the goroutine debounces
// writes so a burst of concurrent page completions coalesces into one atomic
// file replacement.
type ScreenshotsWriter struct {
	dir      string
	debounce time.Duration

	mu      sync.Mutex
	latest  analyzer.ScreenshotResult
	dirty   bool
	lastErr error

	wakeup    chan struct{}
	closed    chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

// NewScreenshotsWriter starts a background goroutine that owns
// screenshots.md in dir. The caller MUST call Close exactly once (it is safe
// to call more than once); after Close returns, the file reflects the most
// recent Push.
func NewScreenshotsWriter(dir string, debounce time.Duration) *ScreenshotsWriter {
	w := &ScreenshotsWriter{
		dir:      dir,
		debounce: debounce,
		wakeup:   make(chan struct{}, 1),
		closed:   make(chan struct{}),
		done:     make(chan struct{}),
	}
	go w.loop()
	return w
}

// Push replaces the writer's current view of the result. The caller passes a
// snapshot built outside the worker's mutex so the writer can format it
// without serializing other workers. The writer is the source of truth for
// the on-disk state, not a delta accumulator.
func (w *ScreenshotsWriter) Push(res analyzer.ScreenshotResult) {
	w.mu.Lock()
	w.latest = res
	w.dirty = true
	w.mu.Unlock()
	select {
	case w.wakeup <- struct{}{}:
	default:
	}
}

// Close flushes any pending state and waits for the writer goroutine to exit.
// It returns the most recent flush error, if any (e.g. disk full at rename
// time). It is safe to call Close more than once.
func (w *ScreenshotsWriter) Close() error {
	w.closeOnce.Do(func() { close(w.closed) })
	<-w.done
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastErr
}

func (w *ScreenshotsWriter) loop() {
	defer close(w.done)
	var timer *time.Timer
	var fire <-chan time.Time
	arm := func() {
		if timer == nil {
			timer = time.NewTimer(w.debounce)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(w.debounce)
		}
		fire = timer.C
	}
	for {
		select {
		case <-w.wakeup:
			arm()
		case <-fire:
			fire = nil
			w.flush()
		case <-w.closed:
			w.flush()
			return
		}
	}
}

func (w *ScreenshotsWriter) flush() {
	w.mu.Lock()
	if !w.dirty {
		w.mu.Unlock()
		return
	}
	res := w.latest
	w.dirty = false
	w.mu.Unlock()

	body := BuildScreenshotsBytes(res)
	tmp := filepath.Join(w.dir, "screenshots.md.tmp")
	final := filepath.Join(w.dir, "screenshots.md")
	err := os.WriteFile(tmp, body, 0o644)
	if err == nil {
		err = os.Rename(tmp, final)
	}
	if err != nil {
		w.mu.Lock()
		w.lastErr = err
		w.mu.Unlock()
	}
}
