package main

import (
	"context"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

// listenReusePort opens a TCP listener with SO_REUSEPORT set, so a second
// process can bind the same address before the first one stops listening.
// That's what makes a zero-downtime restart possible: start the new
// process, let the kernel start handing it new connections, then shut the
// old one down - no gap where the port is unbound and connections get
// refused.
//
// This is Linux/BSD-specific (SO_REUSEPORT isn't a thing on Windows); the
// build is already Linux-only per the README, so that's not a new
// constraint.
func listenReusePort(ctx context.Context, network, addr string) (net.Listener, error) {
	lc := net.ListenConfig{
		Control: func(_, _ string, c syscall.RawConn) error {
			var sockErr error
			if err := c.Control(func(fd uintptr) {
				sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
			}); err != nil {
				return err
			}
			return sockErr
		},
	}

	return lc.Listen(ctx, network, addr)
}
