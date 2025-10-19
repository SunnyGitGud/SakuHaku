package main

import (
	"fmt"
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
