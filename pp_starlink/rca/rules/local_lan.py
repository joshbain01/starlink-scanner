"""
LOCAL_LAN_OR_WIFI — fires if local gateway path is degraded but public path is fine.

Logic:
  - network.local_path has degraded=True records in the incident window
  - network.public_path does NOT have degraded=True records in the same window
    (or signal is absent — degradation is isolated to LAN side)

Priority: 10 (highest — eliminate local causes first)
"""

from __future__ import annotations

from typing import Any, Dict, Optional

from pp_starlink.core.models import ContextSignal, Incident
from pp_starlink.rca.rules.base import RCARule

ROOT_CAUSE = "LOCAL_LAN_OR_WIFI"


class LocalLANRule(RCARule):
    priority = 10

    def evaluate(
        self,
        incident: Incident,
        signals: Dict[str, ContextSignal],
    ) -> Optional[Dict[str, Any]]:
        local = signals.get("network.local_path")
        public = signals.get("network.public_path")

        if local is None:
            return None

        local_records = local.records_in_window(incident.start_time, incident.end_time)
        if not local_records:
            return None

        # Check if local path is degraded
        local_degraded_count = sum(
            1 for r in local_records if r.value.get("degraded") or r.value.get("no_ethernet_link")
        )
        if local_degraded_count == 0:
            return None

        # Check that public path is relatively clean (or absent)
        public_records = []
        if public is not None:
            public_records = public.records_in_window(incident.start_time, incident.end_time)
        public_degraded_count = sum(
            1 for r in public_records if r.value.get("degraded")
        )

        # Local degraded AND public not degraded (or no public signal) → LAN issue
        if public_degraded_count > 0 and public_degraded_count >= local_degraded_count:
            # Both are degraded roughly equally — not isolated to LAN
            return None

        evidence = [
            f"local path degraded in {local_degraded_count}/{len(local_records)} samples",
        ]
        if public_degraded_count == 0:
            evidence.append("public path clean during incident window")
        elif public_degraded_count < local_degraded_count:
            evidence.append(
                f"public path only degraded {public_degraded_count}/{len(public_records)} samples vs local {local_degraded_count}"
            )

        no_eth = any(r.value.get("no_ethernet_link") for r in local_records)
        if no_eth:
            evidence.append("alert_no_ethernet_link=true")

        missing: list = []
        if public is None:
            missing.append("network.public_path (could not confirm public side is clean)")

        return {
            "root_cause": ROOT_CAUSE,
            "confidence": "HIGH",
            "evidence": evidence,
            "missing_evidence": missing,
        }
