package main

import "testing"

func TestParseEpisode(t *testing.T) {
	cases := []struct {
		title string
		want  EpisodeInfo
	}{
		{"[SubsPlease] Dandadan - 05 (1080p) [ABCD1234].mkv", EpisodeInfo{Episode: 5}},
		{"[Erai-raws] Sousou no Frieren - 28 [1080p][Multiple Subtitle][ABCDEF12]", EpisodeInfo{Episode: 28}},
		{"[SubsPlease] Dandadan S2 - 03v2 (720p) [ABCD1234].mkv", EpisodeInfo{Episode: 3}},
		{"Dan Da Dan S02E07 1080p WEB H.264-VARYG", EpisodeInfo{Episode: 7}},
		{"[ASW] Kaiju No. 8 - 12 [1080p HEVC x265 10Bit][AAC]", EpisodeInfo{Episode: 12}},
		{"Frieren Episode 10 [1080p]", EpisodeInfo{Episode: 10}},
		{"[Judas] Chainsaw Man (Season 1) [1080p][HEVC x265 10bit][Eng-Subs] (Batch)", EpisodeInfo{Batch: true}},
		{"[Anime Time] Spy x Family - 01-12 [1080p][HEVC 10bit x265][AAC]", EpisodeInfo{Batch: true, From: 1, To: 12}},
		{"[DB] Frieren (01~28) [Dual Audio 10bit 1080p]", EpisodeInfo{Batch: true, From: 1, To: 28}},
		{"One Piece - 1100 (1080p)", EpisodeInfo{Episode: 1100}},
		{"Oshi no Ko 2nd Season - 05 [1080p].mkv", EpisodeInfo{Episode: 5}},
		{"Some Movie 2023 1080p BluRay 5.1", EpisodeInfo{}},
	}
	for _, c := range cases {
		if got := parseEpisode(c.title); got != c.want {
			t.Errorf("parseEpisode(%q) = %+v, want %+v", c.title, got, c.want)
		}
	}
}

func TestEpisodeContains(t *testing.T) {
	if !(EpisodeInfo{Batch: true, From: 1, To: 12}).Contains(5) {
		t.Error("range should contain 5")
	}
	if (EpisodeInfo{Episode: 4}).Contains(5) {
		t.Error("episode 4 isn't 5")
	}
	if (EpisodeInfo{Batch: true}).Contains(5) {
		t.Error("unknown batch range shouldn't match")
	}
}
