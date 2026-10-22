# sonar

Desktop app (Wails v2: Go backend, React/Vite frontend) for looking up Spotify and Amazon Music links and search results, with a SpotiDownloader-style UI. No sign-in needed.

Status: stage 3a (downloads of Amazon Music sources through lucida and doubledouble).

## Features

- Paste a Spotify track, album, playlist or artist link to see its metadata and track list.
- Paste an Amazon Music track, album, playlist or artist link (any `music.amazon.*` host) to see the same view.
- Search Spotify or Amazon Music by text (switch in the search bar).
- Fetch history (recent fetches and a full history page).
- Download a track, album, playlist or artist from an Amazon Music source through lucida.to or doubledouble.top: Auto alternates between the two and skips a site that is down, or pick one fixed site. A Downloads page shows the queue.
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
- **Format:** both sites always deliver FLAC. MP3 320 or M4A AAC 320 are converted locally with FFmpeg, which must be in PATH or chosen in Settings (sonar does not download it).
- **Rate guard** (on by default): after 50 tracks in a row from one site, sonar warns that the site may ban you for a while. "OK, I'll wait" pauses all downloads for 30 minutes; "I don't care, continue" goes on. The limit can be changed or the guard switched off.

Limits:

- Only Amazon Music sources download for now; Spotify needs matching to Amazon (stage 3b).
- Both sites are unofficial and may break or rate-limit.
- At most 2 tracks download at once.
- No genre tags yet (stage 3c).

## Proxy

Settings has a Proxy field (`host:port` or a full `http://`, `https://`, `socks5://` URL). If it is empty, sonar reads the environment: for https requests `HTTPS_PROXY`, else `HTTP_PROXY`, else `ALL_PROXY` (lowercase names work too). If any of those is set, the OS proxy is not consulted, and hosts matching `NO_PROXY` go direct. With none set, sonar uses the OS system proxy (Windows, macOS, Linux/GNOME). Loopback addresses always go direct.

A proxy set in Settings is never bypassed: if it is unreachable, the request fails with a "proxy ... is unreachable" error instead of retrying direct.

## Languages

English is built in; Russian ships as `frontend/src/locales/ru.json`. Settings > Language lists every `frontend/src/locales/*.json` it finds, and "System" follows the OS language.

To add a language: copy `frontend/src/locales/ru.json` to `<code>.json` (for example `de.json`), translate the values, set `"$name"` to the language's own name, and rebuild. No code changes are needed. Keys missing from a file fall back to English. Plural strings use the CLDR forms of the language (`one`, `few`, `many`, `other`).

Go error texts and Spotify and Amazon data (titles, playlist descriptions) are not translated.

## Roadmap

- Stage 3b: download Spotify links by matching them to Amazon Music.
- Stage 3c: genre tags from chosic.

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
```

## Credits

The UI is based on SpotiDownloader (MIT); see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
