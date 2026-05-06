package parallel_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/parallel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_runsAllItems(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	var seen sync.Map
	err := parallel.Run(context.Background(), items, 3, func(_ context.Context, n int) error {
		seen.Store(n, true)
		return nil
	})
	require.NoError(t, err)
	for _, n := range items {
		_, ok := seen.Load(n)
		assert.Truef(t, ok, "item %d not seen", n)
	}
}

func TestRun_capsConcurrency(t *testing.T) {
	const workers = 3
	const items = 12
	var inFlight int32
	var peak int32
	work := make([]int, items)
	err := parallel.Run(context.Background(), work, workers, func(_ context.Context, _ int) error {
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if cur <= p || atomic.CompareAndSwapInt32(&peak, p, cur) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		return nil
	})
	require.NoError(t, err)
	assert.LessOrEqual(t, peak, int32(workers))
}

func TestRun_firstErrorCancelsRest(t *testing.T) {
	items := make([]int, 50)
	for i := range items {
		items[i] = i
	}
	var ran int32
	sentinel := errors.New("boom")
	err := parallel.Run(context.Background(), items, 4, func(ctx context.Context, n int) error {
		atomic.AddInt32(&ran, 1)
		if n == 0 {
			return sentinel
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return nil
		}
	})
	require.ErrorIs(t, err, sentinel)
	assert.Less(t, atomic.LoadInt32(&ran), int32(50))
}

func TestRun_zeroWorkersDefaultsToOne(t *testing.T) {
	var ran int32
	err := parallel.Run(context.Background(), []int{1, 2}, 0, func(_ context.Context, _ int) error {
		atomic.AddInt32(&ran, 1)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, int32(2), ran)
}

func TestRun_emptySliceIsNoop(t *testing.T) {
	var ran int32
	err := parallel.Run(context.Background(), []int{}, 4, func(_ context.Context, _ int) error {
		atomic.AddInt32(&ran, 1)
		return nil
	})
	require.NoError(t, err)
	assert.Zero(t, ran)
}
