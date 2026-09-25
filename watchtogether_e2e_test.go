package main

import (
	"fmt"
	"math"
	"os/exec"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	tc "github.com/sunnygitgud/sakuhaku/torrentclient"
	"github.com/sunnygitgud/sakuhaku/watchparty"
)

// app is a model with a tiny Bubble Tea runtime: commands run in goroutines
// and their messages are fed back through Update on one goroutine
type app struct {
	m    *model
	msgs chan tea.Msg

	mu    sync.Mutex
	procs []*exec.Cmd // every player started, killed when the test ends

	debug func() string // extra context for timeout failures
}

// handle runs one message through Update, remembering players it started
func (a *app) handle(msg tea.Msg) {
	if started, ok := msg.(playerStartedMsg); ok && started.launch != nil && started.launch.proc != nil {
		a.mu.Lock()
		a.procs = append(a.procs, started.launch.proc)
		a.mu.Unlock()
	}
	if _, ok := msg.(tc.TorrentProgressMsg); ok {
		a.m.update(msg) // no endless ticking in tests
		return
	}
	_, cmd := a.m.Update(msg)
	a.run(cmd)
}

func (a *app) killPlayers() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range a.procs {
		p.Process.Kill()
	}
}

func (a *app) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				a.run(c)
			}
			return
		}
		if msg != nil {
			a.msgs <- msg
		}
	}()
}

