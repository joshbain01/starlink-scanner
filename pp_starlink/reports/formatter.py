"""
Report formatter — produces human-readable text and structured JSON for RCA results.
"""

from __future__ import annotations

import json
from collections import Counter
from datetime import datetime, timezone
from typing import List, Optional

from pp_starlink.core.models import Incident

_CONFIDENCE_BADGE = {
    "HIGH": "🔴 HIGH",
    "MEDIUM": "🟡 MEDIUM",
    "LOW": "⚪ LOW",
    None: "❓ UNKNOWN",
}

_DIVIDER = "─" * 72


def format_incident(incident: Incident, index: Optional[int] = None) -> str:
    """Return a multi-line human-readable block for one incident."""
    lines = [_DIVIDER]
    prefix = f"[{index}] " if index is not None else ""
    lines.append(f"{prefix}Incident {incident.id}")
    lines.append(f"  Window:    {incident.start_time}  →  {incident.end_time}")
    lines.append(f"  Duration:  {incident.duration_seconds}s")

    m = incident.metrics
    loss_max = m.get("packet_loss_max", 0)
    loss_avg = m.get("packet_loss_avg", 0)
    lat = m.get("latency_max")
    jitter = m.get("jitter_max")
    samples = m.get("sample_count", "?")

    lines.append(
        f"  Loss:      max={loss_max*100:.1f}%  avg={loss_avg*100:.1f}%  "
        f"samples={samples}"
    )
    if lat is not None:
        lines.append(f"  Latency:   max={lat:.0f}ms")
    if jitter is not None:
        lines.append(f"  Jitter:    max={jitter:.1f}ms")

    badge = _CONFIDENCE_BADGE.get(incident.confidence, "❓")
    lines.append(f"  Root Cause: {incident.root_cause or 'UNKNOWN'}  [{badge}]")

    if incident.evidence:
        lines.append("  Evidence:")
        for e in incident.evidence:
            lines.append(f"    • {e}")

    if incident.missing_evidence:
        lines.append("  Missing Evidence:")
        for m_ev in incident.missing_evidence:
            lines.append(f"    ✗ {m_ev}")

    return "\n".join(lines)


def format_report(incidents: List[Incident], title: str = "Starlink RCA Report") -> str:
    """Return a full human-readable report for a list of incidents."""
    if not incidents:
        return f"{title}\n\nNo incidents detected.\n"

    header_lines = [
        "=" * 72,
        f"  {title}",
        f"  Generated: {datetime.now(tz=timezone.utc).isoformat()}",
        f"  Total incidents: {len(incidents)}",
        "=" * 72,
    ]

    # Summary table
    causes = Counter(inc.root_cause or "UNKNOWN" for inc in incidents)
    header_lines.append("\nRoot Cause Summary:")
    for cause, count in causes.most_common():
        bar = "█" * min(count, 40)
        header_lines.append(f"  {cause:<35} {count:>4}  {bar}")

    # Worst day
    day_counts: Counter = Counter()
    for inc in incidents:
        day = inc.start_time[:10]
        day_counts[day] += 1
    if day_counts:
        worst_day, worst_count = day_counts.most_common(1)[0]
        header_lines.append(f"\nWorst day: {worst_day}  ({worst_count} incidents)")

    # Per-incident blocks
    incident_blocks = [format_incident(inc, i + 1) for i, inc in enumerate(incidents)]

    return "\n".join(header_lines) + "\n\n" + "\n\n".join(incident_blocks) + "\n"


def incidents_to_json(incidents: List[Incident]) -> str:
    """Return incidents as a JSON string."""
    payload = []
    for inc in incidents:
        payload.append({
            "id": inc.id,
            "start_time": inc.start_time,
            "end_time": inc.end_time,
            "duration_seconds": inc.duration_seconds,
            "root_cause": inc.root_cause,
            "confidence": inc.confidence,
            "metrics": inc.metrics,
            "evidence": inc.evidence,
            "missing_evidence": inc.missing_evidence,
            "signals": inc.signals,
        })
    return json.dumps(payload, indent=2, default=str)


def format_worst_day_summary(incidents: List[Incident]) -> str:
    """One-liner summary of the worst day for quick CLI output."""
    if not incidents:
        return "No incidents found."
    day_counts: Counter = Counter(inc.start_time[:10] for inc in incidents)
    worst_day, count = day_counts.most_common(1)[0]
    day_incidents = [inc for inc in incidents if inc.start_time.startswith(worst_day)]
    causes = Counter(inc.root_cause or "UNKNOWN" for inc in day_incidents)
    cause_str = ", ".join(f"{c}×{n}" for c, n in causes.most_common())
    return f"Worst day: {worst_day}  ({count} incidents)  Causes: {cause_str}"
