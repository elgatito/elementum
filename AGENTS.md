# Repository Guidelines

## Project Structure & Module Organization
`main.go` is the daemon entrypoint. Core Go packages live at the repository root:
- `api/` HTTP and route handlers.
- `bittorrent/` torrent session, player, and filesystem integration.
- `providers/` provider search and payload handling.
- `xbmc/` Kodi/XBMC integration types and RPC bindings.
- `util/` shared helpers (time, files, tracing, networking).

Build outputs are written to `build/<os>_<arch>/`. Operational fixtures and manual test assets are in `_test/`. Supporting runtime pieces are in `config/`, `cache/`, and `database/`.

## Build, Test, and Development Commands
- `make pull PLATFORM=linux-x64` downloads a single cross-compiler image.
- `make pull-all` pulls images for all supported platforms.
- `make linux-x64` builds the Linux x64 binary into `build/linux_x64/`.
- `make linux-x64-shared` builds the shared-library variant.
- `make` builds all platform targets defined in `Makefile`.
- `make zip PLATFORM=linux-x64` packages a platform artifact zip.
- `./test_build.sh local restart-local` builds locally and copies binaries into local Kodi addon paths.
- `/bin/bash test_build.sh sanitize` builds with Address/Leak Sanitizers for memory debugging.

## Coding Style & Naming Conventions
This is a Go codebase; follow standard Go formatting and naming.
- Run `gofmt -w` on changed files before review.
- Use lowercase package names and `CamelCase` for exported identifiers.
- Keep filenames descriptive and consistent with existing patterns (for example, `torrent_file.go`, `xbmcgui.go`).
- Keep platform-specific code split by suffix files (for example, `*_all.go`, `*_arm.go`).

## Testing Guidelines
Automated unit tests are currently sparse (no committed `*_test.go` files yet). For new logic, prefer table-driven tests next to the changed package and run `go test ./...`. For integration-sensitive changes, run at least one target build (for example, `make linux-x64`) and include a local smoke check using `test_build.sh`.

## Commit & Pull Request Guidelines
Recent history favors short, imperative subjects (for example, `Fix ...`, `Update ...`, `Add ...`) and issue references like `(#172)`. Keep commits focused and avoid leaving `WIP` commits in the final PR.

PRs should include:
- What changed and why.
- Affected platforms (`linux-x64`, `windows-x86`, etc.).
- Verification steps/commands you ran.
- Linked issue(s) and logs/screenshots when behavior changes are user-visible.

## Security & Configuration Tips
Do not commit secrets (for example, `GH_TOKEN`, FTP credentials, local paths). Use environment variables for local build/release configuration and keep machine-specific values out of tracked files.
