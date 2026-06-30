"""
RF_INTERFERENCE_OR_EMI — fires when RF telemetry indicates elevated noise and
SNR degradation during an incident while local path remains mostly clean.

Priority: 19 (before generic STARLINK_WAN_OR_POP classification)
"""

from __future__ import annotations

from typing import Any, Dict, Optional

from pp_starlink.core.models import ContextSignal, Incident
from pp_starlink.rca.rules.base import RCARule

ROOT_CAUSE = "RF_INTERFERENCE_OR_EMI"


class RFInterferenceRule(RCARule):
    priority = 19

    def evaluate(
        self,
        incident: Incident,
        signals: Dict[str, ContextSignal],
    ) -> Optional[Dict[str, Any]]:
        rf = signals.get("rf.environment")
        if rf is None:
            return None

        rf_records = rf.records_in_window(incident.start_time, incident.end_time)
        if not rf_records:
            return None

        flagged = [r for r in rf_records if r.value.get("rf_interference_suspected")]
        if not flagged:
            return None

        # Guardrail: if local path is degraded too, do not over-attribute to RF.
        local = signals.get("network.local_path")
        if local is not None:
            local_records = local.records_in_window(incident.start_time, incident.end_time)
            if local_records:
                local_degraded = sum(1 for r in local_records if r.value.get("degraded"))
                if local_degraded > len(local_records) * 0.4:
                    return None

        evidence = [
            f"RF interference signature in {len(flagged)}/{len(rf_records)} RF samples",
        ]

        max_noise_rise = max(
            float(r.value.get("noise_rise_from_median") or 0.0)
            for r in flagged
        )
        max_snr_drop = max(
            float(r.value.get("snr_drop_from_median") or 0.0)
            for r in flagged
        )
        min_margin = min(
            float(r.value.get("snr_margin_db") or 999.0)
            for r in flagged
        )

        evidence.append(f"noise rise max={max_noise_rise:.1f}dB")
        evidence.append(f"snr drop max={max_snr_drop:.1f}dB")
        if min_margin < 999.0:
            evidence.append(f"snr margin min={min_margin:.1f}dB")

        confidence = "HIGH" if len(flagged) >= 2 else "MEDIUM"

        return {
            "root_cause": ROOT_CAUSE,
            "confidence": confidence,
            "evidence": evidence,
            "missing_evidence": [],
        }
