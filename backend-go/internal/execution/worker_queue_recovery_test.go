package execution

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"reflect"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/shilin414/cas/backend-go/internal/platform/redisx"
)

// All Redis commands are intercepted; even unexpected commands cannot dial.
type workerQueueHook struct {
	t       *testing.T
	process func(context.Context, goredis.Cmder) error
}

func (h *workerQueueHook) DialHook(goredis.DialHook) goredis.DialHook {
	return func(context.Context, string, string) (net.Conn, error) {
		h.t.Error("unexpected Redis dial")
		return nil, errors.New("network disabled in worker unit tests")
	}
}
func (h *workerQueueHook) ProcessHook(goredis.ProcessHook) goredis.ProcessHook { return h.process }
func (h *workerQueueHook) ProcessPipelineHook(goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(context.Context, []goredis.Cmder) error {
		h.t.Error("unexpected Redis pipeline")
		return errors.New("unexpected pipeline")
	}
}
func queueTestWorker(t *testing.T, process func(context.Context, goredis.Cmder) error) *Worker {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1", MaxRetries: -1})
	client.AddHook(&workerQueueHook{t: t, process: process})
	t.Cleanup(func() { _ = client.Close() })
	rdb := redisx.NewWithPrefix("worker-unit")
	rdb.UniversalClient = client
	return &Worker{RDB: rdb, Provider: "test", Group: "workers", WorkerID: "worker", Log: slog.Default()}
}

func TestWorkerRecreatesMissingGroup(t *testing.T) {
	for _, source := range []string{"read", "reclaim"} {
		for _, raced := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/concurrent_creator=%v", source, raced), func(t *testing.T) {
				available, creates := false, 0
				w := queueTestWorker(t, func(_ context.Context, cmd goredis.Cmder) error {
					switch cmd.Name() {
					case "xgroup":
						creates++
						args := cmd.Args()
						if args[1] != "create" || args[3] != "workers" || args[4] != "0" || args[5] != "mkstream" {
							t.Fatalf("group must replay backlog from 0 with MKSTREAM: %v", args)
						}
						available = true
						if raced {
							return errors.New("BUSYGROUP Consumer Group name already exists")
						}
						cmd.(*goredis.StatusCmd).SetVal("OK")
					case "xreadgroup":
						if !available {
							return errors.New("NOGROUP consumer group was lost")
						}
						args := cmd.Args()
						stream := args[len(args)-2].(string)
						cmd.(*goredis.XStreamSliceCmd).SetVal([]goredis.XStream{{Stream: stream, Messages: []goredis.XMessage{{ID: "1-0", Values: map[string]any{"run_id": "backlog"}}}}})
					case "xautoclaim":
						if !available {
							return errors.New("NOGROUP consumer group was lost")
						}
						cmd.(*goredis.XAutoClaimCmd).SetVal(nil, "0-0")
					default:
						t.Fatalf("unexpected command: %v", cmd.Args())
					}
					return nil
				})
				stream := w.classStreams()[0]
				if source == "read" {
					w.readOne(context.Background(), "consumer", stream)
				} else {
					w.reclaimStream(context.Background(), stream, time.Minute)
				}
				if creates == 0 {
					t.Fatal("missing consumer group was not recreated")
				}
				sm, ok := w.readOne(context.Background(), "consumer", stream)
				if !ok || sm.stream != stream || sm.msg.ID != "1-0" {
					t.Fatalf("backlog unreadable after group recovery: %+v, %v", sm, ok)
				}
			})
		}
	}
}

func TestWorkerQueueErrorsDoNotRecreateGroups(t *testing.T) {
	for _, reply := range []error{goredis.Nil, errors.New("connection reset"), errors.New("WRONGTYPE wrong kind of value")} {
		t.Run(reply.Error(), func(t *testing.T) {
			w := queueTestWorker(t, func(_ context.Context, cmd goredis.Cmder) error {
				if cmd.Name() != "xreadgroup" && cmd.Name() != "xautoclaim" {
					t.Fatalf("unrelated error triggered command %v", cmd.Args())
				}
				return reply
			})
			stream := w.classStreams()[0]
			if _, ok := w.readOne(context.Background(), "consumer", stream); ok {
				t.Fatal("read succeeded on error")
			}
			w.reclaimStream(context.Background(), stream, time.Minute)
		})
	}
}

