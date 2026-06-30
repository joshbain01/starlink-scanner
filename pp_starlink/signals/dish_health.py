"""
dish.health signal — terminal-side health and alert surface from network telemetry.

Sources (network_telemetry):
  alert_is_heating
  alert_power_supply_thermal_throttle
  alert_dish_water_detected
  alert_router_water_detected
  alert_no_ethernet_link
  alert_roaming
  dl_bandwidth_restricted_reason
  ul_bandwidth_restricted_reason
  eth_speed_mbps

SignalRecord.value keys:
  active_alerts: list[str]
  alert_count: int
  link_limited: bool
  roaming: bool
  restricted_reasons: list[str]
  eth_speed_mbps: float | None
  terminal_degraded: bool
"""

from __future__ import annotations

import os
from datetime import datetime, timezone
from typing import List, Optional

from pp_starlink.core.models import ContextSignal, SignalRecord
from pp_starlink.signals.base import SignalModule
from pp_starlink.telemetry.reader import TelemetryReader

_ALERT_KEYS = {
    "alert_is_heating": "heating",
    "alert_power_supply_thermal_throttle": "thermal_throttle",
    "alert_dish_water_detected": "dish_water",
    "alert_router_water_detected": "router_water",
    "alert_no_ethernet_link": "no_ethernet",
    "alert_roaming": "roaming",
}


def _ts_to_iso(unix_s: int) -> str:
    return datetime.fromtimestamp(unix_s, tz=timezone.utc).isoformat()


class DishHealthModule(SignalModule):
    id = "dish.health"
    name = "Dish Health / Terminal Alerts"

    def __init__(self, db_path: Optional[str] = None) -> None:
        self._db_path = db_path or os.getenv("DB_PATH", "/data/starlink_telemetry.db")

    def collect(self) -> List[dict]:
        with TelemetryReader(self._db_path) as r:
            return r.read_network()

    def parse(self, raw: List[dict]) -> List[dict]:
        parsed: List[dict] = []
        for row in raw:
            ts = row.get("timestamp")
            if ts is None:
                continue

            active_alerts = [
                label
                for col, label in _ALERT_KEYS.items()
                if bool(row.get(col))
            ]

            restricted_reasons = []
            dl_reason = row.get("dl_bandwidth_restricted_reason")
            ul_reason = row.get("ul_bandwidth_restricted_reason")
            if dl_reason:
                restricted_reasons.append(f"dl:{dl_reason}")
            if ul_reason:
                restricted_reasons.append(f"ul:{ul_reason}")

            link_limited = bool(restricted_reasons)
            roaming = "roaming" in active_alerts
            eth_speed = row.get("eth_speed_mbps")

            parsed.append(
                {
                    "timestamp": ts,
                    "active_alerts": active_alerts,
                    "alert_count": len(active_alerts),
                    "link_limited": link_limited,
                    "roaming": roaming,
                    "restricted_reasons": restricted_reasons,
                    "eth_speed_mbps": eth_speed,
                    "terminal_degraded": bool(active_alerts or restricted_reasons),
                }
            )
        return parsed

    def normalize(self, parsed: List[dict]) -> ContextSignal:
        records: List[SignalRecord] = []
        for p in parsed:
            evidence = None
            if p["terminal_degraded"]:
                parts: list[str] = []
                if p["active_alerts"]:
                    parts.append(f"alerts={','.join(p['active_alerts'])}")
                if p["restricted_reasons"]:
                    parts.append(f"restricted={','.join(p['restricted_reasons'])}")
                if p["eth_speed_mbps"] is not None:
                    parts.append(f"eth_speed={p['eth_speed_mbps']}")
                evidence = parts or None

            records.append(
                SignalRecord(
                    timestamp=_ts_to_iso(int(p["timestamp"])),
                    value={
                        "active_alerts": p["active_alerts"],
                        "alert_count": p["alert_count"],
                        "link_limited": p["link_limited"],
                        "roaming": p["roaming"],
                        "restricted_reasons": p["restricted_reasons"],
                        "eth_speed_mbps": p["eth_speed_mbps"],
                        "terminal_degraded": p["terminal_degraded"],
                    },
                    quality="observed",
                    evidence=evidence,
                )
            )

        return ContextSignal(
            id=self.id,
            name=self.name,
            source="network_telemetry",
            resolution="sample",
            records=records,
        )
