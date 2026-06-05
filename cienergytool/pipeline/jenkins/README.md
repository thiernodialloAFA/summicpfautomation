# Jenkins integration

cienergy ships a [Jenkins Shared Library](https://www.jenkins.io/doc/book/pipeline/shared-libraries/)
step `cienergyMeasure` that wraps a Pipeline block with energy & carbon
measurement and archives a SCI-compliant `energy-report.json` artifact.

## Files

| Path | Purpose |
|---|---|
| `pipeline/jenkins/vars/cienergyMeasure.groovy` | The shared library step (`vars/` layout) |
| `examples/jenkins/Jenkinsfile`                 | Minimal consumer pipeline |

## Install the shared library

In Jenkins → *Manage Jenkins → System → Global Pipeline Libraries*, add:

| Field | Value |
|---|---|
| Name             | `cienergy` |
| Default version  | `v1.0.0` (or `main`) |
| Retrieval method | *Modern SCM* → Git → `https://github.com/axa-oss/cienergytool.git` |
| Library Path     | `pipeline/jenkins`  *(under "Advanced")* |

Then in any `Jenkinsfile`:

```groovy
@Library('cienergy') _

pipeline {
  agent { label 'linux' }
  stages {
    stage('Build') {
      steps {
        cienergyMeasure(region: 'WE', team: 'claims-platform') {
          sh './build.sh'
          sh './test.sh'
        }
      }
    }
  }
}
```

## Parameters

| Param | Default | Purpose |
|---|---|---|
| `region`                | `'WORLD'` | Electricity Maps zone |
| `cpuModel`              | `'Intel Xeon Platinum 8370C'` | CPU model for the energy model |
| `cpuTdpW`               | `270` | CPU TDP in watts |
| `team`                  | `''` | Written to `metadata.team` |
| `costCenter`            | `''` | Written to `metadata.costCenter` |
| `ingesterUrl`           | `''` | If set, POST to `${url}/v1/runs` |
| `ingesterTokenCred`     | `''` | Jenkins SecretText credential id for the ingester token |
| `electricityMapsTokenCred` | `''` | Jenkins SecretText credential id for the EMAPS API token |
| `aggregatorUrl`         | GitHub release URL | Override to vendor the binary internally |
| `artifactName`          | `'cienergy-report'` | Prefix for the archived artifact |

## Required plugins

- **Pipeline: Shared Groovy Libraries** (`workflow-cps-global-lib`)
- **Pipeline Utility Steps** (`pipeline-utility-steps`)
- **Credentials Binding** (`credentials-binding`) — for `withCredentials`

## What gets produced

| File | Description |
|---|---|
| `.cienergy/energy-report.json` | SCI-compliant report, archived as a build artifact (`fingerprint: true`) |
| `.cienergy/util.jsonl`         | Raw CPU samples (not archived) |
| `.cienergy/steps.jsonl`        | One JSON line per measured step, consumed by the aggregator |

## Limitations (v1.0)

- The step assumes a **Linux agent** with `bash`, `curl`, `awk`, `top`.
  Containerised agents work; minimal images may need `apt-get install procps`.
- The default fetch URL is the GitHub release page. For air-gapped Jenkins
  controllers, mirror the binary internally and pass `aggregatorUrl:`.
- Multi-agent matrix builds: call `cienergyMeasure` inside each `stage` —
  per-stage reports are merged server-side via the ingester (Mode A) or by
  dropping all build artifacts into the embedded dashboard (Mode B).

## Auto-detected metadata

The aggregator's `detectPlatform()` picks up `JENKINS_URL`, `JOB_NAME`,
`BUILD_NUMBER`, `GIT_URL`, `GIT_BRANCH`, `GIT_COMMIT` from the standard
Jenkins environment, so `run.platform = "jenkins"` and the report links back
to the build automatically.

## See also

- [Jenkins Shared Libraries documentation](https://www.jenkins.io/doc/book/pipeline/shared-libraries/)
- [PLAN.md](../../PLAN.md)
- [docs/methodology.md](../../docs/methodology.md)

