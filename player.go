package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/pkg/browser"
	tc "github.com/sunnygitgud/sakuhaku/torrentclient"
)

//go:embed mpv/sakuhaku.lua
var mpvScript []byte

// watchedThreshold is how far into an episode counts as having watched it
const watchedThreshold = 0.85

// playback is the state of the episode currently being streamed
type playback struct {
	InfoHash string
	FilePath string // display path of the file inside the torrent
	Release  string // torrent name
	URL      string
	Anime    *Anime
	Entry    *UserAnimeEntry
	Episode  int

	Player      string
	Running     bool
	StartedAt   time.Time
	ResumedFrom float64

	dir        string // temp dir holding the info/status files
	statusPath string
	proc       *exec.Cmd

	// From mpv's status file
	Pos, Duration float64
	Paused, EOF   bool

	Tracked  bool   // AniList update sent (or not needed)
	TrackMsg string // outcome shown on the streaming screen
}

// Title is a human friendly name for what is playing
func (pb *playback) Title() string {
	name := pb.Release
	if pb.Anime != nil {
		name = animeTitle(pb.Anime)
	}
	if pb.Episode > 0 {
		name += fmt.Sprintf(" — Episode %d", pb.Episode)
	}
	return name
}

type playerStartedMsg struct {
	pb  *playback
	err error
}

type playerExitedMsg struct {
	pb  *playback
	err error
}

// chooseVideoFile picks the file to play: the one for the wanted episode if a
// batch has it, otherwise the largest video
func chooseVideoFile(t *torrent.Torrent, wantEpisode int) (*torrent.File, int) {
	videos := tc.GetAllVideoFiles(t)
	if wantEpisode > 0 {
		for _, f := range videos {
			if parseEpisode(path.Base(f.DisplayPath())).Episode == wantEpisode {
				return f, wantEpisode
			}
		}
	}
	f := tc.GetLargestVideoFile(t)
	if f == nil {
		return nil, 0
	}
	ep := parseEpisode(path.Base(f.DisplayPath())).Episode
	if ep == 0 {
		ep = parseEpisode(t.Name()).Episode
	}
	return f, ep
}

