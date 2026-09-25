package watchparty

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strconv"
)

// Message is one line of newline-delimited JSON on the room connection.
//
//	guest -> host: hello, control, status, ping
//	host -> guest: welcome, state, note, pong, error
type Message struct {
	Type string `json:"type"`

	// hello / welcome
	Token   string   `json:"token,omitempty"`
	Name    string   `json:"name,omitempty"`
	Members []string `json:"members,omitempty"`

	// state (host's player) / status (guest's player)
	Paused    bool    `json:"paused"`
	Pos       float64 `json:"pos"`
	Buffering bool    `json:"buffering,omitempty"`
	// Hold is set while the host has paused everyone for a buffering guest
	Hold string `json:"hold,omitempty"`

	// control: a guest's pause/play/seek
	Action string `json:"action,omitempty"`

	// ping / pong carry the sender's timestamp back for round trip time
	Stamp int64 `json:"stamp,omitempty"`

	Text string `json:"text,omitempty"`
}

const (
	msgHello   = "hello"
	msgWelcome = "welcome"
	msgState   = "state"
	msgControl = "control"
	msgStatus  = "status"
	msgPing    = "ping"
	msgPong    = "pong"
	msgError   = "error"
	msgNote    = "note" // something happened, shown in mpv: "alex paused"

	actionPause = "pause"
	actionPlay  = "play"
	actionSeek  = "seek"
)

// Invite is everything a guest needs to join: where the host is and what is
// playing. It's shared as a sakuhaku://watch?... link.
type Invite struct {
	Host    string // host:port of the room
	Token   string // shared secret
	Source  string // magnet link of the torrent
	File    string // file inside the torrent
	Peer    string // host's torrent client (ip:port) to fetch from directly
	AnimeID int
	Episode int
	Title   string
}

// Link encodes the invite
func (inv Invite) Link() string {
	q := url.Values{}
	q.Set("host", inv.Host)
	q.Set("token", inv.Token)
	q.Set("src", inv.Source)
	if inv.File != "" {
		q.Set("file", inv.File)
	}
	if inv.Peer != "" {
		q.Set("peer", inv.Peer)
	}
	if inv.AnimeID > 0 {
		q.Set("anime", strconv.Itoa(inv.AnimeID))
	}
	if inv.Episode > 0 {
		q.Set("ep", strconv.Itoa(inv.Episode))
	}
	if inv.Title != "" {
		q.Set("title", inv.Title)
	}
	return "sakuhaku://watch?" + q.Encode()
}

// ParseInvite decodes a room link
func ParseInvite(link string) (Invite, error) {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "sakuhaku" || u.Host != "watch" {
		return Invite{}, fmt.Errorf("not a SakuHaku room link")
	}
	q := u.Query()
	inv := Invite{
		Host:   q.Get("host"),
		Token:  q.Get("token"),
		Source: q.Get("src"),
		File:   q.Get("file"),
		Peer:   q.Get("peer"),
		Title:  q.Get("title"),
	}
	inv.AnimeID, _ = strconv.Atoi(q.Get("anime"))
	inv.Episode, _ = strconv.Atoi(q.Get("ep"))
	if _, _, err := net.SplitHostPort(inv.Host); err != nil {
		return Invite{}, fmt.Errorf("room link has no valid host: %w", err)
	}
	if inv.Token == "" || inv.Source == "" {
		return Invite{}, fmt.Errorf("room link is incomplete")
	}
	return inv, nil
}

// newToken makes a random room secret
func newToken() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// LocalIP guesses the address other machines on the LAN can reach us at.
// Dialing UDP sends no packets; it just picks the outgoing interface.
func LocalIP() string {
	conn, err := net.Dial("udp", "192.0.2.1:9") // TEST-NET-1, never routed
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
