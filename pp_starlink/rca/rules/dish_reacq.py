"""
DISH_REACQUISITION — fires when the dish is in SKY_SEARCH / re-acquisition
mode during the incident with high packet loss (>80%).

Logic:
  - dish.state has sky_search=True records in the incident window
  - incident metrics show packet_loss_max > 0.80

Priority: 15 (between LAN and WAN — dish reacq is very specific)
"""

from __future__ import annotations

from typing import Any, Dict, Optional

from pp_starlink.core.models import ContextSignal, Incident
from pp_starlink.rca.rules.base import RCARule

ROOT_CAUSE = "DISH_REACQUISITION"
_SKY_SEARCH_LOSS_THRESHOLD = 0.80


class DishReacquisitionRule(RCARule):
    priority = 15

    def evaluate(
        self,
        incident: Incident,
        signals: Dict[str, ContextSignal],
    ) -> Optional[Dict[str, Any]]:
        dish_state = signals.get("dish.state")

        if dish_state is None:
            return None

        state_records = dish_state.records_in_window(incident.start_time, incident.end_time)
        if not state_records:
            return None

        sky_search_records = [r for r in state_records if r.value.get("sky_search")]
        if not sky_search_records:
            return None

        # High loss is required
        loss_max = incident.metrics.get("packet_loss_max", 0.0)
        if loss_max < _SKY_SEARCH_LOSS_THRESHOLD:
            return None

        evidence = [
            f"dish.state=SKY_SEARCH in {len(sky_search_records)}/{len(state_records)} samples",
            f"packet_loss_max={loss_max*100:.0f}%",
        ]

        # Include outage cause strings as additional evidence
        causes = {
            r.value.get("cause")
            for r in sky_search_records
            if r.value.get("cause")
        }
        for c in sorted(causes):
            evidence.append(f"outage_cause={c}")

        return {
            "root_cause": ROOT_CAUSE,
            "confidence": "HIGH",
            "evidence": evidence,
            "missing_evidence": [],
        }
