// Package ipc connects to local IPC endpoints: Unix domain sockets on Linux
// and macOS, named pipes on Windows. mpv's --input-ipc-server and Discord's
// RPC both use these.
package ipc

import (
	"net"
	"time"
)

// DialTimeout bounds how long connecting to an endpoint may take
const DialTimeout = 2 * time.Second

// Dial connects to a socket path (or \\.\pipe\name on Windows)
func Dial(path string) (net.Conn, error) {
	return dial(path)
}

// Listen creates an endpoint at path, used by tests to fake mpv/Discord
func Listen(path string) (net.Listener, error) {
	return listen(path)
}
