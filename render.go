package main

import (
	"fmt"
	"strings"

	tc "github.com/sunnygitgud/sakuhaku/torrentclient"
)

func (m *model) renderView() string {
	if m.loading {
		return fmt.Sprintf("\n\n   %s %s\n\n", m.spinner.View(), m.loadingMsg)
	}
	if m.mode == ModeLogin {
		return fmt.Sprintf("\n\n  🎬 AniList Torrent Browser\n\n  %s\n\n", m.loginMsg)
	}

	if !m.ready {
		return "\n  Loading..."
	}
	return fmt.Sprintf("%s\n%s\n%s",
		m.headerView(),
		m.viewport.View(),
		m.footerView())
}

// View Components
func (m *model) renderContent() string {
	switch m.mode {
	case ModeUserList:
		return m.renderUserListContent()
	case ModeAnimeSearch:
		return m.renderAnimeContent()
	case ModeTorrents:
		return m.renderTorrentContent()
	}
	return ""
}

func (m *model) renderUserListContent() string {
	if len(m.userEntries) == 0 {
		return "No anime found."
	}

	// Get selected entry for right panel
	var selectedEntry *UserAnimeEntry
	if m.userEntryCursor < len(m.userEntries) {
		selectedEntry = &m.userEntries[m.userEntryCursor]
	}

	// Calculate dynamic sizes based on viewport
	leftWidth := m.viewport.Width / 2
	rightWidth := m.viewport.Width - leftWidth - 3

	imageWidth := m.viewport.Width / 3
	imageHeight := m.viewport.Height / 2 // Not really used, aspect ratio handles it

	// Left panel - list
	var leftPanel strings.Builder

	// List header
	listTitle := m.currentListType.String()
	if m.username != "" && (m.currentListType == ListCurrentlyWatching || m.currentListType == ListPlanToWatch) {
		leftPanel.WriteString(fmt.Sprintf("👤 %s's %s\n\n", m.username, listTitle))
	} else {
		leftPanel.WriteString(fmt.Sprintf("📺 %s\n\n", listTitle))
	}

	for i, entry := range m.userEntries {
		cursor := "  "
		if m.userEntryCursor == i {
			cursor = "▶ "
		}

		title := entry.Media.Title.English
		if title == "" {
			title = entry.Media.Title.Romaji
		}

		// Truncate title based on left panel width
		maxTitleWidth := leftWidth - 15
		if maxTitleWidth < 20 {
			maxTitleWidth = 20
		}
		if len(title) > maxTitleWidth {
			title = title[:maxTitleWidth-3] + "..."
		}

		// Show different info based on list type
		var info string
		if m.currentListType == ListCurrentlyWatching || m.currentListType == ListPlanToWatch {
			episodes := "?"
			if entry.Media.Episodes != nil {
				episodes = fmt.Sprintf("%d", *entry.Media.Episodes)
			}
			progress := fmt.Sprintf("%d/%s", entry.Progress, episodes)
			info = fmt.Sprintf(" (%s)", progress)
		} else {
			if entry.Media.Score != nil {
				info = fmt.Sprintf(" ⭐%d%%", *entry.Media.Score)
			}
		}

		// Apply pink color to selected entry
		if m.userEntryCursor == i {
			line := fmt.Sprintf("%s%s%s\n", cursor, selectedStyle.Render(title), info)
			leftPanel.WriteString(line)
		} else {
			line := fmt.Sprintf("%s%s%s\n", cursor, title, info)
			leftPanel.WriteString(line)
		}
	}

	// Right panel - details
	var rightPanel strings.Builder
	if selectedEntry != nil {
		// Render poster with dynamic sizing
		if selectedEntry.Media.CoverImage.Large != "" {
			poster := getAnimePoster(selectedEntry.Media.CoverImage.Large, imageWidth, imageHeight)
			rightPanel.WriteString(poster)
			rightPanel.WriteString("\n")
		}

		// Title
		title := selectedEntry.Media.Title.English
		if title == "" {
			title = selectedEntry.Media.Title.Romaji
		}

		// Wrap title if too long for right panel
		wrappedTitle := wrapText(title, rightWidth)
		rightPanel.WriteString(fmt.Sprintf("📺 %s\n\n", wrappedTitle))

		// Show different details based on list type
		if m.currentListType == ListCurrentlyWatching || m.currentListType == ListPlanToWatch {
			// User's personal data
			episodes := "?"
			if selectedEntry.Media.Episodes != nil {
				episodes = fmt.Sprintf("%d", *selectedEntry.Media.Episodes)
			}
			rightPanel.WriteString(fmt.Sprintf("Progress: %d/%s\n", selectedEntry.Progress, episodes))

			if selectedEntry.Score > 0 {
				rightPanel.WriteString(fmt.Sprintf("Your Score: ⭐ %.1f/10\n", selectedEntry.Score))
			}

			rightPanel.WriteString(fmt.Sprintf("Status: %s\n", selectedEntry.Status))

			if selectedEntry.UpdatedAt > 0 {
				rightPanel.WriteString(fmt.Sprintf("Updated: %s\n", formatRelativeTime(selectedEntry.UpdatedAt)))
			}
			rightPanel.WriteString("\n")
		}

		// Anime details
		score := "N/A"
		if selectedEntry.Media.Score != nil {
			score = fmt.Sprintf("%d%%", *selectedEntry.Media.Score)
		}

		rightPanel.WriteString(fmt.Sprintf("Format: %s\n", selectedEntry.Media.Format))
		rightPanel.WriteString(fmt.Sprintf("Avg Score: %s\n", score))

		episodes := "?"
		if selectedEntry.Media.Episodes != nil {
			episodes = fmt.Sprintf("%d", *selectedEntry.Media.Episodes)
		}
		rightPanel.WriteString(fmt.Sprintf("Episodes: %s\n", episodes))

		if selectedEntry.Media.Season != "" {
			year := ""
			if selectedEntry.Media.SeasonYear != nil {
				year = fmt.Sprintf(" %d", *selectedEntry.Media.SeasonYear)
			}
			rightPanel.WriteString(fmt.Sprintf("Season: %s%s\n", selectedEntry.Media.Season, year))
		}

		// Cache information
		if cachedEntries, exists := m.cacheInfo[selectedEntry.Media.ID]; exists && len(cachedEntries) > 0 {
			rightPanel.WriteString("\n━━━ Cache Status ━━━\n")
			rightPanel.WriteString(fmt.Sprintf("💾 %d episode(s) cached\n", len(cachedEntries)))

			// Show cached episodes
			for i, cached := range cachedEntries {
				if i >= 3 { // Show max 3 episodes
					rightPanel.WriteString(fmt.Sprintf("...and %d more episodes\n", len(cachedEntries)-3))
					break
				}
				progress := int(cached.Progress * 100)
				rightPanel.WriteString(fmt.Sprintf("  Episode %d: %d%% (%s)\n",
					cached.Episode, progress, tc.FormatBytes(cached.Size)))
			}
		}

		if selectedEntry.Media.SiteURL != "" {
			rightPanel.WriteString(fmt.Sprintf("\n🔗 %s\n", hyperlink("AniList", selectedEntry.Media.SiteURL)))
		}
	}

	// Combine panels side by side
	return combinePanels(leftPanel.String(), rightPanel.String(), m.viewport.Width)
}

