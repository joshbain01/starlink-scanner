"""
pp-starlink CLI — root-cause analysis tool for Starlink MOS drops.

Commands:
  collect       Run all signal modules and write data to storage.
  analyze       Detect incidents and run the RCA engine.
  report        Print or export a formatted RCA report.
  incident      List stored incidents.
  signals list  List registered signals and their status.
    signals-list  Alias for signals list.
"""

from __future__ import annotations

import json
import os
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

import click

from pp_starlink.core.models import ContextSignal
from pp_starlink.incidents.detector import detect_incidents
from pp_starlink.rca.engine import RCAEngine
from pp_starlink.reports.formatter import (
    format_report,
    format_worst_day_summary,
    incidents_to_ai_bundle_json,
    incidents_to_json,
)
from pp_starlink.signals.dish_reboot import DishRebootModule
from pp_starlink.signals.dish_state import DishStateModule
from pp_starlink.signals.load import LoadModule
from pp_starlink.signals.network_local import NetworkLocalModule
from pp_starlink.signals.network_public import NetworkPublicModule
from pp_starlink.signals.network_routing import NetworkRoutingModule
from pp_starlink.signals.obstruction import ObstructionModule
from pp_starlink.signals.registry import SignalRegistry
from pp_starlink.signals.rtc import RTCModule
from pp_starlink.signals.weather_daily import WeatherDailyModule
from pp_starlink.signals.weather_hourly import WeatherHourlyModule
from pp_starlink.storage.writer import StorageWriter
from pp_starlink.telemetry.reader import TelemetryReader

_DB_PATH_DEFAULT = os.getenv("DB_PATH", "/data/starlink_telemetry.db")
_DATA_DIR_DEFAULT = os.getenv("PP_DATA_DIR", "./data")


def _build_registry(db_path: str) -> SignalRegistry:
    registry = SignalRegistry()
    modules = [
        DishStateModule(db_path),
        DishRebootModule(db_path),
        NetworkLocalModule(db_path),
        NetworkPublicModule(db_path),
        NetworkRoutingModule(db_path),
        ObstructionModule(db_path),
        LoadModule(db_path),
        WeatherHourlyModule(),
        WeatherDailyModule(),
        RTCModule(),
    ]
    for mod in modules:
        try:
            signal = mod.run()
            registry.register(signal)
        except Exception as exc:
            click.echo(
                f"  [warn] Signal module {mod.id} failed: {exc}", err=True
            )
    return registry


@click.group()
@click.option(
    "--db",
    default=_DB_PATH_DEFAULT,
    envvar="DB_PATH",
    show_default=True,
    help="Path to the shared Starlink SQLite telemetry database.",
)
@click.option(
    "--data-dir",
    default=_DATA_DIR_DEFAULT,
    envvar="PP_DATA_DIR",
    show_default=True,
    help="Directory for JSONL signals, RCA SQLite, and JSON exports.",
)
@click.pass_context
def cli(ctx: click.Context, db: str, data_dir: str) -> None:
    """pp-starlink: root-cause analysis for Starlink MOS drops."""
    ctx.ensure_object(dict)
    ctx.obj["db"] = db
    ctx.obj["data_dir"] = Path(data_dir)


# ---------------------------------------------------------------------------
# collect
# ---------------------------------------------------------------------------

@cli.command()
@click.pass_context
def collect(ctx: click.Context) -> None:
    """Run all signal modules and write collected data to storage."""
    db_path = ctx.obj["db"]
    data_dir = ctx.obj["data_dir"]

    click.echo(f"Collecting signals from {db_path} ...")

    registry = _build_registry(db_path)
    signals = registry.all()

    if not signals:
        click.echo("No signals collected.", err=True)
        sys.exit(1)

    with StorageWriter(data_dir=data_dir) as writer:
        total_records = 0
        for sig_id, signal in signals.items():
            n = writer.write_signal(signal)
            click.echo(f"  {sig_id:<35} {n:>6} records")
            total_records += n

    click.echo(f"\nCollected {total_records} records across {len(signals)} signals.")


