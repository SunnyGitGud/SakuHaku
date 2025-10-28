package torrent

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
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
)

// TorrentMetadata stores metadata for cached torrents
type TorrentMetadata struct {
	InfoHash string    `json:"infoHash"`
	MediaID  int       `json:"mediaID"`
	Episode  int       `json:"episode"`
	Date     time.Time `json:"date"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Progress float64   `json:"progress"`
	Bitfield []byte    `json:"bitfield"`
	Files    int       `json:"files"`
	Announce []string  `json:"announce"`
	URLList  []string  `json:"urlList"`
	Private  bool      `json:"private"`
}

// LibraryEntry represents a cached torrent in the library
type LibraryEntry struct {
	MediaID  int       `json:"mediaID"`
	Episode  int       `json:"episode"`
	Files    int       `json:"files"`
	Hash     string    `json:"hash"`
	Progress float64   `json:"progress"`
	Date     time.Time `json:"date"`
	Size     int64     `json:"size"`
	Name     string    `json:"name"`
}

// TorrentFile represents a file in a torrent
type TorrentFile struct {
	Hash string `json:"hash"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
	Path string `json:"path"`
	ID   int    `json:"id"`
	URL  string `json:"url"`
}

// PeerInfo represents information about a peer
type PeerInfo struct {
	IP         string   `json:"ip"`
	Seeder     bool     `json:"seeder"`
	Client     string   `json:"client"`
	Progress   float64  `json:"progress"`
	Downloaded int64    `json:"downloaded"`
	Uploaded   int64    `json:"uploaded"`
	DownSpeed  int64    `json:"downSpeed"`
	UpSpeed    int64    `json:"upSpeed"`
	Flags      []string `json:"flags"`
}

// FileInfo represents information about a torrent file
type FileInfo struct {
	Name       string  `json:"name"`
	Size       int64   `json:"size"`
	Progress   float64 `json:"progress"`
	Selections int     `json:"selections"`
}

// TorrentInfo represents comprehensive torrent statistics
type TorrentInfo struct {
	Hash     string  `json:"hash"`
	Name     string  `json:"name"`
	Progress float64 `json:"progress"`
	Peers    struct {
		Seeders  int `json:"seeders"`
		Leechers int `json:"leechers"`
		Wires    int `json:"wires"`
	} `json:"peers"`
	Speed struct {
		Down int64 `json:"down"`
		Up   int64 `json:"up"`
	} `json:"speed"`
	Size struct {
		Downloaded int64 `json:"downloaded"`
		Uploaded   int64 `json:"uploaded"`
		Total      int64 `json:"total"`
	} `json:"size"`
	Time struct {
		Remaining int64 `json:"remaining"`
		Elapsed   int64 `json:"elapsed"`
	} `json:"time"`
	Pieces struct {
		Total int `json:"total"`
		Size  int `json:"size"`
	} `json:"pieces"`
}

// ProtocolStatus represents protocol status information
type ProtocolStatus struct {
	DHT        bool `json:"dht"`
	LSD        bool `json:"lsd"`
	PEX        bool `json:"pex"`
	NAT        bool `json:"nat"`
	Forwarding bool `json:"forwarding"`
	Persisting bool `json:"persisting"`
	Streaming  bool `json:"streaming"`
}

// ScrapeResponse represents scrape data for a torrent
type ScrapeResponse struct {
	Hash       string `json:"hash"`
	Complete   int    `json:"complete"`
	Downloaded int    `json:"downloaded"`
	Incomplete int    `json:"incomplete"`
}

// TorrentSettings represents client configuration
type TorrentSettings struct {
	Path                    string
	TorrentDHT              bool
	TorrentPeX              bool
	TorrentSpeed            float64
	TorrentPort             int
	DHTPort                 int
	MaxConns                int
	TorrentStreamedDownload bool
	TorrentPersist          bool
	DisableIPv6             bool
}

