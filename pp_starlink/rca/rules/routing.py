"""
ROUTING_OR_POP_CHANGE — fires if the network.routing signal shows a POP
fingerprint change during or shortly before the incident.

Priority: 30
"""

from __future__ import annotations

from datetime import datetime, timedelta
from typing import Any, Dict, Optional

from pp_starlink.core.models import ContextSignal, Incident
from pp_starlink.rca.rules.base import RCARule

ROOT_CAUSE = "ROUTING_OR_POP_CHANGE"
_PRE_INCIDENT_WINDOW_S = 120  # 2 minutes before incident


def _dt_to_iso(dt: datetime) -> str:
    return dt.isoformat()


class RoutingRule(RCARule):
    priority = 30

    def evaluate(
        self,
        incident: Incident,
        signals: Dict[str, ContextSignal],
    ) -> Optional[Dict[str, Any]]:
        routing = signals.get("network.routing")

        if routing is None:
            return None

        start_dt = datetime.fromisoformat(incident.start_time)
        look_back = _dt_to_iso(start_dt - timedelta(seconds=_PRE_INCIDENT_WINDOW_S))

        # Window: 2 min before incident through incident end
        routing_records = routing.records_in_window(look_back, incident.end_time)
        change_records = [r for r in routing_records if r.value.get("routing_change")]

        if not change_records:
            return None

        evidence = [
            f"routing/POP change detected in {len(change_records)} sample(s)",
        ]
        for r in change_records[:3]:  # cap to avoid bloated output
            fp = r.value.get("pop_fingerprint", "?")
            evidence.append(f"POP fingerprint changed to {fp} at {r.timestamp}")

        # Add latency context
        lats = [
            r.value.get("pop_latency_ms")
            for r in routing_records
            if r.value.get("pop_latency_ms") is not None
        ]
        if lats:
            evidence.append(f"POP latency range during window: {min(lats):.0f}–{max(lats):.0f}ms")

        return {
            "root_cause": ROOT_CAUSE,
            "confidence": "MEDIUM",  # fingerprint is derived, not direct IP evidence
            "evidence": evidence,
            "missing_evidence": [
                "public IP change log (not collected — would elevate confidence to HIGH)"
            ],
        }
