/*  cienergy embedded dashboard
 *  Vanilla ES2022. Reads cienergy `energy-report.json` files (v1 schema) and
 *  renders summary cards, charts and a sortable/filterable table.
 *  No build step, no framework, no backend, no telemetry.
 */
"use strict";

// ---------- State -----------------------------------------------------------
const state = {
  reports: [],          // valid Report objects
  errors: [],           // {file, message}
  sortKey: "startedAt",
  sortAsc: false,
  filter: "",
  charts: {},           // Chart.js instances
  simulator: {
    zone: "",           // "" = use measured intensity from each report
    runsPerDay: 1,
    daysPerYear: 365,
  },
  suggestionsMinSeverity: "info",   // "info" shows all
};

/**
 * Annual grid carbon-intensity averages, gCO₂eq/kWh (Ember 2024 / IEA).
 * Used only for the "what-if" scenario simulator — measured runs keep
 * their original Electricity-Maps / Ember value.
 */
const ZONE_INTENSITY = {
  "FR": 56,  "DE": 380, "PL": 660, "SE": 30,  "NO": 30,
  "ES": 150, "IT": 270, "GB": 200, "NL": 330, "IE": 290,
  "US-VA": 280, "US-CA": 230, "US-TX": 410, "CA-QC": 30, "BR": 100,
  "IN": 700, "CN": 580, "JP": 460, "AU": 540, "ZA": 900,
  "WORLD": 475,
};
const PETROL_CAR_GCO2_PER_KM = 170; // EU avg (EEA 2024)

const $ = (sel) => document.querySelector(sel);

/**
 * Magnitude-aware formatters returning `{ value, unit }`.
 * Energy is stored in kWh, carbon in gCO₂eq throughout the codebase — these
 * helpers pick the most readable SI / mass prefix so every visible figure
 * scales automatically (kWh → MWh → GWh → TWh ; g → kg → t → kt → Mt).
 */
function fmtEnergyParts(kwh) {
  if (kwh == null || !isFinite(kwh)) return { value: "—", unit: "kWh" };
  const a = Math.abs(kwh);
  const d = (n, p = 2) => (Math.abs(n) >= 100 ? n.toFixed(0) : Math.abs(n) >= 10 ? n.toFixed(1) : n.toFixed(p));
  if (a >= 1e9)   return { value: d(kwh / 1e9),  unit: "TWh" };
  if (a >= 1e6)   return { value: d(kwh / 1e6),  unit: "GWh" };
  if (a >= 1e3)   return { value: d(kwh / 1e3),  unit: "MWh" };
  if (a >= 1)     return { value: d(kwh, 3),     unit: "kWh" };
  if (a >= 1e-3)  return { value: d(kwh * 1e3),  unit: "Wh"  };
  return                  { value: d(kwh * 1e6), unit: "mWh" };
}
function fmtCarbonParts(g) {
  if (g == null || !isFinite(g)) return { value: "—", unit: "gCO₂eq" };
  const a = Math.abs(g);
  const d = (n, p = 2) => (Math.abs(n) >= 100 ? n.toFixed(0) : Math.abs(n) >= 10 ? n.toFixed(1) : n.toFixed(p));
  if (a >= 1e12) return { value: d(g / 1e12), unit: "MtCO₂eq" };  // megatonnes
  if (a >= 1e9)  return { value: d(g / 1e9),  unit: "ktCO₂eq" };  // kilotonnes
  if (a >= 1e6)  return { value: d(g / 1e6),  unit: "tCO₂eq"  };  // tonnes (1 t = 10⁶ g)
  if (a >= 1e3)  return { value: d(g / 1e3),  unit: "kgCO₂eq" };
  if (a >= 1)    return { value: d(g, 3),     unit: "gCO₂eq"  };
  return                { value: d(g * 1e3),  unit: "mgCO₂eq" };
}
const fmtE = (v) => { const p = fmtEnergyParts(v); return `${p.value} ${p.unit}`; };
const fmtC = (v) => { const p = fmtCarbonParts(v); return `${p.value} ${p.unit}`; };
function setKpi(idValue, idUnit, parts, unitSuffix = "") {
  const elV = document.getElementById(idValue);
  const elU = document.getElementById(idUnit);
  if (elV) elV.textContent = parts.value;
  if (elU) elU.textContent = parts.unit + unitSuffix;
}

const fmt = {
  kwh:  (v) => (v == null ? "—" : v.toFixed(v < 0.01 ? 5 : 3)),
  co2:  (v) => (v == null ? "—" : v < 10 ? v.toFixed(3) : v.toFixed(1)),
  date: (s) => new Date(s).toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" }),
  pct:  (v) => `${v.toFixed(1)}%`,
};

// ---------- Schema validation (Ajv) ----------------------------------------
// Inline minimal schema (matches docs/schema/v1.json — keep in sync).
const SCHEMA_V1 = {
  type: "object",
  required: ["specVersion", "run", "runner", "energy", "carbon", "sci"],
  properties: {
    specVersion: { type: "string" },
    run: { type: "object", required: ["id","platform","repository","workflow","commitSha","startedAt","endedAt","durationSeconds"] },
    runner: { type: "object", required: ["os","arch","vcpu","ramGiB","cpuModel","tdpWatts","provider","region"] },
    energy: { type: "object", required: ["totalKWh","byStep"] },
    carbon: { type: "object", required: ["operationalGCO2eq","embodiedGCO2eq","totalGCO2eq","gridIntensity"] },
    sci:    { type: "object", required: ["value","unit","functionalUnit","R"] },
  },
};
let validateReport = () => true;
try {
  // The ajv7.bundle exposes the constructor at window.ajv7 (default export).
  const AjvCtor = (window.ajv7 && (window.ajv7.default || window.ajv7)) || null;
  if (AjvCtor) {
    const ajv = new AjvCtor({ allErrors: true, strict: false });
    validateReport = ajv.compile(SCHEMA_V1);
  } else {
    console.warn("Ajv not loaded; running without schema validation.");
  }
} catch (e) {
  console.warn("Ajv unavailable, validation relaxed:", e);
}

// ---------- Loading ---------------------------------------------------------
async function loadFiles(fileList) {
  state.errors = [];
  for (const f of fileList) {
    try {
      const text = await f.text();
      const json = JSON.parse(text);
      if (!validateReport(json)) {
        const msg = (validateReport.errors || []).map(e => `${e.instancePath} ${e.message}`).join("; ");
        state.errors.push({ file: f.name, message: msg || "invalid schema" });
        continue;
      }
      state.reports.push(json);
    } catch (e) {
      state.errors.push({ file: f.name, message: String(e) });
    }
  }
  render();
}

