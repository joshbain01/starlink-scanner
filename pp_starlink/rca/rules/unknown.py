"""
UNKNOWN — fallback rule that always fires and enumerates missing signals.

GUARDRAIL (from spec): MUST list all absent signals as missing_evidence.

Priority: 100 (lowest — only fires if no other rule matched)
"""

from __future__ import annotations

from typing import Any, Dict, Optional

from pp_starlink.core.models import ContextSignal, Incident
from pp_starlink.rca.rules.base import RCARule

ROOT_CAUSE = "UNKNOWN"

# All signals that ideally should be present for a complete RCA
_EXPECTED_SIGNALS = [
    "dish.state",
    "dish.reboot",
    "network.local_path",
    "network.public_path",
    "network.routing",
    "weather.hourly",
    "obstruction",
    "load",
]


class UnknownRule(RCARule):
    priority = 100  # always last

    def evaluate(
        self,
        incident: Incident,
        signals: Dict[str, ContextSignal],
    ) -> Optional[Dict[str, Any]]:
        # Enumerate which expected signals are absent or empty in this window
        missing = []
        present = []

        for sig_id in _EXPECTED_SIGNALS:
            sig = signals.get(sig_id)
            if sig is None:
                missing.append(f"{sig_id} (signal not registered)")
            elif sig.is_empty():
                missing.append(f"{sig_id} (signal registered but has no records)")
            else:
                window_records = sig.records_in_window(incident.start_time, incident.end_time)
                if not window_records:
                    missing.append(f"{sig_id} (no records in incident window)")
                else:
                    present.append(sig_id)

        evidence = [
            f"No specific rule matched — {len(present)} signal(s) examined, "
            f"{len(missing)} signal(s) absent or empty"
        ]
        if present:
            evidence.append(f"Signals present but inconclusive: {', '.join(present)}")

        # Summarize incident characteristics
        loss = incident.metrics.get("packet_loss_max", 0)
        lat = incident.metrics.get("latency_max")
        evidence.append(f"Incident: loss_max={loss*100:.1f}%, duration={incident.duration_seconds}s")
        if lat:
            evidence.append(f"latency_max={lat:.0f}ms")

        return {
            "root_cause": ROOT_CAUSE,
            "confidence": "LOW",
            "evidence": evidence,
            "missing_evidence": missing,
        }
