// This file is released into the public domain.

package idletracker_test

import (
	"context"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/wmark/idletracker"
)

func Example() {
	ctx, cancelFn := context.WithCancel(context.Background())
	lingerCtx := idletracker.NewIdleTracker(ctx, 15*time.Minute)

	server := &http.Server{
		IdleTimeout: 2 * time.Minute,
		ConnState:   lingerCtx.ConnState,
	}

	ln, _ := net.Listen("tcp", "[::1]:0")
	go func() {
		err := server.Serve(ln)
		if err != nil && err != http.ErrServerClosed {
			// …
			log.Fatalf("serve.Serve: %v\n", err)
		}
	}()
	go func() {
		<-lingerCtx.Done()
		server.Shutdown(ctx)
		cancelFn()
	}()

	<-ctx.Done()
}