func (m *model) renderAnimeContent() string {
	if len(m.anime) == 0 {
		return "No anime found."
	}

	// Get selected anime for right panel
	var selectedAnime *Anime
	if m.animeCursor < len(m.anime) {
		selectedAnime = &m.anime[m.animeCursor]
	}

	// Calculate dynamic sizes
	leftWidth := m.viewport.Width / 2
	rightWidth := m.viewport.Width - leftWidth - 3

	// Image dimensions - MAXIMUM SIZE for best quality
	imageWidth := rightWidth - 4
	if imageWidth > 100 {
		imageWidth = 100
	}
	if imageWidth < 40 {
		imageWidth = 40
	}

	imageHeight := m.viewport.Height - 15
	if imageHeight > 60 {
		imageHeight = 60
	}
	if imageHeight < 30 {
		imageHeight = 30
	}

	// Left panel - list
	var leftPanel strings.Builder
	leftPanel.WriteString("📺 Anime Search Results\n\n")

	for i, a := range m.anime {
		cursor := "  "
		if m.animeCursor == i {
			cursor = "▶ "
		}

		title := a.Title.English
		if title == "" {
			title = a.Title.Romaji
		}

		// Truncate based on left width
		maxLen := leftWidth - 20
		if maxLen < 20 {
			maxLen = 20
		}
		if len(title) > maxLen {
			title = title[:maxLen-3] + "..."
		}

		score := "N/A"
		if a.Score != nil {
			score = fmt.Sprintf("%d%%", *a.Score)
		}

		// Apply pink color to selected entry
		if m.animeCursor == i {
			line := fmt.Sprintf("%s%s (⭐ %s)\n", cursor, selectedStyle.Render(title), score)
			leftPanel.WriteString(line)
		} else {
			line := fmt.Sprintf("%s%s (⭐ %s)\n", cursor, title, score)
			leftPanel.WriteString(line)
		}
	}

	// Right panel - details with image
	var rightPanel strings.Builder
	if selectedAnime != nil {
		// Render poster with dynamic sizing
		if selectedAnime.CoverImage.Large != "" {
			poster := getAnimePoster(selectedAnime.CoverImage.Large, imageWidth, imageHeight)
			rightPanel.WriteString(poster)
			rightPanel.WriteString("\n")
		}

		// Title
		title := selectedAnime.Title.English
		if title == "" {
			title = selectedAnime.Title.Romaji
		}
		wrappedTitle := wrapText(title, rightWidth)
		rightPanel.WriteString(fmt.Sprintf("📺 %s\n\n", wrappedTitle))

		// Details
		episodes := "?"
		if selectedAnime.Episodes != nil {
			episodes = fmt.Sprintf("%d", *selectedAnime.Episodes)
		}

		score := "N/A"
		if selectedAnime.Score != nil {
			score = fmt.Sprintf("%d%%", *selectedAnime.Score)
		}

		year := "?"
		if selectedAnime.SeasonYear != nil {
			year = fmt.Sprintf("%d", *selectedAnime.SeasonYear)
		}

		rightPanel.WriteString(fmt.Sprintf("Format: %s\n", selectedAnime.Format))
		rightPanel.WriteString(fmt.Sprintf("Episodes: %s\n", episodes))
		rightPanel.WriteString(fmt.Sprintf("Score: ⭐ %s\n", score))
		rightPanel.WriteString(fmt.Sprintf("Season: %s %s\n", selectedAnime.Season, year))
		rightPanel.WriteString(fmt.Sprintf("Status: %s\n\n", selectedAnime.Status))

		if selectedAnime.SiteURL != "" {
			rightPanel.WriteString(fmt.Sprintf("🔗 %s\n", hyperlink("AniList", selectedAnime.SiteURL)))
		}
	}

	// Combine panels side by side
	return combinePanels(leftPanel.String(), rightPanel.String(), m.viewport.Width)
}

