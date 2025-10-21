package main

import (
	"fmt"
	"strings"
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

		line := fmt.Sprintf("%s%s%s\n", cursor, title, info)
		leftPanel.WriteString(line)
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

		line := fmt.Sprintf("%s%s (⭐ %s)\n", cursor, title, score)
		leftPanel.WriteString(line)
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
	perPage := 20
	visible := m.visibleTorrents(perPage)

	if len(visible) == 0 {
		return "No torrents found for this anime."
	}

	var sb strings.Builder

	if m.selectedAnime != nil {
		title := m.selectedAnime.Title.English
		if title == "" {
			title = m.selectedAnime.Title.Romaji
		}
		sb.WriteString(fmt.Sprintf("🎬 Torrents for: %s\n\n", title))
	}

	for i, t := range visible {
		cursor := " "
		if m.torrentCursor == i {
			cursor = ">"
		}

		actualIndex := m.torrentPage*perPage + i
		checked := " "
		if _, ok := m.selectedTorrents[actualIndex]; ok {
			checked = "x"
		}

		// Show source badge
		sourceBadge := "📦"
		if t.Source == "nyaa" {
			sourceBadge = "🐱"
		}

		line := fmt.Sprintf("%s [%s] %s %s\n   💾 %s | 🌱 %s | 🧲 %s | 📤 %s\n\n",
			cursor, checked, sourceBadge, t.Title,
			formatBytes(t.TotalSize),
			toString(t.Seeders),
			toString(t.Leechers),
			hyperlink("magnet", t.MagnetURI))
		sb.WriteString(line)
	}
	return sb.String()
}
