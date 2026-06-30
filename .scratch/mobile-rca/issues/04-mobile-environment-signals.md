# 04 — Mobile environmental signals (weather, RF)

Status: needs-triage (lowest priority of the four; independent of 02/03,
only depends on 01)

## Why

The user asked specifically about predictive MOS incorporating "known RF
conditions" and "known environmental conditions" alongside position/speed/
heading. This issue covers what's realistically buildable from data this
repo already has hooks for; it does not invent a new external RF-congestion
database (out of scope per the PRD).

## Current state (verified this session, stationary deployment)

`pp_starlink/signals/weather_hourly.py` and `weather_daily.py` are
registered signal modules but returned **0 records** even in a 9-day
stationary baseline (confirmed via `collect` output and the AI bundle's
`signal_coverage` field, both populated/fixed earlier in this project).
`rf.environment` (the RTL-SDR-backed signal) also returned 0 records in
the same baseline — meaning the RF half of this issue may be moot
regardless of mobility if the dongle isn't producing data at all today.
Resolve that independently of mobility before investing here — see PRD
open question #2.

## Scope

### Weather

If `weather_hourly`/`weather_daily` get fixed for the stationary case
first (separate, smaller fix — not blocked on mobility), the mobile
extension is: key the weather lookup on `(lat, lon, time)` per sample
instead of one static location. Check whatever weather API
`pp_starlink/signals/weather_hourly.py` currently calls (or is stubbed to
call) for rate limits before doing this per-sample — at a 15s tick rate
that's potentially 240 calls/hour, likely way over any free-tier limit.
Realistic approach: cache by rounded `(lat, lon)` to a coarse grid (e.g.
0.1°) and time bucket (e.g. 15 min), not a literal call per telemetry row.

### RF

Contingent on resolving whether the RTL-SDR dongle produces any usable
signal while mobile at all (PRD open question #2 — antenna orientation,
vibration, and calibration assumptions baked into a stationary setup may
not hold in a car or aircraft). This needs a real test with the actual
hardware before any code is written here; there's nothing in the existing
`rf_listener.py` to extend until that's confirmed viable.

"Known RF conditions" in the broader sense the user likely means (known
interference corridors, spot-beam congestion patterns) has no data source
in this repo at all — explicitly out of scope per the PRD, flagged there as
a separate initiative if the user wants to pursue it.

## Acceptance criteria

Not defined yet — this issue needs the weather-API and RTL-SDR-in-motion
questions answered first. Revisit scope once those are resolved rather than
estimating acceptance criteria against unknowns.
