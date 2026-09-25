package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joho/godotenv"
)

func init() {
	err := godotenv.Load()
	if err != nil {
		fmt.Printf("Warning: Could not load .env file: %v\n", err)
	} else {
		fmt.Println("✓ .env file loaded successfully")
	}
}

func main() {
	m := initialModel()
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	// Flush piece completion state so partial downloads resume next time
	if m.torrentClient != nil {
		m.torrentClient.Close()
	}
	if err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
