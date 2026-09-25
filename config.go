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
	// SubsPleaseURL and TokyoToshoURL are the base URLs of those sites
	SubsPleaseURL string
	TokyoToshoURL string
	// Sources limits which sites are searched (empty = all)
	Sources []string
	// DownloadDir is where torrents are saved
	DownloadDir string
	// Player is the preferred video player command (auto-detected if empty)
	Player string
	// NoTracking disables updating AniList progress from playback
	NoTracking bool
	// DiscordClientID is the Discord application used for Rich Presence
	DiscordClientID string
	// NoDiscord turns Rich Presence off
	NoDiscord bool
	// Watch together: our name, the port rooms listen on, the address guests
	// should use (for port forwarding / VPNs) and a link to join at startup
	Name     string
	RoomPort string
	RoomAddr string
	JoinLink string
}

var cfg = Config{
	NyaaURL:       "https://nyaa.si",
	AnimeToshoURL: "https://feed.animetosho.org",
	SubsPleaseURL: "https://subsplease.org",
	TokyoToshoURL: "https://www.tokyotosho.info",
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
	fs.StringVar(&cfg.SubsPleaseURL, "subsplease", envOr("SAKUHAKU_SUBSPLEASE_URL", cfg.SubsPleaseURL),
		"SubsPlease base URL")
	fs.StringVar(&cfg.TokyoToshoURL, "tokyotosho", envOr("SAKUHAKU_TOKYOTOSHO_URL", cfg.TokyoToshoURL),
		"TokyoTosho base URL")
	sources := fs.String("sources", envOr("SAKUHAKU_SOURCES", "animetosho,nyaa,subsplease,tokyotosho"),
		"comma separated torrent sites to search")
	fs.StringVar(&cfg.DownloadDir, "download-dir", os.Getenv("SAKUHAKU_DOWNLOAD_DIR"),
		"where downloads are saved (default ~/Downloads/SakuHaku)")
	fs.StringVar(&cfg.Player, "player", os.Getenv("SAKUHAKU_PLAYER"),
		"video player to use: mpv, vlc, ffplay or mplayer (default: first one found)")
	fs.BoolVar(&cfg.NoTracking, "no-tracking", os.Getenv("SAKUHAKU_NO_TRACKING") != "",
		"don't update your AniList progress when you finish an episode")
	fs.StringVar(&cfg.DiscordClientID, "discord-client-id", envOr("SAKUHAKU_DISCORD_CLIENT_ID", os.Getenv("DISCORD_CLIENT_ID")),
		"Discord application ID for Rich Presence (see README)")
	fs.BoolVar(&cfg.NoDiscord, "no-discord", os.Getenv("SAKUHAKU_NO_DISCORD") != "",
		"don't show what you're watching on Discord")
	fs.StringVar(&cfg.Name, "name", envOr("SAKUHAKU_NAME", defaultName()),
		"your name in watch-together rooms")
	fs.StringVar(&cfg.RoomPort, "room-port", envOr("SAKUHAKU_ROOM_PORT", "0"),
		"TCP port for hosting rooms (0 = random; set one to port-forward it)")
	fs.StringVar(&cfg.RoomAddr, "room-addr", os.Getenv("SAKUHAKU_ROOM_ADDR"),
		"address guests reach you at, e.g. your public IP or Tailscale name (default: LAN IP)")
	fs.StringVar(&cfg.JoinLink, "join", "", "join a watch-together room link on startup")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg.Sources = nil
	for _, s := range strings.Split(*sources, ",") {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		known := false
		for _, src := range allSources() {
			known = known || src.id == s
		}
		if !known {
			return fmt.Errorf("-sources: unknown source %q (have animetosho, nyaa, subsplease, tokyotosho)", s)
		}
		cfg.Sources = append(cfg.Sources, s)
	}

	for _, u := range []*string{&cfg.NyaaURL, &cfg.AnimeToshoURL, &cfg.SubsPleaseURL, &cfg.TokyoToshoURL} {
		*u = strings.TrimRight(*u, "/")
	}
	for name, u := range map[string]string{"-nyaa": cfg.NyaaURL, "-animetosho": cfg.AnimeToshoURL,
		"-subsplease": cfg.SubsPleaseURL, "-tokyotosho": cfg.TokyoToshoURL} {
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
