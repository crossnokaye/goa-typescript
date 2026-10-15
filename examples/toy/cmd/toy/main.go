// Command toy serves the toy design so that the generated TypeScript client
// can be tested against a real goa server.
//
// It needs the code under gen/, which is not committed: run `goa gen` first
// (see the README next to the design).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	goahttp "goa.design/goa/v3/http"

	"github.com/crossnokaye/goa-typescript/examples/toy/gen/echo"
	echosvr "github.com/crossnokaye/goa-typescript/examples/toy/gen/http/echo/server"
	toysvr "github.com/crossnokaye/goa-typescript/examples/toy/gen/http/toy/server"
	"github.com/crossnokaye/goa-typescript/examples/toy/gen/toy"
)

func main() {
	var (
		addr  = flag.String("http-addr", "127.0.0.1:0", "address to listen on; port 0 picks a free one")
		token = flag.String("token", "toy-token", "the JWT and the API key that requests must present")
	)
	flag.Parse()

	var (
		toyEndpoints  = toy.NewEndpoints(&toyService{token: *token})
		echoEndpoints = echo.NewEndpoints(&echoService{token: *token})
		mux           = goahttp.NewMuxer()
		dec           = goahttp.RequestDecoder
		enc           = goahttp.ResponseEncoder
	)
	toysvr.Mount(mux, toysvr.New(toyEndpoints, mux, dec, enc, nil, nil,
		newUpgrader(), &toysvr.ConnConfigurer{WatchTicksFn: cancelOnClose}))
	echosvr.Mount(mux, echosvr.New(echoEndpoints, mux, dec, enc, nil, nil))

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: recordAuthorization(mux), ReadHeaderTimeout: 10 * time.Second}

	// Tests wait for this exact line to learn the port.
	fmt.Printf("listening on http://%s\n", ln.Addr())

	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve: %v", err)
		}
	case <-stop:
	}

	// Streams in flight would hold a graceful shutdown open, so it is bounded.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		_ = srv.Close()
	}
}

type requestHeadersKey struct{}

// requestHeaders is what the service methods report about the raw request.
type requestHeaders struct {
	authorization bool
	accept        string
}

// recordAuthorization notes whether a request carried an Authorization
// header, which stream_public_sse reports back, and its Accept header, which
// stream_or_list checks. It also gives the request a connHolder for the
// WebSocket methods.
func recordAuthorization(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen := requestHeaders{authorization: r.Header.Get("Authorization") != "", accept: r.Header.Get("Accept")}
		next.ServeHTTP(w, withConnHolder(r.WithContext(context.WithValue(r.Context(), requestHeadersKey{}, seen))))
	})
}

func authorizationSeen(ctx context.Context) bool {
	seen, _ := ctx.Value(requestHeadersKey{}).(requestHeaders)
	return seen.authorization
}

func acceptSeen(ctx context.Context) string {
	seen, _ := ctx.Value(requestHeadersKey{}).(requestHeaders)
	return seen.accept
}
