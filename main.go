package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"os"
)

func hyperlink(text, link string) string {
	if link == "" {
		return text
	}
	return fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", link, text)
}

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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
