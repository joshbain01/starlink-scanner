# pp-starlink

Agent-native network + RF diagnostics for Starlink. Correlates packet drops and jitter spikes with dish telemetry, RF signal quality, and satellite look-angles in a single SQL query so an AI agent can diagnose the root cause in one execution loop.

## Developer quickstart

This section is the shortest path from clone to a working local environment.

### One-command bootstrap

```bash
make bootstrap
```

### One-command update + start prep

```bash
make sync-start
```

This target pulls `origin/master`, bootstraps/builds the environment, initializes the DB, and prompts for `lat`/`lon` if location is missing.

For non-interactive use:

```bash
make sync-start LAT=47.6062 LON=-122.3321
```

This target creates/updates `venv`, installs lockfile dependencies, and installs the package in editable mode.

By default it creates a minimal embedded web placeholder if UI assets are not built yet.

To also attempt binary build during bootstrap:

```bash
make bootstrap BOOTSTRAP_BUILD=1
```

To include web asset build as part of bootstrap:

```bash
make bootstrap BOOTSTRAP_BUILD=1 BOOTSTRAP_WEB=1
```

### One-command cleanup

```bash
make cleanup
```

Removes local generated artifacts (`bin`, `web/node_modules`, `.athena/output`, `__pycache__`) and restores tracked cache files.

### 1) System prerequisites

- Python 3.12+
- Go toolchain
- Node.js + npm (used by `make build` for web assets)
- Docker + Docker Compose (for production-like runs)
- System packages:
	- `protobuf-compiler`
	- `grpcurl`
	- `traceroute`
	- `iputils-ping`
	- `rtl-sdr` (for RF listener / `rtl_power`)

### 2) Create a Python venv and install dependencies

```bash
python3.13 -m venv venv
venv/bin/pip install -r requirements.lock -r requirements-dev.lock
venv/bin/pip install -e . --no-deps
```

### 3) Build binaries

```bash
make build
```

This generates:

- `bin/pp-starlink`
- `bin/e2e`

### 4) Run locally

```bash
./bin/pp-starlink init
./bin/pp-starlink status --json
./bin/pp-starlink set-location --lat 47.6062 --lon -122.3321
./bin/pp-starlink daemon
```

In another terminal:

```bash
./bin/pp-starlink insights --compact
./bin/pp-starlink predict-window --duration 60
```

### 5) Run with Docker Compose

```bash
docker compose up --build -d
docker compose logs -f pp-starlink-engine
docker compose logs -f rf-listener
```

### 6) Run tests and checks

Athena baseline suite:

```bash
venv/bin/athena test tests/athena
```

Update baselines after intentional changes:

```bash
venv/bin/athena test -b tests/athena
```

Live-dish smoke test:

```bash
./bin/e2e
```

`go test ./...` is expected to find little or no unit-test coverage in this repo.

---

## How it works

Two background daemons write into a shared SQLite time-series database every 15 seconds:

- **pp-starlink-engine** (Go) — polls the dish gRPC API for uptime, obstruction, and alert flags; concurrently pings the local gateway, Starlink POP, and `1.1.1.1` to measure per-hop jitter and packet loss; and (when a location is configured) calculates the azimuth and elevation of the highest Starlink satellite using a Keplerian orbital propagator against live TLE data from CelesTrak.
- **rf-listener** (Python) — drives `rtl_power` against a Ku-band LNB to track beacon SNR and wideband noise floor.

`pp-starlink insights` joins the two streams on timestamp and prints a classified drop cause:

| RF condition at drop time | Diagnosis |
|---|---|
| Beacon SNR dipped > 3 dB below 24h avg | Physical Blockage / Canopy Handoff |
| Noise floor spiked > 3 dB above 24h avg | Local EMI / Radar Interference |
| RF flat | Downstream POP / Carrier Congestion |

When orbital data has been collected, `insights` also prints a spatial obstruction map — az/el grid cells ranked by average packet loss — making it possible to identify physical obstructions by direction.

`pp-starlink predict-window` steps through future satellite passes and flags windows where a satellite will cross a historically lossy az/el zone.

---

## Hardware requirements

| Component | Notes |
|---|---|
| Raspberry Pi 5 | Host; runs the Docker stack |
| External USB 3.0 drive | Database lives at `/mnt/usb3/starlink_data` |
| Starlink dish | Accessible at `192.168.100.1:9200` (unauthenticated gRPC) |
| RTL-SDR V3 or V4 | Any RTL2832U-based dongle works |
| Ku-band LNB | Universal LNB with 9.75 GHz local oscillator; aimed at the Starlink satellite arc |

---

## Prerequisites