async function loadSamples() {
  try {
    const index = await fetch("./sample-reports/index.json", { cache: "no-store" }).then(r => r.json());
    const files = await Promise.all(index.reports.map(async name => {
      const json = await fetch(`./sample-reports/${name}`, { cache: "no-store" }).then(r => r.json());
      return { name, json };
    }));
    for (const { name, json } of files) {
      if (validateReport(json)) state.reports.push(json);
      else state.errors.push({ file: name, message: "invalid schema" });
    }
    render();
  } catch (e) {
    state.errors.push({ file: "sample-reports/index.json", message: String(e) });
    render();
  }
}

// Also support ?src=<url-to-index> or ?src=<url-to-single-report.json>
async function loadFromQuery() {
  const src = new URLSearchParams(location.search).get("src");
  if (!src) return false;
  try {
    const head = await fetch(src, { cache: "no-store" }).then(r => r.json());
    const list = Array.isArray(head?.reports) ? head.reports : null;
    const base = src.replace(/[^/]+$/, "");
    if (list) {
      for (const name of list) {
        const j = await fetch(base + name).then(r => r.json());
        if (validateReport(j)) state.reports.push(j);
      }
    } else if (validateReport(head)) {
      state.reports.push(head);
    }
    // Optional CSRD CSV reference written by run.sh — surfaces a download link.
    if (head?.csrdCsv) {
      state.csrd = {
        url:    base + head.csrdCsv,
        period: head.csrdPeriod || "",
        by:     head.csrdBy || "",
      };
    }
    render();
    return true;
  } catch (e) {
    state.errors.push({ file: src, message: String(e) });
    render();
    return false;
  }
}

// ---------- Aggregation helpers --------------------------------------------
function totals(reports) {
  return reports.reduce((acc, r) => {
    acc.kwh += r.energy.totalKWh;
    acc.co2 += r.carbon.totalGCO2eq;
    acc.saved += r.cache?.savedGCO2eqEstimate || 0;
    // Σ GPU contribution across all steps of all runs (when probe ran).
    for (const s of (r.energy.byStep || [])) {
      acc.gpu += (s.gpuKWh || 0);
    }
    return acc;
  }, { kwh: 0, co2: 0, saved: 0, gpu: 0 });
}

function archKey(r) {
  const a = (r.runner.arch || "").toLowerCase();
  if (a === "amd64" || a === "x86_64") return "x86_64";
  if (a === "aarch64") return "arm64";
  return a || "unknown";
}

// Produce a short (≤ 8 chars) label for the X-axis of per-run charts.
// Uses the repository basename (segment after the last "/") so a repo like
// `myorg/green-api-workshop-final` shows up as `green-a…` instead of the
// full slug, and 30+ char labels never blow up the chart width.
// Long names are truncated to 7 chars + "…".
//
//   axa/claims                        →  claims
//   axa/green-api-workshop-final      →  green-a…
//   thiernodialloAFA/creedengo-java   →  creeden…
function shortRunLabel(r) {
  const repo = (r?.run?.repository || r?.run?.id || "").trim();
  const base = repo.split("/").pop() || repo || "?";
  if (base.length <= 8) return base;
  return base.slice(0, 7) + "…";
}

// Disambiguate identical short labels across the dataset: when two repos
// share the same basename (rare but possible), append a numeric suffix so
// the chart still gives each bar a unique X tick.
function buildShortRunLabels(reports) {
  const seen = new Map();   // base → count
  return reports.map(r => {
    const base = shortRunLabel(r);
    const n = (seen.get(base) || 0) + 1;
    seen.set(base, n);
    return n === 1 ? base : `${base.slice(0, 6)}…${n}`;
  });
}

function dominantSource(r) {
  const counts = {};
  let bestKwh = -1, bestSrc = "—";
  for (const s of r.energy.byStep || []) {
    counts[s.source] = (counts[s.source] || 0) + s.kWh;
    if (counts[s.source] > bestKwh) { bestKwh = counts[s.source]; bestSrc = s.source; }
  }
  return bestSrc;
}

// ---------- Rendering -------------------------------------------------------
function render() {
  const hasData = state.reports.length > 0;
  $("#summary").classList.toggle("hidden", !hasData);
  $("#simulator").classList.toggle("hidden", !hasData);
  $("#charts").classList.toggle("hidden",  !hasData);
  $("#tableCard").classList.toggle("hidden", !hasData);
  $("#suggestionsCard").classList.toggle("hidden", !hasData);
  $("#dropZone").classList.toggle("hidden", hasData);

  renderErrors();
  renderCsrdBanner();
  if (!hasData) return;

  renderKpis();
  renderSimulator();
  renderCharts();
  renderTable();
  renderSuggestions();
}

function renderCsrdBanner() {
  const el = $("#csrdBanner");
  if (!el) return;
  if (!state.csrd?.url) { el.classList.add("hidden"); return; }
  el.classList.remove("hidden");
  const a = $("#csrdDownload");
  a.href = state.csrd.url;
  a.setAttribute("download", state.csrd.url.split("/").pop() || "cienergy-csrd.csv");
  const sub = $("#csrdBannerSub");
  const parts = [];
  if (state.csrd.period) parts.push(`period ${state.csrd.period}`);
  if (state.csrd.by)     parts.push(`aggregated by ${state.csrd.by}`);
  parts.push("GHG Protocol Scope 2 (operational) + Scope 3.1 (embodied)");
  sub.textContent = parts.join(" · ");
}

function renderErrors() {
  const el = $("#errors");
  if (!state.errors.length) { el.classList.add("hidden"); el.innerHTML = ""; return; }
  el.classList.remove("hidden");
  el.innerHTML = `<strong>${state.errors.length} file(s) rejected:</strong><ul>${
    state.errors.map(e => `<li><code>${e.file}</code> — ${e.message}</li>`).join("")
  }</ul>`;
}

