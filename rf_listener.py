#!/usr/bin/env python3
"""
rf-listener: samples Ku-band pilot tones via rtl_power and writes SNR +
noise floor to the shared SQLite database.

LNB local oscillator: 9.75 GHz  =>  Starlink Ku downlinks (~10.7–12.75 GHz)
appear in the RTL-SDR as ~950 MHz–3000 MHz IF.
Pilot tone window (adjust CENTER_FREQ / BANDWIDTH for your LO and target):
  Default: 1575 MHz ± 2 MHz  (tune with RF_CENTER_HZ / RF_BW_HZ env vars)
"""

import os
import re
import sqlite3
import subprocess
import sys
import time
from datetime import datetime, timezone

DB_PATH     = os.getenv("DB_PATH",     "/data/starlink_telemetry.db")
CENTER_FREQ = int(os.getenv("RF_CENTER_HZ", "1_575_000_000"))
BANDWIDTH   = int(os.getenv("RF_BW_HZ",     "4_000_000"))
GAIN        = os.getenv("RTL_GAIN",         "40")      # dB; 0=auto
INTERVAL    = int(os.getenv("SAMPLE_INTERVAL_S", "10"))  # seconds per sweep

PRAGMAS = [
    "PRAGMA journal_mode = WAL",
    "PRAGMA temp_store = MEMORY",
    "PRAGMA synchronous = OFF",
    "PRAGMA cache_size = -4000",
    "PRAGMA auto_vacuum = INCREMENTAL",
]


def open_db():
    con = sqlite3.connect(DB_PATH, check_same_thread=False)
    for p in PRAGMAS:
        con.execute(p)
    return con


def run_rtl_power():
    """
    Run rtl_power for one sweep and return list of (freq_hz, power_db) pairs.
    rtl_power CSV: date, time, freq_lo, freq_hi, bin_width, samples, pwr...
    """
    start = CENTER_FREQ - BANDWIDTH // 2
    stop  = CENTER_FREQ + BANDWIDTH // 2
    cmd = [
        "rtl_power",
        "-f", f"{start}:{stop}:50000",   # 50 kHz bins
        "-g", GAIN,
        "-e", "1s",                       # exit after 1 second sweep
        "-",                              # write CSV to stdout
    ]
    try:
        out = subprocess.check_output(cmd, stderr=subprocess.DEVNULL, timeout=15)
    except (subprocess.CalledProcessError, subprocess.TimeoutExpired) as e:
        print(f"rtl_power error: {e}", file=sys.stderr)
        return []

    samples = []
    for line in out.decode().splitlines():
        parts = line.split(",")
        if len(parts) < 7:
            continue
        try:
            f_lo = float(parts[2])
            bw   = float(parts[4])
            pwrs = [float(x) for x in parts[6:] if x.strip()]
            for i, p in enumerate(pwrs):
                samples.append((f_lo + i * bw, p))
        except ValueError:
            continue
    return samples


def analyze(samples):
    """
    Returns (beacon_snr_db, noise_floor_db).
    beacon_snr_db: peak power minus noise floor in the sweep window.
    noise_floor_db: 10th percentile of all bins (wideband baseline).
    """
    if not samples:
        return None, None
    powers = [p for _, p in samples]
    powers_sorted = sorted(powers)
    noise_floor = powers_sorted[max(0, len(powers_sorted) // 10)]   # 10th pct
    peak        = powers_sorted[-1]
    beacon_snr  = peak - noise_floor
    return round(beacon_snr, 2), round(noise_floor, 2)


def write_rf(con, snr, noise):
    ts = int(datetime.now(timezone.utc).timestamp())
    con.execute(
        "INSERT INTO rf_telemetry VALUES (?,?,?)", (ts, snr, noise)
    )
    con.commit()


def main():
    print(f"rf-listener starting  center={CENTER_FREQ/1e6:.1f} MHz  bw={BANDWIDTH/1e6:.1f} MHz  interval={INTERVAL}s")
    con = open_db()

    while True:
        start = time.monotonic()
        samples = run_rtl_power()
        snr, noise = analyze(samples)
        if snr is not None:
            write_rf(con, snr, noise)
            print(f"snr={snr:+.1f} dB  noise={noise:.1f} dB", flush=True)
        else:
            print("no samples", file=sys.stderr)

        elapsed = time.monotonic() - start
        time.sleep(max(0, INTERVAL - elapsed))


if __name__ == "__main__":
    main()
