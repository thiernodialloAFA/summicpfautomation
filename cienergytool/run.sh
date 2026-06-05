#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# cienergy — local analyzer entry point.
#
# What it does:
#   1. Builds the 3 helper binaries if missing (aggregator, csrd-export, gpu-probe).
#   2. (Optional) If `nvidia-smi` is on PATH, runs cienergy-gpu-probe in the
#      background for a few seconds to capture real GPU energy and appends the
#      sample to the steps file so the aggregator picks it up.
#   3. Runs the aggregator for one or more repositories in a single invocation
#      (multi-repo / monorepo mode), writing ./reports/cienergy-<repo>.json
#      per repository.
#   4. Runs cienergy-csrd-export over the reports directory → CSV ready for
#      ESRS E1 disclosure (Scope 2 + Scope 3.1). Staged for the dashboard.
#   5. If the ingester (default http://localhost:8085) answers /readyz, POSTs
#      every generated report to /v1/runs so the Grafana dashboard is fed,
#      then GETs /v1/runs to verify the rows are actually persisted.
#   6. Stages the reports + CSRD CSV under dashboard/embedded/local-reports/,
#      writes an index.json, starts a tiny local HTTP server (python3) on
#      ${DASHBOARD_PORT} (default 8086), and opens the embedded dashboard
#      pre-loaded with the just-generated reports (?src=…).
#
# Configuration via env (with sensible defaults):
#   REGION         Electricity Maps zone code           (default: FR)
#   REPOS          comma-separated list of repos        (default: 4 demo repos)
#                  Entries that look like a git URL (http(s)://…, git@…, .git)
#                  are cloned on the fly (see "Clone remotes" step below),
#                  scanned with cidetect, and deleted on script exit.
#   REPO_REMOTES   comma-/newline-separated 'slug=git-url' pairs. Same effect
#                  as putting URLs into $REPOS but lets you keep a clean slug.
#                  Example: REPO_REMOTES='acme/api=https://github.com/acme/api.git'
#   CLONE_DEPTH    shallow-clone depth for remotes (default: 1, 0 = full)
#   KEEP_CLONES    set to "1" to skip the temp-clone cleanup (debug)
#   CLONE_DIR      override the temp clone root (default: mktemp -d)
#   REPO_PATHS     comma- or newline-separated 'slug=path' pairs that map a
#                  repo slug from $REPOS to a *local checkout*. When set,
#                  cienergy-aggregator scans each path for CI YAML files
#                  (.github/workflows/*.yml, .gitlab-ci.yml, azure-pipelines.yml,
#                  Jenkinsfile, tekton/) and emits one *distinct* report per
#                  detected pipeline — fixes the previous "all repos report
#                  identical numbers" behaviour. Repos without a mapping fall
#                  back to the shared $STEPS_FILE.
#                  Example: REPO_PATHS='axa/claims=./axa-claims,axa/policy=./axa-policy'
#   STEPS_FILE     JSONL steps file                     (default: bundled sample)
#   OUT_TEMPLATE   --out template (supports {repo})     (default: ./reports/cienergy-{repo}.json)
#   INGESTER_URL   base URL of cienergy-ingester        (default: http://localhost:8085)
#   INGESTER_TOKEN bearer token if the ingester requires one (default: empty)
#   DASHBOARD_PORT static-server port for the dashboard (default: 8086)
#   OPEN_DASHBOARD set to "0" to skip opening the browser   (default: 1)
#   CSRD_PERIOD    free-text period stamped into the CSV    (default: derived YYYY-Qn)
#   CSRD_BY        aggregation granularity (run|day|month|repository|team|cost-center)
#                                                            (default: repository)
#   GPU_PROBE_SECS how long to run the GPU probe (if any)   (default: 4)
#   SKIP_GPU       set to "1" to disable the GPU probe step (default: 0)
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

