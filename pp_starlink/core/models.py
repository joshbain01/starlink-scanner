"""
Core domain models for pp_starlink RCA.

All cross-module data structures live here to prevent circular imports.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Dict, List, Literal, Optional


@dataclass
class SignalRecord:
    """One time-stamped sample from a signal module."""

    timestamp: str  # ISO-8601, UTC
    value: Dict[str, Any]
    quality: Literal["observed", "derived", "missing"]
    evidence: Optional[List[str]] = None


@dataclass
class ContextSignal:
    """Named, sourced collection of SignalRecords for one logical signal."""

    id: str
    name: str
    source: str
    resolution: str  # sample | second | minute | hour | event
    records: List[SignalRecord] = field(default_factory=list)

    def contains(self, value_key: str, incident: "Incident") -> bool:
        """Return True if *value_key* appears in any record within the incident window."""
        for r in self.records:
            if r.timestamp >= incident.start_time and r.timestamp <= incident.end_time:
                if value_key in str(r.value):
                    return True
        return False

    def records_in_window(self, start: str, end: str) -> List[SignalRecord]:
        """Return records whose timestamps fall within [start, end] (inclusive)."""
        return [r for r in self.records if start <= r.timestamp <= end]

    def is_empty(self) -> bool:
        return len(self.records) == 0


@dataclass
class Incident:
    """A detected degradation window with optional RCA results attached."""

    id: str
    start_time: str   # ISO-8601, UTC
    end_time: str     # ISO-8601, UTC
    duration_seconds: int
    metrics: Dict[str, Any]
    signals: List[str] = field(default_factory=list)
    root_cause: Optional[str] = None
    confidence: Optional[str] = None
    evidence: List[str] = field(default_factory=list)
    missing_evidence: List[str] = field(default_factory=list)
