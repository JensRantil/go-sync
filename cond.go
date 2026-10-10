package sync

import (
	"context"
	"sync"
)

// Cond behaves similarly to sync.Cond, but also supports context.Context.
// It has slight nuance in behaviour for Broadcast and Signal methods.
type Cond struct {
	noCopy noCopy

	L sync.Locker

	// mu protects head and tail. Broadcast and Signal drop it before
	// sending, so waking waiters does not run under the lock.
	mu   sync.Mutex
	head *waiter
	tail *waiter
}

// NewCond returns a WaitCond.
func NewCond(l sync.Locker) *Cond {
	return &Cond{
		L: l,
	}
}

// Broadcast wakes all goroutines waiting on c.
func (c *Cond) Broadcast() {
	for w := c.detachWaiters(); w != nil; {
		next := w.next
		w.next = nil
		w.ch <- struct{}{}
		w = next
	}
}

// Signal wakes one goroutine waiting on c, if there is any.
func (c *Cond) Signal() {
	w := c.popWaiter()
	if w == nil {
		return
	}
	w.ch <- struct{}{}
}

// Wait atomically unlocks c.L and suspends execution of the calling goroutine.
// After later resuming execution, Wait locks c.L before returning. Unlike in
// other systems, Wait cannot return unless awoken by Broadcast or Signal.
//
// Because c.L is not locked when Wait first resumes, the caller typically
// cannot assume that the condition is true when Wait returns. Instead, the
// caller should Wait in a loop:
//
//	c.L.Lock()
//	for !condition() {
//	    c.Wait()
//	}
//	... make use of condition ...
//	c.L.Unlock()
func (c *Cond) Wait() {
	w := c.queueWaiter()

	c.L.Unlock()
	defer c.L.Lock()

	<-w.ch
	waiterNodePool.Put(w)
}

// WaitWithContext behaves similar as Wait, but also supports deadline. It
// returns context.Err().
func (c *Cond) WaitWithContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		// No reason to continue if we've already timed out.
		return err
	}

	w := c.queueWaiter()

	c.L.Unlock()
	defer c.L.Lock()

	select {
	// Always trying to wake up before checking if the context is Done. By doing
	// this, we make the behaviour for this method deterministic if calling it
	// with a cancelled context.
	case <-w.ch:
		waiterNodePool.Put(w)
		return nil
	default:
	}

	select {
	case <-w.ch:
		waiterNodePool.Put(w)
	case <-ctx.Done():
		// w stays queued. A later Signal or Broadcast may still send on it,
		// so it cannot go back to the pool.
		return ctx.Err()
	}

	// Not returning ctx.Err() here because there's a small race condition that
	// the context has become Done _after_ we managed to lock.
	return nil
}

func (c *Cond) queueWaiter() *waiter {
	w := waiterNodePool.Get()
	c.mu.Lock()
	if c.tail != nil {
		c.tail.next = w
	} else {
		c.head = w
	}
	c.tail = w
	c.mu.Unlock()
	return w
}

func (c *Cond) popWaiter() *waiter {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.head
	if w == nil {
		return nil
	}
	c.head = w.next
	if c.head == nil {
		c.tail = nil
	}
	w.next = nil
	return w
}

func (c *Cond) detachWaiters() *waiter {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.head
	c.head = nil
	c.tail = nil
	return w
}

// waiter is one goroutine blocked in Wait or WaitWithContext.
type waiter struct {
	ch   chan struct{}
	next *waiter
}

// waiterNodePool is a typed sync.Pool of waiter nodes, each with a
// one-buffered channel.
var waiterNodePool = waiterPool{
	p: sync.Pool{
		New: func() any {
			return &waiter{ch: make(chan struct{}, 1)}
		},
	},
}

// waiterPool is a sync.Pool whose elements are *waiter.
// It is safe for concurrent use by multiple goroutines because it is backed by sync.Pool.
type waiterPool struct {
	p sync.Pool
}

func (p *waiterPool) Get() *waiter {
	w := p.p.Get().(*waiter)
	w.next = nil
	return w
}

// Put a waiter on the pool.
//
// Important that nothing has been queued on w.ch.
func (p *waiterPool) Put(w *waiter) {
	w.next = nil
	p.p.Put(w)
}
