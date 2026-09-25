package torrentclient

import (
	"context"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	InternalStreamPort = "8888"
	ClientName         = "anilist-torrent-browser"

	// MetadataTimeout bounds how long we wait for a magnet's metadata before giving up
	MetadataTimeout = 2 * time.Minute
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
	// HTTPProxy, when set, is used for HTTP tracker announces
	HTTPProxy func(*http.Request) (*url.URL, error)

	// Download manager state, see manager.go
	mu      sync.Mutex
	tracked map[metainfo.Hash]*tracked
	order   []metainfo.Hash
}

// NewTorrentClient creates a new torrent client instance
func NewTorrentClient(name string, port string) *TorrentClient {
	return &TorrentClient{
		Name:     name,
		Port:     port,
		NoServer: false,
		Seed:     true,
		tracked:  make(map[metainfo.Hash]*tracked),
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
	if c.HTTPProxy != nil {
		cfg.HTTPProxy = c.HTTPProxy
	}

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
	if err != nil && !cfg.DisableIPv6 {
		// Systems with IPv6 turned off (some WSL/Docker/Linux setups) fail
		// to open the IPv6 listener; carry on with IPv4 only
		log.Println("torrent client: retrying without IPv6:", err)
		cfg.DisableIPv6 = true
		c.DisableIPV6 = true
		client, err = torrent.NewClient(cfg)
	}
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

	// Plain file storage keeps downloads as normal files under
	// downloadDir/<torrent name>, which the download manager relies on when
	// deleting data, and avoids mmap faults on large files.
	return storage.NewFileWithCompletion(downloadDir, mstor), nil
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

// Adding Torrents

// AddTorrent adds a torrent from magnet, URL, or file and waits for its metadata
func (c *TorrentClient) AddTorrent(tor string) (*torrent.Torrent, error) {
	t, err := c.addTorrentNoWait(tor)
	if err != nil {
		return nil, err
	}
	return c.waitForInfo(t)
}

// addTorrentNoWait registers a torrent with the client without waiting for metadata
func (c *TorrentClient) addTorrentNoWait(tor string) (*torrent.Torrent, error) {
	switch {
	case strings.HasPrefix(tor, "magnet"):
		return c.Client.AddMagnet(tor)
	case strings.HasPrefix(tor, "http://"), strings.HasPrefix(tor, "https://"):
		return c.addTorrentURLNoWait(tor)
	default:
		return c.Client.AddTorrentFromFile(tor)
	}
}

// waitForInfo blocks until metadata arrives, dropping the torrent on timeout
func (c *TorrentClient) waitForInfo(t *torrent.Torrent) (*torrent.Torrent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), MetadataTimeout)
	defer cancel()
	select {
	case <-t.GotInfo():
		return t, nil
	case <-t.Closed():
		return nil, fmt.Errorf("torrent was removed before metadata arrived")
	case <-ctx.Done():
		c.forget(t.InfoHash())
		t.Drop()
		return nil, fmt.Errorf("timed out after %s waiting for torrent metadata (no peers?)", MetadataTimeout)
	}
}

// AddMagnet adds a torrent from a magnet link
func (c *TorrentClient) AddMagnet(magnet string) (*torrent.Torrent, error) {
	return c.AddTorrent(magnet)
}

// AddTorrentFile adds a torrent from a file path
func (c *TorrentClient) AddTorrentFile(file string) (*torrent.Torrent, error) {
	return c.AddTorrent(file)
}

// AddTorrentURL adds a torrent from a URL
func (c *TorrentClient) AddTorrentURL(url string) (*torrent.Torrent, error) {
	return c.AddTorrent(url)
}

func (c *TorrentClient) addTorrentURLNoWait(u string) (*torrent.Torrent, error) {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching torrent file: %s", resp.Status)
	}

	mi, err := metainfo.Load(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parsing torrent file: %w", err)
	}
	return c.Client.AddTorrent(mi)
}

// DownloadTorrent adds a torrent and marks it for complete download
func (c *TorrentClient) DownloadTorrent(torrent string) error {
	t, err := c.AddTorrent(torrent)
	if err != nil {
		return err
	}
	c.track(t, ModeDownload)
	t.DownloadAll()
	return nil
}

// HTTP Streaming Server

// StartServer starts the HTTP streaming server
func (c *TorrentClient) StartServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/stream", c.handler)
	c.Server = &http.Server{Addr: fmt.Sprintf("localhost:%s", c.Port), Handler: mux}

	go func() {
		if err := c.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Don't take the whole TUI down, streaming just won't work
			log.Println("stream server:", err)
		}
	}()
}

