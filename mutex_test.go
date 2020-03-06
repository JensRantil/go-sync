package sync

import (
	"context"
	"sync"
	"testing"
)

func TestLockingMutex(t *testing.T) {
	var m Mutex
	m.Lock()
}

func TestLockingAndUnlockingMutex(t *testing.T) {
	var m Mutex
	m.Lock()
	m.Unlock()
}

func TestUnlockingUnlockedMutexPanics(t *testing.T) {
	var panicked bool
	defer func() {
		if !panicked {
			t.Error("expected function to panic")
		}
	}()
	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
	}()
	var m Mutex
	m.Unlock()
}

// TestForMutexRaceConditions is a catch-all test that makes sure we don't
// deadlock in some weird edge case.
func TestForMutexRaceConditions(t *testing.T) {
	nthreads := 3
	niterations := 10000

	var done sync.WaitGroup

	var m Mutex
	var counter int
	for i := 0; i < nthreads; i++ {
		done.Add(1)
		go func() {
			for i := 0; i < niterations; i++ {
				m.Lock()
				counter++
				m.Unlock()
			}
			done.Done()
		}()
	}
	done.Wait()
	if expected := nthreads * niterations; counter != expected {
		t.Error("wrong value of counter. Was:", counter, "Expected:", expected)
	}
}

func TestMutexLockDeadlineError(t *testing.T) {
	var m Mutex
	m.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := m.LockWithContext(ctx)
	if expected := context.Canceled; err != expected {
		t.Error("Unexpected error. Was:", err, "Expected:", expected)
	}
}

func TestMutexLockDeadlineDoesntLock(t *testing.T) {
	var m Mutex
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = m.LockWithContext(ctx)

	var panicked bool
	defer func() {
		if !panicked {
			t.Error("expected function to panic")
		}
	}()
	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
	}()
	m.Unlock()
}

func TestMutexLockWithContextGenerallyHasNoError(t *testing.T) {
	var m Mutex
	err := m.LockWithContext(context.Background())
	if err != nil {
		t.Error("expected no error")
	}
}

func TestLockingAndUnlockingMutexWithContext(t *testing.T) {
	var m Mutex
	m.LockWithContext(context.Background())
	m.Unlock()
}

func BenchmarkMutexLockUnlock(b *testing.B) {
	var m Mutex

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Lock()
		m.Unlock()
	}
}

func BenchmarkStandardMutexLockUnlock(b *testing.B) {
	var m sync.Mutex

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Lock()
		m.Unlock()
	}
}

func BenchmarkMutexLockWithContextUnlock(b *testing.B) {
	var m Mutex
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.LockWithContext(ctx)
		m.Unlock()
	}
}
