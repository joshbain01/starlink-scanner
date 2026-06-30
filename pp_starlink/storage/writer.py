"""
Storage writer — persists signals and incidents using SD-card-safe patterns.

Signal data: JSONL files under ./data/signals/{signal_id}.jsonl
  - Appended in batches; one JSON object per line.
  - File rotated daily (filename includes date).

Normalized incidents: SQLite table `rca_incidents`
  - INSERT OR REPLACE on primary key (incident.id).
  - Writes are batched, not per-sample.

JSON export: ./data/reports/incidents_{date}.json
  - Full JSON array; written atomically via temp file + rename.
"""

from __future__ import annotations

import json
import os
import sqlite3
import tempfile
from datetime import datetime, timezone
from pathlib import Path
from typing import List, Optional

from pp_starlink.core.models import ContextSignal, Incident

_DATA_DIR = Path(os.getenv("PP_DATA_DIR", "./data"))
_SIGNALS_DIR = _DATA_DIR / "signals"
_REPORTS_DIR = _DATA_DIR / "reports"
_RCA_DB_PATH = _DATA_DIR / "rca.db"

_PRAGMAS = [
    "PRAGMA journal_mode = WAL",
    "PRAGMA temp_store = MEMORY",
    "PRAGMA synchronous = NORMAL",
    "PRAGMA cache_size = -4000",
]

_CREATE_TABLE = """
CREATE TABLE IF NOT EXISTS rca_incidents (
    id                TEXT PRIMARY KEY,
    start_time        TEXT NOT NULL,
    end_time          TEXT NOT NULL,
    duration_seconds  INTEGER,
    root_cause        TEXT,
    confidence        TEXT,
    packet_loss_max   REAL,
    packet_loss_avg   REAL,
    latency_max       REAL,
    jitter_max        REAL,
    sample_count      INTEGER,
    evidence          TEXT,
    missing_evidence  TEXT,
    signals           TEXT,
    written_at        TEXT
)
"""


def _today() -> str:
    return datetime.now(tz=timezone.utc).strftime("%Y-%m-%d")


def _open_rca_db(path: Path) -> sqlite3.Connection:
    path.parent.mkdir(parents=True, exist_ok=True)
    con = sqlite3.connect(str(path), check_same_thread=False)
    for p in _PRAGMAS:
        con.execute(p)
    con.execute(_CREATE_TABLE)
    con.commit()
    return con


