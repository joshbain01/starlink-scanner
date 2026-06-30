"""
weather.hourly signal — stub module for hourly weather data.

This module is intentionally a stub: the Go daemon does not collect weather
data, so records are always quality="missing".  The WEATHER_CORRELATED RCA
rule checks resolution == "hour" and quality before firing with HIGH confidence.

To integrate real weather data:
  1. Override collect() to fetch from an API (e.g., Open-Meteo)
  2. Override parse() + normalize() to produce observed records

SignalRecord.value keys (when data is present):
  temp_c:        float
  precipitation: float (mm/h)
  wind_speed_ms: float
  condition:     str description
"""

from __future__ import annotations

from typing import Any, List

from pp_starlink.core.models import ContextSignal, SignalRecord
from pp_starlink.signals.base import SignalModule


class WeatherHourlyModule(SignalModule):
    id = "weather.hourly"
    name = "Hourly Weather (stub)"

    def collect(self) -> Any:
        # No weather source integrated — return empty list.
        return []

    def parse(self, raw: Any) -> List[dict]:
        return []

    def normalize(self, parsed: List[dict]) -> ContextSignal:
        # Always produces an empty ContextSignal with resolution="hour".
        # The WEATHER_CORRELATED rule checks is_empty() before firing HIGH.
        return ContextSignal(
            id=self.id,
            name=self.name,
            source="external/weather",
            resolution="hour",
            records=[],
        )
