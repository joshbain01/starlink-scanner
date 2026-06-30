"""
load signal — bufferbloat and bandwidth load indicators.

Sources:
  network_telemetry.local_jitter      — gateway ICMP jitter (proxy for bufferbloat)
  network_telemetry.pop_jitter        — POP jitter (asymmetry indicator)
  network_telemetry.downlink_bps
  network_telemetry.uplink_bps
  network_telemetry.max_latency_ms    — peak latency in the 15 s window
  network_telemetry.min_latency_ms
  network_telemetry.brief_outage_count
  network_telemetry.brief_outage_duration_s

SignalRecord.value keys:
  local_jitter_ms:       float
  pop_jitter_ms:         float
  downlink_bps:          float
  uplink_bps:            float
  max_latency_ms:        float
  min_latency_ms:        float
  brief_outage_count:    int
  brief_outage_duration_s: float
  jitter_asymmetry_ms:   float (pop_jitter - local_jitter; positive = WAN-side)
  bufferbloat_suspected: True if jitter high but loss low
"""

from __future__ import annotations

import os
from datetime import datetime, timezone
from typing import List, Optional

from pp_starlink.core.models import ContextSignal, SignalRecord
from pp_starlink.signals.base import SignalModule
from pp_starlink.telemetry.reader import TelemetryReader

JITTER_HIGH_MS = 20.0        # local jitter threshold for bufferbloat suspicion
LOW_LOSS_THRESHOLD = 0.05    # loss < this while jitter high → bufferbloat


def _ts_to_iso(unix_s: int) -> str:
    return datetime.fromtimestamp(unix_s, tz=timezone.utc).isoformat()


class LoadModule(SignalModule):
    id = "load"
    name = "Load / Bufferbloat"

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
            local_j = row.get("local_jitter")
            pop_j = row.get("pop_jitter")
            loss = row.get("public_packet_loss") or 0.0
            asymmetry = None
            if pop_j is not None and local_j is not None:
                asymmetry = pop_j - local_j
            bufferbloat = (
                local_j is not None
                and local_j > JITTER_HIGH_MS
                and loss < LOW_LOSS_THRESHOLD
            )
            parsed.append(
                {
                    "timestamp": ts,
                    "local_jitter_ms": local_j,
                    "pop_jitter_ms": pop_j,
                    "downlink_bps": row.get("downlink_bps"),
                    "uplink_bps": row.get("uplink_bps"),
                    "max_latency_ms": row.get("max_latency_ms"),
                    "min_latency_ms": row.get("min_latency_ms"),
                    "brief_outage_count": row.get("brief_outage_count"),
                    "brief_outage_duration_s": row.get("brief_outage_duration_s"),
                    "jitter_asymmetry_ms": asymmetry,
                    "bufferbloat_suspected": bufferbloat,
                }
            )
        return parsed

    def normalize(self, parsed: List[dict]) -> ContextSignal:
        records: List[SignalRecord] = []
        for p in parsed:
            quality = "observed" if p["local_jitter_ms"] is not None else "missing"
            evidence = None
            if p["bufferbloat_suspected"]:
                evidence = [
                    f"local_jitter={p['local_jitter_ms']:.1f}ms with low loss"
                ]
            records.append(
                SignalRecord(
                    timestamp=_ts_to_iso(p["timestamp"]),
                    value={
                        "local_jitter_ms": p["local_jitter_ms"],
                        "pop_jitter_ms": p["pop_jitter_ms"],
                        "downlink_bps": p["downlink_bps"],
                        "uplink_bps": p["uplink_bps"],
                        "max_latency_ms": p["max_latency_ms"],
                        "min_latency_ms": p["min_latency_ms"],
                        "brief_outage_count": p["brief_outage_count"],
                        "brief_outage_duration_s": p["brief_outage_duration_s"],
                        "jitter_asymmetry_ms": p["jitter_asymmetry_ms"],
                        "bufferbloat_suspected": p["bufferbloat_suspected"],
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