class StorageWriter:
    """Batched writer for signals (JSONL) and incidents (SQLite + JSON)."""

    def __init__(
        self,
        data_dir: Optional[Path] = None,
        rca_db_path: Optional[Path] = None,
    ) -> None:
        self._data_dir = data_dir or _DATA_DIR
        self._signals_dir = self._data_dir / "signals"
        self._reports_dir = self._data_dir / "reports"
        self._rca_db_path = rca_db_path or (self._data_dir / "rca.db")
        self._con: Optional[sqlite3.Connection] = None

    def open(self) -> "StorageWriter":
        self._signals_dir.mkdir(parents=True, exist_ok=True)
        self._reports_dir.mkdir(parents=True, exist_ok=True)
        self._con = _open_rca_db(self._rca_db_path)
        return self

    def close(self) -> None:
        if self._con is not None:
            self._con.close()
            self._con = None

    def __enter__(self) -> "StorageWriter":
        return self.open()

    def __exit__(self, *_) -> None:
        self.close()

    def _conn(self) -> sqlite3.Connection:
        if self._con is None:
            raise RuntimeError("StorageWriter not opened")
        return self._con

    # ------------------------------------------------------------------
    # Signal JSONL writes (batched)
    # ------------------------------------------------------------------

    def write_signal(self, signal: ContextSignal, date: Optional[str] = None) -> int:
        """
        Append all records from *signal* to its daily JSONL file.

        Returns number of records written.
        SD-card safe: one file open/close per call, batched writes.
        """
        if not signal.records:
            return 0

        today = date or _today()
        # Sanitize signal id for use as filename
        safe_id = signal.id.replace("/", "_").replace(".", "_")
        path = self._signals_dir / f"{safe_id}_{today}.jsonl"

        lines = []
        for rec in signal.records:
            obj = {
                "signal_id": signal.id,
                "timestamp": rec.timestamp,
                "value": rec.value,
                "quality": rec.quality,
                "evidence": rec.evidence,
            }
            lines.append(json.dumps(obj, default=str))

        with path.open("a", encoding="utf-8") as f:
            f.write("\n".join(lines) + "\n")

        return len(lines)

    # ------------------------------------------------------------------
    # Incident SQLite writes (batch INSERT OR REPLACE)
    # ------------------------------------------------------------------

    def write_incidents(self, incidents: List[Incident]) -> int:
        """
        Upsert incidents into rca_incidents table.

        Uses INSERT OR REPLACE — safe for repeated analyze runs.
        Returns number of rows written.
        """
        if not incidents:
            return 0

        now_iso = datetime.now(tz=timezone.utc).isoformat()
        rows = []
        for inc in incidents:
            rows.append((
                inc.id,
                inc.start_time,
                inc.end_time,
                inc.duration_seconds,
                inc.root_cause,
                inc.confidence,
                inc.metrics.get("packet_loss_max"),
                inc.metrics.get("packet_loss_avg"),
                inc.metrics.get("latency_max"),
                inc.metrics.get("jitter_max"),
                inc.metrics.get("sample_count"),
                json.dumps(inc.evidence),
                json.dumps(inc.missing_evidence),
                json.dumps(inc.signals),
                now_iso,
            ))

        self._conn().executemany(
            """INSERT OR REPLACE INTO rca_incidents
               (id, start_time, end_time, duration_seconds, root_cause,
                confidence, packet_loss_max, packet_loss_avg, latency_max,
                jitter_max, sample_count, evidence, missing_evidence, signals,
                written_at)
               VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)""",
            rows,
        )
        self._conn().commit()
        return len(rows)

    # ------------------------------------------------------------------
    # JSON export (atomic write via temp file + rename)
    # ------------------------------------------------------------------

    def export_incidents_json(
        self,
        incidents: List[Incident],
        date: Optional[str] = None,
    ) -> Path:
        """
        Write incidents to a dated JSON file atomically.

        Returns the path of the written file.
        SD-card safe: writes to a temp file then renames (single flash write).
        """
        today = date or _today()
        out_path = self._reports_dir / f"incidents_{today}.json"

        payload = []
        for inc in incidents:
            payload.append({
                "id": inc.id,
                "start_time": inc.start_time,
                "end_time": inc.end_time,
                "duration_seconds": inc.duration_seconds,
                "root_cause": inc.root_cause,
                "confidence": inc.confidence,
                "metrics": inc.metrics,
                "evidence": inc.evidence,
                "missing_evidence": inc.missing_evidence,
                "signals": inc.signals,
            })

        # Write to temp file in same directory, then rename (atomic on POSIX)
        tmp_fd, tmp_path = tempfile.mkstemp(
            dir=str(self._reports_dir), suffix=".json.tmp"
        )
        try:
            with os.fdopen(tmp_fd, "w", encoding="utf-8") as f:
                json.dump(payload, f, indent=2, default=str)
            os.replace(tmp_path, out_path)
        except Exception:
            try:
                os.unlink(tmp_path)
            except OSError:
                pass
            raise

        return out_path

    # ------------------------------------------------------------------
    # Read back incidents from SQLite (for CLI commands)
    # ------------------------------------------------------------------

    def list_incidents(self, limit: int = 50) -> List[dict]:
        """Return recent incidents from rca_incidents as plain dicts."""
        rows = self._conn().execute(
            """SELECT id, start_time, end_time, duration_seconds,
                      root_cause, confidence, packet_loss_max, latency_max,
                      sample_count, written_at
               FROM rca_incidents
               ORDER BY start_time DESC
               LIMIT ?""",
            (limit,),
        ).fetchall()
        cols = [
            "id", "start_time", "end_time", "duration_seconds",
            "root_cause", "confidence", "packet_loss_max", "latency_max",
            "sample_count", "written_at",
        ]
        return [dict(zip(cols, row)) for row in rows]
