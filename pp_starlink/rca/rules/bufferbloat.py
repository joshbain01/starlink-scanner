"""
BUFFERBLOAT_OR_LOAD — fires when jitter is high but packet loss is low,
OR there is significant latency/jitter asymmetry between local and WAN paths.

Signature: high jitter + low loss = buffer congestion (not connectivity failure).

Priority: 35
"""

from __future__ import annotations

from typing import Any, Dict, Optional

from pp_starlink.core.models import ContextSignal, Incident
from pp_starlink.rca.rules.base import RCARule

ROOT_CAUSE = "BUFFERBLOAT_OR_LOAD"
_JITTER_HIGH_MS = 20.0
_LOW_LOSS = 0.05          # < 5% loss → bufferbloat, not outage
_ASYMMETRY_THRESHOLD_MS = 15.0  # pop_jitter - local_jitter > this → WAN-side load


class BufferbloatRule(RCARule):
    priority = 35

    def evaluate(
        self,
        incident: Incident,
        signals: Dict[str, ContextSignal],
    ) -> Optional[Dict[str, Any]]:
        load = signals.get("load")

        if load is None:
            return None

        load_records = load.records_in_window(incident.start_time, incident.end_time)
        if not load_records:
            return None

        bloat_records = [r for r in load_records if r.value.get("bufferbloat_suspected")]
        asymmetry_records = [
            r for r in load_records
            if (r.value.get("jitter_asymmetry_ms") or 0.0) > _ASYMMETRY_THRESHOLD_MS
        ]

        if not bloat_records and not asymmetry_records:
            return None

        # Additional guard: loss must be low overall
        loss_max = incident.metrics.get("packet_loss_max", 1.0)
        if loss_max >= _LOW_LOSS and not asymmetry_records:
            return None

        evidence = []
        if bloat_records:
            jitters = [
                r.value.get("local_jitter_ms", 0.0)
                for r in bloat_records
                if r.value.get("local_jitter_ms") is not None
            ]
            if jitters:
                evidence.append(
                    f"high local jitter (max {max(jitters):.1f}ms) with loss<5% in {len(bloat_records)} samples"
                )

        if asymmetry_records:
            asym_vals = [
                r.value.get("jitter_asymmetry_ms", 0.0)
                for r in asymmetry_records
                if r.value.get("jitter_asymmetry_ms") is not None
            ]
            if asym_vals:
                evidence.append(
                    f"jitter asymmetry: WAN-side jitter {max(asym_vals):.1f}ms > local in {len(asymmetry_records)} samples"
                )

        evidence.append(f"packet_loss_max={loss_max*100:.1f}% (below outage threshold)")

        return {
            "root_cause": ROOT_CAUSE,
            "confidence": "MEDIUM",
            "evidence": evidence,
            "missing_evidence": [
                "active bandwidth utilization data (not collected)"
            ],
        }
