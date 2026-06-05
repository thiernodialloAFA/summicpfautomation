# AXA Data Science & Software Engineering Summit 2026 — Talk Proposal

## 🎤 Title

**"Build-Time Energy: The Invisible Kilowatts in Your CI — From Code Pipelines to AI Model Deployments"**

## 📝 Abstract

Every pull request you merge, every model you retrain, every agent workflow you orchestrate triggers a CI/CD pipeline — and almost nobody measures its energy cost.

A monorepo rebuilt 50 times a day with cold caches and mutable base images can burn more electricity than the application it ships will consume in a week of production. The pattern is amplified by **AI workloads**: ingestion pipelines that reprocess terabytes on every schema change, ML training jobs that restart from scratch because a dependency hash shifted, agent workflows that spawn dozens of redundant container builds per iteration, and model-deployment pipelines that rebuild multi-GB serving images on every prompt-template tweak.

**CI is the invisible second runtime of your code — and AI engineering makes the bill heavier.**

Concrete reference points:

- Training **GPT-3** consumed an estimated **1 287 MWh** and emitted **~552 t CO₂eq** ([Patterson et al., *Carbon Emissions and Large Neural Network Training*, 2021](https://arxiv.org/abs/2104.10350)).
- Training **BLOOM-176B** consumed **~433 MWh** for **~25 t CO₂eq**, with a further **~10 % overhead from idle / pre-emption / restarts** ([Luccioni et al., 2022](https://arxiv.org/abs/2211.02001)) — exactly the share that better CI/training pipelines can claw back.
- AWS reports that **~90 % of ML lifecycle cost is inference**, but the **build/retrain pipeline** is the part engineers actually control on each commit ([AWS re:Invent 2022 — ML cost optimization](https://aws.amazon.com/blogs/machine-learning/optimizing-mlops-cost/)).
- The **CNCF Environmental Sustainability TAG** flagged CI runners as the largest unmeasured energy line in cloud-native estates ([Cloud Native Sustainability Whitepaper, 2023](https://tag-env-sustainability.cncf.io/publications/cloud-native-sustainability-whitepaper/)).

In this talk, **Olivier and Thierno** expose the hidden energy footprint of build-time infrastructure and show how it compounds in AI-era engineering — with measurements, not slogans.

### What we'll cover

1. **The big picture** — Runner energy ≈ wall-time × TDP × PUE. Same green build, **up to 25× less electricity** when caches, digests and runner choice are correct. Real numbers from AXA pipelines, instrumented with [eco-ci](https://github.com/green-coding-solutions/eco-ci-energy-estimation) (Green Coding Solutions, ex-Green Coding Berlin) and [Cloud Carbon Footprint](https://www.cloudcarbonfootprint.org/docs/methodology/).

2. **Why AI amplifies build-time waste**
   - **Data ingestion pipelines** rebuild on every source change; without incremental processing (CDC, Delta/Iceberg/Hudi), a 2 GB parquet re-ingest costs the same as the initial load.
   - **Model training & fine-tuning** triggered by CI without cache-aware checkpointing restart GPU hours from zero — directly visible in the BLOOM and GPT-3 reported overheads above.
   - **Agent workflows** (LangChain, CrewAI, AutoGen, LangGraph) spawn multiple container builds per agent skill; unpinned dependencies mean cold builds every time.
   - **Model deployment** (MLflow, Seldon Core, KServe, BentoML) rebuilds multi-GB serving images on every config change when Dockerfile layer order is wrong.

3. **Six universal levers, zero functional cost** — Dockerfile layer ordering, **digest pinning** ([OCI Image Spec](https://github.com/opencontainers/image-spec)), dependency caching ([GitHub Actions docs: 30–60 % build-time reduction](https://docs.github.com/en/actions/using-workflows/caching-dependencies-to-speed-up-workflows)), concurrency control, **ARM64 runners**, and **SHA-pinned actions**. Each lever shown on both backend CI and AI/ML pipelines, with measured gains (CI-minutes ÷ 3 to 5).

4. **The AI-specific levers** — Beyond the six universal rules:
   - **Incremental ingestion** (CDC, Delta Lake, Iceberg, Hudi) vs. full-reload pipelines
   - **Checkpoint-aware training** — resume from last epoch, do not restart
   - **Agent skill caching** — pre-built, pinned skill containers instead of on-the-fly builds
   - **Model registry as cache** — promote artifacts, do not rebuild serving images (MLflow / SageMaker Model Registry pattern)
   - **GPU runner right-sizing** — do not allocate A100/H100s for a lint job that precedes training

5. **Automate the measurement** — Live wiring of [eco-ci](https://github.com/green-coding-solutions/eco-ci-energy-estimation), [Kepler](https://sustainable-computing.io/) (eBPF kernel-level energy attribution, CNCF Sandbox project since 2023), and [Cloud Carbon Footprint](https://www.cloudcarbonfootprint.org/) into GitHub Actions / GitLab CI, producing a dashboard that becomes a regression test — for software builds **and** ML pipelines. Output is expressed in the [SCI unit (gCO₂eq / functional unit) — ISO/IEC 21031:2024](https://sci.greensoftware.foundation/).

6. **The decision tree** — A practical, copy-pasteable flowchart to assess any pipeline (backend, data, ML, agent) in under 5 minutes and pick the highest-impact lever first.

### Key takeaways for the audience

| # | Takeaway | Evidence |
|---|----------|----------|
| 1 | CI is the largest **unmeasured** energy line in most engineering orgs. | [CNCF Sustainability Whitepaper, 2023](https://tag-env-sustainability.cncf.io/publications/cloud-native-sustainability-whitepaper/) |
| 2 | AI/ML pipelines compound the problem: GPU time, multi-GB images, frequent retrains, agent sprawl. | Patterson et al. 2021; Luccioni et al. 2022 |
| 3 | Six universal levers can divide CI-minutes by **3–5×** with zero functional regression. | Measured on AXA pipelines + [GitHub Actions caching docs](https://docs.github.com/en/actions/using-workflows/caching-dependencies-to-speed-up-workflows) |
| 4 | AI-specific levers (incremental ingestion, checkpoint resume, skill caching) yield another **2–4×**. | Delta Lake / MLflow benchmarks; reproduced on our pipelines |
| 5 | Measurement tooling exists today (**eco-ci, Kepler, CCF**) — wire it once, leave it running. | All three are OSS, CNCF or independently audited |
| 6 | Every unpinned dependency is **both a security risk and a cache-miss waiting to happen**. | [tj-actions/changed-files supply-chain attack, CVE-2025-30066, March 2025](https://www.cve.org/CVERecord?id=CVE-2025-30066) |

### Why it matters at AXA

AXA operates hundreds of APIs, dozens of ML models, and a growing fleet of GenAI agent workflows. Each deployment triggers CI pipelines. At scale, the invisible kilowatts become visible on the cloud bill **and** on the carbon dashboard required by [CSRD / ESRS E1 reporting](https://www.efrag.org/lab6) (mandatory for large EU companies, including AXA, from FY 2024). This talk gives every team a practical, measured, sourced playbook to cut build-time energy by an observed **60–80 %** across workload types.

## 🏷️ Track

**Quality & Sustainability** | Data & AI / GenAI | DevSecOps & IaC | DevEx & Openness

| Track | Relevance |
|-------|-----------|
| **Quality & Sustainability** | Core — measurable CI energy reduction, SCI-aligned |
| **Data & AI / GenAI** | AI pipeline optimisation, agent workflow efficiency |
| **DevSecOps & IaC** | Supply-chain security (SHA-pinned actions, digest-pinned images) |
| **DevEx & Openness** | OSS tooling (eco-ci, Kepler, CCF), reproducible builds |

## 👥 Speakers

- **Olivier SAILLY** — Staff Engineer, AXA
- **Thierno DIALLO** — Staff Engineer, AXA

## ⏱️ Format

**30 minutes** (25 min presentation + 5 min Q&A) — live demo + data-driven slides with real pipeline measurements.

## 📚 Sources & References

- Patterson, D. et al. — *Carbon Emissions and Large Neural Network Training* (Google, 2021): https://arxiv.org/abs/2104.10350
- Luccioni, A. S. et al. — *Estimating the Carbon Footprint of BLOOM, a 176B Parameter Language Model* (2022): https://arxiv.org/abs/2211.02001
- The Shift Project — *Lean ICT: Towards Digital Sobriety* (2019): https://theshiftproject.org/en/article/lean-ict-our-new-report/
- ADEME × Arcep — *Empreinte environnementale du numérique en France*, volet 2 (mars 2023): https://www.arcep.fr/la-regulation/grands-dossiers-thematiques-transverses/lempreinte-environnementale-du-numerique.html
- CNCF Environmental Sustainability TAG — *Cloud Native Sustainability Whitepaper* (2023): https://tag-env-sustainability.cncf.io/publications/cloud-native-sustainability-whitepaper/
- Cloud Carbon Footprint — methodology: https://www.cloudcarbonfootprint.org/docs/methodology/
- Green Coding Solutions — **eco-ci** energy estimation action: https://github.com/green-coding-solutions/eco-ci-energy-estimation
- **Kepler** — Kubernetes-based Efficient Power Level Exporter (CNCF Sandbox): https://sustainable-computing.io/
- Green Software Foundation — **SCI specification, ISO/IEC 21031:2024**: https://sci.greensoftware.foundation/
- GitHub Docs — *Caching dependencies to speed up workflows*: https://docs.github.com/en/actions/using-workflows/caching-dependencies-to-speed-up-workflows
- GitHub Blog (June 2024) — *Arm64 on GitHub Actions*: https://github.blog/2024-06-03-arm64-on-github-actions-powering-faster-more-efficient-build-systems/
- Docker — BuildKit cache reference: https://docs.docker.com/build/cache/
- OCI Image Specification (digest pinning): https://github.com/opencontainers/image-spec
- Sysdig — *2024 Cloud-Native Security & Usage Report* (mutable-tag prevalence): https://sysdig.com/2024-cloud-native-security-and-usage-report/
- **CVE-2025-30066** — *tj-actions/changed-files* supply-chain compromise (March 2025): https://www.cve.org/CVERecord?id=CVE-2025-30066
- StepSecurity — root-cause analysis of the tj-actions incident: https://www.stepsecurity.io/blog/harden-runner-detection-tj-actions-changed-files-action-is-compromised
- Green Software Foundation — *Patterns Catalog*: https://patterns.greensoftware.foundation/
- EFRAG — **ESRS E1 Climate Change** reporting standard (CSRD): https://www.efrag.org/lab6

---

*Submitted for the AXA Data Science & Software Engineering Summit 2026*
*Companion talk to "There Is No AI Without API — Efficiency Goes Beyond Inference and Prompt Engineering"*
