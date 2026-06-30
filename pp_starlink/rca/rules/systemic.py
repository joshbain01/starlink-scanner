"""
SYSTEMIC_STARLINK — fires if multiple incidents appear to be clustered in a
short time window (suggesting a network-wide or regional Starlink issue rather
than a local problem).

This rule receives the incident under analysis and all signals, but it also
needs access to *sibling incidents* to detect clustering.  The RCAEngine
passes the full incident list via signals["__incidents__"] (a synthetic
ContextSignal with incidents serialized in its records).

If fewer than 2 sibling incidents are found in the same 30-minute window →
rule does not fire.

Priority: 55 (before UNKNOWN but after specific causes)
"""

from __future__ import annotations

from datetime import datetime, timedelta
from typing import Any, Dict, List, Optional

from pp_starlink.core.models import ContextSignal, Incident
from pp_starlink.rca.rules.base import RCARule

ROOT_CAUSE = "SYSTEMIC_STARLINK"
_CLUSTER_WINDOW_S = 1800  # 30 minutes
_MIN_CLUSTER_INCIDENTS = 2  # incidents besides current one in window


class SystemicStarlinkRule(RCARule):
    priority = 55

    def evaluate(
        self,
        incident: Incident,
        signals: Dict[str, ContextSignal],
    ) -> Optional[Dict[str, Any]]:
        # Engine injects all incidents as a synthetic signal
        incidents_signal = signals.get("__incidents__")
        if incidents_signal is None:
            return None

        all_incidents: List[Incident] = incidents_signal.records[0].value.get("incidents", [])  # type: ignore[index]
        if not all_incidents:
            return None

        start_dt = datetime.fromisoformat(incident.start_time)
        window_start = start_dt - timedelta(seconds=_CLUSTER_WINDOW_S)
        window_end = start_dt + timedelta(seconds=_CLUSTER_WINDOW_S)

        cluster = [
            inc for inc in all_incidents
            if inc.id != incident.id
            and window_start <= datetime.fromisoformat(inc.start_time) <= window_end
        ]

        if len(cluster) < _MIN_CLUSTER_INCIDENTS:
            return None

        evidence = [
            f"{len(cluster)} other incidents within ±30 min window",
        ]
        for inc in cluster[:3]:
            loss = inc.metrics.get("packet_loss_max", 0)
            evidence.append(
                f"  incident {inc.id} at {inc.start_time} loss={loss*100:.0f}%"
            )

        return {
            "root_cause": ROOT_CAUSE,
            "confidence": "MEDIUM",
            "evidence": evidence,
            "missing_evidence": [
                "Starlink service status API (not available — would confirm regional outage)"
            ],
        }
