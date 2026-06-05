# GitLab CI integration

cienergy ships an `include`-able job template that turns any GitLab CI job
into an instrumented one. The template downloads the aggregator binary,
samples CPU utilisation in the background, and on `after_script` emits the
SCI-compliant `energy-report.json` artifact plus an optional push to the
ingester.

## Files

| Path | Purpose |
|---|---|
| `pipeline/gitlab-ci/cienergy.yml`     | `extends:`-able job template + reusable script fragments |
| `examples/gitlab-ci/.gitlab-ci.yml`   | Minimal consumer pipeline |

## Quick start

```yaml
include:
  - project: 'axa-oss/cienergytool'
    ref:     'v1.0.0'
    file:    '/pipeline/gitlab-ci/cienergy.yml'

build:
  extends: .cienergy
  image: ubuntu:22.04
  script:
    - ./build.sh
  variables:
    CIENERGY_REGION: 'WE'
    CIENERGY_TEAM:   'claims-platform'
```

## Variables

| Variable | Default | Purpose |
|---|---|---|
| `CIENERGY_REGION`         | `WORLD` | Electricity Maps zone (e.g. `WE`, `FR`, `US-VA`) |
| `CIENERGY_CPU_MODEL`      | `Intel Xeon Platinum 8370C` | CPU model for the eco-ci energy model |
| `CIENERGY_CPU_TDP_W`      | `270`   | CPU TDP in watts |
| `CIENERGY_TEAM`           | *(empty)* | Written to `metadata.team` |
| `CIENERGY_COST_CENTER`    | *(empty)* | Written to `metadata.costCenter` |
| `CIENERGY_INGESTER_URL`   | *(empty)* | If set, POST the report to `${url}/v1/runs` |
| `CIENERGY_INGESTER_TOKEN` | *(empty)* | Bearer token for the ingester (mark as masked) |
| `CIENERGY_AGGREGATOR_URL` | GitHub release URL | Override to vendor the binary internally |

The aggregator picks up GitLab metadata from the standard predefined variables (see [GitLab docs](https://docs.gitlab.com/ee/ci/variables/predefined_variables.html)):

| GitLab variable | Maps to (report field) |
|---|---|
| `CI_PIPELINE_ID`       | `run.id` |
| `CI_PROJECT_PATH`      | `run.repository` |
| `CI_PIPELINE_NAME` / `CI_JOB_NAME` | `run.workflow` |
| `CI_COMMIT_REF_NAME`   | `run.ref` |
| `CI_COMMIT_SHA`        | `run.commitSha` |
| `GITLAB_CI`            | platform auto-detection trigger → `run.platform = "gitlab-ci"` |

## Self-hosted runners

If your runners have privileged access to `/sys/class/powercap/intel-rapl:*`
(common for shell or `privileged: true` podman executors), the aggregator
will switch to **RAPL hardware measurement** instead of the eco-ci model —
no config change needed; the source is recorded in
`energy.byStep[].source` for audit.

## Limitations (v1.0)

- The bundled sampler shells out to `top`. On minimal Alpine images, add
  `procps` to your `image:` or your `before_script`.
- The `.cienergy` template is a job-level extension — for multi-job pipelines,
  apply `extends: .cienergy` to each job you want to instrument.
- Multi-stage aggregation across jobs happens server-side in the ingester
  (Mode A) or in the embedded dashboard (Mode B) by dropping all artifacts.

## See also

- [GitLab Docs — `include`](https://docs.gitlab.com/ee/ci/yaml/includes.html)
- [GitLab Docs — Predefined CI/CD variables](https://docs.gitlab.com/ee/ci/variables/predefined_variables.html)
- [PLAN.md](../../PLAN.md)
- [docs/methodology.md](../../docs/methodology.md)