// handler handles HTTP streaming requests
func (c *TorrentClient) handler(w http.ResponseWriter, r *http.Request) {
	queries := r.URL.Query()
	hash := strings.TrimSpace(queries.Get("hash"))
	fpath := strings.TrimSpace(queries.Get("filepath"))

	if hash == "" {
		http.Error(w, "missing hash", http.StatusBadRequest)
		return
	}

	var ih metainfo.Hash
	if err := ih.FromHexString(hash); err != nil {
		http.Error(w, "invalid hash", http.StatusBadRequest)
		return
	}

	targetTorrent, ok := c.Client.Torrent(ih)
	if !ok {
		http.Error(w, "torrent not found", http.StatusNotFound)
		return
	}

	select {
	case <-targetTorrent.GotInfo():
	case <-r.Context().Done():
		return
	}

	files := targetTorrent.Files()
	var targetFile *torrent.File
	switch {
	case fpath != "":
		for _, f := range files {
			if f.DisplayPath() == fpath {
				targetFile = f
				break
			}
		}
	case len(files) == 1:
		targetFile = files[0]
	default:
		targetFile = GetLargestVideoFile(targetTorrent)
	}

	if targetFile == nil {
		http.Error(w, "file not found in torrent", http.StatusNotFound)
		return
	}

	reader := targetFile.NewReader()
	defer reader.Close()
	// Prioritise pieces around the playback position and keep a buffer ahead
	reader.SetResponsive()
	reader.SetReadahead(targetFile.Length() / 100)

	ctype := mime.TypeByExtension(path.Ext(targetFile.DisplayPath()))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	http.ServeContent(w, r, targetFile.DisplayPath(), time.Unix(targetTorrent.Metainfo().CreationDate, 0), reader)
}

// ServeTorrent generates a streaming link for a torrent
func (c *TorrentClient) ServeTorrent(t *torrent.Torrent) string {
	mh := t.InfoHash().HexString()
	return fmt.Sprintf("http://localhost:%s/stream?hash=%s", c.Port, mh)
}

// ServeTorrentEpisode generates a streaming link for a specific file
func (c *TorrentClient) ServeTorrentEpisode(t *torrent.Torrent, filePath string) string {
	mh := t.InfoHash().HexString()
	return fmt.Sprintf("http://localhost:%s/stream?hash=%s&filepath=%s", c.Port, mh, url.QueryEscape(filePath))
}

// Torrent Management

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
	c.forget(t.InfoHash())
	t.Drop()
}

// Close stops the client and closes all connections
func (c *TorrentClient) Close() []error {
	if c.Server != nil {
		c.Server.Close()
	}
	if c.Client == nil {
		return nil
	}
	return c.Client.Close()
}

// Utility Functions

// IsVideoFile checks if a file is a video
func IsVideoFile(f *torrent.File) bool {
	ext := strings.ToLower(path.Ext(f.Path()))
	switch ext {
	case ".mp4", ".mkv", ".avi", ".avif", ".av1", ".mov", ".flv", ".f4v", ".webm", ".wmv", ".mpeg", ".mpg", ".mlv", ".hevc", ".m4v", ".ts", ".m2ts", ".ogv":
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
	Mode    Mode
	Error   error
	// Tag is whatever was passed to AddAsyncTagged, handed back untouched
	Tag any
}

// TorrentProgressMsg is sent periodically to refresh download stats
type TorrentProgressMsg struct{}

// AddTorrentAsync adds a torrent for streaming asynchronously and returns a Bubble Tea command
func (c *TorrentClient) AddTorrentAsync(source string) tea.Cmd {
	return c.AddAsync(source, ModeStream)
}

// AddAsync registers the torrent with the download manager straight away (so
// it shows up while metadata is being fetched) and reports back once it is
// ready. ModeDownload torrents are fetched in full.
func (c *TorrentClient) AddAsync(source string, mode Mode) tea.Cmd {
	return c.AddAsyncTagged(source, mode, nil)
}

// AddAsyncTagged is AddAsync with caller context returned in TorrentAddedMsg.Tag
func (c *TorrentClient) AddAsyncTagged(source string, mode Mode, tag any) tea.Cmd {
	return func() tea.Msg {
		t, err := c.addTorrentNoWait(source)
		if err != nil {
			return TorrentAddedMsg{Mode: mode, Error: err, Tag: tag}
		}
		c.track(t, mode)
		t, err = c.waitForInfo(t)
		if err != nil {
			return TorrentAddedMsg{Mode: mode, Error: err, Tag: tag}
		}
		if mode == ModeDownload {
			t.DownloadAll()
		}
		return TorrentAddedMsg{Torrent: t, Mode: mode, Tag: tag}
	}
}

// TickProgress returns a command that periodically updates torrent progress
func TickProgress() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
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
	progress := float64(t.BytesCompleted()) / float64(t.Length()) * 100

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
