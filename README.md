# SakuHaku

A terminal app for watching anime. Browse your [AniList](https://anilist.co)
lists or search for a show, pick a torrent from nyaa or AnimeTosho, and stream it
in mpv straight away. When you finish an episode, SakuHaku updates your AniList
progress for you.

Everything runs locally. Torrents are streamed through a small HTTP server on
`localhost` that mpv plays from, so an episode starts as soon as the first pieces
arrive.

## Features

- **AniList lists.** Currently watching, plan to watch, trending, popular this
  season and top rated, with high-resolution cover art drawn right in the terminal.
  The public lists are paginated.
- **Torrent search.** Searches nyaa and AnimeTosho for the romaji and English
  titles, de-duplicates the results, and shows size, seeders, leechers and the
  detected episode.
- **Filters.** Filter by episode (batches count if their range includes it) and
  by minimum seeders, and sort by seeders or size. Opening a show from *Currently
  watching* pre-filters to your next episode.
- **Streaming screen.** Shows playback position, how much of the file is
  buffered, a live piece map with a playhead, how much is ready to play ahead,
  speeds and peers.
- **mpv integration.** A bundled Lua script shows an AniList info card (title,
  episode, score, genres, studio, next airing, synopsis, your progress) and
  reports playback position back to SakuHaku.
- **Progress tracking.** Once you've watched 85% of an episode, your AniList
  progress is updated. Episodes you've already watched never lower it, rewatches
  stay `REPEATING`, and the last episode marks the show `COMPLETED`. If you stop
  partway, the next play resumes where you left off.
- **Download manager.** Download whole torrents (or keep one you streamed) with
  pause, resume, remove, delete, ETA and speeds.
- **Proxy and mirror support.** Use `-proxy` and `-nyaa` if a site is blocked
  where you are.

## Setup

### 1. Install the requirements

- [Go](https://go.dev/dl/) 1.25 or newer
- [mpv](https://mpv.io/installation/). VLC, ffplay and mplayer also work, but
  only mpv gets the info card, resume and progress tracking.
- A terminal with truecolor support (Windows Terminal, iTerm2, kitty, WezTerm,
  Alacritty, GNOME Terminal…) for the best-looking posters. Others fall back to
  256 colors.

### 2. Create an AniList API client (only needed to log in)

You can browse the public lists and search without an account. To see your own
lists and have progress synced, you need an AniList API client:

1. Go to <https://anilist.co/settings/developer> and click **Create New Client**.
2. Set the redirect URL to `http://localhost:8888/callback`.
3. Copy the client ID and secret into a `.env` file in the project folder:

```sh
cp .env.example .env
# then edit .env
```

```env
ANILIST_CLIENT_ID=12345
ANILIST_CLIENT_SECRET=your-secret
# ANILIST_REDIRECT_URI=http://localhost:8888/callback
```

Real environment variables work too, and take precedence over `.env`.

> Upgrading from an older checkout? Delete your old `credentials.go`, since
> credentials now come from the environment. Leaving it in place causes a
> "redeclared" build error.

### 3. Run it

```sh
go mod download
go run .
# or build a binary
go build -o sakuhaku . && ./sakuhaku
```

Press `l` to log in (your browser opens AniList), or `s` to browse without an
account. Your login is remembered in `~/.anilist_token`.

## Usage

### Keys

| Screen | Key | Action |
| --- | --- | --- |
| everywhere | `D` | download manager |
| everywhere | `q` / `ctrl+c` | quit (asks again if downloads are running) |
| lists | `j`/`k` or arrows | move |
| lists | `Tab` | next list (watching, planning, trending, season, top rated) |
| lists | `n` / `p` | next / previous page (public lists) |
| lists | `s` | search AniList |
| lists | `r` | refresh |
| lists | `Enter` | find torrents for the selected anime |
| lists | `L` | log out |
| torrents | `Enter` | stream in your player |
| torrents | `Space` / `d` | mark torrents / download the marked ones (or the current one) |
| torrents | `e` / `E` | filter by episode / clear the episode filter |
| torrents | `f` | cycle minimum seeders: 0, 1, 5, 10, 25, 50, 100 |
| torrents | `o` | sort by relevance, seeders or size |
| torrents | `n` / `p` | next / previous page |
| streaming | `w` | reopen the player (resumes where you stopped) |
| streaming | `s` | stop the player |
| streaming | `m` | mark the episode watched on AniList now |
| streaming | `d` | keep the whole torrent (download all of it) |
| downloads | `Enter` | watch |
| downloads | `Space` | pause / resume |
| downloads | `d` | download everything (for a streamed torrent) |
| downloads | `x` / `X` `X` | remove / remove and delete files |
| downloads | `o` | open the folder |
| mpv | `a` | toggle the AniList info card |

### Options

Every flag can also be set with an environment variable (or in `.env`).

| Flag | Env | Default | What it does |
| --- | --- | --- | --- |
| `-proxy URL` | `SAKUHAKU_PROXY` | none | Send AniList, torrent sites, posters and tracker announces through an `http://`, `https://` or `socks5://` proxy |
| `-nyaa URL` | `SAKUHAKU_NYAA_URL` | `https://nyaa.si` | nyaa base URL. Point this at a mirror if nyaa.si is blocked |
| `-animetosho URL` | `SAKUHAKU_ANIMETOSHO_URL` | `https://feed.animetosho.org` | AnimeTosho feed URL |
| `-download-dir DIR` | `SAKUHAKU_DOWNLOAD_DIR` | `~/Downloads/SakuHaku` | Where torrents are saved |
| `-player CMD` | `SAKUHAKU_PLAYER` | first found of mpv, vlc, ffplay, mplayer | Player to launch |
| `-no-tracking` | `SAKUHAKU_NO_TRACKING=1` | off | Don't update AniList progress |

```sh
# nyaa blocked by your ISP? use a mirror, a proxy, or both
go run . -nyaa https://nyaa.land -proxy socks5://127.0.0.1:1080
```

The proxy covers HTTP traffic and HTTP tracker announces. Peer-to-peer torrent
traffic is not proxied, so use a VPN if you need that.

### Where things are stored

| What | Where |
| --- | --- |
| Downloads and stream data | `~/Downloads/SakuHaku` (or `-download-dir`) |
| Piece completion database | `<user cache dir>/anilist-torrent-browser` |
| Poster cache | `~/.anilist_cli_cache` |
| Resume positions | `<user config dir>/sakuhaku/history.json` |
| AniList login | `~/.anilist_token` |

Streamed episodes stay in the download folder, so rewatching is instant. Remove
them from the download manager with `X` `X`.

## Architecture

SakuHaku is a single Go binary built on [Bubble Tea](https://github.com/charmbracelet/bubbletea)
(an Elm-style TUI framework) and [anacrolix/torrent](https://github.com/anacrolix/torrent).

```
                ┌───────────── Bubble Tea program (model / Update / View) ─────────────┐
 keypress ─────▶│ keyhandlers.go ──▶ state change + tea.Cmd                             │
                │ ui.go Update   ◀── tea.Msg results (lists, torrents, player, ticks)  │
                │ render.go View ──▶ header + viewport + footer                         │
                └──────┬──────────────────┬──────────────────────┬──────────────────────┘
                       │ tea.Cmd           │ tea.Cmd               │ tea.Cmd
                 alQueries.go /      nyaa.go (nyaa +         torrentclient/
                 auth.go             AnimeTosho search)      (anacrolix client, manager,
                 (AniList GraphQL)                           HTTP stream server)
                                                                   │  http://localhost:PORT/stream
                                                                   ▼
                                        player.go ──launches──▶ mpv + mpv/sakuhaku.lua
                                            ▲                         │
                                            └──── status.json ◀───────┘ (position, eof)
```

All slow work (HTTP, torrent metadata, the player process) runs in `tea.Cmd`s
off the UI goroutine. Their results come back as messages that `Update` handles,
so the UI never blocks. A one-second tick (`TorrentProgressMsg`) refreshes
download stats and reads the player's status file while anything is active.

### Files

| File | Responsibility |
| --- | --- |
| `main.go` | Loads `.env`, parses flags, runs the program, closes the torrent client on exit |
| `config.go` | Flags, env vars, proxy setup, AniList client credentials |
| `models.go` | Data types (AniList, torrents) and the `model` struct holding all UI state |
| `ui.go` | `Init`/`Update`: handles every message; header/footer; viewport sizing |
| `keyhandlers.go` | Key bindings for each screen |
| `render.go` | `View` content for each screen: split list/poster, torrents, downloads, streaming |
| `image.go` | Poster download/cache and half-block truecolor rendering |
| `alQueries.go` | AniList GraphQL client, paginated lists, progress updates |
| `auth.go`, `token.go` | AniList OAuth login and token storage |
| `search.go` | AniList search |
| `nyaa.go` | nyaa (RSS) and AnimeTosho (JSON) search, merge and de-duplicate |
| `episode.go` | Works out episode numbers and batch ranges from release names |
| `filters.go` | Episode, seeder and sort filters for torrent results |
| `streamHandler.go` | Starts streams, picks the file, decides when to sync AniList |
| `player.go` | Launches the player, writes the mpv info file, reads status, resume history |
| `mpv/sakuhaku.lua` | mpv script (embedded in the binary): info card and position reporting |
| `torrentclient/client.go` | anacrolix client setup, adding torrents, HTTP streaming server |
| `torrentclient/manager.go` | Download manager: tracking, stats, speed, pause/remove, piece maps |
| `utils.go` | Formatting helpers |

### How a stream works

1. `Enter` on a torrent calls `AddAsyncTagged`, which adds the magnet and waits
   (up to 2 minutes) for its metadata. The anime and episode ride along as a tag.
2. `playTorrent` picks the file: the wanted episode from a batch, or the largest
   video. The torrent's HTTP server serves that file with range support, and a
   responsive reader prioritises the pieces around wherever the player is reading.
3. `startPlayback` writes `info.json` and the Lua script to a temp dir and
   launches mpv with `--script` and environment variables pointing at them.
4. The Lua script writes `status.json` every 2 seconds. The progress tick reads
   it and, past 85%, sends the AniList update.
5. When mpv exits, the final position is stored in the resume history.

## Contributing

Contributions are welcome. A quick guide:

```sh
go build ./...      # build
go vet ./...        # vet
gofmt -l .          # should print nothing
go test ./...       # tests don't need network access
```

- **Tests.** The parser, filters, AniList updates (against a fake GraphQL
  server), flags, screen layout and the download manager (with a local torrent,
  no network) all have tests. Please add one for new behaviour.
- **UI work.** Keep blocking work in `tea.Cmd`s, never in `Update` or `View`. If
  a screen shows a list, make sure it still fits the terminal:
  `TestEveryScreenFitsTheTerminal` in `ui_test.go` catches overflow.
- **New screens.** Add a `ViewMode` in `models.go`, a render function in
  `render.go`, keys in `keyhandlers.go`, and header/footer text in `ui.go`.
- **New torrent sources.** Add a `searchX(query) ([]Torrent, error)` in `nyaa.go`
  and call it from `performTorrentSearch`.
- **mpv script.** Edit `mpv/sakuhaku.lua`. It's embedded at build time, so
  rebuild to test it. You can also run it directly:
  `SAKUHAKU_INFO=info.json SAKUHAKU_STATUS=status.json mpv --script=mpv/sakuhaku.lua video.mkv`.
- **Commits.** Keep them focused and describe the why. Branch off `main` and open
  a pull request.

### Ideas / roadmap

- Show a download-speed history graph on the streaming screen
- Support more torrent sources (SubsPlease RSS, TokyoTosho)
- Detect absolute episode numbering (e.g. One Piece batches) against AniList
  episode counts
- Discord rich presence

## Troubleshooting

- **Torrent search fails with "connection reset" or "forbidden".** nyaa is
  blocked on your network. Use `-nyaa` with a mirror, `-proxy`, or both.
- **"Fetching metadata" never finishes.** The torrent has no reachable peers.
  Pick one with more seeders (`f` filters them).
- **Posters look blocky or have odd colors.** Your terminal probably lacks
  truecolor support. Try another terminal, or set `COLORTERM=truecolor` if
  yours supports it but doesn't advertise it.
- **Login fails.** Check that `ANILIST_CLIENT_ID` and `ANILIST_CLIENT_SECRET`
  are set, and that the redirect URL on AniList is exactly
  `http://localhost:8888/callback`.
