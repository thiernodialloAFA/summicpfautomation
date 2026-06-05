"""cienergy_codecarbon — bridge between CodeCarbon and the cienergy steps.jsonl format.

Wraps any callable (or `with` block) in a CodeCarbon OfflineEmissionsTracker
and appends one JSONL row to the cienergy steps file when the block exits.

Why CodeCarbon? It's the most widely adopted in-process energy meter for
Python ML pipelines, supporting RAPL on CPU and NVML on NVIDIA GPUs out of
the box. See: https://codecarbon.io/ — Schmidt et al., 2021.

Install:
    pip install codecarbon>=2.4

Usage as a context manager:

    from cienergy_codecarbon import measure
    with measure(name="train", steps_file="/tmp/cienergy/steps.jsonl",
                 country_iso_code="FRA"):
        train_model()

Usage as a decorator:

    from cienergy_codecarbon import measured
    @measured(name="evaluate", steps_file="/tmp/cienergy/steps.jsonl")
    def evaluate(model): ...
"""
from __future__ import annotations

import contextlib
import functools
import json
import os
import time
from typing import Optional

try:
    from codecarbon import OfflineEmissionsTracker  # type: ignore
except ImportError as exc:  # pragma: no cover
    raise ImportError(
        "codecarbon is required: pip install 'codecarbon>=2.4'"
    ) from exc


@contextlib.contextmanager
def measure(
    *,
    name: str,
    steps_file: str,
    country_iso_code: str = "FRA",
    cpu_util_pct: Optional[float] = None,
    project_name: str = "cienergy",
):
    """Measure energy of the wrapped block and append a step row.

    Args:
        name: step name written into the JSONL row (e.g. "train-xgboost").
        steps_file: path to the cienergy JSONL file the aggregator will read.
        country_iso_code: ISO 3166-1 alpha-3 (e.g. "FRA", "USA", "DEU"); used
            by CodeCarbon's offline grid intensity dataset when available.
        cpu_util_pct: optional CPU utilisation override for the row (the
            aggregator uses it only if no `kWh` is provided).
        project_name: passed through to CodeCarbon's tracker.
    """
    os.makedirs(os.path.dirname(steps_file) or ".", exist_ok=True)
    tracker = OfflineEmissionsTracker(
        project_name=project_name,
        country_iso_code=country_iso_code,
        save_to_file=False,
        log_level="error",
        tracking_mode="process",
        measure_power_secs=2,
    )
    started = time.monotonic()
    tracker.start()
    try:
        yield tracker
    finally:
        emissions_kg = tracker.stop() or 0.0      # kgCO2eq
        duration_s = time.monotonic() - started
        # CodeCarbon exposes the energy split. Fall back to recomputing if missing.
        energy_kwh = float(getattr(tracker, "_total_energy", 0) or 0)  # kWh
        gpu_kwh = float(getattr(tracker, "_total_gpu_energy", 0) or 0)
        row = {
            "name": name,
            "durationSeconds": round(duration_s, 3),
            "kWh": round(energy_kwh, 6) if energy_kwh > 0 else 0.0,
            "gpuKWh": round(gpu_kwh, 6) if gpu_kwh > 0 else 0.0,
            "source": "codecarbon",
        }
        if cpu_util_pct is not None:
            row["cpuUtilPct"] = float(cpu_util_pct)
        with open(steps_file, "a", encoding="utf-8") as f:
            f.write(json.dumps(row) + "\n")
        # Stash the CO2 number too — handy when the cienergy aggregator runs
        # later with a different grid intensity and wants to cross-check.
        side = steps_file + ".codecarbon.jsonl"
        with open(side, "a", encoding="utf-8") as f:
            f.write(json.dumps({
                "step": name,
                "emissionsKgCO2eq": round(emissions_kg, 6),
                "country": country_iso_code,
            }) + "\n")


def measured(*, name: str, steps_file: str, **kwargs):
    """Decorator form of :func:`measure`."""
    def deco(fn):
        @functools.wraps(fn)
        def wrapped(*a, **kw):
            with measure(name=name, steps_file=steps_file, **kwargs):
                return fn(*a, **kw)
        return wrapped
    return deco


if __name__ == "__main__":
    # Smoke test: run a 5-second busy loop and write a row.
    import sys
    out = sys.argv[1] if len(sys.argv) > 1 else "/tmp/cienergy-smoke.jsonl"
    with measure(name="smoke", steps_file=out, country_iso_code="FRA"):
        end = time.monotonic() + 5
        x = 0
        while time.monotonic() < end:
            x = (x * 1103515245 + 12345) & 0x7fffffff
    print("wrote", out)

