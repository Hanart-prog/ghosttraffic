package replay

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stepClock jumps forward on each After, so due times are exact and tests never sleep.
type stepClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stepClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	ch := make(chan time.Time, 1)
	ch <- c.now
	return ch
}

func collect(t *testing.T, ctx context.Context, cfg Config, offsets []time.Duration, work func(Slot)) ([]Slot, error) {
	t.Helper()
	var mu sync.Mutex
	var slots []Slot
	err := Schedule(ctx, cfg, offsets, func(_ context.Context, s Slot) {
		if s.Status == Run && work != nil {
			work(s)
		}
		mu.Lock()
		defer mu.Unlock()
		slots = append(slots, s)
	})
	return slots, err
}

func TestSpeedScalesDueTimes(t *testing.T) {
	for _, speed := range []float64{1, 2} {
		clock := &stepClock{now: time.Unix(0, 0)}
		cfg := Config{Concurrency: 2, QueueLimit: 10, Speed: speed, Clock: clock}
		offsets := []time.Duration{100 * time.Millisecond, 300 * time.Millisecond, 200 * time.Millisecond}
		slots, err := collect(t, context.Background(), cfg, offsets, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range slots {
			if want := time.Duration(float64(offsets[s.Index]) / speed); s.Scheduled != want {
				t.Errorf("speed %v index %d: scheduled %v, want %v", speed, s.Index, s.Scheduled, want)
			}
		}
		if got, want := clock.Now().Sub(time.Unix(0, 0)), time.Duration(float64(300*time.Millisecond)/speed); got != want {
			t.Errorf("speed %v: finished at %v, want %v", speed, got, want)
		}
	}
}

func TestNeverStartsEarly(t *testing.T) {
	offsets := []time.Duration{0, 20 * time.Millisecond, 40 * time.Millisecond}
	slots, err := collect(t, context.Background(), Config{Concurrency: 3, QueueLimit: 3, Speed: 1}, offsets, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range slots {
		if s.Start < s.Scheduled {
			t.Errorf("index %d started at %v, before %v", s.Index, s.Start, s.Scheduled)
		}
	}
}

// The core coordinated-omission guarantee: a stall shows up as delay on the
// requests behind it, and later due times do not move.
func TestStallBecomesSchedulingDelay(t *testing.T) {
	const stall = 60 * time.Millisecond
	offsets := []time.Duration{0, 5 * time.Millisecond, 10 * time.Millisecond}
	slots, err := collect(t, context.Background(), Config{Concurrency: 1, QueueLimit: 10, Speed: 1}, offsets,
		func(Slot) { time.Sleep(stall) })
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range slots {
		if s.Scheduled != offsets[s.Index] {
			t.Errorf("index %d due time moved to %v", s.Index, s.Scheduled)
		}
		if s.Index == 2 && s.Delay < 2*stall-offsets[2] {
			t.Errorf("last item delay = %v, want at least %v", s.Delay, 2*stall-offsets[2])
		}
	}
}

func TestConcurrencyCapHolds(t *testing.T) {
	const limit = 4
	offsets := make([]time.Duration, 200)
	var inFlight, peak atomic.Int64
	slots, err := collect(t, context.Background(), Config{Concurrency: limit, QueueLimit: len(offsets), Speed: 1}, offsets,
		func(Slot) {
			n := inFlight.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			inFlight.Add(-1)
		})
	if err != nil {
		t.Fatal(err)
	}
	if peak.Load() != limit {
		t.Errorf("peak in-flight = %d, want %d", peak.Load(), limit)
	}
	if len(slots) != len(offsets) {
		t.Errorf("got %d results", len(slots))
	}
}

func TestOverflowIsDroppedExplicitly(t *testing.T) {
	release := make(chan struct{})
	offsets := make([]time.Duration, 50)
	var once sync.Once
	var mu sync.Mutex
	counts := map[Status]int{}
	seen := map[int]int{}
	err := Schedule(context.Background(), Config{Concurrency: 1, QueueLimit: 2, Speed: 1}, offsets, func(_ context.Context, s Slot) {
		if s.Status == Run {
			<-release
		}
		mu.Lock()
		defer mu.Unlock()
		counts[s.Status]++
		seen[s.Index]++
		// One worker and two queue slots admit at most 3 while blocked.
		if counts[Dropped] == 45 {
			once.Do(func() { close(release) })
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if counts[Dropped] < 45 || counts[Run]+counts[Dropped] != len(offsets) {
		t.Errorf("counts = %v", counts)
	}
	for i := range offsets {
		if seen[i] != 1 {
			t.Errorf("index %d handled %d times", i, seen[i])
		}
	}
}

func TestCancellationGivesEveryItemAResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	offsets := []time.Duration{0, time.Hour, 2 * time.Hour}
	var cancelled atomic.Int64
	err := Schedule(ctx, Config{Concurrency: 1, QueueLimit: 3, Speed: 1}, offsets, func(_ context.Context, s Slot) {
		if s.Status == Run {
			cancel()
		}
		if s.Status == Cancelled {
			cancelled.Add(1)
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if cancelled.Load() != 2 {
		t.Fatalf("cancelled = %d, want 2", cancelled.Load())
	}
}

func TestGoroutinesAreBoundedAndReleased(t *testing.T) {
	before := runtime.NumGoroutine()
	offsets := make([]time.Duration, 5000)
	var peak atomic.Int64
	if _, err := collect(t, context.Background(), Config{Concurrency: 8, QueueLimit: 5000, Speed: 1}, offsets, func(Slot) {
		if n := int64(runtime.NumGoroutine()); n > peak.Load() {
			peak.Store(n)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if limit := int64(before + 8 + 1); peak.Load() > limit {
		t.Errorf("peak goroutines = %d, limit %d", peak.Load(), limit)
	}
}

func TestScheduleRejectsBadConfig(t *testing.T) {
	for _, cfg := range []Config{
		{QueueLimit: 1, Speed: 1},
		{Concurrency: 1, Speed: 1},
		{Concurrency: 1, QueueLimit: 1},
	} {
		if err := Schedule(context.Background(), cfg, nil, nil); err == nil {
			t.Errorf("accepted %+v", cfg)
		}
	}
}

func TestTargetOrderAlternates(t *testing.T) {
	if TargetOrder(1) == TargetOrder(2) {
		t.Fatal("order must alternate")
	}
	if TargetOrder(1) != TargetOrder(3) {
		t.Fatal("odd trials must agree")
	}
}

func TestDependentsWaitAndTheWaitIsNotDelay(t *testing.T) {
	const parentTime = 40 * time.Millisecond
	var mu sync.Mutex
	slots := map[int]Slot{}
	items := []Item{{Offset: 0}, {Offset: 5 * time.Millisecond, After: []int{0}}}
	err := ScheduleItems(context.Background(), Config{Concurrency: 4, QueueLimit: 4, Speed: 1}, items,
		func(_ context.Context, s Slot) bool {
			if s.Index == 0 {
				time.Sleep(parentTime)
			}
			mu.Lock()
			defer mu.Unlock()
			slots[s.Index] = s
			return true
		})
	if err != nil {
		t.Fatal(err)
	}
	child := slots[1]
	if child.Start < parentTime {
		t.Errorf("child started at %v, before its parent finished", child.Start)
	}
	if child.DependencyWait < parentTime-5*time.Millisecond {
		t.Errorf("dependency wait = %v", child.DependencyWait)
	}
	if child.Delay > 20*time.Millisecond {
		t.Errorf("waiting for a parent was charged as delay: %v", child.Delay)
	}
	if child.Scheduled != 5*time.Millisecond {
		t.Errorf("recorded due time moved to %v", child.Scheduled)
	}
}

func TestFailureSkipsDescendantsOnly(t *testing.T) {
	items := []Item{
		{},                                       // fails
		{After: []int{0}},                        // skipped
		{After: []int{1}},                        // skipped transitively
		{},                                       // unrelated
		{After: []int{3}},                        // runs
		{After: []int{0}, RunAfterFailure: true}, // policy continue
	}
	var mu sync.Mutex
	statuses := map[int]Status{}
	err := ScheduleItems(context.Background(), Config{Concurrency: 2, QueueLimit: 10, Speed: 1}, items,
		func(_ context.Context, s Slot) bool {
			mu.Lock()
			defer mu.Unlock()
			if _, dup := statuses[s.Index]; dup {
				t.Errorf("item %d handled twice", s.Index)
			}
			statuses[s.Index] = s.Status
			return s.Index != 0
		})
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]Status{0: Run, 1: DependencySkipped, 2: DependencySkipped, 3: Run, 4: Run, 5: Run}
	for index, status := range want {
		if statuses[index] != status {
			t.Errorf("item %d = %v, want %v", index, statuses[index], status)
		}
	}
}

func TestParentFinishingBeforeChildIsDueStillWaitsForDue(t *testing.T) {
	due := 30 * time.Millisecond
	items := []Item{{}, {Offset: due, After: []int{0}}}
	var start atomic.Int64
	err := ScheduleItems(context.Background(), Config{Concurrency: 2, QueueLimit: 2, Speed: 1}, items,
		func(_ context.Context, s Slot) bool {
			if s.Index == 1 {
				start.Store(int64(s.Start))
			}
			return true
		})
	if err != nil {
		t.Fatal(err)
	}
	if time.Duration(start.Load()) < due {
		t.Fatalf("started at %v", time.Duration(start.Load()))
	}
}

func TestCancelReachesParkedDependents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	items := []Item{{}, {After: []int{0}}, {Offset: time.Hour, After: []int{1}}}
	var mu sync.Mutex
	statuses := map[int]Status{}
	err := ScheduleItems(ctx, Config{Concurrency: 1, QueueLimit: 3, Speed: 1}, items,
		func(_ context.Context, s Slot) bool {
			mu.Lock()
			defer mu.Unlock()
			statuses[s.Index] = s.Status
			if s.Index == 0 {
				cancel()
			}
			return true
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if statuses[1] != Cancelled || statuses[2] != Cancelled {
		t.Fatalf("statuses = %v", statuses)
	}
}

func TestOverflowOfAReleasedChildIsDropped(t *testing.T) {
	block := make(chan struct{})
	items := []Item{{}, {}, {After: []int{0}}, {After: []int{0}}, {After: []int{0}}}
	var mu sync.Mutex
	counts := map[Status]int{}
	done := make(chan error, 1)
	go func() {
		done <- ScheduleItems(context.Background(), Config{Concurrency: 1, QueueLimit: 1, Speed: 1}, items,
			func(_ context.Context, s Slot) bool {
				if s.Status == Run && s.Index == 1 {
					<-block
				}
				mu.Lock()
				defer mu.Unlock()
				counts[s.Status]++
				return true
			})
	}()
	time.Sleep(20 * time.Millisecond)
	close(block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if total := counts[Run] + counts[Dropped]; total != len(items) {
		t.Fatalf("counts = %v", counts)
	}
}

func TestCycleIsRejectedNotAHang(t *testing.T) {
	items := []Item{{After: []int{1}}, {After: []int{0}}}
	err := ScheduleItems(context.Background(), Config{Concurrency: 1, QueueLimit: 1, Speed: 1}, items,
		func(context.Context, Slot) bool { return true })
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("err = %v", err)
	}
	bad := []Item{{After: []int{9}}}
	if err := ScheduleItems(context.Background(), Config{Concurrency: 1, QueueLimit: 1, Speed: 1}, bad,
		func(context.Context, Slot) bool { return true }); err == nil {
		t.Fatal("accepted an unknown dependency")
	}
}