```bash
# On the Pi host
apt install grpcurl traceroute protobuf-compiler

# Go toolchain (for local builds)
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# USB drive mount
mkdir -p /mnt/usb3/starlink_data
# add to /etc/fstab or mount manually before starting the stack
```

---

## Verify the dish gRPC API

Field numbers in `proto/device.proto` were sourced from community reverse-engineering and may shift with firmware updates. Confirm before first build:

```bash
make verify-dish
```

Expected output includes `uptime_s`, `fraction_obstructed`, `thermal_shutdown`, etc. If `DishGetStatus` comes back empty, the `oneof` field numbers in the proto have drifted — compare against the live descriptor output and update `proto/device.proto` accordingly.

---

## Build

```bash
# Generate Go stubs from proto + compile binary
make build

# Output: bin/pp-starlink
```

---

## Run with Docker Compose

```bash
docker compose up --build -d
```

Both services start automatically. The Go engine runs `init` (schema creation) then `daemon` on container start.

To tail logs:

```bash
docker compose logs -f pp-starlink-engine
docker compose logs -f rf-listener
```

---

## CLI reference

### `pp-starlink init`

Creates the database schema and applies SQLite tuning pragmas. Safe to re-run.

```bash
pp-starlink init
```

### `pp-starlink status [--json]`

Fetches live dish status (without running the daemon). Useful for quick health checks and debugging proto drift.

```bash
pp-starlink status
pp-starlink status --json
```

### `pp-starlink set-location --lat <float> --lon <float>`

Saves observer coordinates to the database. Required before orbital features (satellite az/el, `predict-window`) are active.

```bash
pp-starlink set-location --lat 47.6062 --lon -122.3321
# Location set: lat=47.6062 lon=-122.3321
```

Add `--compact` for Markdown-list output (agent-friendly).

### `pp-starlink daemon`

Starts the collection loop. Samples every 15 seconds:
- gRPC dish status (uptime, obstruction fraction, alert bitmask)
- ICMP ping to `192.168.100.1`, auto-detected POP IP, and `1.1.1.1`
- Highest-elevation Starlink satellite az/el (if location is configured)

POP IP is detected once at startup via `traceroute` hop 2. If detection fails, POP pings are skipped and a warning is logged.

On first run with a location set, the daemon downloads the current Starlink TLE set from CelesTrak and caches it in `/tmp/starlink_current.tle` (tmpfs — no disk wear). The cache is refreshed in memory once every 24 hours.

```bash
pp-starlink daemon
```

### `pp-starlink insights [--compact]`

Queries the last 30 days for drop events (`public_packet_loss > 5%`), cross-references RF telemetry within a 1-second window, and prints a classified cause per event. When orbital data exists, appends a spatial obstruction map.

```bash
# Human-readable
pp-starlink insights

# Agent-compact: one Markdown list item per event, no ANSI
pp-starlink insights --compact
```

**Compact output example:**
```
- 2026-06-21T03:14:22Z loss=12% [RF] Transient Physical Blockage / Canopy Handoff Failure
- 2026-06-21T03:29:07Z loss=8%  [RF] Local Terrestrial EMI / Radar Interference
- 2026-06-21T04:01:55Z loss=19% [!] Downstream Network Pop / Carrier Congestion

## Obstruction map (az/el buckets with packet loss)
| Az (°) | El (°) | Avg loss | Incidents |
|--------|--------|----------|-----------|
|     40 |     30 |    18.2% |        14 |
|     50 |     25 |    11.7% |         9 |
```

### `pp-starlink predict-window --duration <minutes>`

Loads the historical bad az/el zones from the database and steps through the next N minutes of satellite passes at 1-minute resolution. Flags any window where the highest satellite crosses a historically lossy zone (±5° az, ±2.5° el tolerance).

Requires `set-location` to have been run first.

```bash
pp-starlink predict-window --duration 60

## Predicted drop risk windows (next 60 min)
| Start    | End      | Satellite            | Az (°) | El (°) |
|----------|----------|----------------------|--------|--------|
| 14:22:00 | 14:27:00 | STARLINK-3421        |   42.0 |   28.5 |
```

---

## Database schema

**`network_telemetry`**

