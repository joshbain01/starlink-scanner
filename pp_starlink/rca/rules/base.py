"""
RCARule abstract base class.

All rules receive a fully-populated Incident and a Dict of ContextSignals.
They must NOT read raw telemetry directly — only through the provided
ContextSignals.

Return contract:
  None         → rule does not apply; engine continues to next rule
  Dict[str, Any] → rule matched; keys used by engine:
      root_cause:        str   (required)
      confidence:        str   "HIGH" | "MEDIUM" | "LOW"
      evidence:          List[str]
      missing_evidence:  List[str]
"""

from __future__ import annotations

from abc import ABC, abstractmethod
from typing import Any, Dict, Optional

from pp_starlink.core.models import ContextSignal, Incident


class RCARule(ABC):
    """Abstract base for all RCA rules."""

    priority: int = 50  # lower integer = higher priority; engine sorts ascending

    @abstractmethod
    def evaluate(
        self,
        incident: Incident,
        signals: Dict[str, ContextSignal],
    ) -> Optional[Dict[str, Any]]:
        """
        Evaluate whether this rule explains the incident.

        Returns a dict with at minimum:
            {"root_cause": str, "confidence": str, "evidence": [...], "missing_evidence": [...]}
        or None if this rule does not apply.
        """
        ...
