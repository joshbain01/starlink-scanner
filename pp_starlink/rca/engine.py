"""
RCAEngine — runs all registered RCA rules against each incident.

Usage:
    engine = RCAEngine()
    # (default rules are pre-registered)
    annotated = engine.run(incidents, registry)

Rule evaluation order: ascending priority (lowest int = runs first).
First rule returning a non-None result wins and is attached to the incident.
The UNKNOWN rule has priority=100 so it always fires as a fallback.
"""

from __future__ import annotations

from typing import Any, Dict, List, Optional

from pp_starlink.core.models import ContextSignal, Incident, SignalRecord
from pp_starlink.rca.rules.base import RCARule
from pp_starlink.rca.rules.bufferbloat import BufferbloatRule
from pp_starlink.rca.rules.dish_health import DishHealthRule
from pp_starlink.rca.rules.dish_reacq import DishReacquisitionRule
from pp_starlink.rca.rules.local_lan import LocalLANRule
from pp_starlink.rca.rules.obstruction import ObstructionRule
from pp_starlink.rca.rules.rf_interference import RFInterferenceRule
from pp_starlink.rca.rules.reboot import RebootRule
from pp_starlink.rca.rules.routing import RoutingRule
from pp_starlink.rca.rules.starlink_wan import StarlinkWANRule
from pp_starlink.rca.rules.systemic import SystemicStarlinkRule
from pp_starlink.rca.rules.unknown import UnknownRule
from pp_starlink.rca.rules.weather import WeatherRule
from pp_starlink.signals.registry import SignalRegistry

_DEFAULT_RULES: List[RCARule] = [
    LocalLANRule(),
    RebootRule(),
    DishReacquisitionRule(),
    DishHealthRule(),
    RFInterferenceRule(),
    StarlinkWANRule(),
    ObstructionRule(),
    RoutingRule(),
    BufferbloatRule(),
    WeatherRule(),
    SystemicStarlinkRule(),
    UnknownRule(),  # always last
]


def _make_incidents_signal(incidents: List[Incident]) -> ContextSignal:
    """Synthetic signal that carries the full incident list for systemic rule."""
    return ContextSignal(
        id="__incidents__",
        name="All Incidents (internal)",
        source="engine",
        resolution="event",
        records=[
            SignalRecord(
                timestamp="1970-01-01T00:00:00+00:00",
                value={"incidents": incidents},
                quality="derived",
            )
        ],
    )


class RCAEngine:
    """Runs RCA rules in priority order and annotates incidents in-place."""

    def __init__(self, rules: Optional[List[RCARule]] = None) -> None:
        self._rules: List[RCARule] = sorted(
            rules if rules is not None else list(_DEFAULT_RULES),
            key=lambda r: r.priority,
        )

    def add_rule(self, rule: RCARule) -> None:
        self._rules.append(rule)
        self._rules.sort(key=lambda r: r.priority)

    def run(
        self,
        incidents: List[Incident],
        registry: SignalRegistry,
    ) -> List[Incident]:
        """
        Evaluate all rules against each incident and attach RCA results.

        Modifies incidents in-place and returns the same list.
        """
        signals: Dict[str, ContextSignal] = registry.all()

        # Inject synthetic incidents signal for systemic rule
        signals["__incidents__"] = _make_incidents_signal(incidents)

        for incident in incidents:
            self._evaluate_incident(incident, signals)

        return incidents

    def _evaluate_incident(
        self,
        incident: Incident,
        signals: Dict[str, ContextSignal],
    ) -> None:
        for rule in self._rules:
            try:
                result = rule.evaluate(incident, signals)
            except Exception as exc:  # noqa: BLE001
                # Isolate rule failures — log and continue to next rule
                import logging
                logging.getLogger(__name__).warning(
                    "Rule %s raised exception for incident %s: %s",
                    type(rule).__name__,
                    incident.id,
                    exc,
                )
                continue

            if result is not None:
                incident.root_cause = result.get("root_cause")
                incident.confidence = result.get("confidence")
                incident.evidence = result.get("evidence", [])
                incident.missing_evidence = result.get("missing_evidence", [])
                return  # first match wins

        # Should never reach here (UnknownRule always fires), but be defensive
        incident.root_cause = "UNKNOWN"
        incident.confidence = "LOW"
        incident.evidence = ["No rule matched (engine fallback)"]
        incident.missing_evidence = []
