# 01 — Collect mobility telemetry from the dish

Status: ready-for-agent

## Why

The dish already reports mobility-relevant fields over gRPC
(`mobility_class`, `is_moving_fast_persisted`, `gps_stats`,
`ned2dish_quaternion`, and a separate `GetLocation` RPC), confirmed present
in `schema/dish_schema.txt` but absent from the committed
`proto/device.proto` and from `internal/starlink.Status`. Every other phase
in this feature (`.scratch/mobile-rca/PRD.md`) depends on this data
existing in `network_telemetry`. This issue is just the wiring — no new
RCA logic, no schema redesign, just stop discarding data we already have
access to.

## Before writing code: verify against the live dish

`proto/device.proto`'s header comment says field numbers are
community-sourced from live reflection and may drift with firmware
(CLAUDE.md → Proto). Run, against an actual dish:

```bash
grpcurl -plaintext 192.168.100.1:9200 describe SpaceX.API.Device.DishGetStatusResponse
grpcurl -plaintext 192.168.100.1:9200 describe SpaceX.API.Device.DishGpsStats
grpcurl -plaintext 192.168.100.1:9200 describe SpaceX.API.Device.UserMobilityClass
grpcurl -plaintext 192.168.100.1:9200 describe SpaceX.API.Device.Quaternion
grpcurl -plaintext 192.168.100.1:9200 describe SpaceX.API.Device.GetLocationResponse
grpcurl -plaintext 192.168.100.1:9200 describe SpaceX.API.Device.DishInhibitGpsRequest
```

Confirm field numbers/types match what's below before committing to them —
`schema/dish_schema.txt` only expanded `DishGetStatusResponse` itself;
`DishGpsStats`, `UserMobilityClass`, `Quaternion`, and `GetLocationResponse`
are referenced by name in field lists but their own bodies were never
dumped. Update `schema/dish_schema.txt` via `make refresh-schema` once
confirmed, so it stays the source of truth.

Also confirm whether `GetLocationResponse` actually returns data without
first calling something related to `DishInhibitGpsRequest`
(`schema/dish_schema.txt:58`) — the name suggests GPS can be inhibited, the
inverse (does it need explicit enabling) needs a live check.

## Tasks

### `proto/device.proto`

Add to `DishGetStatusResponse` (currently ends at line 68):
```protobuf
DishGpsStats            gps_stats                      = 1015;
UserMobilityClass       mobility_class                 = 1017;
bool                    is_moving_fast_persisted        = 1042;
Quaternion               ned2dish_quaternion            = 1049;
```
(Field numbers per `schema/dish_schema.txt:221-249` — reverify per the
section above before relying on them.)

New messages (exact field layout TBD from live `describe` output):
```protobuf
message DishGpsStats {
  bool   gps_valid = 1; // verify
  uint32 gps_sats  = 2; // verify
}

enum UserMobilityClass {
  MOBILITY_UNKNOWN    = 0; // verify exact enum values live
  // ... STATIONARY / MOBILE / IN_MOTION or similar
}

message Quaternion {
  float w = 1; // verify
  float x = 2;
  float y = 3;
  float z = 4;
}
```

New RPC for position (add to the `Request`/`Response` oneofs alongside the
existing `get_status`/`get_history`/`get_device_info` entries, using field
number 1017 in the request/response oneof per
`schema/dish_schema.txt:28,129` — note this is a different numbering
namespace than the `mobility_class` field above, they don't collide):
```protobuf
message GetLocationRequest {}
message GetLocationResponse {
  // TBD — verify via grpcurl describe. Expect something like:
  // LLA lla = N;
  // LocationSource source = N;
  // bool gps_time_valid = N;
}
```

Regenerate stubs with `make proto` after editing.

### `internal/starlink/client.go`

Add to `Status` struct (`internal/starlink/client.go:51`):
```go
MobilityClass         string  // enum stringified, like OutageCause already does
IsMovingFastPersisted bool
GpsValid              bool
GpsSats               int32
Quaternion            *Quaternion // nil if dish hasn't reported one yet (e.g. not locked)
```
where `Quaternion` is a small local struct `{W, X, Y, Z float32}` — keep it
in this package, don't leak the proto type into callers (matches the
existing pattern of `Status`/`Alerts` being hand-rolled, not raw proto
structs).

