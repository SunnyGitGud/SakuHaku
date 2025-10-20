package main

import (
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pkg/browser"
)

func (m *model) startTorrentStream(magnetURI string) tea.Cmd {
	if m.torrentClient == nil {
		return nil
	}

	return m.torrentClient.AddTorrentAsync(magnetURI)
}

func openVideoPlayer(streamURL string) tea.Cmd {
	return func() tea.Msg {
		// Try different video players in order of preference
		players := []string{"mpv", "vlc", "ffplay", "mplayer"}

		for _, player := range players {
			cmd := exec.Command(player, streamURL)
			if err := cmd.Start(); err == nil {
				return videoPlayerOpenedMsg{player: player, url: streamURL}
			}
		}

		// Fallback: open in browser
		browser.OpenURL(streamURL)
		return videoPlayerOpenedMsg{player: "browser", url: streamURL}
	}
}

type videoPlayerOpenedMsg struct {
	player string
	url    string
}
