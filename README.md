# trains

Next Brisbane trains between two stations, with live delays, platforms and train positions, from Translink's open data. It's one Go binary with two modes:

```
$ trains central roma
Central station → Roma Street station   Fri 23 Oct, 06:35 AEST  (live, 15s old)

  DEPART  PLAT  ARRIVE  STATUS         SERVICE                                       RUN
  06:43   5     06:44   ● on time      Caboolture – Springfield → Springfield Cent…  DK10
  06:46   5     06:48   ● on time      Redcliffe Peninsula – Brisbane City → Roma …  D904
  06:47   2     06:49   ● on time      Shorncliffe – Cleveland → Cleveland           D814
  ...
```

```
$ trains serve          # web UI and JSON API on http://localhost:8080
```

Rail only (South East Queensland). Data © Translink, licensed [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).

## Build

Requires Go 1.26+ and Node 22+.

```sh
npm --prefix web ci
npm --prefix web run build        # writes web/dist, which the binary embeds
go build -o trains.exe ./cmd/trains
```

`go build` also works without the UI build: `trains serve` then shows a "UI not built" page and the API still works. `trains --version` prints the version: release builds are stamped with it, local builds report `dev+<commit>`.

Prebuilt binaries for Linux (amd64, arm64, armv7), macOS (amd64, arm64) and Windows (amd64, arm64) are attached to each [release](../../releases), and a multi-arch image is published to `ghcr.io/<owner>/<repo>`:

```sh
docker run -p 8080:8080 -v trains-cache:/home/nonroot/.cache ghcr.io/<owner>/<repo>:latest
```

## Query mode

```
trains <origin> <destination> [flags]

  -n, --count int     journeys to show (default 5)
      --at string     query time, RFC3339 or HH:MM today (default now)
      --direct        suppress one-change journeys
      --json          emit JSON (the same schema as /api/journeys)
      --no-color      disable colour (also: NO_COLOR, or any non-terminal stdout)
      --cache string  index cache dir (default <user cache dir>/trains)
```

- **Station names** are free text. Exact names win, then prefixes, word matches, and up to two typos: `central`, `roma st`, `indro` and `romma` all resolve. Ambiguous input lists the candidates: `south` matches South Bank and South Brisbane. Quote multi-word names, as in `trains "ferny grove" cleveland`.
- **One-change journeys** change at Bowen Hills, Roma Street, Central, Fortitude Valley, Eagle Junction, Northgate or Boggo Road. A change needs at least 4 minutes. Changes under 5 minutes are marked `⚠ tight`.
- **Status glyphs:** `●` on time, `▲` late, `▼` early, `✕` cancelled or not stopping. `○` means **no realtime data for that train**; it is never reported as on time.
- **Exit codes:**
  - `0` success.
  - `1` realtime unavailable; the schedule is still printed under a `schedule only` banner.
  - `2` usage error, or an ambiguous or unknown station.

## Serve mode

```
trains serve [flags]

  --addr string        listen address (default localhost:8080; use :8080 for other devices on your network)
  --dev                allow CORS from http://localhost:* (for the Vite dev server)
  --cache string       index cache dir
  --static-url URL     static GTFS zip (default Translink's SEQ_GTFS.zip)
  --feeds-url URL      GTFS-RT base URL; reads <URL>/TripUpdates/Rail, /VehiclePositions/Rail, /Alerts
                       (default Translink's SEQ realtime root)
```

| Endpoint | Returns |
|---|---|
| `GET /api/stations` | every rail station, with aliases |
| `GET /api/journeys?origin=&destination=&limit=&at=&direct=` | next journeys; `origin`/`destination` accept station IDs or free text |
| `GET /api/legs/{tripId}?date=YYYY-MM-DD&from=&to=` | one trip, every stop, with the live vehicle |
| `GET /api/shapes/{shapeId}` | simplified route polyline |
| `GET /api/healthz` | realtime feed status |

The wire format lives in `gtfsrail/wire` and is mirrored in `web/src/api/types.ts`. Background polling is fixed: TripUpdates and VehiclePositions every 15 s, Alerts every 60 s, however many clients connect.

UI development: run `trains serve --dev` and `npm --prefix web run dev`. Vite proxies `/api` to `localhost:8080`; set `TRAINS_API` to point it elsewhere.

## How a time is decided

When a time looks wrong, this is where to look. The code is `gtfsrail/legs.go` and `gtfsrail/journeys.go`; the design is in `SPEC.md` §3.5–3.6.

**The static timetable is the spine and realtime only overlays it.** Translink's realtime feed looks only about 50 minutes ahead, so most trains in a 5-row board have no realtime yet.

For each stop of a trip, in order:

1. **Observation table.** `trains serve` keeps every stop event it has seen today. It resets at 03:00, after the last 24:00+ trip has finished. These events are used first, because the feed later trims stops the train has already passed.
2. **Live update by sequence number**, but only if the update is at the *same station* as the timetable's stop. `REPLACEMENT` trips can be renumbered. For example, D510 was once re-sent with sequence 1 meaning Central, which is the timetable's stop 21. One sequence number can also carry two different stops. A stop the timetable doesn't know, such as 600350 (an Ipswich platform), can't be shown to be elsewhere, so it keeps its sequence, and the platform is shown as unknown.
3. **Live update by station**, for updates that didn't match their sequence.
4. **No update yet, before the first update:** the scheduled time, labelled `scheduled`.
5. **No update, after the last update:** the scheduled time plus the last delay, labelled `projected`.
6. **`SKIPPED`:** marked "not stopping". It does not reset the delay carried forward from earlier stops.

