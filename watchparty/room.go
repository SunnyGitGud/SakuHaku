package watchparty

import (
	"bufio"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Tuning for the sync loop. Drift is how far a guest's position is from the
// host's (estimated) position right now.
const (
	syncInterval = 500 * time.Millisecond
	// Beyond this a guest seeks; below it the playback speed is nudged
	seekThreshold = 3.0
	// Start nudging speed above this drift, stop below speedStopDrift
	speedStartDrift = 0.15
	speedStopDrift  = 0.05
	// Speed changes by speedGain per second of drift, capped at maxNudge, so
	// catching up is smooth instead of a visible jump (mpv keeps the audio
	// pitch, so a few percent isn't noticeable)
	speedGain = 0.25
	maxNudge  = 0.08
	// How long our own pause changes are recognised as echoes
	echoWindow = 1500 * time.Millisecond
	// After a guest sends a control, it trusts itself until the host's state
	// reflects it, instead of snapping back to the old state
	controlGrace = 1200 * time.Millisecond
	// A guest's buffering report is ignored when older than this
	statusMaxAge = 5 * time.Second
	// A guest only corrects a deviation it didn't get from the host (likely
	// the user just paused or seeked) once it has lasted this long, giving the
	// user's action time to be reported and forwarded instead of undone
	confirmDelay = 400 * time.Millisecond
	// Longest we wait for a seek to land before correcting again (a seek on a
	// stream can take a while if the data isn't downloaded yet)
	seekLandTimeout = 10 * time.Second
	// A seek landing this close to where we sent it is our own
	seekEchoTolerance = 1.5
)

// Role says whether we host the room or joined it
type Role int

const (
	RoleHost Role = iota
	RoleGuest
)

// Status is a snapshot for the UI
type Status struct {
	Role    Role
	Link    string
	Members []string // everyone in the room, host first
	Drift   float64  // guest: seconds ahead (+) or behind (-) the host
	RTT     time.Duration
	Hold    string // why playback is held, e.g. "waiting for alice"
	Note    string // last thing that happened, e.g. "bob paused"
	Err     error
	Closed  bool
}

// Room is a running watch party, either side
type Room struct {
	role   Role
	player Player
	name   string

	updates chan Status
	done    chan struct{}
	once    sync.Once
	wg      sync.WaitGroup

	mu     sync.Mutex
	status Status
	echo   echoFilter

	// host side
	listener net.Listener
	token    string
	guests   map[*peer]bool
	hold     string
	calm     int // ticks without anyone buffering while holding

	// guest side
	conn       *peer
	hostState  Message
	haveState  bool
	stateAt    time.Time
	rtt        time.Duration
	speed      float64
	graceUntil time.Time
	syncMu     sync.Mutex // sync runs from the reader and the ticker
	deviating  time.Time  // when the guest was first seen out of step
	seekingTil time.Time  // a seek is in flight: don't correct until it lands
	userSeek   float64    // where the user's last seek went, and when
	userSeekAt time.Time
}

// peer is one connection with serialised writes
type peer struct {
	conn    net.Conn
	name    string
	writeMu sync.Mutex

	// host side view of a guest's player
	buffering bool
	pos       float64
	statusAt  time.Time
}

func (p *peer) send(m Message) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	p.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err = p.conn.Write(append(data, '\n'))
	return err
}

// echoFilter recognises player events caused by our own commands
type echoFilter struct {
	mu         sync.Mutex
	pause      *bool
	pauseUntil time.Time
	seekTarget float64
	seekAt     time.Time
}

func (e *echoFilter) expectPause(paused bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pause = &paused
	e.pauseUntil = time.Now().Add(echoWindow)
}

func (e *echoFilter) expectSeek(target float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seekTarget, e.seekAt = target, time.Now()
}

