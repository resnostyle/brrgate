package hub

import (
	"context"
	"sync"
)

// Waiter allows one manual approval decision buffered for a single waiter.
type Waiter struct {
	ch   chan bool
	once sync.Once
}

// NewWaiter constructs a waiter with buffer size 1.
func NewWaiter() *Waiter {
	return &Waiter{ch: make(chan bool, 1)}
}

// Resolve delivers accept/reject exactly once (ignored afterward).
func (w *Waiter) Resolve(accept bool) {
	w.once.Do(func() { w.ch <- accept })
}

// Wait consumes the decision once; when ctx expires it forces reject=false.
func (w *Waiter) Wait(ctx context.Context) bool {
	select {
	case v := <-w.ch:
		return v
	case <-ctx.Done():
		w.Resolve(false)
		return <-w.ch
	}
}
