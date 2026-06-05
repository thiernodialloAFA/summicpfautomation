#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# scripts/setup-github-roadmap.sh
#
# Idempotently materialises the README roadmap into the GitHub repository:
#   • status labels   (status:shipped, status:in-progress, status:planned, status:idea)
#   • lane  labels    (lane:v1, lane:v1.x, lane:v2)
#   • routing label   (roadmap — used by the README "open issue" link)
#   • milestones      (v1 — Q1–Q2 2026, v1.x — Q3 2026, v2 — 2026-Q4 → 2027)
#   • issues          (one per v1.x / v2 line item, tagged with status + lane,
#                      pre-assigned to the matching milestone)
#
# Re-runs are safe: existing labels/milestones/issues are skipped (matched by name/title).
#
# Usage:
#   ./scripts/setup-github-roadmap.sh                  # apply to the current `gh repo set-default` repo
#   ./scripts/setup-github-roadmap.sh --repo owner/r   # explicit repo
#   ./scripts/setup-github-roadmap.sh --dry-run        # show what would be created, write nothing
#   CLOSE_SHIPPED=1 ./scripts/setup-github-roadmap.sh  # also create+close issues for v1 items
#
# Requires: gh ≥ 2.20, jq, bash 3.2+.
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

REPO=""
DRY_RUN=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)     REPO="$2"; shift 2 ;;
    --dry-run)  DRY_RUN=1; shift ;;
    -h|--help)  sed -n '2,28p' "$0"; exit 0 ;;
    *)          echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

# ── Pre-flight ──────────────────────────────────────────────────────────────
command -v gh >/dev/null || { echo "✗ gh CLI not found — install from https://cli.github.com/" >&2; exit 1; }
command -v jq >/dev/null || { echo "✗ jq not found" >&2; exit 1; }
gh auth status >/dev/null 2>&1 || { echo "✗ not logged in — run 'gh auth login'" >&2; exit 1; }

if [[ -z "$REPO" ]]; then
  REPO="$(gh repo view --json nameWithOwner -q .nameWithOwner)"
fi
echo "▸ target repo: ${REPO}"
echo "▸ dry-run:     $([[ $DRY_RUN -eq 1 ]] && echo yes || echo no)"
echo

run() {  # run <description> -- <cmd…>
  local desc="$1"; shift
  [[ "$1" == "--" ]] && shift
  if [[ $DRY_RUN -eq 1 ]]; then
    printf "  DRY  %-60s %s\n" "$desc" "$*"
  else
    if "$@" >/tmp/gh-roadmap.out 2>&1; then
      printf "  ✓    %-60s\n" "$desc"
    else
      # gh returns non-zero for "already exists" — we treat those as ok.
      if grep -qiE 'already_exists|already exists|name has already been taken' /tmp/gh-roadmap.out; then
        printf "  ↻    %-60s (exists, skipped)\n" "$desc"
      else
        printf "  ✗    %-60s\n" "$desc"
        sed 's/^/       /' /tmp/gh-roadmap.out
        return 1
      fi
    fi
  fi
}

# ── Labels ──────────────────────────────────────────────────────────────────
# Format: name|color (hex w/o #)|description
LABELS=(
  "roadmap|0E8A16|Roadmap item — surfaced from README.md"
  "status:shipped|2EA043|Capability has shipped (v1)"
  "status:in-progress|FBCA04|Active work — v1.x lane"
  "status:planned|0366D6|Planned — v2 lane"
  "status:idea|D4C5F9|RFC / idea, not committed yet"
  "lane:v1|2EA043|v1 — Q1–Q2 2026"
  "lane:v1.x|FBCA04|v1.x — Q3 2026"
  "lane:v2|0366D6|v2 — Q4 2026 → 2027"
)
echo "── Labels ──────────────────────────────────────────────────────────────"
for spec in "${LABELS[@]}"; do
  IFS='|' read -r name color desc <<< "$spec"
  run "label '${name}'" -- \
    gh label create "$name" --color "$color" --description "$desc" --repo "$REPO" --force
done
echo

# ── Milestones ──────────────────────────────────────────────────────────────
# Format: title|due (YYYY-MM-DD)|description
MILESTONES=(
  "v1|2026-06-30|Shipped — Q1–Q2 2026 (foundations through GitHub Pages deploy)"
  "v1.x|2026-09-30|In progress — Q3 2026 (GitLab, Jenkins, Tekton, OCI, gate, auto-PRs)"
  "v2|2027-03-31|Planned — Q4 2026 → 2027 (live grid, multi-tenant, APIGreenScore bridge, …)"
)
echo "── Milestones ──────────────────────────────────────────────────────────"
# gh has no first-class `milestone create`, so we go through the REST API.
existing_milestones="$(gh api "repos/${REPO}/milestones?state=all&per_page=100" -q '.[].title' 2>/dev/null || true)"
for spec in "${MILESTONES[@]}"; do
  IFS='|' read -r title due desc <<< "$spec"
  if grep -Fxq "$title" <<< "$existing_milestones"; then
    printf "  ↻    %-60s (exists, skipped)\n" "milestone '${title}'"
    continue
  fi
  run "milestone '${title}' (due ${due})" -- \
    gh api "repos/${REPO}/milestones" \
      --method POST \
      --field "title=${title}" \
      --field "description=${desc}" \
      --field "due_on=${due}T23:59:59Z"