// isEcho reports (and consumes) an event we caused ourselves. Seeks are
// matched by where they landed, not just timing, so a user's seek right after
// one of ours isn't swallowed.
func (e *echoFilter) isEcho(ev Event, pos float64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	switch ev.Kind {
	case EventPause, EventPlay:
		if e.pause != nil && now.Before(e.pauseUntil) && *e.pause == (ev.Kind == EventPause) {
			e.pause = nil
			return true
		}
	case EventSeek, EventSeeking:
		// Not consumed: our own seek shows up twice (starting and landing),
		// and landing can take a while on a stream that's still downloading
		return !e.seekAt.IsZero() && now.Sub(e.seekAt) < seekLandTimeout &&
			math.Abs(pos-e.seekTarget) < seekEchoTolerance
	}
	return false
}

func newRoom(role Role, player Player, name string) *Room {
	return &Room{
		role:    role,
		player:  player,
		name:    name,
		updates: make(chan Status, 1),
		done:    make(chan struct{}),
		guests:  map[*peer]bool{},
		speed:   1,
		status:  Status{Role: role},
	}
}

// Updates delivers status changes; only the latest is kept if the reader is slow
func (r *Room) Updates() <-chan Status { return r.updates }

// Status returns the current status
func (r *Room) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

// Role reports whether we're hosting or a guest
func (r *Room) Role() Role { return r.role }

// publish updates the status and notifies the UI. update runs with r.mu held,
// so it must not call anything that locks r.mu (compute values first).
func (r *Room) publish(update func(*Status)) {
	r.mu.Lock()
	update(&r.status)
	s := r.status
	r.mu.Unlock()
	select {
	case <-r.updates: // drop a stale unread update
	default:
	}
	select {
	case r.updates <- s:
	default:
	}
}

// Close leaves or shuts down the room. The player is left running.
func (r *Room) Close() {
	r.once.Do(func() {
		close(r.done)
		if r.listener != nil {
			r.listener.Close()
		}
		r.mu.Lock()
		for g := range r.guests {
			g.conn.Close()
		}
		r.mu.Unlock()
		if r.conn != nil {
			r.conn.conn.Close()
		}
	})
	r.wg.Wait()
	if r.role == RoleGuest {
		r.syncMu.Lock()
		r.setSpeed(1)
		r.syncMu.Unlock()
	}
	r.publish(func(s *Status) { s.Closed = true })
}

func (r *Room) stopped() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

func (r *Room) every(d time.Duration, f func()) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		t := time.NewTicker(d)
		defer t.Stop()
		for {
			select {
			case <-r.done:
				return
			case <-t.C:
				f()
			}
		}
	}()
}

// handleEvent filters out our own echoes and passes the user's actions on.
// A user's seek is passed on as soon as it starts (mpv already reports the
// target position), not when it lands: landing can take seconds while the
// data downloads, and everyone else should jump right away.
func (r *Room) handleEvent(ev Event, handle func(Event)) {
	pos := 0.0
	if ev.Kind == EventSeek || ev.Kind == EventSeeking {
		if st, err := r.player.State(); err == nil {
			pos = st.Pos
		}
	}

	switch ev.Kind {
	case EventSeeking:
		r.seekStarted()
		if r.echo.isEcho(ev, pos) {
			return
		}
		r.mu.Lock()
		r.userSeek, r.userSeekAt = pos, time.Now()
		r.mu.Unlock()
		handle(Event{Kind: EventSeek})
	case EventSeek:
		r.seekLanded()
		r.mu.Lock()
		alreadySent := time.Since(r.userSeekAt) < seekLandTimeout && math.Abs(pos-r.userSeek) < seekEchoTolerance
		r.mu.Unlock()
		if alreadySent || r.echo.isEcho(ev, pos) {
			return
		}
		handle(ev) // a seek we never saw start
	default:
		if !r.echo.isEcho(ev, pos) {
			handle(ev)
		}
	}
}

func (r *Room) watchEvents(handle func(Event)) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		for {
			select {
			case <-r.done:
				return
			case ev, ok := <-r.player.Events():
				if !ok {
					return
				}
				r.handleEvent(ev, handle)
			}
		}
	}()
}

// Host -----------------------------------------------------------------------