// TorrentStore manages persistent torrent metadata
type TorrentStore struct {
	cacheFolder string
	mu          sync.RWMutex
}

// NewTorrentStore creates a new torrent store
func NewTorrentStore(basePath string) (*TorrentStore, error) {
	cacheFolder := filepath.Join(basePath, "hayase-cache")
	err := os.MkdirAll(cacheFolder, 0755)
	if err != nil {
		return nil, err
	}
	return &TorrentStore{
		cacheFolder: cacheFolder,
	}, nil
}

// Get retrieves metadata for a torrent
func (s *TorrentStore) Get(infoHash string) (*TorrentMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if infoHash == "" {
		return nil, nil
	}

	filePath := filepath.Join(s.cacheFolder, infoHash+".json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var metadata TorrentMetadata
	err = json.Unmarshal(data, &metadata)
	if err != nil {
		return nil, err
	}

	return &metadata, nil
}

// Set stores metadata for a torrent
func (s *TorrentStore) Set(metadata *TorrentMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}

	filePath := filepath.Join(s.cacheFolder, metadata.InfoHash+".json")
	return os.WriteFile(filePath, data, 0666)
}

// Delete removes metadata for a torrent
func (s *TorrentStore) Delete(infoHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filePath := filepath.Join(s.cacheFolder, infoHash+".json")
	return os.Remove(filePath)
}

// List returns all cached torrent hashes
func (s *TorrentStore) List() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	files, err := os.ReadDir(s.cacheFolder)
	if err != nil {
		return nil, err
	}

	var hashes []string
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
			hash := strings.TrimSuffix(file.Name(), ".json")
			hashes = append(hashes, hash)
		}
	}

	return hashes, nil
}

// Entries returns all cached torrent metadata
func (s *TorrentStore) Entries() ([]*TorrentMetadata, error) {
	hashes, err := s.List()
	if err != nil {
		return nil, err
	}

	var entries []*TorrentMetadata
	for _, hash := range hashes {
		metadata, err := s.Get(hash)
		if err != nil {
			continue
		}
		entries = append(entries, metadata)
	}

	return entries, nil
}

// Torrent Client
type TorrentClient struct {
	Name              string
	DataDir           string
	DownloadDir       string
	Seed              bool
	NoServer          bool
	Port              string
	TorrentPort       int
	Client            *torrent.Client
	Server            *http.Server
	Store             *TorrentStore
	DisableIPV6       bool
	Persist           bool
	Streamed          bool
	mu                sync.RWMutex
	activeTorrentHash string
	startTimes        map[string]time.Time
	lastBytesRead     map[string]int64
	lastBytesWritten  map[string]int64
	lastUpdateTime    map[string]time.Time
	speedUpdateMutex  sync.Mutex
}

