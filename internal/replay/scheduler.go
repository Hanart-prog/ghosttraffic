// Package replay schedules recorded requests against one target. The schedule
// is open-loop: due times are fixed up front and never depend on responses, so
// a stalled target accumulates visible scheduling delay (ADR 0004).
package replay

import (
	"cmp"
	"context"
	"errors"
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
)

// Slot is one scheduling decision. Offsets are relative to the run start.
// Start and Delay are set only for Run.
type Slot struct {
	Index     int
	Status    Status
	Scheduled time.Duration
	Start     time.Duration
	Delay     time.Duration
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

// Schedule calls handle exactly once per offset, concurrently from up to
// Concurrency + 1 goroutines, and returns after every call has finished. It
// returns ctx's error if the run was cancelled.
func Schedule(ctx context.Context, cfg Config, offsets []time.Duration, handle func(context.Context, Slot)) error {
	if cfg.Concurrency < 1 || cfg.QueueLimit < 1 || !(cfg.Speed > 0) {
		return errors.New("replay: concurrency, queue limit, and speed must be positive")
	}
	clock := cfg.Clock
	if clock == nil {
		clock = realClock{}
	}

	order := make([]int, len(offsets))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(offsets[a], offsets[b]) })

	start := clock.Now()
	queue := make(chan Slot, cfg.QueueLimit)
	var workers sync.WaitGroup
	for range cfg.Concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for slot := range queue {
				if ctx.Err() != nil {
					slot.Status = Cancelled
				} else {
					slot.Start = clock.Now().Sub(start)
					slot.Delay = max(slot.Start-slot.Scheduled, 0)
				}
				handle(ctx, slot)
			}
		}()
	}

	for _, index := range order {
		slot := Slot{Index: index, Scheduled: time.Duration(float64(offsets[index]) / cfg.Speed)}
		if wait := slot.Scheduled - clock.Now().Sub(start); wait > 0 {
			select {
			case <-clock.After(wait):
			case <-ctx.Done():
			}
		}
		if ctx.Err() != nil {
			slot.Status = Cancelled
			handle(ctx, slot)
			continue
		}
		select {
		case queue <- slot:
		default:
			slot.Status = Dropped
			handle(ctx, slot)
		}
	}
	close(queue)
	workers.Wait()
	return ctx.Err()
}

// TargetOrder gives the pass order for a 1-based trial, alternating so neither
// target always runs second on warm caches (ADR 0003).
func TargetOrder(trial int) [2]string {
	if trial%2 == 1 {
		return [2]string{"baseline", "candidate"}
	}
	return [2]string{"candidate", "baseline"}
}
