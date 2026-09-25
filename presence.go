package main

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sunnygitgud/sakuhaku/discord"
)

// presenceMsg reports the result of a Rich Presence update
type presenceMsg struct{ err error }

// presenceActivity describes what's playing for Discord, nil when nothing is
func (m *model) presenceActivity() *discord.Activity {
	pb := m.playback
	if pb == nil || !pb.Running {
		return nil
	}

	a := &discord.Activity{Type: discord.ActivityWatching, Details: pb.Release}
	if pb.Anime != nil {
		a.Details = animeTitle(pb.Anime)
	}

	state := "Watching"
	if pb.Episode > 0 {
		state = fmt.Sprintf("Episode %d", pb.Episode)
		if pb.Anime != nil && pb.Anime.Episodes != nil {
			state += fmt.Sprintf(" of %d", *pb.Anime.Episodes)
		}
	}
	if pb.Paused {
		state += " · Paused"
	}
	if m.room != nil {
		state += " · watching together"
	}
	a.State = state

	if !pb.Paused {
		// With a start and end Discord shows the time left in the episode.
		// Rounded so the start doesn't wobble between updates.
		start := time.Now().Add(-time.Duration(pb.Pos * float64(time.Second))).Truncate(5 * time.Second)
		a.Timestamps = &discord.Timestamps{Start: start.UnixMilli()}
		if pb.Duration > 0 {
			a.Timestamps.End = start.Add(time.Duration(pb.Duration * float64(time.Second))).UnixMilli()
		} else if pb.Pos == 0 {
			a.Timestamps.Start = pb.StartedAt.UnixMilli()
		}
	}

	if pb.Anime != nil {
		a.Assets = &discord.Assets{LargeImage: pb.Anime.PosterURL(), LargeText: animeTitle(pb.Anime)}
		if pb.Anime.SiteURL != "" {
			a.Buttons = []discord.Button{{Label: "View on AniList", URL: pb.Anime.SiteURL}}
		}
	}
	return a
}

// presenceKey summarises an activity; updates are only sent when it changes
// (Discord rate limits SET_ACTIVITY to about one every 4 seconds)
func presenceKey(a *discord.Activity) string {
	if a == nil {
		return "none"
	}
	key := a.Details + "|" + a.State
	if a.Timestamps != nil {
		// Normal playback keeps start constant; a seek moves it
		key += fmt.Sprintf("|%d", a.Timestamps.Start/10000)
	}
	return key
}

// updatePresence sends the current activity to Discord if it changed
func (m *model) updatePresence() tea.Cmd {
	if m.presence == nil {
		return nil
	}
	a := m.presenceActivity()
	key := presenceKey(a)
	if key == m.presenceKey {
		return nil
	}
	m.presenceKey = key
	client := m.presence
	return func() tea.Msg {
		return presenceMsg{err: client.SetActivity(a)}
	}
}
