package sync

import (
	"context"
	"sync"
	"testing"
)

func newTestCond() *Cond {
	// using stdlib to not have to debug two things at the same time.
	var m sync.Mutex
	return NewCond(&m)
}

func TestCondBasicSignal(t *testing.T) {
	c := newTestCond()

	c.L.Lock()
	c.Signal()
	c.L.Unlock()
}

func TestCondBasicBroadcast(t *testing.T) {
	c := newTestCond()

	c.L.Lock()
	c.Broadcast()
	c.L.Unlock()
}

func TestBasicCondSignal(t *testing.T) {
	c := newTestCond()

	var waiterdone sync.WaitGroup
	var waiterstarted sync.WaitGroup

	waiterdone.Add(1)
	waiterstarted.Add(1)

	go func() {
		c.L.Lock()
		waiterstarted.Done()
		c.Wait()
		c.L.Unlock()
		waiterdone.Done()
	}()

	waiterstarted.Wait()

	c.L.Lock()
	c.Signal()
	c.L.Unlock()

	waiterdone.Wait()
}

func TestBasicCondBroadcast(t *testing.T) {
	nthreads := 4

	c := newTestCond()

	var waitersdone sync.WaitGroup
	var waitersstarted sync.WaitGroup

	waitersdone.Add(nthreads)
	waitersstarted.Add(nthreads)

	f := func() {
		c.L.Lock()
		waitersstarted.Done()
		c.Wait()
		c.L.Unlock()
		waitersdone.Done()
	}
	for i := 0; i < nthreads; i++ {
		go f()
	}

	waitersstarted.Wait()

	c.L.Lock()
	c.Broadcast()
	c.L.Unlock()

	waitersdone.Wait()
}

func TestCondWaitDeadline(t *testing.T) {
	c := newTestCond()

	c.L.Lock()
	defer c.L.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.WaitWithContext(ctx)
	if expected := context.Canceled; err != expected {
		t.Error("unexpected error. was:", err, "expected:", expected)
	}
}

// TestForCondRaceConditions is a catch-all test that makes sure we don't
// deadlock in some weird edge case.
func TestCondSignal(t *testing.T) {
	c := newTestCond()
	nthreads := 2
	started := make(chan struct{})
	awake := make(chan struct{}, nthreads)
	var wg sync.WaitGroup
	for i := 0; i < nthreads; i++ {
		wg.Add(1)
		go func() {
			c.L.Lock()
			started <- struct{}{}
			c.Wait()
			awake <- struct{}{}
			c.L.Unlock()
			wg.Done()
		}()
	}
	for i := 0; i < nthreads; i++ {
		<-started
	}
	// All threads are now in a waiting state.
	for i := 0; i < nthreads; i++ {
		select {
		case <-awake:
			t.Fatal("at least one goroutine is not asleep")
		default:
		}
		c.L.Lock()
		c.Signal()
		c.L.Unlock()
		<-awake // Will deadlock if no goroutine wakes up
		select {
		case <-awake:
			t.Fatal("unexpected goroutines awake")
		default:
		}
	}
	wg.Wait()
}

func TestCondBroadcast(t *testing.T) {
	c := newTestCond()
	nthreads := 200
	started := make(chan int, nthreads)
	awake := make(chan int, nthreads)
	var wg sync.WaitGroup
	for i := 0; i < nthreads; i++ {
		wg.Add(1)
		go func(g int) {
			c.L.Lock()
			started <- g
			c.Wait()
			awake <- g
			c.L.Unlock()
			wg.Done()
		}(i)
	}

	for i := 0; i < nthreads; i++ {
		<-started
	}
	// nthreads goroutines are now running

	select {
	case <-awake:
		t.Fatal("at least one goroutine is not asleep")
	default:
	}

	c.L.Lock()
	c.Broadcast()
	c.L.Unlock()

	seen := make([]bool, nthreads)
	for i := 0; i < nthreads; i++ {
		g := <-awake
		if seen[g] {
			t.Fatal("goroutine woke up twice")
		}
		seen[g] = true
	}

	select {
	case <-started:
		t.Fatal("goroutine did not exit")
	default:
	}

	wg.Wait()
}
