package privilege

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"os"
	"time"

	"github.com/dalugm/veer/engine"
	"github.com/dalugm/veer/session"
)

type helperOptions struct {
	engine.Options
	LogReader string
}

func authenticate(conn net.Conn, token string) (*bufio.Reader, error) {
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(conn, 1024)
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return nil, err
	}
	var got string
	if err = json.Unmarshal(line, &got); err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
		return nil, errors.New("invalid helper token")
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return nil, err
	}
	return reader, nil
}

// Serve is only entered through the private helper dispatch in main.
func Serve(parent context.Context, address, token string) (result error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		return errors.New("helper requires a loopback address")
	}
	if len(token) != 64 {
		return errors.New("invalid helper token length")
	}
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(parent, "tcp4", address)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if err = json.NewEncoder(conn).Encode(token); err != nil {
		return err
	}
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	dec := json.NewDecoder(conn)
	var options helperOptions
	if err = dec.Decode(&options); err != nil {
		return err
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return err
	}
	if err := validateLogReader(options.LogReader); err != nil {
		return err
	}
	controller := session.New(
		engine.Xray{},
		session.WithLogAccess(func(ctx context.Context, file *os.File) error {
			return grantLogRead(ctx, file, options.LogReader)
		}),
	)
	return serveController(parent, conn, dec, controller, options.Options)
}

// helperUpdate keeps the session snapshot and the acknowledgement for one stop
// operation together. A failed restoration keeps the helper and its snapshot alive.
type helperUpdate struct {
	session.Snapshot
	StopResult *helperStopResult `json:",omitempty"`
}

// helperCommand and its reply share an ID so cancelled callers can discard
// their eventual reply without affecting a later restoration attempt.
type helperCommand struct {
	ID     uint64
	Action string
}

type helperStopResult struct {
	ID               uint64
	Error            string
	DeadlineExceeded bool
	Canceled         bool
}

func (result helperStopResult) err() error {
	if result.Error == "" {
		return nil
	}
	err := errors.New(result.Error)
	if result.DeadlineExceeded {
		return errors.Join(context.DeadlineExceeded, err)
	}
	if result.Canceled {
		return errors.Join(context.Canceled, err)
	}
	return err
}

func serveController(
	parent context.Context,
	conn net.Conn,
	dec *json.Decoder,
	controller Backend,
	options engine.Options,
) (result error) {
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		result = errors.Join(result, controller.Stop(cleanup))
	}()
	type request struct {
		command helperCommand
		err     error
	}
	commands := make(chan request)
	readerDone := make(chan struct{})
	defer close(readerDone)
	go func() {
		for {
			var command helperCommand
			err := dec.Decode(&command)
			select {
			case commands <- request{command, err}:
			case <-readerDone:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	started := make(chan error, 1)
	go func() { started <- controller.Start(ctx, options) }()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	send := func(snapshot session.Snapshot, stopResult *helperStopResult) error {
		if err := conn.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
			return err
		}
		return json.NewEncoder(conn).
			Encode(helperUpdate{Snapshot: snapshot, StopResult: stopResult})
	}
	stopping := false
	stopSession := func(id uint64) (bool, error) {
		stopping = true
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		err := controller.Stop(cleanup)
		stop()
		reply := &helperStopResult{ID: id}
		if err != nil {
			reply.Error = err.Error()
			reply.DeadlineExceeded = errors.Is(err, context.DeadlineExceeded)
			reply.Canceled = errors.Is(err, context.Canceled)
		}
		s := controller.Snapshot()
		if sendErr := send(s, reply); sendErr != nil {
			return true, errors.Join(err, sendErr)
		}
		return (s.State == session.Stopped || s.State == session.Failed) && !s.CleanupPending, err
	}
	cancelled := ctx.Done()
	for {
		select {
		case request := <-commands:
			if request.err != nil {
				return request.err
			}
			if request.command.Action != "stop" || request.command.ID == 0 {
				return errors.New("invalid helper command")
			}
			if finished, err := stopSession(request.command.ID); finished {
				return err
			}
		case <-cancelled:
			if finished, err := stopSession(0); finished {
				return err
			}
			cancelled = nil
		case err := <-started:
			if sendErr := send(controller.Snapshot(), nil); sendErr != nil {
				return sendErr
			}
			if err != nil && !controller.Snapshot().CleanupPending {
				return err
			}
		case <-ticker.C:
			s := controller.Snapshot()
			if s.State == session.Stopped && !stopping {
				continue
			}
			if err := send(s, nil); err != nil {
				return err
			}
			if s.State == session.Stopped {
				return nil
			}
			if s.State == session.Failed && !s.CleanupPending {
				return errors.New(s.Error)
			}
		}
	}
}
