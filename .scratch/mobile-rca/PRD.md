# PRD: Mobile RCA & Predictive MOS for Starlink Mini

Status: needs-triage

## Problem

This codebase assumes a stationary dish. `set-location` writes a single
lat/lon to `app_config` once; `internal/orbit.HighestSatellite` and
`predict-window` use that same fixed point for the life of the deployment;
`SpatialBuckets`/the obstruction map in `insights` key purely on
earth-frame (compass) azimuth/elevation, on the theory that "az 110°/el 80°"
reliably means the same tree or roofline every time.

The user runs a Starlink Mini that travels — sometimes in a car, sometimes
in a small airplane. Every one of those assumptions is wrong in motion:

- Position changes every tick, so a one-time `set-location` is stale within
  minutes of starting to drive.
- "Az 110°" means a different point in the sky every time the vehicle turns.
  What's actually fixed is the *vehicle-relative* direction — e.g. "the wing
  root blocks the dish whenever it's 40° off the nose" — not the compass
  bearing.
- RCA rules and the obstruction map (`cmd/pp-starlink/main.go:780-794`,
  `internal/db/db.go:339` `SpatialBuckets`) silently produce nonsense
  findings when fed mobile data, because they were never told the dish was
  moving.
- `predict-window` (`cmd/pp-starlink/main.go:826` `cmdPredictWindow`) can
  only forecast risk for the static `set-location` point — useless for "what
  will my MOS look like in 20 minutes if I keep driving this heading at this
  speed."

## Goals

1. Collect the mobility data the dish already exposes but the daemon
   currently discards.
2. Make position a per-sample value, not one-time config, so RCA and
   prediction work correctly while moving.
3. Give predictive MOS a real input: current position + speed + heading,
   not just a fixed point.
4. Make the obstruction/RCA model heading-relative so findings are
   meaningful for a vehicle or airframe, not just a fixed mount.

## Non-goals (this PRD)

- SGP4-grade orbital precision. The existing Keplerian approximation
  (~1°/orbit error, documented in `CLAUDE.md` under Orbital mechanics) is
  judged sufficient for stationary use today; mobile use does not raise the
  bar enough to justify replacing it. Out of scope unless a later phase
  proves otherwise.
- A general-purpose "known RF interference corridor" database. There is no
  existing data source for this in the repo; building one (crowdsourced or
  hand-maintained) is a separate initiative, noted as an open question below
  but not scoped here.
- Avionics/OBD integration for vehicle telemetry (true airspeed, OBD speed,
  etc). The dish's own GPS and `ned2dish_quaternion` cover the same ground
  for the dish's-eye view and are the better source of truth for "what can
  the dish see" — see Phase 1.

## Background: what the dish already exposes

`schema/dish_schema.txt` (the committed live-reflection snapshot, see
CLAUDE.md "Live dish schema") shows `DishGetStatusResponse` already carries
fields the committed `proto/device.proto` doesn't parse:

| Field | Wire # | What it gives us |
|---|---|---|
| `mobility_class` | 1017 | `UserMobilityClass` enum — the dish's own stationary-vs-mobile classification. Free segmentation. |
| `is_moving_fast_persisted` | 1042 | Direct bool, no GPS math needed. |
| `ned2dish_quaternion` | 1049 | Dish orientation relative to true North/level (NED frame). If the dish is rigidly mounted, this *is* vehicle/airframe attitude (pitch/roll/yaw) for free — no avionics feed required. |
| `gps_stats` | 1015 | `DishGpsStats` — validity + satellite count (confidence signal for any GPS-derived data). |

None of `mobility_class`, `is_moving_fast_persisted`, `ned2dish_quaternion`,
or `gps_stats` are in `proto/device.proto` today — they're visible in the
schema dump but never parsed into `internal/starlink.Status`
(`internal/starlink/client.go:51`) or written to `network_telemetry`.

Position itself comes from a separate RPC, `GetLocationRequest` /
`GetLocationResponse` (referenced at `schema/dish_schema.txt:28,129`, field
1017 in the `Request`/`Response` oneofs — distinct from the `mobility_class`
field number above, different namespace). **This RPC's own message body is
not expanded in the committed schema dump** — only top-level messages got
their own `## heading` section when the dump was generated. Its field
layout (lat/lon/altitude precision, source enum, accuracy, timestamp) must
be confirmed against the live dish before implementing, the same way
`make verify-dish` already does for other messages (see CLAUDE.md → Proto).
It's gated behind `dish_inhibit_gps` (`schema/dish_schema.txt:58`), which
implies it may require an explicit opt-in toggle — confirm this live too.

## Proposed phases

Phase 1 is a hard prerequisite for 2 and 3. Phase 2 and 3 can be sequenced
either order; Phase 3 (predictive MOS) can ship as a V1 using the existing
absolute-frame buckets and get more accurate once Phase 2 (heading-relative
buckets) lands, rather than blocking on it. Phase 4 is independent.