# Always work from the directory this script lives in (project root).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# ── Configuration ───────────────────────────────────────────────────────────
# NOTE: the literal "{repo}" placeholder must not appear inside a
# `${VAR:-default}` expansion — the `}` would close the parameter expansion
# early. Assign the default through a separate variable to keep it intact.
# axa/claims,axa/policy,axa/shared-lib,
DEFAULT_OUT_TEMPLATE='./reports/cienergy-{repo}.json'
# Mix of demo + real public repos covering all CI/AI families:
#   • workshop demo                       → bundled tree (always local)
#   • backend code-quality tooling        → creedengo-* (cloned)
#   • AI training & inference            → ultralytics (YOLO), vllm
#   • AI serving / model deployment       → bentoml
#   • AI agents / orchestration           → langgraph
#   • Data / ML pipelines (DAG)          → dvc
# Anything that looks like a git URL is shallow-cloned, scanned by cidetect,
# and deleted on script exit. Override with REPOS='…' to scope down for speed.
DEFAULT_REPOS='myorg/green-api-workshop-final,https://github.com/thiernodialloAFA/creedengo-java,https://github.com/thiernodialloAFA/creedengo-rules-specifications,https://github.com/ultralytics/ultralytics.git,https://github.com/vllm-project/vllm.git,https://github.com/bentoml/BentoML.git,https://github.com/langchain-ai/langgraph.git,https://github.com/iterative/dvc.git'
# Default REPO_PATHS: only the workshop repo is a real checkout in this tree.
# The 3 axa/* slugs stay synthetic unless the user provides their own checkouts.
DEFAULT_REPO_PATHS='myorg/green-api-workshop-final=./myorg/green-api-workshop-final'

REGION="${REGION:-FR}"
REPOS="${REPOS:-$DEFAULT_REPOS}"
REPO_PATHS="${REPO_PATHS:-$DEFAULT_REPO_PATHS}"
STEPS_FILE="${STEPS_FILE:-./examples/samples/steps.jsonl}"
OUT_TEMPLATE="${OUT_TEMPLATE:-$DEFAULT_OUT_TEMPLATE}"
INGESTER_URL="${INGESTER_URL:-http://localhost:8085}"
INGESTER_TOKEN="${INGESTER_TOKEN:-}"
DASHBOARD_PORT="${DASHBOARD_PORT:-8086}"
OPEN_DASHBOARD="${OPEN_DASHBOARD:-1}"
# OpenTelemetry / OTLP collector (HTTP). The aggregator probes it; if it
# answers, every report is pushed as OTLP/HTTP-JSON gauges to <url>/v1/metrics.
OTLP_ENDPOINT="${OTLP_ENDPOINT:-http://localhost:4318}"
OTLP_HEADER="${OTLP_HEADER:-}"
SKIP_OTLP="${SKIP_OTLP:-0}"
# Derive a sensible default reporting period — current year + quarter.
DEFAULT_PERIOD="$(date -u +%Y)-Q$((( $(date -u +%-m) - 1 ) / 3 + 1))"
CSRD_PERIOD="${CSRD_PERIOD:-$DEFAULT_PERIOD}"
CSRD_BY="${CSRD_BY:-repository}"
GPU_PROBE_SECS="${GPU_PROBE_SECS:-4}"
SKIP_GPU="${SKIP_GPU:-0}"
REPO_REMOTES="${REPO_REMOTES:-}"
CLONE_DEPTH="${CLONE_DEPTH:-1}"
KEEP_CLONES="${KEEP_CLONES:-0}"

# ── Tiny logging helpers (colour-aware, no dependency) ──────────────────────
if [[ -t 1 ]]; then
  C_BOLD="\033[1m"; C_GREEN="\033[32m"; C_YEL="\033[33m"; C_BLUE="\033[34m"; C_RED="\033[31m"; C_OFF="\033[0m"
else
  C_BOLD=""; C_GREEN=""; C_YEL=""; C_BLUE=""; C_RED=""; C_OFF=""
fi
log()    { printf "${C_BLUE}▸${C_OFF} %s\n" "$*"; }
ok()     { printf "${C_GREEN}✓${C_OFF} %s\n" "$*"; }
warn()   { printf "${C_YEL}!${C_OFF} %s\n" "$*"; }
err()    { printf "${C_RED}✗${C_OFF} %s\n" "$*" >&2; }
section(){ printf "\n${C_BOLD}=== %s ===${C_OFF}\n" "$*"; }