function renderKpis() {
  const t = totals(state.reports);
  const s = scenarioAggregate();
  const n = state.reports.length || 1;

  // Top-level KPIs now reflect the active what-if scenario (zone override +
  // X runs/day × Y days/year). When neither knob has moved (zone = measured,
  // runsPerYear = N, where N = number of loaded reports), the KPIs collapse
  // back to the raw measured totals so the dashboard still shows the source
  // numbers out-of-the-box.
  const zoneActive  = !!s.zone;
  const projecting  = s.runsPerYear !== n && s.runsPerYear > 0;

  // Energy: independent of zone (E doesn't depend on grid intensity), but
  // scales linearly with the projection factor.
  const energyScale = projecting ? s.runsPerYear / n : 1;
  const projectedKwh = t.kwh * energyScale;

  // Carbon: re-projected against the selected zone *then* scaled. We rely on
  // scenarioAggregate's projection which uses mean per-run × runsPerYear;
  // when not projecting, fall back to the cumulative measured total
  // (recomputed with zone override if any) so the figure matches the JSON.
  let displayedCO2;
  if (projecting) {
    displayedCO2 = s.annualCO2;  // mean × runsPerYear, with zone override
  } else if (zoneActive) {
    // No projection but zone override: sum per-run with the new zone.
    displayedCO2 = projectReports(s.zone).reduce((acc, r) => acc + r.total, 0);
  } else {
    displayedCO2 = t.co2;        // raw measured total
  }

  // Suffix used by both Energy and Carbon KPIs so they tell the same story.
  const parts = [];
  if (projecting)  parts.push(`× ${s.runsPerYear.toLocaleString()} runs/yr`);
  if (zoneActive)  parts.push(`zone ${s.zone} @ ${Math.round(s.intensity)} g/kWh`);
  if (!projecting && !zoneActive) parts.push(`measured · ${n} run(s)`);
  const suffix = "  · " + parts.join(" · ");

  $("#kpiRuns").textContent = state.reports.length;
  setKpi("kpiKWh", "kpiKWhUnit", fmtEnergyParts(projectedKwh), suffix);

  // GPU energy slice (sum of step.gpuKWh) — kept as raw kWh + qualifier suffix.
  // (Not projected: GPU samples are an instrumentation artefact, not a scenario.)
  const gpuParts = fmtEnergyParts(t.gpu);
  setKpi("kpiGPU", "kpiGPUUnit", gpuParts, t.gpu > 0 ? "  (Σ gpuKWh)" : "  (no GPU samples)");

  setKpi("kpiCO2", "kpiCO2Unit", fmtCarbonParts(displayedCO2), suffix);

  const meanSci = state.reports.reduce((acc, r) => acc + r.sci.value, 0) / n;
  setKpi("kpiSCI", "kpiSCIUnit", fmtCarbonParts(meanSci), " / run");
  setKpi("kpiSaved", "kpiSavedUnit", fmtCarbonParts(t.saved), " avoided");
}

function chartColors() {
  const cs = getComputedStyle(document.body);
  return {
    text: cs.color, grid: cs.getPropertyValue("--border").trim() || "#ccc",
    primary: cs.getPropertyValue("--primary").trim() || "#0c7c59",
    accent:  cs.getPropertyValue("--accent").trim()  || "#1f6feb",
    palette: ["#0c7c59","#1f6feb","#b86e00","#b3261e","#6f42c1","#0aa","#888"],
  };
}

function destroyCharts() {
  for (const k of Object.keys(state.charts)) { state.charts[k]?.destroy(); }
  state.charts = {};
}

