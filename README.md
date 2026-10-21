# sonar

Desktop app (Wails v2: Go backend, React/Vite frontend) for looking up Spotify links and search results, with a SpotiDownloader-style UI. No sign-in needed.

Status: stage 1 (shell + Spotify viewer). Nothing is downloaded yet.

## Features

- Paste a Spotify track, album, playlist or artist link to see its metadata and track list.
- Search Spotify by text.
- Fetch history (recent fetches and a full history page).
- Themes and soft UI sounds, configurable in Settings.

## Proxy

Settings has a Proxy field (`host:port` or a full `http://`, `https://`, `socks5://` URL). If it is empty, sonar reads the environment: for https requests `HTTPS_PROXY`, else `HTTP_PROXY`, else `ALL_PROXY` (lowercase names work too). If any of those is set, the OS proxy is not consulted, and hosts matching `NO_PROXY` go direct. With none set, sonar uses the OS system proxy (Windows, macOS, Linux/GNOME). Loopback addresses always go direct.

A proxy set in Settings is never bypassed: if it is unreachable, the request fails with a "proxy ... is unreachable" error instead of retrying direct.

## Roadmap

- Stage 2: Amazon Music.
- Stage 3: downloads via lucida.to.

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
```

## Credits

The UI is based on SpotiDownloader (MIT); see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
