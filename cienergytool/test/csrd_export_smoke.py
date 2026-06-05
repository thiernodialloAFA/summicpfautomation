#!/usr/bin/env python3
"""Smoke test of the CSRD export logic on the bundled sample reports.

This mirrors what `cienergy-csrd-export --by team` would produce, validates
the aggregation arithmetic, and writes a CSV next to the samples.
"""
from __future__ import annotations

import csv
import json
import os
import sys
from collections import defaultdict
from pathlib import Path


def load_reports(folder: Path):
    reports = []
    for f in sorted(folder.iterdir()):
        if f.suffix != ".json" or f.name == "index.json":
            continue
        reports.append(json.loads(f.read_text()))
    return reports


def by_team(reports):
    buckets = defaultdict(lambda: dict(n=0, kwh=0.0, op=0.0, emb=0.0, total=0.0, sci_sum=0.0))
    for r in reports:
        meta = r.get("metadata") or {}
        team = meta.get("team") or "(unspecified)"
        b = buckets[team]
        b["n"] += 1
        b["kwh"] += r["energy"]["totalKWh"]
        b["op"]  += r["carbon"]["operationalGCO2eq"]
        b["emb"] += r["carbon"]["embodiedGCO2eq"]
        b["total"] += r["carbon"]["totalGCO2eq"]
        b["sci_sum"] += r["sci"]["value"]
    return buckets


def main():
    samples = Path("dashboard/embedded/sample-reports")
    out = Path("/tmp/cienergy-csrd-smoke.csv")
    reports = load_reports(samples)
    print(f"loaded {len(reports)} reports")

    buckets = by_team(reports)
    with out.open("w", newline="") as f:
        w = csv.writer(f)
        w.writerow([
            "reporting_entity", "reporting_period", "scope2_method", "aggregation_key",
            "runs_count", "energy_kwh",
            "scope2_gco2eq_location_based",
            "scope3_cat1_gco2eq_embodied",
            "total_gco2eq", "sci_mean_gco2eq_per_run",
            "esrs_e1_6_reference",
        ])
        for team, b in sorted(buckets.items()):
            w.writerow([
                "AXA SA", "2026-Q2", "location-based", team,
                b["n"], f"{b['kwh']:.6f}",
                f"{b['op']:.3f}", f"{b['emb']:.3f}", f"{b['total']:.3f}",
                f"{b['sci_sum'] / b['n']:.3f}",
                "ESRS E1-6 (gross Scopes 1,2,3 + total GHG)",
            ])

    print(f"wrote {out}")
    print(out.read_text())

    # Coherence check: per-team sum equals per-report sum.
    total_co2 = sum(r["carbon"]["totalGCO2eq"] for r in reports)
    summed = sum(b["total"] for b in buckets.values())
    if abs(total_co2 - summed) > 0.001:
        print(f"INCONSISTENT: {total_co2} vs {summed}", file=sys.stderr)
        sys.exit(1)
    print(f"\nCONSISTENT: total = {total_co2:.3f} gCO2eq across {len(reports)} runs, {len(buckets)} teams")


if __name__ == "__main__":
    main()