function renderCharts() {
  if (!window.Chart) return;
  destroyCharts();
  const C = chartColors();
  Chart.defaults.color = C.text;
  Chart.defaults.borderColor = C.grid;

  const ordered = [...state.reports].sort((a, b) => new Date(a.run.startedAt) - new Date(b.run.startedAt));

  // Trend: SCI per run
  state.charts.trend = new Chart($("#chartTrend"), {
    type: "line",
    data: {
      labels: ordered.map(r => fmt.date(r.run.startedAt)),
      datasets: [{
        label: "SCI",
        data: ordered.map(r => r.sci.value),
        borderColor: C.primary, backgroundColor: C.primary + "33",
        fill: true, tension: 0.25, pointRadius: 4,
      }],
    },
    options: { responsive: true, maintainAspectRatio: false,
      scales: { y: { beginAtZero: true, ticks: { callback: (v) => fmtC(v) } } },
      plugins: {
        legend: { display: false },
        tooltip: { callbacks: { label: (ctx) => `SCI: ${fmtC(ctx.parsed.y)}` } },
      } },
  });

  // Breakdown: stacked bar of kWh per step, per run.
  // Scaled by the scenario multiplier (X runs/day × Y days/year).
  const mult = Math.max(0, (state.simulator.runsPerDay || 0) * (state.simulator.daysPerYear || 0)) || 1;
  const scaled = mult !== 1;
  const stepNames = [...new Set(ordered.flatMap(r => r.energy.byStep.map(s => s.name)))];

  // Stash the breakdown inputs; the chart is built lazily when the
  // accordion is opened (Chart.js can't measure a hidden <details>).
  state.breakdownCtx = { ordered, stepNames, mult, scaled, C };
  const meta = $("#breakdownSummaryMeta");
  if (meta) meta.textContent = `${state.reports.length} run(s) · ${stepNames.length} unique step(s)${scaled ? ` · scaled ×${mult.toLocaleString()}` : ""}`;
  const subBreakdown = $("#chartBreakdownSub");
  if (subBreakdown) subBreakdown.textContent = scaled ? ` ${mult.toLocaleString()} runs/yr` : "";
  if ($("#breakdownCard")?.open) renderBreakdownChart();

  // Carbon footprint: stacked bar per run — operational vs embodied gCO2eq.
  // Operational re-projected against the simulator's "what-if" zone when set,
  // and the whole stack scaled by the X×Y multiplier (gCO₂eq / year).
  const simZone = state.simulator.zone;
  const simI = simZone && ZONE_INTENSITY[simZone] != null ? ZONE_INTENSITY[simZone] : null;
  const opLabel = simI != null
    ? `Operational (Scope 2) — what-if ${simZone} @ ${simI} g/kWh`
    : "Operational (Scope 2)";
  state.charts.carbon = new Chart($("#chartCarbon"), {
    type: "bar",
    data: {
      // Short ≤8-char labels (repo basename, truncated) — full repo name
      // surfaces in the tooltip title below.
      labels: buildShortRunLabels(ordered),
      datasets: [
        {
          label: opLabel,
          backgroundColor: simI != null ? C.accent : C.primary,
          data: ordered.map(r => (simI != null
            ? (r.energy.totalKWh || 0) * simI
            : r.carbon.operationalGCO2eq) * mult),
        },
        {
          label: "Embodied (Scope 3.1)",
          backgroundColor: C.palette[2], // amber
          data: ordered.map(r => (r.carbon.embodiedGCO2eq || 0) * mult),
        },
      ],
    },
    options: {
      responsive: true, maintainAspectRatio: false,
      scales: {
        x: { stacked: true,
             ticks: { autoSkip: false, maxRotation: 0, minRotation: 0, font: { size: 11 } } },
        y: { stacked: true,
             title: { display: true, text: scaled ? "carbon / year" : "carbon" },
             beginAtZero: true,
             ticks: { callback: (v) => fmtC(v) } },
      },
      plugins: {
        legend: { position: "bottom" },
        tooltip: {
          callbacks: {
            // Title = full repository slug + run.id so hovering recovers
            // the information lost to the ≤8-char X-axis labels.
            title:  (items) => {
              const i = items[0]?.dataIndex;
              const r = ordered[i];
              if (!r) return "";
              const repo = r.run?.repository || "";
              const id   = r.run?.id ? ` · ${r.run.id}` : "";
              return repo + id;
            },
            label:  (ctx)   => `${ctx.dataset.label}: ${fmtC(ctx.parsed.y)}`,
            footer: (items) => {
              const total = items.reduce((s, it) => s + (it.parsed?.y || 0), 0);
              return `Total: ${fmtC(total)}`;
            },
          },
        },
      },
    },
  });
  const subCarbon = $("#chartCarbonSub");
  if (subCarbon) {
    const parts = ["operational + embodied"];
    if (simI != null) parts.push(`zone ${simZone}`);
    if (scaled)       parts.push(`× ${mult.toLocaleString()} runs/yr`);
    subCarbon.textContent = "(" + parts.join(" · ") + ")";
  }

  // Runner mix doughnut: share of *total energy* spent on each CPU arch.
  // Actionable signal — if x86 dominates, switching the matrix to arm64
  // runners (Graviton, Ampere, M-series) typically cuts ~30 % of the
  // energy footprint at iso-workload.
  //
  // Both runner-arch doughnuts live in <details> accordions closed by
  // default — Chart.js can't size a hidden canvas, so we stash the inputs
  // here and (re)build the chart on every accordion-open event.
  const ARCH_LABELS = {
    "x86_64":  "x86 (Intel/AMD)",
    "arm64":   "ARM (Graviton/Ampere/M-series)",
    "unknown": "unknown arch",
  };
  const archAgg = {};
  for (const r of state.reports) {
    const k = archKey(r);
    archAgg[k] = (archAgg[k] || 0) + r.energy.totalKWh;
  }
  const archKeys  = Object.keys(archAgg);
  const archTotal = archKeys.reduce((s, k) => s + archAgg[k], 0) || 1;

  // Carbon split — same fleet, weighted by each run's grid intensity.
  const archCarbonAgg = {};
  for (const r of state.reports) {
    const k = archKey(r);
    archCarbonAgg[k] = (archCarbonAgg[k] || 0) + (r.carbon.totalGCO2eq || 0);
  }
  const archCarbonKeys  = Object.keys(archCarbonAgg);
  const archCarbonTotal = archCarbonKeys.reduce((s, k) => s + archCarbonAgg[k], 0) || 1;

  // Stash inputs for the lazy chart builders, then refresh the summary
  // badges + verdicts (cheap, always visible — even when collapsed).
  state.runnerEnergyCtx = { keys: archKeys,        agg: archAgg,        total: archTotal,        labels: ARCH_LABELS, palette: C.palette };
  state.runnerCarbonCtx = { keys: archCarbonKeys,  agg: archCarbonAgg,  total: archCarbonTotal,  labels: ARCH_LABELS, palette: C.palette };

  renderRunnerEnergyBadgeAndVerdict();
  renderRunnerCarbonBadgeAndVerdict(archAgg, archTotal);

  if ($("#runnerEnergyCard")?.open) renderRunnerEnergyChart();
  if ($("#runnerCarbonCard")?.open) renderRunnerCarbonChart();

  // Repo leaderboard (gCO2eq totals)
  const repoAgg = {};
  for (const r of state.reports) {
    repoAgg[r.run.repository] = (repoAgg[r.run.repository] || 0) + r.carbon.totalGCO2eq;
  }
  const repos = Object.entries(repoAgg).sort((a, b) => b[1] - a[1]).slice(0, 10);
  state.charts.repo = new Chart($("#chartRepoLeader"), {
    type: "bar",
    data: { labels: repos.map(x => x[0]),
      datasets: [{ label: "carbon", data: repos.map(x => x[1]), backgroundColor: C.accent }] },
    options: { indexAxis: "y", responsive: true, maintainAspectRatio: false,
      scales: { x: { ticks: { callback: (v) => fmtC(v) } } },
      plugins: {
        legend: { display: false },
        tooltip: { callbacks: { label: (ctx) => fmtC(ctx.parsed.x) } },
      } },
  });
}

function renderTable() {
  const rows = state.reports
    .map(r => ({
      startedAt:  r.run.startedAt,
      repository: r.run.repository,
      workflow:   r.run.workflow,
      arch:       archKey(r),
      zone:       r.carbon.gridIntensity.zone,
      intensity:  r.carbon.gridIntensity.valueGCO2eqPerKWh,
      kwh:        r.energy.totalKWh,
      co2:        r.carbon.totalGCO2eq,
      sci:        r.sci.value,
      source:     dominantSource(r),
      meta:       r.metadata,
      cache:      r.cache,
    }))
    .filter(row => {
      if (!state.filter) return true;
      const blob = `${row.repository} ${row.workflow} ${row.zone} ${row.meta?.team || ""}`.toLowerCase();
      return blob.includes(state.filter.toLowerCase());
    })
    .sort((a, b) => {
      const k = state.sortKey, dir = state.sortAsc ? 1 : -1;
      const va = a[k], vb = b[k];
      if (typeof va === "number") return (va - vb) * dir;
      return String(va).localeCompare(String(vb)) * dir;
    });

  const tbody = $("#runsTable tbody");
  tbody.innerHTML = rows.map(r => `
    <tr>
      <td>${fmt.date(r.startedAt)}</td>
      <td>${r.repository}</td>
      <td>${r.workflow}</td>
      <td><span class="badge">${r.arch}</span></td>
      <td>${r.zone}</td>
      <td class="mono">${Math.round(r.intensity)} <span class="muted">g/kWh</span></td>
      <td class="mono">${fmtE(r.kwh)}</td>
      <td class="mono">${fmtC(r.co2)}</td>
      <td class="mono">${fmtC(r.sci)} ${r.cache?.hit ? '<span class="badge ok" title="cache hit">cache</span>' : ""}</td>
      <td><span class="badge">${r.source}</span></td>
    </tr>`).join("");

  // Sort indicators
  document.querySelectorAll("#runsTable thead th").forEach(th => {
    const k = th.dataset.sort;
    th.setAttribute("aria-sort", k === state.sortKey ? (state.sortAsc ? "ascending" : "descending") : "none");
  });

  // Refresh the accordion summary so the user sees the row count even when
  // the table is collapsed.
  const meta = $("#tableSummaryMeta");
  if (meta) {
    const total = state.reports.length;
    const shown = rows.length;
    meta.textContent = shown === total
      ? `${total} run(s)`
      : `${shown} / ${total} run(s) — filter “${state.filter}”`;
  }
}

