package model

import (
	"context"
	"sync"
	"time"
)

// Clock is how the handlers and the rules read the time and wait. Production
// uses SystemClock; tests use FakeClock so that the 25 s verdict wait runs in
// no time and expiry checks are deterministic.
type Clock interface {
	Now() time.Time
	// Sleep waits for d, or until ctx is done, in which case it returns ctx.Err().
	Sleep(ctx context.Context, d time.Duration) error
}

// SystemClock is the real clock.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

func (SystemClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// FakeClock is a clock under the test's control. Sleep advances the clock by
// the requested duration without waiting, then calls OnSleep when set, so a
// test can decide a request "while" the handler is waiting for the verdict.
type FakeClock struct {
	mu      sync.Mutex
	now     time.Time
	OnSleep func(now time.Time)
}

// NewFakeClock returns a fake clock set to t.
func NewFakeClock(t time.Time) *FakeClock { return &FakeClock{now: t.UTC()} }

func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Set moves the clock to t.
func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	c.now = t.UTC()
	c.mu.Unlock()
}

// Advance moves the clock forward by d.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func (c *FakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.Advance(d)
	if c.OnSleep != nil {
		c.OnSleep(c.Now())
	}
	return nil
}
