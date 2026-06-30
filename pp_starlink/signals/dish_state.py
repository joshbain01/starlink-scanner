"""
dish.state signal — tracks dish operating mode and outage cause from
network_telemetry.outage_cause and outage_telemetry.cause.

Key values surfaced in SignalRecord.value:
  state:    "SEARCHING" | "CONNECTED" | "BOOTING" | "STOWED" | "UNKNOWN"
  cause:    raw outage_cause string from the dish (may be None)
  sky_search: True if cause contains SKY_SEARCH
"""

from __future__ import annotations

import os
from datetime import datetime, timezone
from typing import Any, List, Optional

from pp_starlink.core.models import ContextSignal, SignalRecord
from pp_starlink.signals.base import SignalModule
from pp_starlink.telemetry.reader import TelemetryReader


def _ts_to_iso(unix_s: int) -> str:
    return datetime.fromtimestamp(unix_s, tz=timezone.utc).isoformat()


def _outage_state(outage_cause: Optional[str], currently_obstructed: Optional[int]) -> str:
    if outage_cause and outage_cause not in ("", "UNKNOWN"):
        cause_up = outage_cause.upper()
        if "SEARCHING" in cause_up or "SKY_SEARCH" in cause_up:
            return "SEARCHING"
        if "BOOTING" in cause_up or "REBOOTING" in cause_up:
            return "BOOTING"
        if "STOWED" in cause_up:
            return "STOWED"
        if "OBSTRUCT" in cause_up:
            return "OBSTRUCTED"
        return "DEGRADED"
    if currently_obstructed:
        return "OBSTRUCTED"
    return "CONNECTED"


class DishStateModule(SignalModule):
    id = "dish.state"
    name = "Dish Operating State"

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
            cause = row.get("outage_cause")
            obstructed = row.get("currently_obstructed") or 0
            state = _outage_state(cause, obstructed)
            parsed.append(
                {
                    "timestamp": ts,
                    "state": state,
                    "cause": cause,
                    "sky_search": bool(cause and "SKY_SEARCH" in str(cause).upper()),
                    "uptime_s": row.get("uptime_s"),
                }
            )
        return parsed

    def normalize(self, parsed: List[dict]) -> ContextSignal:
        records: List[SignalRecord] = []
        for p in parsed:
            records.append(
                SignalRecord(
                    timestamp=_ts_to_iso(p["timestamp"]),
                    value={
                        "state": p["state"],
                        "cause": p["cause"],
                        "sky_search": p["sky_search"],
                        "uptime_s": p["uptime_s"],
                    },
                    quality="observed",
                    evidence=[f"outage_cause={p['cause']}"] if p["cause"] else None,
                )
            )
        return ContextSignal(
            id=self.id,
            name=self.name,
            source="network_telemetry",
            resolution="sample",
            records=records,
        )
