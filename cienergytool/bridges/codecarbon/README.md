# CodeCarbon bridge

In-process energy meter for **Python ML pipelines**, powered by
[CodeCarbon](https://codecarbon.io/) (Schmidt et al., 2021 — used by Hugging
Face, AllenNLP, MLflow plug-ins, and the BigScience BLOOM training).

CodeCarbon measures **CPU energy via RAPL** and **GPU energy via NVML** at
the process level, which is exactly the granularity ML engineers want when
profiling a specific training, fine-tuning, or evaluation step.

## Why a bridge?

The cienergy aggregator reads a single JSONL file where each line is a
`stepSample`. The bridge wraps a Python block in CodeCarbon's
`OfflineEmissionsTracker`, then appends one cienergy-shaped row when the
block exits — so the same aggregator handles classic CI (eco-ci model) and
ML jobs (CodeCarbon) without special-casing.

## Install

```sh
pip install 'codecarbon>=2.4'
```

Pure Python, MIT licence. Works on Linux (RAPL), macOS, Windows. GPU energy
requires NVIDIA + NVML.

## Use it as a context manager

```python
from cienergy_codecarbon import measure

STEPS = "/tmp/cienergy/steps.jsonl"   # same file the aggregator reads

with measure(name="ingest-features", steps_file=STEPS, country_iso_code="FRA"):
    df = fetch_features(start, end)

with measure(name="train-xgboost",   steps_file=STEPS, country_iso_code="FRA"):
    model.fit(df, y)

with measure(name="evaluate",        steps_file=STEPS, country_iso_code="FRA"):
    metrics = evaluate(model, df_test)
```

After the run, hand the JSONL to the cienergy aggregator:

```sh
cienergy-aggregator \
  --start "$START" --end "$(date -u +%FT%TZ)" \
  --steps-file /tmp/cienergy/steps.jsonl \
  --region FR \
  --workflow train-xgboost.yml \
  --repo axa/risk-models \
  --out energy-report.json
```

## Decorator form

```python
from cienergy_codecarbon import measured

@measured(name="train-xgboost", steps_file="/tmp/cienergy/steps.jsonl",
          country_iso_code="FRA")
def train(df, y):
    return model.fit(df, y)
```

## What gets written

Per `measure(...)` invocation, **one line** appended to `steps_file`:

```json
{"name":"train-xgboost","durationSeconds":7184.2,
 "kWh":1.62,"gpuKWh":1.54,"source":"codecarbon"}
```

Plus a sidecar `steps.jsonl.codecarbon.jsonl` with CodeCarbon's own
gCO2eq estimate, so you can cross-check against the cienergy aggregator's
output (which uses Electricity Maps grid intensity, possibly different from
CodeCarbon's bundled one).

## Why offline tracker?

`OfflineEmissionsTracker` uses a **bundled grid-intensity dataset** and does
no outbound HTTP — safer for air-gapped runners and CI. The cienergy
aggregator then re-applies the **fresh** Electricity Maps value (if a token
is configured), so the final report reflects current grid conditions and
not CodeCarbon's snapshot.

## Country codes

ISO 3166-1 **alpha-3** (FRA, USA, DEU, GBR, NLD, …). See the CodeCarbon
[data file](https://github.com/mlco2/codecarbon/blob/master/codecarbon/data/private_infra/2016/global_energy_mix.json).

## Smoke test

```sh
python -m bridges.codecarbon.cienergy_codecarbon /tmp/smoke.jsonl
cat /tmp/smoke.jsonl
```

## References

- CodeCarbon repo & paper — https://codecarbon.io/, https://github.com/mlco2/codecarbon
- Schmidt et al., *CodeCarbon: estimate and track carbon emissions from your computing*, 2021
- Luccioni et al., *Power Hungry Processing: ⚡Watts⚡ Driving the Cost of AI Deployment?*, 2024 — https://arxiv.org/abs/2311.16863
- ML CO2 Impact calculator — https://mlco2.github.io/impact/

