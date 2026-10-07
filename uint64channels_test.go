package sync

import (
	"sync"
	"testing"
)

func TestUint64ChannelsGetMissing(t *testing.T) {
	var u uint64Channels
	ch, ok := u.Get(1)
	if ok {
		t.Error("expected missing key")
	}
	if ch != nil {
		t.Error("expected nil channel for missing key")
	}
}

func TestUint64ChannelsSetThenGet(t *testing.T) {
	var u uint64Channels
	want := make(chan struct{})
	u.Set(1, want)

	got, ok := u.Get(1)
	if !ok {
		t.Fatal("expected key to exist")
	}
	if got != want {
		t.Error("got different channel than set")
	}
}

func TestUint64ChannelsSetOverwrite(t *testing.T) {
	var panicked bool
	defer func() {
		if !panicked {
			t.Error("expected Set to panic on overwrite")
		}
	}()
	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
	}()

	var u uint64Channels
	u.Set(1, make(chan struct{}))
	u.Set(1, make(chan struct{}))
}

func TestUint64ChannelsDelete(t *testing.T) {
	var u uint64Channels
	u.Set(1, make(chan struct{}))
	u.Delete(1)

	if _, ok := u.Get(1); ok {
		t.Error("expected key to be deleted")
	}

	// Deleting a missing key is a no-op.
	u.Delete(1)
	u.Delete(2)
}

func TestUint64ChannelsIterateAll(t *testing.T) {
	var u uint64Channels
	want := map[uint64]chan struct{}{
		1: make(chan struct{}),
		2: make(chan struct{}),
		3: make(chan struct{}),
	}
	for id, ch := range want {
		u.Set(id, ch)
	}

	seen := make(map[uint64]chan struct{})
	u.Iterate(func(id uint64, ch chan struct{}) bool {
		seen[id] = ch
		return true
	})

	if len(seen) != len(want) {
		t.Fatalf("visited %d entries, want %d", len(seen), len(want))
	}
	for id, ch := range want {
		if seen[id] != ch {
			t.Errorf("id %d: got different channel", id)
		}
	}
}

func TestUint64ChannelsIterateEarlyStop(t *testing.T) {
	var u uint64Channels
	for id := uint64(1); id <= 5; id++ {
		u.Set(id, make(chan struct{}))
	}

	var visits int
	u.Iterate(func(id uint64, ch chan struct{}) bool {
		visits++
		return false
	})

	if visits != 1 {
		t.Errorf("visited %d entries, want 1", visits)
	}
}

func TestUint64ChannelsConcurrent(t *testing.T) {
	nthreads := 3
	niterations := 10000

	var done sync.WaitGroup
	var u uint64Channels

	for i := 0; i < nthreads; i++ {
		done.Add(1)
		go func(base uint64) {
			for j := 0; j < niterations; j++ {
				id := base*uint64(niterations) + uint64(j)
				ch := make(chan struct{}, 1)
				u.Set(id, ch)
				if got, ok := u.Get(id); !ok || got != ch {
					t.Error("Get after Set failed")
				}
				u.Iterate(func(uint64, chan struct{}) bool {
					return true
				})
				u.Delete(id)
			}
			done.Done()
		}(uint64(i))
	}
	done.Wait()
}
