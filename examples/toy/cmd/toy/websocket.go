package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/crossnokaye/goa-typescript/examples/toy/gen/toy"
)

// The WebSocket methods of the toy service. goa upgrades the connection on
// the first Send, Recv or Close, after the endpoint's security handler has
// run: an error returned before that is an ordinary HTTP response, which
// the browser WebSocket API reports as a failed connection.
//
// Every method defers dropConn: goa's stream Close does not close the
// connection when it cannot write its close frame (because the client
// closed first, say), and a client then waits for the server to drop it.

func (s *toyService) WatchTicks(ctx context.Context, p *toy.WatchTicksPayload, stream toy.WatchTicksServerStream) error {
	if p.ID == "missing" {
		return toy.MakeNotFound(fmt.Errorf("thing %q not found", p.ID))
	}
	defer dropConn(ctx)
	defer func() { _ = stream.Close() }()
	for i := 1; i <= p.Count; i++ {
		if i > 1 && p.DelayMs > 0 {
			select {
			case <-ctx.Done():
				// cancelOnClose saw the client go away.
				return nil
			case <-time.After(time.Duration(p.DelayMs) * time.Millisecond):
			}
		}
		tick := &toy.Tick{N: i, ID: p.ID, At: time.Date(2026, 1, 2, 3, 4, i, 0, time.UTC).Format(time.RFC3339)}
		if err := stream.SendWithContext(ctx, tick); err != nil {
			return err
		}
	}
	// Close sends the normal closure (1000) that ends the client's iteration.
	return stream.Close()
}

func (s *toyService) EchoWs(ctx context.Context, p *toy.EchoWsPayload, stream toy.EchoWsServerStream) error {
	defer dropConn(ctx)
	defer func() { _ = stream.Close() }()
	for {
		msg, err := stream.RecvWithContext(ctx)
		if errors.Is(err, io.EOF) {
			// The client sent goa's null: it has nothing more to say.
			return stream.Close()
		}
		if err != nil {
			return err
		}
		if msg.Text == "kick" {
			return closeWith(ctx, 4001, "kicked")
		}
		text := strings.Repeat(strings.ToUpper(msg.Text), msg.Repeat)
		if p.Prefix != nil {
			text = *p.Prefix + text
		}
		if err := stream.SendWithContext(ctx, &toy.EchoWsResult{Text: text, Length: len(msg.Text)}); err != nil {
			return err
		}
	}
}

func (s *toyService) SumInts(ctx context.Context, _ *toy.SumIntsPayload, stream toy.SumIntsServerStream) error {
	defer dropConn(ctx)
	total := &toy.SumIntsResult{}
	for {
		n, err := stream.RecvWithContext(ctx)
		if errors.Is(err, io.EOF) {
			// goa writes the result, then drops the connection without a
			// close frame.
			return stream.SendAndCloseWithContext(ctx, total)
		}
		if err != nil {
			return err
		}
		total.Sum += n
		total.Count++
	}
}

// cancelOnClose is the connection configurer of watch_ticks, which never
// reads from the socket: it reads in the background, which answers the
// client's close frame, and cancels the method's context when the client
// goes away.
func cancelOnClose(conn *websocket.Conn, cancel context.CancelFunc) *websocket.Conn {
	go func() {
		for {
			if _, _, err := conn.NextReader(); err != nil {
				cancel()
				return
			}
		}
	}()
	return conn
}

type connKey struct{}

// connHolder receives the connection of the request that carries it, so a
// method can close it with a code goa's stream does not offer.
type connHolder struct {
	conn *websocket.Conn
}

// withConnHolder gives every request a connHolder.
func withConnHolder(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), connKey{}, &connHolder{}))
}

// holdingUpgrader upgrades like gorilla's upgrader, accepting any origin, and
// records the connection in the request's connHolder.
type holdingUpgrader struct {
	websocket.Upgrader
}

func newUpgrader() *holdingUpgrader {
	return &holdingUpgrader{websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}}
}

func (u *holdingUpgrader) Upgrade(w http.ResponseWriter, r *http.Request, h http.Header) (*websocket.Conn, error) {
	conn, err := u.Upgrader.Upgrade(w, r, h)
	if err == nil {
		if holder, ok := r.Context().Value(connKey{}).(*connHolder); ok {
			holder.conn = conn
		}
	}
	return conn, err
}

// dropConn closes the request's WebSocket connection, if it was upgraded.
func dropConn(ctx context.Context) {
	if holder, ok := ctx.Value(connKey{}).(*connHolder); ok && holder.conn != nil {
		_ = holder.conn.Close()
	}
}

// closeWith closes the request's WebSocket with the given close code.
func closeWith(ctx context.Context, code int, reason string) error {
	holder, ok := ctx.Value(connKey{}).(*connHolder)
	if !ok || holder.conn == nil {
		return errors.New("no WebSocket connection to close")
	}
	msg := websocket.FormatCloseMessage(code, reason)
	if err := holder.conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(time.Second)); err != nil {
		return err
	}
	return holder.conn.Close()
}
