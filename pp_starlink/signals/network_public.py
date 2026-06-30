"""
network.public_path signal — internet / public packet-loss path health.

Sources: network_telemetry.public_packet_loss
         network_telemetry.pop_latency_ms
         network_telemetry.pop_drop_rate
         network_telemetry.pop_jitter

SignalRecord.value keys:
  public_packet_loss: float 0–1
  pop_latency_ms:     float (None if unavailable)
  pop_drop_rate:      float 0–1 (None if unavailable)
  pop_jitter_ms:      float (None if unavailable)
  degraded:           True if loss > LOSS_THRESHOLD or pop_latency > LATENCY_THRESHOLD_MS
"""

from __future__ import annotations

import os
from datetime import datetime, timezone
from typing import List, Optional

from pp_starlink.core.models import ContextSignal, SignalRecord
from pp_starlink.signals.base import SignalModule
from pp_starlink.telemetry.reader import TelemetryReader

LOSS_THRESHOLD = 0.05       # 5 %
LATENCY_THRESHOLD_MS = 200.0


def _ts_to_iso(unix_s: int) -> str:
    return datetime.fromtimestamp(unix_s, tz=timezone.utc).isoformat()


class NetworkPublicModule(SignalModule):
    id = "network.public_path"
    name = "Public Internet Path (packet loss + POP)"

    def __init__(self, db_path: Optional[str] = None) -> None:
        self._db_path = db_path or os.getenv("DB_PATH", "/data/starlink_telemetry.db")

    def collect(self) -> List[dict]:
        with TelemetryReader(self._db_path) as r:
            return r.read_network()

    def parse(self, raw: List[dict]) -> List[dict]:
        parsed = []
        for row in raw:
            ts = row.get("timestamp")
            if ts is None:
                continue
            loss = row.get("public_packet_loss") or 0.0
            pop_lat = row.get("pop_latency_ms")
            pop_drop = row.get("pop_drop_rate")
            pop_jitter = row.get("pop_jitter")
            degraded = loss > LOSS_THRESHOLD or (
                pop_lat is not None and pop_lat > LATENCY_THRESHOLD_MS
            )
            parsed.append(
                {
                    "timestamp": ts,
                    "public_packet_loss": loss,
                    "pop_latency_ms": pop_lat,
                    "pop_drop_rate": pop_drop,
                    "pop_jitter_ms": pop_jitter,
                    "degraded": degraded,
                }
            )
        return parsed

    def normalize(self, parsed: List[dict]) -> ContextSignal:
        records: List[SignalRecord] = []
        for p in parsed:
            quality = "observed"
            evidence = None
            if p["degraded"]:
                parts = []
                if p["public_packet_loss"] > LOSS_THRESHOLD:
                    parts.append(f"loss={p['public_packet_loss']*100:.1f}%")
                if p["pop_latency_ms"] is not None and p["pop_latency_ms"] > LATENCY_THRESHOLD_MS:
                    parts.append(f"pop_lat={p['pop_latency_ms']:.0f}ms")
                evidence = parts or None
            records.append(
                SignalRecord(
                    timestamp=_ts_to_iso(p["timestamp"]),
                    value={
                        "public_packet_loss": p["public_packet_loss"],
                        "pop_latency_ms": p["pop_latency_ms"],
                        "pop_drop_rate": p["pop_drop_rate"],
                        "pop_jitter_ms": p["pop_jitter_ms"],
                        "degraded": p["degraded"],
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
