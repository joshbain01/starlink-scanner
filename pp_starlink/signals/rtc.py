"""
rtc signal — real-time clock / time-sync quality (optional stub).

The Go daemon does not expose NTP/RTC quality directly.  This module is a
no-op stub that always returns an empty ContextSignal.  Future work: correlate
kernel clock_gettime() or NTP sync logs via syslog/journald if relevant.

SignalRecord.value keys (when data is present):
  ntp_sync:      bool
  offset_ms:     float
  stratum:       int
"""

from __future__ import annotations

from typing import Any, List

from pp_starlink.core.models import ContextSignal
from pp_starlink.signals.base import SignalModule


class RTCModule(SignalModule):
    id = "rtc"
    name = "Real-Time Clock / NTP Sync (stub)"

    def collect(self) -> Any:
        return []

    def parse(self, raw: Any) -> List[dict]:
        return []

    def normalize(self, parsed: List[dict]) -> ContextSignal:
        return ContextSignal(
            id=self.id,
            name=self.name,
            source="system/rtc",
            resolution="event",
            records=[],
        )
