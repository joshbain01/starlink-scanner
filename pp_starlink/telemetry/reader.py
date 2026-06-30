"""
Telemetry reader: reads raw rows from the shared SQLite database.

This module never writes — it is a pure read path.  DB_PATH follows the same
env-var convention as the Go daemon and rf_listener.py.

network_telemetry.timestamp is stored as Unix seconds (int).
rf_telemetry.timestamp is stored as Unix nanoseconds (int) — per rf_listener.py.
outage_telemetry.start_timestamp_ns is Unix nanoseconds.
"""

from __future__ import annotations

import os
import sqlite3
from typing import Any, Dict, Iterator, List, Optional

DB_PATH_DEFAULT = "/data/starlink_telemetry.db"

_NETWORK_COLS = (
    "timestamp",
    "uptime_s",
    "obstruction_fraction",
    "alert_flags",
    "local_jitter",
    "pop_jitter",
    "public_packet_loss",
    "lower_signal_than_predicted",
    "is_snr_above_noise_floor",
    "target_satellite_id",
    "calculated_azimuth",
    "calculated_elevation",
    "pop_latency_ms",
    "pop_drop_rate",
    "currently_obstructed",
    "outage_cause",
    "downlink_bps",
    "uplink_bps",
    "is_snr_persistently_low",
    "boresight_azimuth_deg",
    "boresight_elevation_deg",
    "tilt_angle_deg",
    "dl_bandwidth_restricted_reason",
    "ul_bandwidth_restricted_reason",
    "eth_speed_mbps",
    "alert_is_heating",
    "alert_power_supply_thermal_throttle",
    "alert_dish_water_detected",
    "alert_router_water_detected",
    "alert_no_ethernet_link",
    "alert_roaming",
    "max_latency_ms",
    "min_latency_ms",
    "brief_outage_count",
    "brief_outage_duration_s",
)

_RF_COLS = ("timestamp", "beacon_snr_db", "noise_floor_db")

_OUTAGE_COLS = ("start_timestamp_ns", "duration_ns", "cause", "did_switch")

_EVENT_COLS = ("start_timestamp_ns", "severity", "reason", "duration_ns", "device_id")


def _open(db_path: str) -> sqlite3.Connection:
    con = sqlite3.connect(db_path, check_same_thread=False)
    con.row_factory = sqlite3.Row
    con.execute("PRAGMA journal_mode = WAL")
    con.execute("PRAGMA temp_store = MEMORY")
    con.execute("PRAGMA synchronous = OFF")
    con.execute("PRAGMA cache_size = -4000")
    return con


def _rows_to_dicts(rows: List[sqlite3.Row]) -> List[Dict[str, Any]]:
    return [dict(r) for r in rows]


