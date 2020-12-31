// This file is released into the public domain.

package netutil_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	netutil "github.com/wmark/go.netutil"
)

func ExampleIdleTracker() {
	// Stops the server after 15 minutes without any connection.

	ctx, cancelFn := context.WithCancel(context.Background())
	lingerCtx := netutil.NewIdleTracker(ctx, 15*time.Minute)

	server := &http.Server{
		IdleTimeout: 2 * time.Minute,
		ConnState:   lingerCtx.ConnState,
	}
	ln, _ := net.Listen("tcp", "localhost:0")

	go func() {
		<-lingerCtx.Done()
		server.Shutdown(ctx)
	}()

	_ = server.Serve(ln)
	cancelFn()
}

func TestImplementsStringer(t *testing.T) {
	parentCtx, cancelParent := context.WithCancel(context.Background())
	i := netutil.NewIdleTracker(parentCtx, 1*time.Second)
	got := fmt.Sprintf("%v", i)
	cancelParent()

	if want := i.String(); got != want {
		t.Errorf("fmt.Stringer is not implemented properly, want %s got %s",
			want, got)
	}
}

func TestParentPassthrough(t *testing.T) {
	rootCtx := context.WithValue(context.Background(), "key", "foo")
	parentCtx, cancelParent := context.WithCancel(rootCtx)
	i := netutil.NewIdleTracker(parentCtx, 0)

	if err := i.Err(); err != nil {
		t.Errorf("A fresh IdleTracker gave an error, but shouldn: %v", err)
	}
	select {
	case _, open := <-i.Done():
		if open {
			t.Fatal("Done, but shouldn't.")
		}
		t.Fatal("Done channel was not drained, but should.")
	default:
	}
	if a, b := i.Value("key"), rootCtx.Value("key"); a != b {
		t.Errorf("IdleTracker and parent don't agree on a value:\n  %v != %v",
			a, b)
	}

	cancelParent()
	<-time.After(5 * time.Millisecond)

	select {
	case _, open := <-i.Done():
		if open {
			t.Error("Done is not closed after cancelling the parent.")
		}
	default:
		t.Error("IdleTracker is not done although the parent has been cancelled.")
	}
	if err := i.Err(); err != parentCtx.Err() {
		t.Errorf("The parent has been cancelled, but IdleTracker didn't propagate its Err: %v", err)
	}
}

func TestDeadParent(t *testing.T) {
	// Covers the path that avoids a goroutine.
	parentCtx, cancelParent := context.WithCancel(context.Background())
	cancelParent()
	i := netutil.NewIdleTracker(parentCtx, 1*time.Second)

	if err := i.Err(); err != parentCtx.Err() {
		t.Errorf("The parent has been cancelled, but IdleTracker didn't propagate its Err: %v", err)
	}
}

func TestEmptyCtxParent(t *testing.T) {
	// Rules out any errors due to a 'nil' returned somewhere.
	emptyCtx := context.Background()
	i := netutil.NewIdleTracker(emptyCtx, 100*time.Millisecond)

	select {
	case _, open := <-i.Done():
		if !open {
			t.Error("Done fired but should block.")
		}
	default:
	}

	<-time.After(5*time.Millisecond + 100*time.Millisecond)
	select {
	case _, open := <-i.Done():
		if open {
			t.Error("Done should be closed by now, but had a value")
		}
	default:
		t.Error("Done should be closed by now")
	}
}

func TestDeadlineAspect(t *testing.T) {
	// Rules out any errors due to a 'nil' returned somewhere.
	parentCtx, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	i := netutil.NewIdleTracker(parentCtx, 100*time.Millisecond)

	d, onDeadline := i.Deadline()
	if !onDeadline {
		t.Fatal("IdleTracker erroneously claims its not on a deadline.")
	}
	if d.Before(time.Now()) {
		t.Fatal("The Deadline is not in the future.")
	}

	<-time.After(5*time.Millisecond + 100*time.Millisecond)
	d, _ = i.Deadline()
	if !d.Before(time.Now()) {
		t.Fatal("After waiting past the deadline, it's actually not in the past.")
	}

	select {
	case _, open := <-i.Done():
		if open {
			t.Error("Done should be closed by now, but had a value")
		}
	default:
		t.Error("Done should be closed by now")
	}
	if err := i.Err(); err != context.DeadlineExceeded {
		t.Fatalf("Past deadline, the error is not context.DeadlineExceeded: %v",
			err)
	}

	cancelParent()

	if err := i.Err(); err != context.DeadlineExceeded {
		t.Fatalf("Cancelling the parent must not overwrite IdleTracker's own permErr: %v",
			err)
	}
}

func TestConnectionTracking(t *testing.T) {
	parentCtx, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	i := netutil.NewIdleTracker(parentCtx, 100*time.Millisecond)

	originalDeadline, _ := i.Deadline()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		<-time.After(20 * time.Millisecond)
		fmt.Fprint(w, "spam is best served with spam")
	})
	server := &http.Server{
		Handler:     mux,
		IdleTimeout: 2 * time.Minute,
		ConnState:   i.ConnState,
	}
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		ln, _ = net.Listen("tcp", "127.0.0.1:0")
	}
	var teardownByIdleTracker bool
	var teardownCancel context.CancelFunc
	go func() {
		<-i.Done()
		teardownByIdleTracker = true
		tearDownCtx, c := context.WithTimeout(parentCtx, 20*time.Millisecond)
		teardownCancel = c
		server.Shutdown(tearDownCtx)
	}()
	go server.Serve(ln)
	localEndpoint := "http://" + ln.Addr().String() + "/"

	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:       10,
			IdleConnTimeout:    30 * time.Second,
			DisableCompression: true,
		},
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		res, err := client.Get(localEndpoint)
		if err != nil {
			t.Logf("client.Get has an error: %v", err)
		}
		if res.Body != nil {
			ioutil.ReadAll(res.Body)
			res.Body.Close()
		}
		wg.Done()
	}()
	<-time.After(1 * time.Millisecond) // yield
	if _, onDeadline := i.Deadline(); onDeadline {
		t.Error("With one fresh (idle) connection, IdleTracker should not be on a deadline")
	}
	wg.Wait()
	client.CloseIdleConnections()
	<-time.After(5 * time.Millisecond) // yield

	advancedDeadline, _ := i.Deadline()
	if !originalDeadline.Before(advancedDeadline) {
		t.Fatalf("After a connection, the deadline has not been advanced. Was %v, is %v",
			originalDeadline, advancedDeadline)
	}

	<-time.After(105 * time.Millisecond)
	if !teardownByIdleTracker {
		t.Error("IdleTracker was not done after its deadline")
	}
	if teardownCancel != nil {
		teardownCancel()
	}
}
