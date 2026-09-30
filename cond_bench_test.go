package sync_test

// Benchmarks for github.com/JensRantil/go-sync's Cond, compared against the
// standard library's sync.Cond (and, where relevant, a channel).
//
// Requires Go 1.20+ (uses testing.B.Elapsed and atomic.Int64).
//
// Suggested usage:
//
//	go test -run '^$' -bench . -benchmem -cpu 1,2,4,8 -count 10 | tee bench.txt
//	benchstat -col /impl bench.txt
//
// The implementation name is always the LAST sub-benchmark component so that
// `benchstat -col /impl` lines up std vs go-sync vs go-sync-ctx.

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	gosync "github.com/JensRantil/go-sync"
)

// cond is the common subset implemented by both sync.Cond and gosync.Cond.
type cond interface {
	Wait()
	Signal()
	Broadcast()
}

type newCond func(sync.Locker) cond

// cancelableCtx is a never-cancelled context with a non-nil Done() channel, so
// WaitWithContext has to exercise its real select/registration path instead of
// a possible context.Background() fast path.
var cancelableCtx, _ = context.WithCancel(context.Background())

// ctxCond adapts gosync.Cond so that Wait() goes through WaitWithContext.
type ctxCond struct {
	*gosync.Cond
	ctx context.Context
}

func (c ctxCond) Wait() {
	if err := c.Cond.WaitWithContext(c.ctx); err != nil {
		panic(err) // ctx is never cancelled, so this would be a bug.
	}
}

var impls = []struct {
	name string
	new  newCond
}{
	{"std", func(l sync.Locker) cond { return sync.NewCond(l) }},
	{"go-sync", func(l sync.Locker) cond { return gosync.NewCond(l) }},
	{"go-sync-ctx", func(l sync.Locker) cond { return ctxCond{gosync.NewCond(l), cancelableCtx} }},
}

// eachImpl runs f as a sub-benchmark once per implementation.
func eachImpl(b *testing.B, f func(b *testing.B, nc newCond)) {
	for _, im := range impls {
		b.Run(im.name, func(b *testing.B) { f(b, im.new) })
	}
}

// -----------------------------------------------------------------------------
// 1. Signal ping-pong: round-trip Wait/Signal/reschedule latency, no contention.
// -----------------------------------------------------------------------------

func BenchmarkCondPingPong(b *testing.B) {
	eachImpl(b, func(b *testing.B, nc newCond) {
		var mu sync.Mutex
		c := nc(&mu)
		turn := 0 // 0 = main's turn, 1 = helper's turn
		done := make(chan struct{})

		go func() {
			defer close(done)
			mu.Lock()
			defer mu.Unlock()
			for i := 0; i < b.N; i++ {
				for turn != 1 {
					c.Wait()
				}
				turn = 0
				c.Signal()
			}
		}()

		b.ResetTimer()
		mu.Lock()
		for i := 0; i < b.N; i++ {
			turn = 1
			c.Signal()
			for turn != 0 {
				c.Wait()
			}
		}
		mu.Unlock()
		b.StopTimer()
		<-done
	})
}

// -----------------------------------------------------------------------------
// 2. Broadcast fan-out: N waiters, one broadcaster. Reports ns/wakeup.
// -----------------------------------------------------------------------------

func BenchmarkCondBroadcast(b *testing.B) {
	for _, n := range []int{1, 10, 100, 1000, 10000} {
		b.Run(fmt.Sprintf("waiters=%d", n), func(b *testing.B) {
			eachImpl(b, func(b *testing.B, nc newCond) {
				var mu sync.Mutex
				wake, ack := nc(&mu), nc(&mu) // only the main goroutine waits on ack
				var ready, acked, gen int
				stop := false
				var wg sync.WaitGroup

				for i := 0; i < n; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						mu.Lock()
						defer mu.Unlock()
						seen := 0
						ready++
						if ready == n {
							ack.Signal()
						}
						for {
							for gen == seen && !stop {
								wake.Wait()
							}
							if stop {
								return
							}
							seen = gen
							acked++
							if acked == n {
								ack.Signal()
							}
						}
					}()
				}

				mu.Lock()
				for ready < n { // all waiters are now parked in wake.Wait()
					ack.Wait()
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					gen++
					acked = 0
					wake.Broadcast()
					for acked < n {
						ack.Wait()
					}
				}
				b.StopTimer()
				stop = true
				wake.Broadcast()
				mu.Unlock()
				wg.Wait()

				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/(float64(b.N)*float64(n)), "ns/wakeup")
			})
		})
	}
}