Add a `GetLocation(ctx context.Context) (Location, error)` method
alongside the existing `GetStatus`/`GetHistory` (`internal/starlink/client.go:105`),
following the same dial/timeout/error-handling shape. `Location` struct:
`{Lat, Lon, AltitudeM float64; Valid bool; Timestamp time.Time}` — exact
fields depend on the live `describe` output.

### `internal/db/db.go`

Migration version is currently `9` (last entries at
`internal/db/db.go:202-203`). Add version `10`:
```go
{10, `ALTER TABLE network_telemetry ADD COLUMN mobility_class TEXT`},
{10, `ALTER TABLE network_telemetry ADD COLUMN is_moving_fast INTEGER`},
{10, `ALTER TABLE network_telemetry ADD COLUMN gps_valid INTEGER`},
{10, `ALTER TABLE network_telemetry ADD COLUMN gps_sats INTEGER`},
{10, `ALTER TABLE network_telemetry ADD COLUMN gps_lat REAL`},
{10, `ALTER TABLE network_telemetry ADD COLUMN gps_lon REAL`},
{10, `ALTER TABLE network_telemetry ADD COLUMN gps_altitude_m REAL`},
{10, `ALTER TABLE network_telemetry ADD COLUMN quat_w REAL`},
{10, `ALTER TABLE network_telemetry ADD COLUMN quat_x REAL`},
{10, `ALTER TABLE network_telemetry ADD COLUMN quat_y REAL`},
{10, `ALTER TABLE network_telemetry ADD COLUMN quat_z REAL`},
```
Follow the existing nullable-pointer pattern already used for
`SatelliteID`/`Azimuth`/`Elevation` in `NetworkSample`
(`internal/db/db.go:19-64`) for all of the above — GPS/quaternion data
won't be present on every tick (no fix yet, dish not locked, etc), same as
orbital look-angle today.

This does **not** change write frequency. One row per existing 15s tick,
same as today — see the PRD's "Hard constraints" section. Do not add a
separate higher-frequency table in this issue; if dead-reckoning in Phase 3
needs tighter sampling, that's scoped there, not here.

### `cmd/pp-starlink/collector.go`

In `Collector.sample` (`cmd/pp-starlink/collector.go:246`), call
`c.sc.GetLocation(ctx)` in the existing `sync.WaitGroup` alongside the
parallel `GetHistory` call (`collector.go:280-292`) — don't add it
serially after `GetStatus`, keep the existing parallel-fetch shape. Add the
new `Status` fields and the location result into the `db.NetworkSample`
literal (`collector.go:317-354`), following the existing nil-pointer-when-
absent pattern used for boresight (`collector.go:308-315`).

A failed `GetLocation` call (e.g. GPS not locked, or inhibited) must not
fail the whole tick — log and leave the GPS fields nil, same as how a
failed `GetHistory` already degrades gracefully (`collector.go:284-288`).

## Acceptance criteria

- `go build ./...` succeeds.
- `./bin/pp-starlink status --json` includes the new fields when run
  against a live dish that has a GPS fix.
- A fresh `./bin/pp-starlink init` followed by one daemon tick produces a
  `network_telemetry` row with the new columns populated (or explicitly
  NULL when no fix), without any change in row-count-per-tick.
- `venv/bin/athena test tests/athena` passes (CLAUDE.md commit gate).
- `schema/dish_schema.txt` regenerated via `make refresh-schema` and
  committed alongside the proto changes, so the two stay in sync per the
  existing convention.

## Out of scope here

Using any of this data for RCA, prediction, or obstruction bucketing — that's
Phases 2 and 3. This issue only collects and persists it.
