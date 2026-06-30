"""
SignalRegistry — central registry for all ContextSignal instances.

Modules register signals here; RCA rules look them up by id.
"""

from __future__ import annotations

from typing import Dict, Optional

from pp_starlink.core.models import ContextSignal


class SignalRegistry:
    """Holds all collected ContextSignals keyed by signal id."""

    def __init__(self) -> None:
        self._signals: Dict[str, ContextSignal] = {}

    def register(self, signal: ContextSignal) -> None:
        self._signals[signal.id] = signal

    def get(self, signal_id: str) -> Optional[ContextSignal]:
        return self._signals.get(signal_id)

    def all(self) -> Dict[str, ContextSignal]:
        return dict(self._signals)

    def ids(self) -> list:
        return list(self._signals.keys())
