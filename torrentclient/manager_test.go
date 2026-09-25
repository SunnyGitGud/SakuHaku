package torrentclient

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

// newLocalTorrent builds a .torrent for files already in downloadDir, so the
// client "has" the whole thing without touching the network
func newLocalTorrent(t *testing.T, downloadDir, name string, files map[string][]byte) string {
	t.Helper()
	root := filepath.Join(downloadDir, name)
	os.MkdirAll(root, 0o755)
	for n, data := range files {
		if err := os.WriteFile(filepath.Join(root, n), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	info := metainfo.Info{PieceLength: 64 << 10}
	if err := info.BuildFromFilePath(root); err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{}
	mi.InfoBytes, _ = bencode.Marshal(info)
	path := filepath.Join(t.TempDir(), "test.torrent")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := mi.Write(f); err != nil {
		t.Fatal(err)
	}
	return path
}

func newTestClient(t *testing.T) (*TorrentClient, string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	dl := filepath.Join(tmp, "downloads")
	os.MkdirAll(dl, 0o755)

	c := NewTorrentClient("sakuhaku-test", InternalStreamPort)
	c.SetDownloadDir(dl)
	c.DisableIPV6 = true
	if err := c.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, dl
}

func TestManagerLifecycle(t *testing.T) {
	c, dl := newTestClient(t)
	video := bytes.Repeat([]byte("0123456789abcdef"), 1<<15) // 512 KiB
	name := "Ep 01 & more.mkv"                               // needs URL escaping
	tf := newLocalTorrent(t, dl, "Show [1080p]", map[string][]byte{name: video, "notes.txt": []byte("hi")})

	msg := c.AddAsync(tf, ModeDownload)().(TorrentAddedMsg)
	if msg.Error != nil {
		t.Fatal(msg.Error)
	}
	msg.Torrent.VerifyData()
	c.Refresh()

	downloads := c.Downloads()
	if len(downloads) != 1 {
		t.Fatalf("got %d downloads", len(downloads))
	}
	d := downloads[0]
	if d.State != StateCompleted || d.Path != filepath.Join(dl, "Show [1080p]") {
		t.Fatalf("unexpected snapshot %+v", d)
	}

	// Per-file piece map
	vid := GetLargestVideoFile(msg.Torrent)
	if vid == nil || vid.DisplayPath() != name {
		t.Fatalf("largest video = %v", vid)
	}
	fs, err := c.FileStats(d.InfoHash, vid.DisplayPath(), 20)
	if err != nil {
		t.Fatal(err)
	}
	if fs.Completed != int64(len(video)) || len(fs.Pieces) != 20 || fs.Pieces[0] != PieceComplete {
		t.Fatalf("file stats %+v", fs)
	}

	// Streaming, including a range request like a player seeking
	u := c.ServeTorrentEpisode(msg.Torrent, vid.DisplayPath())
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Range", "bytes=1000-1999")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(body, video[1000:2000]) {
		t.Fatalf("range request: %s, %d bytes", resp.Status, len(body))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/x-matroska" {
		t.Errorf("content type %q", ct)
	}

	if paused, _ := c.TogglePause(d.InfoHash); !paused {
		t.Error("expected paused")
	}
	if paused, _ := c.TogglePause(d.InfoHash); paused {
		t.Error("expected resumed")
	}

	if err := c.Remove(d.InfoHash, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dl, "Show [1080p]")); !os.IsNotExist(err) {
		t.Fatal("torrent data should be deleted")
	}
	if _, err := os.Stat(dl); err != nil {
		t.Fatal("the download dir itself must survive")
	}
	if len(c.Downloads()) != 0 {
		t.Fatal("torrent still tracked after remove")
	}
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                  "∞",
		42 * time.Second:   "42s",
		192 * time.Second:  "3m12s",
		3723 * time.Second: "1h02m",
		500 * time.Hour:    ">99h",
	} {
		if got := FormatDuration(d); got != want {
			t.Errorf("FormatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}
