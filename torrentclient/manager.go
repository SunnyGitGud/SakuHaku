package torrentclient

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

// Mode says why a torrent was added
type Mode int

const (
	// ModeStream torrents only fetch the pieces the player asks for
	ModeStream Mode = iota
	// ModeDownload torrents are fetched in full
	ModeDownload
)

func (m Mode) String() string {
	if m == ModeDownload {
		return "Download"
	}
	return "Stream"
}

// State is the high level status of a managed torrent
type State int

const (
	StateMetadata State = iota
	StateDownloading
	StateStreaming
	StatePaused
	StateStalled
	StateCompleted
)

func (s State) String() string {
	switch s {
	case StateMetadata:
		return "Fetching metadata"
	case StateDownloading:
		return "Downloading"
	case StateStreaming:
		return "Streaming"
	case StatePaused:
		return "Paused"
	case StateStalled:
		return "Stalled"
	case StateCompleted:
		return "Completed"
	}
	return "Unknown"
}

// DownloadInfo is a point-in-time snapshot of a managed torrent, safe to
// render from the UI without touching the torrent itself.
type DownloadInfo struct {
	InfoHash  string
	Name      string
	Mode      Mode
	State     State
	Size      int64
	Completed int64
	Progress  float64 // 0..1
	DownRate  float64 // bytes/s
	UpRate    float64 // bytes/s
	Peers     int
	Seeders   int
	ETA       time.Duration // 0 when unknown
	Path      string
	AddedAt   time.Time
}

type tracked struct {
	t       *torrent.Torrent
	mode    Mode
	paused  bool
	addedAt time.Time

	lastRead    int64
	lastWritten int64
	lastSample  time.Time
	downRate    float64
	upRate      float64
}

// rateSmoothing is the EWMA weight given to the newest sample
const rateSmoothing = 0.4

func (c *TorrentClient) track(t *torrent.Torrent, mode Mode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ih := t.InfoHash()
	if e, ok := c.tracked[ih]; ok {
		// Streaming something we then choose to download upgrades it
		if mode == ModeDownload {
			e.mode = ModeDownload
		}
		return
	}
	c.tracked[ih] = &tracked{t: t, mode: mode, addedAt: time.Now()}
	c.order = append(c.order, ih)
}

func (c *TorrentClient) forget(ih metainfo.Hash) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.tracked[ih]; !ok {
		return
	}
	delete(c.tracked, ih)
	for i, h := range c.order {
		if h == ih {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

func (c *TorrentClient) lookup(infoHash string) (*tracked, error) {
	var ih metainfo.Hash
	if err := ih.FromHexString(infoHash); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.tracked[ih]
	if !ok {
		return nil, fmt.Errorf("no managed torrent with hash %s", infoHash)
	}
	return e, nil
}

// Refresh samples transfer counters to update speed estimates. Call it about
// once a second (see TickProgress).
func (c *TorrentClient) Refresh() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for _, e := range c.tracked {
		stats := e.t.Stats()
		read := stats.BytesReadUsefulData.Int64()
		written := stats.BytesWrittenData.Int64()
		if !e.lastSample.IsZero() {
			dt := now.Sub(e.lastSample).Seconds()
			if dt > 0 {
				down := float64(read-e.lastRead) / dt
				up := float64(written-e.lastWritten) / dt
				e.downRate = rateSmoothing*down + (1-rateSmoothing)*e.downRate
				e.upRate = rateSmoothing*up + (1-rateSmoothing)*e.upRate
			}
		}
		e.lastRead, e.lastWritten, e.lastSample = read, written, now
	}
}

// Downloads returns snapshots of all managed torrents in the order they were added
func (c *TorrentClient) Downloads() []DownloadInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]DownloadInfo, 0, len(c.order))
	for _, ih := range c.order {
		out = append(out, c.snapshot(c.tracked[ih]))
	}
	return out
}

func (c *TorrentClient) snapshot(e *tracked) DownloadInfo {
	t := e.t
	d := DownloadInfo{
		InfoHash: t.InfoHash().HexString(),
		Name:     t.Name(),
		Mode:     e.mode,
		DownRate: e.downRate,
		UpRate:   e.upRate,
		AddedAt:  e.addedAt,
	}

	stats := t.Stats()
	d.Peers = stats.ActivePeers
	d.Seeders = stats.ConnectedSeeders

	if t.Info() == nil {
		d.State = StateMetadata
		if d.Name == "" {
			d.Name = d.InfoHash
		}
		return d
	}

	d.Size = t.Length()
	d.Completed = t.BytesCompleted()
	if d.Size > 0 {
		d.Progress = float64(d.Completed) / float64(d.Size)
	}
	d.Path = c.dataPath(t)

	switch {
	case d.Completed >= d.Size:
		d.State = StateCompleted
		d.Progress = 1
	case e.paused:
		d.State = StatePaused
	case e.mode == ModeStream:
		d.State = StateStreaming
	case d.Peers == 0 && e.downRate < 1:
		d.State = StateStalled
	default:
		d.State = StateDownloading
	}

	if d.State == StateDownloading && e.downRate > 1 {
		d.ETA = time.Duration(float64(d.Size-d.Completed)/e.downRate) * time.Second
	}
	return d
}

