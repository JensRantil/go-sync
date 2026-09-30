package sync

import (
	"context"
	"sync"
	"testing"
)

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
