"""
REBOOT_OR_POWER — fires if a dish reboot/uptime-reset occurred during or
immediately before (within 5 minutes) the incident.

Priority: 12 (after LAN but before most other WAN rules)
"""

from __future__ import annotations

from datetime import datetime, timedelta, timezone
from typing import Any, Dict, Optional

from pp_starlink.core.models import ContextSignal, Incident
from pp_starlink.rca.rules.base import RCARule

ROOT_CAUSE = "REBOOT_OR_POWER"
_PRE_INCIDENT_WINDOW_S = 300  # 5 minutes before incident start


def _iso_to_dt(iso: str) -> datetime:
    return datetime.fromisoformat(iso)


def _dt_to_iso(dt: datetime) -> str:
    return dt.isoformat()


class RebootRule(RCARule):
    priority = 12

    def evaluate(
        self,
        incident: Incident,
        signals: Dict[str, ContextSignal],
    ) -> Optional[Dict[str, Any]]:
        dish_reboot = signals.get("dish.reboot")

        if dish_reboot is None:
            return None

        start_dt = _iso_to_dt(incident.start_time)
        look_back = _dt_to_iso(start_dt - timedelta(seconds=_PRE_INCIDENT_WINDOW_S))

        # Window: 5 min before incident through incident end
        reboot_records = dish_reboot.records_in_window(look_back, incident.end_time)
        reboot_events = [r for r in reboot_records if r.value.get("reboot")]

        if not reboot_events:
            return None

        evidence = []
        for r in reboot_events:
            prev = r.value.get("prev_uptime")
            curr = r.value.get("uptime_s")
            ts = r.timestamp
            if prev is not None and curr is not None:
                evidence.append(f"uptime reset at {ts}: {prev}s → {curr}s")
            else:
                evidence.append(f"uptime reset at {ts}")

        before_count = sum(
            1 for r in reboot_events if r.timestamp < incident.start_time
        )
        during_count = len(reboot_events) - before_count
        if before_count > 0:
            evidence.append(f"{before_count} reboot(s) in 5 min window before incident")
        if during_count > 0:
            evidence.append(f"{during_count} reboot(s) during incident window")

        return {
            "root_cause": ROOT_CAUSE,
            "confidence": "HIGH",
            "evidence": evidence,
            "missing_evidence": [],
        }
