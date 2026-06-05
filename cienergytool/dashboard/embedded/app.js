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
};

const $ = (sel) => document.querySelector(sel);
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
    if (list) {
      const base = src.replace(/[^/]+$/, "");
      for (const name of list) {
        const j = await fetch(base + name).then(r => r.json());
        if (validateReport(j)) state.reports.push(j);
      }
    } else if (validateReport(head)) {
      state.reports.push(head);
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
    return acc;
  }, { kwh: 0, co2: 0, saved: 0 });
}

function archKey(r) {
  const a = (r.runner.arch || "").toLowerCase();
  if (a === "amd64" || a === "x86_64") return "x86_64";
  if (a === "aarch64") return "arm64";
  return a || "unknown";
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
  $("#charts").classList.toggle("hidden",  !hasData);
  $("#tableCard").classList.toggle("hidden", !hasData);
  $("#dropZone").classList.toggle("hidden", hasData);

  renderErrors();
  if (!hasData) return;

  renderKpis();
  renderCharts();
  renderTable();
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
  $("#kpiRuns").textContent  = state.reports.length;
  $("#kpiKWh").textContent   = fmt.kwh(t.kwh);
  $("#kpiCO2").textContent   = fmt.co2(t.co2);
  const meanSci = state.reports.reduce((s, r) => s + r.sci.value, 0) / state.reports.length;
  $("#kpiSCI").textContent   = fmt.co2(meanSci);
  $("#kpiSaved").textContent = fmt.co2(t.saved);
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
        label: "SCI (gCO₂eq)",
        data: ordered.map(r => r.sci.value),
        borderColor: C.primary, backgroundColor: C.primary + "33",
        fill: true, tension: 0.25, pointRadius: 4,
      }],
    },
    options: { responsive: true, maintainAspectRatio: false,
      scales: { y: { beginAtZero: true } }, plugins: { legend: { display: false } } },
  });

  // Breakdown: stacked bar of kWh per step, per run
  const stepNames = [...new Set(ordered.flatMap(r => r.energy.byStep.map(s => s.name)))];
  state.charts.breakdown = new Chart($("#chartBreakdown"), {
    type: "bar",
    data: {
      labels: ordered.map(r => r.run.id),
      datasets: stepNames.map((name, i) => ({
        label: name,
        backgroundColor: C.palette[i % C.palette.length],
        data: ordered.map(r => (r.energy.byStep.find(s => s.name === name)?.kWh) || 0),
      })),
    },
    options: { responsive: true, maintainAspectRatio: false,
      scales: { x: { stacked: true }, y: { stacked: true, title: { display: true, text: "kWh" } } },
      plugins: { legend: { position: "bottom" } } },
  });

  // Runner mix doughnut (by kWh)
  const archAgg = {};
  for (const r of state.reports) {
    const k = archKey(r);
    archAgg[k] = (archAgg[k] || 0) + r.energy.totalKWh;
  }
  state.charts.runner = new Chart($("#chartRunnerMix"), {
    type: "doughnut",
    data: { labels: Object.keys(archAgg),
      datasets: [{ data: Object.values(archAgg),
        backgroundColor: Object.keys(archAgg).map((_, i) => C.palette[i % C.palette.length]) }] },
    options: { responsive: true, maintainAspectRatio: false,
      plugins: { legend: { position: "bottom" } } },
  });

  // Repo leaderboard (gCO2eq totals)
  const repoAgg = {};
  for (const r of state.reports) {
    repoAgg[r.run.repository] = (repoAgg[r.run.repository] || 0) + r.carbon.totalGCO2eq;
  }
  const repos = Object.entries(repoAgg).sort((a, b) => b[1] - a[1]).slice(0, 10);
  state.charts.repo = new Chart($("#chartRepoLeader"), {
    type: "bar",
    data: { labels: repos.map(x => x[0]),
      datasets: [{ label: "gCO₂eq", data: repos.map(x => x[1]), backgroundColor: C.accent }] },
    options: { indexAxis: "y", responsive: true, maintainAspectRatio: false,
      plugins: { legend: { display: false } } },
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
      <td class="mono">${r.intensity}</td>
      <td class="mono">${fmt.kwh(r.kwh)}</td>
      <td class="mono">${fmt.co2(r.co2)}</td>
      <td class="mono">${fmt.co2(r.sci)} ${r.cache?.hit ? '<span class="badge ok" title="cache hit">cache</span>' : ""}</td>
      <td><span class="badge">${r.source}</span></td>
    </tr>`).join("");

  // Sort indicators
  document.querySelectorAll("#runsTable thead th").forEach(th => {
    const k = th.dataset.sort;
    th.setAttribute("aria-sort", k === state.sortKey ? (state.sortAsc ? "ascending" : "descending") : "none");
  });
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

// ---------- Theme -----------------------------------------------------------
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

