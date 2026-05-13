package loop

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunrf-renlab-ai/pace/pkg/action"
)

// countingBrain counts how many times Decide is called and always returns ignore.
type countingBrain struct {
	calls int32
}

func (b *countingBrain) Decide(ctx context.Context, in DeciderInput) (*Decision, error) {
	atomic.AddInt32(&b.calls, 1)
	return &Decision{Decision: "ignore"}, nil
}

func (b *countingBrain) Calls() int { return int(atomic.LoadInt32(&b.calls)) }

// Single Notify → exactly one brain call after the debounce window.
func TestDebouncerSingleNotify(t *testing.T) {
	s := newState(t)
	b := &countingBrain{}
	l := New(s, b, action.NewRegistry(&fakeNotifier{}))
	l.QuietDebounce = 100 * time.Millisecond
	l.MaxWait = 1 * time.Second
	l.Strategic = time.Hour // suppress

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l.Start(ctx)
	defer l.Stop()

	l.Notify()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.Calls() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := b.Calls(); got != 1 {
		t.Errorf("brain calls = %d, want 1 after debounce", got)
	}
}

// Burst of 10 Notify in quick succession → exactly one brain call (coalesced).
func TestDebouncerCoalescesBurst(t *testing.T) {
	s := newState(t)
	b := &countingBrain{}
	l := New(s, b, action.NewRegistry(&fakeNotifier{}))
	l.QuietDebounce = 200 * time.Millisecond
	l.MaxWait = 2 * time.Second
	l.Strategic = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l.Start(ctx)
	defer l.Stop()

	for i := 0; i < 10; i++ {
		l.Notify()
		time.Sleep(20 * time.Millisecond) // each ping resets quiet timer
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.Calls() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// After the last ping, ~200ms quiet → 1 call.
	if got := b.Calls(); got != 1 {
		t.Errorf("brain calls = %d, want 1 (burst should coalesce)", got)
	}
}

// MaxWait kicks in if events keep arriving without quiet period.
func TestDebouncerMaxWait(t *testing.T) {
	s := newState(t)
	b := &countingBrain{}
	l := New(s, b, action.NewRegistry(&fakeNotifier{}))
	l.QuietDebounce = 500 * time.Millisecond // never reachable in this test
	l.MaxWait = 200 * time.Millisecond       // kicks first
	l.Strategic = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l.Start(ctx)
	defer l.Stop()

	// Hammer notifies every 50ms — quiet window never closes, max-wait must fire.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				l.Notify()
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if b.Calls() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(stop)
	if got := b.Calls(); got < 1 {
		t.Errorf("brain calls = %d, max-wait should have forced ≥1 call", got)
	}
}

// Notify is non-blocking even when no consumer is reading.
func TestNotifyNonBlocking(t *testing.T) {
	s := newState(t)
	l := New(s, nil, action.NewRegistry(&fakeNotifier{}))
	// Don't Start — channel has no consumer.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			l.Notify()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Notify blocked")
	}
}
