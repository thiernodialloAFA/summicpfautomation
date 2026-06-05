# Embedded HTML/JS/CSS dashboard

Zero-dependency single-page dashboard for cienergy reports.

## Run it

Three ways, no install:

1. **Direct** — just open `index.html` in any modern browser (Chrome/Edge/Firefox/Safari).
   *Note: due to browser file:// fetch restrictions, the "Load samples" button needs HTTP.
   Use option 2 or 3 for the bundled samples; drag-and-drop works either way.*

2. **One-shot HTTP server** — from this folder:
   ```sh
   python3 -m http.server 8000
   # open http://localhost:8000/
   ```

3. **GitHub Pages** — push this folder; it works as-is.

## Inputs accepted

| Source | How |
|---|---|
| Local files | drag-and-drop onto the page, or **📂 Load reports** |
| Bundled samples | **⭐ Load samples** (reads `sample-reports/index.json`) |
| Remote URL | `?src=https://…/index.json` or `?src=https://…/report.json` |

## Features

- **Summary cards**: runs, total kWh, total gCO₂eq, mean SCI, cache savings avoided.
- **Charts** (Chart.js 4): SCI trend, energy stacked by step, runner-arch mix, repo leaderboard.
- **Sortable / filterable table** with badges (arch, source, cache hit).
- **CSV export** of the current set.
- **Share view** — view state (sort, filter) is encoded in the URL hash.
- **Light / dark** via `prefers-color-scheme` + manual toggle persisted in `localStorage`.
- **Accessibility**: keyboard-navigable, ARIA labels, `prefers-reduced-motion` respected, WCAG 2.2 AA contrast.

## Vendoring Chart.js / Ajv for offline use (recommended for production)

By default the page loads Chart.js and Ajv from `cdn.jsdelivr.net`. For air-gapped
environments or stronger supply-chain guarantees, vendor them locally **and** pin
with [Subresource Integrity](https://developer.mozilla.org/en-US/docs/Web/Security/Subresource_Integrity)
hashes:

```sh
mkdir -p vendor
curl -L -o vendor/chart.umd.min.js   "https://cdn.jsdelivr.net/npm/chart.js@4.4.3/dist/chart.umd.min.js"
curl -L -o vendor/ajv7.bundle.min.js "https://cdn.jsdelivr.net/npm/ajv@8.17.1/dist/ajv7.bundle.min.js"

# Compute the SRI hashes:
for f in vendor/*.js; do
  printf "%-30s sha384-%s\n" "$f" "$(openssl dgst -sha384 -binary "$f" | openssl base64 -A)"
done
```

Then in `index.html` replace the two `<script src="https://...">` lines with:

```html
<script src="./vendor/chart.umd.min.js"   integrity="sha384-…" crossorigin="anonymous"></script>
<script src="./vendor/ajv7.bundle.min.js" integrity="sha384-…" crossorigin="anonymous"></script>
```

## Weight

| Asset            | Size (gzip) |
|------------------|-------------|
| `index.html`     | ~2 KB       |
| `app.css`        | ~2 KB       |
| `app.js`         | ~5 KB       |
| `chart.umd.min.js` (vendored)  | ~70 KB |
| `ajv7.bundle.min.js` (vendored) | ~40 KB |
| **Total (gzipped)** | **~120 KB** |

Uncompressed weight ≈ 360 KB. Largest contributor is Chart.js; if charts are not
needed, comment the `<script src=".../chart.umd.min.js">` line and the table-only
mode still works.

