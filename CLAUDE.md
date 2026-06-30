# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Fast command map

One-command update + start prep (pull + bootstrap + init + location setup):

```bash
make sync-start
```

At the location prompt, paste one line in Google Maps format:

```text
32.853359101557814, -97.25425341002087
```

Non-interactive location setup:

```bash
make sync-start LAT=47.6062 LON=-122.3321
```

One-command bootstrap (venv + deps):

```bash
make bootstrap
```

Attempt binary build during bootstrap:

```bash
make bootstrap BOOTSTRAP_BUILD=1
```

Include web build in bootstrap when needed:

```bash
make bootstrap BOOTSTRAP_BUILD=1 BOOTSTRAP_WEB=1
```

One-command cleanup:

```bash
make cleanup
```

Build and generate proto/web assets:

```bash
make build
```

Generate proto stubs only:

```bash
make proto
```

Run production-like stack:

```bash
docker compose up --build -d
```

Tail service logs:

```bash
docker compose logs -f pp-starlink-engine
docker compose logs -f rf-listener
```

Run Athena checks:

```bash
venv/bin/athena test tests/athena
```

Update Athena baselines after intentional changes:

```bash
venv/bin/athena test -b tests/athena
```

Run e2e smoke test (requires live dish):

```bash
./bin/e2e
```

## Start sequence after bootstrap

This is the full local start sequence after cloning.

Shortcut:

```bash
make sync-start
```

1. Setup environment:

```bash
make bootstrap
```

2. Build binaries:

```bash
make bootstrap BOOTSTRAP_BUILD=1
```

3. Initialize DB schema:

```bash
./bin/pp-starlink init
```

4. Verify live dish connectivity:

```bash
./bin/pp-starlink status --json
```

5. Set observer location (required for orbital features):

```bash
./bin/pp-starlink set-location --lat 47.6062 --lon -122.3321
```

6. Start collection loop:

```bash
./bin/pp-starlink daemon
```

7. In a second terminal, run Go analysis commands:

```bash
./bin/pp-starlink insights --compact
./bin/pp-starlink predict-window --duration 60
# Dev/demo without historical zones:
./bin/pp-starlink predict-window --duration 60 --synthetic
```

8. Run the Python RCA pipeline (after collecting telemetry):

```bash
# Collect signals from the telemetry DB
venv/bin/pp-starlink collect

# Detect incidents and classify root causes
venv/bin/pp-starlink analyze

# Human-readable report
venv/bin/pp-starlink report

# AI-first bundle (single JSON object with everything for RCA)
venv/bin/pp-starlink report --ai-bundle
```

8. If you prefer full-stack runtime instead of direct CLI:

```bash
docker compose up --build -d
docker compose logs -f pp-starlink-engine
docker compose logs -f rf-listener
```

9. Validate health before finishing work:

```bash
venv/bin/athena test tests/athena
```

10. If local artifacts are messy or stale:

```bash
make cleanup
```

There are no unit tests beyond the e2e binary. go test ./... may find little or no tests.

Required system packages for the daemon: iputils-ping, traceroute. Required for RF listener: rtl-sdr (provides rtl_power). Required for proto generation: protobuf-compiler, grpcurl.

## Architecture

Two separate data collectors write to a shared SQLite database (`/data/starlink_telemetry.db`), and a CLI command layer reads from it.

**Go daemon (`cmd/pp-starlink`)** — polls the Starlink dish every 15 seconds via gRPC, runs concurrent ICMP pings to three targets (gateway, POP, public DNS), optionally computes the highest-elevation Starlink satellite from live TLEs, and writes all results into `network_telemetry`.

**Python RF listener (`rf_listener.py`)** — shells out to `rtl_power` (the C binary from `librtlsdr`) every 10 seconds to sample Ku-band pilot tones via an RTL-SDR dongle and writes beacon SNR + noise floor into `rf_telemetry`.

