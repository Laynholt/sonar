# sonar

Desktop app (Wails + Go + React) that finds a Spotify track or album on Amazon Music.
Paste a Spotify link, get the matching Amazon Music link. No sign-in needed.

## How it works

- Spotify metadata comes from the public embed page (`open.spotify.com/embed/...`).
- Amazon Music is searched through the web player's guest search API. It is unofficial and US-catalog only, so it can break when Amazon changes the web player.
- Results are ranked by title and first artist; a remaster counts as the same recording, a remix or live take does not.

## Requirements

- Go 1.27+ (the Wails CLI is pinned in `go.mod` as a tool, no global install)
- Node.js 24 and pnpm 10 (`corepack enable` or `corepack pnpm`)
- WebView2 (built into Windows 10/11)

## Build

```powershell
go tool wails build
```

Output: `build/bin/sonar.exe`.

Dev mode with hot reload: `go tool wails dev`.

## Tests

```powershell
go test .
$env:SONAR_LIVE=1; go test -run Live -v .   # hits real Spotify and Amazon
```