Each stop then gets two independent labels:

- **Position:** `passed`, `current` or `upcoming`. A fresh vehicle report (under 120 s old) decides this. Otherwise the clock decides.
- **Source:** `observed`, `predicted`, `projected` or `scheduled`. A stop the train has passed without a realtime time shows its scheduled time marked "not observed", never an extrapolation.

## Feed quirks the code handles

| Quirk | Handling |
|---|---|
| A trip starting after midnight arrives with the *calendar* date and a time under 24h, e.g. 24:02 on the 22nd's service is sent as 2026-10-23 00:02 | the update is matched to a run by its start instant (the nearest run within 12 h), not by date |
| Times exceed 24:00:00 (latest is 26:16) | times are stored as seconds since midnight of the service day; searches cover yesterday, today and tomorrow |
| Stale entities: trips 21 h old, a vehicle fix 6 h old | trips with every stop over 60 min past are dropped; vehicle fixes over 120 s old are marked stale |
| Four realtime stop IDs absent from every static feed | shown as `Unknown stop 600192` |
| Park Road station was renamed Boggo Road but kept the ID `place_parsta` | interchanges and aliases are checked by name at startup, with a warning if any no longer match |
| A new ETag on every ~10 s republish | conditional GETs still apply: `/api/healthz` counts the 304s as `notModified` |
| HTTP 429 under heavy load | the timetable falls back to the cached index; realtime degrades to `schedule only` |

## Layout

```
gtfsrail/                     library: Service, Journeys, Leg, Resolve, domain types
gtfsrail/wire/                JSON contract shared by serve mode and --json
gtfsrail/internal/gtfs/       static timetable parse and on-disk cache
gtfsrail/internal/gtfsrt/     realtime polling, decoding, observation table
gtfsrail/internal/stationlex/ free-text station matching
gtfsrail/internal/geo/        route shape simplification
cmd/trains/                   the binary: dispatch, query/, serve/
cmd/capture, cmd/mkfixture    developer tools for test fixtures
cmd/feedserver                serves testdata/ in Translink's URL layout, for offline runs
web/                          React UI (atomic design), embedded at build time
testdata/                     real timetable fixture and a raw realtime capture
scratch/                      the research behind SPEC.md
```

## Tests

```sh
go test ./...                         # offline; uses testdata/
go test -tags live -run Live ./...    # against the live feeds; needs scratch/SEQ_GTFS.zip
```

The merge-engine golden tests are in `gtfsrail/golden_test.go`. Each case reproduces something seen in the live feed. They build realtime as real GTFS-RT protobuf and decode it with the production decoder.

**Smoke tests** (`web/smoke/`, Playwright) drive a running server, one test per page. To run them fully offline against the committed fixtures:

```sh
go run ./cmd/feedserver &
TRAINS_NOW=2026-10-23T00:04:00+10:00 trains serve \
  --static-url http://localhost:8091/GTFS/SEQ_GTFS.zip --feeds-url http://localhost:8091/api/realtime/SEQ &
cd web && npx playwright install chromium
SMOKE_BASE_URL=http://localhost:8080 SMOKE_NOW=2026-10-23T00:04:00+10:00 npm run smoke
```

`TRAINS_NOW` pins the server clock inside the fixture's timetable (22 Oct to 13 Nov 2026); `SMOKE_NOW` pins the browser to match. Omit both, and the feed flags, to smoke-test against live data.

## CI and releases

`.github/workflows/ci.yml` runs in stages:

| Stage | Runs on | What |
|---|---|---|
| Version | every push and PR | semantic version from conventional commits |
| Unit + lint, Integration | every push and PR, in parallel | gofmt, `go vet`, strict TypeScript; `go test -race` split into package tests and the golden + HTTP tests |
| Build | every push and PR, after tests | branches and PRs: the Linux host binary; main: all 7 binaries |
| Smoke | main | the release Linux binary served from the fixtures, Playwright on every page at phone and desktop size |
| Live | main, informational | live-tagged Go tests and the smoke suite against Translink; never blocks a release |
| Container | main, after smoke | `linux/amd64`, `linux/arm64`, `linux/arm/v7` image pushed to ghcr, tagged `X.Y.Z`, `X.Y`, `X`, `sha-…` and `latest` |
| Release | main, after the image | tags `vX.Y.Z` and publishes the binaries, `SHA256SUMS` and grouped release notes |

Every commit on main bumps the version, decided by its conventional-commit type alone: `type!:` is major; `feat`, `refactor`, `revert` and `style` are minor; anything else is patch. Only the subject's type prefix counts, so a breaking change must be written `feat!:` or `fix(scope)!:`; a `BREAKING CHANGE:` footer is not read. Main runs queue rather than cancel, so each one sees the previous release's tag.