// Host opens a room on listenAddr (e.g. ":0" for any free port). inv describes
// what's playing; its Host and Token are filled in. publicHost, when set, is
// the address guests should use instead of this machine's LAN IP (a port
// forwarded public IP, a Tailscale name, ...).
func Host(player Player, listenAddr, publicHost, name string, inv Invite) (*Room, error) {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("opening room: %w", err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)

	host := publicHost
	if host == "" {
		host = LocalIP()
	}
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, port)
	}
	inv.Host = host
	inv.Token = newToken()

	r := newRoom(RoleHost, player, name)
	r.listener = ln
	r.token = inv.Token
	r.status.Link = inv.Link()
	r.status.Members = []string{name}

	r.wg.Add(1)
	go r.acceptLoop()
	r.watchEvents(r.hostEvent)
	r.every(time.Second, r.hostTick)
	r.publish(func(*Status) {})
	return r, nil
}

func (r *Room) acceptLoop() {
	defer r.wg.Done()
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			return
		}
		r.wg.Add(1)
		go r.serveGuest(conn)
	}
}

func (r *Room) serveGuest(conn net.Conn) {
	defer r.wg.Done()
	defer conn.Close()
	p := &peer{conn: conn}
	scanner := bufio.NewScanner(conn)

	// Handshake
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var hello Message
	if !scanner.Scan() || json.Unmarshal(scanner.Bytes(), &hello) != nil || hello.Type != msgHello {
		return
	}
	if subtle.ConstantTimeCompare([]byte(hello.Token), []byte(r.token)) != 1 {
		p.send(Message{Type: msgError, Text: "wrong room token"})
		return
	}
	conn.SetReadDeadline(time.Time{})

	r.mu.Lock()
	p.name = r.uniqueName(hello.Name)
	r.guests[p] = true
	r.mu.Unlock()

	members := r.members()
	r.publish(func(s *Status) { s.Members = members; s.Note = p.name + " joined" })
	r.player.ShowText(p.name + " joined")
	p.send(Message{Type: msgWelcome, Name: p.name, Members: r.members()})
	r.broadcastState()

	for scanner.Scan() {
		var m Message
		if json.Unmarshal(scanner.Bytes(), &m) != nil {
			continue
		}
		switch m.Type {
		case msgPing:
			p.send(Message{Type: msgPong, Stamp: m.Stamp})
		case msgStatus:
			r.mu.Lock()
			p.buffering, p.pos, p.statusAt = m.Buffering, m.Pos, time.Now()
			r.mu.Unlock()
		case msgControl:
			r.applyControl(p, m)
		}
	}

	r.mu.Lock()
	delete(r.guests, p)
	r.mu.Unlock()
	if !r.stopped() {
		members := r.members()
		r.publish(func(s *Status) { s.Members = members; s.Note = p.name + " left" })
		r.player.ShowText(p.name + " left")
	}
}

// uniqueName avoids two members with the same name (r.mu held)
func (r *Room) uniqueName(name string) string {
	if name == "" {
		name = "guest"
	}
	taken := map[string]bool{r.name: true}
	for g := range r.guests {
		taken[g.name] = true
	}
	candidate := name
	for i := 2; taken[candidate]; i++ {
		candidate = fmt.Sprintf("%s-%d", name, i)
	}
	return candidate
}

func (r *Room) members() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.membersLocked()
}

func (r *Room) membersLocked() []string {
	names := make([]string, 0, len(r.guests))
	for g := range r.guests {
		names = append(names, g.name)
	}
	sort.Strings(names)
	return append([]string{r.name}, names...)
}

