package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

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

	// Anything logged would draw over the full-screen UI, so log to a file
	if logFile := openLogFile(); logFile != nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}

	m := initialModel()
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	// Flush piece completion state so partial downloads resume next time
	if m.torrentClient != nil {
		m.torrentClient.Close()
	}
	if m.presence != nil {
		m.presence.Close()
	}
	if m.room != nil {
		m.room.Close()
		m.roomPlayer.Close()
	}
	if err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}

// openLogFile opens <user cache dir>/sakuhaku/sakuhaku.log for appending
func openLogFile() *os.File {
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil
	}
	dir = filepath.Join(dir, "sakuhaku")
	if os.MkdirAll(dir, 0o755) != nil {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(dir, "sakuhaku.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil
	}
	return f
}
