package privilege

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/session"
)

type slowStopBackend struct {
	mu       sync.Mutex
	snapshot session.Snapshot
	stops    int
}

func (b *slowStopBackend) Start(context.Context, engine.Options) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.snapshot = session.Snapshot{State: session.Running}
	return nil
}

func (b *slowStopBackend) Snapshot() session.Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.snapshot
}

func (b *slowStopBackend) Stop(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stops++
	if b.stops == 1 {
		b.snapshot.State = session.Stopping
		return context.DeadlineExceeded
	}
	b.snapshot = session.Snapshot{State: session.Stopped}
	return nil
}

func TestHelperRetainsStoppingSessionAfterStopTimeout(t *testing.T) {
	for _, parentCancelled := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "stop request", true: "parent cancelled"}[parentCancelled],
			func(t *testing.T) {
				client, server := net.Pipe()
				defer func() { _ = client.Close() }()
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				backend := &slowStopBackend{}
				finished := make(chan error, 1)
				go func() { finished <- serveController(ctx, server, json.NewDecoder(server), backend, engine.Options{}) }()
				if err := client.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
					t.Fatal(err)
				}
				dec, enc := json.NewDecoder(client), json.NewEncoder(client)
				var update helperUpdate
				for update.State != session.Running {
					if err := dec.Decode(&update); err != nil {
						t.Fatal(err)
					}
				}
				if parentCancelled {
					cancel()
				} else if err := enc.Encode(helperCommand{ID: 1, Action: "stop"}); err != nil {
					t.Fatal(err)
				}
				for update.StopResult == nil {
					if err := dec.Decode(&update); err != nil {
						t.Fatal(err)
					}
				}
				if update.State != session.Stopping ||
					!errors.Is(update.StopResult.err(), context.DeadlineExceeded) {
					t.Fatalf("timeout reported as success: %+v", update)
				}
				select {
				case err := <-finished:
					t.Fatalf("helper abandoned unfinished cleanup: %v", err)
				default:
				}
				backend.mu.Lock()
				backend.snapshot = session.Snapshot{
					State:          session.Failed,
					CleanupPending: true,
					Error:          "DNS rollback denied",
				}
				backend.mu.Unlock()
				for !update.CleanupPending {
					if err := dec.Decode(&update); err != nil {
						t.Fatal(err)
					}
				}
				if err := enc.Encode(helperCommand{ID: 2, Action: "stop"}); err != nil {
					t.Fatal(err)
				}
				for update.StopResult == nil || update.StopResult.ID != 2 {
					if err := dec.Decode(&update); err != nil {
						t.Fatal(err)
					}
				}
				if update.State != session.Stopped || update.CleanupPending ||
					update.StopResult.err() != nil {
					t.Fatalf("restoration retry failed: %+v", update)
				}
				select {
				case err := <-finished:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("helper survived successful cleanup")
				}
			},
		)
	}
}

func TestRemoteStopIgnoresLateReplyFromCancelledAttempt(t *testing.T) {
	for _, newError := range []string{"", "retry DNS rollback denied"} {
		t.Run(
			map[bool]string{true: "retry succeeds", false: "retry fails"}[newError == ""],
			func(t *testing.T) {
				firstReceived := make(chan helperCommand, 1)
				secondReceived := make(chan helperCommand, 1)
				cleanupReceived := make(chan helperCommand, 1)
				allowOldReply := make(chan struct{})
				helperDone := make(chan error, 1)
				r := &remote{launch: func(_ string, address, token string) (<-chan error, error) {
					go func() {
						conn, err := net.Dial("tcp", address)
						if err != nil {
							helperDone <- err
							return
						}
						defer func() { _ = conn.Close() }()
						enc, dec := json.NewEncoder(conn), json.NewDecoder(conn)
						if err := enc.Encode(token); err != nil {
							helperDone <- err
							return
						}
						var options engine.Options
						if err := dec.Decode(&options); err != nil {
							helperDone <- err
							return
						}
						if err := enc.Encode(
							helperUpdate{State: session.Running},
						); err != nil {
							helperDone <- err
							return
						}
						var first, second helperCommand
						if err := dec.Decode(&first); err != nil {
							helperDone <- err
							return
						}
						firstReceived <- first
						if err := dec.Decode(&second); err != nil {
							helperDone <- err
							return
						}
						secondReceived <- second
						<-allowOldReply
						old := helperUpdate{
							State:          session.Failed,
							CleanupPending: true,
							Error:          "old rollback denied",
							StopResult: &helperStopResult{
								ID:    first.ID,
								Error: "old rollback denied",
							},
						}
						if err := enc.Encode(old); err != nil {
							helperDone <- err
							return
						}
						current := helperUpdate{
							State:      session.Stopped,
							StopResult: &helperStopResult{ID: second.ID, Error: newError},
						}
						if newError != "" {
							current.Snapshot = session.Snapshot{
								State:          session.Failed,
								CleanupPending: true,
								Error:          newError,
							}
						}
						if err := enc.Encode(current); err != nil {
							helperDone <- err
							return
						}
						if newError != "" {
							var cleanup helperCommand
							if err := dec.Decode(&cleanup); err != nil {
								helperDone <- err
								return
							}
							cleanupReceived <- cleanup
							if err := enc.Encode(
								helperUpdate{
									State:      session.Stopped,
									StopResult: &helperStopResult{ID: cleanup.ID},
								},
							); err != nil {
								helperDone <- err
								return
							}
						}
						helperDone <- nil
					}()
					return nil, nil
				}}
				ctx, stop := context.WithTimeout(t.Context(), 5*time.Second)
				defer stop()
				if err := r.Start(ctx, engine.Options{}); err != nil {
					t.Fatal(err)
				}
				firstCtx, cancelFirst := context.WithCancel(ctx)
				defer cancelFirst()
				firstResult := make(chan error, 1)
				go func() { firstResult <- r.Stop(firstCtx) }()
				var first helperCommand
				select {
				case first = <-firstReceived:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				cancelFirst()
				select {
				case err := <-firstResult:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("cancelled stop: %v", err)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				secondResult := make(chan error, 1)
				go func() { secondResult <- r.Stop(ctx) }()
				select {
				case second := <-secondReceived:
					if first.ID == second.ID {
						t.Fatal("stop request IDs were reused")
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				close(allowOldReply)
				select {
				case err := <-secondResult:
					if newError == "" && err != nil {
						t.Fatalf("successful retry received old failure: %v", err)
					}
					if newError != "" && (err == nil || !strings.Contains(err.Error(), newError)) {
						t.Fatalf("retry received another attempt's result: %v", err)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if newError != "" {
					cleanup := make(chan error, 1)
					go func() { cleanup <- r.Stop(ctx) }()
					select {
					case <-cleanupReceived:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					select {
					case err := <-cleanup:
						if err != nil {
							t.Fatal(err)
						}
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
				select {
				case err := <-helperDone:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				<-r.done
			},
		)
	}
}
