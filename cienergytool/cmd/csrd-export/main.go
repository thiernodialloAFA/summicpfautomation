// cienergy-csrd-export reads one or many cienergy energy-report.json files
// (or a directory of them) and emits a CSV ready for CSRD / ESRS E1 climate
// reporting (EU Directive 2022/2464).
//
// Each row aggregates the runs at a chosen granularity (run | day | month |
// repository | team | cost-center) and maps energy/carbon values to the
// canonical GHG Protocol scope buckets used by the ESRS E1-6 disclosure:
//
//   - Scope 2 (location-based)                  ← operational gCO2eq
//   - Scope 3, Category 1 (purchased goods)     ← embodied gCO2eq (amortised hw)
//
// CI runners are not Scope 1 (no direct combustion). PPAs of cloud providers
// are NOT netted out — this is the conservative location-based view as
// recommended by the GHG Protocol Scope 2 Guidance (2015).
//
//	cienergy-csrd-export --in ./reports/ --period 2025-Q4 --by team --out csrd.csv
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/axa-oss/cienergytool/internal/model"
)

func main() {
	var (
		inPath = flag.String("in", "", "directory or single .json file (required)")
		out    = flag.String("out", "csrd.csv", "output CSV path ('-' for stdout)")
		period = flag.String("period", "", "free-text reporting period, copied into every row (e.g. 2025-Q4)")
		by     = flag.String("by", "run", "aggregation: run | day | month | repository | team | cost-center")
		entity = flag.String("entity", "AXA SA", "reporting entity name")
		method = flag.String("method", "location-based", "GHG Scope 2 method: location-based | market-based")
	)
	flag.Parse()
	if *inPath == "" {
		fmt.Fprintln(os.Stderr, "error: --in is required")
		os.Exit(2)
	}

	reports, err := loadAll(*inPath)
	if err != nil { die(err) }
	if len(reports) == 0 { die(fmt.Errorf("no valid reports under %s", *inPath)) }

	rows := aggregate(reports, *by)

	var w *csv.Writer
	if *out == "-" {
		w = csv.NewWriter(os.Stdout)
	} else {
		f, err := os.Create(*out)
		if err != nil { die(err) }
		defer f.Close()
		w = csv.NewWriter(f)
	}
	defer w.Flush()

	// Header — column names aligned with EFRAG ESRS E1-6 disclosure tables.
	_ = w.Write([]string{
		"reporting_entity", "reporting_period", "scope2_method", "aggregation_key",
		"runs_count",
		"energy_kwh",
		"scope2_gco2eq_location_based",
		"scope3_cat1_gco2eq_embodied",
		"total_gco2eq",
		"sci_mean_gco2eq_per_run",
		"esrs_e1_6_reference",
	})
	for _, r := range rows {
		_ = w.Write([]string{
			*entity, *period, *method, r.Key,
			fmt.Sprintf("%d", r.N),
			fmt.Sprintf("%.6f", r.KWh),
			fmt.Sprintf("%.3f", r.OperationalCO2),
			fmt.Sprintf("%.3f", r.EmbodiedCO2),
			fmt.Sprintf("%.3f", r.TotalCO2),
			fmt.Sprintf("%.3f", r.MeanSCI),
			"ESRS E1-6 (gross Scopes 1,2,3 + total GHG)",
		})
	}
	if *out != "-" {
		fmt.Fprintf(os.Stderr, "wrote %s (%d rows, by=%s)\n", *out, len(rows), *by)
	}
}

type agg struct {
	Key            string
	N              int
	KWh            float64
	OperationalCO2 float64
	EmbodiedCO2    float64
	TotalCO2       float64
	SCISum         float64
}

func (a *agg) MeanSCI() float64 {
	if a.N == 0 { return 0 }
	return a.SCISum / float64(a.N)
}

// Wrap agg with computed mean for the row layout.
type aggRow struct {
	Key            string
	N              int
	KWh            float64
	OperationalCO2 float64
	EmbodiedCO2    float64
	TotalCO2       float64
	MeanSCI        float64
}

func aggregate(reports []*model.Report, by string) []aggRow {
	buckets := map[string]*agg{}
	for _, r := range reports {
		k := key(r, by)
		b := buckets[k]
		if b == nil { b = &agg{Key: k}; buckets[k] = b }
		b.N++
		b.KWh += r.Energy.TotalKWh
		b.OperationalCO2 += r.Carbon.OperationalGCO2eq
		b.EmbodiedCO2    += r.Carbon.EmbodiedGCO2eq
		b.TotalCO2       += r.Carbon.TotalGCO2eq
		b.SCISum         += r.SCI.Value
	}
	keys := make([]string, 0, len(buckets))
	for k := range buckets { keys = append(keys, k) }
	sort.Strings(keys)
	out := make([]aggRow, 0, len(keys))
	for _, k := range keys {
		b := buckets[k]
		out = append(out, aggRow{Key: k, N: b.N, KWh: round(b.KWh, 6),
			OperationalCO2: round(b.OperationalCO2, 3),
			EmbodiedCO2:    round(b.EmbodiedCO2, 3),
			TotalCO2:       round(b.TotalCO2, 3),
			MeanSCI:        round(b.MeanSCI(), 3),
		})
	}
	return out
}

func key(r *model.Report, by string) string {
	switch by {
	case "day":         return r.Run.StartedAt.UTC().Format("2006-01-02")
	case "month":       return r.Run.StartedAt.UTC().Format("2006-01")
	case "repository":  return r.Run.Repository
	case "team":
		if r.Metadata != nil && r.Metadata.Team != "" { return r.Metadata.Team }
		return "(unspecified)"
	case "cost-center":
		if r.Metadata != nil && r.Metadata.CostCenter != "" { return r.Metadata.CostCenter }
		return "(unspecified)"
	default: return r.Run.ID
	}
}

func loadAll(p string) ([]*model.Report, error) {
	info, err := os.Stat(p)
	if err != nil { return nil, err }
	var paths []string
	if info.IsDir() {
		_ = filepath.Walk(p, func(path string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() || !strings.HasSuffix(path, ".json") { return nil }
			if strings.HasSuffix(path, "/index.json") { return nil }
			paths = append(paths, path)
			return nil
		})
	} else {
		paths = []string{p}
	}
	out := make([]*model.Report, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil { continue }
		var r model.Report
		if err := json.Unmarshal(data, &r); err != nil { continue }
		if r.Run.ID == "" { continue }
		// Defensive: skip rows with impossible timestamps.
		if r.Run.StartedAt.IsZero() || r.Run.StartedAt.After(time.Now().Add(24*time.Hour)) { continue }
		out = append(out, &r)
	}
	return out, nil
}

func die(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }

func round(v float64, decimals int) float64 {
	p := 1.0
	for i := 0; i < decimals; i++ { p *= 10 }
	return float64(int64(v*p+0.5)) / p
}