### Phase 1 — Collect mobility telemetry
See `issues/01-collect-mobility-telemetry.md`.

Add `mobility_class`, `is_moving_fast_persisted`, `gps_stats`,
`ned2dish_quaternion` to `proto/device.proto` and the parsing path in
`internal/starlink/client.go`. Add the `GetLocation` RPC (after live
verification of its field layout). Add corresponding `network_telemetry`
columns via the existing `ALTER TABLE` migration pattern
(`internal/db/db.go`, next version is `10`). Wire into
`Collector.sample` (`cmd/pp-starlink/collector.go:246`).

### Phase 2 — Heading-relative obstruction model
See `issues/03-heading-relative-obstruction-model.md`.

`SpatialBuckets` (`internal/db/db.go:339`) and the obstruction map in
`cmdInsights` (`cmd/pp-starlink/main.go:780`) need a second az/el axis
computed relative to vehicle heading (derived from consecutive GPS fixes
or directly from `ned2dish_quaternion`), not just compass-absolute. Gate
on `mobility_class` from Phase 1: stationary samples keep using the
absolute-frame bucket logic unchanged; mobile samples get bucketed
heading-relative instead (or in addition — open question, see issue).

### Phase 3 — Predictive MOS from dead-reckoned position
See `issues/02-position-aware-predictive-mos.md`.

Extend `predict-window` to optionally project a future position from a
current GPS fix + speed + heading (simple linear dead-reckoning — no Kalman
filter, consistent with the existing Keplerian-approximation philosophy
documented in CLAUDE.md), then feed that projected point through the
*existing* `orbit.PassesInWindow` / `predictWindowMOS`
(`cmd/pp-starlink/main.go:826,900`) pipeline instead of the static
`set-location` point. The orbital math doesn't change — only the observer
point fed into it does.

### Phase 4 — Mobile environmental signals
See `issues/04-mobile-environment-signals.md`.

`pp_starlink/signals/weather_hourly.py` and `weather_daily.py` are stub
modules that returned 0 records even for the stationary deployment tested
in this session (confirmed via `collect` output and the AI bundle's
`signal_coverage`). For mobile use they need to key on (lat, lon, time) per
sample instead of one static call. Lower priority — flagged here for
completeness since the user explicitly asked about "known environmental
conditions," but no phase 1-3 work depends on it.

## Hard constraints carried over from CLAUDE.md — do not violate

- **SD card write ceiling**: "`network_telemetry`: one row per 15-second
  daemon tick is the hard ceiling. Never propose per-second writes to this
  table." Any temptation to poll GPS faster while `mobility_class` indicates
  motion (for tighter dead-reckoning) must NOT turn into additional writes
  to `network_telemetry`. If finer-grained position is ever needed, it
  belongs in a separate low-write-rate mechanism (in-memory only, or an
  `INSERT OR IGNORE`-deduped table following the `outage_telemetry` pattern
  — see CLAUDE.md → Hardware), not a faster tick.
- **Athena gate**: per CLAUDE.md, `venv/bin/athena test tests/athena` must
  pass before any commit/push from these issues.
- **Proto field numbers are community-sourced and may drift with firmware**
  (CLAUDE.md → Proto). Every new field added in Phase 1 must be verified
  live via `grpcurl describe` against an actual dish before being trusted,
  same as the existing fields.

## Open questions / risks (not resolved by this PRD — flag back to the user)

1. **Location data sensitivity.** Storing raw lat/lon of where the user
   drives/flies in a SQLite DB is materially more sensitive than today's
   "one fixed home location in `app_config`." This PRD does not decide
   retention, redaction, or export behavior for that data — surface it to
   the user before Phase 1 writes any position history to disk.
2. **RTL-SDR feasibility in motion.** The RF listener
   (`rf_listener.py`/`rtl_power`) was already found to be producing 0
   records even in the stationary baseline tested this session (see prior
   RCA finding: `rf.environment` had zero coverage across 64 incidents).
   Whether a dongle stays usefully calibrated/oriented in a moving vehicle
   or aircraft at all is a hardware question this PRD can't answer — flag
   before investing in Phase 4's RF half.
3. **`GetLocationResponse`'s exact field layout** — must be confirmed live;
   not guessable from the committed schema snapshot (see Background).
4. **Heading-relative bucket granularity and whether to keep absolute
   buckets too** — left as an open design question in Phase 2's issue file
   rather than decided here, since it affects both schema and the
   `insights` output format.

## Success criteria

- `network_telemetry` carries `mobility_class`, position, and orientation
  data per tick when available, without exceeding the existing one-row-per-
  15s write rate.
- `predict-window` can be pointed at a moving observer (current GPS fix +
  speed + heading) and produce a forecast for a projected future position,
  not just the static `set-location` point.
- The obstruction map distinguishes stationary-compass-relative findings
  from vehicle-heading-relative findings, so RCA output is not silently
  wrong when `mobility_class` says the dish is moving.