// NewTorrentClient creates a new torrent client instance
func NewTorrentClient(name string, port string) *TorrentClient {
	return &TorrentClient{
		Name:             name,
		Port:             port,
		NoServer:         false,
		Seed:             true,
		startTimes:       make(map[string]time.Time),
		lastBytesRead:    make(map[string]int64),
		lastBytesWritten: make(map[string]int64),
		lastUpdateTime:   make(map[string]time.Time),
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

	// Initialize store
	store, err := NewTorrentStore(c.DataDir)
	if err != nil {
		return fmt.Errorf("error creating torrent store: %v", err)
	}
	c.Store = store

	if !c.NoServer {
		c.StartServer()
	}

	c.Client = client
	return nil
}

// UpdateSettings updates client settings at runtime
func (c *TorrentClient) UpdateSettings(settings TorrentSettings) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.DisableIPV6 = settings.DisableIPv6
	c.TorrentPort = settings.TorrentPort
	c.Persist = settings.TorrentPersist
	c.Streamed = settings.TorrentStreamedDownload

	if settings.Path != "" {
		c.DownloadDir = settings.Path
		store, err := NewTorrentStore(settings.Path)
		if err != nil {
			return err
		}
		c.Store = store
	}

	// Note: Some settings require client restart to take effect
	// rn we just update the stored values
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

// PlayTorrent adds and prepares a torrent for playback
func (c *TorrentClient) PlayTorrent(id string, mediaID int, episode int) ([]TorrentFile, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if torrent already exists
	var existing *torrent.Torrent
	for _, t := range c.Client.Torrents() {
		if t.InfoHash().String() == id || t.Name() == id {
			existing = t
			break
		}
	}

	// Load from cache if not already loaded
	if existing == nil {
		infoHash, err := c.ToInfoHash(id)
		if err == nil && infoHash != "" {
			_, _ = c.Store.Get(infoHash)
		}
	}

	// Remove old torrent if exists and we're adding a new one
	if existing == nil && len(c.Client.Torrents()) > 0 {
		oldTorrent := c.Client.Torrents()[0]
		oldHash := oldTorrent.InfoHash().String()

		// Save metadata before removal
		c.saveTorrentMetadata(oldTorrent, 0, 0)

		oldTorrent.Drop()
		if !c.Persist {
			c.Store.Delete(oldHash)
			// Also remove files if not persisting
			oldTorrent.Files()
		}
	}

	var t *torrent.Torrent
	var err error

	if existing != nil {
		t = existing
	} else {
		// Add new torrent
		t, err = c.AddTorrent(id)
		if err != nil {
			return nil, err
		}
	}

	<-t.GotInfo()

	// Track start time
	c.startTimes[t.InfoHash().String()] = time.Now()
	c.activeTorrentHash = t.InfoHash().String()

	// If streamed mode, don't download all files automatically
	if !c.Streamed {
		t.DownloadAll()
	}

	// Save metadata
	go c.periodicMetadataSave(t, mediaID, episode)

	// Build file list
	files := make([]TorrentFile, len(t.Files()))
	for i, f := range t.Files() {
		files[i] = TorrentFile{
			Hash: t.InfoHash().String(),
			Name: f.DisplayPath(),
			Type: getFileType(f.Path()),
			Size: f.Length(),
			Path: f.Path(),
			ID:   i,
			URL:  c.ServeTorrentEpisode(t, f.DisplayPath()),
		}
	}

	return files, nil
}

// periodicMetadataSave saves torrent metadata periodically
func (c *TorrentClient) periodicMetadataSave(t *torrent.Torrent, mediaID, episode int) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	saveFunc := func() {
		c.saveTorrentMetadata(t, mediaID, episode)
	}

	// Initial save
	saveFunc()

	for {
		select {
		case <-ticker.C:
			saveFunc()
		case <-t.Closed():
			saveFunc()
			return
		}
	}
}

// saveTorrentMetadata saves current torrent state to store
func (c *TorrentClient) saveTorrentMetadata(t *torrent.Torrent, mediaID, episode int) {
	if t.Info() == nil {
		return
	}

	stats := t.Stats()
	progress := float64(stats.BytesReadData.Int64()) / float64(t.Length())

	metadata := &TorrentMetadata{
		InfoHash: t.InfoHash().String(),
		MediaID:  mediaID,
		Episode:  episode,
		Date:     time.Now(),
		Name:     t.Name(),
		Size:     t.Length(),
		Progress: progress,
		Files:    len(t.Files()),
		Private:  *t.Info().Private,
	}

	// Get announce list
	if t.Metainfo().Announce != "" {
		metadata.Announce = []string{t.Metainfo().Announce}
	}
	for _, tier := range t.Metainfo().AnnounceList {
		metadata.Announce = append(metadata.Announce, tier...)
	}

	// Get URL list
	metadata.URLList = t.Metainfo().UrlList

	c.Store.Set(metadata)
}

