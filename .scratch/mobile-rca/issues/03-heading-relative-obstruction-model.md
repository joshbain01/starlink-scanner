# 03 — Heading-relative obstruction model

Status: needs-triage (blocked on 01; design decisions below need a human
call before implementation, not just an agent's best guess)

## Why

`SpatialBuckets` (`internal/db/db.go:339`) and the obstruction map printed
by `cmdInsights` (`cmd/pp-starlink/main.go:780-794`) bucket packet loss by
*compass-absolute* azimuth/elevation, 10° buckets (per CLAUDE.md → Orbital
mechanics). That's a sound model for a fixed dish: "az 110°/el 80°" really
does mean the same tree or roofline every time. It's meaningless for a
moving vehicle or aircraft — the compass bearing to whatever's actually
blocking the dish (a roof rack, a wing root, a tail fin) changes every time
the vehicle turns, while the *vehicle-relative* bearing to that same
physical obstruction stays constant.

Run unmodified against mobile data, `SpatialBuckets` will silently produce
buckets that mix together unrelated real-world directions, diluting any
real recurring obstruction into noise — the exact same class of problem
already documented in the live RCA session that motivated this PRD (single-
incident buckets being mistaken for real obstructions, see prior session's
finding: only the az110/el80 bucket with n=6 incidents was statistically
trustworthy out of ~140 buckets).

## Depends on

Issue 01 for position/heading data. Conceptually downstream of issue 01,
parallel-or-before issue 02 (see PRD phase ordering note).

## Design questions that need a human decision before implementation

This issue is `needs-triage`, not `ready-for-agent`, because of these. An
implementing agent should stop and ask rather than guess:

1. **Keep both frames, or switch entirely when mobile?** Two options:
   - (a) Add a second bucket dimension (heading-relative az/el) computed
     only for samples where `mobility_class` indicates motion, and report
     both in `insights` output — absolute buckets stay meaningful for any
     stationary dwell time (e.g. car parked, plane on the ground), relative
     buckets cover motion.
   - (b) Switch bucket frame entirely based on `mobility_class` per sample,
     storing in one column whose meaning depends on a sibling
     `mobility_class` value.
   (a) is more honest about what the data means but doubles the bucket
   table size and the `insights` output. (b) is simpler but conflates two
   different things in one column, which is exactly the kind of ambiguity
   that caused the original `signal_coverage` bug fixed earlier in this
   project (a field whose meaning didn't match what its consumers assumed).
   Recommend (a) for that reason, but this is the user's call.

2. **Vehicle vs. aircraft heading source.** A car's heading is well
   approximated by GPS-bearing-between-fixes (issue 02's dead-reckoning
   heading derivation can be reused directly). An aircraft's *heading*
   (nose direction) and its *track* (GPS bearing of actual movement) can
   differ significantly in a crosswind — and more importantly, what matters
   for "is the wing blocking the dish" is airframe attitude, not track.
   This is where `ned2dish_quaternion` (issue 01) matters most: if the dish
   is rigidly mounted, it gives airframe-relative orientation directly,
   independent of wind drift. Confirm with the user whether the Mini's
   mount on their aircraft is rigid enough for this assumption to hold
   before relying on it as the primary heading source for flight use.

3. **Bucket granularity for the relative frame.** The existing 10° buckets
   were sized for a fixed mount's typical obstruction width (a tree,
   roofline). A vehicle's self-obstruction (roof rack, antenna mast, tail)
   may be narrower or wider — this needs real driving/flying data to tune,
   not a guess upfront. Recommend shipping with the same 10° default and
   revisiting once real heading-relative data exists, rather than blocking
   on getting it perfectly right first.

## Tasks (once design questions above are resolved)

- Extend `db.SpatialBucket` (`internal/db/db.go:73-77`) with a
  `RelativeAzBucket, RelativeElBucket *float64` pair (nil when the sample
  was stationary or had no heading data) if option (a) above is chosen.
- Extend the `spatialSQL` query (referenced at `internal/db/db.go:340`,
  find its definition) to compute relative az/el as
  `(absolute_az - heading) mod 360` and `absolute_el` (elevation is
  unaffected by yaw — note this explicitly, only azimuth needs the heading
  correction) when heading data exists.
- Update `cmdInsights`'s obstruction map output
  (`cmd/pp-starlink/main.go:788-794`) to print both tables when relative
  data exists, clearly labeled, not silently merged into one.
- Update `pp_starlink/rca/rules/obstruction.py` to be aware of which frame
  it's looking at — it currently has no concept of mobility at all (per the
  module's own docstring guardrail about requiring az/el pointing data).

## Acceptance criteria

- Stationary deployments (no mobility data, or `mobility_class` always
  "stationary") produce byte-identical `insights` obstruction-map output to
  today — this issue must not change behavior for the existing primary use
  case.
- A synthetic or recorded mobile dataset (driving in a loop, ideally)
  produces a heading-relative bucket table where a real fixed self-
  obstruction (if one exists on the test vehicle) concentrates into a
  narrow heading-relative range, rather than being smeared across all
  compass directions as it would be in the absolute-frame table.
- `venv/bin/athena test tests/athena` passes.
