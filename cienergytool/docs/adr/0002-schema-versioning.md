# ADR 0002 — JSON schema versioning

## Status

Accepted (2026-06).

## Context

`energy-report.json` is consumed by humans, the embedded dashboard, the Grafana ingester, and third-party tools. Breaking changes must be discoverable, not silent.

## Decision

- The schema is published at a versioned URL: `https://axa-oss.github.io/cienergy/schema/v{MAJOR}.json`.
- Every report includes `specVersion: "MAJOR.MINOR.PATCH"` and `$schema` pointing to the versioned URL.
- **SemVer rules** apply to the schema:
  - **MAJOR** — incompatible change (field removed/renamed, enum value removed).
  - **MINOR** — backward-compatible additive change (new optional field).
  - **PATCH** — documentation / description only.
- Consumers must validate against the schema URL declared by the report, not a hard-coded one.
- We ship a **migrator CLI** (`cienergy migrate`) for each MAJOR bump.

## Consequences

- Old reports remain readable forever; the dashboard ships validators for all MAJOR versions it supports.
- Third parties can pin to a MAJOR and trust additive changes.