// Build (or rebuild) the full-width stacked bar chart that powers the
// "Energy breakdown by step" accordion. Called on every accordion-open
// because Chart.js can't size itself inside a closed <details>.
function renderBreakdownChart() {
  if (!window.Chart || !state.breakdownCtx) return;
  const { ordered, stepNames, mult, scaled, C } = state.breakdownCtx;
  state.charts.breakdown?.destroy();

  // Give the canvas enough horizontal room: each run needs ~120 px to keep
  // its label legible. When the natural width exceeds the wrap, the parent
  // `.breakdown-canvas-wrap` scrolls horizontally (overflow-x: auto).
  const canvas = $("#chartBreakdown");
  const wrap = canvas?.parentElement;
  if (wrap) {
    const desiredPx = Math.max(wrap.clientWidth, ordered.length * 120);
    canvas.style.width = desiredPx + "px";
  }

  state.charts.breakdown = new Chart(canvas, {
    type: "bar",
    data: {
      labels: buildShortRunLabels(ordered),
      datasets: stepNames.map((name, i) => ({
        label: name,
        backgroundColor: C.palette[i % C.palette.length],
        data: ordered.map(r => ((r.energy.byStep.find(s => s.name === name)?.kWh) || 0) * mult),
      })),
    },
    options: {
      responsive: true, maintainAspectRatio: false,
      scales: {
        x: { stacked: true,
             ticks: { autoSkip: false, maxRotation: 0, minRotation: 0, font: { size: 11 } } },
        y: { stacked: true,
             title: { display: true, text: scaled ? "energy / year" : "energy" },
             ticks: { callback: (v) => fmtE(v) } },
      },
      plugins: {
        legend: { position: "bottom" },
        tooltip: {
          callbacks: {
            title: (items) => {
              const i = items[0]?.dataIndex;
              const r = ordered[i];
              return r ? (r.run?.repository || "") + (r.run?.id ? ` · ${r.run.id}` : "") : "";
            },
            label: (ctx) => `${ctx.dataset.label}: ${fmtE(ctx.parsed.y)}`,
          },
        },
      },
    },
  });
}

// ---------- Runner-arch accordions (lazy) ----------------------------------
// Both per-arch doughnuts live inside <details> closed by default.
// Chart.js can't size a hidden canvas, so we (re)build them on every open.

function renderRunnerEnergyChart() {
  if (!window.Chart || !state.runnerEnergyCtx) return;
  const { keys, agg, total, labels: NAMES, palette } = state.runnerEnergyCtx;
  state.charts.runner?.destroy();
  const data = keys.map(k => agg[k]);
  const labels = keys.map(k => `${NAMES[k] || k} — ${((agg[k] / total) * 100).toFixed(0)} %`);
  state.charts.runner = new Chart($("#chartRunnerMix"), {
    type: "doughnut",
    data: { labels, datasets: [{ data, backgroundColor: keys.map((_, i) => palette[i % palette.length]) }] },
    options: { responsive: true, maintainAspectRatio: false,
      plugins: {
        legend: { position: "bottom" },
        tooltip: { callbacks: {
          label: (ctx) => `${ctx.label.split(" — ")[0]}: ${fmtE(ctx.parsed)} (${((ctx.parsed / total) * 100).toFixed(1)} %)`,
        } },
      } },
  });
}

function renderRunnerCarbonChart() {
  if (!window.Chart || !state.runnerCarbonCtx) return;
  const { keys, agg, total, labels: NAMES, palette } = state.runnerCarbonCtx;
  state.charts.runnerCarbon?.destroy();
  const data = keys.map(k => agg[k]);
  const labels = keys.map(k => `${NAMES[k] || k} — ${((agg[k] / total) * 100).toFixed(0)} %`);
  state.charts.runnerCarbon = new Chart($("#chartRunnerCarbon"), {
    type: "doughnut",
    data: { labels, datasets: [{ data, backgroundColor: keys.map((_, i) => palette[i % palette.length]) }] },
    options: { responsive: true, maintainAspectRatio: false,
      plugins: {
        legend: { position: "bottom" },
        tooltip: { callbacks: {
          label: (ctx) => `${ctx.label.split(" — ")[0]}: ${fmtC(ctx.parsed)} (${((ctx.parsed / total) * 100).toFixed(1)} %)`,
        } },
      } },
  });
}

function renderRunnerEnergyBadgeAndVerdict() {
  const ctx = state.runnerEnergyCtx; if (!ctx) return;
  const { keys, agg, total, labels: NAMES } = ctx;
  const x86 = ((agg["x86_64"] || 0) / total) * 100;
  const arm = ((agg["arm64"]  || 0) / total) * 100;
  const badge = $("#runnerEnergyBadge");
  if (badge) badge.textContent = keys.length === 1
    ? `100 % ${NAMES[keys[0]] || keys[0]}`
    : `x86 ${x86.toFixed(0)} % · ARM ${arm.toFixed(0)} %`;
  const verdict = $("#runnerMixVerdict");
  if (verdict) {
    if (keys.length === 1)        verdict.textContent = `All runs on ${NAMES[keys[0]] || keys[0]}.`;
    else if (x86 >= 50)           verdict.textContent = `⚠ ${x86.toFixed(0)} % of energy spent on x86 — moving these jobs to ARM runners typically saves ~30 % kWh at iso-workload.`;
    else if (arm >= 50)           verdict.textContent = `✅ ${arm.toFixed(0)} % of energy already on ARM runners — good baseline.`;
    else                          verdict.textContent = `Mixed fleet (x86 ${x86.toFixed(0)} % · ARM ${arm.toFixed(0)} %).`;
  }
}

