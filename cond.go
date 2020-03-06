package sync

import (
	"context"
	"sync"
)

// Cond behaves similarly to sync.Cond, but also supports context.Context.
// It has slight nuance in behaviour for Broadcast and Signal methods.
type Cond struct {
	noCopy noCopy

	nextID       uint64
	channelWaits map[uint64]chan struct{}
	L            sync.Locker
}

// NewCond returns a WaitCond.
func NewCond(l sync.Locker) *Cond {
	return &Cond{
		noCopy{},
		0,
		make(map[uint64]chan struct{}),
		l,
	}
}

// Broadcast wakes all goroutines waiting on c.
//
// Compared to sync.Cond.Broadcast, a lock _must_ be held when calling this.
func (c *Cond) Broadcast() {
	var todelete []uint64
	for k, c := range c.channelWaits {
		select {
		case c <- struct{}{}:
			todelete = append(todelete, k)
		default:
		}
	}

	for _, k := range todelete {
		delete(c.channelWaits, k)
	}
}

// Signal wakes one goroutine waiting on c, if there is any.
//
// Compared to sync.Cond.Signal, a lock _must_ be held when calling this.
func (c *Cond) Signal() {
	for k, ch := range c.channelWaits {
		select {
		case ch <- struct{}{}:
			delete(c.channelWaits, k)
			return
		default:
		}
	}
}

// Wait atomically unlocks c.L and suspends execution of the calling goroutine.
// After later resuming execution, Wait locks c.L before returning. Unlike in
// other systems, Wait cannot return unless awoken by Broadcast or Signal.
//
// Because c.L is not locked when Wait first resumes, the caller typically
// cannot assume that the condition is true when Wait returns. Instead, the
// caller should Wait in a loop:
//
//    c.L.Lock()
//    for !condition() {
//        c.Wait()
//    }
//    ... make use of condition ...
//    c.L.Unlock()
//
func (c *Cond) Wait() {
	// Implement this without a context.
	c.WaitWithContext(context.TODO())
}

// WaitWithContext behaves similar as Wait, but also supports deadline. It returns context.Err().
func (c *Cond) WaitWithContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		// No reason to continue if we've already timed out.
		return err
	}

	var id uint64
	for {
		// Using a a for-loop in the extremelly theoretically rare case when we have a wait that has been around for a really long time.

		c.nextID++
		id = c.nextID
		if _, exist := c.channelWaits[id]; !exist {
			break
		}
	}

	ch := make(chan struct{}, 1)
	c.channelWaits[id] = ch

	c.L.Unlock()
	defer c.L.Lock()

	select {
	// Always trying to wake up before checking if the context is Done. By doing
	// this, we make the behaviour for this method deterministic if calling it
	// with a cancelled context.
	case <-ch:
		return nil
	default:
	}

	select {
	case <-ch:
	case <-ctx.Done():
		return ctx.Err()
	}

	// Not returning ctx.Err() here because there's a small race condition that
	// the context has become Done _after_ we managed to lock.
	return nil
}
