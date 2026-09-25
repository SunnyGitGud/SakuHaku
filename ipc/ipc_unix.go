//go:build !windows

package ipc

import (
	"net"
	"os"
)

func dial(path string) (net.Conn, error) {
	return net.DialTimeout("unix", path, DialTimeout)
}

func listen(path string) (net.Listener, error) {
	os.Remove(path)
	return net.Listen("unix", path)
}