function renderRunnerCarbonBadgeAndVerdict(archAgg, archTotal) {
  const ctx = state.runnerCarbonCtx; if (!ctx) return;
  const { keys, agg, total, labels: NAMES } = ctx;
  const x86C = ((agg["x86_64"] || 0) / total) * 100;
  const armC = ((agg["arm64"]  || 0) / total) * 100;
  const x86E = ((archAgg["x86_64"] || 0) / archTotal) * 100;
  const gap = x86C - x86E;  // > 0  →  x86 over-contributes to carbon vs energy
  const badge = $("#runnerCarbonBadge");
  if (badge) badge.textContent = keys.length === 1
    ? `100 % ${NAMES[keys[0]] || keys[0]}`
    : `x86 ${x86C.toFixed(0)} % · ARM ${armC.toFixed(0)} %`;
  const verdict = $("#runnerCarbonVerdict");
  if (verdict) {
    if (keys.length === 1)            verdict.textContent = `All carbon on ${NAMES[keys[0]] || keys[0]}.`;
    else if (Math.abs(gap) >= 10) {
      const arch = gap > 0 ? "x86" : "ARM";
      verdict.textContent = `⚠ ${arch} runners over-contribute to carbon vs energy (${Math.abs(gap).toFixed(0)} pts gap) — they run on a dirtier grid; relocate them to a low-carbon region first.`;
    }
    else if (x86C >= 50)              verdict.textContent = `⚠ ${x86C.toFixed(0)} % of carbon emitted on x86 — switching to ARM cuts both energy and carbon proportionally.`;
    else if (armC >= 50)              verdict.textContent = `✅ ${armC.toFixed(0)} % of carbon already on ARM runners.`;
    else                              verdict.textContent = `Mixed carbon split (x86 ${x86C.toFixed(0)} % · ARM ${armC.toFixed(0)} %).`;
  }
}

