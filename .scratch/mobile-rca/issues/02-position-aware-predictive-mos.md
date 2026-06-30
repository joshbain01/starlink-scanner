# 02 — Position-aware predictive MOS (dead-reckoning)

Status: needs-triage (blocked on 01)

## Why

`predict-window` (`cmd/pp-starlink/main.go:826` `cmdPredictWindow`) and the
MOS heuristic it calls, `predictWindowMOS`
(`cmd/pp-starlink/main.go:900`), already do the hard part — Keplerian
propagation of upcoming Starlink passes and a loss-weighted MOS estimate
per pass. The only thing wrong for mobile use is the observer point: both
currently read `lat`/`lon` once from `app_config`
(`cmd/pp-starlink/main.go:842-854`, via `set-location`) and treat it as
fixed for the whole forecast window.

For a moving vehicle, "predict my MOS for the next 60 minutes" is
meaningless if it assumes you're not moving. This issue makes the observer
point a projection from current motion instead of a static config value.

## Depends on

Issue 01 (`mobility_class`, GPS position via `GetLocation`, speed/heading
derivation all need to exist in `network_telemetry` first).

## Design

### Heading and speed

The dish's `GetLocation` RPC (issue 01) gives position, not necessarily a
velocity vector directly — confirm this when verifying the live schema in
issue 01. If it's position-only:

- Derive heading and speed from the last two `network_telemetry` rows with
  valid GPS fixes (great-circle bearing between two lat/lon points, divided
  by the elapsed time between samples).
- At the existing 15s tick rate this is noisy at low speed (GPS fix jitter
  dominates) but adequate at driving/flying speeds, which is the actual use
  case. Don't over-build a filter for the stationary case — `mobility_class`
  from issue 01 already tells us when to even attempt this.
- If `ned2dish_quaternion` reliably tracks airframe attitude (confirmed
  feasible in issue 01's research), it's a second, lower-noise heading
  source worth comparing against the GPS-bearing derivation — but don't
  block this issue on building that comparison; GPS-bearing dead-reckoning
  alone is a legitimate V1.

### Dead-reckoning

Simple linear projection: given current `(lat, lon)`, `speed_mps`,
`heading_deg`, and a forecast horizon, project forward in straight-line
segments (e.g. one projected point per 60s of the forecast window, reusing
the existing window-bucketing already implicit in `PassesInWindow`'s
duration handling). This is consistent with the project's existing
tolerance for orbital approximation error (CLAUDE.md → Orbital mechanics:
~1°/orbit, "acceptable for 10°-bucket spatial analysis") — a Kalman filter
or road-network-aware routing is explicitly not warranted for the same
reason. A turn invalidates the projection; that's fine, re-run on the next
request.

### Wiring into the existing pipeline

`orbit.PassesInWindow(tles, lat, lon, bad, dur)`
(`internal/orbit/orbit.go:219`) and `predictWindowMOS`
(`cmd/pp-starlink/main.go:900`) already take `lat, lon` as plain floats —
they have no idea whether the value came from static config or a live
projection. The minimal change is in `cmdPredictWindow`
(`cmd/pp-starlink/main.go:826`): when `mobility_class` indicates the dish
is currently mobile (and a recent GPS fix exists), compute a projected
`(lat, lon)` per forecast sub-window instead of reading the single static
value at `main.go:842-854`, and call the existing functions once per
sub-window with the projected point instead of once for the whole window.

Add a `--mobile` flag (or auto-detect via `mobility_class` from the most
recent sample — prefer auto-detect, it's one less thing for the user to
remember, matching the `synthetic` flag's existing opt-in shape only where
genuinely ambiguous). Stationary behavior (today's single static lookup)
must remain the default/fallback when no recent fix or mobility signal is
present — this issue must not regress the existing stationary use case.

### Bucket evidence caveat

`predictWindowMOS` weighs its MOS estimate against `db.SpatialBucket`
historical loss data, which Phase 2 of the PRD (heading-relative
obstruction model, issue 03) hasn't landed yet when this issue starts. V1
of this issue can and should ship using the existing absolute-frame
buckets as-is — same caveat the obstruction map already has today, not a
new one. Note this explicitly in the `predict-window` output's rationale
string (`main.go:968-985` already has a `rationale` field; extend it to
say e.g. "evidence is compass-frame, not heading-relative — see issue 03"
when running in mobile mode) so nobody mistakes a rough estimate for a
precise one.

## Acceptance criteria

- `predict-window` run against a dish with recent GPS fixes and
  `mobility_class` indicating motion produces a forecast based on a
  projected, not static, position.
- `predict-window` run against a stationary deployment (or one with no GPS
  data, e.g. before issue 01 lands on an older DB) behaves exactly as it
  does today — this is strictly additive.
- `--synthetic` mode (`main.go:868-870`) still works unchanged for
  dev/demo use.
- `venv/bin/athena test tests/athena` passes.

## Open question to flag back to the user

How far ahead is a useful forecast for a car vs. a plane? A car at 70mph
covers ~1.7mi in 90s; a plane at 150kt covers ~4.6mi in the same window.
The existing default forecast horizon (`--duration` minutes, user-supplied)
already accommodates this via the existing flag — confirm this is
sufficient rather than adding a separate speed-scaled default, to avoid
inventing a heuristic nobody asked for.