// applyControl carries out a guest's pause/play/seek on the host's player,
// which then propagates to everyone through the state broadcast
func (r *Room) applyControl(from *peer, m Message) {
	who := from.name
	switch m.Action {
	case actionPause:
		r.mu.Lock()
		r.hold = "" // an explicit pause replaces a buffering hold
		r.mu.Unlock()
		r.echo.expectPause(true)
		r.player.SetPaused(true)
		r.note(who+" paused", from)
	case actionPlay:
		r.mu.Lock()
		r.hold = ""
		r.mu.Unlock()
		r.echo.expectPause(false)
		r.player.SetPaused(false)
		r.note(who+" resumed", from)
	case actionSeek:
		r.echo.expectSeek(m.Pos)
		r.player.Seek(m.Pos)
		r.note(fmt.Sprintf("%s jumped to %s", who, clock(m.Pos)), from)
	}
	r.broadcastState()
}

// note tells everyone except the person who did it (their own mpv already
// shows the change) what just happened
func (r *Room) note(text string, except *peer) {
	if except != nil {
		r.player.ShowText(text)
	}
	r.publish(func(s *Status) { s.Note = text })
	r.mu.Lock()
	guests := make([]*peer, 0, len(r.guests))
	for g := range r.guests {
		if g != except {
			guests = append(guests, g)
		}
	}
	r.mu.Unlock()
	for _, g := range guests {
		g.send(Message{Type: msgNote, Text: text})
	}
}

// hostEvent handles the host's own pause/play/seek
func (r *Room) hostEvent(ev Event) {
	switch ev.Kind {
	case EventPause:
		r.note(r.name+" paused", nil)
	case EventPlay:
		r.note(r.name+" resumed", nil)
	case EventSeek:
		if st, err := r.player.State(); err == nil {
			r.note(fmt.Sprintf("%s jumped to %s", r.name, clock(st.Pos)), nil)
		}
	}
	r.mu.Lock()
	if r.hold != "" && (ev.Kind == EventPlay || ev.Kind == EventPause) {
		// The host took over manually: stop holding for buffering guests
		r.hold = ""
	}
	r.mu.Unlock()
	r.broadcastState()
}

// hostTick pauses everyone while a guest is buffering, and keeps guests
// updated with the host's position
func (r *Room) hostTick() {
	st, err := r.player.State()
	if err != nil {
		return
	}

	r.mu.Lock()
	var waiting []string
	for g := range r.guests {
		if g.buffering && time.Since(g.statusAt) < statusMaxAge {
			waiting = append(waiting, g.name)
		}
	}
	sort.Strings(waiting)
	action := ""
	switch {
	case len(waiting) > 0 && r.hold == "" && !st.Paused:
		r.hold = "waiting for " + joinNames(waiting)
		r.calm = 0
		action = "hold"
	case len(waiting) > 0 && r.hold != "":
		r.hold = "waiting for " + joinNames(waiting)
		r.calm = 0
	case len(waiting) == 0 && r.hold != "":
		// Give it a second so we don't flap on a single report
		r.calm++
		if r.calm >= 2 {
			r.hold = ""
			action = "release"
		}
	}
	hold := r.hold
	r.mu.Unlock()

	switch action {
	case "hold":
		r.echo.expectPause(true)
		r.player.SetPaused(true)
		r.player.ShowText("Paused: " + hold)
	case "release":
		r.echo.expectPause(false)
		r.player.SetPaused(false)
		r.player.ShowText("Everyone's ready")
	}
	members := r.members()
	r.publish(func(s *Status) { s.Hold = hold; s.Members = members })
	r.broadcastState()
}

func joinNames(names []string) string {
	if len(names) <= 2 {
		out := names[0]
		if len(names) == 2 {
			out += " and " + names[1]
		}
		return out
	}
	return fmt.Sprintf("%s and %d others", names[0], len(names)-1)
}

func (r *Room) broadcastState() {
	st, err := r.player.State()
	if err != nil {
		return
	}
	r.mu.Lock()
	msg := Message{Type: msgState, Paused: st.Paused, Pos: st.Pos, Buffering: st.Buffering,
		Hold: r.hold, Members: r.membersLocked()}
	guests := make([]*peer, 0, len(r.guests))
	for g := range r.guests {
		guests = append(guests, g)
	}
	r.mu.Unlock()
	for _, g := range guests {
		if g.send(msg) != nil {
			g.conn.Close() // the reader goroutine cleans up
		}
	}
}

