package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Config holds runtime settings from flags and the environment
type Config struct {
	// Proxy routes all HTTP(S) traffic (AniList, torrent sites, posters,
	// tracker announces) through an http://, https:// or socks5:// proxy
	Proxy string
	// NyaaURL is the base URL of nyaa or one of its mirrors
	NyaaURL string
	// AnimeToshoURL is the base URL of the AnimeTosho feed
	AnimeToshoURL string
	// DownloadDir is where torrents are saved
	DownloadDir string
	// Player is the preferred video player command (auto-detected if empty)
	Player string
	// NoTracking disables updating AniList progress from playback
	NoTracking bool
}

var cfg = Config{
	NyaaURL:       "https://nyaa.si",
	AnimeToshoURL: "https://feed.animetosho.org",
}

// AniList OAuth client, from the environment or a .env file
var (
	clientID     string
	clientSecret string
	redirectURI  string
)

// parseFlags reads command line flags (falling back to SAKUHAKU_* env vars)
// into cfg and applies them
func parseFlags(args []string) error {
	fs := flag.NewFlagSet("sakuhaku", flag.ContinueOnError)
	fs.StringVar(&cfg.Proxy, "proxy", os.Getenv("SAKUHAKU_PROXY"),
		"proxy for all HTTP traffic, e.g. http://127.0.0.1:8080 or socks5://127.0.0.1:1080")
	fs.StringVar(&cfg.NyaaURL, "nyaa", envOr("SAKUHAKU_NYAA_URL", cfg.NyaaURL),
		"nyaa base URL, use a mirror if nyaa.si is blocked for you")
	fs.StringVar(&cfg.AnimeToshoURL, "animetosho", envOr("SAKUHAKU_ANIMETOSHO_URL", cfg.AnimeToshoURL),
		"AnimeTosho feed base URL")
	fs.StringVar(&cfg.DownloadDir, "download-dir", os.Getenv("SAKUHAKU_DOWNLOAD_DIR"),
		"where downloads are saved (default ~/Downloads/SakuHaku)")
	fs.StringVar(&cfg.Player, "player", os.Getenv("SAKUHAKU_PLAYER"),
		"video player to use: mpv, vlc, ffplay or mplayer (default: first one found)")
	fs.BoolVar(&cfg.NoTracking, "no-tracking", os.Getenv("SAKUHAKU_NO_TRACKING") != "",
		"don't update your AniList progress when you finish an episode")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg.NyaaURL = strings.TrimRight(cfg.NyaaURL, "/")
	cfg.AnimeToshoURL = strings.TrimRight(cfg.AnimeToshoURL, "/")
	for name, u := range map[string]string{"-nyaa": cfg.NyaaURL, "-animetosho": cfg.AnimeToshoURL} {
		if p, err := url.Parse(u); err != nil || p.Scheme == "" || p.Host == "" {
			return fmt.Errorf("%s: %q is not a valid URL", name, u)
		}
	}

	if cfg.Proxy != "" {
		proxyURL, err := url.Parse(cfg.Proxy)
		if err != nil || proxyURL.Host == "" {
			return fmt.Errorf("-proxy: %q is not a valid proxy URL", cfg.Proxy)
		}
		// Every http.Get / http.DefaultClient in the app goes through this
		if t, ok := http.DefaultTransport.(*http.Transport); ok {
			t.Proxy = http.ProxyURL(proxyURL)
		}
	}

	clientID = os.Getenv("ANILIST_CLIENT_ID")
	clientSecret = os.Getenv("ANILIST_CLIENT_SECRET")
	redirectURI = envOr("ANILIST_REDIRECT_URI", "http://localhost:8888/callback")
	if u, err := url.Parse(redirectURI); err == nil && u.Port() != "" {
		callbackPort = u.Port()
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// proxyFunc returns the configured proxy for libraries that take one explicitly
func proxyFunc() func(*http.Request) (*url.URL, error) {
	if cfg.Proxy == "" {
		return nil
	}
	u, err := url.Parse(cfg.Proxy)
	if err != nil {
		return nil
	}
	return http.ProxyURL(u)
}
