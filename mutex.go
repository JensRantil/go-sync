package sync

import (
	"context"
	"sync"
)

// A Mutex is a mutual exclusion lock. The zero value for a Mutex is an
// unlocked mutex.
type Mutex struct {
	noCopy noCopy

	c    chan struct{}
	once sync.Once
}

func (m *Mutex) init() {
	m.c = make(chan struct{}, 1)
}

// Lock locks m. If the lock is already in use, the calling goroutine blocks
// until the mutex is available.
func (m *Mutex) Lock() {
	m.once.Do(m.init)
	m.c <- struct{}{}
}

// LockWithContext locks m. It behaves similarly to calling m.Lock() but also
// adds support for context.Context deadlines.
//
// The function returns ctx.Err() if the context deadlined, nil otherwise. The
// caller _must_ check the error code to decide if it should unlock the mutex
// later or not.
func (m *Mutex) LockWithContext(ctx context.Context) error {
	m.once.Do(m.init)

	// If the context is cancelled we return immediately. This check is to make
	// the function call deterministic.
	if err := ctx.Err(); err != nil {
		return err
	}

	select {
	// Always trying to lock before checking if the context is Done. By doing
	// this, we make the behaviour for this method deterministic if calling it
	// with a cancelled context.
	case m.c <- struct{}{}:
		return nil
	default:
	}

	select {
	case m.c <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}

	// Not returning ctx.Err() here because there's a small race condition that
	// the context has become Done _after_ we managed to lock.
	return nil
}

// Unlock unlocks m. It is a run-time error if m is not locked on entry to
// Unlock.
//
// A locked Mutex is not associated with a particular goroutine. It is allowed
// for one goroutine to lock a Mutex and then arrange for another goroutine to
// unlock it.
func (m *Mutex) Unlock() {
	m.once.Do(m.init)

	select {
	case <-m.c:
	default:
		panic("mutex wasn't locked - have you made sure you actually took the lock if using m.LockWithContext(...)?")
	}
}
