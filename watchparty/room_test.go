package watchparty

import (
	"math"
	"sync"
	"testing"
	"time"
)

// simPlayer is a player driven by the real clock. rate simulates a machine
// whose playback runs slightly fast or slow (clock skew, dropped frames).
type simPlayer struct {
	mu        sync.Mutex
	paused    bool
	pos       float64
	at        time.Time
	speed     float64
	rate      float64
	buffering bool
	events    chan Event
	texts     []string
}

func newSim(pos float64, paused bool, rate float64) *simPlayer {
	return &simPlayer{pos: pos, paused: paused, at: time.Now(), speed: 1, rate: rate, events: make(chan Event, 64)}
}

// advance moves pos forward to now (mu held)
func (p *simPlayer) advance() {
	now := time.Now()
	if !p.paused && !p.buffering {
		p.pos += now.Sub(p.at).Seconds() * p.speed * p.rate
	}
	p.at = now
}

func (p *simPlayer) State() (PlayerState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.advance()
	return PlayerState{Paused: p.paused, Pos: p.pos, Buffering: p.buffering, Speed: p.speed}, nil
}

func (p *simPlayer) SetPaused(paused bool) error {
	p.mu.Lock()
	p.advance()
	changed := p.paused != paused
	p.paused = paused
	p.mu.Unlock()
	if changed {
		if paused {
			p.events <- Event{Kind: EventPause}
		} else {
			p.events <- Event{Kind: EventPlay}
		}
	}
	return nil
}

// Seek behaves like mpv: the position reports the target straight away, then
// "seeking" and "landed" events follow
func (p *simPlayer) Seek(pos float64) error {
	p.mu.Lock()
	p.advance()
	p.pos = pos
	p.mu.Unlock()
	p.events <- Event{Kind: EventSeeking}
	p.events <- Event{Kind: EventSeek}
	return nil
}

func (p *simPlayer) SetSpeed(s float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.advance()
	p.speed = s
	return nil
}

func (p *simPlayer) setBuffering(b bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.advance()
	p.buffering = b
}

func (p *simPlayer) ShowText(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.texts = append(p.texts, text)
}

func (p *simPlayer) Events() <-chan Event { return p.events }
func (p *simPlayer) Close() error         { return nil }

func startRoom(t *testing.T, host, guest *simPlayer) (*Room, *Room) {
	t.Helper()
	h, err := Host(host, "127.0.0.1:0", "127.0.0.1", "host", Invite{Source: "magnet:?xt=urn:btih:abc", Episode: 5})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := ParseInvite(h.Status().Link)
	if err != nil {
		t.Fatal(err)
	}
	g, err := Join(guest, inv, "guest")
	if err != nil {
		h.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { g.Close(); h.Close() })
	return h, g
}

func pos(p Player) float64 {
	st, _ := p.State()
	return st.Pos
}

func paused(p Player) bool {
	st, _ := p.State()
	return st.Paused
}

