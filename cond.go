package sync

import (
	"context"
	"sync"
	"sync/atomic"
)

// Cond behaves similarly to sync.Cond, but also supports context.Context.
// It has slight nuance in behaviour for Broadcast and Signal methods.
type Cond struct {
	noCopy noCopy

	nextID       atomic.Uint64
	channelWaits uint64Channels
	L            sync.Locker
}

// NewCond returns a WaitCond.
func NewCond(l sync.Locker) *Cond {
	return &Cond{
		L: l,
	}
}

// Broadcast wakes all goroutines waiting on c.
func (c *Cond) Broadcast() {
	c.channelWaits.Iterate(func(k uint64, ch chan struct{}) bool {
		select {
		case ch <- struct{}{}:
			c.channelWaits.Delete(k)
		default:
		}
		return true
	})
}

// Signal wakes one goroutine waiting on c, if there is any.
func (c *Cond) Signal() {
	c.channelWaits.Iterate(func(k uint64, ch chan struct{}) bool {
		select {
		case ch <- struct{}{}:
			c.channelWaits.Delete(k)
			return false
		default:
			return true
		}
	})
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
	var id uint64
	for {
		// Using a a for-loop in the extremely theoretically rare case when we
		// have a wait that has been around for a really long time such that
		// c.nextID has wrapped around.

		id = c.nextID.Add(1)
		if _, exist := c.channelWaits.Get(id); !exist {
			break
		}
	}

	ch := make(chan struct{}, 1)
	c.channelWaits.Set(id, ch)

	c.L.Unlock()
	defer c.L.Lock()

	select {
	case <-ch:
		// TODO: Put the channel in a pool to reduce allocations.
		return
	}
}

// WaitWithContext behaves similar as Wait, but also supports deadline. It
// returns context.Err().
func (c *Cond) WaitWithContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		// No reason to continue if we've already timed out.
		return err
	}

	var id uint64
	for {
		// Using a a for-loop in the extremely theoretically rare case when we
		// have a wait that has been around for a really long time such that
		// c.nextID has wrapped around.

		id = c.nextID.Add(1)
		if _, exist := c.channelWaits.Get(id); !exist {
			break
		}
	}

	// TODO: Pull `ch` from a pool if available to reduce allocations. Only instantiate if the pool is empty.
	ch := make(chan struct{}, 1)
	c.channelWaits.Set(id, ch)

	c.L.Unlock()
	defer c.L.Lock()

	select {
	// Always trying to wake up before checking if the context is Done. By doing
	// this, we make the behaviour for this method deterministic if calling it
	// with a cancelled context.
	case <-ch:
		// TODO: Put the channel in a pool to reduce allocations.
		return nil
	default:
	}

	select {
	case <-ch:
		// TODO: Put the channel in a pool to reduce allocations.
	case <-ctx.Done():
		return ctx.Err()
	}

	// Not returning ctx.Err() here because there's a small race condition that
	// the context has become Done _after_ we managed to lock.
	return nil
}
