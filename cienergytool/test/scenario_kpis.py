"""Compute the cienergy KPI values for an arbitrary scenario, replicating
the JS formatting logic exactly so the printed strings match what the
embedded dashboard renders in the Total energy / Total carbon cards.
"""
import json, glob, sys

ZONE_PL = 660.0            # gCO2eq/kWh, Poland 2024
RUNS_PER_YEAR = 36_500     # ex.: 100 runs/day x 365 days

paths = sorted(glob.glob("dashboard/embedded/local-reports/cienergy-*.json"))
reports = [json.load(open(p)) for p in paths]
n = len(reports)
if n == 0:
    print("no reports staged in dashboard/embedded/local-reports/")
    sys.exit(1)

print(f"Loaded {n} report(s):")
for r in reports:
    print(f"  - {r['run']['repository']:35s} "
          f"E={r['energy']['totalKWh']:.6f} kWh  "
          f"embodied={r['carbon']['embodiedGCO2eq']:.3f} g")

sum_kwh      = sum(r["energy"]["totalKWh"]        for r in reports)
sum_embodied = sum(r["carbon"]["embodiedGCO2eq"] for r in reports)
sum_oper_PL  = sum(r["energy"]["totalKWh"] * ZONE_PL for r in reports)
sum_total_PL = sum_oper_PL + sum_embodied

mean_kwh   = sum_kwh      / n
mean_co2PL = sum_total_PL / n

factor        = RUNS_PER_YEAR / n
projected_kwh = sum_kwh * factor                 # = mean_kwh   * RUNS_PER_YEAR
projected_co2 = mean_co2PL * RUNS_PER_YEAR       # = mean_co2PL * RUNS_PER_YEAR

print()
print(f"=== Scenario: zone PL @ {ZONE_PL:.0f} g/kWh  x {RUNS_PER_YEAR:,} runs/yr ===")
print(f"Sum measured kWh                : {sum_kwh:.6f} kWh")
print(f"Sum measured carbon @PL         : {sum_total_PL:.3f} g  "
      f"(op {sum_oper_PL:.3f} + embodied {sum_embodied:.3f})")
print(f"Mean per run kWh                : {mean_kwh:.6f} kWh")
print(f"Mean per run carbon @PL         : {mean_co2PL:.3f} g")
print(f"Projection factor (runsYr / N)  : {factor:.2f}")
print()
print(f"-> Total energy (annual)        : {projected_kwh:.3f} kWh"
      f"    ({projected_kwh/1000:.4f} MWh)")
print(f"-> Total carbon (annual)        : {projected_co2:.1f} g"
      f"        ({projected_co2/1000:.3f} kgCO2eq, "
      f"{projected_co2/1e6:.6f} tCO2eq)")


def d(v, p=2):
    a = abs(v)
    if a >= 100: return f"{v:.0f}"
    if a >= 10:  return f"{v:.1f}"
    return f"{v:.{p}f}"

def fmt_energy(kwh):
    a = abs(kwh)
    if a >= 1e9: return d(kwh/1e9), "TWh"
    if a >= 1e6: return d(kwh/1e6), "GWh"
    if a >= 1e3: return d(kwh/1e3), "MWh"
    if a >= 1:   return d(kwh, 3),  "kWh"
    if a >= 1e-3:return d(kwh*1e3), "Wh"
    return            d(kwh*1e6),   "mWh"

def fmt_carbon(g):
    a = abs(g)
    if a >= 1e12: return d(g/1e12), "MtCO2eq"
    if a >= 1e9:  return d(g/1e9),  "ktCO2eq"
    if a >= 1e6:  return d(g/1e6),  "tCO2eq"
    if a >= 1e3:  return d(g/1e3),  "kgCO2eq"
    if a >= 1:    return d(g, 3),   "gCO2eq"
    return             d(g*1e3),    "mgCO2eq"

ev, eu = fmt_energy(projected_kwh)
cv, cu = fmt_carbon(projected_co2)
suffix = f"x {RUNS_PER_YEAR:,} runs/yr  zone PL @ {ZONE_PL:.0f} g/kWh"

print()
print("As rendered in the dashboard KPI cards:")
print(f"  Total energy : {ev} {eu}  ::  {suffix}")
print(f"  Total carbon : {cv} {cu}  ::  {suffix}")

