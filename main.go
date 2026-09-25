package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joho/godotenv"
)

func main() {
	// A .env file is optional; real environment variables take precedence
	_ = godotenv.Load()

	if err := parseFlags(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

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