# ── 1. Build if needed ──────────────────────────────────────────────────────
section "Build"
# Rebuild whenever any .go source under cmd/<name>/ or internal/ is newer
# than the binary. This avoids the silent "stale binary" footgun where a
# feature added to the source never reaches the runtime because the cached
# bin/* is still the previous build.
# Force a clean rebuild with FORCE_BUILD=1.
FORCE_BUILD="${FORCE_BUILD:-0}"
build_one() {
  local name="$1" pkg="$2"
  local bin="./bin/${name}"
  local needs_build=0
  if [[ "${FORCE_BUILD}" == "1" || ! -x "${bin}" ]]; then
    needs_build=1
  else
    # Any .go file under the package dir or under internal/ newer than the
    # binary → rebuild. `find -newer` is portable across BSD (macOS) and GNU.
    local newer
    newer=$(find "${pkg}" ./internal -type f -name '*.go' -newer "${bin}" 2>/dev/null | head -n1)
    [[ -n "${newer}" ]] && needs_build=1
  fi
  if [[ "${needs_build}" -eq 1 ]]; then
    if ! command -v go >/dev/null 2>&1; then
      err "Go toolchain not found on PATH. Install Go or build the binaries manually with 'make build'."
      exit 1
    fi
    log "building ${bin} (sources newer or forced)"
    go build -o "${bin}" "${pkg}"
  fi
}
build_one cienergy-aggregator  ./cmd/aggregator
build_one cienergy-csrd-export ./cmd/csrd-export
build_one cienergy-gpu-probe   ./cmd/gpu-probe
build_one cienergy-cidetect    ./cmd/cidetect
ok "binaries ready: cienergy-aggregator, cienergy-csrd-export, cienergy-gpu-probe, cienergy-cidetect"
# Sanity check: --vary-runner flag must be present (proves the binary embeds
# the per-job runner picker added in this patch).
if ! ./bin/cienergy-aggregator -h 2>&1 | grep -q -- '--vary-runner\|-vary-runner'; then
  warn "aggregator binary does not advertise --vary-runner — likely stale."
  warn "Run 'FORCE_BUILD=1 ./run.sh' to force a clean rebuild."
fi

# ── 2. GPU probe (optional, when nvidia-smi is available) ───────────────────
section "GPU probe"
EFFECTIVE_STEPS_FILE="${STEPS_FILE}"
if [[ "${SKIP_GPU}" == "1" ]]; then
  log "SKIP_GPU=1 — skipping GPU probe"
elif ! command -v nvidia-smi >/dev/null 2>&1; then
  warn "nvidia-smi not found — skipping GPU probe (no GPU contribution will be added)."
  warn "On a GPU host the probe samples nvidia-smi and appends a step entry to the steps file."
else
  ok "nvidia-smi detected — sampling for ${GPU_PROBE_SECS}s"
  # Copy the steps file so we don't mutate the bundled sample, then have the
  # probe append a GPU sample to the *copy*. The aggregator will then include
  # the GPU contribution automatically.
  EFFECTIVE_STEPS_FILE="$(mktemp -t cienergy-steps.XXXXXX.jsonl)"
  cp "${STEPS_FILE}" "${EFFECTIVE_STEPS_FILE}"
  ./bin/cienergy-gpu-probe \
    --name "gpu-sample" \
    --steps-file "${EFFECTIVE_STEPS_FILE}" \
    --interval-ms 1000 \
    --cpu-util 40 &
  PROBE_PID=$!
  log "probe PID=${PROBE_PID}"
  # Simulate a workload window (in a real pipeline this would wrap ./train.py).
  sleep "${GPU_PROBE_SECS}"
  kill -TERM "${PROBE_PID}" 2>/dev/null || true
  wait "${PROBE_PID}" 2>/dev/null || true
  added=$(tail -n1 "${EFFECTIVE_STEPS_FILE}" 2>/dev/null || true)
  if [[ -n "${added}" ]]; then
    ok "GPU sample appended: ${added}"
  else
    warn "GPU probe ran but appended no sample (nvidia-smi might have failed)"
  fi
fi

# ── 3. Run the aggregator ───────────────────────────────────────────────────
section "Aggregate"
mkdir -p ./reports
START_TS="$(date -u +%FT%TZ)"

