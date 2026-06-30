"""
STARLINK_WAN_OR_POP — fires if local path is clean, Starlink-level latency is
elevated, but no dish state alert is present.

Logic:
  - network.local_path clean (no degraded records in window)
  - network.public_path degraded (high latency or loss)
  - dish.state does NOT show SKY_SEARCH, BOOTING, or OBSTRUCTED
  - No REBOOT event in dish.reboot

Priority: 20
"""

from __future__ import annotations

from typing import Any, Dict, Optional

from pp_starlink.core.models import ContextSignal, Incident
from pp_starlink.rca.rules.base import RCARule

ROOT_CAUSE = "STARLINK_WAN_OR_POP"

_DISH_ALERT_STATES = {"SEARCHING", "BOOTING", "OBSTRUCTED", "DEGRADED"}

_CONF_ORDER = ("LOW", "MEDIUM", "HIGH")


def _downgrade_confidence(level: str, steps: int = 1) -> str:
    """Demote confidence by N steps while staying in LOW..HIGH."""
    idx = _CONF_ORDER.index(level)
    return _CONF_ORDER[max(0, idx - max(0, steps))]


class StarlinkWANRule(RCARule):
    priority = 20

    def evaluate(
        self,
        incident: Incident,
        signals: Dict[str, ContextSignal],
    ) -> Optional[Dict[str, Any]]:
        local = signals.get("network.local_path")
        public = signals.get("network.public_path")
        dish_state = signals.get("dish.state")
        dish_reboot = signals.get("dish.reboot")

        if public is None:
            return None

        public_records = public.records_in_window(incident.start_time, incident.end_time)
        if not public_records:
            return None

        # Public path must be degraded
        pub_degraded = sum(1 for r in public_records if r.value.get("degraded"))
        if pub_degraded == 0:
            return None

        # Local path must be clean (or absent — give benefit of doubt)
        if local is not None:
            local_records = local.records_in_window(incident.start_time, incident.end_time)
            local_degraded = sum(1 for r in local_records if r.value.get("degraded"))
            if local_degraded > len(local_records) * 0.3:
                return None  # local path also degraded → LocalLANRule territory

        # Dish state must not show alerting mode
        if dish_state is not None:
            state_records = dish_state.records_in_window(incident.start_time, incident.end_time)
            alert_states = [
                r for r in state_records if r.value.get("state") in _DISH_ALERT_STATES
            ]
            if alert_states:
                return None  # dish-level issue → different rule

        # No reboot during incident
        if dish_reboot is not None:
            reboot_records = dish_reboot.records_in_window(incident.start_time, incident.end_time)
            if any(r.value.get("reboot") for r in reboot_records):
                return None

        evidence = [
            f"public path degraded in {pub_degraded}/{len(public_records)} samples",
        ]

        # Add latency evidence
        high_lat = [
            r for r in public_records
            if r.value.get("pop_latency_ms") and r.value["pop_latency_ms"] > 200
        ]
        if high_lat:
            max_lat = max(r.value["pop_latency_ms"] for r in high_lat)
            evidence.append(f"peak POP latency={max_lat:.0f}ms")

        evidence.append("local path clean, no dish state alert")

        missing: list[str] = []
        if local is None:
            missing.append("network.local_path (could not confirm LAN is clean)")
        if dish_state is None:
            missing.append("dish.state (could not rule out dish-level cause)")

        # Calibrate confidence by coverage quality, not only signal presence.
        confidence = "HIGH" if dish_state is not None and local is not None else "MEDIUM"

        sample_count = len(public_records)
        degraded_ratio = pub_degraded / sample_count if sample_count else 0.0
        if sample_count < 3:
            confidence = _downgrade_confidence(confidence)
        if degraded_ratio < 0.75:
            confidence = _downgrade_confidence(confidence)

        # Very short windows with modest loss are suggestive, not decisive.
        loss_max = float(incident.metrics.get("packet_loss_max") or 0.0)
        if incident.duration_seconds < 10 and loss_max < 0.5:
            confidence = _downgrade_confidence(confidence)

        if confidence != "HIGH":
            evidence.append(
                f"confidence reduced by sparse/short evidence (samples={sample_count}, "
                f"degraded_ratio={degraded_ratio:.2f})"
            )

        return {
            "root_cause": ROOT_CAUSE,
            "confidence": confidence,
            "evidence": evidence,
            "missing_evidence": missing,
        }
