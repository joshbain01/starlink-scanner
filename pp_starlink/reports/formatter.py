"""
Report formatter — produces human-readable text and structured JSON for RCA results.
"""

from __future__ import annotations

import json
from collections import Counter
from datetime import datetime, timezone
from typing import Any, List, Optional

from pp_starlink.core.models import Incident

_CONFIDENCE_BADGE = {
    "HIGH": "🔴 HIGH",
    "MEDIUM": "🟡 MEDIUM",
    "LOW": "⚪ LOW",
    None: "❓ UNKNOWN",
}

_DIVIDER = "─" * 72

_CAUSE_PLAYBOOK: dict[str, dict[str, list[str]]] = {
    "LOCAL_LAN_ISSUE": {
        "next_checks": [
            "validate LAN packet loss between host and router during incident window",
            "check Ethernet link flaps / speed renegotiation",
            "confirm no local QoS or queue saturation",
        ],
        "immediate_actions": [
            "replace / reseat Ethernet cable and connectors",
            "force stable link speed if hardware allows",
            "isolate heavy local traffic producers",
        ],
    },
    "DISH_REBOOT": {
        "next_checks": [
            "correlate reboot timestamps with power quality events",
            "inspect thermal / power alerts around reboot window",
            "review firmware update / maintenance timing",
        ],
        "immediate_actions": [
            "stabilize power path (UPS / PSU / connectors)",
            "ensure dish thermal operating envelope",
            "monitor reboot recurrence rate over 24h",
        ],
    },
    "DISH_REACQUISITION": {
        "next_checks": [
            "inspect outage_cause sequences for searching / reacquire patterns",
            "compare look-angle continuity versus outage onset",
            "check dish orientation / mount stability",
        ],
        "immediate_actions": [
            "verify mount is rigid and near-vertical",
            "clear near-horizon obstructions in dominant azimuth",
            "collect additional directional obstruction samples",
        ],
    },
    "STARLINK_WAN_OR_POP": {
        "next_checks": [
            "compare local path health versus POP/public degradation",
            "inspect POP latency and drop spikes during incidents",
            "check correlation with concurrent incidents cluster",
        ],
        "immediate_actions": [
            "fail over critical traffic if multi-WAN is available",
            "reduce real-time QoS sensitivity during high-loss windows",
            "capture escalation bundle for provider ticket",
        ],
    },
    "OBSTRUCTION": {
        "next_checks": [
            "inspect obstruction map bins with repeated incidents",
            "correlate affected az/el bins with physical structures",
            "verify boresight / elevation ranges during outage windows",
        ],
        "immediate_actions": [
            "remove or trim blocking objects in affected direction",
            "consider dish relocation to wider sky view",
            "re-run daemon collection after remediation",
        ],
    },
    "ROUTING_OR_POP_PATH_CHANGE": {
        "next_checks": [
            "track POP fingerprint / path changes before incidents",
            "compare drop rate shortly after path changes",
            "review regional pattern across time-of-day",
        ],
        "immediate_actions": [
            "flag unstable POP transitions in monitoring",
            "prefer resilient app retry / timeout policy",
            "prepare provider escalation with path-change evidence",
        ],
    },
    "RF_INTERFERENCE_OR_EMI": {
        "next_checks": [
            "compare RF noise-floor rise with incident windows",
            "look for recurring EMI timing patterns (hour/day)",
            "validate nearby emitters and cabling/shielding conditions",
        ],
        "immediate_actions": [
            "increase physical separation from likely EMI sources",
            "inspect and harden RF cable/connectors and shielding",
            "capture focused RF trace around next recurrence window",
        ],
    },
    "BUFFERBLOAT_OR_LOAD": {
        "next_checks": [
            "inspect uplink/downlink load near incident start",
            "validate latency growth under throughput pressure",
            "separate local saturation from WAN impairment",
        ],
        "immediate_actions": [
            "cap non-critical bulk traffic during peaks",
            "apply SQM / queue management where possible",
            "schedule heavy transfers off critical windows",
        ],
    },
    "WEATHER_IMPAIRMENT": {
        "next_checks": [
            "correlate precipitation / wind windows with incidents",
            "validate hourly weather feed quality",
            "check whether outages persist after weather clears",
        ],
        "immediate_actions": [
            "expect temporary MOS degradation during severe weather",
            "prioritize resilient transport / retries",
            "monitor post-weather recovery slope",
        ],
    },
    "SYSTEMIC_STARLINK_EVENT": {
        "next_checks": [
            "measure incident density over rolling 15-minute windows",
            "compare multi-signal symptom consistency",
            "rule out local environmental contributors",
        ],
        "immediate_actions": [
            "switch critical workflows to backup link",
            "capture time-bounded evidence set for escalation",
            "defer non-critical real-time operations",
        ],
    },
    "UNKNOWN": {
        "next_checks": [
            "close missing-evidence gaps called out in report",
            "add higher-fidelity signal modules for weak areas",
            "re-run analyze after additional data collection",
        ],
        "immediate_actions": [
            "treat as degraded service until disproven",
            "increase telemetry coverage around recurrence windows",
            "track recurrence signature by time and conditions",
        ],
    },
}