done
echo

# Build a {title → number} map so we can attach issues to milestones.
declare_milestone_numbers() {
  if [[ $DRY_RUN -eq 1 ]]; then
    MS_V1=0; MS_V1X=0; MS_V2=0; return
  fi
  local json
  json="$(gh api "repos/${REPO}/milestones?state=all&per_page=100")"
  MS_V1=$(jq -r '.[] | select(.title=="v1")   | .number' <<<"$json"  | head -n1)
  MS_V1X=$(jq -r '.[] | select(.title=="v1.x") | .number' <<<"$json" | head -n1)
  MS_V2=$(jq -r '.[] | select(.title=="v2")   | .number' <<<"$json"  | head -n1)
}
declare_milestone_numbers

# ── Issues ──────────────────────────────────────────────────────────────────
# Format: lane|title|body|extra-labels
# (status + lane labels are added automatically below — list only extras here)
V1X_ISSUES=(
  "v1.x|[v1.x] GitLab CI include template|Mirror the Azure DevOps wrapper pattern: a reusable \`include\` template that any GitLab project can pull. Surface kWh / SCI / suggestions in the MR widget via a GitLab Reports CI artefact.|integration,gitlab"
  "v1.x|[v1.x] Jenkins shared library (cienergyStep)|Expose a Groovy step \`cienergyStep { … }\` that wraps any declarative or scripted Jenkins pipeline and uploads a SCI report as a build artefact.|integration,jenkins"
  "v1.x|[v1.x] Tekton Task / Pipeline definitions|Provide a Tekton \`Task\` and example \`Pipeline\` so OpenShift Pipelines / CD-Foundation users can adopt the same SCI model.|integration,tekton"
  "v1.x|[v1.x] Publish an OCI image of the aggregator|Push \`ghcr.io/thiernodialloAFA/cienergy-aggregator:v1.x\` so users can wire the tool via \`image:\`-style integrations (Tekton, Argo, GitLab \`image:\`).|packaging,supply-chain"
  "v1.x|[v1.x] cienergy-gate per-step budget|Fail or warn the build above a configurable kWh or gCO₂eq budget defined in \`cienergy.yaml\`. Exit codes: 0 ok, 1 warn, 2 fail.|gating,policy"
  "v1.x|[v1.x] Auto-PR for deterministic suggestions|When a suggestion is deterministic (e.g. switch \`ubuntu-latest\` → \`ubuntu-22.04-arm\`, reorder Dockerfile layers), open a PR with the fix.|suggestions,automation"
)
V2_ISSUES=(
  "v2|[v2] Streaming Electricity Maps grid feed|Switch from polled REST to WebSocket for sub-hour grid-intensity resolution. Cache locally to survive disconnects.|grid"
  "v2|[v2] Per-cloud PUE catalogue|Curate a per-cloud / per-region PUE table (AWS, Azure, GCP, OVH, Scaleway) feeding the Scope-2 calc. Source the values from each provider's sustainability disclosure.|methodology"
  "v2|[v2] Multi-tenant ingester (OIDC + transparency log)|Add OIDC auth, per-org quotas and signed report receipts published to a transparency log (Rekor-style).|ingester,security"
  "v2|[v2] APIGreenScore × cienergy SCI bridge|Join the cienergy SCI score with the 123-criteria APIGreenScore (https://github.com/cnumr/APIGreenScore) into one unified eco-score per release. Connects talks #1 and #2.|bridges,scoring"
  "v2|[v2] AppCAT plug-in for Java/.NET upgrades|Surface cienergy suggestions inside Microsoft's App Modernization CAT (https://learn.microsoft.com/azure/migrate/appcat/) so Java/.NET upgrades can pull in build-time energy fixes.|bridges,appcat,idea"
  "v2|[v2] VS Code extension — inline SCI lens|Show kWh / gCO₂eq above each job in \`.github/workflows/*.yml\` with one-click fix suggestions.|tooling,vscode,idea"
  "v2|[v2] /eco-cost GitHub bot|Comment on every PR with the energy / carbon delta vs. \`main\`. Honour the budget set in \`cienergy.yaml\`.|bot,idea"
)
V1_SHIPPED=(  # only created when CLOSE_SHIPPED=1
  "v1|[v1] Phase 0 — Foundations|Go module layout, SCI JSON schema v1.0.0, sample reports, CI matrix.|"
  "v1|[v1] Phase 1 — MVP collector|cienergy-aggregator: 3-tier source priority (eco-ci → RAPL → CPU+TDP), Boavizta embodied carbon, Electricity Maps + Ember 2024 grid intensity.|"
  "v1|[v1] Phase 2 — GitHub Action|Composite action wrapping any job; dogfooded by this repo's own CI.|"
  "v1|[v1] Phase 3 — Storage & dashboards|cienergy-ingester (REST + Postgres), Grafana / Prometheus / OTLP stack, zero-dep embedded HTML dashboard.|"
  "v1|[v1] Phase 4 — AI/ML extensions|cienergy-gpu-probe (NVML / nvidia-smi) and bridges/codecarbon adapter.|"
  "v1|[v1] Phase 5a — Azure DevOps template|pipeline/azure-devops/cienergy-step-template.yml.|"
  "v1|[v1] Phase 5b — CSRD / ESRS E1 exporter|cienergy-csrd-export CSV roll-up (Scope 2 + Scope 3.1).|"
  "v1|[v1] Phase 5c — SLSA-3 release|Reproducible cross-platform binaries, cosign keyless, SLSA Level 3 provenance.|"
  "v1|[v1] Phase 5d — GitHub Pages deploy|Dashboard & MkDocs site auto-published; weekly cron refresh.|"
)