// ---------- Export & sharing -----------------------------------------------
function exportCsv() {
  const headers = ["startedAt","repository","workflow","arch","zone","intensity_gCO2eqPerKWh","energy_kWh","total_gCO2eq","sci_gCO2eq","team","costCenter","source"];
  const lines = [headers.join(",")];
  for (const r of state.reports) {
    lines.push([
      r.run.startedAt, r.run.repository, r.run.workflow, archKey(r),
      r.carbon.gridIntensity.zone, r.carbon.gridIntensity.valueGCO2eqPerKWh,
      r.energy.totalKWh, r.carbon.totalGCO2eq, r.sci.value,
      r.metadata?.team || "", r.metadata?.costCenter || "", dominantSource(r),
    ].map(x => `"${String(x).replace(/"/g, '""')}"`).join(","));
  }
  const blob = new Blob([lines.join("\n")], { type: "text/csv" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url; a.download = `cienergy-${new Date().toISOString().slice(0,10)}.csv`;
  a.click(); URL.revokeObjectURL(url);
}

function shareLink() {
  const state64 = btoa(unescape(encodeURIComponent(JSON.stringify({
    sortKey: state.sortKey, sortAsc: state.sortAsc, filter: state.filter,
  }))));
  const url = `${location.origin}${location.pathname}#s=${state64}`;
  navigator.clipboard?.writeText(url).then(
    () => alert("Permalink copied to clipboard"),
    () => prompt("Copy this URL:", url),
  );
}

function restoreFromHash() {
  if (!location.hash.startsWith("#s=")) return;
  try {
    const s = JSON.parse(decodeURIComponent(escape(atob(location.hash.slice(3)))));
    Object.assign(state, s);
  } catch (e) { /* ignore */ }
}

// ---------- Scenario simulator ---------------------------------------------
/** Build per-report figures, optionally overriding the grid intensity. */
function projectReports(zoneOverride) {
  const useOverride = !!zoneOverride && ZONE_INTENSITY[zoneOverride] != null;
  const intensity = useOverride ? ZONE_INTENSITY[zoneOverride] : null;
  return state.reports.map(r => {
    const kwh = r.energy.totalKWh || 0;
    const embodied = r.carbon.embodiedGCO2eq || 0;
    const operational = useOverride
      ? kwh * intensity
      : r.carbon.operationalGCO2eq;
    return { kwh, embodied, operational, total: operational + embodied };
  });
}

/** Compute scenario aggregates (per-run mean + annual projection). */
function scenarioAggregate() {
  const { zone, runsPerDay, daysPerYear } = state.simulator;
  const projected   = projectReports(zone);
  const measured    = projectReports("");
  const n = projected.length || 1;
  const meanKwh = projected.reduce((s, r) => s + r.kwh, 0) / n;
  const meanCO2 = projected.reduce((s, r) => s + r.total, 0) / n;
  const measMeanCO2 = measured.reduce((s, r) => s + r.total, 0) / n;
  const runsPerYear = Math.max(0, Math.round((+runsPerDay || 0) * (+daysPerYear || 0)));
  const intensity = zone && ZONE_INTENSITY[zone] != null
    ? ZONE_INTENSITY[zone]
    : (state.reports.reduce((s, r) => s + (r.carbon.gridIntensity.valueGCO2eqPerKWh || 0), 0) / n);
  return {
    zone, runsPerDay: +runsPerDay || 0, daysPerYear: +daysPerYear || 0, runsPerYear,
    intensity,
    annualKwh: meanKwh * runsPerYear,
    annualCO2: meanCO2 * runsPerYear,
    measAnnualCO2: measMeanCO2 * runsPerYear,
    perRunMeanCO2: meanCO2,
  };
}

function renderSimulator() {
  const s = scenarioAggregate();
  $("#simMultiplier").textContent = s.runsPerYear.toLocaleString();
  setKpi("simKWh", "simKWhUnit", fmtEnergyParts(s.annualKwh));
  setKpi("simCO2", "simCO2Unit", fmtCarbonParts(s.annualCO2));
  $("#simIntensity").textContent = Math.round(s.intensity).toString();

  // Distance equivalent: scale to km / Mm / Gm as well.
  const km = s.annualCO2 / PETROL_CAR_GCO2_PER_KM;
  let kmVal, kmUnit;
  const a = Math.abs(km);
  if (!isFinite(km))      { kmVal = "—";              kmUnit = "km"; }
  else if (a >= 1e9)      { kmVal = (km/1e9).toFixed(2); kmUnit = "Gm (giga-metres)"; }
  else if (a >= 1e6)      { kmVal = (km/1e6).toFixed(2); kmUnit = "Mm (mega-metres)"; }
  else if (a >= 1e3)      { kmVal = (km/1e3).toFixed(2); kmUnit = "Mkm"; }
  else                    { kmVal = km.toFixed(km >= 10 ? 0 : 1); kmUnit = "km"; }
  $("#simEquiv").textContent = kmVal;
  $("#simEquivUnit").textContent = `${kmUnit} in an avg petrol car (170 g/km)`;

  // Delta vs measured zone
  const delta = $("#simDelta");
  if (!s.zone || s.measAnnualCO2 === 0) {
    delta.textContent = "baseline (measured zones)";
    delta.className = "sim-delta flat";
  } else {
    const pct = ((s.annualCO2 - s.measAnnualCO2) / s.measAnnualCO2) * 100;
    const sign = pct > 0 ? "▲ +" : pct < 0 ? "▼ " : "● ";
    delta.textContent = `${sign}${Math.abs(pct).toFixed(1)}% vs measured`;
    delta.className = "sim-delta " + (pct > 0.5 ? "up" : pct < -0.5 ? "down" : "flat");
  }

  // Accordion summary badge so the user sees the active scenario even when collapsed
  const badge = $("#simSummaryBadge");
  if (badge) {
    const zoneLabel = s.zone ? `${s.zone} @ ${Math.round(s.intensity)} g/kWh` : "measured zones";
    badge.textContent = `× ${s.runsPerYear.toLocaleString()} runs/yr · ${zoneLabel}`;
    badge.className = "badge " + (s.zone ? "ok" : "");
  }
}


function toggleTheme() {
  const cur = document.documentElement.getAttribute("data-theme");
  const next = cur === "dark" ? "light" : "dark";
  document.documentElement.setAttribute("data-theme", next);
  localStorage.setItem("cienergy.theme", next);
  if (state.reports.length) renderCharts();
}
function initTheme() {
  const saved = localStorage.getItem("cienergy.theme");
  if (saved) document.documentElement.setAttribute("data-theme", saved);
}

// ---------- Suggestions panel ----------------------------------------------
// Renders the per-report improvement suggestions emitted by the aggregator
// (see internal/suggest). Reports without a `suggestions` field (older runs)
// fall back to an empty list.
const SEVERITY_RANK = { critical: 0, major: 1, minor: 2, info: 3 };
const SEVERITY_LABEL = { critical: "critical", major: "major", minor: "minor", info: "info" };

function fmtSavingCarbon(g) {
  if (!isFinite(g) || g <= 0) return null;
  if (g >= 1000) return { v: (g / 1000).toFixed(2), u: "kg CO₂eq" };
  return { v: g.toFixed(2), u: "g CO₂eq" };
}
function fmtSavingEnergy(kwh) {
  if (!isFinite(kwh) || kwh <= 0) return null;
  if (kwh >= 1)    return { v: kwh.toFixed(3), u: "kWh" };
  if (kwh >= 1e-3) return { v: (kwh * 1000).toFixed(2), u: "Wh" };
  return { v: (kwh * 1e6).toFixed(0), u: "mWh" };
}
function escapeHTML(s) {
  return String(s ?? "").replace(/[&<>"']/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#39;"}[c]));
}

// projectSaving turns the per-run saving baked into a suggestion (computed by
// the aggregator against the report's original grid intensity) into a
// scenario-aware {kwh, co2, unit} triplet that reflects:
//   1. the active hosting zone (carbon is re-projected against ZONE_INTENSITY)
//   2. the runs/year scenario (multiplied by runsPerDay × daysPerYear when > 1)
// `unit` is "run" when the scenario is the trivial 1×1 / no projection case,
// or "yr" once the user has dialled in any X×Y > 1.
function projectSaving(s, report) {
  const baseKwh = s.estimatedSavingKWh || 0;
  const baseCo2 = s.estimatedSavingGCO2eq || 0;
  const zone = state.simulator.zone;
  const I = zone && ZONE_INTENSITY[zone] != null
    ? ZONE_INTENSITY[zone]
    : (baseKwh > 0
        ? baseCo2 / baseKwh
        : (report.carbon?.gridIntensity?.valueGCO2eqPerKWh || 0));
  const co2PerRun = baseKwh * I;
  const runsPerYr = Math.max(0,
    (state.simulator.runsPerDay  || 0) *
    (state.simulator.daysPerYear || 0));
  const projecting = runsPerYr > 1;
  return {
    kwh: projecting ? baseKwh   * runsPerYr : baseKwh,
    co2: projecting ? co2PerRun * runsPerYr : co2PerRun,
    unit: projecting ? "yr" : "run",
    runsPerYr, intensity: I, zone,
  };
}

function renderSuggestions() {
  const body = $("#suggestionsBody");
  const summary = $("#suggestionsSummary");
  if (!body) return;

  const minRank = SEVERITY_RANK[state.suggestionsMinSeverity] ?? SEVERITY_RANK.info;
  let totalGain = 0;
  let totalCount = 0;
  const blocks = [];

  // Probe once so the unit/zone suffix stay consistent across all blocks.
  const probe = projectSaving(
    { estimatedSavingKWh: 1, estimatedSavingGCO2eq: 1 },
    state.reports[0] || { carbon: { gridIntensity: { valueGCO2eqPerKWh: 0 } } },
  );
  const unit = probe.unit;
  const zoneSuffix = probe.zone ? ` · zone ${probe.zone} @ ${Math.round(probe.intensity)} g/kWh` : "";

  for (const r of state.reports) {
    const sugs = (r.suggestions || []).filter(s => (SEVERITY_RANK[s.severity] ?? 3) <= minRank);
    if (!sugs.length) continue;
    totalCount += sugs.length;

    let repoGain = 0;
    const items = sugs.map(s => {
      const proj = projectSaving(s, r);
      repoGain += proj.co2;
      const gC = fmtSavingCarbon(proj.co2);
      const eC = fmtSavingEnergy(proj.kwh);
      const savingHTML = gC
        ? `<div class="sugg-saving">~${gC.v}<small>${gC.u}/${proj.unit}${eC ? ` · ${eC.v} ${eC.u}/${proj.unit}` : ""}</small></div>`
        : `<div class="sugg-saving" aria-hidden="true"></div>`;
      const refHTML = s.reference
        ? ` <a href="${escapeHTML(s.reference)}" target="_blank" rel="noopener noreferrer">[docs]</a>`
        : "";
      return `
        <li class="sugg-item sev-${escapeHTML(s.severity || "info")}">
          <span class="sugg-sev">${escapeHTML(SEVERITY_LABEL[s.severity] || s.severity || "info")}</span>
          <div class="sugg-body">
            <strong>${escapeHTML(s.title)}</strong>
            <p>${escapeHTML(s.detail)}${refHTML}</p>
          </div>
          ${savingHTML}
        </li>`;
    }).join("");
    totalGain += repoGain;

    const repoGainParts = fmtSavingCarbon(repoGain);
    const headerRight = repoGainParts
      ? `<span class="sugg-repo-meta">potential save ~${repoGainParts.v} ${repoGainParts.u}/${unit}</span>`
      : "";
    blocks.push(`
      <div class="sugg-repo">
        <div class="sugg-repo-head">
          <div>
            <div class="sugg-repo-title">${escapeHTML(r.run.repository)}</div>
            <div class="sugg-repo-meta">${escapeHTML(r.run.workflow || "")} · ${sugs.length} suggestion(s)</div>
          </div>
          ${headerRight}
        </div>
        <ul class="sugg-list">${items}</ul>
      </div>`);
  }

  if (!blocks.length) {
    body.innerHTML = `<p class="sugg-empty">No suggestions at severity ≥ ${state.suggestionsMinSeverity}. 🎉</p>`;
    summary.textContent = "";
    return;
  }

  const totalParts = fmtSavingCarbon(totalGain);
  summary.textContent = totalParts
    ? `${totalCount} suggestion(s) across ${blocks.length} report(s) · potential save ~${totalParts.v} ${totalParts.u}/${unit}${zoneSuffix}`
    : `${totalCount} suggestion(s) across ${blocks.length} report(s)`;
  body.innerHTML = blocks.join("");
}

// ---------- Wiring ----------------------------------------------------------
function wire() {
  $("#filePicker").addEventListener("change", e => loadFiles([...e.target.files]));
  $("#dropPickBtn").addEventListener("click", () => $("#filePicker").click());
  $("#loadSamplesBtn").addEventListener("click", loadSamples);
  $("#dropSamplesBtn").addEventListener("click", loadSamples);
  $("#exportCsvBtn").addEventListener("click", exportCsv);
  $("#copyLinkBtn").addEventListener("click", shareLink);
  $("#themeBtn").addEventListener("click", toggleTheme);
  $("#filterInput").addEventListener("input", e => { state.filter = e.target.value; renderTable(); });
  // Auto-open the Run-details accordion when the user focuses the filter
  // input that lives in the summary (otherwise they'd be typing blind).
  $("#filterInput").addEventListener("focus", () => {
    const tc = $("#tableCard");
    if (tc && !tc.open) tc.open = true;
  });

  // Suggestions severity filter
  const sevEl = $("#suggestionsSeverity");
  if (sevEl) {
    sevEl.addEventListener("change", e => {
      state.suggestionsMinSeverity = e.target.value || "info";
      renderSuggestions();
    });
  }

  // Accordion: build the breakdown chart only when the user opens it.
  // Chart.js needs a visible (non-zero) container, so we (re)render on
  // every open event — also covers width changes after the user resizes.
  const breakdownEl = $("#breakdownCard");
  if (breakdownEl) {
    breakdownEl.addEventListener("toggle", () => {
      if (breakdownEl.open) renderBreakdownChart();
    });
  }
  // Same lazy pattern for the per-arch energy / carbon doughnuts.
  const rEnergyEl = $("#runnerEnergyCard");
  if (rEnergyEl) {
    rEnergyEl.addEventListener("toggle", () => {
      if (rEnergyEl.open) renderRunnerEnergyChart();
    });
  }
  const rCarbonEl = $("#runnerCarbonCard");
  if (rCarbonEl) {
    rCarbonEl.addEventListener("toggle", () => {
      if (rCarbonEl.open) renderRunnerCarbonChart();
    });
  }
  // Suggestions: rebuild on open so the latest state of the severity
  // filter and the latest report set are reflected without flicker.
  const suggEl = $("#suggestionsCard");
  if (suggEl) {
    suggEl.addEventListener("toggle", () => {
      if (suggEl.open) renderSuggestions();
    });
  }

  // Scenario simulator
  const onSimChange = () => {
    const zoneEl = $("#simZone"), runsEl = $("#simRuns"), daysEl = $("#simDays");
    state.simulator.zone        = zoneEl.value;
    state.simulator.runsPerDay  = Math.max(0, +runsEl.value || 0);
    state.simulator.daysPerYear = Math.max(0, Math.min(365, +daysEl.value || 0));
    if (state.reports.length) { renderKpis(); renderSimulator(); renderCharts(); renderSuggestions(); }
  };
  $("#simZone").addEventListener("change", onSimChange);
  $("#simRuns").addEventListener("input",  onSimChange);
  $("#simDays").addEventListener("input",  onSimChange);
  $("#simResetBtn").addEventListener("click", (e) => {
    e.preventDefault();   // don't toggle the <details>
    e.stopPropagation();
    state.simulator = { zone: "", runsPerDay: 1, daysPerYear: 365 };
    $("#simZone").value = ""; $("#simRuns").value = 1; $("#simDays").value = 365;
    if (state.reports.length) { renderKpis(); renderSimulator(); renderCharts(); renderSuggestions(); }
  });

  const dz = $("#dropZone");
  ["dragenter","dragover"].forEach(ev => dz.addEventListener(ev, e => { e.preventDefault(); dz.classList.add("dragover"); }));
  ["dragleave","drop"].forEach(ev => dz.addEventListener(ev, e => { e.preventDefault(); dz.classList.remove("dragover"); }));
  dz.addEventListener("drop", e => {
    const files = [...(e.dataTransfer?.files || [])].filter(f => f.name.endsWith(".json"));
    if (files.length) loadFiles(files);
  });
  // Also allow drag-drop on the whole page
  document.addEventListener("dragover", e => e.preventDefault());
  document.addEventListener("drop", e => {
    e.preventDefault();
    const files = [...(e.dataTransfer?.files || [])].filter(f => f.name.endsWith(".json"));
    if (files.length) loadFiles(files);
  });

  document.querySelectorAll("#runsTable thead th").forEach(th => {
    th.addEventListener("click", () => {
      const k = th.dataset.sort;
      if (state.sortKey === k) state.sortAsc = !state.sortAsc;
      else { state.sortKey = k; state.sortAsc = true; }
      renderTable();
    });
  });
}

// ---------- Boot ------------------------------------------------------------
initTheme();
restoreFromHash();
wire();
loadFromQuery();