_CONFIDENCE_SCORE = {
    "HIGH": 3,
    "MEDIUM": 2,
    "LOW": 1,
    None: 0,
}

_CLUSTER_GAP_SECONDS = 300


def _root_cause_key(incident: Incident) -> str:
    return (incident.root_cause or "UNKNOWN").strip().upper().replace(" ", "_")


def _cause_playbook(incident: Incident) -> dict[str, list[str]]:
    return _CAUSE_PLAYBOOK.get(_root_cause_key(incident), _CAUSE_PLAYBOOK["UNKNOWN"])


def _severity_score(incident: Incident) -> float:
    m = incident.metrics
    loss = float(m.get("packet_loss_max") or 0.0)
    latency = float(m.get("latency_max") or 0.0)
    jitter = float(m.get("jitter_max") or 0.0)
    duration = float(incident.duration_seconds or 0)
    pop_drop = float(m.get("pop_drop_max") or 0.0)

    # Weighted [0,100] severity for AI prioritization.
    score = 0.0
    score += min(45.0, loss * 45.0)
    score += min(20.0, latency / 20.0)
    score += min(10.0, jitter / 10.0)
    score += min(15.0, duration / 12.0)
    score += min(10.0, pop_drop * 10.0)
    return round(max(0.0, min(100.0, score)), 1)


def _severity_label(score: float) -> str:
    if score >= 75:
        return "CRITICAL"
    if score >= 50:
        return "HIGH"
    if score >= 25:
        return "MEDIUM"
    return "LOW"


def _readiness_score(incident: Incident) -> float:
    present = len(incident.evidence)
    missing = len(incident.missing_evidence)
    total = present + missing
    if total == 0:
        return 0.0
    return round(100.0 * present / total, 1)


def _fmt_ts(ts: str) -> str:
    try:
        dt = datetime.fromisoformat(ts)
        return dt.isoformat()
    except Exception:
        return ts


def _parse_ts(ts: str) -> datetime | None:
    try:
        return datetime.fromisoformat(ts)
    except Exception:
        return None


def _loss_bucket(incident: Incident) -> str:
    loss = float(incident.metrics.get("packet_loss_max") or 0.0) * 100.0
    if loss >= 80.0:
        return "loss_80_100"
    if loss >= 40.0:
        return "loss_40_79"
    if loss >= 20.0:
        return "loss_20_39"
    if loss >= 10.0:
        return "loss_10_19"
    return "loss_00_09"


def _duration_bucket(incident: Incident) -> str:
    dur = int(incident.duration_seconds or 0)
    if dur < 10:
        return "dur_00_09s"
    if dur < 30:
        return "dur_10_29s"
    if dur < 120:
        return "dur_30_119s"
    return "dur_120s_plus"


