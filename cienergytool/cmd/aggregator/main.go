// cienergy-aggregator reads a JSONL file of step samples and emits a
// SCI-compliant energy report (v1 schema) on stdout or to a file.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/axa-oss/cienergytool/internal/embodied"
	"github.com/axa-oss/cienergytool/internal/exporter/otlp"
	"github.com/axa-oss/cienergytool/internal/grid"
	"github.com/axa-oss/cienergytool/internal/model"
	"github.com/axa-oss/cienergytool/internal/probe/ecoci"
	"github.com/axa-oss/cienergytool/internal/probe/rapl"
	"github.com/axa-oss/cienergytool/internal/sci"
)

// stepSample is one line of the --steps-file JSONL input.
type stepSample struct {
	Name            string  `json:"name"`
	DurationSeconds float64 `json:"durationSeconds"`
	CPUUtilPct      float64 `json:"cpuUtilPct"`
	KWh             float64 `json:"kWh,omitempty"`    // already measured (rapl/kepler/nvml)
	Source          string  `json:"source,omitempty"` // override source
	GPUKWh          float64 `json:"gpuKWh,omitempty"`
}

func main() {
	// Auto-detect platform from environment so the same binary works for
	// GitHub Actions, Azure DevOps, GitLab CI and local runs.
	defPlatform, defRepo, defWorkflow, defRef, defCommit, defRunID, defOS, defArch := detectPlatform()

	var (
		start     = flag.String("start", "", "run start time (RFC3339); default: now")
		end       = flag.String("end", "", "run end time (RFC3339); default: now")
		platform  = flag.String("platform", defPlatform, "ci platform")
		repo      = flag.String("repo", defRepo, "repository slug")
		workflow  = flag.String("workflow", defWorkflow, "workflow / pipeline name")
		ref       = flag.String("ref", defRef, "git ref")
		commit    = flag.String("commit", defCommit, "commit sha")
		runID     = flag.String("run-id", defRunID, "run id")
		os_       = flag.String("os", defOS, "runner OS")
		arch      = flag.String("arch", defArch, "runner arch")
		cpuModel  = flag.String("cpu-model", "Intel Xeon Platinum 8370C", "CPU model")
		tdp       = flag.Float64("tdp", 270, "CPU TDP in watts")
		vcpu      = flag.Int("vcpu", 4, "vCPU count")
		ramGiB    = flag.Float64("ram", 16, "RAM in GiB")
		provider  = flag.String("provider", defaultProvider(defPlatform), "runner provider")
		region    = flag.String("region", "WORLD", "Electricity Maps zone code (e.g. FR, US-VA)")
		stepsFile = flag.String("steps-file", "", "JSONL file with step samples")
		out       = flag.String("out", "energy-report.json", "output JSON file ('-' for stdout)")
		emapsTok  = flag.String("emaps-token", envOr("CIENERGY_EMAPS_TOKEN", ""), "Electricity Maps token (optional)")
		team      = flag.String("team", envOr("CIENERGY_TEAM", ""), "team label")
		costCtr   = flag.String("cost-center", envOr("CIENERGY_COST_CENTER", ""), "cost-center label")
		funcUnit  = flag.String("functional-unit", "1 pipeline run", "SCI functional unit description")
		rValue    = flag.Float64("R", 1, "SCI R (functional unit count)")
		embodiedOverride = flag.Float64("embodied-gco2eq", -1, "override amortised embodied carbon for the run (gCO2eq); -1 = resolve via Boavizta")
		cacheHit  = flag.Bool("cache-hit", false, "mark this run as cache-hit")
		raplKWh   = flag.Float64("rapl-kwh", -1, "override total energy from an external RAPL measurement (kWh); -1 = ignore")
		otlpURL   = flag.String("otlp-endpoint", envOr("CIENERGY_OTLP_ENDPOINT", ""), "optional OTLP/HTTP-JSON base URL (POST to /v1/metrics)")
		otlpAuth  = flag.String("otlp-header", envOr("CIENERGY_OTLP_HEADER", ""), "optional 'Header: Value' pair to add to the OTLP request (e.g. for auth)")
	)
	flag.Parse()

	startT := parseTimeOr(*start, time.Now().UTC())
	endT := parseTimeOr(*end, time.Now().UTC())
	if endT.Before(startT) {
		endT = startT
	}

	// Defensive defaults: empty values from $(git rev-parse HEAD) when outside
	// a git repo, or from unset env vars, would produce an invalid report.
	if strings.TrimSpace(*commit) == "" {
		*commit = "0000000"
	}
	if strings.TrimSpace(*repo) == "" {
		*repo = "local/repo"
	}
	if strings.TrimSpace(*workflow) == "" {
		*workflow = "local"
	}

	steps, err := loadSteps(*stepsFile)
	check(err)

	// Energy aggregation.
	var totalKWh float64
	reportSteps := make([]model.Step, 0, len(steps))
	for _, s := range steps {
		kwh := s.KWh
		src := s.Source
		if kwh == 0 {
			kwh = ecoci.EstimateKWh(*tdp, s.CPUUtilPct, s.DurationSeconds)
			if src == "" {
				src = "eco-ci-model"
			}
		}
		if src == "" {
			src = "rapl"
		}
		kwh += s.GPUKWh
		totalKWh += kwh
		reportSteps = append(reportSteps, model.Step{
			Name:            s.Name,
			KWh:             round(kwh, 6),
			DurationSeconds: s.DurationSeconds,
			Source:          src,
			CPUUtilPct:      s.CPUUtilPct,
			GPUKWh:          s.GPUKWh,
		})
	}

	// Try RAPL probe automatically (Linux self-hosted) if no value provided
	// and no per-step energy was given. Quiet failure → keeps eco-ci fallback.
	if *raplKWh < 0 && totalKWh == 0 {
		if p, ok, err := rapl.Available(); err == nil && ok {
			if startErr := p.Start(); startErr == nil {
				// Tiny sample window — useful only if the caller wraps a step.
				// In practice the orchestrator calls --rapl-kwh with a pre-measured value.
				time.Sleep(50 * time.Millisecond)
				if v, _ := p.Sample(); v > 0 {
					totalKWh = v
					reportSteps = append(reportSteps, model.Step{Name: "rapl-instant", KWh: round(v, 6), Source: "rapl"})
				}
			}
		}
	}
	if *raplKWh >= 0 {
		totalKWh = *raplKWh
		// Replace synthetic single-step entry to make the source explicit.
		if len(reportSteps) == 0 {
			reportSteps = append(reportSteps, model.Step{Name: "rapl", KWh: round(*raplKWh, 6), Source: "rapl"})
		}
	}

	// Grid intensity.
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	gridR, err := grid.New(*emapsTok).Resolve(ctx, *region)
	check(err)

	// Embodied carbon (Boavizta with CCF-static fallback). Override via flag.
	embRes := embodied.Resolver{HTTPClient: &http.Client{Timeout: 6 * time.Second},
		BaseURL: "https://api.boavizta.org/v1", LifetimeYears: embodied.DefaultLifetimeYears}
	runDuration := endT.Sub(startT).Seconds()
	emb := embRes.Resolve(ctx, *cpuModel, runDuration)
	embodiedG := emb.GCO2eqForRun
	embSource := emb.Source
	if *embodiedOverride >= 0 {
		embodiedG = *embodiedOverride
		embSource = "user-override"
	}

	// SCI.
	op, total, sciVal := sci.Compute(totalKWh, gridR.ValueGCO2eqPerKWh, embodiedG, *rValue)

	r := model.Report{
		Schema:         model.SchemaURL,
		SpecVersion:    model.SpecVersion,
		SCISpecVersion: model.SCISpecVersion,
		Run: model.Run{
			ID:              firstNonEmpty(*runID, fmt.Sprintf("local-%d", time.Now().Unix())),
			Platform:        *platform,
			Repository:      *repo,
			Workflow:        *workflow,
			Ref:             *ref,
			CommitSha:       strings.ToLower(*commit),
			StartedAt:       startT,
			EndedAt:         endT,
			DurationSeconds: endT.Sub(startT).Seconds(),
		},
		Runner: model.Runner{
			OS: strings.ToLower(*os_), Arch: *arch, VCPU: *vcpu, RAMGiB: *ramGiB,
			CPUModel: *cpuModel, TDPWatts: *tdp, Provider: *provider, Region: *region,
		},
		Energy: model.Energy{TotalKWh: round(totalKWh, 6), ByStep: reportSteps},
		Carbon: model.Carbon{
			OperationalGCO2eq: round(op, 3),
			EmbodiedGCO2eq:    round(embodiedG, 3),
			TotalGCO2eq:       round(total, 3),
			GridIntensity: model.GridIntensity{
				ValueGCO2eqPerKWh: gridR.ValueGCO2eqPerKWh,
				Source:            gridR.Source,
				Zone:              gridR.Zone,
				Timestamp:         gridR.Timestamp,
			},
			EmbodiedSource: embSource,
		},
		SCI: model.SCI{Value: round(sciVal, 3), Unit: "gCO2eq", FunctionalUnit: *funcUnit, R: *rValue},
	}
	if *cacheHit {
		r.Cache = &model.Cache{Hit: true}
	}
	if *team != "" || *costCtr != "" {
		r.Metadata = &model.Metadata{Team: *team, CostCenter: *costCtr}
	}

	// Emit.
	buf, err := json.MarshalIndent(r, "", "  ")
	check(err)
	if *out == "-" {
		_, _ = os.Stdout.Write(buf)
		_, _ = os.Stdout.Write([]byte("\n"))
	} else {
		check(os.WriteFile(*out, buf, 0o644))
		fmt.Fprintf(os.Stderr, "wrote %s (SCI=%.3f gCO2eq, E=%.6f kWh, I=%.0f gCO2eq/kWh, source=%s)\n",
			*out, r.SCI.Value, r.Energy.TotalKWh, r.Carbon.GridIntensity.ValueGCO2eqPerKWh, r.Carbon.GridIntensity.Source)
	}

	// OTLP export (best-effort).
	if *otlpURL != "" {
		exp := otlp.New(*otlpURL)
		if *otlpAuth != "" {
			parts := strings.SplitN(*otlpAuth, ":", 2)
			if len(parts) == 2 {
				exp.Headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
		if err := exp.Export(ctx, &r); err != nil {
			fmt.Fprintf(os.Stderr, "warning: OTLP export failed: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "pushed OTLP metrics to %s/v1/metrics\n", *otlpURL)
		}
	}
}

func loadSteps(path string) ([]stepSample, error) {
	if path == "" {
		return nil, fmt.Errorf("--steps-file is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []stepSample
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var s stepSample
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			return nil, fmt.Errorf("invalid step JSON: %w", err)
		}
		out = append(out, s)
	}
	return out, sc.Err()
}

func envOr(k, def string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return def
}

// detectPlatform inspects well-known env vars and returns sensible defaults
// for: platform, repo, workflow, ref, commit, runID, os, arch.
// Detection order: Azure DevOps → GitHub Actions → GitLab CI → Jenkins → local.
func detectPlatform() (platform, repo, workflow, ref, commit, runID, osName, arch string) {
	switch {
	case os.Getenv("TF_BUILD") == "True", os.Getenv("AZURE_HTTP_USER_AGENT") != "":
		// Azure DevOps Pipelines
		// https://learn.microsoft.com/azure/devops/pipelines/build/variables
		platform = "azure-devops"
		repo     = envOr("BUILD_REPOSITORY_NAME", "local/repo")
		workflow = envOr("BUILD_DEFINITIONNAME", "pipeline")
		ref      = envOr("BUILD_SOURCEBRANCH", "")
		commit   = envOr("BUILD_SOURCEVERSION", "0000000")
		runID    = envOr("BUILD_BUILDID", "")
		osName   = envOr("AGENT_OS", "Linux")
		arch     = normalizeArch(envOr("AGENT_OSARCHITECTURE", "x86_64"))
	case os.Getenv("GITHUB_ACTIONS") == "true":
		platform = "github-actions"
		repo     = envOr("GITHUB_REPOSITORY", "local/repo")
		workflow = envOr("GITHUB_WORKFLOW", "ci")
		ref      = envOr("GITHUB_REF", "")
		commit   = envOr("GITHUB_SHA", "0000000")
		runID    = envOr("GITHUB_RUN_ID", "")
		osName   = envOr("RUNNER_OS", "Linux")
		arch     = normalizeArch(envOr("RUNNER_ARCH", "x86_64"))
	case os.Getenv("GITLAB_CI") == "true":
		platform = "gitlab-ci"
		repo     = envOr("CI_PROJECT_PATH", "local/repo")
		workflow = envOr("CI_PIPELINE_NAME", envOr("CI_JOB_NAME", "pipeline"))
		ref      = envOr("CI_COMMIT_REF_NAME", "")
		commit   = envOr("CI_COMMIT_SHA", "0000000")
		runID    = envOr("CI_PIPELINE_ID", "")
		osName   = envOr("CI_RUNNER_DESCRIPTION", "Linux")
		arch     = normalizeArch(envOr("CI_RUNNER_EXECUTABLE_ARCH", "amd64"))
	case os.Getenv("JENKINS_URL") != "":
		platform = "jenkins"
		repo     = envOr("GIT_URL", "local/repo")
		workflow = envOr("JOB_NAME", "pipeline")
		ref      = envOr("GIT_BRANCH", "")
		commit   = envOr("GIT_COMMIT", "0000000")
		runID    = envOr("BUILD_NUMBER", "")
		osName   = envOr("NODE_LABELS", "Linux")
		arch     = "x86_64"
	default:
		platform = envOr("CIENERGY_PLATFORM", "local")
		repo     = "local/repo"
		workflow = "local"
		commit   = "0000000"
		osName   = "Linux"
		arch     = "x86_64"
	}
	return
}

func defaultProvider(platform string) string {
	switch platform {
	case "azure-devops":
		return "azure-pipelines"
	case "gitlab-ci":
		return "gitlab-saas"
	case "jenkins":
		return "jenkins"
	case "local":
		return "local"
	default:
		return "github-hosted"
	}
}

func normalizeArch(a string) string {
	switch strings.ToLower(strings.TrimSpace(a)) {
	case "x86", "x64", "amd64", "x86_64":
		return "x86_64"
	case "arm", "arm64", "aarch64":
		return "arm64"
	default:
		return a
	}
}
func firstNonEmpty(a, b string) string { if a != "" { return a }; return b }
func parseTimeOr(s string, def time.Time) time.Time {
	if s == "" {
		return def
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return def
	}
	return t.UTC()
}
func round(v float64, decimals int) float64 {
	p := 1.0
	for i := 0; i < decimals; i++ {
		p *= 10
	}
	return float64(int64(v*p+0.5)) / p
}
func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

