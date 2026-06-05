// cienergy-gate compares an energy report against a baseline and exits
// non-zero if the regression exceeds a configurable threshold.
//
// Designed for PR gating:
//
//	cienergy-gate \
//	  --current ./energy-report.json \
//	  --baseline ./base/energy-report.json \
//	  --max-increase-pct 25 \
//	  --warn-increase-pct 10
//
// Exit codes:
//   0  — within budget (or below the warn threshold).
//   1  — within warn..max → warning printed, build still passes.
//   2  — over max → fail the build.
//   64 — bad inputs / IO error.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/axa-oss/cienergytool/internal/model"
)

func main() {
	var (
		curPath    = flag.String("current", "", "current energy-report.json (required)")
		basePath   = flag.String("baseline", "", "baseline energy-report.json (required)")
		maxPct     = flag.Float64("max-increase-pct", 25, "fail if SCI grows by more than this %")
		warnPct    = flag.Float64("warn-increase-pct", 10, "print a warning past this % (must be <= max)")
		metric     = flag.String("metric", "sci", "comparison metric: sci | kwh | co2")
		format     = flag.String("format", "text", "output format: text | json | gh-summary")
	)
	flag.Parse()
	if *curPath == "" || *basePath == "" {
		fmt.Fprintln(os.Stderr, "error: --current and --baseline are required")
		os.Exit(64)
	}

	cur, err := loadReport(*curPath)
	if err != nil { die(err) }
	base, err := loadReport(*basePath)
	if err != nil { die(err) }

	c, b := extract(cur, *metric), extract(base, *metric)
	var deltaPct float64
	if b > 0 {
		deltaPct = (c - b) / b * 100.0
	}

	verdict := "ok"
	exit := 0
	switch {
	case deltaPct > *maxPct:
		verdict, exit = "fail", 2
	case deltaPct > *warnPct:
		verdict, exit = "warn", 1
	}

	switch *format {
	case "json":
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"metric":         *metric,
			"baselineValue":  b,
			"currentValue":   c,
			"deltaPct":       round(deltaPct, 2),
			"maxIncreasePct": *maxPct,
			"warnIncreasePct": *warnPct,
			"verdict":        verdict,
		})
	case "gh-summary":
		emoji := map[string]string{"ok": "✅", "warn": "⚠️", "fail": "❌"}[verdict]
		fmt.Printf("## cienergy gate %s\n\n", emoji)
		fmt.Printf("| metric | baseline | current | delta | budget |\n|---|---|---|---|---|\n")
		fmt.Printf("| %s | %.3f | %.3f | **%+.1f%%** | ≤ %.1f%% |\n",
			*metric, b, c, deltaPct, *maxPct)
	default:
		fmt.Printf("cienergy-gate: metric=%s baseline=%.3f current=%.3f delta=%+.2f%% verdict=%s (warn=%.1f%% max=%.1f%%)\n",
			*metric, b, c, deltaPct, verdict, *warnPct, *maxPct)
	}
	os.Exit(exit)
}

func loadReport(p string) (*model.Report, error) {
	data, err := os.ReadFile(p)
	if err != nil { return nil, err }
	var r model.Report
	if err := json.Unmarshal(data, &r); err != nil { return nil, fmt.Errorf("%s: %w", p, err) }
	return &r, nil
}

func extract(r *model.Report, metric string) float64 {
	switch metric {
	case "kwh": return r.Energy.TotalKWh
	case "co2": return r.Carbon.TotalGCO2eq
	default:    return r.SCI.Value
	}
}

func die(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(64) }

func round(v float64, decimals int) float64 {
	p := 1.0
	for i := 0; i < decimals; i++ { p *= 10 }
	return float64(int64(v*p+0.5)) / p
}