// Guest ----------------------------------------------------------------------

// Join connects to a room and keeps player in sync with the host
func Join(player Player, inv Invite, name string) (*Room, error) {
	conn, err := net.DialTimeout("tcp", inv.Host, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("can't reach the room at %s: %w", inv.Host, err)
	}
	p := &peer{conn: conn}
	if err := p.send(Message{Type: msgHello, Token: inv.Token, Name: name}); err != nil {
		conn.Close()
		return nil, err
	}

	scanner := bufio.NewScanner(conn)
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var welcome Message
	if !scanner.Scan() || json.Unmarshal(scanner.Bytes(), &welcome) != nil {
		conn.Close()
		return nil, fmt.Errorf("the room didn't answer")
	}
	if welcome.Type == msgError {
		conn.Close()
		return nil, fmt.Errorf("room refused: %s", welcome.Text)
	}
	if welcome.Type != msgWelcome {
		conn.Close()
		return nil, fmt.Errorf("unexpected reply from room: %s", welcome.Type)
	}
	conn.SetReadDeadline(time.Time{})

	r := newRoom(RoleGuest, player, welcome.Name)
	r.conn = p
	r.status.Link = inv.Link()
	r.status.Members = welcome.Members

	r.wg.Add(1)
	go r.guestReadLoop(scanner)
	r.watchEvents(r.guestEvent)
	tick := 0
	r.every(syncInterval, func() {
		tick++
		r.sync(false)
		if tick%2 == 0 {
			r.sendStatus()
		}
		if tick%10 == 1 {
			p.send(Message{Type: msgPing, Stamp: time.Now().UnixNano()})
		}
	})
	r.publish(func(*Status) {})
	return r, nil
}

func (r *Room) guestReadLoop(scanner *bufio.Scanner) {
	defer r.wg.Done()
	for scanner.Scan() {
		var m Message
		if json.Unmarshal(scanner.Bytes(), &m) != nil {
			continue
		}
		switch m.Type {
		case msgState:
			r.mu.Lock()
			prev := r.hostState
			first := !r.haveState
			r.hostState, r.haveState, r.stateAt = m, true, time.Now()
			r.mu.Unlock()
			r.publish(func(s *Status) { s.Members = m.Members; s.Hold = m.Hold })
			if first || prev.Paused != m.Paused || prev.Hold != m.Hold {
				// The host changed: follow right away
				r.sync(true)
			}
		case msgNote:
			r.player.ShowText(m.Text)
			r.publish(func(s *Status) { s.Note = m.Text })
		case msgPong:
			sample := time.Duration(time.Now().UnixNano() - m.Stamp)
			r.mu.Lock()
			if r.rtt == 0 {
				r.rtt = sample
			} else {
				r.rtt = (r.rtt*3 + sample) / 4
			}
			rtt := r.rtt
			r.mu.Unlock()
			r.publish(func(s *Status) { s.RTT = rtt })
		}
	}
	if !r.stopped() {
		r.publish(func(s *Status) { s.Err = fmt.Errorf("lost connection to the host"); s.Closed = true })
		r.syncMu.Lock()
		r.setSpeed(1)
		r.syncMu.Unlock()
	}
}

// seekStarted stops a guest correcting its position while a seek (the user's
// or ours) is in flight. Otherwise a user's seek that takes a while to load
// would be "corrected" back before it landed and was forwarded to the host.
func (r *Room) seekStarted() {
	if r.role != RoleGuest {
		return
	}
	r.mu.Lock()
	r.seekingTil = time.Now().Add(seekLandTimeout)
	r.mu.Unlock()
}

// seekLanded ends the hold, keeping a short grace so a user's seek is
// forwarded before any correction could run
func (r *Room) seekLanded() {
	r.mu.Lock()
	r.seekingTil = time.Time{}
	if r.role == RoleGuest {
		r.graceUntil = time.Now().Add(controlGrace)
	}
	r.mu.Unlock()
}