// findPlayer returns the player to launch: -player if given, else the first
// supported one on PATH
func findPlayer() (string, error) {
	candidates := []string{"mpv", "vlc", "ffplay", "mplayer"}
	if cfg.Player != "" {
		candidates = append([]string{cfg.Player}, candidates...)
	}
	for _, p := range candidates {
		if resolved, err := exec.LookPath(p); err == nil {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("no video player found (install mpv)")
}

// playerKind maps a player path to the player family we know how to drive:
// "/usr/bin/mpv", "mpv.com" and "mpv-wrapper" are all mpv
func playerKind(player string) string {
	base := strings.ToLower(filepath.Base(player))
	base = strings.TrimSuffix(strings.TrimSuffix(base, ".exe"), ".com")
	for _, known := range []string{"mpv", "vlc", "ffplay", "mplayer"} {
		if strings.HasPrefix(base, known) {
			return known
		}
	}
	return base
}

// startPlayback launches the player for pb
func startPlayback(pb *playback, token bool) tea.Cmd {
	return func() tea.Msg {
		player, err := findPlayer()
		if err != nil {
			// Last resort: most browsers can at least play mp4/webm
			browser.OpenURL(pb.URL)
			pb.Player = "browser"
			return playerStartedMsg{pb: pb, err: err}
		}
		pb.Player = playerKind(player)

		if rec, ok := history.get(pb.InfoHash, pb.FilePath); ok && rec.resumable() {
			pb.ResumedFrom = rec.Pos
		}

		var args []string
		env := os.Environ()
		title := pb.Title()

		switch pb.Player {
		case "mpv":
			if err := pb.prepareMPV(token); err != nil {
				return playerStartedMsg{pb: pb, err: err}
			}
			args = []string{
				"--force-media-title=" + title,
				"--script=" + filepath.Join(pb.dir, "sakuhaku.lua"),
				"--cache=yes",
			}
			if pb.ResumedFrom > 0 {
				args = append(args, fmt.Sprintf("--start=%.0f", pb.ResumedFrom))
			}
			env = append(env,
				"SAKUHAKU_INFO="+filepath.Join(pb.dir, "info.json"),
				"SAKUHAKU_STATUS="+pb.statusPath)
		case "vlc":
			args = []string{"--meta-title=" + title}
			if pb.ResumedFrom > 0 {
				args = append(args, fmt.Sprintf("--start-time=%.0f", pb.ResumedFrom))
			}
		case "ffplay":
			args = []string{"-window_title", title}
			if pb.ResumedFrom > 0 {
				args = append(args, "-ss", fmt.Sprintf("%.0f", pb.ResumedFrom))
			}
		}
		args = append(args, pb.URL)

		cmd := exec.Command(player, args...)
		cmd.Env = env
		if err := cmd.Start(); err != nil {
			pb.cleanup()
			return playerStartedMsg{pb: pb, err: err}
		}
		pb.proc = cmd
		pb.Running = true
		pb.StartedAt = time.Now()
		return playerStartedMsg{pb: pb}
	}
}

// waitForPlayer reports when the player process exits
func waitForPlayer(pb *playback) tea.Cmd {
	return func() tea.Msg {
		err := pb.proc.Wait()
		return playerExitedMsg{pb: pb, err: err}
	}
}

// mpvInfo is what the Lua script shows on its info card
type mpvInfo struct {
	Title         string  `json:"title"`
	Episode       int     `json:"episode"`
	TotalEpisodes int     `json:"total_episodes"`
	Format        string  `json:"format"`
	Season        string  `json:"season"`
	Score         string  `json:"score"`
	Genres        string  `json:"genres"`
	Studios       string  `json:"studios"`
	NextAiring    string  `json:"next_airing"`
	Progress      string  `json:"progress"`
	Description   string  `json:"description"`
	Release       string  `json:"release"`
	Tracking      bool    `json:"tracking"`
	WatchedAt     float64 `json:"watched_at"`
}

var reHTMLTag = regexp.MustCompile(`<[^>]*>`)

// cleanDescription turns AniList's HTML-ish description into plain text
func cleanDescription(s string) string {
	s = strings.NewReplacer("<br>", "\n", "<br/>", "\n", "<br />", "\n", "&quot;", `"`, "&amp;", "&", "&#039;", "'", "&lt;", "<", "&gt;", ">").Replace(s)
	s = reHTMLTag.ReplaceAllString(s, "")
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) > 600 {
		s = string([]rune(s)[:597]) + "..."
	}
	return s
}

// prepareMPV writes the Lua script and the info file for this session
func (pb *playback) prepareMPV(tracking bool) error {
	dir, err := os.MkdirTemp("", "sakuhaku-play-")
	if err != nil {
		return err
	}
	pb.dir = dir
	pb.statusPath = filepath.Join(dir, "status.json")

	if err := os.WriteFile(filepath.Join(dir, "sakuhaku.lua"), mpvScript, 0o644); err != nil {
		return err
	}

	info := mpvInfo{
		Title:     pb.Release,
		Episode:   pb.Episode,
		Release:   pb.Release + " / " + path.Base(pb.FilePath),
		WatchedAt: watchedThreshold,
	}
	if a := pb.Anime; a != nil {
		info.Title = animeTitle(a)
		if a.Title.Romaji != "" && a.Title.Romaji != info.Title {
			info.Title += " (" + a.Title.Romaji + ")"
		}
		if a.Episodes != nil {
			info.TotalEpisodes = *a.Episodes
		}
		info.Format = a.Format
		if a.Season != "" && a.SeasonYear != nil {
			info.Season = fmt.Sprintf("%s %d", a.Season, *a.SeasonYear)
		}
		if a.Score != nil {
			info.Score = fmt.Sprintf("Score %d%%", *a.Score)
		}
		info.Genres = strings.Join(a.Genres, " · ")
		var studios []string
		for _, s := range a.Studios.Nodes {
			studios = append(studios, s.Name)
		}
		info.Studios = strings.Join(studios, ", ")
		if n := a.NextAiringEpisode; n != nil {
			info.NextAiring = fmt.Sprintf("Episode %d %s", n.Episode, formatUntil(n.AiringAt))
		}
		info.Description = cleanDescription(a.Description)
		info.Tracking = tracking && pb.Episode > 0
	}
	if e := pb.Entry; e != nil {
		total := "?"
		if info.TotalEpisodes > 0 {
			total = fmt.Sprint(info.TotalEpisodes)
		}
		info.Progress = fmt.Sprintf("%d/%s (%s)", e.Progress, total, strings.ToLower(e.Status))
	}

	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "info.json"), data, 0o644)
}