// eventually polls cond for up to d
func eventually(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestGuestCatchesUpOnJoin(t *testing.T) {
	host := newSim(600, false, 1)
	guest := newSim(0, true, 1) // guests start paused at the beginning
	_, g := startRoom(t, host, guest)

	eventually(t, 3*time.Second, "guest to jump to the host", func() bool {
		return !paused(guest) && math.Abs(pos(guest)-pos(host)) < 0.5
	})
	if members := g.Status().Members; len(members) != 2 || members[0] != "host" || members[1] != "guest" {
		t.Errorf("members = %v", members)
	}
}

func TestPausePlayAndSeekPropagate(t *testing.T) {
	host := newSim(100, false, 1)
	guest := newSim(0, true, 1)
	startRoom(t, host, guest)
	eventually(t, 3*time.Second, "initial sync", func() bool { return !paused(guest) })

	// Host pauses: guest pauses at the same spot
	host.SetPaused(true)
	eventually(t, 2*time.Second, "guest to pause", func() bool {
		return paused(guest) && math.Abs(pos(guest)-pos(host)) < 0.5
	})

	// The guest is told who paused
	eventually(t, 2*time.Second, "guest to see a note", func() bool { return hasText(guest, "host paused") })

	// Guest resumes: host resumes and shows who did it
	guest.SetPaused(false)
	eventually(t, 2*time.Second, "host to resume", func() bool { return !paused(host) })
	eventually(t, 2*time.Second, "host to see a note", func() bool { return hasText(host, "guest resumed") })

	// Guest seeks: host follows
	guest.Seek(900)
	eventually(t, 2*time.Second, "host to seek", func() bool { h := pos(host); return h >= 900 && h < 903 })

	// Host seeks: guest follows
	host.Seek(1200)
	eventually(t, 3*time.Second, "guest to seek", func() bool {
		return pos(host) >= 1200 && math.Abs(pos(guest)-pos(host)) < 0.3
	})

	// Guest pauses: host pauses and neither snaps back
	guest.SetPaused(true)
	eventually(t, 2*time.Second, "host to pause", func() bool { return paused(host) })
	time.Sleep(1500 * time.Millisecond)
	if !paused(guest) || !paused(host) {
		t.Fatal("pause didn't stick")
	}
}

func TestDriftIsCorrectedSmoothly(t *testing.T) {
	host := newSim(50, false, 1)
	guest := newSim(50, false, 1.04) // guest runs 4% fast
	_, g := startRoom(t, host, guest)

	// 4% is far worse than real clock skew; uncorrected the guest would be
	// 0.3s ahead after 8s and keep going. The speed nudge should hold it close
	// without any hard seeks after the first sync.
	time.Sleep(1500 * time.Millisecond)
	start := time.Now()
	worst := 0.0
	for time.Since(start) < 6*time.Second {
		worst = math.Max(worst, math.Abs(pos(guest)-pos(host)))
		time.Sleep(100 * time.Millisecond)
	}
	if worst > 0.2 {
		t.Fatalf("drift reached %.2fs", worst)
	}
	guest.mu.Lock()
	speed := guest.speed
	guest.mu.Unlock()
	if speed >= 1 {
		t.Errorf("a fast guest should be slowed down, speed = %v", speed)
	}
	t.Logf("worst drift %.3fs, speed %.3f, reported drift %.3f", worst, speed, g.Status().Drift)
}

func TestHostHoldsForBufferingGuest(t *testing.T) {
	host := newSim(10, false, 1)
	guest := newSim(10, false, 1)
	h, _ := startRoom(t, host, guest)
	time.Sleep(time.Second)

	guest.setBuffering(true)
	eventually(t, 4*time.Second, "host to hold", func() bool { return paused(host) && h.Status().Hold != "" })
	if h.Status().Hold != "waiting for guest" {
		t.Errorf("hold = %q", h.Status().Hold)
	}

	guest.setBuffering(false)
	eventually(t, 6*time.Second, "host to resume", func() bool { return !paused(host) && h.Status().Hold == "" })
}

func TestWrongTokenIsRejected(t *testing.T) {
	h, err := Host(newSim(0, true, 1), "127.0.0.1:0", "127.0.0.1", "host", Invite{Source: "magnet:?x"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	inv, _ := ParseInvite(h.Status().Link)
	inv.Token = "nope"
	if _, err := Join(newSim(0, true, 1), inv, "mallory"); err == nil {
		t.Fatal("expected the room to refuse a wrong token")
	}
}

func TestGuestNoticesHostLeaving(t *testing.T) {
	host := newSim(10, false, 1)
	h, g := startRoom(t, host, newSim(0, true, 1))
	h.Close()
	eventually(t, 3*time.Second, "guest to notice", func() bool { return g.Status().Closed && g.Status().Err != nil })
}

func TestInviteLinkRoundTrip(t *testing.T) {
	in := Invite{Host: "192.168.1.5:47800", Token: "t0k", Source: "magnet:?xt=urn:btih:ABC&dn=Show+-+05&tr=udp://x:1",
		File: "Show/[Group] Show - 05 (1080p).mkv", Peer: "192.168.1.5:42069", AnimeID: 42, Episode: 5, Title: "Show & Co"}
	out, err := ParseInvite(in.Link())
	if err != nil || out != in {
		t.Fatalf("round trip: %+v %v", out, err)
	}
	for _, bad := range []string{"https://example.org", "sakuhaku://watch?token=x&src=y", "sakuhaku://watch?host=a:1&src=y"} {
		if _, err := ParseInvite(bad); err == nil {
			t.Errorf("ParseInvite(%q) should fail", bad)
		}
	}
}

func hasText(p *simPlayer, text string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range p.texts {
		if t == text {
			return true
		}
	}
	return false
}