// -----------------------------------------------------------------------------
// 3. Uncontended fast path: Signal/Broadcast with zero waiters.
//    The locker is intentionally not held (allowed by sync.Cond's contract).
// -----------------------------------------------------------------------------

func BenchmarkCondNoWaiters(b *testing.B) {
	ops := []struct {
		name string
		call func(c cond)
	}{
		{"Signal", func(c cond) { c.Signal() }},
		{"Broadcast", func(c cond) { c.Broadcast() }},
	}
	for _, op := range ops {
		b.Run(op.name, func(b *testing.B) {
			b.Run("serial", func(b *testing.B) {
				eachImpl(b, func(b *testing.B, nc newCond) {
					var mu sync.Mutex
					c := nc(&mu)
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						op.call(c)
					}
				})
			})
			// One shared cond hammered from all Ps: exposes contention on
			// internal state even when there is nobody to wake.
			b.Run("parallel-shared", func(b *testing.B) {
				eachImpl(b, func(b *testing.B, nc newCond) {
					var mu sync.Mutex
					c := nc(&mu)
					b.ResetTimer()
					b.RunParallel(func(pb *testing.PB) {
						for pb.Next() {
							op.call(c)
						}
					})
				})
			})
		})
	}
}

// -----------------------------------------------------------------------------
// 4. Producer/consumer bounded queue, compared against a buffered channel.
//    One benchmark iteration == one item passed through the queue.
// -----------------------------------------------------------------------------

type queue struct {
	mu                sync.Mutex
	notEmpty, notFull cond
	buf               []int
	head, n           int
	closed            bool
}

func newQueue(nc newCond, capacity int) *queue {
	q := &queue{buf: make([]int, capacity)}
	q.notEmpty = nc(&q.mu)
	q.notFull = nc(&q.mu)
	return q
}

func (q *queue) put(v int) {
	q.mu.Lock()
	for q.n == len(q.buf) {
		q.notFull.Wait()
	}
	q.buf[(q.head+q.n)%len(q.buf)] = v
	q.n++
	q.notEmpty.Signal()
	q.mu.Unlock()
}

func (q *queue) get() (int, bool) {
	q.mu.Lock()
	for q.n == 0 && !q.closed {
		q.notEmpty.Wait()
	}
	if q.n == 0 { // closed and drained
		q.mu.Unlock()
		return 0, false
	}
	v := q.buf[q.head]
	q.head = (q.head + 1) % len(q.buf)
	q.n--
	q.notFull.Signal()
	q.mu.Unlock()
	return v, true
}

func (q *queue) close() {
	q.mu.Lock()
	q.closed = true
	q.notEmpty.Broadcast()
	q.mu.Unlock()
}

// runProducersConsumers pushes b.N items through the given queue operations.
func runProducersConsumers(b *testing.B, producers, consumers int, put func(int), get func() (int, bool), closeQ func()) {
	var next atomic.Int64
	var pw, cw sync.WaitGroup

	for i := 0; i < consumers; i++ {
		cw.Add(1)
		go func() {
			defer cw.Done()
			for {
				if _, ok := get(); !ok {
					return
				}
			}
		}()
	}
	b.ResetTimer()
	for i := 0; i < producers; i++ {
		pw.Add(1)
		go func() {
			defer pw.Done()
			for next.Add(1) <= int64(b.N) {
				put(1)
			}
		}()
	}
	pw.Wait()
	closeQ()
	cw.Wait()
}

func BenchmarkCondProducerConsumer(b *testing.B) {
	cases := []struct{ producers, consumers, capacity int }{
		{1, 1, 1},
		{1, 1, 128},
		{4, 4, 1},
		{4, 4, 128},
		{16, 16, 128},
		{1, 8, 128}, // consumer-heavy: consumers mostly waiting
		{8, 1, 128}, // producer-heavy: producers mostly waiting
	}
	for _, tc := range cases {
		name := fmt.Sprintf("P=%d/C=%d/cap=%d", tc.producers, tc.consumers, tc.capacity)
		b.Run(name, func(b *testing.B) {
			eachImpl(b, func(b *testing.B, nc newCond) {
				q := newQueue(nc, tc.capacity)
				runProducersConsumers(b, tc.producers, tc.consumers, q.put, q.get, q.close)
			})
			b.Run("chan", func(b *testing.B) {
				ch := make(chan int, tc.capacity)
				runProducersConsumers(b, tc.producers, tc.consumers,
					func(v int) { ch <- v },
					func() (int, bool) { v, ok := <-ch; return v, ok },
					func() { close(ch) })
			})
		})
	}
}