# ── 3a. Clone remote repos on the fly ───────────────────────────────────────
# Any entry in REPOS that looks like a git URL — or anything declared via
# REPO_REMOTES='slug=url,…' — is shallow-cloned into a temp dir, registered as
# a repo-path mapping (so cidetect scans the real CI files), and deleted on
# script exit. This avoids shipping fake/synthetic numbers for remote repos.
is_git_url() {
  case "$1" in
    http://*|https://*|git@*:*|ssh://*|git://*) return 0 ;;
    *.git)                                       return 0 ;;
    *)                                           return 1 ;;
  esac
}
# Derive "owner/repo" (or just "repo") from a git URL.
slug_from_url() {
  local u="$1"
  u="${u%.git}"                       # strip trailing .git
  u="${u%/}"                          # strip trailing slash
  case "$u" in
    git@*:*)        u="${u#*:}" ;;    # git@host:owner/repo  → owner/repo
    *://*)          u="${u#*://}"; u="${u#*/}" ;;  # scheme://host/owner/repo → owner/repo
  esac
  printf '%s' "$u"
}

CLONE_ROOT=""
cleanup_clones() {
  if [[ -n "${CLONE_ROOT}" && -d "${CLONE_ROOT}" ]]; then
    if [[ "${KEEP_CLONES}" == "1" ]]; then
      warn "KEEP_CLONES=1 — leaving clones in ${CLONE_ROOT}"
    else
      rm -rf "${CLONE_ROOT}" && ok "cleaned up clones in ${CLONE_ROOT}" || \
        warn "could not remove ${CLONE_ROOT}"
    fi
  fi
}
trap cleanup_clones EXIT

# Build the working REPOS list (URLs replaced by their slug) and append any
# clone-derived mappings to REPO_PATHS.
NEW_REPOS=""; CLONED=0
add_clone_entry() {  # slug, url
  local slug="$1" url="$2" dest
  if [[ -z "${CLONE_ROOT}" ]]; then
    CLONE_ROOT="${CLONE_DIR:-$(mktemp -d -t cienergy-clones.XXXXXX)}"
    log "clone root: ${CLONE_ROOT}"
  fi
  # Use the slug as directory name (replace "/" so it nests cleanly).
  dest="${CLONE_ROOT}/${slug//\//__}"
  if [[ -d "${dest}/.git" ]]; then
    log "already cloned: ${slug}"
  else
    local depth_args=()
    [[ "${CLONE_DEPTH}" != "0" ]] && depth_args=(--depth "${CLONE_DEPTH}")
    log "cloning ${url} → ${dest}"
    if ! git clone --quiet ${depth_args[@]+"${depth_args[@]}"} "${url}" "${dest}" 2>&1 | sed 's/^/    /'; then
      warn "git clone failed for ${url} — keeping slug as-is (will fall back to --steps-file)"
      return 1
    fi
    CLONED=$((CLONED+1))
  fi
  REPO_PATHS="${REPO_PATHS:+${REPO_PATHS},}${slug}=${dest}"
  return 0
}

if [[ -n "${REPO_REMOTES}" || "${REPOS}" == *http*://* || "${REPOS}" == *git@* ]]; then
  if ! command -v git >/dev/null 2>&1; then
    err "git not found on PATH — cannot clone remote repos. Install git or remove URLs from REPOS / REPO_REMOTES."
    exit 1
  fi
fi

# 1) explicit slug=url pairs from REPO_REMOTES
if [[ -n "${REPO_REMOTES}" ]]; then
  while IFS= read -r entry; do
    entry="$(printf '%s' "$entry" | sed 's/^ *//;s/ *$//')"
    [[ -z "$entry" ]] && continue
    slug="${entry%%=*}"; url="${entry#*=}"
    if [[ -z "$slug" || -z "$url" || "$slug" == "$url" ]]; then
      warn "ignoring malformed REPO_REMOTES entry '${entry}' (expected slug=url)"
      continue
    fi
    add_clone_entry "${slug}" "${url}" || true
    # Make sure the slug shows up in REPOS so the aggregator emits a report for it.
    case ",${REPOS}," in *",${slug},"*) : ;; *) REPOS="${REPOS:+${REPOS},}${slug}" ;; esac
  done < <(printf '%s\n' "${REPO_REMOTES}" | tr ',' '\n')
fi

# 2) URLs found inside $REPOS — clone & rewrite to slugs
while IFS= read -r entry; do
  entry="$(printf '%s' "$entry" | sed 's/^ *//;s/ *$//')"
  [[ -z "$entry" ]] && continue
  if is_git_url "$entry"; then
    slug="$(slug_from_url "$entry")"
    add_clone_entry "${slug}" "${entry}" || slug="${entry}"  # fallback keeps original
    NEW_REPOS="${NEW_REPOS:+${NEW_REPOS},}${slug}"
  else
    NEW_REPOS="${NEW_REPOS:+${NEW_REPOS},}${entry}"
  fi
