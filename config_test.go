package main

import (
	"net/http"
	"testing"
)

func TestParseFlags(t *testing.T) {
	saved := cfg
	defer func() { cfg = saved }()
	tr := http.DefaultTransport.(*http.Transport)
	oldProxy := tr.Proxy
	defer func() { tr.Proxy = oldProxy }()

	if err := parseFlags([]string{"-nyaa", "https://nyaa.example.org/", "-proxy", "socks5://127.0.0.1:1080"}); err != nil {
		t.Fatal(err)
	}
	if cfg.NyaaURL != "https://nyaa.example.org" {
		t.Fatalf("nyaa = %q", cfg.NyaaURL)
	}
	req, _ := http.NewRequest("GET", "https://graphql.anilist.co", nil)
	u, err := tr.Proxy(req)
	if err != nil || u == nil || u.String() != "socks5://127.0.0.1:1080" {
		t.Fatalf("proxy = %v %v", u, err)
	}

	for _, bad := range [][]string{{"-nyaa", "nyaa.si"}, {"-proxy", "::"}} {
		if err := parseFlags(bad); err == nil {
			t.Errorf("parseFlags(%v) should fail", bad)
		}
	}
}
