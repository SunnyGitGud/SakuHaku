package main

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"
	tc "github.com/sunnygitgud/sakuhaku/torrentclient"
	"github.com/sunnygitgud/sakuhaku/watchparty"
)

// Watch together
//
// The host presses W on the streaming screen: SakuHaku opens a room (a small
// TCP server) and shows a sakuhaku://watch link. A guest pastes it after
// pressing J (or starts with -join LINK): their SakuHaku fetches the same
// torrent, connecting straight to the host's torrent client as a peer, opens
// mpv paused and follows the host. Pause, play and seek from anyone apply to
// everyone; see watchparty/room.go for how drift is corrected.

type roomStartedMsg struct {
	room   *watchparty.Room
	player *watchparty.MPV
	pb     *playback
	err    error
}

type roomStatusMsg struct {
	room   *watchparty.Room
	status watchparty.Status
}

// joinResolvedMsg is sent once a room link has been parsed and the anime
// looked up, ready to add the torrent
type joinResolvedMsg struct {
	ctx *streamContext
	err error
}

// defaultName is how we appear to others in a room
func defaultName() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		// Windows usernames come as DOMAIN\name
		return filepath.Base(strings.ReplaceAll(u.Username, `\`, "/"))
	}
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "someone"
}

// waitRoom delivers the next status update of a room
func waitRoom(room *watchparty.Room) tea.Cmd {
	return func() tea.Msg {
		return roomStatusMsg{room: room, status: <-room.Updates()}
	}
}

// hostRoom opens a room for the current playback
func (m *model) hostRoom() tea.Cmd {
	pb := m.playback
	switch {
	case pb == nil || !pb.Running:
		m.statusMsg = "Start playing something first"
		return nil
	case pb.Player != "mpv" || pb.ipcPath == "":
		m.statusMsg = "Watch together needs mpv"
		return nil
	case m.torrentClient == nil:
		return nil
	}
	magnet, err := m.torrentClient.MagnetLink(pb.InfoHash)
	if err != nil {
		m.statusMsg = err.Error()
		return nil
	}

	inv := watchparty.Invite{Source: magnet, File: pb.FilePath, Episode: pb.Episode, Title: pb.Title()}
	if pb.Anime != nil {
		inv.AnimeID = pb.Anime.ID
	}
	// Guests fetch from our torrent client directly; it's reachable wherever
	// the room is
	peerHost := watchparty.LocalIP()
	if cfg.RoomAddr != "" {
		peerHost = cfg.RoomAddr
		if h, _, err := net.SplitHostPort(cfg.RoomAddr); err == nil {
			peerHost = h
		}
	}
	if port := m.torrentClient.ListenPort(); port > 0 {
		inv.Peer = net.JoinHostPort(peerHost, fmt.Sprint(port))
	}

	m.statusMsg = "Opening room..."
	return func() tea.Msg {
		player, err := watchparty.DialMPV(pb.ipcPath, 5*time.Second)
		if err != nil {
			return roomStartedMsg{pb: pb, err: err}
		}
		room, err := watchparty.Host(player, net.JoinHostPort("", cfg.RoomPort), cfg.RoomAddr, cfg.Name, inv)
		if err != nil {
			player.Close()
			return roomStartedMsg{pb: pb, err: err}
		}
		return roomStartedMsg{room: room, player: player, pb: pb}
	}
}

// startJoin begins joining a room from its link
func (m *model) startJoin(link string) tea.Cmd {
	inv, err := watchparty.ParseInvite(strings.TrimSpace(link))
	if err != nil {
		m.statusMsg = "Can't join: " + err.Error()
		return nil
	}
	if m.torrentClient == nil {
		m.statusMsg = "Can't join: torrent client unavailable"
		return nil
	}
	m.loading = true
	m.loadingMsg = "Joining room at " + inv.Host + "..."
	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		ctx := &streamContext{episode: inv.Episode, invite: &inv}
		if inv.AnimeID > 0 {
			// Only for the info card, presence and tracking; joining works without it
			if a, err := fetchAnimeByID(inv.AnimeID); err == nil {
				ctx.anime = a
				n, _ := walkPrequels(a.ID)
				ctx.numbering = n
			}
		}
		return joinResolvedMsg{ctx: ctx}
	})
}

// joinRoom connects a guest's freshly started mpv to the host
func joinRoom(pb *playback) tea.Cmd {
	return func() tea.Msg {
		player, err := watchparty.DialMPV(pb.ipcPath, 15*time.Second)
		if err != nil {
			return roomStartedMsg{pb: pb, err: err}
		}
		room, err := watchparty.Join(player, *pb.invite, cfg.Name)
		if err != nil {
			player.Close()
			return roomStartedMsg{pb: pb, err: err}
		}
		return roomStartedMsg{room: room, player: player, pb: pb}
	}
}

// closeRoom leaves or shuts down the current room
func (m *model) closeRoom() tea.Cmd {
	room, player := m.room, m.roomPlayer
	m.room, m.roomPlayer = nil, nil
	m.roomStatus = watchparty.Status{}
	if room == nil {
		return nil
	}
	return func() tea.Msg {
		room.Close()
		player.Close()
		return nil
	}
}

// copyToClipboard uses the terminal's OSC 52 support, which also works over SSH
func copyToClipboard(s string) {
	termenv.Copy(s)
}

func (m *model) handleRoomMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case joinResolvedMsg:
		ctx := msg.ctx
		inv := ctx.invite
		if ctx.anime != nil {
			m.selectedAnime = ctx.anime
		}
		m.loadingMsg = "Fetching the torrent from the room..."
		return m.torrentClient.AddAsyncTagged(inv.Source, tc.ModeStream, ctx, inv.Peer), true

	case roomStartedMsg:
		if msg.err != nil {
			m.statusMsg = "Watch together: " + msg.err.Error()
			if msg.pb.invite != nil {
				m.statusMsg += " (is the host reachable? see README)"
			}
			return nil, true
		}
		if msg.pb != m.playback || !msg.pb.Running {
			// The player closed while we were connecting
			msg.room.Close()
			msg.player.Close()
			return nil, true
		}
		m.closeRoom()
		m.room, m.roomPlayer = msg.room, msg.player
		m.roomStatus = msg.room.Status()
		if msg.room.Role() == watchparty.RoleHost {
			copyToClipboard(m.roomStatus.Link)
			m.statusMsg = "Room open, invite link copied (c copies it again)"
		} else {
			m.statusMsg = "Joined the room, following the host"
		}
		if m.mode == ModeStreaming {
			m.viewport.SetContent(m.renderContent())
		}
		return waitRoom(msg.room), true

	case roomStatusMsg:
		if msg.room != m.room {
			return nil, true // an old room
		}
		m.roomStatus = msg.status
		var cmd tea.Cmd
		if msg.status.Closed {
			if msg.status.Err != nil {
				m.statusMsg = "Watch together: " + msg.status.Err.Error()
			}
			cmd = m.closeRoom()
		} else {
			if msg.status.Note != "" {
				m.statusMsg = msg.status.Note
			}
			cmd = waitRoom(msg.room)
		}
		if m.mode == ModeStreaming {
			m.viewport.SetContent(m.renderContent())
		}
		return cmd, true
	}
	return nil, false
}

// handleJoinInput handles pasting a room link after J
func (m *model) handleJoinInput(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		m.joinInputMode = false
		m.joinInput = ""
	case tea.KeyEnter:
		m.joinInputMode = false
		link := m.joinInput
		m.joinInput = ""
		return m.startJoin(link)
	case tea.KeyBackspace:
		if len(m.joinInput) > 0 {
			m.joinInput = m.joinInput[:len(m.joinInput)-1]
		}
	case tea.KeyRunes, tea.KeySpace:
		// Pastes arrive as one message with all the runes
		m.joinInput += string(msg.Runes)
	}
	return nil
}

// renderRoom adds the watch-together rows to the streaming screen
func (m *model) renderRoom(row func(label, value string), width int) string {
	st := m.roomStatus
	if m.room == nil {
		if m.playback != nil && m.playback.Player == "mpv" && m.playback.Running {
			row("Together", dimStyle.Render("W opens a watch-together room"))
		}
		return ""
	}

	role := "hosting"
	if st.Role == watchparty.RoleGuest {
		role = "joined"
	}
	row("Together", fmt.Sprintf("%s · %d watching: %s  (W to leave)", role, len(st.Members), strings.Join(st.Members, ", ")))
	if st.Hold != "" {
		row("", statusStyle.Render("Paused: "+st.Hold))
	}
	if st.Role == watchparty.RoleGuest {
		sync := "in sync"
		switch d := st.Drift; {
		case d > 0.15:
			sync = fmt.Sprintf("%.2fs ahead, catching up", d)
		case d < -0.15:
			sync = fmt.Sprintf("%.2fs behind, catching up", -d)
		}
		ping := "<1ms"
		if st.RTT >= time.Millisecond {
			ping = st.RTT.Round(time.Millisecond).String()
		}
		row("Sync", fmt.Sprintf("%s · ping %s", sync, ping))
	}

	// The link can be long: wrap it so it can be selected in full
	var sb strings.Builder
	link := st.Link
	chunk := max(20, width-12)
	for i := 0; i < len(link); i += chunk {
		label := ""
		if i == 0 {
			label = "Invite"
		}
		row(label, link[i:min(len(link), i+chunk)])
	}
	row("", dimStyle.Render("c copies the link · guests press J and paste it"))
	return sb.String()
}
