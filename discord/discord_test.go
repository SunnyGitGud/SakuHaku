package discord

import (
	"encoding/json"
	"net"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sunnygitgud/sakuhaku/ipc"
)

// fakeDiscord accepts one connection at a time and records the activities it
// receives. rejectRich makes it fail activities that use type or buttons, like
// older Discord builds.
type fakeDiscord struct {
	activities chan map[string]any
	handshakes chan map[string]any
}

func startFake(t *testing.T, rejectRich bool) *fakeDiscord {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets")
	}
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	l, err := ipc.Listen(filepath.Join(dir, "discord-ipc-0"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })

	f := &fakeDiscord{activities: make(chan map[string]any, 10), handshakes: make(chan map[string]any, 10)}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go f.serve(conn, rejectRich)
		}
	}()
	return f
}

func (f *fakeDiscord) serve(conn net.Conn, rejectRich bool) {
	defer conn.Close()
	op, data, err := readFrame(conn)
	if err != nil || op != opHandshake {
		return
	}
	var hs map[string]any
	json.Unmarshal(data, &hs)
	f.handshakes <- hs
	writeFrame(conn, opFrame, map[string]any{"cmd": "DISPATCH", "evt": "READY", "data": map[string]any{"v": 1}})

	for {
		op, data, err := readFrame(conn)
		if err != nil || op == opClose {
			return
		}
		var cmd struct {
			Cmd   string `json:"cmd"`
			Nonce string `json:"nonce"`
			Args  struct {
				Activity map[string]any `json:"activity"`
			} `json:"args"`
		}
		json.Unmarshal(data, &cmd)
		a := cmd.Args.Activity
		if rejectRich && a != nil && (a["type"] != nil || a["buttons"] != nil) {
			writeFrame(conn, opFrame, map[string]any{"cmd": cmd.Cmd, "evt": "ERROR", "nonce": cmd.Nonce,
				"data": map[string]any{"code": 4000, "message": "child \"activity\" fails"}})
			continue
		}
		f.activities <- a
		writeFrame(conn, opFrame, map[string]any{"cmd": cmd.Cmd, "nonce": cmd.Nonce, "data": a})
	}
}

func TestSetActivity(t *testing.T) {
	f := startFake(t, false)
	c := New("123456")

	err := c.SetActivity(&Activity{
		Type:       ActivityWatching,
		Details:    "Frieren",
		State:      "Episode 5 of 28",
		Timestamps: &Timestamps{Start: 1000, End: 2000},
		Buttons:    []Button{{Label: "AniList", URL: "https://anilist.co/anime/154587"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hs := <-f.handshakes; hs["client_id"] != "123456" || hs["v"] != float64(1) {
		t.Fatalf("handshake %v", hs)
	}
	a := <-f.activities
	if a["details"] != "Frieren" || a["type"] != float64(ActivityWatching) || a["timestamps"].(map[string]any)["end"] != float64(2000) {
		t.Fatalf("activity %v", a)
	}

	// Clearing sends a null activity
	if err := c.SetActivity(nil); err != nil {
		t.Fatal(err)
	}
	if a := <-f.activities; a != nil {
		t.Fatalf("expected nil activity, got %v", a)
	}
	c.Close()
}

func TestFallsBackForOldDiscord(t *testing.T) {
	f := startFake(t, true)
	c := New("1")
	if err := c.SetActivity(&Activity{Type: ActivityWatching, Details: "x", Buttons: []Button{{"a", "https://x"}}}); err != nil {
		t.Fatal(err)
	}
	a := <-f.activities
	if a["type"] != nil || a["buttons"] != nil || a["details"] != "x" {
		t.Fatalf("activity %v", a)
	}
}

func TestReconnectsAfterDiscordRestart(t *testing.T) {
	f := startFake(t, false)
	c := New("1")
	if err := c.SetActivity(&Activity{Details: "one"}); err != nil {
		t.Fatal(err)
	}
	<-f.activities
	c.conn.Close() // simulate Discord going away; the client doesn't know yet

	if err := c.SetActivity(&Activity{Details: "two"}); err != nil {
		t.Fatal(err)
	}
	if a := <-f.activities; a["details"] != "two" {
		t.Fatalf("activity %v", a)
	}
}

func TestNoDiscordRunning(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	if err := New("1").SetActivity(&Activity{Details: "x"}); err == nil {
		t.Fatal("expected an error without Discord")
	}
}
