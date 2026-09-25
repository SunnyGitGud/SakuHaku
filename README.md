## Kiri ki mkc

To use the repository : 

`go mod tidy`

`go run .`


# Add rename `credentials.txt` file to `credentials.go`

## Posters

Covers are fetched in the background at AniList's highest resolution and drawn with
truecolor half blocks (falls back to 256 colors if your terminal doesn't support it,
force truecolor with `COLORTERM=truecolor`). Cached in `~/.anilist_cli_cache`.

## Download manager

Files are saved to `~/Downloads/SakuHaku` (override with `SAKUHAKU_DOWNLOAD_DIR`).

| Where | Key | Action |
| --- | --- | --- |
| anywhere | `D` | open the download manager |
| torrent results | `Enter` | stream in mpv/vlc |
| torrent results | `Space` / `d` | mark torrents / download marked (or current) |
| download manager | `Enter` | watch |
| download manager | `Space` | pause / resume |
| download manager | `d` | keep a streamed torrent (download all of it) |
| download manager | `x` / `X X` | remove / remove and delete files |
| download manager | `o` | open the folder |


Check for `debug.log`: 

If 

`Nyaa error: Get "https://nyaa.si/?page=rss&q=DAN+DA+DAN+Season+2&c=1_2&f=0": read tcp 192.168.1.9:63818->186.2.163.20:443: wsarecv: An existing connection was forcibly closed by the remote host.`

Then that's sad.... Idk still have to fix zzzz gn.

# add loading screen at every tea.msg update doneX
## add data streaming info screen on playing a torrent 
## add add episode and seeder filters 
## add pagination to hard codded anilist lists 
## edit mpv config with a script to show relevent details on the player pulling from anilist api
## add proxy site arguemnt to the program


## for future add timestamp tracking to mpv to dinamically update anilist anime status 
## todo just make the program take an arguement of proxy url


