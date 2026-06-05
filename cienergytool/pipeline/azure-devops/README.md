# Azure DevOps integration

cienergy ships a reusable YAML steps template that wraps any Azure DevOps job
with energy / carbon measurement and publishes a SCI-compliant
`energy-report.json` (plus the embedded HTML dashboard) as a pipeline artifact.

## Files

| Path | Purpose |
|---|---|
| `pipeline/azure-devops/cienergy-step-template.yml` | Reusable template (pre/post job hooks + publish artifact) |
| `examples/azure-devops/sample-pipeline.yml`        | Minimal consumer pipeline showing how to use it |

## How it works

The template inserts **5 steps**:

1. `cienergy · start` — records the start timestamp and spawns a 2 s CPU sampler.
2. `cienergy · install aggregator` — downloads (or builds) the `cienergy-aggregator` binary.
3. *(your build/test/deploy steps go between the template invocation and the aggregator call)*
4. `cienergy · aggregate` — kills the sampler, computes average CPU utilisation, runs the aggregator and writes `energy-report.json`. Runs with `condition: always()` so you always get a measurement, even on failure.
5. `cienergy · attach dashboard` — copies the embedded HTML dashboard pre-loaded with the run.
6. `cienergy · publish artifact` — uploads everything via `PublishPipelineArtifact@1`.

The aggregator picks up Azure DevOps metadata from the standard predefined variables (see [Microsoft docs](https://learn.microsoft.com/azure/devops/pipelines/build/variables)):

| ADO variable | Maps to (report field) |
|---|---|
| `BUILD_BUILDID`           | `run.id` |
| `BUILD_REPOSITORY_NAME`   | `run.repository` |
| `BUILD_DEFINITIONNAME`    | `run.workflow` |
| `BUILD_SOURCEBRANCH`      | `run.ref` |
| `BUILD_SOURCEVERSION`     | `run.commitSha` |
| `AGENT_OS`                | `runner.os` |
| `AGENT_OSARCHITECTURE`    | `runner.arch` |
| `TF_BUILD`                | platform auto-detection trigger → `run.platform = "azure-devops"` |

## Quick start

1. **Create a service connection** in your Azure DevOps project pointing at the
   GitHub `axa-oss/cienergytool` repository (or mirror it into your own ADO Git
   and reference it as `type: git`).
2. **Add a variable group** named `cienergy-secrets` containing a secret
   variable `EMAPS_TOKEN` with your [Electricity Maps](https://www.electricitymaps.com/) API token (optional —
   without it, the tool falls back to bundled Ember monthly averages).
3. **Copy** `examples/azure-devops/sample-pipeline.yml` into your repo at
   `.azure-pipelines/ci.yml` and adapt the build/test steps.
4. **Run the pipeline.** After completion, download the
   `cienergy-report-<BuildId>` artifact and open `dashboard/index.html` — the
   embedded dashboard will load with the run pre-selected.

## Where to put the template invocation

The template invocation must be placed **before** your build/test/deploy steps
so its pre-job hooks fire first. The post-job aggregation steps are part of the
same template and run automatically after — they use `condition: always()` so a
failed build still produces a report.

```yaml
steps:
  # pre-job hooks (start, install)
  - template: pipeline/azure-devops/cienergy-step-template.yml@cienergy
    parameters: { region: 'WE', team: 'claims-platform' }

  # your work
  - script: ./build.sh
  - script: ./test.sh

  # post-job hooks (aggregate, attach dashboard, publish) are appended
  # automatically by the template — no extra invocation needed.
```

## Self-hosted agents

If you run [self-hosted agents](https://learn.microsoft.com/azure/devops/pipelines/agents/agents) on Linux with privileged access to
`/sys/class/powercap`, you get **RAPL** measurements instead of the eco-ci CPU
model — set the runner-class label in metadata and the aggregator will pick the
better source automatically.

For Windows / macOS self-hosted agents, the eco-ci model is used (no RAPL on
those platforms in v1.0).

## Limitations (v1.0)

- The bundled sampler shells out to `top`; on minimal agent images, install `procps`
  or replace the template with the Go collector when it ships (Phase 1.x).
- GPU measurement (NVML) requires self-hosted agents with NVIDIA drivers and
  `nvidia-smi` available — same constraint as the GitHub Action path.
- Multi-stage / matrix builds: the template runs **per job** — aggregating across
  jobs is done by the Grafana ingester (Mode A) or by dropping all artifacts
  into the embedded dashboard (Mode B).

## See also

- [PLAN.md](../../PLAN.md) — full project plan
- [docs/methodology.md](../../docs/methodology.md) — SCI formula and probe priority
- Microsoft Learn — [Define and use templates](https://learn.microsoft.com/azure/devops/pipelines/process/templates)
- Microsoft Learn — [Predefined variables](https://learn.microsoft.com/azure/devops/pipelines/build/variables)

