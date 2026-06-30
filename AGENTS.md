# AGENTS.md

This file is for coding agents working in this repository.

## Goal

Implement safe, reviewable changes quickly, and verify behavior with the repo's real workflows.

## Fast command map

One-command update + start prep (pull + bootstrap + init + location setup):

```bash
make sync-start
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

## Environment setup

Use Python 3.12+ and a local venv:

```bash
python3.13 -m venv venv
venv/bin/pip install -r requirements.lock -r requirements-dev.lock
venv/bin/pip install -e . --no-deps
```

System-level packages commonly required:

- protobuf-compiler
- grpcurl
- traceroute
- iputils-ping
- rtl-sdr (provides rtl_power)

Toolchains required by build:

- Go
- Node.js + npm
- Docker + Docker Compose

## Typical implementation workflow

1. Read README.md and relevant files under docs/agents/.
2. Make a minimal targeted code change.
3. Run the narrowest validation first (command, check, or binary).
4. Run Athena checks before finishing.
5. If expected output changes, update only the required baselines.
6. Keep commits focused and easy to review.

## Validation matrix

For CLI/data-path changes:

```bash
./bin/pp-starlink init
./bin/pp-starlink status --json
./bin/pp-starlink insights --compact
```

For ingestion/loop behavior:

```bash
./bin/pp-starlink daemon
```

For repo health and regressions:

```bash
venv/bin/athena test tests/athena
```

## Important constraints

- Data collection targets Raspberry Pi with storage-write constraints.
- Do not introduce per-second writes into network_telemetry.
- Prefer incremental maintenance patterns (prune/vacuum/refresh) over frequent heavy operations.
- Keep behavior deterministic where checks rely on baseline output.

## Live-hardware assumptions

Some commands require a reachable dish at 192.168.100.1:9200 and/or RTL-SDR hardware.

Examples requiring live environment:

- make verify-dish
- make refresh-schema
- ./bin/e2e
- status/daemon behaviors that contact the dish

If hardware is unavailable, state this clearly and run the checks that are still meaningful.

## Where to look first

- cmd/pp-starlink/: CLI entrypoints and runtime behavior
- internal/db/: schema, writes, query paths
- internal/orbit/: TLE handling and predictions
- internal/starlink/: dish client
- rf_listener.py: RF ingestion path
- tests/athena/: baseline checks

## Documentation pointers

- README.md: operator + developer usage
- docs/agents/issue-tracker.md: local issue tracking conventions
- docs/agents/triage-labels.md: triage vocabulary
- docs/agents/domain.md: domain-doc reading order
