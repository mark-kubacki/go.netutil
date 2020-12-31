// Copyright 2017 Mark Kubacki. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package netutil contains specialty network utilities
// for on-prem “cloud”-like services.
package netutil

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"
)

var _ context.Context = &IdleTracker{}

// IdleTracker is done after no new connections happened for some time.
// This can be used to stop idle services.
//
// It can be used in place of a context.WithDeadline to bind any
// lifetime/runtime of residual work to that of the server's.
type IdleTracker struct {
	mu       sync.RWMutex
	dangling map[net.Conn]struct{}

	timer    *time.Timer
	deadline time.Time
	patience time.Duration

	parent  context.Context
	done    <-chan struct{}
	permErr error
}

// NewIdleTracker returns an instance with a running deadline timer.
// That is, even absent any original connection, the service will have a lifetime.
//
// Don't reuse this as its assumption is that a server that has been torn down won't be revived.
func NewIdleTracker(parent context.Context, patience time.Duration) *IdleTracker {
	if patience <= 0 {
		patience = 15 * time.Minute
	}
	t := time.NewTimer(patience)
	doneChan := make(chan struct{})
	i := &IdleTracker{
		done:     doneChan,
		dangling: make(map[net.Conn]struct{}),
		patience: patience,
		timer:    t,
		deadline: time.Now().Add(patience),
		parent:   parent,
	}

	parentDone := parent.Done()
	if parentDone == nil {
		// Cannot be cancelled, ever, therefore rely on our timer and skip racking up its counter.
		go func() {
			<-t.C
			i.permErr = context.DeadlineExceeded
			close(doneChan)
		}()
		return i
	}

	select {
	case <-parentDone:
		// Avoid a goroutine.
		i.permErr = parent.Err()
		i.deadline = time.Now()
		close(doneChan)
		return i
	default:
	}

	go func() {
		select {
		case <-parent.Done():
			i.permErr = parent.Err()
		case <-t.C:
			i.permErr = context.DeadlineExceeded
		}
		close(doneChan)
	}()
	return i
}

// ConnState implements the net/http.Server.ConnState interface.
func (t *IdleTracker) ConnState(conn net.Conn, state http.ConnState) {
	t.mu.Lock()
	defer t.mu.Unlock()

	oldActive := len(t.dangling)
	switch state {
	case http.StateNew, http.StateActive:
		t.dangling[conn] = struct{}{}
		if oldActive == 0 {
			t.timer.Stop()
		}
	case http.StateHijacked:
		delete(t.dangling, conn)
	case http.StateIdle, http.StateClosed:
		delete(t.dangling, conn)
		if oldActive > 0 && len(t.dangling) == 0 {
			t.timer.Stop()
			t.timer.Reset(t.patience)
			t.deadline = time.Now().Add(t.patience)
		}
	}
}

// Deadline implements the context.Context interface
// but breaks the promise of always returning the same deadline.
func (t *IdleTracker) Deadline() (deadline time.Time, ok bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.dangling) > 0 {
		return // ok will be false as we're not idle waiting.
	}
	return t.deadline, true
}

// Done implements the context.Context interface.
func (t *IdleTracker) Done() <-chan struct{} {
	return t.done
}

// Err implements the context.Context interface.
func (t *IdleTracker) Err() error {
	return t.permErr
}

// Value implements the context.Context interface.
func (t *IdleTracker) Value(key interface{}) interface{} {
	return t.parent.Value(key)
}

// String implements the fmt.Stringer interface.
func (*IdleTracker) String() string {
	return "netutil.IdleTracker"
}
