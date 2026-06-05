# ADR 0001 — Energy source priority

## Status

Accepted (2026-06).

## Context

Multiple probes can measure or estimate CI runner energy: RAPL, Kepler (eBPF), NVML, the eco-ci CPU-utilisation model, and the Cloud Carbon Footprint coefficient model. Each has different availability constraints and accuracy.

## Decision

We pick the **best source available per step**, in this fixed priority order:

1. RAPL (`/sys/class/powercap/intel-rapl:*/energy_uj`) — hardware counter, gold standard.
2. Kepler — eBPF kernel-level attribution, accurate on Kubernetes.
3. NVML / DCGM — for GPU steps, complements CPU probe.
4. eco-ci model — `kWh = TDP × util × time`, the only viable option on hosted runners.
5. Cloud Carbon Footprint coefficients — last-resort fallback per cloud region.

The selected source is always written to `energy.byStep[].source` for auditability.

## Consequences

- Numbers are comparable **within** a runner type but not always **across** runner types — we expose `source` to make that visible.
- Hosted GitHub runners always end up at priority 4 → we calibrate the eco-ci model against RAPL on self-hosted runners and document the delta.

