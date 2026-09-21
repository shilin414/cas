package sse

import (
	"context"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Exercise the real forwarding loop without opening a Redis connection.
// Closing the upstream input must also unblock a forwarder already waiting
// to send to a hub that has stopped reading after its terminal frame.
type lifetimeUpstream struct {
	channel *lifetimeChannel
}

type lifetimeChannel struct {
	*redisUpstreamChannel
	raw       chan *goredis.Message
	closed    chan struct{}
	forwarded chan struct{}
	once      sync.Once
}

func (u *lifetimeUpstream) Subscribe(ctx context.Context, _ string) (HubUpstreamChannel, error) {
	go func() {
		defer close(u.channel.forwarded)
		u.channel.forward(ctx, u.channel.raw)
	}()
	return u.channel, nil
}

func (c *lifetimeChannel) Close() error {
	c.once.Do(func() { close(c.raw); close(c.closed) })
	return nil
}

func TestTerminalReleasesBlockedUpstreamForwarder(t *testing.T) {
	channel := &lifetimeChannel{
		redisUpstreamChannel: &redisUpstreamChannel{out: make(chan []byte, upstreamForwardBuffer)},
		raw:                  make(chan *goredis.Message, upstreamForwardBuffer+2),
		closed:               make(chan struct{}),
		forwarded:            make(chan struct{}),
	}
	channel.raw <- &goredis.Message{Payload: `{"sequence":1,"event_type":"run.completed","payload":{}}`}
	// Fill the production-sized forwarding buffer beyond the terminal.
	// Closing raw alone cannot interrupt the next pending send.
	for i := 0; i < upstreamForwardBuffer+1; i++ {
		channel.raw <- &goredis.Message{Payload: `{"sequence":2,"event_type":"content.chunk","payload":{}}`}
	}
	mgr := NewHubManagerWithUpstream(context.Background(), &lifetimeUpstream{channel}, nil, HubOptions{IdleTTL: time.Hour})
	t.Cleanup(func() {
		mgr.Close()
		select {
		case <-channel.forwarded:
		case <-time.After(time.Second):
			t.Error("forwarder did not stop during cleanup")
		}
	})
	hub := mgr.GetOrCreate("terminal-forwarder")
	select {
	case <-channel.closed:
	case <-time.After(time.Second):
		t.Fatal("hub did not release the terminal upstream")
	}
	select {
	case <-channel.forwarded:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("terminal hub closed its upstream but leaked the blocked forwarding goroutine")
	}
	// Cancellation is upstream-only: terminal replay still reuses this hub.
	if !hub.Serving() || mgr.Lookup(hub.RunID()) != hub {
		t.Fatal("terminal hub must remain available for cached replay")
	}
	if events, ok := hub.ReplayAfter(0); !ok || len(events) != 1 || !events[0].IsTerminal() {
		t.Fatalf("terminal cache lost: events=%v hit=%v", events, ok)
	}
}

func TestExpiredIdleCallbackCannotEvictNewIdlePeriod(t *testing.T) {
	mgr := NewHubManagerWithUpstream(context.Background(), newFakeUpstream(), nil, HubOptions{IdleTTL: time.Hour})
	defer mgr.Close()
	hub := mgr.GetOrCreate("idle-period")

	// Save the old callback, modeling an AfterFunc that already fired and is
	// waiting for hub.mu. Stop cannot retract such a callback.
	hub.mu.Lock()
	expiredGeneration := hub.idleGeneration
	hub.mu.Unlock()
	expiredCallback := func() { hub.evictIfIdle(expiredGeneration) }
	sub, ok := hub.Subscribe(StreamProtocolRangeDelta)
	if !ok {
		t.Fatal("subscribe failed")
	}
	sub.Close(DropReasonHubClosed)
	hub.mu.Lock()
	newTimer := hub.idleTimer
	newGeneration := hub.idleGeneration
	hub.mu.Unlock()
	if newTimer == nil {
		t.Fatal("disconnect did not start a new idle period")
	}

	expiredCallback()
	if !hub.Serving() || mgr.Lookup(hub.RunID()) != hub {
		t.Fatal("callback from the previous idle period evicted the newly idle hub before its TTL")
	}
	hub.mu.Lock()
	timerPreserved := hub.idleTimer == newTimer
	hub.mu.Unlock()
	if !timerPreserved {
		t.Fatal("stale callback cleared the current idle timer")
	}
	// The guard must not disable eviction for the current idle period.
	newTimer.Stop()
	hub.evictIfIdle(newGeneration)
	if hub.Serving() || mgr.Lookup(hub.RunID()) != nil {
		t.Fatal("current idle callback failed to evict the hub")
	}
}
