package sync

import (
	"sync"
)

// uint64Channels is a thread-safe mapping from uint64 to chan struct{}.
type uint64Channels struct {
	m sync.Map // map[uint64]chan struct{}
}

// Get returns the channel associated with id, if any.
func (u *uint64Channels) Get(id uint64) (chan struct{}, bool) {
	v, ok := u.m.Load(id)
	if !ok {
		return nil, false
	}
	return v.(chan struct{}), true
}

// Set associates id with ch. It panics if id is already present.
func (u *uint64Channels) Set(id uint64, ch chan struct{}) {
	if _, loaded := u.m.LoadOrStore(id, ch); loaded {
		panic("uint64Channels: key already exists")
	}
}

// Delete removes the channel associated with id, if any.
func (u *uint64Channels) Delete(id uint64) {
	u.m.Delete(id)
}

// Iterate calls f for each entry. If f returns false, iteration stops.
func (u *uint64Channels) Iterate(f func(id uint64, ch chan struct{}) bool) {
	u.m.Range(func(key, value any) bool {
		return f(key.(uint64), value.(chan struct{}))
	})
}