| Column | Type | Description |
|---|---|---|
| `timestamp` | INTEGER | Unix epoch |
| `uptime_s` | INTEGER | Dish uptime in seconds (reboot detection) |
| `obstruction_fraction` | REAL | 0.0–1.0 physical obstruction from dish API |
| `alert_flags` | INTEGER | Bitmask: bit 0=ThermalShutdown, 1=ThermalThrottle, 2=SlowEthernet |
| `local_jitter` | REAL | Gateway ping mdev (ms) |
| `pop_jitter` | REAL | POP ping mdev (ms) |
| `public_packet_loss` | REAL | `1.1.1.1` packet loss ratio (0.0–1.0) |
| `lower_signal_than_predicted` | INTEGER | 1 if dish alert fired |
| `is_snr_above_noise_floor` | INTEGER | 0 if dish SNR alert fired |
| `target_satellite_id` | TEXT | TLE name of the highest-elevation satellite (NULL if no location set) |
| `calculated_azimuth` | REAL | Satellite azimuth in degrees 0–360 (NULL if no location set) |
| `calculated_elevation` | REAL | Satellite elevation in degrees 0–90 (NULL if no location set) |

**`rf_telemetry`**

| Column | Type | Description |
|---|---|---|
| `timestamp` | INTEGER | Unix epoch |
| `beacon_snr_db` | REAL | Peak − noise floor in the sweep window (dB) |
| `noise_floor_db` | REAL | 10th-percentile bin power across the sweep (dBm) |

**`app_config`**

| Column | Type | Description |
|---|---|---|
| `key` | TEXT | Configuration key (e.g. `latitude`, `longitude`) |
| `value` | TEXT | Configuration value |

Rows older than 30 days are pruned on every write cycle. `PRAGMA incremental_vacuum(50)` reclaims pages to reduce storage wear.

---

## SQLite tuning (wear mitigation)

Applied automatically on every database open:

```sql
PRAGMA journal_mode = WAL;      -- writers don't block readers
PRAGMA temp_store = MEMORY;     -- temp tables stay in RAM
PRAGMA synchronous = OFF;       -- defers fsync to OS; safe on power-stable Pi
PRAGMA cache_size = -4000;      -- 4 MB page cache in RAM
PRAGMA auto_vacuum = INCREMENTAL;
```

`synchronous = OFF` trades crash-safety for drastically fewer write amplification events on the USB flash. Acceptable here because the data is diagnostic telemetry, not financial records.

---

## RF calibration

The default center frequency (`1575 MHz`) targets a 9.75 GHz LO downconverting an ~11.325 GHz Starlink pilot. Your LNB and satellite geometry will differ.

Tune via environment variables in `docker-compose.yml`:

```yaml
RF_CENTER_HZ=1575000000    # center of rtl_power sweep
RF_BW_HZ=4000000           # sweep bandwidth (±2 MHz either side)
RTL_GAIN=40                # RTL-SDR gain in dB; 0 = auto
SAMPLE_INTERVAL_S=10       # seconds between sweeps
```

To find your pilot tone manually:

```bash
# Sweep 950–2150 MHz (full Ku IF range) and look for the peak
rtl_power -f 950000000:2150000000:1000000 -g 40 -e 10s -
```

Plot the CSV output and identify the sharpest peak — that's your pilot. Set `RF_CENTER_HZ` to that frequency.

Diagnosis thresholds are relative to the 24-hour rolling average, so they self-calibrate after the first day of data. Override if your environment is unusual:

```yaml
STARLINK_SNR_DELTA=3.0    # dB drop below 24h avg → physical blockage
STARLINK_NOISE_DELTA=3.0  # dB spike above 24h avg → EMI
```

---

## Project structure

```
pp-starlink/
├── cmd/pp-starlink/main.go      # CLI: init / daemon / insights / set-location / predict-window
├── internal/
│   ├── db/db.go                 # SQLite open, schema, write, prune, config, spatial buckets
│   ├── orbit/orbit.go           # TLE fetch/cache, Keplerian propagator, az/el, risk windows
│   ├── ping/ping.go             # exec ping, parse mdev + loss
│   └── starlink/client.go       # gRPC dial + GetStatus
├── proto/device.proto           # Minimal Starlink dish proto
├── rf_listener.py               # RTL-SDR via rtl_power → SQLite
├── Dockerfile.go                # Multi-stage Go build
├── Dockerfile.python            # python:3.12-slim + rtl-sdr
├── docker-compose.yml           # Orchestration + USB passthrough
└── Makefile                     # proto / build / verify-dish
```

### Orbital propagation notes

`internal/orbit/orbit.go` implements a Keplerian (two-body) propagator — no J2 zonal harmonic or atmospheric drag corrections. Error is approximately 1° per orbit (~90 minutes), which is within the 10°-az / 5°-el bucket granularity used for obstruction mapping. If sub-degree accuracy is required, replace with an SGP4 library.

TLE data is sourced from CelesTrak's public GP API. The downloaded file is cached to `/tmp/starlink_current.tle` (tmpfs — survives daemon restarts, cleared on reboot) and refreshed in memory once every 24 hours. No persistent disk writes.
