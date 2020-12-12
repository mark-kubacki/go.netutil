package netutil

import (
	"net"
	"os"
	"sync"
)

// AcceptedConnection wraps the connection as net.Listener.
//
// This is meant for socekt activated services that get run for every
// incoming connection the caller (like systemd) has already accepted.
//
// Unless the http.Server is meant to handle more than one request per
// connection, remember to have http.Server close it after the first,
// by setting this HTTP Header:
//
//     w.Header().Set("Connection", "close")
//
// Passed to a http.Server, the latter will also return os.ErrClosed
// signalling its natural end (shutdown). Check for this wherever you
// expect http.ErrServerClosed to avoid that "false" error.
func AcceptedConnection(connection *os.File) (net.Listener, error) {
	// net.FileListener will provide method 'Addr'.
	pc, err := net.FileListener(connection)
	if err != nil {
		return nil, err
	}
	return &acceptedConnection{
		Listener: pc,
		file:     connection,
	}, nil
}

// acceptedConnection implements net.Listener.
type acceptedConnection struct {
	// Both are backed by the same file descriptor.
	net.Listener
	file *os.File

	mu       sync.Mutex
	doneChan <-chan struct{}
	permErr  error
}

// Accept implements net.Listener.
// Only the first call will deliver, all subsequent will block
// until it is closed.
func (c *acceptedConnection) Accept() (net.Conn, error) {
	// The FileConn is gotten here for its error "fcntl: too many open files"
	// that can be used to back off.
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.permErr != nil {
		return nil, c.permErr
	}
	if c.doneChan != nil {
		return c.tailWaitUntilFirstIsDone()
	}

	conn, err := net.FileConn(c.file)
	if err != nil {
		c.permErr = err
		return nil, err
	}

	sharedBlockingChan := make(chan struct{})
	c.doneChan = sharedBlockingChan
	return &cascadingCloser{conn, sharedBlockingChan}, nil
}

func (c *acceptedConnection) tailWaitUntilFirstIsDone() (net.Conn, error) {
	<-c.doneChan
	return nil, os.ErrClosed
}

// Close implements net.Listener.
func (c *acceptedConnection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.permErr == nil {
		c.permErr = os.ErrClosed
	}
	return c.file.Close()
}

// cascadingCloser is used to unblock any receivers listening to the given channel.
type cascadingCloser struct {
	net.Conn

	closeChan chan struct{}
}

// Close implements the net.Conn interface.
func (c *cascadingCloser) Close() error {
	// Elaborate check because the underlying connection might've been closed sooner.
	select {
	case _, open := <-c.closeChan:
		if open {
			close(c.closeChan)
		}
	default:
		close(c.closeChan)
	}
	return c.Conn.Close()
}