done < <(printf '%s\n' "${REPOS}" | tr ',' '\n')
REPOS="${NEW_REPOS}"

if [[ "${CLONED}" -gt 0 ]]; then
  ok "cloned ${CLONED} remote repo(s) into ${CLONE_ROOT} — will be deleted on exit"
fi

log "region=${REGION}  repos=[${REPOS}]  steps=${EFFECTIVE_STEPS_FILE}"
log "out template: ${OUT_TEMPLATE}"

# Resolve REPO_PATHS into repeated --repo-path flags. Entries can be separated
# by commas or newlines, so monorepo configs stay readable in env files.
REPO_PATH_ARGS=()
if [[ -n "${REPO_PATHS}" ]]; then
  # Translate commas to newlines, then iterate.
  while IFS= read -r entry; do
    entry="$(printf '%s' "$entry" | sed 's/^ *//;s/ *$//')"
    [[ -z "$entry" ]] && continue
    slug="${entry%%=*}"
    rp="${entry#*=}"
    if [[ -z "$slug" || -z "$rp" || "$slug" == "$rp" ]]; then
      warn "ignoring malformed REPO_PATHS entry '${entry}' (expected slug=path)"
      continue
    fi
    if [[ ! -d "$rp" ]]; then
      warn "REPO_PATHS '${slug}=${rp}' — directory not found, will fall back to --steps-file"
      continue
    fi
    REPO_PATH_ARGS+=(--repo-path "${slug}=${rp}")
    # Pre-flight peek so the operator sees what was detected.
    if [[ -x ./bin/cienergy-cidetect ]]; then
      ./bin/cienergy-cidetect --repo "${rp}" --quiet 2>&1 \
        | python3 -c 'import json,sys; pls=json.load(sys.stdin); [print(f"    {p[\"Platform\"]:14s} {p[\"RelPath\"]:50s} steps={len(p[\"Steps\"]):3d}") for p in pls]' 2>/dev/null \
        | sed "s|^|${slug} ▸ |" || true
    fi
  done < <(printf '%s\n' "${REPO_PATHS}" | tr ',' '\n')