// pump processes messages until cond holds or timeout
func (a *app) pump(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.After(d)
	for !cond() {
		select {
		case msg := <-a.msgs:
			a.handle(msg)
		case <-deadline:
			extra := ""
			if a.debug != nil {
				extra = a.debug()
			}
			t.Fatalf("timed out waiting for %s (status %q) %s", what, a.m.statusMsg, extra)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// serve processes messages until stop is closed
func (a *app) serve(stop chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case msg := <-a.msgs:
			a.handle(msg)
		}
	}
}

func newApp(t *testing.T, name, dl string) *app {
	c := tc.NewTorrentClient("party-"+name, tc.InternalStreamPort)
	c.SetDownloadDir(dl)
	c.DisableIPV6 = true
	if err := c.Init(); err != nil {
		t.Fatal(err)
	}
	m := &model{ready: true, torrentClient: c, torrentCtx: map[string]*streamContext{}, selectedTorrents: map[int]struct{}{}}
	m.viewport = viewport.New(120, 40)
	m.mode = ModeTorrents
	a := &app{m: m, msgs: make(chan tea.Msg, 100)}
	t.Cleanup(func() { a.killPlayers(); c.Close() })
	return a
}

// TestWatchTogetherE2E runs two SakuHaku instances in one process, each with
// its own torrent client and a real (headless) mpv. The host streams an
// episode and opens a room; the guest joins from the link, fetches the
// episode from the host as a peer, and both stay in sync through pause and
// seek. Needs mpv and ffmpeg; skipped otherwise.
func TestWatchTogetherE2E(t *testing.T) {
	mpv, err := exec.LookPath("mpv")
	if err != nil {
		t.Skip("mpv not installed")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	if testing.Short() {
		t.Skip("slow")
	}

	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	// Headless mpv: MPV_HOME makes it read only this config
	mpvHome := filepath.Join(tmp, "mpv")
	os.MkdirAll(mpvHome, 0o755)
	os.WriteFile(filepath.Join(mpvHome, "mpv.conf"), []byte("vo=null\nao=null\n"), 0o644)
	t.Setenv("MPV_HOME", mpvHome)

	saved := cfg
	defer func() { cfg = saved }()
	cfg.Player = mpv
	cfg.RoomPort = "0"
	cfg.RoomAddr = ""

	video := filepath.Join(tmp, "episode.mkv")
	gen := exec.Command(ffmpeg, "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=90:size=320x240:rate=24",
		"-f", "lavfi", "-i", "sine=duration=90",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", video)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg can't make a test video: %v %s", err, out)
	}

	// Host has the episode on disk
	hostDL, guestDL := filepath.Join(tmp, "host"), filepath.Join(tmp, "guest")
	os.MkdirAll(filepath.Join(hostDL, "Show"), 0o755)
	os.MkdirAll(guestDL, 0o755)
	data, err := os.ReadFile(video)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(hostDL, "Show", "[Test] Show - 07 (1080p).mkv"), data, 0o644)
	info := metainfo.Info{PieceLength: 64 << 10}
	info.BuildFromFilePath(filepath.Join(hostDL, "Show"))
	mi := metainfo.MetaInfo{}
	mi.InfoBytes, _ = bencode.Marshal(info)
	tf := filepath.Join(tmp, "show.torrent")
	f, _ := os.Create(tf)
	mi.Write(f)
	f.Close()

	host := newApp(t, "host", hostDL)
	guest := newApp(t, "guest", guestDL)

	// Host starts streaming episode 7
	cfg.Name = "host"
	msg := host.m.torrentClient.AddAsyncTagged(tf, tc.ModeStream, &streamContext{})().(tc.TorrentAddedMsg)
	if msg.Error != nil {
		t.Fatal(msg.Error)
	}
	msg.Torrent.VerifyData()
	_, cmd := host.m.Update(msg)
	host.run(cmd)
	host.pump(t, 10*time.Second, "host player", func() bool { return host.m.playback != nil && host.m.playback.Running })

	// Host opens a room
	host.run(host.m.hostRoom())
	host.pump(t, 10*time.Second, "room", func() bool { return host.m.room != nil })
	link := host.m.roomStatus.Link
	t.Log("link:", link)
	inv, err := watchparty.ParseInvite(link)
	if err != nil || inv.Peer == "" || inv.File == "" || inv.Episode != 7 {
		t.Fatalf("invite %+v %v", inv, err)
	}

	// Guest joins with the link, like pressing J and pasting it
	cfg.Name = "guest"
	guest.run(guest.m.startJoin(link))
	// Keep the host's event loop running while the test drives the guest
	stop := make(chan struct{})
	defer close(stop)
	go host.serve(stop)
	guest.pump(t, 30*time.Second, "guest in the room", func() bool {
		return guest.m.room != nil && guest.m.playback != nil && guest.m.playback.Running
	})
	if guest.m.playback.FilePath != inv.File || guest.m.playback.Episode != 7 {
		t.Fatalf("guest plays %q ep %d", guest.m.playback.FilePath, guest.m.playback.Episode)
	}

	hp, gp := host.m.roomPlayer, guest.m.roomPlayer
	state := func(p watchparty.Player) watchparty.PlayerState { s, _ := p.State(); return s }
	inSync := func() bool {
		h, g := state(hp), state(gp)
		return h.Paused == g.Paused && math.Abs(h.Pos-g.Pos) < 0.3 && h.Pos > 0.5
	}
	guest.debug = func() string {
		return fmt.Sprintf("host %+v guest %+v room %+v", state(hp), state(gp), guest.m.roomStatus)
	}
	guest.pump(t, 20*time.Second, "guest to sync up", inSync)
	t.Logf("synced: host %+v guest %+v", state(hp), state(gp))

	// Host pauses in mpv (as if pressing space)
	hp.SetPaused(true)
	guest.pump(t, 5*time.Second, "guest to pause", func() bool { return state(gp).Paused && inSync() })

	// Guest seeks to 60s and resumes
	gp.Seek(60)
	guest.pump(t, 5*time.Second, "host to follow the seek", func() bool { return math.Abs(state(hp).Pos-60) < 1 })
	gp.SetPaused(false)
	guest.pump(t, 5*time.Second, "host to resume", func() bool { return !state(hp).Paused })
	guest.pump(t, 10*time.Second, "sync after resume", inSync)
	t.Logf("after seek+resume: host %+v guest %+v drift %.3f members %v", state(hp), state(gp),
		guest.m.roomStatus.Drift, guest.m.roomStatus.Members)

	for _, a := range []*app{host, guest} {
		a.m.refreshDownloads()
		out := a.m.renderContent()
		for i, l := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(l); w > 120 {
				t.Errorf("line %d is %d wide: %q", i, w, l)
			}
		}
	}
	if got := len(host.m.roomStatus.Members); got != 2 {
		t.Errorf("host sees %d members", got)
	}
	host.m.room.Close()
	guest.m.room.Close()
}
