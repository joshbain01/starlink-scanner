"""
pp_starlink — Root-cause analysis tool for Starlink MOS drops.

Read from the shared SQLite telemetry database written by the Go daemon and
rf_listener.py, detect degradation incidents, run structured RCA rules, and
emit per-incident reports.
"""

__version__ = "0.1.0"
