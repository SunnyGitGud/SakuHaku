package torrentclient

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	InternalStreamPort = "8888"
	ClientName         = "anilist-torrent-browser"
)

// Torrent Client

type TorrentClient struct {
	Name        string
	DataDir     string
	DownloadDir string
	Seed        bool
	NoServer    bool
	Port        string
	TorrentPort int
	Client      *torrent.Client
	Server      *http.Server
	Torrents    []*torrent.Torrent
	DisableIPV6 bool
}

// NewTorrentClient creates a new torrent client instance
func NewTorrentClient(name string, port string) *TorrentClient {
	return &TorrentClient{
		Name:     name,
		Port:     port,
		NoServer: false,
		Seed:     true,
	}
}

func (c *TorrentClient) SetDownloadDir(dir string) {
	c.DownloadDir = dir
}

// SetServerOFF turns off the internal HTTP streaming server
func (c *TorrentClient) SetServerOFF(off bool) {
	c.NoServer = off
}

// Init initializes the torrent client
func (c *TorrentClient) Init() error {
	cfg := torrent.NewDefaultClientConfig()
	s, err := c.getStorage()
	if err != nil {
		return err
	}

	cfg.DisableIPv6 = c.DisableIPV6

	// Get open port
	if c.TorrentPort < 5 {
		port, err := GetFreePort()
		if err != nil {
			c.TorrentPort = 42069
		} else {
			c.TorrentPort = port
		}
	}

	if c.Port == InternalStreamPort {
		p, err := GetFreePortString()
		if err != nil {
			c.Port = InternalStreamPort
		} else {
			c.Port = p
		}
	}

	cfg.ListenPort = c.TorrentPort
	c.DataDir = s

	var stor storage.ClientImpl
	if c.DownloadDir == "" {
		c.DownloadDir = s
		stor = storage.NewFileByInfoHash(c.DataDir)
	} else {
		stor, err = getMetadataDir(c.DataDir, c.DownloadDir)
		if err != nil {
			return err
		}
	}

	cfg.DefaultStorage = stor

	client, err := torrent.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("error creating torrent client: %v", err)
	}

	if !c.NoServer {
		c.StartServer()
	}

	c.Client = client
	return nil
}

// getMetadataDir sets up metadata and download directories
func getMetadataDir(metadataDir, downloadDir string) (storage.ClientImpl, error) {
	mstor, err := storage.NewDefaultPieceCompletionForDir(metadataDir)
	if err != nil {
		log.Println("unable to set download dir, falling back to data dir:", err)
		return storage.NewMMap(downloadDir), nil
	}

	tstor := storage.NewMMapWithCompletion(downloadDir, mstor)
	if err != nil {
		return nil, err
	}

	return tstor, nil
}

// getStorage creates and returns the storage directory path
func (c *TorrentClient) getStorage() (string, error) {
	s, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("couldn't get user cache directory: %v", err)
	}

	p := filepath.Join(s, c.Name)
	if p == "" || c.Name == "" {
		return "", fmt.Errorf("couldn't construct client path: empty path or project name")
	}

	err = os.MkdirAll(p, 0o755)
	if err != nil {
		return "", fmt.Errorf("couldn't create project directory: %v", err)
	}

	_, err = os.Stat(p)
	if err == nil {
		return p, nil
	}
	return "", err
}

// ----- Adding Torrents -----

// AddTorrent adds a torrent from magnet, URL, or file
func (c *TorrentClient) AddTorrent(tor string) (*torrent.Torrent, error) {
	if strings.HasPrefix(tor, "magnet") {
		return c.AddMagnet(tor)
	} else if strings.Contains(tor, "http") {
		return c.AddTorrentURL(tor)
	} else {
		return c.AddTorrentFile(tor)
	}
}

// AddMagnet adds a torrent from a magnet link
func (c *TorrentClient) AddMagnet(magnet string) (*torrent.Torrent, error) {
	t, err := c.Client.AddMagnet(magnet)
	if err != nil {
		return nil, err
	}
	<-t.GotInfo()
	return t, nil
}

