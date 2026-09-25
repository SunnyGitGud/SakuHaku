//go:build windows

package ipc

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

func dial(path string) (net.Conn, error) {
	timeout := DialTimeout
	return winio.DialPipe(path, (*time.Duration)(&timeout))
}

func listen(path string) (net.Listener, error) {
	return winio.ListenPipe(path, nil)
}
