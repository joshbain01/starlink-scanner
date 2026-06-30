"""
DISH_HEALTH_OR_TERMINAL — fires when terminal-side alerts are active during an
incident (thermal, water, roaming, bandwidth restriction, link-related alerts).

Priority: 17 (after reboot/reacquisition, before RF/WAN generic rules)
"""

from __future__ import annotations

from typing import Any, Dict, Optional

from pp_starlink.core.models import ContextSignal, Incident
from pp_starlink.rca.rules.base import RCARule

ROOT_CAUSE = "DISH_HEALTH_OR_TERMINAL"


class DishHealthRule(RCARule):
    priority = 17

    def evaluate(
        self,
        incident: Incident,
        signals: Dict[str, ContextSignal],
    ) -> Optional[Dict[str, Any]]:
        health = signals.get("dish.health")
        if health is None:
            return None

        records = health.records_in_window(incident.start_time, incident.end_time)
        if not records:
            return None

        degraded = [r for r in records if r.value.get("terminal_degraded")]
        if not degraded:
            return None

        alert_counts: dict[str, int] = {}
        for r in degraded:
            for alert in r.value.get("active_alerts", []):
                alert_counts[alert] = alert_counts.get(alert, 0) + 1

        reasons = sorted({
            reason
            for r in degraded
            for reason in r.value.get("restricted_reasons", [])
        })

        evidence = [
            f"terminal alerts/degradation in {len(degraded)}/{len(records)} samples",
        ]
        if alert_counts:
            top = ", ".join(
                f"{name}={count}" for name, count in sorted(alert_counts.items(), key=lambda i: i[1], reverse=True)[:4]
            )
            evidence.append(f"top alerts: {top}")
        if reasons:
            evidence.append(f"bandwidth restrictions observed: {', '.join(reasons)}")

        confidence = "MEDIUM"
        high_impact_alerts = {"thermal_throttle", "dish_water", "router_water", "heating"}
        if any(a in high_impact_alerts for a in alert_counts):
            confidence = "HIGH"
        elif len(degraded) >= 2:
            confidence = "HIGH"

        return {
            "root_cause": ROOT_CAUSE,
            "confidence": confidence,
            "evidence": evidence,
            "missing_evidence": [],
        }
