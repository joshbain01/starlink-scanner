"""
weather.daily signal — stub module for daily weather summaries.

Intentionally a stub for the same reason as weather.hourly.  Resolution is
"day" — the WEATHER_CORRELATED rule will NOT fire HIGH based on daily data
alone (per the guardrail: must have hourly data for HIGH confidence).

SignalRecord.value keys (when data is present):
  date:          str YYYY-MM-DD
  max_temp_c:    float
  precip_mm:     float
  condition:     str description
"""

from __future__ import annotations

from typing import Any, List

from pp_starlink.core.models import ContextSignal, SignalRecord
from pp_starlink.signals.base import SignalModule


class WeatherDailyModule(SignalModule):
    id = "weather.daily"
    name = "Daily Weather Summary (stub)"

    def collect(self) -> Any:
        return []

    def parse(self, raw: Any) -> List[dict]:
        return []

    def normalize(self, parsed: List[dict]) -> ContextSignal:
        return ContextSignal(
            id=self.id,
            name=self.name,
            source="external/weather",
            resolution="day",
            records=[],
        )
