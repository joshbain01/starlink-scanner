"""
obstruction signal — physical sky obstruction data from the dish.

Sources:
  network_telemetry.obstruction_fraction   — fraction of sky blocked (0–1)
  network_telemetry.currently_obstructed   — boolean flag from dish
  network_telemetry.boresight_azimuth_deg  — dish pointing azimuth
  network_telemetry.boresight_elevation_deg
  network_telemetry.calculated_azimuth     — satellite az (orbital module)
  network_telemetry.calculated_elevation   — satellite el (orbital module)

The OBSTRUCTION RCA rule checks that azimuth/elevation data is present before
firing (per spec guardrail).

SignalRecord.value keys:
  obstruction_fraction:    float 0–1
  currently_obstructed:    bool
  boresight_azimuth_deg:   float | None
  boresight_elevation_deg: float | None
  satellite_azimuth:       float | None
  satellite_elevation:     float | None
  obstructed:              True if currently_obstructed OR fraction > threshold
"""

from __future__ import annotations

import os
from datetime import datetime, timezone
from typing import List, Optional

from pp_starlink.core.models import ContextSignal, SignalRecord
from pp_starlink.signals.base import SignalModule
from pp_starlink.telemetry.reader import TelemetryReader

OBSTRUCTION_THRESHOLD = 0.01  # > 1% of sky blocked = meaningful obstruction


def _ts_to_iso(unix_s: int) -> str:
    return datetime.fromtimestamp(unix_s, tz=timezone.utc).isoformat()


class ObstructionModule(SignalModule):
    id = "obstruction"
    name = "Sky Obstruction"

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
            fraction = row.get("obstruction_fraction") or 0.0
            obstructed_flag = bool(row.get("currently_obstructed"))
            boresight_az = row.get("boresight_azimuth_deg")
            boresight_el = row.get("boresight_elevation_deg")
            sat_az = row.get("calculated_azimuth")
            sat_el = row.get("calculated_elevation")
            obstructed = obstructed_flag or (fraction > OBSTRUCTION_THRESHOLD)
            parsed.append(
                {
                    "timestamp": ts,
                    "obstruction_fraction": fraction,
                    "currently_obstructed": obstructed_flag,
                    "boresight_azimuth_deg": boresight_az,
                    "boresight_elevation_deg": boresight_el,
                    "satellite_azimuth": sat_az,
                    "satellite_elevation": sat_el,
                    "obstructed": obstructed,
                    "has_pointing_data": boresight_az is not None or sat_az is not None,
                }
            )
        return parsed

    def normalize(self, parsed: List[dict]) -> ContextSignal:
        records: List[SignalRecord] = []
        for p in parsed:
            quality = "observed"
            evidence = None
            if p["obstructed"]:
                parts = []
                if p["currently_obstructed"]:
                    parts.append("currently_obstructed=true")
                if p["obstruction_fraction"] > OBSTRUCTION_THRESHOLD:
                    parts.append(f"obstruction_fraction={p['obstruction_fraction']:.3f}")
                if p["boresight_elevation_deg"] is not None:
                    parts.append(f"dish_el={p['boresight_elevation_deg']:.1f}°")
                evidence = parts or None
            records.append(
                SignalRecord(
                    timestamp=_ts_to_iso(p["timestamp"]),
                    value={
                        "obstruction_fraction": p["obstruction_fraction"],
                        "currently_obstructed": p["currently_obstructed"],
                        "boresight_azimuth_deg": p["boresight_azimuth_deg"],
                        "boresight_elevation_deg": p["boresight_elevation_deg"],
                        "satellite_azimuth": p["satellite_azimuth"],
                        "satellite_elevation": p["satellite_elevation"],
                        "obstructed": p["obstructed"],
                        "has_pointing_data": p["has_pointing_data"],
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