def _incident_signature(incident: Incident) -> tuple[str, str, str, str, str]:
    return (
        incident.root_cause or "UNKNOWN",
        incident.confidence or "UNKNOWN",
        _severity_label(_severity_score(incident)),
        _loss_bucket(incident),
        _duration_bucket(incident),
    )


def _build_recurrence_clusters(incidents: list[Incident]) -> list[dict[str, Any]]:
    ordered = sorted(incidents, key=lambda inc: inc.start_time)
    clusters: list[dict[str, Any]] = []

    for inc in ordered:
        sig = _incident_signature(inc)
        start_dt = _parse_ts(inc.start_time)
        end_dt = _parse_ts(inc.end_time) or start_dt

        matched = False
        if start_dt is not None and end_dt is not None:
            for c in reversed(clusters):
                if c["signature"] != sig:
                    continue
                gap = (start_dt - c["last_end"]).total_seconds()
                if gap <= _CLUSTER_GAP_SECONDS:
                    c["count"] += 1
                    c["last_end"] = max(c["last_end"], end_dt)
                    c["max_severity"] = max(c["max_severity"], _severity_score(inc))
                    c["incident_ids"].append(inc.id)
                    matched = True
                    break

        if not matched:
            if start_dt is None:
                start_dt = datetime.min.replace(tzinfo=timezone.utc)
            if end_dt is None:
                end_dt = start_dt
            clusters.append(
                {
                    "signature": sig,
                    "first_start": start_dt,
                    "last_end": end_dt,
                    "count": 1,
                    "max_severity": _severity_score(inc),
                    "incident_ids": [inc.id],
                }
            )

    clusters = [c for c in clusters if c["count"] > 1]
    clusters.sort(key=lambda c: (c["count"], c["max_severity"]), reverse=True)
    return clusters


def format_incident(incident: Incident, index: Optional[int] = None) -> str:
    """Return a multi-line human-readable block for one incident."""
    sev_score = _severity_score(incident)
    sev_label = _severity_label(sev_score)
    readiness = _readiness_score(incident)
    playbook = _cause_playbook(incident)

    lines = [_DIVIDER]
    prefix = f"[{index}] " if index is not None else ""
    lines.append(f"{prefix}Incident {incident.id}")
    lines.append(f"  Window:    {_fmt_ts(incident.start_time)}  ->  {_fmt_ts(incident.end_time)}")
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
    pop_drop = m.get("pop_drop_max")
    if pop_drop is not None:
        lines.append(f"  POP Drop:  max={pop_drop*100:.1f}%")

    badge = _CONFIDENCE_BADGE.get(incident.confidence, "❓")
    lines.append(f"  Root Cause: {incident.root_cause or 'UNKNOWN'}  [{badge}]")
    lines.append(f"  RCA Rank:  severity={sev_label} ({sev_score:.1f}/100)  readiness={readiness:.1f}%")

    if incident.signals:
        lines.append("  Signals Used:")
        for s in sorted(incident.signals):
            lines.append(f"    - {s}")

    if incident.evidence:
        lines.append("  Evidence:")
        for e in incident.evidence:
            lines.append(f"    • {e}")

    if incident.missing_evidence:
        lines.append("  Missing Evidence:")
        for m_ev in incident.missing_evidence:
            lines.append(f"    ✗ {m_ev}")

    lines.append("  Next Checks:")
    for check in playbook["next_checks"]:
        lines.append(f"    - {check}")

    lines.append("  Immediate Actions:")
    for action in playbook["immediate_actions"]:
        lines.append(f"    - {action}")

    lines.append("  AI RCA Packet:")
    lines.append(f"    incident_id: {incident.id}")
    lines.append(f"    hypothesis: {incident.root_cause or 'UNKNOWN'}")
    lines.append(f"    confidence: {incident.confidence or 'UNKNOWN'}")
    lines.append(f"    severity_score: {sev_score:.1f}")
    lines.append(f"    readiness_score: {readiness:.1f}")
    lines.append(f"    disconfirmers_needed: {len(incident.missing_evidence)}")

    return "\n".join(lines)