// AddTorrentFile adds a torrent from a file path
func (c *TorrentClient) AddTorrentFile(file string) (*torrent.Torrent, error) {
	t, err := c.Client.AddTorrentFromFile(file)
	if err != nil {
		return nil, err
	}
	<-t.GotInfo()
	return t, nil
}

// AddTorrentURL adds a torrent from a URL
func (c *TorrentClient) AddTorrentURL(url string) (*torrent.Torrent, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	fname := path.Base(url)
	tmp := os.TempDir()
	fpath := filepath.Join(tmp, fname)

	file, err := os.Create(fpath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return nil, err
	}

	t, err := c.Client.AddTorrentFromFile(file.Name())
	if err != nil {
		return nil, err
	}
	<-t.GotInfo()
	return t, nil
}

// DownloadTorrent adds a torrent and marks it for complete download
func (c *TorrentClient) DownloadTorrent(torrent string) error {
	t, err := c.AddTorrent(torrent)
	if err != nil {
		return err
	}
	t.DownloadAll()
	return nil
}

// ----- HTTP Streaming Server -----

// StartServer starts the HTTP streaming server
func (c *TorrentClient) StartServer() {
	port := fmt.Sprintf(":%s", c.Port)
	c.Server = &http.Server{Addr: port}
	http.HandleFunc("/stream", c.handler)

	go func() {
		if err := c.Server.ListenAndServe(); err != nil {
			if err == http.ErrServerClosed {
				return
			} else {
				log.Fatal(err)
			}
		}
	}()
}

// handler handles HTTP streaming requests
func (c *TorrentClient) handler(w http.ResponseWriter, r *http.Request) {
	ts := c.Client.Torrents()
	queries := r.URL.Query()
	hash := queries.Get("hash")
	fpath := queries.Get("filepath")

	// Clean input
	hash = strings.TrimSpace(strings.ReplaceAll(hash, "\n", ""))
	fpath = strings.TrimSpace(strings.ReplaceAll(fpath, "\n", ""))

	if hash == "" {
		http.Error(w, http.StatusText(400), http.StatusBadRequest)
		log.Println("server handler: hash query is empty")
		return
	}

	var targetTorrent *torrent.Torrent
	for _, t := range ts {
		<-t.GotInfo()
		if t.InfoHash().String() == hash {
			targetTorrent = t
			break
		}
	}

	if targetTorrent == nil {
		http.Error(w, http.StatusText(500), http.StatusInternalServerError)
		log.Println("server handler: couldn't find torrent by infohash")
		return
	}

	fileCount := len(targetTorrent.Files())
	var targetFile *torrent.File

	if fileCount == 1 {
		targetFile = targetTorrent.Files()[0]
	} else if fpath != "" && fileCount > 1 {
		for _, f := range targetTorrent.Files() {
			if f.DisplayPath() == fpath {
				targetFile = f
				break
			}
		}
	}

	if targetFile == nil {
		http.Error(w, http.StatusText(500), http.StatusInternalServerError)
		log.Println("server handler: couldn't find torrent file requested")
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(w, r, targetFile.DisplayPath(), time.Unix(targetFile.Torrent().Metainfo().CreationDate, 0), targetFile.NewReader())
}

// ServeTorrent generates a streaming link for a torrent
func (c *TorrentClient) ServeTorrent(t *torrent.Torrent) string {
	mh := t.InfoHash().String()
	return fmt.Sprintf("http://localhost:%s/stream?hash=%s", c.Port, mh)
}

// ServeTorrentEpisode generates a streaming link for a specific file
func (c *TorrentClient) ServeTorrentEpisode(t *torrent.Torrent, filePath string) string {
	mh := t.InfoHash().String()
	return fmt.Sprintf("http://localhost:%s/stream?hash=%s&filepath=%s", c.Port, mh, filePath)
}

// ----- Torrent Management -----

// ShowTorrents returns all loaded torrents
func (c *TorrentClient) ShowTorrents() []*torrent.Torrent {
	return c.Client.Torrents()
}

// FindByInfoHash finds a torrent by its info hash
func (c *TorrentClient) FindByInfoHash(infoHash string) (*torrent.Torrent, error) {
	torrents := c.Client.Torrents()
	for _, t := range torrents {
		if t.InfoHash().AsString() == infoHash {
			return t, nil
		}
	}
	return nil, fmt.Errorf("no torrents match info hash: %v", infoHash)
}

// DropTorrent removes a torrent from the client
func (c *TorrentClient) DropTorrent(t *torrent.Torrent) {
	t.Drop()
}

// Close stops the client and closes all connections
func (c *TorrentClient) Close() []error {
	return c.Client.Close()
}

// ----- Utility Functions -----

// IsVideoFile checks if a file is a video
func IsVideoFile(f *torrent.File) bool {
	ext := path.Ext(f.Path())
	switch ext {
	case ".mp4", ".mkv", ".avi", ".avif", ".av1", ".mov", ".flv", ".f4v", ".webm", ".wmv", ".mpeg", ".mpg", ".mlv", ".hevc", ".flac", ".flic":
		return true
	default:
		return false
	}
}

// GetFreePort returns an available port
func GetFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// GetFreePortString returns an available port as a string
func GetFreePortString() (string, error) {
	port, err := GetFreePort()
	if err != nil {
		return InternalStreamPort, err
	}
	return fmt.Sprintf("%d", port), nil
}

// Bubble Tea Integration

// TorrentAddedMsg is sent when a torrent is successfully added
type TorrentAddedMsg struct {
	Torrent *torrent.Torrent
	Error   error
}

// TorrentProgressMsg is sent to update download progress
type TorrentProgressMsg struct {
	InfoHash string
	Progress float64
	Stats    torrent.TorrentStats
}

// AddTorrentAsync adds a torrent asynchronously and returns a Bubble Tea command
func (c *TorrentClient) AddTorrentAsync(magnetURI string) tea.Cmd {
	return func() tea.Msg {
		t, err := c.AddMagnet(magnetURI)
		return TorrentAddedMsg{
			Torrent: t,
			Error:   err,
		}
	}
}

// GetTorrentProgress returns the download progress for a torrent
func GetTorrentProgress(t *torrent.Torrent) tea.Cmd {
	return func() tea.Msg {
		stats := t.Stats()
		progress := float64(stats.BytesReadData.Int64()) / float64(t.Length())

		return TorrentProgressMsg{
			InfoHash: t.InfoHash().String(),
			Progress: progress * 100,
			Stats:    stats,
		}
	}
}

// TickProgress returns a command that periodically updates torrent progress
func TickProgress() tea.Cmd {
	return tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
		return TorrentProgressMsg{}
	})
}

