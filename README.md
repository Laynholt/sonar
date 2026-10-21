# sonar

Desktop app (Wails v2: Go backend, React/Vite frontend) for looking up Spotify and Amazon Music links and search results, with a SpotiDownloader-style UI. No sign-in needed.

Status: stage 2 (Spotify + Amazon Music viewer). Nothing is downloaded yet.

## Features

- Paste a Spotify track, album, playlist or artist link to see its metadata and track list.
- Paste an Amazon Music track, album, playlist or artist link (any `music.amazon.*` host) to see the same view.
- Search Spotify or Amazon Music by text (switch in the search bar).
- Fetch history (recent fetches and a full history page).
- Themes and soft UI sounds, configurable in Settings.

## Amazon Music limits

Amazon is read as a guest (no sign-in) from the US catalog, so:

- Community playlists (`/user-playlists/...`) need an account and are not supported; search hides them.
- Search shows only the first page (up to 10 items per type), with no "load more".
- No previews.
- No disc numbers, label, plays, followers or listeners; empty counts are hidden.

## Proxy

Settings has a Proxy field (`host:port` or a full `http://`, `https://`, `socks5://` URL). If it is empty, sonar reads the environment: for https requests `HTTPS_PROXY`, else `HTTP_PROXY`, else `ALL_PROXY` (lowercase names work too). If any of those is set, the OS proxy is not consulted, and hosts matching `NO_PROXY` go direct. With none set, sonar uses the OS system proxy (Windows, macOS, Linux/GNOME). Loopback addresses always go direct.

A proxy set in Settings is never bypassed: if it is unreachable, the request fails with a "proxy ... is unreachable" error instead of retrying direct.

## Languages

English is built in; Russian ships as `frontend/src/locales/ru.json`. Settings > Language lists every `frontend/src/locales/*.json` it finds, and "System" follows the OS language.

To add a language: copy `frontend/src/locales/ru.json` to `<code>.json` (for example `de.json`), translate the values, set `"$name"` to the language's own name, and rebuild. No code changes are needed. Keys missing from a file fall back to English. Plural strings use the CLDR forms of the language (`one`, `few`, `many`, `other`).

Go error texts and Spotify data (titles, playlist descriptions) are not translated.

## Roadmap

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
