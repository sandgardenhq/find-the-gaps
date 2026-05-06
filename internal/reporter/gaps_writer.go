package reporter

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// GapsWriter owns gaps.md for the duration of a drift run. Workers Push the
// accumulated findings; the goroutine debounces writes so a burst of
// concurrent finishes coalesces into one atomic file replacement.
type GapsWriter struct {
	dir      string
	prefix   string
	debounce time.Duration

	mu     sync.Mutex
	latest []analyzer.DriftFinding
	dirty  bool

	wakeup    chan struct{}
	closed    chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

// NewGapsWriter starts a background goroutine that owns gaps.md in dir. The
// caller MUST call Close exactly once (it is safe to call more than once);
// after Close returns, the file reflects the most recent Push.
func NewGapsWriter(dir, prefix string, debounce time.Duration) *GapsWriter {
	w := &GapsWriter{
		dir:      dir,
		prefix:   prefix,
		debounce: debounce,
		wakeup:   make(chan struct{}, 1),
		closed:   make(chan struct{}),
		done:     make(chan struct{}),
	}
	go w.loop()
	return w
}

// Push replaces the writer's current view of all findings. The caller passes
// the full accumulated slice each time; the writer is the source of truth for
// the on-disk state, not a delta accumulator.
func (w *GapsWriter) Push(findings []analyzer.DriftFinding) {
	w.mu.Lock()
	w.latest = append(w.latest[:0], findings...)
	w.dirty = true
	w.mu.Unlock()
	select {
	case w.wakeup <- struct{}{}:
	default:
	}
}

// Close flushes any pending state and waits for the writer goroutine to exit.
// It is safe to call Close more than once.
func (w *GapsWriter) Close() error {
	w.closeOnce.Do(func() { close(w.closed) })
	<-w.done
	return nil
}

func (w *GapsWriter) loop() {
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

func (w *GapsWriter) flush() {
	w.mu.Lock()
	if !w.dirty {
		w.mu.Unlock()
		return
	}
	findings := append([]analyzer.DriftFinding(nil), w.latest...)
	w.dirty = false
	w.mu.Unlock()

	body := w.prefix + "\n## Stale Documentation\n\n" + BuildGapsStaleSection(findings)
	tmp := filepath.Join(w.dir, "gaps.md.tmp")
	final := filepath.Join(w.dir, "gaps.md")
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, final)
}