func TestWorkerReclaimTraversesEmptyPages(t *testing.T) {
	var cursors []string
	acked := false
	w := queueTestWorker(t, func(_ context.Context, cmd goredis.Cmder) error {
		switch cmd.Name() {
		case "xautoclaim":
			cursor := cmd.Args()[5].(string)
			cursors = append(cursors, cursor)
			switch cursor {
			case "0":
				cmd.(*goredis.XAutoClaimCmd).SetVal(nil, "101-0")
			case "101-0":
				cmd.(*goredis.XAutoClaimCmd).SetVal(nil, "201-0")
			case "201-0":
				cmd.(*goredis.XAutoClaimCmd).SetVal([]goredis.XMessage{{ID: "250-0", Values: map[string]any{"run_id": "malformed"}}}, "0-0")
			default:
				t.Fatalf("unexpected cursor %q", cursor)
			}
		case "xack":
			acked = true
			cmd.(*goredis.IntCmd).SetVal(1)
		default:
			t.Fatalf("unexpected command: %v", cmd.Args())
		}
		return nil
	})
	w.reclaimStream(context.Background(), w.classStreams()[0], time.Minute)
	if !reflect.DeepEqual(cursors, []string{"0", "101-0", "201-0"}) || !acked {
		t.Fatalf("reclaim stopped before later pending entry: cursors=%v acked=%v", cursors, acked)
	}
}

func TestWorkerReclaimStopsOnTerminalOrStalledCursor(t *testing.T) {
	for _, next := range []string{"0", "0-0", "10-0"} {
		t.Run(next, func(t *testing.T) {
			calls := 0
			w := queueTestWorker(t, func(_ context.Context, cmd goredis.Cmder) error {
				calls++
				if calls > 2 {
					t.Fatal("reclaim spun after terminal/stalled cursor")
				}
				cmd.(*goredis.XAutoClaimCmd).SetVal(nil, next)
				return nil
			})
			w.reclaimStream(context.Background(), w.classStreams()[0], time.Minute)
			want := 1
			if next == "10-0" {
				want = 2
			}
			if calls != want {
				t.Fatalf("calls = %d, want %d", calls, want)
			}
		})
	}
}

func TestWorkerQueueRecoveryRetriesOnNextPoll(t *testing.T) {
	creates := 0
	w := queueTestWorker(t, func(_ context.Context, cmd goredis.Cmder) error {
		switch cmd.Name() {
		case "xreadgroup":
			return errors.New("NOGROUP group disappeared")
		case "xgroup":
			creates++
			return errors.New("temporarily unavailable")
		default:
			t.Fatalf("unexpected command: %v", cmd.Args())
		}
		return nil
	})
	stream := w.classStreams()[0]
	for poll := 1; poll <= 2; poll++ {
		if _, ok := w.readOne(context.Background(), "consumer", stream); ok {
			t.Fatal("failed recovery returned a message")
		}
		if creates != poll {
			t.Fatalf("recovery must attempt once per poll, attempts=%d poll=%d", creates, poll)
		}
	}
}

func TestWorkerQueueRecoveryHonorsCancellation(t *testing.T) {
	for _, source := range []string{"read", "reclaim"} {
		t.Run(source, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			w := queueTestWorker(t, func(_ context.Context, cmd goredis.Cmder) error {
				calls++
				if cmd.Name() == "xgroup" {
					t.Fatal("cancelled queue operation attempted group recreation")
				}
				cancel()
				return errors.New("NOGROUP group disappeared")
			})
			stream := w.classStreams()[0]
			if source == "read" {
				w.readOne(ctx, "consumer", stream)
			} else {
				w.reclaimStream(ctx, stream, time.Minute)
			}
			if calls != 1 {
				t.Fatalf("calls=%d, want 1", calls)
			}
		})
	}
}

func TestWorkerReclaimHonorsCancellationBetweenEmptyPages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	w := queueTestWorker(t, func(_ context.Context, cmd goredis.Cmder) error {
		calls++
		if calls > 1 {
			t.Fatal("reclaim continued after cancellation")
		}
		cancel()
		cmd.(*goredis.XAutoClaimCmd).SetVal(nil, "10-0")
		return nil
	})
	w.reclaimStream(ctx, w.classStreams()[0], time.Minute)
	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
}

func TestWorkerEnsuresAllPriorityGroupsIdempotently(t *testing.T) {
	groups := make(map[string]int)
	w := queueTestWorker(t, func(_ context.Context, cmd goredis.Cmder) error {
		if cmd.Name() != "xgroup" {
			t.Fatalf("unexpected command: %v", cmd.Args())
		}
		args := cmd.Args()
		if args[1] != "create" || args[3] != "workers" || args[4] != "0" || args[5] != "mkstream" {
			t.Fatalf("unexpected group creation arguments: %v", args)
		}
		stream := args[2].(string)
		groups[stream]++
		if groups[stream] > 1 {
			return errors.New("BUSYGROUP Consumer Group name already exists")
		}
		cmd.(*goredis.StatusCmd).SetVal("OK")
		return nil
	})
	w.ensureGroup(context.Background())
	w.ensureGroup(context.Background())
	if len(groups) != len(PriorityClasses) {
		t.Fatalf("created %d groups, want %d", len(groups), len(PriorityClasses))
	}
	for _, stream := range w.classStreams() {
		if groups[stream] != 2 {
			t.Errorf("group creation attempts for %s = %d, want 2", stream, groups[stream])
		}
	}
}