def format_report(incidents: List[Incident], title: str = "Starlink RCA Report") -> str:
    """Return a full human-readable report for a list of incidents."""
    if not incidents:
        return f"{title}\n\nNo incidents detected.\n"

    ranked = sorted(
        incidents,
        key=lambda inc: (
            _severity_score(inc),
            _CONFIDENCE_SCORE.get(inc.confidence, 0),
            inc.duration_seconds,
        ),
        reverse=True,
    )

    header_lines = [
        "=" * 72,
        f"  {title}",
        f"  Generated: {datetime.now(tz=timezone.utc).isoformat()}",
        f"  Total incidents: {len(ranked)}",
        "=" * 72,
    ]

    # Summary table
    causes = Counter(inc.root_cause or "UNKNOWN" for inc in ranked)
    header_lines.append("\nRoot Cause Summary:")
    for cause, count in causes.most_common():
        bar = "█" * min(count, 40)
        header_lines.append(f"  {cause:<35} {count:>4}  {bar}")

    conf = Counter(inc.confidence or "UNKNOWN" for inc in ranked)
    header_lines.append("\nConfidence Distribution:")
    for level in ("HIGH", "MEDIUM", "LOW", "UNKNOWN"):
        if level in conf:
            header_lines.append(f"  {level:<8} {conf[level]:>4}")

    sev = Counter(_severity_label(_severity_score(inc)) for inc in ranked)
    header_lines.append("\nSeverity Distribution:")
    for level in ("CRITICAL", "HIGH", "MEDIUM", "LOW"):
        if level in sev:
            header_lines.append(f"  {level:<8} {sev[level]:>4}")

    missing = Counter(m for inc in ranked for m in inc.missing_evidence)
    if missing:
        header_lines.append("\nTop Missing Evidence Gaps:")
        for item, count in missing.most_common(8):
            header_lines.append(f"  - {item} ({count})")

    clusters = _build_recurrence_clusters(ranked)
    if clusters:
        header_lines.append("\nRecurring Incident Clusters (dedup lens):")
        for idx, c in enumerate(clusters[:8], start=1):
            cause, conf_level, sev_label, loss_bucket, dur_bucket = c["signature"]
            start_s = c["first_start"].isoformat()
            end_s = c["last_end"].isoformat()
            examples = ", ".join(c["incident_ids"][:3])
            header_lines.append(
                f"  {idx}. {cause} x{c['count']} [{conf_level}, {sev_label}, {loss_bucket}, {dur_bucket}] "
                f"window={start_s}..{end_s} examples={examples}"
            )

    # Worst day
    day_counts: Counter = Counter()
    for inc in ranked:
        day = inc.start_time[:10]
        day_counts[day] += 1
    if day_counts:
        worst_day, worst_count = day_counts.most_common(1)[0]
        header_lines.append(f"\nWorst day: {worst_day}  ({worst_count} incidents)")

    top3 = ranked[:3]
    if top3:
        header_lines.append("\nTop 3 Incidents To Triage First:")
        for inc in top3:
            s = _severity_score(inc)
            header_lines.append(
                f"  - {inc.id}: {_severity_label(s)} ({s:.1f}/100), "
                f"cause={inc.root_cause or 'UNKNOWN'}, conf={inc.confidence or 'UNKNOWN'}"
            )

    # Per-incident blocks
    incident_blocks = [format_incident(inc, i + 1) for i, inc in enumerate(ranked)]

    return "\n".join(header_lines) + "\n\n" + "\n\n".join(incident_blocks) + "\n"


def incidents_to_json(incidents: List[Incident]) -> str:
    """Return incidents as a JSON string."""
    payload = [_incident_payload(inc) for inc in incidents]
    return json.dumps(payload, indent=2, default=str)