// ToInfoHash converts various torrent identifiers to info hash
func (c *TorrentClient) ToInfoHash(torrentId string) (string, error) {
	if len(torrentId) == 40 {
		return torrentId, nil
	}

	// Try to parse as magnet
	if strings.HasPrefix(torrentId, "magnet:") {
		spec, err := torrent.TorrentSpecFromMagnetUri(torrentId)
		if err != nil {
			return "", err
		}
		return spec.InfoHash.String(), nil
	}

	// Try to parse as torrent file
	if strings.HasPrefix(torrentId, "http") || filepath.Ext(torrentId) == ".torrent" {
		var mi *metainfo.MetaInfo
		var err error

		if strings.HasPrefix(torrentId, "http") {
			resp, httpErr := http.Get(torrentId)
			if httpErr != nil {
				return "", httpErr
			}
			defer resp.Body.Close()
			mi, err = metainfo.Load(resp.Body)
		} else {
			mi, err = metainfo.LoadFromFile(torrentId)
		}

		if err != nil {
			return "", err
		}

		_, err = mi.UnmarshalInfo()
		if err != nil {
			return "", err
		}

		return mi.HashInfoBytes().String(), nil
	}

	return "", fmt.Errorf("unable to extract info hash from: %s", torrentId)
}

// Library returns all cached torrents
func (c *TorrentClient) Library() ([]LibraryEntry, error) {
	entries, err := c.Store.Entries()
	if err != nil {
		return nil, err
	}

	library := make([]LibraryEntry, len(entries))
	for i, entry := range entries {
		library[i] = LibraryEntry{
			MediaID:  entry.MediaID,
			Episode:  entry.Episode,
			Files:    entry.Files,
			Hash:     entry.InfoHash,
			Progress: entry.Progress,
			Date:     entry.Date,
			Size:     entry.Size,
			Name:     entry.Name,
		}
	}

	return library, nil
}

// Cached returns list of cached torrent hashes
func (c *TorrentClient) Cached() ([]string, error) {
	return c.Store.List()
}

// DeleteTorrents removes torrents and their data
func (c *TorrentClient) DeleteTorrents(hashes []string) error {
	for _, hash := range hashes {
		// Skip if it's the currently active torrent
		if hash == c.activeTorrentHash {
			continue
		}

		// Remove from store
		c.Store.Delete(hash)
	}

	return nil
}

// IsAnimeCached checks if any episodes for a specific anime are cached
func (c *TorrentClient) IsAnimeCached(mediaID int) (bool, []LibraryEntry, error) {
	entries, err := c.Library()
	if err != nil {
		return false, nil, err
	}

	var cachedEntries []LibraryEntry
	for _, entry := range entries {
		if entry.MediaID == mediaID {
			cachedEntries = append(cachedEntries, entry)
		}
	}

	return len(cachedEntries) > 0, cachedEntries, nil
}

// GetCachedEpisodes returns all cached episodes for a specific anime
func (c *TorrentClient) GetCachedEpisodes(mediaID int) ([]LibraryEntry, error) {
	entries, err := c.Library()
	if err != nil {
		return nil, err
	}

	var cachedEpisodes []LibraryEntry
	for _, entry := range entries {
		if entry.MediaID == mediaID {
			cachedEpisodes = append(cachedEpisodes, entry)
		}
	}

	return cachedEpisodes, nil
}

// ClearAnimeCache removes all cached torrents for a specific anime
func (c *TorrentClient) ClearAnimeCache(mediaID int) error {
	entries, err := c.Library()
	if err != nil {
		return err
	}

	var hashesToDelete []string
	for _, entry := range entries {
		if entry.MediaID == mediaID {
			hashesToDelete = append(hashesToDelete, entry.Hash)
		}
	}

	return c.DeleteTorrents(hashesToDelete)
}

// GetCacheStats returns cache statistics
func (c *TorrentClient) GetCacheStats() (totalEntries int, totalSize int64, err error) {
	entries, err := c.Library()
	if err != nil {
		return 0, 0, err
	}

	totalEntries = len(entries)
	for _, entry := range entries {
		totalSize += entry.Size
	}

	return totalEntries, totalSize, nil
}