// -----------------------------------------------------------------------------
// 5. Many independent conds in parallel: catches accidental global state
//    (global waiter lists, global locks, shared allocators...).
//    Each parallel worker owns a private mutex+cond and a private helper
//    goroutine, and ping-pongs with it.
// -----------------------------------------------------------------------------

func BenchmarkCondManyConds(b *testing.B) {
	for _, pairsPerP := range []int{1, 8} {
		b.Run(fmt.Sprintf("pairsPerP=%d", pairsPerP), func(b *testing.B) {
			eachImpl(b, func(b *testing.B, nc newCond) {
				b.SetParallelism(pairsPerP)
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					var mu sync.Mutex
					c := nc(&mu)
					turn := 0 // 0 = worker's turn, 1 = helper's turn
					quit := false
					var wg sync.WaitGroup

					wg.Add(1)
					go func() {
						defer wg.Done()
						mu.Lock()
						defer mu.Unlock()
						for {
							for turn != 1 && !quit {
								c.Wait()
							}
							if quit {
								return
							}
							turn = 0
							c.Signal()
						}
					}()

					mu.Lock()
					for pb.Next() {
						turn = 1
						c.Signal()
						for turn != 0 {
							c.Wait()
						}
					}
					quit = true
					c.Signal()
					mu.Unlock()
					wg.Wait()
				})
			})
		})
	}
}

// -----------------------------------------------------------------------------
// 6. Thundering herd: N waiters share ONE cond but each waits for its own
//    predicate. Every iteration sets one waiter's token and Broadcasts, so all
//    N wake up, one proceeds, and N-1 immediately re-wait. Measures how cheap
//    futile wakeups and re-queuing are. Reports ns/wakeup (N wakeups per op).
// -----------------------------------------------------------------------------

func BenchmarkCondThunderingHerd(b *testing.B) {
	for _, n := range []int{2, 16, 128, 1024} {
		b.Run(fmt.Sprintf("waiters=%d", n), func(b *testing.B) {
			eachImpl(b, func(b *testing.B, nc newCond) {
				var mu sync.Mutex
				wake, ack := nc(&mu), nc(&mu) // only the main goroutine waits on ack
				tokens := make([]bool, n)
				var ready, woke int
				done := false
				var wg sync.WaitGroup

				for i := 0; i < n; i++ {
					wg.Add(1)
					go func(i int) {
						defer wg.Done()
						mu.Lock()
						defer mu.Unlock()
						ready++
						if ready == n {
							ack.Signal()
						}
						for {
							for !tokens[i] && !done {
								wake.Wait()
								woke++
								if woke == n {
									ack.Signal()
								}
							}
							if done {
								return
							}
							tokens[i] = false
						}
					}(i)
				}

				mu.Lock()
				for ready < n {
					ack.Wait()
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					woke = 0
					tokens[i%n] = true
					wake.Broadcast()
					for woke < n { // wait until everyone has woken and re-waited
						ack.Wait()
					}
				}
				b.StopTimer()
				done = true
				wake.Broadcast()
				mu.Unlock()
				wg.Wait()

				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/(float64(b.N)*float64(n)), "ns/wakeup")
			})
		})
	}
}

// -----------------------------------------------------------------------------
// 7. Contended mutex interplay: a cond-based counting semaphore hammered by
//    all Ps, optionally with "bystander" goroutines that only fight over the
//    same mutex. Wake-up cost is often dominated by re-acquiring L.
// -----------------------------------------------------------------------------

func BenchmarkCondContendedLock(b *testing.B) {
	for _, permits := range []int{1, 4} {
		for _, bystanders := range []int{0, 8} {
			name := fmt.Sprintf("permits=%d/bystanders=%d", permits, bystanders)
			b.Run(name, func(b *testing.B) {
				eachImpl(b, func(b *testing.B, nc newCond) {
					var mu sync.Mutex
					c := nc(&mu)
					avail := permits
					spins := 0 // protected by mu; gives bystanders a non-empty critical section
					var stop atomic.Bool
					var wg sync.WaitGroup

					for i := 0; i < bystanders; i++ {
						wg.Add(1)
						go func() {
							defer wg.Done()
							for !stop.Load() {
								mu.Lock()
								spins++
								mu.Unlock()
							}
						}()
					}

					b.ResetTimer()
					b.RunParallel(func(pb *testing.PB) {
						for pb.Next() {
							// acquire
							mu.Lock()
							for avail == 0 {
								c.Wait()
							}
							avail--
							mu.Unlock()
							// release
							mu.Lock()
							avail++
							c.Signal()
							mu.Unlock()
						}
					})
					b.StopTimer()
					stop.Store(true)
					wg.Wait()
				})
			})
		}
	}
}