# ---------------------------------------------------------------------------
# analyze
# ---------------------------------------------------------------------------

@cli.command()
@click.option(
    "--loss-threshold",
    default=0.05,
    show_default=True,
    type=float,
    help="Packet loss fraction to trigger incident (0–1).",
)
@click.option(
    "--latency-threshold",
    default=200.0,
    show_default=True,
    type=float,
    help="Latency threshold in ms to trigger incident.",
)
@click.option("--export-json", is_flag=True, default=False, help="Also export incidents as JSON.")
@click.pass_context
def analyze(
    ctx: click.Context,
    loss_threshold: float,
    latency_threshold: float,
    export_json: bool,
) -> None:
    """Detect incidents and run the RCA engine. Results saved to storage."""
    db_path = ctx.obj["db"]
    data_dir = ctx.obj["data_dir"]

    click.echo(f"Reading telemetry from {db_path} ...")

    try:
        with TelemetryReader(db_path) as reader:
            rows = reader.read_network()
    except Exception as exc:
        click.echo(f"Error reading database: {exc}", err=True)
        sys.exit(1)

    if not rows:
        click.echo("No network_telemetry rows found.", err=True)
        sys.exit(1)

    click.echo(f"  {len(rows)} samples loaded.")

    # Detect incidents
    incidents = detect_incidents(
        rows,
        loss_threshold=loss_threshold,
        latency_threshold=latency_threshold,
    )
    click.echo(f"  {len(incidents)} incident(s) detected.")

    if not incidents:
        click.echo("No incidents found — nothing to analyze.")
        return

    # Collect signals
    click.echo("Collecting context signals ...")
    registry = _build_registry(db_path)

    # Run RCA engine
    click.echo("Running RCA engine ...")
    engine = RCAEngine()
    engine.run(incidents, registry)

    # Persist
    with StorageWriter(data_dir=data_dir) as writer:
        n = writer.write_incidents(incidents)
        click.echo(f"  {n} incidents written to {writer._rca_db_path}")
        if export_json:
            path = writer.export_incidents_json(incidents)
            click.echo(f"  JSON exported to {path}")

    # Print summary
    click.echo("\n" + format_worst_day_summary(incidents))

    # Cause breakdown
    from collections import Counter
    causes = Counter(inc.root_cause or "UNKNOWN" for inc in incidents)
    click.echo("\nRoot causes:")
    for cause, count in causes.most_common():
        click.echo(f"  {cause:<35} {count}")


# ---------------------------------------------------------------------------
# report
# ---------------------------------------------------------------------------

@cli.command()
@click.option("--json-output", "as_json", is_flag=True, default=False, help="Output as JSON.")
@click.option("--ai-bundle", is_flag=True, default=False, help="Output AI-first RCA bundle JSON.")
@click.option("--limit", default=100, show_default=True, type=int, help="Max incidents to include.")
@click.pass_context
def report(ctx: click.Context, as_json: bool, ai_bundle: bool, limit: int) -> None:
    """Print a formatted RCA report from stored incidents."""
    data_dir = ctx.obj["data_dir"]

    with StorageWriter(data_dir=data_dir) as writer:
        stored = writer.list_incidents(limit=limit)

    if not stored:
        click.echo("No incidents found in storage. Run `analyze` first.")
        return

    # Reconstruct minimal Incident objects for formatting
    from pp_starlink.core.models import Incident

    def _parse_json_list(raw: object) -> list[str]:
        if not raw:
            return []
        if isinstance(raw, str):
            try:
                v = json.loads(raw)
                if isinstance(v, list):
                    return [str(x) for x in v]
            except Exception:
                return []
        if isinstance(raw, list):
            return [str(x) for x in raw]
        return []

    incidents = []
    for row in stored:
        inc = Incident(
            id=row["id"],
            start_time=row["start_time"],
            end_time=row["end_time"],
            duration_seconds=row.get("duration_seconds") or 0,
            metrics={
                "packet_loss_max": row.get("packet_loss_max") or 0.0,
                "packet_loss_avg": row.get("packet_loss_avg") or 0.0,
                "latency_max": row.get("latency_max"),
                "jitter_max": row.get("jitter_max"),
                "pop_drop_max": row.get("pop_drop_max"),
                "sample_count": row.get("sample_count"),
            },
            root_cause=row.get("root_cause"),
            confidence=row.get("confidence"),
            evidence=_parse_json_list(row.get("evidence")),
            missing_evidence=_parse_json_list(row.get("missing_evidence")),
            signals=_parse_json_list(row.get("signals")),
        )
        incidents.append(inc)

    if ai_bundle:
        click.echo(incidents_to_ai_bundle_json(incidents))
    elif as_json:
        click.echo(incidents_to_json(incidents))
    else:
        click.echo(format_report(incidents))


