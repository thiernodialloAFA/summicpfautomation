# AXA Data Science & Software Engineering Summit 2026 — Talk Proposal

## 🎤 Title

**"There Is No AI Without API — Efficiency Goes Beyond Inference and Prompt Engineering"**

## 📝 Abstract

The industry is racing to optimise AI models, shrink inference costs, and fine-tune prompts.
But here is the blind spot: **every AI capability is exposed and consumed through an API**.
If the API layer is wasteful — no pagination, no caching, no compression, bloated payloads — energy and budget are burned long before a single token is generated.

Two data points frame the problem:

- The ICT sector represents an estimated **3–4 % of global greenhouse-gas emissions** and is growing **~6 % per year** ([The Shift Project, *Lean ICT*, 2019](https://theshiftproject.org/en/article/lean-ict-our-new-report/); [ADEME × Arcep joint study, March 2023](https://www.arcep.fr/la-regulation/grands-dossiers-thematiques-transverses/lempreinte-environnementale-du-numerique.html)).
- HTTP/JSON APIs dominate east-west cloud traffic, and the median JSON response observed in the wild is **40–60 % compressible** ([HTTP Archive — *Web Almanac 2024*, Compression chapter](https://almanac.httparchive.org/en/2024/compression)). Every uncompressed, unpaginated response is measurable waste.

In this talk, **Olivier and Thierno** present a fully automated approach to **Green API scoring** and **eco-design analysis** that plugs directly into the development lifecycle:

1. **APIGreenScore** — a **123-criteria** open framework ([Collectif Numérique Responsable / APIdays](https://github.com/cnumr/APIGreenScore)) that measures API sustainability across design, build and runtime (pagination, ETag/304, gzip/Brotli, field filtering, delta sync, binary formats, rate limiting, payload-size budgets…).
2. **Creedengo** (ex-*ecoCode*, [Green Code Initiative](https://github.com/green-code-initiative)) — static eco-design rules for **Java, .NET, Python, Android and Rust**, packaged as SonarQube plugins, that catch energy-wasting patterns (N+1 queries, string concatenation in loops, `SELECT *`, unclosed resources, unbounded streams…) before they ship.
3. **Shift-left automation** — an offline OpenAPI linter that runs in the IDE, in pre-commit hooks, and as a CI gate, so every design change is scored *before* merge. Built on [Spectral](https://stoplight.io/open-source/spectral) rulesets aligned with the [GR491 sustainable-design reference](https://gr491.isit-europe.org/) (491 criteria, Institut du Numérique Responsable).
4. **AI agents with the right skills** — we embed APIGreenScore and Creedengo rules as **GitHub Copilot custom instructions** ([GA since July 2024](https://github.blog/2024-07-25-github-copilot-now-supports-custom-instructions/)) and as [**Model Context Protocol**](https://modelcontextprotocol.io/) (MCP) tools. The AI then *natively* produces efficient, low-footprint APIs. **The right agent with the right skills beats a better prompt.**

### Key takeaways for the audience

| # | Takeaway | Evidence |
|---|----------|----------|
| 1 | API design choices have a **measurable** energy and network cost — we show the live score on a real endpoint. | APIGreenScore v2 reference implementation |
| 2 | Wiring scoring into CI/CD (`build → score → badge → dashboard`) turns sustainability into a **continuous regression metric**, not an annual audit. | Aligned with the [Software Carbon Intensity (SCI) specification — ISO/IEC 21031:2024](https://sci.greensoftware.foundation/) |
| 3 | Embedding eco-design rules as **agent skills** (Copilot instructions, MCP tools) makes AI-generated code green *by default*. | [GitHub Blog, 2024 — custom instructions reduce style/security defects](https://github.blog/2024-07-25-github-copilot-now-supports-custom-instructions/) |
| 4 | The toolchain is **stack-agnostic** (Java, .NET, Python) and integrates with SonarQube, Spectral, GitHub Actions and GitLab CI. | OSS, MIT / Apache-2 licensed |
| 5 | Live demo: API scoring, automated badge, AI-assisted refactor guided by Creedengo — **before vs. after** in numbers (payload size, latency, energy proxy). | Demo on a public mock API |

### Why it matters at AXA

- **Quality & Sustainability** — measurable eco-design enforced every sprint, aligned with AXA’s public climate commitments ([AXA Climate Strategy](https://www.axa.com/en/about-us/climate-strategy)).
- **DevSecOps & IaC** — pipeline-native scoring and threshold gates; the same gate also flags unbounded payloads, a known abuse vector ([OWASP API Security Top 10 — API4:2023 Unrestricted Resource Consumption](https://owasp.org/API-Security/editions/2023/en/0xa4-unrestricted-resource-consumption/)).
- **GenAI & DevEx** — proves that the most efficient AI code generation comes from *domain-aware agents*, not bigger models.
- **Integration & APIs** — directly impacts every team exposing or consuming REST/GraphQL APIs.

## 🏷️ Track

**Quality & Sustainability** | Integration & APIs | GenAI & DevEx

## 👥 Speakers

- **Olivier SAILLY** — Staff Engineer, AXA
- **Thierno DIALLO** — Staff Engineer, AXA

## ⏱️ Format

30-minute talk + 10-minute live demo

## 📚 Sources & References

- The Shift Project — *Lean ICT: Towards Digital Sobriety* (2019): https://theshiftproject.org/en/article/lean-ict-our-new-report/
- ADEME × Arcep — *Évaluation de l’impact environnemental du numérique en France*, volet 2 (mars 2023): https://www.arcep.fr/la-regulation/grands-dossiers-thematiques-transverses/lempreinte-environnementale-du-numerique.html
- APIGreenScore framework (123 criteria, CNumR): https://github.com/cnumr/APIGreenScore
- Green Code Initiative — **Creedengo** (ex-ecoCode) rules: https://github.com/green-code-initiative
- Institut du Numérique Responsable — **GR491** (491 sustainable-design criteria): https://gr491.isit-europe.org/
- Green Software Foundation — **Software Carbon Intensity (SCI)** specification, now **ISO/IEC 21031:2024**: https://sci.greensoftware.foundation/
- OWASP API Security Top 10, 2023 edition: https://owasp.org/API-Security/editions/2023/en/
- HTTP Archive — *Web Almanac 2024*, Compression chapter: https://almanac.httparchive.org/en/2024/compression
- GitHub Copilot — custom instructions (GA, July 2024): https://github.blog/2024-07-25-github-copilot-now-supports-custom-instructions/
- Model Context Protocol specification (Anthropic, 2024): https://modelcontextprotocol.io/

---

> *"Optimising the prompt is the last mile. Optimising the API is the whole highway."*
