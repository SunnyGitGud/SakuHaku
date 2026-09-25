package main

import (
	"fmt"
	"path"

	"github.com/anacrolix/torrent"
	tea "github.com/charmbracelet/bubbletea"
	tc "github.com/sunnygitgud/sakuhaku/torrentclient"
)

// streamContext is what we know about a torrent from where it was picked:
// which anime, the user's list entry and the episode they were after
type streamContext struct {
	anime   *Anime
	entry   *UserAnimeEntry
	episode int
}

// currentStreamContext captures the anime/episode the torrent list is for
func (m *model) currentStreamContext() *streamContext {
	ctx := &streamContext{episode: m.epFilter}
	if m.selectedAnime != nil {
		a := *m.selectedAnime
		ctx.anime = &a
	}
	if m.selectedEntry != nil {
		e := *m.selectedEntry
		ctx.entry = &e
	}
	return ctx
}

func (m *model) startTorrentStream(source string) tea.Cmd {
	if m.torrentClient == nil {
		return nil
	}
	return m.torrentClient.AddAsyncTagged(source, tc.ModeStream, m.currentStreamContext())
}

// playTorrent starts streaming the right file of t and opens the streaming screen
func (m *model) playTorrent(t *torrent.Torrent, ctx *streamContext) tea.Cmd {
	if ctx == nil {
		ctx = &streamContext{}
	}
	file, episode := chooseVideoFile(t, ctx.episode)
	if file == nil {
		m.torrentClient.StartDownload(t.InfoHash().HexString())
		m.statusMsg = "No video file found in torrent, downloading it instead (D to view)"
		return nil
	}

	pb := &playback{
		InfoHash: t.InfoHash().HexString(),
		FilePath: file.DisplayPath(),
		Release:  t.Name(),
		URL:      m.torrentClient.ServeTorrentEpisode(t, file.DisplayPath()),
		Anime:    ctx.anime,
		Entry:    ctx.entry,
		Episode:  episode,
	}
	m.streamURL = pb.URL
	m.playback = pb

	if m.mode != ModeStreaming {
		m.streamFrom = m.mode
	}
	m.mode = ModeStreaming
	m.statusMsg = "Starting player for " + path.Base(file.DisplayPath())
	if m.ready {
		m.viewport.SetContent(m.renderContent())
		m.viewport.GotoTop()
	}
	return tea.Batch(startPlayback(pb, m.trackingEnabled()), m.startTicking())
}

func (m *model) trackingEnabled() bool {
	return m.accessToken != "" && !cfg.NoTracking
}

// maybeTrack sends the AniList progress update once an episode is watched
func (m *model) maybeTrack(pb *playback, force bool) tea.Cmd {
	if pb == nil || pb.Tracked || (!force && !pb.watched()) {
		return nil
	}
	pb.Tracked = true
	switch {
	case cfg.NoTracking:
		pb.TrackMsg = "Tracking disabled (-no-tracking)"
	case m.accessToken == "":
		pb.TrackMsg = "Log in to sync your progress to AniList"
	case pb.Anime == nil || pb.Anime.ID == 0:
		pb.TrackMsg = "Don't know which anime this is, AniList not updated"
	case pb.Episode <= 0:
		pb.TrackMsg = "Couldn't tell which episode this is, AniList not updated"
	default:
		pb.TrackMsg = fmt.Sprintf("Updating AniList: episode %d...", pb.Episode)
		return updateAniListProgress(m.accessToken, pb.Anime.ID, animeTitle(pb.Anime), pb.Episode, pb.Anime.Episodes)
	}
	return nil
}
