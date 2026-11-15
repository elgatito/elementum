Elementum [![Build Status](https://gitlab.com/elgatito/elementum/badges/master/pipeline.svg?ignore_skipped=true)](https://gitlab.com/elgatito/elementum)
======

Elementum is a torrent finding and streaming engine: it searches for a movie or an episode, picks a torrent, and streams the file over HTTP while it downloads. It keeps a library, follows watched state and resume points, and syncs with Trakt, Simkl, MDBList and Scrob.

It started as a Kodi add-on, and still works as one. It is also an application on its own: one process with an HTTP server that any client can drive.

Fork of the great [Pulsar daemon](https://github.com/steeve/pulsar) and [Quasar daemon](https://github.com/scakemyer/quasar).

# Ways to run it

| Mode | How | Settings come from |
| --- | --- | --- |
| Kodi add-on | Install [plugin.video.elementum](https://github.com/elgatito/plugin.video.elementum). The add-on starts Elementum (as a binary or a shared library) next to Kodi and stops it with Kodi. | Kodi add-on settings |
| Standalone | Start the binary yourself with `-configPath`. It keeps running without Kodi and without a parent process. | A JSON or YAML file |
| Container | Elementum in an image, see [elementum-docker](https://github.com/ElementumOrg/elementum-docker) for its setup. Set `-serverExternalIP` when the container sits behind NAT. | As the image is set up: a file, or a remote Kodi |
| Server for client apps | Any of the above. Clients such as the Samsung TV app ([elementum-tizen](https://github.com/ElementumOrg/elementum-tizen)) browse, search and play through the [JSON API](#json-api-api). | As the mode it runs in |
| Remote Kodi | Elementum on one machine, Kodi with the add-on on another: `-remoteHost` / `-remotePort` point at Kodi. | Kodi add-on settings |

Everything is served on one port (`-localPort`, default `65220`): the JSON API under `/api`, the routes the Kodi add-on uses, the torrent Web UI under `/web`, and the files being streamed.

A minimal standalone start:

```
./elementum -configPath=/data/elementum/settings.json -localLogin=me -localPassword=secret
```

The settings file is created with defaults when it does not exist. See [Settings from a file](#settings-from-a-file) for the details and `./elementum -help` for all flags (data, download and library folders, network interfaces, logging).

## What still needs Kodi

- **Kodi provider add-ons**, if you prefer them. Elementum searches trackers itself (the providers of Burst, public and private, see `providers/burst` and `/api/providers`); the `providers_source` setting picks this built-in search, Kodi add-ons, or both. By default the built-in search is used when no Kodi is connected.
- **The Kodi library**: scanning, cleaning and watched sync with Kodi's database.
- **Playback inside Kodi.**

Browsing, search of movies and shows, torrent search, tracker lists and device login, the Elementum library, torrents, and playback in a client app work without Kodi.

# JSON API (`/api`)

The API for client apps and integrations: everything a client needs to browse, search, manage the library, and play.
It runs on the same HTTP server and port as the Kodi routes, and reuses the same logic (TMDB, trackers, library, torrents).
There is one unversioned contract, so update Elementum and a client built against it together.
The contract lives in `api/json/openapi.yaml`; regenerate its Go bindings with `go generate ./api/json/...`.

- Swagger UI: `/api/docs`. The OpenAPI spec (the contract, with examples): `/api/openapi.yaml`.
- **Auth:** HTTP Basic with the existing `-localLogin` / `-localPassword` credentials (the ones the Kodi routes use). With no credentials set, the API is open. Failures are JSON `401` without a `WWW-Authenticate` header, so browsers show no login popup. Errors are always `{"error": {"code", "message"}}`.
- **CORS:** every origin is allowed (`Access-Control-Allow-Origin: *`, `GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS`), and `X-UI-Epoch` is exposed.
- **Signed stream links:** `GET /api/stream/{id}/{file_index}` serves the media bytes (with Range) and is authorized by the link itself, not by Basic auth: the link carries an expiry and an HMAC-SHA256 signature made with a per-process key. Use the `stream_url` of the playback session as is. Expired, tampered or stopped-session links get `403`.

## Endpoint groups

| Group | Paths |
|---|---|
| system | `/system/info` (version, `auth_required`, language, tracker, `kodi_connected`, `settings_source`) |
| catalog, search | `/movies/lists/{list}`, `/movies/genres`, `/movies/{tmdb_id}`, `/shows/...` (seasons, episodes), `/search` |
| library | `/library/movies`, `/library/shows`, `/library/{media}/{tmdb_id}`, `/watched/{kind}` |
| tracker, Trakt | `/trakt/{media}/lists/{list}`, `/tracker/{media}/watchlist`, `/tracker/{media}/collection`, `/tracker/shows/progress`, `/tracker/lists`, `/tracker/lists/{list_id}/items` |
| streams | `/movies/{tmdb_id}/streams`, `/shows/.../episodes/{episode}/streams` (provider search) |
| playback, stream | `/playback`, `/playback/{id}`, `/playback/{id}/progress`, `/stream/{id}/{file_index}` |
| torrents | `/torrents`, `/torrents/{info_hash}` (+ `pause`, `resume`) |
| people | `/people/{tmdb_id}`, `/people/{tmdb_id}/credits` |
| providers | `/providers`, `/providers/{id}` (trackers of the built-in search: enable, credentials, mirror), `/providers/options`, `/providers/options/{id}` (filters, sizes, result limits), `/providers/cookie-sync/login` (GitHub device login) |
| UI events | `/ui/events`, `/ui/requests/{id}` |

## Built-in torrent search

Elementum searches trackers itself, with the provider definitions of the Burst add-on (`providers/burst`). `providers_source` ("Search torrents with" in the add-on settings) picks this search, Kodi provider add-ons, or both; without a Kodi the built-in search is used.

Everything of it is in `<profile>/providers/`:

| File | What |
|---|---|
| `providers.json` | Optional. Your own complete set of providers, used **in place of** the bundled one. |
| any other `*.json` | Optional. Additions, merged over the set key by key in file name order: a new provider, or single fields of an existing one. |
| `providers.default.json` | The bundled set, rewritten at every start. Copy from it; do not edit it. |
| `settings.json` | General options (filters, sizes, languages, cookie sync) and each provider's settings (enabled, login, mirror, cookies). Private: it holds passwords. |
| `cookies.json`, `icons/` | The cookies sites have set, and provider icons. |

Changing one field of a bundled provider, and adding a provider, in `my.json`:

```json
{
  "yts": { "base_url": "https://yts.example/api/v2/list_movies.json?query_term=QUERY" },
  "mine": {
    "name": "Mine", "color": "FF00FF00", "language": "en", "separator": "+",
    "base_url": "https://mine.example/search/QUERY",
    "movie_keywords": "{title} {year}", "tv_keywords": "{title} s{season:2}e{episode:2}",
    "parser": {
      "row": [{ "css": "table.results > tbody > tr" }],
      "name": { "css": "td:nth-child(1) a" },
      "torrent": { "css": "td:nth-child(1) a", "attr": "href" },
      "size": { "css": "td:nth-child(2)" },
      "seeds": { "css": "td:nth-child(3)" },
      "peers": { "css": "td:nth-child(4)" }
    }
  }
}
```

- A provider of your own is on at once; bundled ones are switched on in the settings.
- `parser` fields are selector specs: CSS (`css`, with `attr` for an attribute and `nth` for the n-th match), or Burst's counting form (`tag`, `select`, `nth`), and ops around them such as `{"op": "format", "format": "https://mine.example%s", "arg": {...}}`, `regex`, `split`, `replace`, `concat`. `providers.default.json` has an example of each under `builtin`.
- Burst's Python expressions are not run. A file in Burst's own format works when it has the `builtin` part that Burst's `scripts/builtin.py` writes.
- Settings: in Kodi, "Built-in search settings" in Elementum's main menu; for other clients `/api/providers`, `/api/providers/options` and `/api/providers/cookie-sync/login`. On the first search from Kodi, Elementum offers to import the Burst add-on's settings.
- Cookie sync: with a GitHub gist kept by a browser extension, cookies are read before a search and the ones sites changed are written back. Sign in with the device code from the settings dialog or the API.

## UI host

Elementum's dialogs, notifications, confirmations, lists, text prompts and progress go through a UI host interface. Kodi is one host. The other serves HTTP clients:

- `GET /api/ui/events?client_id=...` is a long-poll: it returns as soon as an event is queued, or `204` after `wait` seconds. Pass the last `seq` as `since` on the next call.
- `POST /api/ui/requests/{id}` answers a question (`yes`, `selected`, `text`) or cancels it / a progress dialog.
- A question goes to every connected client and the first answer wins (`410` for the others). Elementum asks Kodi only when no client is connected.
- Texts are translated on the server, into the client's `lang`. The translations are built into the binary (a copy of the addon's strings, refreshed with `go generate ./ui`), so they are there without the addon; when the addon folder is present its own files are used.

## Settings from a file

By default Elementum reads its settings from Kodi. With `-configPath=<file>` (or the `ELEMENTUM_CONFIG_PATH` environment variable, used when the flag is empty) it reads them from a file instead, and never asks Kodi for settings:

- The API login does not come from the settings file: use `-localLogin` / `-localPassword` (in Kodi, the addon's Authorization "Remote host login/password", ids `remote_login` / `remote_password`, passed to the binary as those flags).
- The file is JSON (`.json`) or YAML. A JSON file has a `"Version": 1` field (`settings-file-v1`); a newer version is rejected. `Info.ID`, `Info.Profile`, `Platform` and `Settings` are required.
- If the file does not exist, a default one is created (mode `0600`) and a warning is logged. The addon writes the file for you when it starts the binary (plugin.video.elementum 0.1.120 and later).
- Developers: the defaults without `-addonPath` come from `config/default_settings.xml`, a copy of the addon's `resources/settings.xml`. Refresh it with `ELEMENTUM_ADDON_PATH=<addon checkout> go generate ./config` (default `../plugin.video.elementum`); it is not run by the Makefile or Docker builds.
- The file is polled every 2 s. A valid change reloads the settings; an invalid file is logged and the previous settings are kept. `GET /reload` re-reads it at once.
- Write-back: tokens and other values Elementum changes at runtime (Trakt and OpenSubtitles tokens, expiries) are written into the file atomically (`.json` only), and into Kodi when it is connected. A change that only touches such keys does not restart services.
- `GET /api/system/info` reports `settings_source` (`file` or `kodi`) and `kodi_connected`.
- What works without Kodi in file mode is listed under [What still needs Kodi](#what-still-needs-kodi).

## Security

- Set a login (`-localLogin` / `-localPassword`) on any network you do not fully trust. Without credentials anyone who can reach the port can use it.
- CORS allows any origin (`*`) on all Elementum routes, not just `/api` (for example `/shutdown` and `/playuri`).
- `/files/` has no login at all.
- UI notifications sent to HTTP clients can contain file paths or URLs from error messages.
- The settings file holds tokens and passwords: keep it private (it is created `0600`), and do not share it.

# Supported platforms

Each platform is built as a binary (`make <platform>`) and a shared library (`make <platform>-shared`):

| Platform | Binary folder | Minimum OS |
| -------- | ------------- | ---------- |
| Windows x64 / x86 | `windows_x64`, `windows_x86` | Windows 10 / Windows Server 2016 |
| Linux x64 / x86 | `linux_x64`, `linux_x86` | glibc 2.28 (Debian 10, Ubuntu 20.04, RHEL 8), kernel 3.2 |
| Linux arm64 | `linux_arm64` | glibc 2.28, kernel 3.2 |
| Linux armv7 (hard-float / softfp) | `linux_armv7`, `linux_armv7_softfp` | glibc 2.28, kernel 3.2 |
| Linux armv6 (Raspberry Pi 1 / Zero) | `linux_armv6` | glibc 2.10, kernel 3.2 |
| Android arm / arm64 / x86 / x64 | `android_arm`, `android_arm64`, `android_x86`, `android_x64` | Android 5.0 (API 21) |
| macOS Intel | `darwin_x64` | macOS 13 Ventura |
| macOS Apple Silicon | `darwin_arm64` | macOS 13 Ventura |

# Development

## Easy development environment set up

There is an easy way to prepare local environment for running/compiling Elementum.

1. Clone `libtorrent-go` repository:
```
git clone git@github.com:ElementumOrg/libtorrent-go.git
```
2. Run `make local-env` that will create `local-env` directory will all required components.
```
make local-env
```
3. Modify `LOCAL-ENV` variable in `test_build.sh` to use `local-env` directory:
```
  export LOCAL_ENV=$GOPATH/src/github.com/ElementumOrg/libtorrent-go/local-env/
```

Or you can apply environment variables and compile in any other way:
```
  export LOCAL_ENV=$GOPATH/src/github.com/ElementumOrg/libtorrent-go/local-env/
  export PATH=$PATH:$LOCAL_ENV/bin/
  export PKG_CONFIG_PATH=$LOCAL_ENV/lib/pkgconfig
  export SWIG_LIB=$LOCAL_ENV/share/swig/4.1.1/
```

## Setting up Go workspace for using local environment for libtorrent-go

By default Go will fetch `libtorrent-go` module from Github. 
To use local version of `libtorrent-go` we can set up the Go workspace, by adding `go.work` file inside `elementum directory:

```

go 1.26.0

use (
	.
	../libtorrent-go
)

// Enable for local development of libtorrent-go
replace github.com/ElementumOrg/libtorrent-go v0.0.0 => ../libtorrent-go

```

That would expect `libtorrent-go` folder to be available at `../libtorrent-go`.
That would be also used when running compilation with `test_build.sh`, but would not work with `Docker` runs, as it is forced to ignore Go workspaces.

## How to build

1. Build the [cross-compiler](https://github.com/ElementumOrg/cross-compiler) images,
    or alternatively, pull the cross-compiler images from [Docker Hub](https://hub.docker.com/r/elementumorg/cross-compiler):

    ```
    make pull-all
    ```

    Or for a specific platform:
    ```
    make pull PLATFORM=android-x64
    ```

2. Set GOPATH

    ```
    export GOPATH="~/go"
    ```

3. go get

    ```
    go get -d github.com/elgatito/elementum
    ```

    For Windows support, but required for all builds, you also need:

    ```
    go get github.com/mattn/go-isatty
    ```

4. Build libtorrent-go libraries:

    ```
    make libs
    ```

5. Make specific platforms, or all of them:

    Linux-x64
    ```
    make linux-x64
    ```

    Darwin-x64 / Darwin-arm64 (Apple Silicon)
    ```
    make darwin-x64
    make darwin-arm64
    ```

    Windows
    ```
    make windows-x86
    ```

    All platforms (4 builds in parallel; use `make JOBS=1` to build one at a time with full live output)
    ```
    make
    ```

## Find memory leaks

To find memory leaks, we can use Valgrind or use Sanitizers.

```
/bin/bash test_build.sh sanitize
```
This will build the binary with enabled sanitizer, so just run it and wait for errors in the console.

```
valgrind --leak-check=full ./elementum -disableBackup
```
This will run the binary with Valgrind. When you cose the process, Valgrind will show statistics.
It is better to use usual binary, without sanitizer, and add backup disable options, as well as disable Kodi library integration. Or it will take a lot of time/CPU at the startup (unless you do need it).

## How to release

Release of a binary part of Elementum means compiling binaries for all platforms and putting them into <https://github.com/elgatito/elementum-binaries> repository (if we run on the git tag).

Release is done with `release.sh` script.