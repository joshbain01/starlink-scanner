"""
OBSTRUCTION — fires if the obstruction signal shows physical sky blockage
during the incident window.

GUARDRAIL (from spec): this rule MUST check that azimuth/elevation pointing
data is present before firing.  Without pointing data the evidence is weaker
(fraction-only) → MEDIUM confidence.

Priority: 25
"""

from __future__ import annotations

from typing import Any, Dict, Optional

from pp_starlink.core.models import ContextSignal, Incident
from pp_starlink.rca.rules.base import RCARule

ROOT_CAUSE = "OBSTRUCTION"


class ObstructionRule(RCARule):
    priority = 25

    def evaluate(
        self,
        incident: Incident,
        signals: Dict[str, ContextSignal],
    ) -> Optional[Dict[str, Any]]:
        obs = signals.get("obstruction")

        if obs is None:
            return None

        obs_records = obs.records_in_window(incident.start_time, incident.end_time)
        if not obs_records:
            return None

        obstructed_records = [r for r in obs_records if r.value.get("obstructed")]
        if not obstructed_records:
            return None

        # GUARDRAIL: check for pointing data
        has_pointing = any(r.value.get("has_pointing_data") for r in obstructed_records)

        evidence = [
            f"obstruction confirmed in {len(obstructed_records)}/{len(obs_records)} samples",
        ]
        missing: list = []

        if has_pointing:
            # Include pointing data in evidence
            pointing_records = [r for r in obstructed_records if r.value.get("has_pointing_data")]
            boresight_els = [
                r.value["boresight_elevation_deg"]
                for r in pointing_records
                if r.value.get("boresight_elevation_deg") is not None
            ]
            if boresight_els:
                evidence.append(f"dish boresight elevation range: {min(boresight_els):.1f}°–{max(boresight_els):.1f}°")

            sat_els = [
                r.value["satellite_elevation"]
                for r in pointing_records
                if r.value.get("satellite_elevation") is not None
            ]
            if sat_els:
                evidence.append(f"satellite elevation range: {min(sat_els):.1f}°–{max(sat_els):.1f}°")

            confidence = "HIGH"
        else:
            # No az/el data — weaker evidence
            missing.append("boresight_azimuth_deg / boresight_elevation_deg (pointing data absent)")
            missing.append("calculated_azimuth / calculated_elevation (orbital data absent)")
            # Still fire, but at lower confidence

            # Add fraction evidence
            fractions = [
                r.value.get("obstruction_fraction", 0.0)
                for r in obstructed_records
                if r.value.get("obstruction_fraction") is not None
            ]
            if fractions:
                evidence.append(f"obstruction_fraction max={max(fractions):.3f}")

            confidence = "MEDIUM"

        flag_count = sum(1 for r in obstructed_records if r.value.get("currently_obstructed"))
        if flag_count > 0:
            evidence.append(f"currently_obstructed=true in {flag_count} samples")

        return {
            "root_cause": ROOT_CAUSE,
            "confidence": confidence,
            "evidence": evidence,
            "missing_evidence": missing,
        }