// dataPath is where the torrent's data lives on disk
func (c *TorrentClient) dataPath(t *torrent.Torrent) string {
	info := t.Info()
	if info == nil || c.DownloadDir == "" || c.DownloadDir == c.DataDir {
		return ""
	}
	name := info.BestName()
	if name == "" || name == metainfo.NoName || name != filepath.Base(name) || name == "." || name == ".." {
		return ""
	}
	return filepath.Join(c.DownloadDir, name)
}

// Torrent returns the underlying torrent for a managed info hash
func (c *TorrentClient) Torrent(infoHash string) (*torrent.Torrent, error) {
	e, err := c.lookup(infoHash)
	if err != nil {
		return nil, err
	}
	return e.t, nil
}

// StartDownload switches a managed torrent to full download
func (c *TorrentClient) StartDownload(infoHash string) error {
	e, err := c.lookup(infoHash)
	if err != nil {
		return err
	}
	c.mu.Lock()
	e.mode = ModeDownload
	c.mu.Unlock()
	if e.t.Info() != nil {
		e.t.DownloadAll()
	}
	return nil
}

// TogglePause pauses or resumes data transfer for a managed torrent
func (c *TorrentClient) TogglePause(infoHash string) (paused bool, err error) {
	e, err := c.lookup(infoHash)
	if err != nil {
		return false, err
	}
	c.mu.Lock()
	e.paused = !e.paused
	paused = e.paused
	c.mu.Unlock()
	if paused {
		e.t.DisallowDataDownload()
	} else {
		e.t.AllowDataDownload()
	}
	return paused, nil
}

// Remove stops and forgets a managed torrent, optionally deleting its data
func (c *TorrentClient) Remove(infoHash string, deleteFiles bool) error {
	e, err := c.lookup(infoHash)
	if err != nil {
		return err
	}
	dataPath := c.dataPath(e.t)
	c.forget(e.t.InfoHash())
	e.t.Drop()
	<-e.t.Closed()

	if !deleteFiles {
		return nil
	}
	if dataPath == "" {
		return fmt.Errorf("don't know where the data for %q is stored", e.t.Name())
	}
	// Belt and braces: never delete anything outside the download directory
	rel, err := filepath.Rel(c.DownloadDir, dataPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("refusing to delete %q", dataPath)
	}
	return os.RemoveAll(dataPath)
}

// ActiveCount returns how many managed torrents are still transferring
func (c *TorrentClient) ActiveCount() int {
	n := 0
	for _, d := range c.Downloads() {
		if d.State != StateCompleted && d.State != StatePaused {
			n++
		}
	}
	return n
}

// FormatDuration renders an ETA like "1h02m" or "3m12s"
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return "∞"
	}
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	switch {
	case h > 99:
		return ">99h"
	case h > 0:
		return fmt.Sprintf("%dh%02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm%02ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// PieceCell summarises a run of pieces for display
type PieceCell uint8

const (
	PieceMissing  PieceCell = iota
	PieceWanted             // prioritised, nothing received yet
	PiecePartial            // some data received
	PieceComplete           // verified
)

// FileStats describes download progress of a single file in a torrent
type FileStats struct {
	Path      string
	Length    int64
	Completed int64
	// Pieces is the file's piece map squeezed into a fixed number of cells
	Pieces []PieceCell
}

// FileStats reports progress for one file, with its piece map summarised into
// the given number of cells
func (c *TorrentClient) FileStats(infoHash, displayPath string, cells int) (FileStats, error) {
	t, err := c.Torrent(infoHash)
	if err != nil {
		return FileStats{}, err
	}
	if t.Info() == nil {
		return FileStats{}, fmt.Errorf("metadata not available yet")
	}

	var f *torrent.File
	for _, candidate := range t.Files() {
		if candidate.DisplayPath() == displayPath {
			f = candidate
			break
		}
	}
	if f == nil {
		return FileStats{}, fmt.Errorf("no file %q in torrent", displayPath)
	}

	fs := FileStats{Path: displayPath, Length: f.Length(), Completed: f.BytesCompleted()}
	begin, end := f.BeginPieceIndex(), f.EndPieceIndex()
	n := end - begin
	if cells <= 0 || n <= 0 {
		return fs, nil
	}

	fs.Pieces = make([]PieceCell, cells)
	for cell := range cells {
		lo := begin + cell*n/cells
		hi := begin + (cell+1)*n/cells
		if hi <= lo {
			hi = lo + 1
		}
		complete, any, wanted := true, false, false
		for i := lo; i < hi && i < end; i++ {
			ps := t.PieceState(i)
			if ps.Complete {
				any = true
				continue
			}
			complete = false
			if ps.Partial {
				any = true
			}
			if ps.Priority != torrent.PiecePriorityNone {
				wanted = true
			}
		}
		switch {
		case complete:
			fs.Pieces[cell] = PieceComplete
		case any:
			fs.Pieces[cell] = PiecePartial
		case wanted:
			fs.Pieces[cell] = PieceWanted
		}
	}
	return fs, nil
}
