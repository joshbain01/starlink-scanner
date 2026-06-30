"""
Incident detector — identifies degradation windows from network_telemetry.

This module only detects; it does NOT assign root causes.  RCA is handled
separately by rca/engine.py.

Detection logic:
  An incident STARTS when packet_loss > loss_threshold OR pop_latency > latency_threshold
  for 2+ consecutive samples.

  An incident ENDS when metrics are below both thresholds for 3+ consecutive
  "recovery" samples in a row.

Metrics attached to each Incident:
  packet_loss_max:  peak packet loss (fraction 0–1)
  packet_loss_avg:  mean packet loss
  latency_max:      peak pop_latency_ms or max_latency_ms
  jitter_max:       peak local_jitter
  pop_drop_max:     peak pop_drop_rate
  sample_count:     number of 15 s samples in window
"""

from __future__ import annotations

import hashlib
from datetime import datetime, timezone
from typing import Dict, List, Optional

from pp_starlink.core.models import Incident

_DEFAULT_LOSS_THRESHOLD = 0.05    # 5 %
_DEFAULT_LATENCY_THRESHOLD = 200.0  # ms
_MIN_TRIGGER_SAMPLES = 2          # consecutive bad samples to open incident
_MIN_RECOVERY_SAMPLES = 3         # consecutive clean samples to close incident


def _ts_to_iso(unix_s: int) -> str:
    return datetime.fromtimestamp(unix_s, tz=timezone.utc).isoformat()


def _incident_id(start_iso: str) -> str:
    h = hashlib.md5(start_iso.encode()).hexdigest()[:8]  # noqa: S324 — not security-critical
    return f"inc-{h}"


def _is_bad(row: dict, loss_threshold: float, latency_threshold: float) -> bool:
    loss = row.get("public_packet_loss") or 0.0
    pop_lat = row.get("pop_latency_ms")
    max_lat = row.get("max_latency_ms")
    effective_lat = pop_lat if pop_lat is not None else (max_lat or 0.0)
    return loss > loss_threshold or effective_lat > latency_threshold


def _build_metrics(window: List[dict]) -> Dict:
    losses = [r.get("public_packet_loss") or 0.0 for r in window]
    pop_lats = [r["pop_latency_ms"] for r in window if r.get("pop_latency_ms") is not None]
    max_lats = [r["max_latency_ms"] for r in window if r.get("max_latency_ms") is not None]
    jitters = [r["local_jitter"] for r in window if r.get("local_jitter") is not None]
    pop_drops = [r["pop_drop_rate"] for r in window if r.get("pop_drop_rate") is not None]

    # Use POP latency if available, fall back to max_latency_ms from history window
    all_lats = pop_lats if pop_lats else max_lats

    return {
        "packet_loss_max": max(losses) if losses else 0.0,
        "packet_loss_avg": sum(losses) / len(losses) if losses else 0.0,
        "latency_max": max(all_lats) if all_lats else None,
        "jitter_max": max(jitters) if jitters else None,
        "pop_drop_max": max(pop_drops) if pop_drops else None,
        "sample_count": len(window),
    }


def detect_incidents(
    rows: List[dict],
    loss_threshold: float = _DEFAULT_LOSS_THRESHOLD,
    latency_threshold: float = _DEFAULT_LATENCY_THRESHOLD,
) -> List[Incident]:
    """
    Scan network_telemetry rows and return detected Incident objects.

    Args:
        rows:              ordered list of dicts from TelemetryReader.read_network()
        loss_threshold:    packet_loss fraction to trigger (default 0.05)
        latency_threshold: pop_latency_ms or max_latency_ms to trigger (default 200)

    Returns:
        List of Incident objects (no root_cause attached — run RCAEngine separately).
    """
    if not rows:
        return []

    incidents: List[Incident] = []

    # State machine
    in_incident = False
    incident_window: List[dict] = []
    trigger_candidates: List[dict] = []  # accumulating pre-incident bad samples
    recovery_count = 0

    for row in rows:
        bad = _is_bad(row, loss_threshold, latency_threshold)

        if not in_incident:
            if bad:
                trigger_candidates.append(row)
                if len(trigger_candidates) >= _MIN_TRIGGER_SAMPLES:
                    # Open incident; the trigger samples are part of the window
                    in_incident = True
                    incident_window = list(trigger_candidates)
                    trigger_candidates = []
                    recovery_count = 0
            else:
                trigger_candidates = []  # reset — consecutive bad required
        else:
            # Inside incident
            if bad:
                incident_window.append(row)
                recovery_count = 0  # reset recovery streak
            else:
                # Recovery candidate
                incident_window.append(row)
                recovery_count += 1
                if recovery_count >= _MIN_RECOVERY_SAMPLES:
                    # Close the incident; exclude the trailing recovery samples
                    core_window = incident_window[: len(incident_window) - _MIN_RECOVERY_SAMPLES]
                    if core_window:
                        incidents.append(_make_incident(core_window))
                    in_incident = False
                    incident_window = []
                    recovery_count = 0

    # If still in incident at end of data, close it
    if in_incident and incident_window:
        incidents.append(_make_incident(incident_window))

    return incidents


def _make_incident(window: List[dict]) -> Incident:
    start_ts = _ts_to_iso(window[0]["timestamp"])
    end_ts = _ts_to_iso(window[-1]["timestamp"])
    duration = window[-1]["timestamp"] - window[0]["timestamp"]
    metrics = _build_metrics(window)
    return Incident(
        id=_incident_id(start_ts),
        start_time=start_ts,
        end_time=end_ts,
        duration_seconds=max(duration, 0),
        metrics=metrics,
    )
