package main

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func key(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// assertFits checks the whole view is exactly the terminal size: anything
// taller scrolls the header off screen, anything wider wraps
func assertFits(t *testing.T, m *model, w, h int, screen string) {
	t.Helper()
	lines := strings.Split(m.View(), "\n")
	if len(lines) != h {
		t.Errorf("%s: view is %d lines, terminal is %d", screen, len(lines), h)
	}
	for i, l := range lines {
		if lw := ansi.StringWidth(l); lw > w {
			t.Errorf("%s: line %d is %d wide, terminal is %d", screen, i, lw, w)
		}
	}
}

func TestEveryScreenFitsTheTerminal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	saved := cfg
	defer func() { cfg = saved }()
	cfg.DownloadDir = t.TempDir()

	const w, h = 120, 30
	m := initialModel()
	defer m.torrentClient.Close()
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})

	eps, score, year := 12, 85, 2024
	var entries []UserAnimeEntry
	for i := range 40 {
		a := Anime{ID: i + 1, Episodes: &eps, Score: &score, SeasonYear: &year, Format: "TV", Season: "FALL"}
		a.Title.Romaji = fmt.Sprintf("葬送のフリーレン Sousou no Frieren with a long title %d", i)
		entries = append(entries, UserAnimeEntry{Media: a})
	}

	m.Update(key("s"))
	m.Update(listPageMsg{listType: ListTrending, entries: entries, page: 1, lastPage: 3, hasNext: true})
	assertFits(t, m, w, h, "list")
	for range 35 {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	assertFits(t, m, w, h, "list scrolled")
	if !strings.Contains(m.View(), "title 35") {
		t.Error("selected entry scrolled out of view")
	}
	if !strings.Contains(m.View(), "Page 1/3") {
		t.Error("footer should show the page")
	}

	var torrents []Torrent
	for i := range 30 {
		torrents = append(torrents, Torrent{Title: fmt.Sprintf("[Group] Frieren - %02d (1080p) [ABCD1234].mkv", i+1), Seeders: i, TotalSize: 1 << 30, Source: "nyaa"})
	}
	m.Update(key("\r"))
	m.Update(torrentSearchResultMsg{torrents: torrents})
	assertFits(t, m, w, h, "torrents")

	// Filters
	m.Update(key("f"))
	m.Update(key("f"))
	m.Update(key("f")) // >= 10 seeders
	if len(m.torrents) != 20 {
		t.Errorf("min seeders 10: got %d torrents", len(m.torrents))
	}
	m.Update(key("e"))
	m.Update(key("1"))
	m.Update(key("5"))
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.torrents) != 1 || !strings.Contains(m.torrents[0].Title, "- 15") {
		t.Errorf("episode filter: got %v", m.torrents)
	}
	assertFits(t, m, w, h, "torrents filtered")

	m.Update(key("D"))
	assertFits(t, m, w, h, "downloads")
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != ModeTorrents {
		t.Errorf("esc from downloads should go back to torrents, got %v", m.mode)
	}

	m.playback = &playback{Release: "Show", FilePath: "Show - 15.mkv", Episode: 15, Player: "mpv", Pos: 60, Duration: 1440}
	m.mode = ModeStreaming
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	assertFits(t, m, w, h, "streaming")
}