create_issue() {  # lane title body extra-labels [--close]
  local lane="$1" title="$2" body="$3" extras="$4" close_flag="${5:-}"
  # Status label per lane
  local status_label lane_label milestone_num
  case "$lane" in
    v1)    status_label="status:shipped";     lane_label="lane:v1";   milestone_num="$MS_V1"  ;;
    v1.x)  status_label="status:in-progress"; lane_label="lane:v1.x"; milestone_num="$MS_V1X" ;;
    v2)    status_label="status:planned";     lane_label="lane:v2";   milestone_num="$MS_V2"  ;;
    *)     echo "✗ unknown lane: $lane" >&2; return 1 ;;
  esac
  # "idea" extras swap planned → idea status
  [[ "$extras" == *idea* ]] && status_label="status:idea"

  # Skip if an issue with the same title already exists (open or closed)
  if [[ $DRY_RUN -eq 0 ]]; then
    if gh issue list --repo "$REPO" --state all --search "in:title \"${title}\"" --json title \
        --jq '.[].title' 2>/dev/null | grep -Fxq "$title"; then
      printf "  ↻    %-60s (issue exists, skipped)\n" "${title:0:60}"
      return 0
    fi
  fi

  local label_csv="roadmap,${status_label},${lane_label}"
  [[ -n "$extras" ]] && label_csv="${label_csv},${extras}"

  if [[ $DRY_RUN -eq 1 ]]; then
    printf "  DRY  issue '%s'\n         labels=%s  milestone=%s\n" "${title:0:60}" "$label_csv" "$milestone_num"
    return 0
  fi

  local body_full="${body}

— Auto-created by \`scripts/setup-github-roadmap.sh\` from the README roadmap section."
  local milestone_args=()
  [[ -n "$milestone_num" && "$milestone_num" != "null" && "$milestone_num" != "0" ]] \
    && milestone_args=(--milestone "$milestone_num")

  local url
  url="$(gh issue create --repo "$REPO" --title "$title" --body "$body_full" \
            --label "$label_csv" "${milestone_args[@]}" 2>&1)" \
    && printf "  ✓    issue %s\n" "$url" \
    || { printf "  ✗    issue '%s'\n       %s\n" "${title:0:60}" "$url"; return 1; }

  if [[ "$close_flag" == "--close" ]]; then
    local n="${url##*/}"
    gh issue close "$n" --repo "$REPO" --reason completed >/dev/null && \
      printf "       closed (shipped)\n" || true
  fi
}

echo "── Issues (v1.x — in progress) ─────────────────────────────────────────"
for spec in "${V1X_ISSUES[@]}"; do
  IFS='|' read -r lane title body extras <<< "$spec"
  create_issue "$lane" "$title" "$body" "$extras"
done
echo
echo "── Issues (v2 — planned / ideas) ───────────────────────────────────────"
for spec in "${V2_ISSUES[@]}"; do
  IFS='|' read -r lane title body extras <<< "$spec"
  create_issue "$lane" "$title" "$body" "$extras"
done
echo

if [[ "${CLOSE_SHIPPED:-0}" == "1" ]]; then
  echo "── Issues (v1 — shipped, will be created & closed) ─────────────────────"
  for spec in "${V1_SHIPPED[@]}"; do
    IFS='|' read -r lane title body extras <<< "$spec"
    create_issue "$lane" "$title" "$body" "$extras" --close
  done
  echo
fi

echo "━━━ done ━━━"
echo "▸ next:  gh issue list --repo ${REPO} --label roadmap"
echo "▸ board: https://github.com/${REPO}/issues?q=label%3Aroadmap"