// Helper Functions for Display

// FormatBytes formats bytes into human-readable format
func FormatBytes(bytes int64) string {
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

// FormatSpeed formats download speed
func FormatSpeed(bytesPerSecond int64) string {
	return FormatBytes(bytesPerSecond) + "/s"
}

// GetLargestVideoFile returns the largest video file from a torrent
func GetLargestVideoFile(t *torrent.Torrent) *torrent.File {
	var largest *torrent.File
	var largestSize int64

	for _, f := range t.Files() {
		if IsVideoFile(f) && f.Length() > largestSize {
			largest = f
			largestSize = f.Length()
		}
	}

	return largest
}

// GetAllVideoFiles returns all video files from a torrent
func GetAllVideoFiles(t *torrent.Torrent) []*torrent.File {
	var videos []*torrent.File
	for _, f := range t.Files() {
		if IsVideoFile(f) {
			videos = append(videos, f)
		}
	}
	return videos
}

// GetTorrentInfo returns formatted information about a torrent
func GetTorrentInfo(t *torrent.Torrent) string {
	stats := t.Stats()
	progress := float64(stats.BytesReadData.Int64()) / float64(t.Length()) * 100

	return fmt.Sprintf(
		"Name: %s\nSize: %s\nProgress: %.1f%%\nDown Speed: %s\nUp Speed: %s\nPeers: %d\nSeeders: %d",
		t.Name(),
		FormatBytes(t.Length()),
		progress,
		FormatSpeed(stats.BytesRead.Int64()),
		FormatSpeed(stats.BytesWrittenData.Int64()),
		stats.ActivePeers,
		stats.ConnectedSeeders,
	)
}
