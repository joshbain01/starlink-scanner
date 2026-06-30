"""
WEATHER_CORRELATED — fires ONLY if hourly weather data overlaps the incident.

CRITICAL GUARDRAIL (from spec):
  This rule MUST check signal.resolution == "hour" before firing with HIGH
  confidence.  Daily weather data alone → LOW confidence.
  No weather data at all → this rule does NOT fire.

This prevents spurious weather blame when only daily summaries exist or the
weather.hourly stub is empty (as it is in the default installation).

Priority: 60 (late — after all structural causes are ruled out)
"""

from __future__ import annotations

from typing import Any, Dict, Optional

from pp_starlink.core.models import ContextSignal, Incident
from pp_starlink.rca.rules.base import RCARule

ROOT_CAUSE = "WEATHER_CORRELATED"

_PRECIP_THRESHOLD = 2.0   # mm/h — meaningful precipitation
_WIND_THRESHOLD_MS = 15.0  # m/s — strong wind


class WeatherRule(RCARule):
    priority = 60

    def evaluate(
        self,
        incident: Incident,
        signals: Dict[str, ContextSignal],
    ) -> Optional[Dict[str, Any]]:
        hourly = signals.get("weather.hourly")
        daily = signals.get("weather.daily")

        # GUARDRAIL: no hourly signal → do not fire at all (avoid false HIGH)
        # We allow LOW confidence from daily data only as a weak signal.
        if hourly is None and daily is None:
            return None

        evidence = []
        missing = []
        confidence = "LOW"

        # --- Hourly data path (can achieve HIGH confidence) ---
        if hourly is not None and hourly.resolution == "hour":
            hourly_records = hourly.records_in_window(incident.start_time, incident.end_time)
            if not hourly_records:
                # Hourly signal registered but no data in window
                missing.append("weather.hourly: no records overlap incident window")
            else:
                # Check precipitation and wind
                high_precip = [
                    r for r in hourly_records
                    if (r.value.get("precipitation") or 0.0) > _PRECIP_THRESHOLD
                ]
                high_wind = [
                    r for r in hourly_records
                    if (r.value.get("wind_speed_ms") or 0.0) > _WIND_THRESHOLD_MS
                ]
                if not high_precip and not high_wind:
                    # Hourly data present but no adverse conditions → rule doesn't apply
                    return None
                if high_precip:
                    max_p = max(r.value["precipitation"] for r in high_precip)
                    evidence.append(f"precipitation {max_p:.1f}mm/h during incident")
                    confidence = "HIGH"
                if high_wind:
                    max_w = max(r.value["wind_speed_ms"] for r in high_wind)
                    evidence.append(f"wind {max_w:.1f}m/s during incident")
                    confidence = "HIGH"
        else:
            # Hourly signal absent or wrong resolution
            missing.append(
                "weather.hourly signal absent or not hour-resolution — "
                "HIGH confidence not achievable; WEATHER_CORRELATED cannot fire HIGH"
            )

        # --- Daily data path (LOW confidence only) ---
        if daily is not None and not daily.is_empty():
            daily_records = daily.records_in_window(incident.start_time, incident.end_time)
            if daily_records:
                for r in daily_records[:1]:
                    condition = r.value.get("condition", "unknown")
                    evidence.append(f"daily weather condition: {condition} (LOW confidence — not hourly)")
                # Do NOT upgrade confidence for daily data alone

        if not evidence:
            return None

        return {
            "root_cause": ROOT_CAUSE,
            "confidence": confidence,
            "evidence": evidence,
            "missing_evidence": missing,
        }
