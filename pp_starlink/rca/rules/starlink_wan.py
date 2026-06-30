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
_LOSS_THRESHOLD = 0.05
_POP_LAT_THRESHOLD_MS = 200.0
_POP_DROP_THRESHOLD = 0.05
_POP_JITTER_THRESHOLD_MS = 20.0

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

        local_records = []
        local_degraded = 0
        # Local path must be clean (or absent — give benefit of doubt)
        if local is not None:
            local_records = local.records_in_window(incident.start_time, incident.end_time)
            local_degraded = sum(1 for r in local_records if r.value.get("degraded"))
            if local_degraded > len(local_records) * 0.3:
                return None  # local path also degraded → LocalLANRule territory

        state_records = []
        # Dish state must not show alerting mode
        if dish_state is not None:
            state_records = dish_state.records_in_window(incident.start_time, incident.end_time)
            alert_states = [
                r for r in state_records if r.value.get("state") in _DISH_ALERT_STATES
            ]
            if alert_states:
                return None  # dish-level issue → different rule

        reboot_records = []
        # No reboot during incident
        if dish_reboot is not None:
            reboot_records = dish_reboot.records_in_window(incident.start_time, incident.end_time)
            if any(r.value.get("reboot") for r in reboot_records):
                return None

        evidence = [f"public path degraded in {pub_degraded}/{len(public_records)} samples"]

        max_public_loss = max(float(r.value.get("public_packet_loss") or 0.0) for r in public_records)
        max_public_lat = max(float(r.value.get("pop_latency_ms") or 0.0) for r in public_records)
        max_public_drop = max(float(r.value.get("pop_drop_rate") or 0.0) for r in public_records)
        max_public_jitter = max(float(r.value.get("pop_jitter_ms") or 0.0) for r in public_records)
        evidence.append(
            "public metric peaks: "
            f"loss={max_public_loss*100:.1f}%, "
            f"pop_latency={max_public_lat:.0f}ms, "
            f"pop_drop={max_public_drop*100:.1f}%, "
            f"pop_jitter={max_public_jitter:.1f}ms"
        )

        loss_events = [
            r for r in public_records
            if float(r.value.get("public_packet_loss") or 0.0) > _LOSS_THRESHOLD
        ]
        lat_events = [
            r for r in public_records
            if float(r.value.get("pop_latency_ms") or 0.0) > _POP_LAT_THRESHOLD_MS
        ]
        pop_drop_events = [
            r for r in public_records
            if float(r.value.get("pop_drop_rate") or 0.0) > _POP_DROP_THRESHOLD
        ]
        pop_jitter_events = [
            r for r in public_records
            if float(r.value.get("pop_jitter_ms") or 0.0) > _POP_JITTER_THRESHOLD_MS
        ]

        trigger_parts: list[str] = []
        if loss_events:
            max_loss = max(float(r.value.get("public_packet_loss") or 0.0) for r in loss_events)
            trigger_parts.append(f"loss>{_LOSS_THRESHOLD*100:.0f}% in {len(loss_events)}/{len(public_records)} (max={max_loss*100:.1f}%)")
        if lat_events:
            max_lat = max(float(r.value.get("pop_latency_ms") or 0.0) for r in lat_events)
            trigger_parts.append(f"POP latency>{_POP_LAT_THRESHOLD_MS:.0f}ms in {len(lat_events)}/{len(public_records)} (max={max_lat:.0f}ms)")
        if pop_drop_events:
            max_drop = max(float(r.value.get("pop_drop_rate") or 0.0) for r in pop_drop_events)
            trigger_parts.append(f"POP drop>{_POP_DROP_THRESHOLD*100:.0f}% in {len(pop_drop_events)}/{len(public_records)} (max={max_drop*100:.1f}%)")
        if pop_jitter_events:
            max_jitter = max(float(r.value.get("pop_jitter_ms") or 0.0) for r in pop_jitter_events)
            trigger_parts.append(f"POP jitter>{_POP_JITTER_THRESHOLD_MS:.0f}ms in {len(pop_jitter_events)}/{len(public_records)} (max={max_jitter:.1f}ms)")
        if trigger_parts:
            evidence.append("public-side triggers: " + "; ".join(trigger_parts))

        if local is None:
            evidence.append("local path unavailable in-window")
        else:
            local_jitter_vals = [
                float(r.value.get("local_jitter_ms") or 0.0)
                for r in local_records
                if r.value.get("local_jitter_ms") is not None
            ]
            local_jitter_peak = max(local_jitter_vals) if local_jitter_vals else 0.0
            evidence.append(
                f"local path clean in-window: degraded={local_degraded}/{len(local_records)}, "
                f"local_jitter_peak={local_jitter_peak:.1f}ms"
            )

        if dish_state is None:
            evidence.append("dish state unavailable in-window")
        else:
            observed_states = sorted({str(r.value.get("state") or "UNKNOWN") for r in state_records})
            evidence.append(
                f"dish state corroboration: no alert states in {len(state_records)} samples "
                f"(states={','.join(observed_states)})"
            )

        if dish_reboot is None:
            evidence.append("reboot signal unavailable in-window")
        else:
            evidence.append(
                f"reboot corroboration: no reboot events in {len(reboot_records)} samples"
            )

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
            missing.append("network.public_path sparse window coverage (<3 samples)")
        if degraded_ratio < 0.75:
            confidence = _downgrade_confidence(confidence)
            missing.append("public degradation not consistent across window")

        # Very short windows with modest loss are suggestive, not decisive.
        loss_max = float(incident.metrics.get("packet_loss_max") or 0.0)
        if incident.duration_seconds < 10 and loss_max < 0.5:
            confidence = _downgrade_confidence(confidence)
            missing.append("short/low-loss window (additional recurrence evidence needed)")

        routing = signals.get("network.routing")
        if routing is None or not routing.records_in_window(incident.start_time, incident.end_time):
            missing.append("network.routing corroboration in incident window")

        rf_env = signals.get("rf.environment")
        if rf_env is None or not rf_env.records_in_window(incident.start_time, incident.end_time):
            missing.append("rf.environment corroboration in incident window")

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