// TorrentInfo returns detailed information about a torrent (matches TypeScript torrentInfo)
func (c *TorrentClient) TorrentInfo(id string) (*TorrentInfo, error) {
	t, err := c.FindByInfoHash(id)
	if err != nil {
		return nil, err
	}

	return c.makeStats(t), nil
}

// FileInfo returns information about torrent files
func (c *TorrentClient) FileInfo(id string) ([]FileInfo, error) {
	t, err := c.FindByInfoHash(id)
	if err != nil {
		return nil, err
	}

	files := t.Files()
	fileInfos := make([]FileInfo, len(files))

	for i, f := range files {
		progress := float64(f.BytesCompleted()) / float64(f.Length())
		fileInfos[i] = FileInfo{
			Name:       f.DisplayPath(),
			Size:       f.Length(),
			Progress:   progress,
			Selections: 0, // Not directly available in anacrolix/torrent
		}
	}

	return fileInfos, nil
}

// ProtocolStatus returns protocol status information
func (c *TorrentClient) ProtocolStatus(id string) (*ProtocolStatus, error) {
	t, err := c.FindByInfoHash(id)
	if err != nil {
		return nil, err
	}

	status := &ProtocolStatus{
		DHT:        c.Client.DhtServers() != nil && len(c.Client.DhtServers()) > 0,
		PEX:        t.Info().Private == nil || !*t.Info().Private, // PEX is disabled for private torrents
		Persisting: c.Persist,
		Streaming:  c.Streamed,
	}

	// Check for incoming connections
	hasIncoming := false
	for range t.PeerConns() {
		// assuming all connections are incoming
		// todo check for connection type
		hasIncoming = true
		break
	}
	status.Forwarding = hasIncoming

	return status, nil
}

// calculateSpeed calculates the current download/upload speed for a torrent
func (c *TorrentClient) calculateSpeed(t *torrent.Torrent) (downSpeed, upSpeed int64) {
	c.speedUpdateMutex.Lock()
	defer c.speedUpdateMutex.Unlock()

	hash := t.InfoHash().String()
	stats := t.Stats()
	currentBytesRead := stats.BytesReadData.Int64()
	currentBytesWritten := stats.BytesWrittenData.Int64()
	now := time.Now()

	// Initialize if first time
	if _, exists := c.lastUpdateTime[hash]; !exists {
		c.lastBytesRead[hash] = currentBytesRead
		c.lastBytesWritten[hash] = currentBytesWritten
		c.lastUpdateTime[hash] = now
		return 0, 0
	}

	// Calculate time difference
	timeDiff := now.Sub(c.lastUpdateTime[hash]).Seconds()
	if timeDiff < 0.1 { // Avoid division by very small numbers
		return 0, 0
	}

	// Calculate speeds
	downSpeed = int64(float64(currentBytesRead-c.lastBytesRead[hash]) / timeDiff)
	upSpeed = int64(float64(currentBytesWritten-c.lastBytesWritten[hash]) / timeDiff)

	// Update stored values
	c.lastBytesRead[hash] = currentBytesRead
	c.lastBytesWritten[hash] = currentBytesWritten
	c.lastUpdateTime[hash] = now

	return downSpeed, upSpeed
}

