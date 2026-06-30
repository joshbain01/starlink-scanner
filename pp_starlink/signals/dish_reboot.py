"""
dish.reboot signal — detects uptime resets in network_telemetry.

A reboot is flagged when uptime_s drops relative to the previous sample
(uptime clock reset) or when uptime_s is very small (<= 60 s) on the first
sample of a window.

SignalRecord.value keys:
  uptime_s:    current uptime in seconds
  reboot:      True if an uptime reset was detected at this timestamp
  prev_uptime: previous uptime_s (None for first record)
"""

from __future__ import annotations

import os
from datetime import datetime, timezone
from typing import Any, List, Optional

from pp_starlink.core.models import ContextSignal, SignalRecord
from pp_starlink.signals.base import SignalModule
from pp_starlink.telemetry.reader import TelemetryReader

_BOOT_WINDOW_S = 60  # uptime <= this on first sample → treat as fresh boot


def _ts_to_iso(unix_s: int) -> str:
    return datetime.fromtimestamp(unix_s, tz=timezone.utc).isoformat()


class DishRebootModule(SignalModule):
    id = "dish.reboot"
    name = "Dish Reboot / Uptime Reset"

    def __init__(self, db_path: Optional[str] = None) -> None:
        self._db_path = db_path or os.getenv("DB_PATH", "/data/starlink_telemetry.db")

    def collect(self) -> List[dict]:
        with TelemetryReader(self._db_path) as r:
            return r.read_network()

    def parse(self, raw: List[dict]) -> List[dict]:
        parsed = []
        prev_uptime: Optional[int] = None
        for row in raw:
            ts = row.get("timestamp")
            uptime = row.get("uptime_s")
            if ts is None:
                prev_uptime = uptime
                continue
            reboot = False
            if uptime is not None:
                if prev_uptime is not None and uptime < prev_uptime:
                    reboot = True  # uptime decreased → reset
                elif prev_uptime is None and uptime <= _BOOT_WINDOW_S:
                    reboot = True  # first sample with very short uptime
            parsed.append(
                {
                    "timestamp": ts,
                    "uptime_s": uptime,
                    "prev_uptime": prev_uptime,
                    "reboot": reboot,
                }
            )
            prev_uptime = uptime
        return parsed

    def normalize(self, parsed: List[dict]) -> ContextSignal:
        records: List[SignalRecord] = []
        for p in parsed:
            quality = "observed" if p["uptime_s"] is not None else "missing"
            evidence = None
            if p["reboot"]:
                evidence = [
                    f"uptime reset: {p['prev_uptime']}s → {p['uptime_s']}s"
                ]
            records.append(
                SignalRecord(
                    timestamp=_ts_to_iso(p["timestamp"]),
                    value={
                        "uptime_s": p["uptime_s"],
                        "reboot": p["reboot"],
                        "prev_uptime": p["prev_uptime"],
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
