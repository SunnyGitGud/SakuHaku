// Package discord sets Discord Rich Presence through the Discord desktop
// client's local RPC socket (no network access or bot token needed).
//
// Protocol: every frame is a little-endian uint32 opcode, a uint32 payload
// length and a JSON payload. The client sends a handshake (opcode 0) with the
// application's client ID, waits for the READY dispatch and then sends
// SET_ACTIVITY commands (opcode 1).
package discord

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/sunnygitgud/sakuhaku/ipc"
)

const (
	opHandshake = 0
	opFrame     = 1
	opClose     = 2

	// ActivityWatching shows "Watching <app name>" instead of "Playing"
	ActivityWatching = 3
)

// Activity is what Discord shows on the user's profile
type Activity struct {
	Type       int         `json:"type,omitempty"`
	Details    string      `json:"details,omitempty"` // first line
	State      string      `json:"state,omitempty"`   // second line
	Timestamps *Timestamps `json:"timestamps,omitempty"`
	Assets     *Assets     `json:"assets,omitempty"`
	Buttons    []Button    `json:"buttons,omitempty"`
}

// Timestamps in unix milliseconds. With both set Discord shows a progress bar.
type Timestamps struct {
	Start int64 `json:"start,omitempty"`
	End   int64 `json:"end,omitempty"`
}

// Assets can be art uploaded to the Discord application or https:// URLs
type Assets struct {
	LargeImage string `json:"large_image,omitempty"`
	LargeText  string `json:"large_text,omitempty"`
	SmallImage string `json:"small_image,omitempty"`
	SmallText  string `json:"small_text,omitempty"`
}

// Button is a link shown under the activity (max 2)
type Button struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Client is a lazily connected RPC client. It is safe for concurrent use and
// reconnects when Discord is restarted.
type Client struct {
	clientID string

	mu    sync.Mutex
	conn  net.Conn
	nonce int
	// Older Discord builds reject activity types and buttons; remember so we
	// only find out once
	simple bool
}

// New returns a client for a Discord application ID
func New(clientID string) *Client {
	return &Client{clientID: clientID}
}

// SetActivity replaces the presence; nil clears it
func (c *Client) SetActivity(a *Activity) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// One retry covers a stale connection after Discord restarted
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		if c.conn == nil {
			if err = c.connect(); err != nil {
				return err
			}
		}
		err = c.setActivity(a)
		if err == nil {
			return nil
		}
		var rpcErr *RPCError
		if errors.As(err, &rpcErr) {
			if !c.simple && a != nil && (a.Type != 0 || len(a.Buttons) > 0) {
				c.simple = true
				continue
			}
			return err
		}
		c.closeLocked()
	}
	return err
}

// Close clears the presence and disconnects
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.setActivity(nil)
		writeFrame(c.conn, opClose, map[string]any{})
		c.closeLocked()
	}
}

func (c *Client) closeLocked() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

// RPCError is an error reported by Discord for a command
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("discord: %s (%d)", e.Message, e.Code) }

type response struct {
	Cmd  string          `json:"cmd"`
	Evt  string          `json:"evt"`
	Data json.RawMessage `json:"data"`
}

func (c *Client) connect() error {
	var lastErr error = errors.New("discord: no running Discord client found")
	for _, path := range SocketPaths() {
		conn, err := ipc.Dial(path)
		if err != nil {
			continue
		}
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		if err := writeFrame(conn, opHandshake, map[string]any{"v": 1, "client_id": c.clientID}); err != nil {
			conn.Close()
			lastErr = err
			continue
		}
		resp, err := readResponse(conn)
		if err != nil {
			conn.Close()
			lastErr = err
			continue
		}
		if resp.Evt != "READY" {
			conn.Close()
			lastErr = fmt.Errorf("discord: handshake failed: %s", string(resp.Data))
			continue
		}
		conn.SetDeadline(time.Time{})
		c.conn = conn
		return nil
	}
	return lastErr
}

func (c *Client) setActivity(a *Activity) error {
	if a != nil && c.simple {
		simple := *a
		simple.Type, simple.Buttons = 0, nil
		a = &simple
	}
	c.nonce++
	payload := map[string]any{
		"cmd":   "SET_ACTIVITY",
		"args":  map[string]any{"pid": os.Getpid(), "activity": a},
		"nonce": strconv.Itoa(c.nonce),
	}
	c.conn.SetDeadline(time.Now().Add(5 * time.Second))
	defer c.conn.SetDeadline(time.Time{})
	if err := writeFrame(c.conn, opFrame, payload); err != nil {
		return err
	}
	resp, err := readResponse(c.conn)
	if err != nil {
		return err
	}
	if resp.Evt == "ERROR" {
		rpcErr := &RPCError{}
		json.Unmarshal(resp.Data, rpcErr)
		return rpcErr
	}
	return nil
}

func writeFrame(w io.Writer, op uint32, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	buf := make([]byte, 8+len(data))
	binary.LittleEndian.PutUint32(buf[0:4], op)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(data)))
	copy(buf[8:], data)
	_, err = w.Write(buf)
	return err
}

func readFrame(r io.Reader) (uint32, []byte, error) {
	var header [8]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	op := binary.LittleEndian.Uint32(header[0:4])
	n := binary.LittleEndian.Uint32(header[4:8])
	if n > 1<<20 {
		return 0, nil, fmt.Errorf("discord: frame too large (%d bytes)", n)
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return 0, nil, err
	}
	return op, data, nil
}

func readResponse(r io.Reader) (*response, error) {
	op, data, err := readFrame(r)
	if err != nil {
		return nil, err
	}
	if op == opClose {
		return nil, fmt.Errorf("discord: connection closed: %s", string(data))
	}
	resp := &response{}
	if err := json.Unmarshal(data, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// SocketPaths lists where the Discord client may be listening, including the
// Flatpak and Snap sandboxes on Linux
func SocketPaths() []string {
	var paths []string
	if runtime.GOOS == "windows" {
		for i := 0; i < 10; i++ {
			paths = append(paths, fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, i))
		}
		return paths
	}

	var dirs []string
	for _, env := range []string{"XDG_RUNTIME_DIR", "TMPDIR", "TMP", "TEMP"} {
		if d := os.Getenv(env); d != "" {
			dirs = append(dirs, d)
		}
	}
	dirs = append(dirs, "/tmp")

	seen := map[string]bool{}
	for _, d := range dirs {
		for _, sub := range []string{"", "app/com.discordapp.Discord", "snap.discord", ".flatpak/dev.vencord.Vesktop/xdg-run"} {
			for i := 0; i < 10; i++ {
				p := filepath.Join(d, sub, fmt.Sprintf("discord-ipc-%d", i))
				if !seen[p] {
					seen[p] = true
					paths = append(paths, p)
				}
			}
		}
	}
	return paths
}
