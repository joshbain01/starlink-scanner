"""
rf.environment signal — RF beacon SNR/noise context from rf_telemetry.

Sources:
  rf_telemetry.beacon_snr_db
  rf_telemetry.noise_floor_db

SignalRecord.value keys:
  beacon_snr_db:         float | None
  noise_floor_db:        float | None
  snr_margin_db:         float | None  (snr - noise)
  snr_drop_from_median:  float | None
  noise_rise_from_median: float | None
  rf_interference_suspected: bool
"""

from __future__ import annotations

import os
from datetime import datetime, timezone
from statistics import median
from typing import List, Optional

from pp_starlink.core.models import ContextSignal, SignalRecord
from pp_starlink.signals.base import SignalModule
from pp_starlink.telemetry.reader import TelemetryReader

_INTERFERENCE_NOISE_RISE_DB = 3.0
_INTERFERENCE_SNR_DROP_DB = 2.0
_LOW_MARGIN_DB = 6.0


def _ns_to_iso(unix_ns: int) -> str:
    return datetime.fromtimestamp(unix_ns / 1_000_000_000, tz=timezone.utc).isoformat()


class RFEnvironmentModule(SignalModule):
    id = "rf.environment"
    name = "RF Environment (SNR + Noise Floor)"

    def __init__(self, db_path: Optional[str] = None) -> None:
        self._db_path = db_path or os.getenv("DB_PATH", "/data/starlink_telemetry.db")

    def collect(self) -> List[dict]:
        with TelemetryReader(self._db_path) as r:
            if not r.table_exists("rf_telemetry"):
                return []
            return r.read_rf()

    def parse(self, raw: List[dict]) -> List[dict]:
        snr_vals = [float(row["beacon_snr_db"]) for row in raw if row.get("beacon_snr_db") is not None]
        noise_vals = [float(row["noise_floor_db"]) for row in raw if row.get("noise_floor_db") is not None]
        snr_med = median(snr_vals) if snr_vals else None
        noise_med = median(noise_vals) if noise_vals else None

        parsed: List[dict] = []
        for row in raw:
            ts = row.get("timestamp")
            if ts is None:
                continue
            snr = row.get("beacon_snr_db")
            noise = row.get("noise_floor_db")

            margin = None
            if snr is not None and noise is not None:
                margin = float(snr) - float(noise)

            snr_drop = None
            if snr is not None and snr_med is not None:
                snr_drop = float(snr_med) - float(snr)

            noise_rise = None
            if noise is not None and noise_med is not None:
                noise_rise = float(noise) - float(noise_med)

            suspected = bool(
                noise_rise is not None
                and noise_rise >= _INTERFERENCE_NOISE_RISE_DB
                and (
                    (snr_drop is not None and snr_drop >= _INTERFERENCE_SNR_DROP_DB)
                    or (margin is not None and margin < _LOW_MARGIN_DB)
                )
            )

            parsed.append(
                {
                    "timestamp": ts,
                    "beacon_snr_db": snr,
                    "noise_floor_db": noise,
                    "snr_margin_db": margin,
                    "snr_drop_from_median": snr_drop,
                    "noise_rise_from_median": noise_rise,
                    "rf_interference_suspected": suspected,
                }
            )
        return parsed

    def normalize(self, parsed: List[dict]) -> ContextSignal:
        records: List[SignalRecord] = []
        for p in parsed:
            quality = "observed" if p["beacon_snr_db"] is not None else "missing"
            evidence = None
            if p["rf_interference_suspected"]:
                parts: list[str] = []
                if p["noise_rise_from_median"] is not None:
                    parts.append(f"noise rise {p['noise_rise_from_median']:.1f}dB")
                if p["snr_drop_from_median"] is not None:
                    parts.append(f"snr drop {p['snr_drop_from_median']:.1f}dB")
                if p["snr_margin_db"] is not None:
                    parts.append(f"snr margin {p['snr_margin_db']:.1f}dB")
                evidence = parts or None

            records.append(
                SignalRecord(
                    timestamp=_ns_to_iso(int(p["timestamp"])),
                    value={
                        "beacon_snr_db": p["beacon_snr_db"],
                        "noise_floor_db": p["noise_floor_db"],
                        "snr_margin_db": p["snr_margin_db"],
                        "snr_drop_from_median": p["snr_drop_from_median"],
                        "noise_rise_from_median": p["noise_rise_from_median"],
                        "rf_interference_suspected": p["rf_interference_suspected"],
                    },
                    quality=quality,
                    evidence=evidence,
                )
            )

        return ContextSignal(
            id=self.id,
            name=self.name,
            source="rf_telemetry",
            resolution="sample",
            records=records,
        )