**Analysis (`insights` command)** — joins `network_telemetry` with `rf_telemetry` on timestamp proximity (±1 s), compares current SNR/noise against a rolling 24-hour baseline, and classifies each packet-loss event into one of four causes: physical blockage/handoff, EMI/radar interference, dish signal alert, or downstream congestion.

### Internal packages

| Package | Role |
|---|---|
| `internal/db` | Schema init, WAL-mode SQLite writes, `QueryInsights`, `SpatialBuckets`, pruning (30-day rolling window) |
| `internal/starlink` | gRPC client wrapping `proto/device` — `Dial` + `GetStatus` |
| `internal/ping` | Shells out to system `ping -c 10 -i 0.2`; parses jitter (mdev) and loss from summary line |
| `internal/orbit` | Fetches/caches Starlink TLEs from CelesTrak, Keplerian propagation (no SGP4), ECI→ECEF→ENU→az/el, risk-window prediction |

### Proto

`proto/device.proto` is a minimal hand-written reverse-engineered subset of SpaceX's private API. Field numbers are community-sourced and may drift with dish firmware. `make verify-dish` uses `grpcurl` against the live dish to confirm field numbers still match. Generated stubs live in `proto/device/` and are committed — only regenerate via `make proto` when `device.proto` changes.

### Database schema

`network_telemetry` uses a bitmask (`alert_flags`) for the three boolean alerts (ThermalShutdown=1, ThermalThrottle=2, SlowEthernet=4). Newer columns (`lower_signal_than_predicted`, `is_snr_above_noise_floor`, satellite az/el, POP latency/drop) were added via `ALTER TABLE` with `isDupColumn` guard — this is intentional for backwards compatibility with existing databases.

### Orbital mechanics

`internal/orbit` implements Keplerian two-body propagation (not SGP4). Error is ~1° per orbit, which is acceptable for 10°-bucket spatial analysis. TLEs are cached at `/tmp/starlink_current.tle` and refreshed every 24 hours in the running daemon.

### Key env vars

| Var | Default | Effect |
|---|---|---|
| `STARLINK_LOSS_THRESHOLD` | `0.05` | Minimum packet loss fraction to flag as a drop event |
| `STARLINK_SNR_DELTA` | `3.0` dB | Drop below 24h SNR baseline → physical blockage |
| `STARLINK_NOISE_DELTA` | `3.0` dB | Spike above 24h noise baseline → EMI/radar |
| `DB_PATH` | `/data/starlink_telemetry.db` | Shared database path (Python + Go must agree) |
| `RF_CENTER_HZ` / `RF_BW_HZ` | `1575 MHz / 4 MHz` | RTL-SDR tune frequency; adjust for your LNB IF |

## Hardware

Runs on a **Raspberry Pi 5 with SD card**. Write frequency is a primary design constraint — SD cards have limited write endurance.

- `network_telemetry`: one row per 15-second daemon tick is the hard ceiling. Never propose per-second writes to this table.
- `outage_telemetry` / `event_log`: use `INSERT OR IGNORE` dedup on `start_timestamp_ns` PRIMARY KEY — write rate is bounded by distinct events, not wall-clock time. These are safe because they don't write on every tick.
- `maybeVacuum` / `maybePrune` / `maybeRefreshSchema` patterns exist to amortize maintenance I/O. Follow this pattern for any new periodic operations.

## Live dish schema

`schema/dish_schema.txt` is a committed snapshot of the dish's live gRPC reflection output. Regenerate with `make refresh-schema`. The daemon checks for schema drift weekly and logs a warning if the live schema hash differs from the committed snapshot.

## Agent skills

### Issue tracker

Issues and PRDs live as markdown files under `.scratch/<feature>/`. See `docs/agents/issue-tracker.md`.

### Triage labels

Five canonical triage roles, default label strings. See `docs/agents/triage-labels.md`.

### Domain docs

Multi-context: `CONTEXT-MAP.md` at the root points to per-context `CONTEXT.md` files. See `docs/agents/domain.md`.
