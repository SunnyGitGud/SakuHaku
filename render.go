package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	tc "github.com/sunnygitgud/sakuhaku/torrentclient"
)

var (
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	barFullStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	barEmptyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
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
	case ModeDownloads:
		return m.renderDownloadsContent()
	}
	return ""
}

func animeTitle(a *Anime) string {
	if a.Title.English != "" {
		return a.Title.English
	}
	return a.Title.Romaji
}

// splitWidths returns the widths of the list and detail panels
func (m *model) splitWidths() (int, int) {
	left := m.viewport.Width / 2
	right := m.viewport.Width - left - 3 // " │ "
	return left, max(0, right)
}

// renderSplit lays out a list on the left and a detail panel on the right. The
// output is exactly as tall as the viewport: the list scrolls within its own
// window so the poster on the right stays pinned in place.
func (m *model) renderSplit(header, items []string, cursor int, right string) string {
	height := max(1, m.viewport.Height)
	leftWidth, rightWidth := m.splitWidths()

	rows := max(1, height-len(header))
	if cursor < m.listOffset {
		m.listOffset = cursor
	}
	if cursor >= m.listOffset+rows {
		m.listOffset = cursor - rows + 1
	}
	m.listOffset = max(0, min(m.listOffset, len(items)-rows))

	left := append([]string{}, header...)
	for i := m.listOffset; i < len(items) && i < m.listOffset+rows; i++ {
		left = append(left, items[i])
	}
	rightLines := strings.Split(right, "\n")

	var sb strings.Builder
	for i := 0; i < height; i++ {
		l := ""
		if i < len(left) {
			l = ansi.Truncate(left[i], leftWidth, "…")
		}
		sb.WriteString(l)
		sb.WriteString(strings.Repeat(" ", max(0, leftWidth-ansi.StringWidth(l))))
		sb.WriteString(dimStyle.Render(" │ "))
		if i < len(rightLines) {
			sb.WriteString(ansi.Truncate(rightLines[i], rightWidth, ""))
		}
		if i < height-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// listItem renders one list row, highlighting the selected one
func listItem(selected bool, text string) string {
	if selected {
		return selectedStyle.Render("▶ " + text)
	}
	return "  " + text
}

// detailPanel puts the poster above the given details, giving the poster all
// the height the details don't need
func (m *model) detailPanel(a *Anime, details []string, width int) string {
	var textLines []string
	for _, d := range details {
		textLines = append(textLines, strings.Split(d, "\n")...)
	}

	posterRows := m.viewport.Height - len(textLines) - 1
	url := a.PosterURL()
	if posterRows < 6 || width < 8 {
		return strings.Join(textLines, "\n")
	}

	m.wantPosters = append(m.wantPosters, url)
	poster := getAnimePoster(url, width, posterRows)

	// Center the poster horizontally in the panel
	pad := (width - lipgloss.Width(poster)) / 2
	if pad > 0 {
		indent := strings.Repeat(" ", pad)
		lines := strings.Split(poster, "\n")
		for i := range lines {
			lines[i] = indent + lines[i]
		}
		poster = strings.Join(lines, "\n")
	}
	return poster + "\n\n" + strings.Join(textLines, "\n")
}

// prefetchNeighbours queues the posters around the cursor so scrolling is instant
func (m *model) prefetchNeighbours(urls func(i int) string, cursor, n int) {
	for _, i := range []int{cursor + 1, cursor - 1, cursor + 2} {
		if i >= 0 && i < n {
			m.wantPosters = append(m.wantPosters, urls(i))
		}
	}
}

func (m *model) renderUserListContent() string {
	if len(m.userEntries) == 0 {
		return "No anime found."
	}

	_, rightWidth := m.splitWidths()

	var header []string
	listTitle := m.currentListType.String()
	if m.username != "" && (m.currentListType == ListCurrentlyWatching || m.currentListType == ListPlanToWatch) {
		header = append(header, fmt.Sprintf("👤 %s's %s", m.username, listTitle), "")
	} else {
		header = append(header, fmt.Sprintf("📺 %s", listTitle), "")
	}

	items := make([]string, len(m.userEntries))
	for i := range m.userEntries {
		entry := &m.userEntries[i]
		var info string
		if m.currentListType == ListCurrentlyWatching || m.currentListType == ListPlanToWatch {
			episodes := "?"
			if entry.Media.Episodes != nil {
				episodes = fmt.Sprintf("%d", *entry.Media.Episodes)
			}
			info = fmt.Sprintf(" (%d/%s)", entry.Progress, episodes)
		} else if entry.Media.Score != nil {
			info = fmt.Sprintf(" ⭐%d%%", *entry.Media.Score)
		}
		items[i] = listItem(m.userEntryCursor == i, animeTitle(&entry.Media)+info)
	}

	var right string
	if m.userEntryCursor < len(m.userEntries) {
		selected := &m.userEntries[m.userEntryCursor]
		media := &selected.Media

		var details []string
		details = append(details, "📺 "+wrapText(animeTitle(media), rightWidth-3), "")

		episodes := "?"
		if media.Episodes != nil {
			episodes = fmt.Sprintf("%d", *media.Episodes)
		}

		if m.currentListType == ListCurrentlyWatching || m.currentListType == ListPlanToWatch {
			details = append(details, fmt.Sprintf("Progress: %d/%s", selected.Progress, episodes))
			if selected.Score > 0 {
				details = append(details, fmt.Sprintf("Your Score: ⭐ %.1f/10", selected.Score))
			}
			details = append(details, "Status: "+selected.Status)
			if selected.UpdatedAt > 0 {
				details = append(details, "Updated: "+formatRelativeTime(selected.UpdatedAt))
			}
		}

		score := "N/A"
		if media.Score != nil {
			score = fmt.Sprintf("%d%%", *media.Score)
		}
		line := fmt.Sprintf("%s · %s eps · ⭐ %s", media.Format, episodes, score)
		if media.Season != "" {
			year := ""
			if media.SeasonYear != nil {
				year = fmt.Sprintf(" %d", *media.SeasonYear)
			}
			line += fmt.Sprintf(" · %s%s", media.Season, year)
		}
		details = append(details, line)

		if media.SiteURL != "" {
			details = append(details, "🔗 "+hyperlink("AniList", media.SiteURL))
		}

		right = m.detailPanel(media, details, rightWidth)
		m.prefetchNeighbours(func(i int) string { return m.userEntries[i].Media.PosterURL() },
			m.userEntryCursor, len(m.userEntries))
	}

	return m.renderSplit(header, items, m.userEntryCursor, right)
}

func (m *model) renderAnimeContent() string {
	if len(m.anime) == 0 {
		return "No anime found."
	}

	_, rightWidth := m.splitWidths()
	header := []string{"📺 Anime Search Results", ""}

	items := make([]string, len(m.anime))
	for i := range m.anime {
		a := &m.anime[i]
		score := "N/A"
		if a.Score != nil {
			score = fmt.Sprintf("%d%%", *a.Score)
		}
		items[i] = listItem(m.animeCursor == i, fmt.Sprintf("%s (⭐ %s)", animeTitle(a), score))
	}

	var right string
	if m.animeCursor < len(m.anime) {
		a := &m.anime[m.animeCursor]

		episodes := "?"
		if a.Episodes != nil {
			episodes = fmt.Sprintf("%d", *a.Episodes)
		}
		score := "N/A"
		if a.Score != nil {
			score = fmt.Sprintf("%d%%", *a.Score)
		}
		year := "?"
		if a.SeasonYear != nil {
			year = fmt.Sprintf("%d", *a.SeasonYear)
		}

		details := []string{
			"📺 " + wrapText(animeTitle(a), rightWidth-3),
			"",
			fmt.Sprintf("%s · %s eps · ⭐ %s", a.Format, episodes, score),
			fmt.Sprintf("%s %s · %s", a.Season, year, a.Status),
		}
		if a.SiteURL != "" {
			details = append(details, "🔗 "+hyperlink("AniList", a.SiteURL))
		}

		right = m.detailPanel(a, details, rightWidth)
		m.prefetchNeighbours(func(i int) string { return m.anime[i].PosterURL() },
			m.animeCursor, len(m.anime))
	}

	return m.renderSplit(header, items, m.animeCursor, right)
}

func (m *model) renderTorrentContent() string {
	perPage := 20
	visible := m.visibleTorrents(perPage)

	if len(visible) == 0 {
		return "No torrents found for this anime."
	}

	var sb strings.Builder

	if m.selectedAnime != nil {
		sb.WriteString(fmt.Sprintf("🎬 Torrents for: %s\n\n", animeTitle(m.selectedAnime)))
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

		title := ansi.Truncate(t.Title, max(10, m.viewport.Width-12), "…")
		if m.torrentCursor == i {
			title = selectedStyle.Render(title)
		}

		line := fmt.Sprintf("%s [%s] %s %s\n   💾 %s | 🌱 %s | 🧲 %s | 📤 %s\n\n",
			cursor, checked, sourceBadge, title,
			formatBytes(t.TotalSize),
			toString(t.Seeders),
			toString(t.Leechers),
			hyperlink("magnet", t.MagnetURI))
		sb.WriteString(line)
	}
	return sb.String()
}

// downloadsHeaderLines is how many lines sit above the first download entry
const downloadsHeaderLines = 3

// downloadEntryLines is how many lines each download entry takes
const downloadEntryLines = 4

func (m *model) renderDownloadsContent() string {
	width := max(20, m.viewport.Width)

	var down, up float64
	for _, d := range m.downloads {
		down += d.DownRate
		up += d.UpRate
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("⬇ Downloads · %d torrent(s) · ↓ %s  ↑ %s\n",
		len(m.downloads), tc.FormatSpeed(int64(down)), tc.FormatSpeed(int64(up))))
	if m.torrentClient != nil {
		sb.WriteString(dimStyle.Render("Saving to: "+m.torrentClient.DownloadDir) + "\n")
	} else {
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	if len(m.downloads) == 0 {
		sb.WriteString(dimStyle.Render("Nothing here yet. Press Enter on a torrent to stream it or d to download it."))
		return sb.String()
	}

	barWidth := min(40, max(10, width-36))
	for i, d := range m.downloads {
		name := ansi.Truncate(d.Name, width-4, "…")
		if i == m.downloadCursor {
			sb.WriteString(selectedStyle.Render("▶ "+name) + "\n")
		} else {
			sb.WriteString("  " + name + "\n")
		}

		sizeInfo := "size unknown"
		if d.Size > 0 {
			sizeInfo = fmt.Sprintf("%s / %s", tc.FormatBytes(d.Completed), tc.FormatBytes(d.Size))
		}
		sb.WriteString(fmt.Sprintf("  %s %5.1f%%  %s\n", progressBar(d.Progress, barWidth), d.Progress*100, sizeInfo))

		parts := []string{stateStyle(d.State).Render("● " + d.State.String())}
		if d.State != tc.StateCompleted && d.State != tc.StatePaused {
			parts = append(parts, fmt.Sprintf("↓ %s ↑ %s", tc.FormatSpeed(int64(d.DownRate)), tc.FormatSpeed(int64(d.UpRate))))
		}
		parts = append(parts, fmt.Sprintf("%d peers (%d seeds)", d.Peers, d.Seeders))
		if d.State == tc.StateDownloading {
			parts = append(parts, "ETA "+tc.FormatDuration(d.ETA))
		}
		if d.Mode == tc.ModeStream && d.State != tc.StateCompleted {
			parts = append(parts, dimStyle.Render("stream only, d to keep"))
		}
		sb.WriteString(ansi.Truncate("  "+strings.Join(parts, " · "), width, "…") + "\n\n")
	}
	return sb.String()
}

func progressBar(p float64, width int) string {
	p = max(0, min(1, p))
	full := int(p*float64(width) + 0.5)
	return barFullStyle.Render(strings.Repeat("█", full)) +
		barEmptyStyle.Render(strings.Repeat("░", width-full))
}

func stateStyle(s tc.State) lipgloss.Style {
	color := "243"
	switch s {
	case tc.StateDownloading:
		color = "42"
	case tc.StateStreaming:
		color = "39"
	case tc.StatePaused:
		color = "214"
	case tc.StateStalled:
		color = "203"
	case tc.StateCompleted:
		color = "42"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
}
