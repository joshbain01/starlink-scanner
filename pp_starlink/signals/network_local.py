"""
network.local_path signal — LAN / gateway path health.

Sources: network_telemetry.local_jitter (gateway ICMP jitter in ms)
         network_telemetry.alert_no_ethernet_link

SignalRecord.value keys:
  local_jitter_ms:   float  (None if not available)
  no_ethernet_link:  bool
  degraded:          True when jitter > JITTER_THRESHOLD_MS
"""

from __future__ import annotations

import os
from datetime import datetime, timezone
from typing import List, Optional

from pp_starlink.core.models import ContextSignal, SignalRecord
from pp_starlink.signals.base import SignalModule
from pp_starlink.telemetry.reader import TelemetryReader

JITTER_THRESHOLD_MS = 20.0  # gateway jitter above this → local path degraded


def _ts_to_iso(unix_s: int) -> str:
    return datetime.fromtimestamp(unix_s, tz=timezone.utc).isoformat()


class NetworkLocalModule(SignalModule):
    id = "network.local_path"
    name = "Local LAN / Gateway Path"

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
            jitter = row.get("local_jitter")
            no_eth = bool(row.get("alert_no_ethernet_link"))
            degraded = (jitter is not None and jitter > JITTER_THRESHOLD_MS) or no_eth
            parsed.append(
                {
                    "timestamp": ts,
                    "local_jitter_ms": jitter,
                    "no_ethernet_link": no_eth,
                    "degraded": degraded,
                }
            )
        return parsed

    def normalize(self, parsed: List[dict]) -> ContextSignal:
        records: List[SignalRecord] = []
        for p in parsed:
            quality = "observed" if p["local_jitter_ms"] is not None else "missing"
            evidence = None
            if p["degraded"]:
                parts = []
                if p["no_ethernet_link"]:
                    parts.append("no ethernet link")
                if p["local_jitter_ms"] is not None and p["local_jitter_ms"] > JITTER_THRESHOLD_MS:
                    parts.append(f"local jitter={p['local_jitter_ms']:.1f}ms")
                evidence = parts or None
            records.append(
                SignalRecord(
                    timestamp=_ts_to_iso(p["timestamp"]),
                    value={
                        "local_jitter_ms": p["local_jitter_ms"],
                        "no_ethernet_link": p["no_ethernet_link"],
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
