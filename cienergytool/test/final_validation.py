#!/usr/bin/env python3
"""Final validation: Go syntax, YAML, JSON schema, CSRD smoke."""
from __future__ import annotations
import json
import pathlib
import subprocess
import sys

import yaml

ROOT = pathlib.Path(".")


def gitlab_reference(loader, node):
    if isinstance(node, yaml.SequenceNode):
        return loader.construct_sequence(node)
    return loader.construct_scalar(node)


def main() -> int:
    failures = 0

    # ----- Go syntax sanity (no compiler available) ----------------------
    print("=== Go syntax sanity ===")
    go_files = sorted(ROOT.rglob("*.go"))
    for p in go_files:
        src = p.read_text()
        ok = src.count("{") == src.count("}") and src.count("(") == src.count(")")
        flag = "OK  " if ok else "FAIL"
        if not ok:
            failures += 1
        print(f"  {flag}  {p}  ({len(src)}B)")
    print(f"  → {len(go_files) - failures}/{len(go_files)} OK")

    # ----- YAML sanity (registers GitLab !reference) ---------------------
    print()
    print("=== YAML sanity ===")
    yaml.SafeLoader.add_constructor("!reference", gitlab_reference)
    yaml_files = sorted(set(list(ROOT.rglob("*.yml")) + list(ROOT.rglob("*.yaml"))))
    yaml_files = [p for p in yaml_files if "node_modules" not in str(p)]
    for p in yaml_files:
        try:
            d = yaml.safe_load(p.read_text())
            top = list(d.keys()) if isinstance(d, dict) else type(d).__name__
            print(f"  OK   {p}  → {top}")
        except Exception as exc:
            failures += 1
            print(f"  FAIL {p}: {exc}")

    # ----- JSON sanity (schema + samples + grafana dashboard) ------------
    print()
    print("=== JSON sanity ===")
    json_files = sorted(ROOT.rglob("*.json"))
    for p in json_files:
        try:
            json.loads(p.read_text())
            print(f"  OK  {p}")
        except Exception as exc:
            failures += 1
            print(f"  FAIL {p}: {exc}")

    # ----- Sample report validation + CSRD smoke -------------------------
    print()
    print("=== Sample reports + CSRD smoke ===")
    for script in ["test/validate_samples.py", "test/csrd_export_smoke.py"]:
        rc = subprocess.call([sys.executable, script])
        if rc != 0:
            failures += 1
            print(f"  FAIL {script} (exit {rc})")

    print()
    if failures:
        print(f"❌ {failures} failures")
        return 1
    print("✅ all checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())

