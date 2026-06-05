// cienergy-cidetect scans a repository for CI/build YAML files (GitHub
// Actions, GitLab CI, Azure Pipelines, Jenkins, Tekton) and prints, for
// each detected pipeline, a JSONL steps file ready to be consumed by
// `cienergy-aggregator --steps-file`.
//
// Usage:
//
//	cienergy-cidetect --repo ./path/to/repo                # human summary
//	cienergy-cidetect --repo ./path --jsonl                # all pipelines, one JSONL stream
//	cienergy-cidetect --repo ./path --out steps/{slug}.jsonl
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/axa-oss/cienergytool/internal/cidetect"
)

func main() {
	var (
		repo  = flag.String("repo", ".", "path to a repository to scan")
		jsonl = flag.Bool("jsonl", false, "emit JSONL steps for the *first* pipeline on stdout")
		out   = flag.String("out", "", "write per-pipeline JSONL files. Supports {slug} placeholder.")
		quiet = flag.Bool("quiet", false, "suppress the human-readable summary")
	)
	flag.Parse()

	pls, err := cidetect.Detect(*repo)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if len(pls) == 0 {
		fmt.Fprintf(os.Stderr, "no CI pipelines detected in %s\n", *repo)
		os.Exit(2)
	}

	if !*quiet {
		fmt.Fprintf(os.Stderr, "detected %d pipeline(s) in %s:\n", len(pls), *repo)
		for _, p := range pls {
			fmt.Fprintf(os.Stderr, "  - %s [%s] %d steps, ~%.0fs total\n",
				p.RelPath, p.Platform, len(p.Steps), p.TotalDurationSec())
		}
	}

	switch {
	case *out != "":
		for _, p := range pls {
			path := strings.ReplaceAll(*out, "{slug}", slug(p.RelPath))
			if !strings.Contains(*out, "{slug}") && len(pls) > 1 {
				path = withSuffix(*out, "-"+slug(p.RelPath))
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				fail(err)
			}
			f, err := os.Create(path)
			if err != nil {
				fail(err)
			}
			writeJSONL(f, p)
			_ = f.Close()
			fmt.Fprintf(os.Stderr, "  → %s\n", path)
		}
	case *jsonl:
		writeJSONL(os.Stdout, pls[0])
	default:
		// Default: pretty-print all pipelines to stdout for human inspection.
		_ = json.NewEncoder(os.Stdout).Encode(pls)
	}
}

func writeJSONL(w *os.File, p cidetect.Pipeline) {
	enc := json.NewEncoder(w)
	for _, s := range p.Steps {
		_ = enc.Encode(map[string]any{
			"name":            s.Name,
			"durationSeconds": s.DurationSeconds,
			"cpuUtilPct":      s.CPUUtilPct,
			"source":          s.Source,
		})
	}
}

func slug(s string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", " ", "_", ":", "_", ".", "_")
	return strings.Trim(r.Replace(s), "_")
}

func withSuffix(path, suffix string) string {
	dot := strings.LastIndex(path, ".")
	if dot < 0 {
		return path + suffix
	}
	return path[:dot] + suffix + path[dot:]
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