// makeStats creates TorrentInfo from a torrent
func (c *TorrentClient) makeStats(t *torrent.Torrent) *TorrentInfo {
	stats := t.Stats()

	seeders := 0
	leechers := 0
	for _, conn := range t.PeerConns() {
		pieces := conn.PeerPieces()
		if pieces.GetCardinality() > 0 {
			seeders++
		} else {
			leechers++
		}
	}

	progress := float64(stats.BytesReadData.Int64()) / float64(t.Length())

	// Calculate current speeds
	downSpeed, upSpeed := c.calculateSpeed(t)

	var remaining int64
	if downSpeed > 0 {
		remainingBytes := t.Length() - stats.BytesReadData.Int64()
		if remainingBytes > 0 {
			remaining = remainingBytes / downSpeed
		}
	}

	var elapsed int64
	if startTime, ok := c.startTimes[t.InfoHash().String()]; ok {
		elapsed = int64(time.Since(startTime).Seconds())
	}

	info := &TorrentInfo{
		Hash:     t.InfoHash().String(),
		Name:     t.Name(),
		Progress: progress,
	}

	info.Peers.Seeders = seeders
	info.Peers.Leechers = leechers
	info.Peers.Wires = stats.ActivePeers

	info.Speed.Down = downSpeed
	info.Speed.Up = upSpeed

	info.Size.Downloaded = stats.BytesReadData.Int64()
	info.Size.Uploaded = stats.BytesWrittenData.Int64()
	info.Size.Total = t.Length()

	info.Time.Remaining = remaining
	info.Time.Elapsed = elapsed

	if t.Info() != nil {
		info.Pieces.Total = t.NumPieces()
		info.Pieces.Size = int(t.Info().PieceLength)
	}

	return info
}

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

// ShowTorrents returns all loaded torrents
func (c *TorrentClient) ShowTorrents() []*torrent.Torrent {
	return c.Client.Torrents()
}

// Torrents returns statistics for all torrents
func (c *TorrentClient) Torrents() []*TorrentInfo {
	torrents := c.Client.Torrents()
	infos := make([]*TorrentInfo, len(torrents))
	for i, t := range torrents {
		infos[i] = c.makeStats(t)
	}
	return infos
}

// FindByInfoHash finds a torrent by its info hash
func (c *TorrentClient) FindByInfoHash(infoHash string) (*torrent.Torrent, error) {
	torrents := c.Client.Torrents()
	for _, t := range torrents {
		if t.InfoHash().String() == infoHash || t.InfoHash().AsString() == infoHash {
			return t, nil
		}
	}
	return nil, fmt.Errorf("no torrents match info hash: %v", infoHash)
}

// DropTorrent removes a torrent from the client
func (c *TorrentClient) DropTorrent(t *torrent.Torrent) {
	// Save metadata before dropping if persist is enabled
	if c.Persist {
		c.saveTorrentMetadata(t, 0, 0)
	}
	t.Drop()
}

// Close stops the client and closes all connections
func (c *TorrentClient) Close() []error {
	// Save all torrent metadata before closing
	for _, t := range c.Client.Torrents() {
		c.saveTorrentMetadata(t, 0, 0)
	}
	return c.Client.Close()
}

// Destroy completely shuts down the client (matches TypeScript destroy)
func (c *TorrentClient) Destroy() error {
	// Save all metadata
	for _, t := range c.Client.Torrents() {
		c.saveTorrentMetadata(t, 0, 0)
	}

	// Close server if running
	if c.Server != nil {
		c.Server.Close()
	}

	// Close client
	c.Client.Close()

	return nil
}

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

// getFileType returns the MIME type for a file
func getFileType(filePath string) string {
	ext := strings.ToLower(path.Ext(filePath))
	switch ext {
	case ".mp4":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".avi":
		return "video/x-msvideo"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".flv":
		return "video/x-flv"
	case ".wmv":
		return "video/x-ms-wmv"
	case ".mpeg", ".mpg":
		return "video/mpeg"
	default:
		return "application/octet-stream"
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

// VerifyDirectoryPermissions verifies read/write permissions for a directory
func (c *TorrentClient) VerifyDirectoryPermissions(path string) error {
	if path == "" {
		path = c.DataDir
	}

	// Try to create a test file
	testFile := filepath.Join(path, ".permission_test")
	f, err := os.Create(testFile)
	if err != nil {
		return fmt.Errorf("insufficient permissions to access directory: %s", path)
	}
	f.Close()
	os.Remove(testFile)

	return nil
}

// Errors sets up error callback handler
func (c *TorrentClient) Errors(callback func(error)) {
	// to do create more comprehensive error handling
	go func() {
		for {
			// Monitor client for errors
			time.Sleep(time.Second)
		}
	}()
}