func (m *model) renderTorrentContent() string {
	if len(m.torrents) == 0 {
		return "No torrents found."
	}

	perPage := 20
	visible := m.visibleTorrents(perPage)

	// Calculate dynamic sizes
	leftWidth := m.viewport.Width / 2
	rightWidth := m.viewport.Width - leftWidth - 3

	// Left panel - torrent list
	var leftPanel strings.Builder
	leftPanel.WriteString("📦 Available Torrents\n\n")

	for i, t := range visible {
		cursor := "  "
		if m.torrentCursor == i {
			cursor = "▶ "
		}

		// Show selection status
		checked := " "
		if _, ok := m.selectedTorrents[m.torrentPage*perPage+i]; ok {
			checked = "✓"
		}

		// Format size and seeders/leechers
		size := tc.FormatBytes(t.TotalSize)
		seeds := toString(t.Seeders)
		leech := toString(t.Leechers)

		// Truncate title based on left panel width
		title := t.Title
		maxTitleWidth := leftWidth - 30 // Account for size and seed info
		if len(title) > maxTitleWidth {
			title = title[:maxTitleWidth-3] + "..."
		}

		// Apply pink color to selected torrent
		if m.torrentCursor == i {
			line := fmt.Sprintf("%s[%s] %s (%s) S:%s/L:%s\n",
				cursor, checked, selectedStyle.Render(title), size, seeds, leech)
			leftPanel.WriteString(line)
		} else {
			line := fmt.Sprintf("%s[%s] %s (%s) S:%s/L:%s\n",
				cursor, checked, title, size, seeds, leech)
			leftPanel.WriteString(line)
		}
	}

	// Right panel - detailed info
	var rightPanel strings.Builder
	if len(visible) > 0 && m.torrentCursor < len(visible) {
		selectedTorrent := visible[m.torrentCursor]
		rightPanel.WriteString("📦 Torrent Details\n\n")

		// Title with word wrap
		wrappedTitle := wrapText(selectedTorrent.Title, rightWidth-2)
		rightPanel.WriteString(fmt.Sprintf("%s\n\n", wrappedTitle))

		// Basic info
		rightPanel.WriteString(fmt.Sprintf("Size: %s\n", tc.FormatBytes(selectedTorrent.TotalSize)))
		rightPanel.WriteString(fmt.Sprintf("Seeds: %s | Leechers: %s\n",
			toString(selectedTorrent.Seeders),
			toString(selectedTorrent.Leechers)))

		// Source info
		source := "Unknown"
		switch selectedTorrent.Source {
		case "nyaa":
			source = "Nyaa.si 🐱"
		case "animetosho":
			source = "AnimeTosho 📦"
		}
		rightPanel.WriteString(fmt.Sprintf("Source: %s\n", source))

		// Check if this anime has cached episodes
		if m.selectedAnime != nil {
			if cachedEntries, exists := m.cacheInfo[m.selectedAnime.ID]; exists && len(cachedEntries) > 0 {
				rightPanel.WriteString("\n━━━ Cache Status ━━━\n\n")
				rightPanel.WriteString(fmt.Sprintf("📦 %d episode(s) cached\n\n", len(cachedEntries)))

				// Show cached episodes
				for i, cached := range cachedEntries {
					if i >= 5 { // Show max 5 episodes
						rightPanel.WriteString(fmt.Sprintf("...and %d more episodes\n", len(cachedEntries)-5))
						break
					}
					progress := int(cached.Progress * 100)
					rightPanel.WriteString(fmt.Sprintf("Episode %d: %d%% (%s)\n",
						cached.Episode, progress, tc.FormatBytes(cached.Size)))
				}

				// Show total cache size
				totalSize := int64(0)
				for _, cached := range cachedEntries {
					totalSize += cached.Size
				}
				rightPanel.WriteString(fmt.Sprintf("\nTotal Cache Size: %s\n", tc.FormatBytes(totalSize)))
			} else {
				// Show that no episodes are cached
				rightPanel.WriteString("\n━━━ Cache Status ━━━\n\n")
				rightPanel.WriteString("No episodes cached\n")
			}
		}

		// If torrent is active, show download status below cache info
		if m.activeTorrent != nil && m.activeTorrent.InfoHash().String() == selectedTorrent.MagnetURI[20:60] {
			// Get torrent info with proper speed calculation
			torrentInfo, err := m.torrentClient.TorrentInfo(m.activeTorrent.InfoHash().String())
			if err == nil {
				rightPanel.WriteString("\n━━━ Download Status ━━━\n\n")

				// Progress bar
				width := rightWidth - 10
				progressBar := makeProgressBar(torrentInfo.Progress, width)
				rightPanel.WriteString(fmt.Sprintf("%s %.1f%%\n\n", progressBar, torrentInfo.Progress*100))

				// Transfer stats
				rightPanel.WriteString(fmt.Sprintf("Downloaded: %s\n",
					tc.FormatBytes(torrentInfo.Size.Downloaded)))
				rightPanel.WriteString(fmt.Sprintf("Uploaded: %s\n",
					tc.FormatBytes(torrentInfo.Size.Uploaded)))
				rightPanel.WriteString(fmt.Sprintf("Download Speed: %s\n",
					tc.FormatSpeed(torrentInfo.Speed.Down)))
				rightPanel.WriteString(fmt.Sprintf("Upload Speed: %s\n",
					tc.FormatSpeed(torrentInfo.Speed.Up)))

				// Peer info
				rightPanel.WriteString(fmt.Sprintf("Connected Peers: %d\n", torrentInfo.Peers.Wires))
				rightPanel.WriteString(fmt.Sprintf("Connected Seeders: %d\n", torrentInfo.Peers.Seeders))
				rightPanel.WriteString(fmt.Sprintf("Connected Leechers: %d\n", torrentInfo.Peers.Leechers))

				// Piece info
				if m.activeTorrent.Info() != nil {
					rightPanel.WriteString(fmt.Sprintf("Pieces: %d/%d\n",
						torrentInfo.Size.Downloaded/int64(m.activeTorrent.Info().PieceLength),
						torrentInfo.Pieces.Total))
				}

				// Time estimates
				if torrentInfo.Time.Remaining > 0 {
					rightPanel.WriteString(fmt.Sprintf("Time Remaining: %s\n", formatDuration(torrentInfo.Time.Remaining)))
				}
				if torrentInfo.Time.Elapsed > 0 {
					rightPanel.WriteString(fmt.Sprintf("Time Elapsed: %s\n", formatDuration(torrentInfo.Time.Elapsed)))
				}

				// Stream status
				if m.streamURL != "" {
					rightPanel.WriteString("\n🎬 Streaming Active\n")
					rightPanel.WriteString(fmt.Sprintf("URL: %s\n", m.streamURL))
				}

				// Video files
				videos := tc.GetAllVideoFiles(m.activeTorrent)
				if len(videos) > 0 {
					rightPanel.WriteString("\n📺 Video Files:\n")
					for i, v := range videos {
						if i >= 5 { // Show max 5 files
							rightPanel.WriteString(fmt.Sprintf("...and %d more files\n", len(videos)-5))
							break
						}
						name := v.DisplayPath()
						if len(name) > rightWidth-6 {
							name = name[:rightWidth-9] + "..."
						}
						rightPanel.WriteString(fmt.Sprintf("%d. %s\n", i+1, name))
					}
				}
			}
		}

		// Add magnet link
		if selectedTorrent.MagnetURI != "" {
			rightPanel.WriteString(fmt.Sprintf("\n🧲 %s\n",
				hyperlink("Magnet Link", selectedTorrent.MagnetURI)))
		}
	}

	return combinePanels(leftPanel.String(), rightPanel.String(), m.viewport.Width)
}

// Helper function to create a progress bar
func makeProgressBar(progress float64, width int) string {
	filled := int(progress * float64(width))
	if filled > width {
		filled = width
	}
	empty := width - filled
	return fmt.Sprintf("[%s%s]",
		strings.Repeat("=", filled),
		strings.Repeat("-", empty))
}

// Helper function to format duration in seconds to human readable format
func formatDuration(seconds int64) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	} else if seconds < 3600 {
		return fmt.Sprintf("%dm %ds", seconds/60, seconds%60)
	} else {
		hours := seconds / 3600
		minutes := (seconds % 3600) / 60
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
}
