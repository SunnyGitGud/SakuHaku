package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestToSeason(t *testing.T) {
	jjk2 := episodeNumbering{Offset: 24, Total: 23, Known: true}
	airing := episodeNumbering{Offset: 24, Known: true} // episode count unknown
	cases := []struct {
		n    episodeNumbering
		in   EpisodeInfo
		want EpisodeInfo
	}{
		{jjk2, EpisodeInfo{Episode: 29}, EpisodeInfo{Episode: 5}},  // absolute
		{jjk2, EpisodeInfo{Episode: 5}, EpisodeInfo{Episode: 5}},   // already relative
		{jjk2, EpisodeInfo{Episode: 23}, EpisodeInfo{Episode: 23}}, // fits the season
		{jjk2, EpisodeInfo{Episode: 60}, EpisodeInfo{Episode: 60}}, // past this season, leave it
		{jjk2, EpisodeInfo{Batch: true, From: 25, To: 47}, EpisodeInfo{Batch: true, From: 1, To: 23}},
		{jjk2, EpisodeInfo{Batch: true, From: 1, To: 24}, EpisodeInfo{Batch: true, From: 1, To: 24}}, // season 1 batch
		{airing, EpisodeInfo{Episode: 30}, EpisodeInfo{Episode: 6}},
		{episodeNumbering{Offset: 24}, EpisodeInfo{Episode: 29}, EpisodeInfo{Episode: 29}},                   // unknown offset: untouched
		{episodeNumbering{Known: true, Total: 1100}, EpisodeInfo{Episode: 1100}, EpisodeInfo{Episode: 1100}}, // One Piece: single entry
	}
	for _, c := range cases {
		if got := c.n.toSeason(c.in); got != c.want {
			t.Errorf("%+v.toSeason(%+v) = %+v, want %+v", c.n, c.in, got, c.want)
		}
	}
}

// fakeRelations serves a prequel chain: id -> (episodes, edges)
func fakeRelations(t *testing.T, media map[int]string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		id := int(body.Variables["id"].(float64))
		fmt.Fprintf(w, `{"data":{"Media":%s}}`, media[id])
	}))
	old := anilistEndpoint
	anilistEndpoint = srv.URL
	t.Cleanup(func() { anilistEndpoint = old; srv.Close() })
}

func TestWalkPrequels(t *testing.T) {
	fakeRelations(t, map[int]string{
		// Jujutsu Kaisen S2 (23 eps): prequels are the movie (skipped) and S1
		145064: `{"id":145064,"episodes":23,"relations":{"edges":[
			{"relationType":"PREQUEL","node":{"id":131573,"type":"ANIME","format":"MOVIE","episodes":1}},
			{"relationType":"PREQUEL","node":{"id":113415,"type":"ANIME","format":"TV","episodes":24}},
			{"relationType":"SOURCE","node":{"id":101517,"type":"MANGA","format":"MANGA","episodes":null}}]}}`,
		113415: `{"id":113415,"episodes":24,"relations":{"edges":[
			{"relationType":"SEQUEL","node":{"id":145064,"type":"ANIME","format":"TV","episodes":23}}]}}`,
		// A sequel whose prequel is still airing: offset unknown
		2: `{"id":2,"episodes":12,"relations":{"edges":[
			{"relationType":"PREQUEL","node":{"id":3,"type":"ANIME","format":"TV","episodes":null}}]}}`,
		// Two entries that name each other as prequel must not loop forever
		10: `{"id":10,"episodes":12,"relations":{"edges":[{"relationType":"PREQUEL","node":{"id":11,"type":"ANIME","format":"TV","episodes":12}}]}}`,
		11: `{"id":11,"episodes":12,"relations":{"edges":[{"relationType":"PREQUEL","node":{"id":10,"type":"ANIME","format":"TV","episodes":12}}]}}`,
	})

	n, err := walkPrequels(145064)
	if err != nil || n != (episodeNumbering{Offset: 24, Total: 23, Known: true}) {
		t.Fatalf("JJK S2: %+v %v", n, err)
	}
	n, err = walkPrequels(2)
	if err != nil || n.Known {
		t.Fatalf("unknown prequel length should give an unknown offset: %+v %v", n, err)
	}
	n, err = walkPrequels(10)
	if err != nil || n.Offset != 12 || !n.Known {
		t.Fatalf("cycle: %+v %v", n, err)
	}
}

func TestEpisodeFilterUsesAbsoluteNumbering(t *testing.T) {
	numberingMu.Lock()
	numberingCache[145064] = episodeNumbering{Offset: 24, Total: 23, Known: true}
	numberingMu.Unlock()

	m := &model{selectedAnime: &Anime{ID: 145064}, epFilter: 5}
	m.allTorrents = []Torrent{
		{Title: "[SubsPlease] Jujutsu Kaisen - 29 (1080p) [ABCD1234].mkv"},
		{Title: "[Other] Jujutsu Kaisen S2 - 05 [1080p]"},
		{Title: "[SubsPlease] Jujutsu Kaisen - 30 (1080p) [ABCD1234].mkv"},
		{Title: "[Group] Jujutsu Kaisen S2 (25-47) [Batch]"},
	}
	m.applyTorrentFilters()
	if len(m.torrents) != 3 {
		t.Fatalf("got %v", m.torrents)
	}
}