def incidents_to_ai_bundle_json(
    incidents: List[Incident],
    title: str = "Starlink RCA AI Bundle",
) -> str:
    """Return a single AI-first JSON bundle for RCA automation."""
    generated_at = datetime.now(tz=timezone.utc).isoformat()

    ranked = sorted(
        incidents,
        key=lambda inc: (
            _severity_score(inc),
            _CONFIDENCE_SCORE.get(inc.confidence, 0),
            inc.duration_seconds,
        ),
        reverse=True,
    )

    causes = Counter(inc.root_cause or "UNKNOWN" for inc in ranked)
    confidence = Counter(inc.confidence or "UNKNOWN" for inc in ranked)
    severity = Counter(_severity_label(_severity_score(inc)) for inc in ranked)
    missing = Counter(m for inc in ranked for m in inc.missing_evidence)
    signals = Counter(s for inc in ranked for s in inc.signals)
    clusters = _build_recurrence_clusters(ranked)

    window_start = min((inc.start_time for inc in ranked), default=None)
    window_end = max((inc.end_time for inc in ranked), default=None)

    top_incidents = [
        {
            "id": inc.id,
            "severity": {
                "label": _severity_label(_severity_score(inc)),
                "score": _severity_score(inc),
            },
            "root_cause": inc.root_cause,
            "confidence": inc.confidence,
            "duration_seconds": inc.duration_seconds,
        }
        for inc in ranked[:5]
    ]

    bundle = {
        "schema_version": "1.0",
        "title": title,
        "generated_at": generated_at,
        "incident_count": len(ranked),
        "analysis_window": {
            "start": window_start,
            "end": window_end,
        },
        "summary": {
            "root_causes": dict(causes),
            "confidence_distribution": dict(confidence),
            "severity_distribution": dict(severity),
            "signal_coverage": dict(signals),
            "unique_recurrence_clusters": len(clusters),
            "recurrence_clusters": [
                {
                    "root_cause": c["signature"][0],
                    "confidence": c["signature"][1],
                    "severity_label": c["signature"][2],
                    "loss_bucket": c["signature"][3],
                    "duration_bucket": c["signature"][4],
                    "count": c["count"],
                    "window_start": c["first_start"].isoformat(),
                    "window_end": c["last_end"].isoformat(),
                    "example_incident_ids": c["incident_ids"][:5],
                    "max_severity_score": round(c["max_severity"], 1),
                }
                for c in clusters[:20]
            ],
            "top_missing_evidence": [
                {"item": item, "count": count}
                for item, count in missing.most_common(12)
            ],
            "top_incidents": top_incidents,
        },
        "incidents": [_incident_payload(inc) for inc in ranked],
    }
    return json.dumps(bundle, indent=2, default=str)


def _incident_payload(inc: Incident) -> dict[str, Any]:
    sev_score = _severity_score(inc)
    readiness = _readiness_score(inc)
    playbook = _cause_playbook(inc)
    return {
        "id": inc.id,
        "start_time": inc.start_time,
        "end_time": inc.end_time,
        "duration_seconds": inc.duration_seconds,
        "root_cause": inc.root_cause,
        "confidence": inc.confidence,
        "severity": {
            "label": _severity_label(sev_score),
            "score": sev_score,
        },
        "readiness_score": readiness,
        "metrics": inc.metrics,
        "evidence": inc.evidence,
        "missing_evidence": inc.missing_evidence,
        "signals": inc.signals,
        "next_checks": playbook["next_checks"],
        "immediate_actions": playbook["immediate_actions"],
    }


def format_worst_day_summary(incidents: List[Incident]) -> str:
    """One-liner summary of the worst day for quick CLI output."""
    if not incidents:
        return "No incidents found."
    day_counts: Counter = Counter(inc.start_time[:10] for inc in incidents)
    worst_day, count = day_counts.most_common(1)[0]
    day_incidents = [inc for inc in incidents if inc.start_time.startswith(worst_day)]
    causes = Counter(inc.root_cause or "UNKNOWN" for inc in day_incidents)
    cause_str = ", ".join(f"{c}×{n}" for c, n in causes.most_common())
    top = max(day_incidents, key=_severity_score)
    top_score = _severity_score(top)
    return (
        f"Worst day: {worst_day}  ({count} incidents)  Causes: {cause_str}  "
        f"Top incident: {top.id} {_severity_label(top_score)} {top_score:.1f}/100"
    )
