"""
network.routing signal — detects routing / POP change events.

Since the Go daemon does not record public IP or traceroute hashes directly,
this module derives a proxy: it hashes (pop_latency_ms bucket, pop_drop_rate
bucket) as a coarse "POP fingerprint".  A fingerprint change between
consecutive samples is flagged as a possible routing change.

SignalRecord.value keys:
  pop_fingerprint:  str hash of current POP characteristics
  routing_change:   True if fingerprint changed from previous sample
  pop_latency_ms:   float
  pop_drop_rate:    float
"""

from __future__ import annotations

import hashlib
import os
from datetime import datetime, timezone
from typing import List, Optional

from pp_starlink.core.models import ContextSignal, SignalRecord
from pp_starlink.signals.base import SignalModule
from pp_starlink.telemetry.reader import TelemetryReader


def _ts_to_iso(unix_s: int) -> str:
    return datetime.fromtimestamp(unix_s, tz=timezone.utc).isoformat()


def _pop_fingerprint(pop_latency_ms: Optional[float], pop_drop_rate: Optional[float]) -> Optional[str]:
    """Coarse fingerprint: bucket latency to 25ms, drop rate to 1%."""
    if pop_latency_ms is None:
        return None
    lat_bucket = int(pop_latency_ms // 25) * 25
    drop_bucket = int((pop_drop_rate or 0.0) * 100)
    raw = f"{lat_bucket}:{drop_bucket}"
    return hashlib.md5(raw.encode()).hexdigest()[:8]  # noqa: S324 — not security-critical


class NetworkRoutingModule(SignalModule):
    id = "network.routing"
    name = "Routing / POP Change"

    def __init__(self, db_path: Optional[str] = None) -> None:
        self._db_path = db_path or os.getenv("DB_PATH", "/data/starlink_telemetry.db")

    def collect(self) -> List[dict]:
        with TelemetryReader(self._db_path) as r:
            return r.read_network()

    def parse(self, raw: List[dict]) -> List[dict]:
        parsed = []
        prev_fp: Optional[str] = None
        for row in raw:
            ts = row.get("timestamp")
            if ts is None:
                continue
            pop_lat = row.get("pop_latency_ms")
            pop_drop = row.get("pop_drop_rate")
            fp = _pop_fingerprint(pop_lat, pop_drop)
            routing_change = (
                fp is not None
                and prev_fp is not None
                and fp != prev_fp
            )
            parsed.append(
                {
                    "timestamp": ts,
                    "pop_fingerprint": fp,
                    "routing_change": routing_change,
                    "pop_latency_ms": pop_lat,
                    "pop_drop_rate": pop_drop,
                }
            )
            if fp is not None:
                prev_fp = fp
        return parsed

    def normalize(self, parsed: List[dict]) -> ContextSignal:
        records: List[SignalRecord] = []
        for p in parsed:
            quality = "derived" if p["pop_fingerprint"] is not None else "missing"
            evidence = None
            if p["routing_change"]:
                evidence = [f"POP fingerprint changed to {p['pop_fingerprint']}"]
            records.append(
                SignalRecord(
                    timestamp=_ts_to_iso(p["timestamp"]),
                    value={
                        "pop_fingerprint": p["pop_fingerprint"],
                        "routing_change": p["routing_change"],
                        "pop_latency_ms": p["pop_latency_ms"],
                        "pop_drop_rate": p["pop_drop_rate"],
                    },
                    quality=quality,
                    evidence=evidence,
                )
            )
        return ContextSignal(
            id=self.id,
            name=self.name,
            source="network_telemetry",
            resolution="sample",
            records=records,
        )
