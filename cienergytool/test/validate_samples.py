#!/usr/bin/env python3
"""Validate cienergy sample reports against docs/schema/v1.json
and verify the SCI formula coherence:  SCI = ((E * I) + M) / R."""
import json
import os
import sys

SCHEMA = json.load(open("docs/schema/v1.json"))


def missing(name, obj, required):
    miss = [k for k in required if k not in obj]
    if miss:
        print(f"  MISSING in {name}: {miss}")
        return True
    return False


def main():
    ok = True
    dirpath = "dashboard/embedded/sample-reports"
    for fn in sorted(os.listdir(dirpath)):
        if not fn.endswith(".json") or fn == "index.json":
            continue
        r = json.load(open(os.path.join(dirpath, fn)))
        print(
            f"{fn}: SCI={r['sci']['value']} gCO2eq, "
            f"E={r['energy']['totalKWh']} kWh, "
            f"I={r['carbon']['gridIntensity']['valueGCO2eqPerKWh']} "
            f"({r['carbon']['gridIntensity']['zone']})"
        )
        if missing(fn, r, SCHEMA["required"]):
            ok = False
        for sub in ["run", "runner", "energy", "carbon", "sci"]:
            if missing(f"{fn}.{sub}", r[sub], SCHEMA["properties"][sub]["required"]):
                ok = False
        E = r["energy"]["totalKWh"]
        I = r["carbon"]["gridIntensity"]["valueGCO2eqPerKWh"]
        M = r["carbon"]["embodiedGCO2eq"]
        R = r["sci"]["R"]
        expected = (E * I + M) / R
        tolerance = max(0.05, 0.01 * expected)
        if abs(expected - r["sci"]["value"]) > tolerance:
            print(f"  ! SCI mismatch in {fn}: computed {expected:.3f}, reported {r['sci']['value']}")
            ok = False
    print()
    print("ALL VALID" if ok else "VALIDATION FAILED")
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()

