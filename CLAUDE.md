# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build the Go binary (also regenerates proto stubs and downloads deps)
make build

# Generate Go stubs from proto/device.proto only
make proto

# Run the e2e smoke test (requires live dish at 192.168.100.1:9200)
./bin/e2e

# Inspect live dish proto fields (useful when dish firmware updates field numbers)
make verify-dish

# Run in Docker (production mode: init + daemon + rf-listener)
docker compose up --build

# One-off CLI commands against a running database
./bin/pp-starlink init
./bin/pp-starlink set-location --lat 47.6062 --lon -122.3321
./bin/pp-starlink daemon
./bin/pp-starlink insights [--compact]
./bin/pp-starlink predict-window --duration 60
```bash
# Refresh live dish gRPC schema snapshot (requires live dish + grpcurl)
make refresh-schema

# Show current dish state (live, no daemon required)
./bin/pp-starlink status [--json]
```

There are no unit tests beyond the e2e binary. `go test ./...` will find nothing.

Required system packages for the daemon: `iputils-ping`, `traceroute`. Required for RF listener: `rtl-sdr` (provides `rtl_power`). Required for proto generation: `protobuf-compiler`, `grpcurl`.

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
