// Package watchparty keeps several mpv players in sync: a host shares a room
// link, guests join it, and play/pause/seek from anyone applies to everyone.
package watchparty

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/sunnygitgud/sakuhaku/ipc"
)

// PlayerState is a snapshot of a player
type PlayerState struct {
	Paused    bool
	Pos       float64 // seconds
	Buffering bool    // stalled waiting for data (mpv's paused-for-cache)
	Speed     float64
}

// EventKind is a user-visible change in a player
type EventKind int

const (
	EventPause EventKind = iota
	EventPlay
	EventSeek
	// EventSeeking is sent when a seek starts, before it lands (EventSeek).
	// A guest holds off corrections meanwhile so it doesn't undo the seek.
	EventSeeking
)

// Event is emitted for every pause/play/seek, including ones we caused
// ourselves; the sync logic filters those out
type Event struct {
	Kind EventKind
}

// Player is what the room needs from a video player. MPV implements it; tests
// use a simulated player.
type Player interface {
	State() (PlayerState, error)
	SetPaused(paused bool) error
	Seek(pos float64) error
	SetSpeed(speed float64) error
	ShowText(text string)
	Events() <-chan Event
	Close() error
}

// MPV controls an mpv instance through its JSON IPC (--input-ipc-server)
type MPV struct {
	conn   net.Conn
	events chan Event

	mu      sync.Mutex
	nextID  int
	pending map[int]chan mpvReply
	closed  bool
	writeMu sync.Mutex
}

type mpvReply struct {
	Data  json.RawMessage `json:"data"`
	Error string          `json:"error"`
}

type mpvMessage struct {
	RequestID int             `json:"request_id"`
	Error     string          `json:"error"`
	Data      json.RawMessage `json:"data"`
	Event     string          `json:"event"`
	Name      string          `json:"name"`
}

// DialMPV connects to mpv's IPC endpoint, waiting up to timeout for mpv to
// create it (it only exists once mpv has started)
func DialMPV(path string, timeout time.Duration) (*MPV, error) {
	deadline := time.Now().Add(timeout)
	var conn net.Conn
	var err error
	for {
		conn, err = ipc.Dial(path)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("connecting to mpv: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	m := &MPV{conn: conn, events: make(chan Event, 32), pending: map[int]chan mpvReply{}}
	go m.readLoop()
	if _, err := m.command("observe_property", 1, "pause"); err != nil {
		conn.Close()
		return nil, err
	}
	return m, nil
}

func (m *MPV) readLoop() {
	scanner := bufio.NewScanner(m.conn)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	seeking := false
	// observe_property reports the current value straight away; that's the
	// baseline, not a change (a guest starts paused, which isn't the user
	// pressing pause). Only real changes become events.
	var lastPaused *bool
	for scanner.Scan() {
		var msg mpvMessage
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			continue
		}
		switch {
		case msg.Event == "":
			m.mu.Lock()
			ch := m.pending[msg.RequestID]
			delete(m.pending, msg.RequestID)
			m.mu.Unlock()
			if ch != nil {
				ch <- mpvReply{Data: msg.Data, Error: msg.Error}
			}
		case msg.Event == "property-change" && msg.Name == "pause":
			var paused bool
			if json.Unmarshal(msg.Data, &paused) != nil {
				continue
			}
			changed := lastPaused != nil && *lastPaused != paused
			lastPaused = &paused
			if !changed {
				continue
			}
			if paused {
				m.emit(Event{Kind: EventPause})
			} else {
				m.emit(Event{Kind: EventPlay})
			}
		case msg.Event == "seek":
			seeking = true
			m.emit(Event{Kind: EventSeeking})
		case msg.Event == "playback-restart" && seeking:
			// The seek has landed; time-pos is now the new position
			seeking = false
			m.emit(Event{Kind: EventSeek})
		}
	}

	m.mu.Lock()
	m.closed = true
	for id, ch := range m.pending {
		ch <- mpvReply{Error: "connection closed"}
		delete(m.pending, id)
	}
	m.mu.Unlock()
	close(m.events)
}

func (m *MPV) emit(e Event) {
	select {
	case m.events <- e:
	default: // nobody listening fast enough; drop rather than block mpv
	}
}

func (m *MPV) command(args ...any) (json.RawMessage, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, fmt.Errorf("mpv: connection closed")
	}
	m.nextID++
	id := m.nextID
	ch := make(chan mpvReply, 1)
	m.pending[id] = ch
	m.mu.Unlock()

	data, _ := json.Marshal(map[string]any{"command": args, "request_id": id})
	m.writeMu.Lock()
	_, err := m.conn.Write(append(data, '\n'))
	m.writeMu.Unlock()
	if err != nil {
		return nil, err
	}

	select {
	case reply := <-ch:
		if reply.Error != "success" {
			return nil, fmt.Errorf("mpv: %v: %s", args, reply.Error)
		}
		return reply.Data, nil
	case <-time.After(3 * time.Second):
		m.mu.Lock()
		delete(m.pending, id)
		m.mu.Unlock()
		return nil, fmt.Errorf("mpv: %v: timed out", args)
	}
}

func (m *MPV) getFloat(name string) (float64, error) {
	data, err := m.command("get_property", name)
	if err != nil {
		return 0, err
	}
	var v float64
	err = json.Unmarshal(data, &v)
	return v, err
}

func (m *MPV) getBool(name string) (bool, error) {
	data, err := m.command("get_property", name)
	if err != nil {
		return false, err
	}
	var v bool
	err = json.Unmarshal(data, &v)
	return v, err
}

// State reads the current playback state
func (m *MPV) State() (PlayerState, error) {
	var st PlayerState
	var err error
	if st.Paused, err = m.getBool("pause"); err != nil {
		return st, err
	}
	// time-pos is unavailable while nothing is loaded yet
	st.Pos, _ = m.getFloat("time-pos")
	st.Buffering, _ = m.getBool("paused-for-cache")
	if st.Speed, err = m.getFloat("speed"); err != nil {
		st.Speed = 1
	}
	return st, nil
}

func (m *MPV) SetPaused(paused bool) error {
	_, err := m.command("set_property", "pause", paused)
	return err
}

func (m *MPV) Seek(pos float64) error {
	_, err := m.command("seek", pos, "absolute+exact")
	return err
}

func (m *MPV) SetSpeed(speed float64) error {
	_, err := m.command("set_property", "speed", speed)
	return err
}

// ShowText shows a short on-screen message in mpv
func (m *MPV) ShowText(text string) {
	go m.command("show-text", text, 3000)
}

func (m *MPV) Events() <-chan Event { return m.events }

func (m *MPV) Close() error { return m.conn.Close() }
