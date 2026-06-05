# cienergy

**Measure the energy consumption and carbon footprint of every CI/CD pipeline
run.** Emit [SCI-compliant](https://sci.greensoftware.foundation/) JSON
(ISO/IEC 21031:2024). Visualise it in either a server-backed Grafana stack or
a zero-dependency embedded HTML/JS/CSS dashboard.

[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://github.com/axa-oss/cienergytool/blob/main/LICENSE)
[![SCI](https://img.shields.io/badge/SCI-ISO%2FIEC%2021031%3A2024-brightgreen)](https://sci.greensoftware.foundation/)
[![SLSA 3](https://slsa.dev/images/gh-badge-level3.svg)](https://slsa.dev/spec/v1.0/levels)

!!! info "What is cienergy?"
    cienergy turns every CI/CD pipeline run into a **measured, signed,
    auditable** SCI report — across GitHub Actions, Azure DevOps, GitLab CI
    and Jenkins. It pairs that with a zero-dependency embedded dashboard
    *and* a server-backed Grafana stack, so adoption never has a "you need
    infra first" friction.

## Five-minute tour

| Layer | What | Where |
|---|---|---|
| **Schema** | One JSON file per run, SCI-compliant | [v1 schema](schema/v1.json) |
| **Probes** | RAPL → Kepler → NVML → eco-ci → CCF → CodeCarbon | [methodology](methodology.md) |
| **CI integrations** | GitHub Actions, Azure DevOps, GitLab CI, Jenkins | see nav → CI/CD integrations |
| **Dashboards** | Embedded (HTML/JS/CSS, no backend) & Grafana stack | see nav → Dashboards |
| **Ingester** | HTTP service, idempotent, OpenAPI documented | see nav → Ingester API |
| **CSRD/ESRS E1 export** | CSV mapped to GHG Protocol Scope 2 + 3.1 | [csrd-mapping.md](csrd-mapping.md) |
| **Supply chain** | cosign-signed binaries + SLSA Level 3 provenance | release notes |

## Why measure CI?

- **CI runners are the largest unmeasured energy line** in cloud-native
  estates ([CNCF Sustainability TAG, 2023](https://tag-env-sustainability.cncf.io/publications/cloud-native-sustainability-whitepaper/)).
- **AI/ML workloads compound the problem**: training large models emits
  hundreds of t CO₂eq, and ~10 % is pure pipeline overhead from restarts
  and idle GPUs ([Luccioni et al., 2022](https://arxiv.org/abs/2211.02001)).
- **CSRD / ESRS E1** is mandatory for AXA (and any large EU company) from
  FY 2024 — see [csrd-mapping.md](csrd-mapping.md).

## Get started

=== "GitHub Actions"

    ```yaml
    - uses: axa-oss/cienergy-action@v1
      with:
        region: US-VA
        electricity-maps-token: ${{ secrets.EMAPS_TOKEN }}
        ingester-url: https://cienergy.example.com
    - run: ./build.sh
    - run: ./test.sh
    ```

=== "Azure DevOps"

    ```yaml
    - template: pipeline/azure-devops/cienergy-step-template.yml@cienergy
      parameters:
        region: WE
        steps:
          - checkout: self
          - script: ./build.sh
          - script: ./test.sh
    ```

=== "GitLab CI"

    ```yaml
    include:
      - project: 'axa-oss/cienergytool'
        file:    '/pipeline/gitlab-ci/cienergy.yml'

    build:
      extends: .cienergy
      image:   ubuntu:22.04
      script:  [ ./build.sh, ./test.sh ]
    ```

=== "Jenkins"

    ```groovy
    @Library('cienergy') _
    pipeline {
      agent { label 'linux' }
      stages {
        stage('Build') {
          steps {
            cienergyMeasure(region: 'WE') {
              sh './build.sh'; sh './test.sh'
            }
          }
        }
      }
    }
    ```

## Open the dashboard

```sh
git clone https://github.com/axa-oss/cienergytool.git
cd cienergytool/dashboard/embedded
python3 -m http.server 8000
# → http://localhost:8000   then click ⭐ Load samples
```

Or run the full Grafana stack:

```sh
cd dashboard/grafana
docker compose up -d --build
# postgres :5432   ingester :8080   grafana :3000
make seed-samples
```

## Standards

- [Software Carbon Intensity (SCI) — ISO/IEC 21031:2024](https://sci.greensoftware.foundation/)
- [GHG Protocol Corporate Standard](https://ghgprotocol.org/corporate-standard) + [Scope 2 Guidance (2015)](https://ghgprotocol.org/scope_2_guidance)
- [EFRAG ESRS E1 — Climate Change (CSRD)](https://www.efrag.org/lab6)
- [OpenTelemetry semantic conventions for sustainability (draft)](https://github.com/open-telemetry/semantic-conventions/issues/1129)
- [SLSA v1.0 — Level 3](https://slsa.dev/spec/v1.0/levels)

