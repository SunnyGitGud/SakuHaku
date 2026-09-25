package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

const subsPleaseJSON = `{
 "Dandadan - 05": {"time":"New","release_date":"10\/31\/24","show":"Dandadan","episode":"05",
  "downloads":[{"res":"480","magnet":"magnet:?xt=urn:btih:AAAA&dn=480"},
               {"res":"1080","magnet":"magnet:?xt=urn:btih:CCCC&dn=1080&xl=1453000000"},
               {"res":"720","magnet":"magnet:?xt=urn:btih:BBBB&dn=720"}],
  "xdcc":"","image_url":"\/wp-content\/uploads\/x.jpg","page":"dandadan"},
 "Dandadan - 04": {"time":"Thursday","release_date":"10\/24\/24","show":"Dandadan","episode":"04",
  "downloads":[{"res":"1080","magnet":"magnet:?xt=urn:btih:DDDD&dn=1080"}],"page":"dandadan"}
}`

const tokyoToshoRSSBody = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Tokyo Toshokan</title>
<item>
 <title><![CDATA[[SubsPlease] Dandadan - 05 (1080p) [ABCD1234].mkv]]></title>
 <link>https://nyaa.si/download/1888888.torrent</link>
 <description><![CDATA[<a href="https://www.tokyotosho.info/details.php?id=1">Tokyo Tosho</a><br />Submitter: SubsPlease | Authorized: Yes<br />Size: 1.35GB<br />Comment: | <a href="magnet:?xt=urn:btih:CCCC&amp;tr=http%3A%2F%2Ftracker">Magnet Link</a>]]></description>
 <category>Anime</category>
</item>
<item>
 <title><![CDATA[[Erai-raws] Dandadan - 05 [720p].mkv]]></title>
 <link>https://example.org/erai.torrent</link>
 <description><![CDATA[Size: 700MB]]></description>
 <category>Anime</category>
</item>
</channel></rss>`

func withSource(t *testing.T, target *string, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	old := *target
	*target = srv.URL
	t.Cleanup(func() { *target = old; srv.Close() })
}

func TestSearchSubsPlease(t *testing.T) {
	withSource(t, &cfg.SubsPleaseURL, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/" || r.URL.Query().Get("f") != "search" || r.URL.Query().Get("s") != "dandadan" {
			t.Errorf("unexpected request %s", r.URL)
		}
		fmt.Fprint(w, subsPleaseJSON)
	})

	got, err := searchSubsPlease("dandadan")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d torrents", len(got))
	}
	// Newest episode first, highest resolution first
	if got[0].Title != "[SubsPlease] Dandadan - 05 (1080p)" || got[0].TotalSize != 1453000000 {
		t.Errorf("first = %+v", got[0])
	}
	if got[3].Title != "[SubsPlease] Dandadan - 04 (1080p)" {
		t.Errorf("last = %+v", got[3])
	}
	if parseEpisode(got[0].Title).Episode != 5 || seedersOf(got[0]) != seedersUnknown {
		t.Error("episode/seeders parsing")
	}
}

func TestSearchSubsPleaseNoResults(t *testing.T) {
	withSource(t, &cfg.SubsPleaseURL, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "[]") })
	got, err := searchSubsPlease("nothing")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestSearchTokyoTosho(t *testing.T) {
	withSource(t, &cfg.TokyoToshoURL, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rss.php" || r.URL.Query().Get("type") != "1" {
			t.Errorf("unexpected request %s", r.URL)
		}
		fmt.Fprint(w, tokyoToshoRSSBody)
	})

	got, err := searchTokyoTosho("dandadan")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d torrents", len(got))
	}
	if got[0].MagnetURI != "magnet:?xt=urn:btih:CCCC&tr=http%3A%2F%2Ftracker" {
		t.Errorf("magnet = %q", got[0].MagnetURI)
	}
	if got[0].TotalSize != size135GB || got[1].TotalSize != 700<<20 {
		t.Errorf("sizes = %d, %d", got[0].TotalSize, got[1].TotalSize)
	}
	if got[1].source() != "https://example.org/erai.torrent" {
		t.Errorf("no magnet should fall back to the .torrent link")
	}
}

func TestCombinedSearchDedupesAndReportsErrors(t *testing.T) {
	saved := cfg.Sources
	defer func() { cfg.Sources = saved }()
	cfg.Sources = []string{"subsplease", "tokyotosho"}

	withSource(t, &cfg.SubsPleaseURL, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, subsPleaseJSON) })
	withSource(t, &cfg.TokyoToshoURL, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tokyoToshoRSSBody) })

	msg := performTorrentSearch("dandadan", "Dan Da Dan")().(torrentSearchResultMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	// 4 SubsPlease + 2 TokyoTosho, the TokyoTosho 1080p is the same torrent (CCCC)
	if len(msg.torrents) != 5 {
		var titles []string
		for _, t := range msg.torrents {
			titles = append(titles, t.Source+": "+t.Title)
		}
		t.Fatalf("got %d:\n%s", len(msg.torrents), strings.Join(titles, "\n"))
	}

	// Every source failing is an error
	withSource(t, &cfg.SubsPleaseURL, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) })
	withSource(t, &cfg.TokyoToshoURL, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	var cmd tea.Cmd = performTorrentSearch("x")
	msg = cmd().(torrentSearchResultMsg)
	if msg.err == nil || !strings.Contains(msg.err.Error(), "SubsPlease") || !strings.Contains(msg.err.Error(), "TokyoTosho") {
		t.Fatalf("err = %v", msg.err)
	}
}

func TestParseSizeString(t *testing.T) {
	for in, want := range map[string]int64{
		"1.5 GiB": 3 << 29, "1.35GB": 1449551462, "700MiB": 700 << 20, "512 KiB": 512 << 10,
		"12 B": 12, "": 0, "big": 0,
	} {
		if got := parseSizeString(in); got != want {
			t.Errorf("parseSizeString(%q) = %d, want %d", in, got, want)
		}
	}
}

// 1.35 GiB as the parser computes it (float, then truncated)
var size135GB = parseSizeString("1.35GB")