class TelemetryReader:
    """Read-only access to the shared SQLite telemetry database."""

    def __init__(self, db_path: Optional[str] = None) -> None:
        self._path = db_path or os.getenv("DB_PATH", DB_PATH_DEFAULT)
        self._con: Optional[sqlite3.Connection] = None

    # ------------------------------------------------------------------
    # Connection lifecycle
    # ------------------------------------------------------------------

    def open(self) -> "TelemetryReader":
        self._con = _open(self._path)
        return self

    def close(self) -> None:
        if self._con is not None:
            self._con.close()
            self._con = None

    def __enter__(self) -> "TelemetryReader":
        return self.open()

    def __exit__(self, *_: Any) -> None:
        self.close()

    def _conn(self) -> sqlite3.Connection:
        if self._con is None:
            raise RuntimeError("TelemetryReader not opened — use as context manager or call .open()")
        return self._con

    # ------------------------------------------------------------------
    # network_telemetry (timestamp = Unix seconds)
    # ------------------------------------------------------------------

    def read_network(
        self,
        start_unix: Optional[int] = None,
        end_unix: Optional[int] = None,
        limit: Optional[int] = None,
    ) -> List[Dict[str, Any]]:
        """Return network_telemetry rows as dicts, ordered by timestamp ASC.

        start_unix / end_unix are inclusive Unix second bounds.
        Only columns present in the schema are selected (missing cols → None).
        """
        existing = self._existing_columns("network_telemetry")
        cols = ", ".join(c if c in existing else f"NULL AS {c}" for c in _NETWORK_COLS)
        sql = f"SELECT {cols} FROM network_telemetry"
        params: List[Any] = []
        clauses: List[str] = []
        if start_unix is not None:
            clauses.append("timestamp >= ?")
            params.append(start_unix)
        if end_unix is not None:
            clauses.append("timestamp <= ?")
            params.append(end_unix)
        if clauses:
            sql += " WHERE " + " AND ".join(clauses)
        sql += " ORDER BY timestamp ASC"
        if limit is not None:
            sql += f" LIMIT {int(limit)}"
        rows = self._conn().execute(sql, params).fetchall()
        return _rows_to_dicts(rows)

    # ------------------------------------------------------------------
    # rf_telemetry (timestamp = Unix nanoseconds per rf_listener.py)
    # ------------------------------------------------------------------

    def read_rf(
        self,
        start_unix_ns: Optional[int] = None,
        end_unix_ns: Optional[int] = None,
    ) -> List[Dict[str, Any]]:
        """Return rf_telemetry rows as dicts, ordered by timestamp ASC."""
        existing = self._existing_columns("rf_telemetry")
        cols = ", ".join(c if c in existing else f"NULL AS {c}" for c in _RF_COLS)
        sql = f"SELECT {cols} FROM rf_telemetry"
        params: List[Any] = []
        clauses: List[str] = []
        if start_unix_ns is not None:
            clauses.append("timestamp >= ?")
            params.append(start_unix_ns)
        if end_unix_ns is not None:
            clauses.append("timestamp <= ?")
            params.append(end_unix_ns)
        if clauses:
            sql += " WHERE " + " AND ".join(clauses)
        sql += " ORDER BY timestamp ASC"
        rows = self._conn().execute(sql, params).fetchall()
        return _rows_to_dicts(rows)

    # ------------------------------------------------------------------
    # outage_telemetry (start_timestamp_ns = Unix nanoseconds)
    # ------------------------------------------------------------------

    def read_outages(
        self,
        start_unix_ns: Optional[int] = None,
        end_unix_ns: Optional[int] = None,
    ) -> List[Dict[str, Any]]:
        """Return outage_telemetry rows ordered by start_timestamp_ns ASC."""
        sql = "SELECT * FROM outage_telemetry"
        params: List[Any] = []
        clauses: List[str] = []
        if start_unix_ns is not None:
            clauses.append("start_timestamp_ns >= ?")
            params.append(start_unix_ns)
        if end_unix_ns is not None:
            clauses.append("start_timestamp_ns <= ?")
            params.append(end_unix_ns)
        if clauses:
            sql += " WHERE " + " AND ".join(clauses)
        sql += " ORDER BY start_timestamp_ns ASC"
        rows = self._conn().execute(sql, params).fetchall()
        return _rows_to_dicts(rows)

    # ------------------------------------------------------------------
    # event_log (start_timestamp_ns = Unix nanoseconds)
    # ------------------------------------------------------------------

    def read_events(
        self,
        start_unix_ns: Optional[int] = None,
        end_unix_ns: Optional[int] = None,
    ) -> List[Dict[str, Any]]:
        """Return event_log rows ordered by start_timestamp_ns ASC."""
        sql = "SELECT * FROM event_log"
        params: List[Any] = []
        clauses: List[str] = []
        if start_unix_ns is not None:
            clauses.append("start_timestamp_ns >= ?")
            params.append(start_unix_ns)
        if end_unix_ns is not None:
            clauses.append("start_timestamp_ns <= ?")
            params.append(end_unix_ns)
        if clauses:
            sql += " WHERE " + " AND ".join(clauses)
        sql += " ORDER BY start_timestamp_ns ASC"
        rows = self._conn().execute(sql, params).fetchall()
        return _rows_to_dicts(rows)

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    def _existing_columns(self, table: str) -> set:
        """Return the set of column names actually present in *table*."""
        rows = self._conn().execute(f"PRAGMA table_info({table})").fetchall()
        return {r["name"] for r in rows}

    def table_exists(self, table: str) -> bool:
        row = self._conn().execute(
            "SELECT 1 FROM sqlite_master WHERE type='table' AND name=?", (table,)
        ).fetchone()
        return row is not None

    def time_range(self, table: str = "network_telemetry") -> tuple:
        """Return (min_ts, max_ts) as Unix seconds (None if table is empty)."""
        row = self._conn().execute(
            f"SELECT MIN(timestamp), MAX(timestamp) FROM {table}"
        ).fetchone()
        if row:
            return row[0], row[1]
        return None, None
