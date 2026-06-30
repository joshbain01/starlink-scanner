"""
SignalModule abstract base class.

Each concrete module must implement collect(), parse(), and normalize().
The optional rules() hook lets a module advertise the RCA rules it supports.
"""

from __future__ import annotations

from abc import ABC, abstractmethod
from typing import Any, List

from pp_starlink.core.models import ContextSignal


class SignalModule(ABC):
    """Abstract base for all signal collection modules."""

    id: str   # unique signal identifier, e.g. "dish.state"
    name: str  # human-readable label

    @abstractmethod
    def collect(self) -> Any:
        """Fetch raw data from the source (DB query, HTTP request, etc.)."""
        ...

    @abstractmethod
    def parse(self, raw: Any) -> Any:
        """Transform raw source data into an intermediate representation."""
        ...

    @abstractmethod
    def normalize(self, parsed: Any) -> ContextSignal:
        """Produce a ContextSignal from the parsed intermediate data."""
        ...

    def run(self) -> ContextSignal:
        """Convenience: collect → parse → normalize in one call."""
        raw = self.collect()
        parsed = self.parse(raw)
        return self.normalize(parsed)

    def rules(self) -> List:
        """Optional: return rule classes that this module makes possible."""
        return []
