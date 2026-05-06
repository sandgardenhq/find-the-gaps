// Package parallel provides a small bounded-concurrency helper used by the
// LLM-heavy analyze phases. Cancellation is propagated through the supplied
// context; the first non-nil error from fn cancels remaining work and is
// returned.
package parallel

import (
	"context"

	"golang.org/x/sync/errgroup"
)

// Run executes fn for every element of items with at most workers in flight.
// A workers value <= 0 is treated as 1 (serial). The context handed to fn is
// cancelled as soon as any fn returns a non-nil error.
func Run[T any](ctx context.Context, items []T, workers int, fn func(context.Context, T) error) error {
	if len(items) == 0 {
		return nil
	}
	if workers <= 0 {
		workers = 1
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(workers)
	for _, item := range items {
		g.Go(func() error {
			return fn(gctx, item)
		})
	}
	return g.Wait()
}