# ---------------------------------------------------------------------------
# incident
# ---------------------------------------------------------------------------

@cli.command()
@click.option("--limit", default=20, show_default=True, type=int)
@click.option("--json-output", "as_json", is_flag=True, default=False)
@click.pass_context
def incident(ctx: click.Context, limit: int, as_json: bool) -> None:
    """List stored incidents (most recent first)."""
    data_dir = ctx.obj["data_dir"]

    with StorageWriter(data_dir=data_dir) as writer:
        rows = writer.list_incidents(limit=limit)

    if not rows:
        click.echo("No incidents found. Run `analyze` first.")
        return

    if as_json:
        click.echo(json.dumps(rows, indent=2, default=str))
        return

    click.echo(f"{'ID':<12} {'Start':<26} {'Duration':>10} {'Loss%':>7} {'Cause':<35} {'Conf'}")
    click.echo("─" * 100)
    for row in rows:
        loss = row.get("packet_loss_max") or 0.0
        dur = row.get("duration_seconds") or 0
        cause = (row.get("root_cause") or "UNKNOWN")[:34]
        conf = (row.get("confidence") or "?")[:6]
        start = (row.get("start_time") or "")[:25]
        inc_id = (row.get("id") or "")[:11]
        click.echo(f"{inc_id:<12} {start:<26} {dur:>9}s {loss*100:>6.1f}% {cause:<35} {conf}")


# ---------------------------------------------------------------------------
# signals
# ---------------------------------------------------------------------------

@cli.group()
def signals() -> None:
    """Signal management commands."""


def _print_signals_list(db_path: str) -> None:
    click.echo(f"Collecting signals from {db_path} ...")
    registry = _build_registry(db_path)

    all_sigs = registry.all()
    if not all_sigs:
        click.echo("No signals collected.")
        return

    click.echo(f"\n{'Signal ID':<35} {'Source':<25} {'Resolution':<12} {'Records':>8} {'Quality'}")
    click.echo("─" * 95)
    for sig_id, sig in sorted(all_sigs.items()):
        qualities = {r.quality for r in sig.records}
        q_str = "+".join(sorted(qualities)) if qualities else "—"
        click.echo(
            f"{sig_id:<35} {sig.source:<25} {sig.resolution:<12} "
            f"{len(sig.records):>8} {q_str}"
        )


@signals.command("list")
@click.pass_context
def signals_list(ctx: click.Context) -> None:
    """List all registered signals and their record counts."""
    db_path = ctx.obj["db"]
    _print_signals_list(db_path)


@cli.command("signals-list")
@click.pass_context
def signals_list_alias(ctx: click.Context) -> None:
    """Alias for `signals list` shown in top-level help output."""
    db_path = ctx.obj["db"]
    _print_signals_list(db_path)


def main() -> None:
    cli(obj={})


if __name__ == "__main__":
    main()
