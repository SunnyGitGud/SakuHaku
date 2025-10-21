package main

import (
	"fmt"
	"strings"
	"time"
)

// Format Unix timestamp to readable date
func formatDate(timestamp int64) string {
	if timestamp == 0 {
		return "Unknown"
	}
	t := time.Unix(timestamp, 0)
	return t.Format("Jan 02, 2006")
}

// Format timestamp to relative time (e.g., "2 days ago")
func formatRelativeTime(timestamp int64) string {
	if timestamp == 0 {
		return "Unknown"
	}

	t := time.Unix(timestamp, 0)
	duration := time.Since(t)

	days := int(duration.Hours() / 24)
	hours := int(duration.Hours())
	minutes := int(duration.Minutes())

	switch {
	case days > 30:
		months := days / 30
		if months == 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", months)
	case days > 0:
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	case hours > 0:
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case minutes > 0:
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	default:
		return "Just now"
	}
}

func formatTime(ts int) string {
	if ts <= 0 {
		return "Unknown"
	}

	t := time.Unix(int64(ts), 0)
	now := time.Now()

	// If timestamp is in the future, just show the formatted date.
	if t.After(now) {
		return t.Format("Jan 2, 2006 15:04")
	}

	d := now.Sub(t)

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2, 2006")
	}
}

// Format bytes (moved from main.go for organization)
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// Convert any value to string (moved from main.go)
func toString(v any) string {
	switch val := v.(type) {
	case float64:
		return fmt.Sprintf("%.0f", val)
	case int:
		return fmt.Sprintf("%d", val)
	case string:
		return val
	case nil:
		return "?"
	default:
		return fmt.Sprintf("%v", val)
	}
}

// Create hyperlink (moved from main.go)
func hyperlink(text, link string) string {
	if link == "" {
		return text
	}
	return fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", link, text)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func combinePanels(left, right string, totalWidth int) string {
	leftLines := strings.Split(left, "\n")
	rightLines := strings.Split(right, "\n")

	// Calculate panel widths
	leftWidth := totalWidth / 2
	// rightWidth := totalWidth - leftWidth - 3 // Account for separator

	var result strings.Builder

	maxLines := max(len(leftLines), len(rightLines))
	for i := 0; i < maxLines; i++ {
		// Left panel
		leftLine := ""
		if i < len(leftLines) {
			leftLine = leftLines[i]
		}

		// Remove ANSI codes for length calculation
		leftDisplayLen := visualLength(leftLine)

		// Truncate if too long
		if leftDisplayLen > leftWidth {
			leftLine = truncateToWidth(leftLine, leftWidth-3) + "..."
			leftDisplayLen = leftWidth
		}

		// Pad to width
		padding := leftWidth - leftDisplayLen
		if padding < 0 {
			padding = 0
		}

		result.WriteString(leftLine)
		result.WriteString(strings.Repeat(" ", padding))
		result.WriteString(" │ ")

		// Right panel
		if i < len(rightLines) {
			result.WriteString(rightLines[i])
		}
		result.WriteString("\n")
	}

	return result.String()
}

// Calculate visual length (excluding ANSI codes)
func visualLength(s string) int {
	inEscape := false
	length := 0

	for _, r := range s {
		if r == '\033' {
			inEscape = true
		} else if inEscape {
			if r == 'm' || r == '\\' {
				inEscape = false
			}
		} else {
			length++
		}
	}

	return length
}

// Truncate string to visual width
func truncateToWidth(s string, width int) string {
	inEscape := false
	length := 0
	result := strings.Builder{}

	for _, r := range s {
		if r == '\033' {
			inEscape = true
			result.WriteRune(r)
		} else if inEscape {
			result.WriteRune(r)
			if r == 'm' || r == '\\' {
				inEscape = false
			}
		} else {
			if length >= width {
				break
			}
			result.WriteRune(r)
			length++
		}
	}

	return result.String()
}

func wrapText(text string, width int) string {
	if len(text) <= width {
		return text
	}

	var result strings.Builder
	words := strings.Fields(text)
	lineLen := 0

	for i, word := range words {
		wordLen := len(word)

		if lineLen+wordLen+1 > width {
			result.WriteString("\n")
			lineLen = 0
		} else if i > 0 {
			result.WriteString(" ")
			lineLen++
		}

		result.WriteString(word)
		lineLen += wordLen
	}

	return result.String()
}
