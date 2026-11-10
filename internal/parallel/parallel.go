// Package parallel runs a function over items concurrently.
package parallel

import (
	"sync"
	"time"
)

const DefaultWorkers = 8

type Outcome[T, R any] struct {
	Item    T
	Value   R
	Err     error
	Elapsed time.Duration
}

// Run calls fn for every item on up to workers goroutines and returns a channel
// that yields each outcome as it completes. The caller must drain the channel.
func Run[T, R any](items []T, workers int, fn func(T) (R, error)) <-chan Outcome[T, R] {
	out := make(chan Outcome[T, R])
	if workers < 1 {
		workers = DefaultWorkers
	}
	go func() {
		defer close(out)
		sem := make(chan struct{}, workers)
		var wg sync.WaitGroup
		for _, item := range items {
			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer func() { <-sem; wg.Done() }()
				started := time.Now()
				value, err := fn(item)
				out <- Outcome[T, R]{Item: item, Value: value, Err: err, Elapsed: time.Since(started)}
			}()
		}
		wg.Wait()
	}()
	return out
}
