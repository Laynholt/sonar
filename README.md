# sonar

Desktop app (Wails v2: Go backend, React/Vite frontend) for looking up Spotify and Amazon Music links and search results, with a SpotiDownloader-style UI. No sign-in needed.

Status: stage 3c (downloads of Spotify and Amazon Music sources, genre tags, built-in FFmpeg installer).

## Features

- Paste a Spotify track, album, playlist or artist link to see its metadata and track list.
- Paste an Amazon Music track, album, playlist or artist link (any `music.amazon.*` host) to see the same view.
- Search Spotify or Amazon Music by text (switch in the search bar).
- Fetch history (recent fetches and a full history page).
- Download a track, album, playlist or artist through lucida.to or doubledouble.top: Auto uses both sites at once (one track per site) and skips a site that is down, or pick one fixed site. A Downloads page shows the queue.
- Spotify sources download by matching each track to Amazon Music (see below).
- Themes and soft UI sounds, configurable in Settings.

## Amazon Music limits

Amazon is read as a guest (no sign-in) from the US catalog, so:

- Community playlists (`/user-playlists/...`) need an account and are not supported; search hides them.
- Search shows only the first page (up to 10 items per type), with no "load more".
- Links from other Amazon stores (e.g. `music.amazon.co.uk`) only open if the release is in the US catalog.
- No previews.
- No disc numbers, label, plays, followers or listeners; empty counts are hidden.

## Downloads

Settings > Downloads:

- **Folder** (default: `Music/sonar` in your home folder) and **folder / file name templates**, with a live preview.
- **Source:** Auto, lucida or doubledouble.
- **doubledouble check:** doubledouble asks for a Cloudflare check now and then. "Pass check" opens a visible Chrome or Edge window on the site; you pass the check yourself, sonar only reads the resulting token, then closes the window. Needs Chrome or Edge installed.
- **Format:** both sites always deliver FLAC. MP3 320 or M4A AAC 320 are converted locally with FFmpeg. sonar looks for it in the path chosen in Settings, then in an `ffmpeg` folder next to `sonar.exe`, then in PATH. If it is missing, sonar offers to install it (Windows only): it downloads the latest `BtbN/FFmpeg-Builds` release from GitHub (about 90 MB), checks it against the release checksums and unpacks `ffmpeg.exe` and its DLLs into the chosen folder.
- **Genres:** on by default. sonar asks chosic.com for up to 5 genres by the Spotify track ID (Amazon tracks are found on Spotify by ISRC) and writes them to the file. "Only one genre" keeps the first. If chosic fails, the file is saved without a genre.
- **Rate guard** (on by default): each site has its own counter. After 50 tracks from one site, sonar warns that the site may ban you for a while. "Wait" pauses only that site (15 min to 2 h, 30 min by default) while the other keeps going; "Continue anyway" goes on. The limit can be changed or the guard switched off.

Spotify matching: a confident match downloads at once; an exact ISRC match counts as confident. A doubtful or missing match waits for you: click the row to pick one of the Amazon candidates (with previews) or paste an Amazon Music link. An album is matched as a whole, an artist as their full discography plus popular tracks from releases by others.

The unfinished queue is saved: on the next start sonar offers to continue it, and a site pause that has not run out keeps counting down. The download card's X collapses it to a round progress badge while a run is active; click the badge to bring the card back.

Limits:

- Both sites are unofficial and may break or rate-limit.
- At most 2 tracks download at once (one per site).
- Matching uses titles and artists; compilations and re-recordings may need your choice.

## Proxy

Settings has a Proxy field (`host:port` or a full `http://`, `https://`, `socks5://` URL). If it is empty, sonar reads the environment: for https requests `HTTPS_PROXY`, else `HTTP_PROXY`, else `ALL_PROXY` (lowercase names work too). If any of those is set, the OS proxy is not consulted, and hosts matching `NO_PROXY` go direct. With none set, sonar uses the OS system proxy (Windows, macOS, Linux/GNOME). Loopback addresses always go direct.

A proxy set in Settings is never bypassed: if it is unreachable, the request fails with a "proxy ... is unreachable" error instead of retrying direct.

## Languages

English is built in; Russian ships as `frontend/src/locales/ru.json`. Settings > Language lists every `frontend/src/locales/*.json` it finds, and "System" follows the OS language.

To add a language: copy `frontend/src/locales/ru.json` to `<code>.json` (for example `de.json`), translate the values, set `"$name"` to the language's own name, and rebuild. No code changes are needed. Keys missing from a file fall back to English. Plural strings use the CLDR forms of the language (`one`, `few`, `many`, `other`).

Go error texts and Spotify and Amazon data (titles, playlist descriptions) are not translated.

## Roadmap

All three planned stages are done. Deferred: the SpotiDownloader utilities (audio converter, audio analysis, file manager).

## Requirements

- Go 1.27+ (the Wails CLI is pinned in `go.mod` as a tool, no global install)
- Node.js and pnpm via Corepack (`corepack pnpm ...`)
- WebView2 on Windows (built into Windows 10/11)

## Run and build

```powershell
go tool wails dev      # hot reload
go tool wails build    # output: build/bin/sonar.exe
```

## Tests

```powershell
go test ./...
cd frontend; corepack pnpm test
$env:SONAR_LIVE=1; $env:HTTPS_PROXY="http://127.0.0.1:10808"; go test -run Live -v ./internal/spotify/   # hits real Spotify; proxy only if you need one
$env:SONAR_LIVE=1; go test -run Live -v ./internal/lucida/ ./internal/amazon/   # real lucida and Amazon downloads
$env:SONAR_LIVE=1; go test -run Live -v ./internal/genre/ ./internal/ffinstall/   # real chosic genres and the FFmpeg release on GitHub
```

## Credits

The UI is based on SpotiDownloader (MIT); see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