fi
if [[ ${#REPO_PATH_ARGS[@]} -gt 0 ]]; then
  ok "ci-detect enabled for $((${#REPO_PATH_ARGS[@]} / 2)) repo(s) — energy will be distinct per pipeline"
else
  log "ci-detect disabled (no REPO_PATHS) — every repo will share the --steps-file numbers"
fi

# Probe the OpenTelemetry collector ahead of time. If it's reachable we ask
# the aggregator to push gauges to it for every emitted report (one set of
# metrics per repository, with run.id / repository / zone as resource labels).
OTLP_ARGS=()
OTLP_STATUS="skipped"
if [[ "${SKIP_OTLP}" != "1" && -n "${OTLP_ENDPOINT}" ]]; then
  otlp_probe=$(curl -fsS -o /dev/null -w "%{http_code}" --max-time 2 \
      "${OTLP_ENDPOINT%/}/v1/metrics" -X POST \
      -H 'Content-Type: application/json' \
      --data '{"resourceMetrics":[]}' 2>/dev/null || echo "000")
  if [[ "${otlp_probe}" =~ ^(2|4)[0-9][0-9]$ ]]; then
    OTLP_ARGS=(--otlp-endpoint "${OTLP_ENDPOINT}")
    [[ -n "${OTLP_HEADER}" ]] && OTLP_ARGS+=(--otlp-header "${OTLP_HEADER}")
    OTLP_STATUS="enabled (${OTLP_ENDPOINT})"
    ok "OTLP collector reachable — pushing metrics to ${OTLP_ENDPOINT}/v1/metrics"
  else
    warn "OTLP collector not reachable at ${OTLP_ENDPOINT}/v1/metrics (HTTP ${otlp_probe}) — skipping OTLP push"
    warn "    start the stack with: (cd dashboard/grafana && podman compose up -d otel-collector prometheus grafana)"
  fi
fi

./bin/cienergy-aggregator \
  --start      "${START_TS}" \
  --region     "${REGION}" \
  --repo       "${REPOS}" \
  --steps-file "${EFFECTIVE_STEPS_FILE}" \
  --out        "${OUT_TEMPLATE}" \
  ${REPO_PATH_ARGS[@]+"${REPO_PATH_ARGS[@]}"} \
  ${OTLP_ARGS[@]+"${OTLP_ARGS[@]}"}

# Collect the reports we just wrote (one per repo).
# The aggregator slugifies "/" → "_" in filenames; we list everything that
# matches the template prefix and is newer than the EFFECTIVE_STEPS_FILE.
# Use a `while read` loop instead of `mapfile` so we stay compatible with
# bash 3.2 (the default on macOS).
REPORT_DIR="$(dirname "${OUT_TEMPLATE}")"
PREFIX="$(basename "${OUT_TEMPLATE%\{repo\}*}")"
SUFFIX=".json"
REPORTS=()
while IFS= read -r line; do
  [[ -n "$line" ]] && REPORTS+=("$line")
done < <(find "${REPORT_DIR}" -type f -name "${PREFIX}*${SUFFIX}" -newer "${EFFECTIVE_STEPS_FILE}" 2>/dev/null | sort)
if [[ ${#REPORTS[@]} -eq 0 ]]; then
  # Fallback: list every json in the report dir (covers single-repo / no template cases)
  while IFS= read -r line; do
    [[ -n "$line" ]] && REPORTS+=("$line")
  done < <(find "${REPORT_DIR}" -maxdepth 1 -type f -name "*.json" | sort)
fi
ok "wrote ${#REPORTS[@]} report(s):"
printf "    %s\n" "${REPORTS[@]}"

# ── 4. CSRD / ESRS E1 export ────────────────────────────────────────────────
section "CSRD export"
CSRD_CSV="${REPORT_DIR}/cienergy-csrd-${CSRD_PERIOD}.csv"
log "aggregating ${#REPORTS[@]} report(s) by ${CSRD_BY} for period ${CSRD_PERIOD}"
if ./bin/cienergy-csrd-export \
    --in     "${REPORT_DIR}" \
    --period "${CSRD_PERIOD}" \
    --by     "${CSRD_BY}" \
    --out    "${CSRD_CSV}" 2>&1 | sed 's/^/    /'; then
  ok "wrote ${CSRD_CSV}"
  # Show the first lines (header + a couple of rows) for quick inspection.
  head -n 6 "${CSRD_CSV}" | sed 's/^/      /' || true
else
  err "cienergy-csrd-export failed"
fi

# ── 3. Ingest if the ingester is up ─────────────────────────────────────────
section "Ingest"
ready_code=$(curl -fsS -o /dev/null -w "%{http_code}" --max-time 2 "${INGESTER_URL}/readyz" 2>/dev/null || echo "000")
if [[ "${ready_code}" == "200" ]]; then
  ok "ingester ready at ${INGESTER_URL}/readyz"
  AUTH_HEADER=()
  if [[ -n "${INGESTER_TOKEN}" ]]; then
    AUTH_HEADER=(-H "Authorization: Bearer ${INGESTER_TOKEN}")
  fi
  pushed=0; failed=0; saw_401=0
  for report in "${REPORTS[@]}"; do
    # Note: ${AUTH_HEADER[@]+...} guard is required so that expanding an empty
    # array doesn't trip `set -u` on bash 3.2 (the default on macOS).
    http_code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${INGESTER_URL}/v1/runs" \
        -H 'Content-Type: application/json' \
        ${AUTH_HEADER[@]+"${AUTH_HEADER[@]}"} \
        --data-binary "@${report}" || echo "000")
    if [[ "${http_code}" =~ ^2[0-9][0-9]$ ]]; then
      ok "POSTed $(basename "${report}") (HTTP ${http_code})"
      pushed=$((pushed+1))
    else
      err "POST $(basename "${report}") → HTTP ${http_code}"
      failed=$((failed+1))
      [[ "${http_code}" == "401" ]] && saw_401=1
    fi
  done
  log "ingestion summary: ${pushed} ok, ${failed} failed"
  if [[ "${saw_401}" == "1" && -z "${INGESTER_TOKEN}" ]]; then
    warn "ingester is bearer-protected — re-run with: INGESTER_TOKEN=<token> ./run.sh"
  fi

  # ── 3b. Verify persistence: list runs from the ingester ────────────────
  section "Verify persistence"
  log "GET ${INGESTER_URL}/v1/runs?limit=200"
  list_body="$(mktemp -t cienergy-runs.XXXXXX)"
  list_code=$(curl -s -o "${list_body}" -w "%{http_code}" \
      ${AUTH_HEADER[@]+"${AUTH_HEADER[@]}"} \
      "${INGESTER_URL}/v1/runs?limit=200" || echo "000")
  if [[ "${list_code}" == "200" ]]; then
    if command -v jq >/dev/null 2>&1; then
      total=$(jq -r '(.items // []) | length' "${list_body}" 2>/dev/null || echo "?")
      ok "${total} run(s) persisted in Postgres"
      # Show the last N (our 4 should be in there)
      jq -r '(.items // []) | .[0:10] | .[] | "    \(.startedAt // "—")  \(.repository // "?")  sci=\(.sci_value // .sci // "?")  id=\(.id // "?")"' "${list_body}" 2>/dev/null || true
    else
      total=$(grep -o '"id":' "${list_body}" | wc -l | tr -d ' ')
      ok "${total} run(s) persisted (install jq for per-run detail)"
      head -c 600 "${list_body}"; echo
    fi
  else
    err "GET /v1/runs returned HTTP ${list_code} — could not verify persistence"
    [[ -s "${list_body}" ]] && head -c 300 "${list_body}" && echo
  fi
  rm -f "${list_body}"
else
  warn "ingester not reachable at ${INGESTER_URL}/readyz (HTTP ${ready_code})"
  warn "skipping ingestion and persistence check — start the stack with:"
  warn "    (cd dashboard/grafana && podman compose up -d)"
fi

# ── 3c. Verify OTLP / Prometheus reception ──────────────────────────────────
section "Verify OTLP metrics"
if [[ "${OTLP_STATUS}" == "skipped" ]]; then
  log "OTLP push was skipped — no metrics to verify"
else
  PROM_URL="${PROM_URL:-http://localhost:9090}"
  prom_code=$(curl -fsS -o /dev/null -w "%{http_code}" --max-time 2 "${PROM_URL}/-/ready" 2>/dev/null || echo "000")
  if [[ "${prom_code}" == "200" ]]; then
    # Give the collector → prometheus pipeline a second to scrape.
    sleep 2
    q='count(count by (service_namespace) (cienergy_energy_kwh))'
    series=$(curl -fsS --get --data-urlencode "query=${q}" "${PROM_URL}/api/v1/query" 2>/dev/null \
              | (command -v jq >/dev/null && jq -r '.data.result[0].value[1] // "0"' || sed -n 's/.*"value":\[[^,]*,"\([0-9.]*\)".*/\1/p'))
    if [[ -n "${series}" && "${series}" != "0" ]]; then
      ok "Prometheus sees ${series} distinct repositor(y/ies) in cienergy_energy_kwh"
      curl -fsS --get --data-urlencode 'query={__name__=~"cienergy_.*"}' "${PROM_URL}/api/v1/query" 2>/dev/null \
        | (command -v jq >/dev/null \
            && jq -r '.data.result[] | "    \(.metric.__name__){repo=\(.metric.service_namespace // "?"), zone=\(.metric.sustainability_grid_zone // "?")} = \(.value[1])"' \
            | sort | head -20 \
            || echo "    (install jq for per-metric detail)")
    else
      warn "Prometheus is up but no cienergy_* series yet (collector might still be scraping)"
    fi
  else
    warn "Prometheus not reachable at ${PROM_URL}/-/ready (HTTP ${prom_code}) — start it with: podman compose up -d prometheus"
  fi
fi

# ── 4. Stage reports for the embedded dashboard auto-load ───────────────────
section "Dashboard"
DASHBOARD_DIR="${SCRIPT_DIR}/dashboard/embedded"
STAGE_DIR="${DASHBOARD_DIR}/local-reports"
if [[ ${#REPORTS[@]} -gt 0 && -d "${DASHBOARD_DIR}" ]]; then
  rm -rf "${STAGE_DIR}"
  mkdir -p "${STAGE_DIR}"
  STAGED_NAMES=()
  for r in "${REPORTS[@]}"; do
    cp "$r" "${STAGE_DIR}/"
    STAGED_NAMES+=("$(basename "$r")")
  done
  # Stage the CSRD CSV too, if it was generated, so the dashboard can link to it.
  CSRD_STAGED_NAME=""
  if [[ -f "${CSRD_CSV}" ]]; then
    cp "${CSRD_CSV}" "${STAGE_DIR}/"
    CSRD_STAGED_NAME="$(basename "${CSRD_CSV}")"
  fi
  # Write index.json matching the schema used by sample-reports/index.json:
  # { "reports": [ ... ], "csrdCsv": "...", "generatedAt": "..." }
  {
    printf '{\n  "generatedAt": "%s",\n' "$(date -u +%FT%TZ)"
    if [[ -n "${CSRD_STAGED_NAME}" ]]; then
      printf '  "csrdCsv": "%s",\n  "csrdPeriod": "%s",\n  "csrdBy": "%s",\n' \
        "${CSRD_STAGED_NAME}" "${CSRD_PERIOD}" "${CSRD_BY}"
    fi
    printf '  "reports": ['
    sep=''
    for n in "${STAGED_NAMES[@]}"; do
      printf '%s\n    "%s"' "$sep" "$n"
      sep=','
    done
    printf '\n  ]\n}\n'
  } > "${STAGE_DIR}/index.json"
  ok "staged ${#STAGED_NAMES[@]} report(s)${CSRD_STAGED_NAME:+ + CSRD CSV} → ${STAGE_DIR}"
fi

# Start a tiny static HTTP server so the browser can fetch ./local-reports/*
# via the dashboard's ?src= URL (file:// can't do that because of CORS).
URL=""
SERVER_STARTED=0
if [[ "${OPEN_DASHBOARD}" == "1" ]]; then
  if command -v python3 >/dev/null 2>&1; then
    # Kill any previous instance of this script's server so re-runs are clean.
    if command -v lsof >/dev/null 2>&1; then
      old_pid=$(lsof -ti tcp:"${DASHBOARD_PORT}" 2>/dev/null || true)
      if [[ -n "${old_pid}" ]]; then
        kill "${old_pid}" 2>/dev/null || true
        sleep 0.3
      fi
    fi
    ( cd "${DASHBOARD_DIR}" && nohup python3 -m http.server "${DASHBOARD_PORT}" --bind 127.0.0.1 \
        > "/tmp/cienergy-dashboard-${DASHBOARD_PORT}.log" 2>&1 & echo $! > "/tmp/cienergy-dashboard-${DASHBOARD_PORT}.pid" )
    sleep 0.5
    SERVER_STARTED=1
    URL="http://127.0.0.1:${DASHBOARD_PORT}/index.html?src=./local-reports/index.json"
    ok "static server on http://127.0.0.1:${DASHBOARD_PORT} (PID $(cat "/tmp/cienergy-dashboard-${DASHBOARD_PORT}.pid"))"
    log "to stop: kill \$(cat /tmp/cienergy-dashboard-${DASHBOARD_PORT}.pid)"
  else
    warn "python3 not found — opening dashboard via file:// (auto-load disabled, drag-drop the reports manually)"
    URL="file://${DASHBOARD_DIR}/index.html"
  fi

  log "opening ${URL}"
  case "$(uname -s)" in
    Darwin*)  open "${URL}" ;;
    Linux*)   if command -v xdg-open >/dev/null 2>&1; then xdg-open "${URL}" >/dev/null 2>&1 & fi ;;
    MINGW*|MSYS*|CYGWIN*) start "" "${URL}" ;;
    *)        warn "don't know how to open a browser on $(uname -s) — open this URL manually:"
              echo "    ${URL}" ;;
  esac
  if [[ "${SERVER_STARTED}" == "1" ]]; then
    ok "dashboard auto-loaded the ${#REPORTS[@]} report(s) — no drag-drop needed."
  fi
  if [[ "${ready_code:-000}" == "200" ]]; then
    ok "Grafana stack is up — also try: http://localhost:3000  (cienergy/overview)"
  fi
else
  log "skipped opening the browser (OPEN_DASHBOARD=0)"
fi

section "Done"
ok "${#REPORTS[@]} report(s) in ${REPORT_DIR}/ — staged in ${STAGE_DIR}"





