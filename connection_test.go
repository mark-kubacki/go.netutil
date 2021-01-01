// This file is released into the public domain.

package netutil_test

import (
	"bytes"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-systemd/v22/activation"
	netutil "github.com/wmark/go.netutil"
)

func ExampleAcceptedConnection() {
	// Upgrades the connection to a net.Listener,
	// like in socket- or connection-activated services.

	var ln net.Listener
	if _, acceptedConn := os.LookupEnv("LISTEN_FDS"); acceptedConn {
		fds := activation.Files(true) // Accept=true makes that connections.
		if len(fds) < 1 {
			log.Fatalf("Conn activated, but not been given any FD")
		}
		ln, _ = netutil.AcceptedConnection(fds[0])
	} else {
		// Traditional standalone invocation.
		ln, _ = net.Listen("tcp", "localhost:0")
	}

	server := &http.Server{}
	server.SetKeepAlivesEnabled(false)

	switch err := server.Serve(ln); err {
	case nil, http.ErrServerClosed, os.ErrClosed:
		return
	default:
		log.Fatalf("server.Serve: %v", err)
	}
}

func TestAcceptedConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen: %v\n", err)
	}
	defer listener.Close()

	const responseText = "response text"
	var requestsServed int
	var ln net.Listener
	var server *http.Server
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		// Accept a single connection to get its FD, as if systemd passed it.
		c, err := listener.Accept()
		if err != nil {
			t.Fatalf("net.Accept: %v\n", err)
		}
		tcpConn, ok := c.(*net.TCPConn)
		f, _ := tcpConn.File()
		t.Logf("Accepted: %v, %v, %v\n", tcpConn, ok, f.Fd())

		// Now emulate the typical use-case.
		ln, err = netutil.AcceptedConnection(f)
		if err != nil {
			t.Fatalf("netutil.AcceptedConnection: %v\n", err)
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			requestsServed++
			w.Header().Set("Connection", "close")
			w.Write([]byte(responseText))
		})
		server = &http.Server{Handler: mux, IdleTimeout: 4 * time.Second}
		switch err := server.Serve(ln); err {
		case nil, http.ErrServerClosed, os.ErrClosed:
			return
		default:
			t.Fatalf("server.Serve: %v", err)
		}
	}()

	remote := listener.Addr().String()
	resp, err := http.Get("http://" + remote + "/")
	if err != nil {
		t.Logf("http error: %v\n", err)
	}
	if requestsServed != 1 {
		t.Errorf("Should have served exactly one request, got: %d\n", requestsServed)
	}
	var buf bytes.Buffer
	io.Copy(&buf, resp.Body)
	resp.Body.Close()
	if got := string(buf.Bytes()); got != responseText {
		t.Errorf("Unexpected response,\n -- got: %s\n -- want: %s\n", got, responseText)
	}

	server.Shutdown(context.Background())
	wg.Wait()
	ln.Close() // Try for a panic.

	// Now read after its conclusion.
	switch _, err := ln.Accept(); err {
	case os.ErrClosed:
		return // That's what we want.
	default:
		t.Errorf("The connection is closed, but Accept's error doesn't reflect that. Got: %v\n", err)
	}
}