// guestEvent forwards the guest's own pause/play/seek to the host
func (r *Room) guestEvent(ev Event) {
	m := Message{Type: msgControl}
	switch ev.Kind {
	case EventPause:
		m.Action = actionPause
	case EventPlay:
		m.Action = actionPlay
	case EventSeek:
		st, err := r.player.State()
		if err != nil {
			return
		}
		m.Action, m.Pos = actionSeek, st.Pos
	}
	r.mu.Lock()
	r.graceUntil = time.Now().Add(controlGrace)
	r.mu.Unlock()
	r.conn.send(m)
}

func (r *Room) sendStatus() {
	st, err := r.player.State()
	if err != nil {
		return
	}
	r.conn.send(Message{Type: msgStatus, Paused: st.Paused, Pos: st.Pos, Buffering: st.Buffering})
}

// sync brings the guest's player in line with the host's. urgent means the
// host just changed state, so a mismatch is certainly ours to fix.
func (r *Room) sync(urgent bool) {
	r.syncMu.Lock()
	defer r.syncMu.Unlock()
	r.mu.Lock()
	if !r.haveState || time.Now().Before(r.graceUntil) || time.Now().Before(r.seekingTil) {
		r.mu.Unlock()
		return
	}
	host, at, rtt := r.hostState, r.stateAt, r.rtt
	r.mu.Unlock()

	wantPaused := host.Paused || host.Buffering || host.Hold != ""
	// Estimate where the host is now, using our own clock only: time since
	// the state arrived plus half a round trip for the time it spent in flight
	expected := host.Pos
	if !wantPaused {
		expected += time.Since(at).Seconds() + rtt.Seconds()/2
	}

	local, err := r.player.State()
	if err != nil {
		return
	}
	drift := local.Pos - expected
	defer r.publish(func(s *Status) { s.Drift = drift })

	needPause := local.Paused != wantPaused
	needSeek := (wantPaused && math.Abs(drift) > 0.5) ||
		(!wantPaused && !local.Buffering && math.Abs(drift) > seekThreshold)
	if !needPause && !needSeek {
		r.deviating = time.Time{}
	} else if !urgent {
		// Our player changed on its own, probably the user. Their action is
		// usually reported within a few ms and then forwarded to the host, so
		// only step in if it's still like this a moment later.
		if r.deviating.IsZero() {
			r.deviating = time.Now()
			return
		}
		if time.Since(r.deviating) < confirmDelay {
			return
		}
	}
	if needPause || needSeek {
		r.deviating = time.Time{}
	}

	if local.Paused != wantPaused {
		r.echo.expectPause(wantPaused)
		r.player.SetPaused(wantPaused)
		if math.Abs(drift) > 1 {
			r.echo.expectSeek(expected)
			r.player.Seek(expected)
		}
		r.setSpeed(1)
		return
	}
	if wantPaused {
		// Line up exactly while paused so resuming starts in sync
		if math.Abs(drift) > 0.5 {
			r.echo.expectSeek(expected)
			r.player.Seek(expected)
		}
		return
	}
	if local.Buffering {
		return // don't fight our own buffering; the host holds for us
	}

	switch abs := math.Abs(drift); {
	case abs > seekThreshold:
		r.echo.expectSeek(expected)
		r.player.Seek(expected)
		r.setSpeed(1)
	case abs > speedStartDrift || (r.speed != 1 && abs > speedStopDrift):
		// Ahead -> slow down a little, behind -> speed up, until caught up
		nudge := math.Max(-maxNudge, math.Min(maxNudge, drift*speedGain))
		r.setSpeed(1 - nudge)
	default:
		r.setSpeed(1)
	}
}

func (r *Room) setSpeed(speed float64) {
	speed = math.Round(speed*1000) / 1000
	if speed == r.speed {
		return
	}
	if r.player.SetSpeed(speed) == nil {
		r.speed = speed
	}
}

func clock(sec float64) string {
	s := int(sec)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