// mpvStatus is written by the Lua script
type mpvStatus struct {
	Pos      float64 `json:"pos"`
	Duration float64 `json:"duration"`
	Paused   bool    `json:"paused"`
	EOF      bool    `json:"eof"`
}

// readStatus refreshes the playback position from mpv's status file
func (pb *playback) readStatus() {
	if pb.statusPath == "" {
		return
	}
	data, err := os.ReadFile(pb.statusPath)
	if err != nil {
		return
	}
	var st mpvStatus
	if json.Unmarshal(data, &st) != nil {
		return
	}
	pb.Pos, pb.Duration, pb.Paused = st.Pos, st.Duration, st.Paused
	pb.EOF = pb.EOF || st.EOF
}

// watched reports whether enough of the episode has been played
func (pb *playback) watched() bool {
	return pb.EOF || (pb.Duration > 0 && pb.Pos/pb.Duration >= watchedThreshold)
}

func (pb *playback) cleanup() {
	if pb.dir != "" {
		os.RemoveAll(pb.dir)
		pb.dir = ""
		pb.statusPath = ""
	}
}

// formatUntil renders a unix time as "in 3d 4h"
func formatUntil(unix int64) string {
	d := time.Until(time.Unix(unix, 0))
	if d <= 0 {
		return "has aired"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	if days > 0 {
		return fmt.Sprintf("in %dd %dh", days, hours)
	}
	return fmt.Sprintf("in %dh %dm", hours, int(d.Minutes())%60)
}

// formatClock renders seconds as 1:02:03 or 12:34
func formatClock(sec float64) string {
	s := int(sec)
	if s < 0 {
		s = 0
	}
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// Watch history ---------------------------------------------------------------

// watchRecord remembers where playback of a file stopped
type watchRecord struct {
	Pos      float64 `json:"pos"`
	Duration float64 `json:"duration"`
	Title    string  `json:"title"`
	Updated  int64   `json:"updated"`
}

// resumable is true when it's worth offering to continue from Pos
func (r watchRecord) resumable() bool {
	return r.Pos > 30 && (r.Duration == 0 || r.Pos < r.Duration*0.95)
}

type watchHistory struct {
	mu      sync.Mutex
	path    string
	records map[string]watchRecord
	loaded  bool
}

var history = &watchHistory{}

func historyKey(infoHash, filePath string) string { return infoHash + "/" + filePath }

func (h *watchHistory) load() {
	if h.loaded {
		return
	}
	h.loaded = true
	h.records = map[string]watchRecord{}
	dir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	h.path = filepath.Join(dir, "sakuhaku", "history.json")
	if data, err := os.ReadFile(h.path); err == nil {
		json.Unmarshal(data, &h.records)
	}
}

func (h *watchHistory) get(infoHash, filePath string) (watchRecord, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	r, ok := h.records[historyKey(infoHash, filePath)]
	return r, ok
}

// save records the position, forgetting finished episodes
func (h *watchHistory) save(pb *playback) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	if h.path == "" {
		return
	}
	key := historyKey(pb.InfoHash, pb.FilePath)
	rec := watchRecord{Pos: pb.Pos, Duration: pb.Duration, Title: pb.Title(), Updated: time.Now().Unix()}
	if pb.EOF || !rec.resumable() {
		delete(h.records, key)
	} else {
		h.records[key] = rec
	}
	data, err := json.MarshalIndent(h.records, "", "  ")
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(h.path), 0o755)
	os.WriteFile(h.path, data, 0o644)
}
