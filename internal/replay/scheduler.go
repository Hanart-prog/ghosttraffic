// Package replay schedules recorded requests against one target. The schedule
// is open-loop: due times are fixed up front and never depend on responses, so
// a stalled target accumulates visible scheduling delay (ADR 0004).
package replay

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// Status says what the scheduler decided for one item.
type Status int

const (
	// Run means a worker slot is held: the handler should send the request.
	Run Status = iota
	// Dropped means the queue was full at the due time. Nothing may be sent.
	Dropped
	// Cancelled means the context ended first. Nothing may be sent.
	Cancelled
	// DependencySkipped means a prerequisite failed. Nothing may be sent.
	DependencySkipped
)

// Slot is one scheduling decision. Offsets are relative to the run start.
// Start and Delay are set only for Run.
type Slot struct {
	Index          int
	Status         Status
	Scheduled      time.Duration // recorded due time, scaled by speed
	Ready          time.Duration // when it could run, after any dependencies
	DependencyWait time.Duration
	Start          time.Duration
	Delay          time.Duration
}

// Clock abstracts time so tests can check due times exactly.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Config bounds a run: goroutines stay at Concurrency + 1 and waiting items at
// QueueLimit, whatever the arrival rate.
type Config struct {
	Concurrency int
	QueueLimit  int
	Speed       float64
	Clock       Clock // nil means the real clock
}

// Item is one schedulable request. After lists the indexes that must finish
// first. RunAfterFailure runs it even if one of those failed.
type Item struct {
	Offset          time.Duration
	After           []int
	RunAfterFailure bool
}

// Schedule runs independent items. See ScheduleItems.
func Schedule(ctx context.Context, cfg Config, offsets []time.Duration, handle func(context.Context, Slot)) error {
	items := make([]Item, len(offsets))
	for i, offset := range offsets {
		items[i].Offset = offset
	}
	return ScheduleItems(ctx, cfg, items, func(ctx context.Context, slot Slot) bool {
		handle(ctx, slot)
		return true
	})
}

// run is the state of one ScheduleItems call. mu guards the two slices and is
// never held while calling handle.
type run struct {
	ctx    context.Context
	cfg    Config
	clock  Clock
	start  time.Time
	queue  chan Slot
	handle func(context.Context, Slot) bool
	items  []Item
	// children[i] are the items waiting on i. pending would be a data race
	// without mu: workers finish parents while the dispatcher marks items due.
	children [][]int
	all      sync.WaitGroup

	mu      sync.Mutex
	waiting []int  // unfinished parents, plus one until the item is due
	failed  []bool // a parent did not succeed
	ready   []time.Duration
}

// ScheduleItems calls handle exactly once per item, concurrently from up to
// Concurrency + 1 goroutines, and returns after every call has finished. It
// returns ctx's error if the run was cancelled. handle reports whether the item
// succeeded, which decides whether dependents run.
//
// An item is ready at the later of its due time and its last parent's finish.
// Delay counts from ready, and the wait for parents is reported separately, so a
// slow parent is not charged to its child.
func ScheduleItems(ctx context.Context, cfg Config, items []Item, handle func(context.Context, Slot) bool) error {
	if cfg.Concurrency < 1 || cfg.QueueLimit < 1 || !(cfg.Speed > 0) {
		return errors.New("replay: concurrency, queue limit, and speed must be positive")
	}
	r := &run{
		ctx: ctx, cfg: cfg, clock: cfg.Clock, items: items, handle: handle,
		queue:    make(chan Slot, cfg.QueueLimit),
		children: make([][]int, len(items)),
		waiting:  make([]int, len(items)),
		failed:   make([]bool, len(items)),
		ready:    make([]time.Duration, len(items)),
	}
	if r.clock == nil {
		r.clock = realClock{}
	}
	for i, item := range items {
		r.waiting[i] = len(item.After) + 1
		for _, parent := range item.After {
			if parent < 0 || parent >= len(items) {
				return fmt.Errorf("replay: item %d depends on unknown item %d", i, parent)
			}
			r.children[parent] = append(r.children[parent], i)
		}
	}
	if err := r.checkAcyclic(); err != nil {
		return err
	}

	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(items[a].Offset, items[b].Offset) })

	r.start = r.clock.Now()
	r.all.Add(len(items))
	var workers sync.WaitGroup
	for range cfg.Concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for slot := range r.queue {
				if ctx.Err() != nil {
					slot.Status = Cancelled
					r.finish(slot, false)
					continue
				}
				slot.Start = r.now()
				slot.Delay = max(slot.Start-slot.Ready, 0)
				r.finish(slot, handle(ctx, slot))
			}
		}()
	}

	for _, index := range order {
		due := r.scheduled(index)
		if wait := due - r.now(); wait > 0 {
			select {
			case <-r.clock.After(wait):
			case <-ctx.Done():
			}
		}
		r.settle(index, due, false)
	}
	r.all.Wait()
	close(r.queue)
	workers.Wait()
	return ctx.Err()
}

func (r *run) now() time.Duration { return r.clock.Now().Sub(r.start) }

func (r *run) scheduled(index int) time.Duration {
	return time.Duration(float64(r.items[index].Offset) / r.cfg.Speed)
}

// checkAcyclic stops a dependency cycle from waiting forever.
func (r *run) checkAcyclic() error {
	remaining := make([]int, len(r.items))
	var free []int
	for i, item := range r.items {
		remaining[i] = len(item.After)
		if remaining[i] == 0 {
			free = append(free, i)
		}
	}
	seen := 0
	for len(free) > 0 {
		index := free[len(free)-1]
		free = free[:len(free)-1]
		seen++
		for _, child := range r.children[index] {
			if remaining[child]--; remaining[child] == 0 {
				free = append(free, child)
			}
		}
	}
	if seen != len(r.items) {
		return errors.New("replay: dependency cycle")
	}
	return nil
}

// settle records that one thing index was waiting for has happened, and
// dispatches it when nothing is left.
func (r *run) settle(index int, at time.Duration, parentFailed bool) {
	r.mu.Lock()
	r.ready[index] = max(r.ready[index], at)
	r.failed[index] = r.failed[index] || parentFailed
	r.waiting[index]--
	done := r.waiting[index] == 0
	ready, failed := r.ready[index], r.failed[index]
	r.mu.Unlock()
	if done {
		r.dispatch(index, ready, failed)
	}
}

func (r *run) dispatch(index int, ready time.Duration, failed bool) {
	due := r.scheduled(index)
	slot := Slot{Index: index, Scheduled: due, Ready: ready, DependencyWait: max(ready-due, 0)}
	switch {
	case r.ctx.Err() != nil:
		slot.Status = Cancelled
	case failed && !r.items[index].RunAfterFailure:
		slot.Status = DependencySkipped
	default:
		select {
		case r.queue <- slot:
			return
		default:
			slot.Status = Dropped
		}
	}
	r.handle(r.ctx, slot)
	r.finish(slot, false)
}

// finish releases dependents before counting this item done, so the queue
// cannot close while a dependent is still on its way in.
func (r *run) finish(slot Slot, ok bool) {
	at := r.now()
	for _, child := range r.children[slot.Index] {
		r.settle(child, at, !ok)
	}
	r.all.Done()
}

// TargetOrder gives the pass order for a 1-based trial, alternating so neither
// target always runs second on warm caches (ADR 0003).
func TargetOrder(trial int) [2]string {
	if trial%2 == 1 {
		return [2]string{"baseline", "candidate"}
	}
	return [2]string{"candidate", "baseline"}
}
