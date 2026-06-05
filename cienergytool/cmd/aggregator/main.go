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

	"github.com/axa-oss/cienergytool/internal/cidetect"
	"github.com/axa-oss/cienergytool/internal/embodied"
	"github.com/axa-oss/cienergytool/internal/exporter/otlp"
	"github.com/axa-oss/cienergytool/internal/grid"
	"github.com/axa-oss/cienergytool/internal/model"
	"github.com/axa-oss/cienergytool/internal/probe/ecoci"
	"github.com/axa-oss/cienergytool/internal/probe/rapl"
	"github.com/axa-oss/cienergytool/internal/sci"
	"github.com/axa-oss/cienergytool/internal/suggest"
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
		repo      = flag.String("repo", defRepo, "repository slug; comma-separated for multi-repo runs (one report per repo will be written)")
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
	var repoPaths multiFlag
	flag.Var(&repoPaths, "repo-path", "map a repo slug to a local checkout: 'org/app=./path'. Repeatable. When set, the aggregator scans the path with cidetect and emits one *distinct* report per detected CI pipeline, replacing the shared --steps-file numbers.")
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

	// Multi-repo support: --repo accepts a comma-separated list. One report per
	// repository is emitted (same energy/runner numbers, distinct run.repository).
	repos := splitRepos(*repo)

	steps, err := loadSteps(*stepsFile)
	if err != nil && len(repoPaths) == 0 {
		// Only fatal when no per-repo path was provided to substitute.
		check(err)
	}

	// Build the list of (repo, pipeline, steps) tuples we need to emit a
	// report for. By default each repo gets one tuple sharing the global
	// --steps-file numbers (legacy "monorepo" behaviour). When --repo-path
	// is given for a repo, cidetect scans the local checkout and we emit one
	// *distinct* report per detected CI YAML, with steps derived from the
	// pipeline definition itself — different repos with different pipelines
	// finally produce different energy/carbon numbers.
	pathByRepo := repoPaths.parseMap()
	type job struct {
		repo         string
		workflowName string // name shown in the report (and used to slug filenames)
		pipelinePath string // optional, recorded as label
		platform     string // overrides --platform when detected
		steps        []stepSample
	}
	var jobs []job
	for _, repoName := range repos {
		if rp, ok := pathByRepo[repoName]; ok {
			pls, derr := cidetect.Detect(rp)
			if derr != nil {
				fmt.Fprintf(os.Stderr, "warning: cidetect %s: %v — falling back to --steps-file\n", repoName, derr)
			}
			if len(pls) == 0 {
				fmt.Fprintf(os.Stderr, "warning: no CI pipelines detected in %s for %s — using --steps-file\n", rp, repoName)
				jobs = append(jobs, job{repo: repoName, workflowName: *workflow, steps: steps})
				continue
			}
			for _, p := range pls {
				js := pipelineToSteps(p)
				jobs = append(jobs, job{
					repo:         repoName,
					workflowName: p.Name,
					pipelinePath: p.RelPath,
					platform:     p.Platform,
					steps:        js,
				})
			}
		} else {
			jobs = append(jobs, job{repo: repoName, workflowName: *workflow, steps: steps})
		}
	}
	if len(jobs) == 0 {
		check(fmt.Errorf("no jobs to process"))
	}

	// Energy aggregation is performed *per job* below (one job = one report).
	// We keep grid resolution + base RAPL probe at the top level because they
	// are run-wide (same runner, same time window).
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	gridR, err := grid.New(*emapsTok).Resolve(ctx, *region)
	check(err)

	// Try a one-shot RAPL probe (Linux self-hosted, when no per-step kWh given).
	var raplBaseKWh float64
	if *raplKWh < 0 {
		if p, ok, perr := rapl.Available(); perr == nil && ok {
			if startErr := p.Start(); startErr == nil {
				time.Sleep(50 * time.Millisecond)
				if v, _ := p.Sample(); v > 0 {
					raplBaseKWh = v
				}
			}
		}
	}

	embRes := embodied.Resolver{HTTPClient: &http.Client{Timeout: 6 * time.Second},
		BaseURL: "https://api.boavizta.org/v1", LifetimeYears: embodied.DefaultLifetimeYears}

	baseRunID := firstNonEmpty(*runID, fmt.Sprintf("local-%d", time.Now().Unix()))

	// Build one report per (repo, pipeline) tuple. Each tuple has its own
	// steps list, hence its own energy/carbon — fixing the previous bug where
	// every repo received an identical clone of the global --steps-file.
	for idx, j := range jobs {
		// 1. Per-job energy aggregation.
		var totalKWh float64
		reportSteps := make([]model.Step, 0, len(j.steps))
		for _, s := range j.steps {
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
		if *raplKWh >= 0 {
			totalKWh = *raplKWh
			if len(reportSteps) == 0 {
				reportSteps = append(reportSteps, model.Step{Name: "rapl", KWh: round(*raplKWh, 6), Source: "rapl"})
			}
		} else if raplBaseKWh > 0 && totalKWh == 0 {
			totalKWh = raplBaseKWh
			reportSteps = append(reportSteps, model.Step{Name: "rapl-instant", KWh: round(raplBaseKWh, 6), Source: "rapl"})
		}

		// 2. Effective wall duration for embodied amortisation: max(end-start,
		// sum(steps)). Without this, a caller passing only --start ends up with
		// embodied ≈ 0 g (regression observed before this fix).
		stepsDuration := 0.0
		for _, s := range j.steps {
			stepsDuration += s.DurationSeconds
		}
		effectiveDuration := endT.Sub(startT).Seconds()
		if stepsDuration > effectiveDuration {
			effectiveDuration = stepsDuration
		}
		jobEnd := endT
		if stepsDuration > endT.Sub(startT).Seconds() && stepsDuration > 0 {
			jobEnd = startT.Add(time.Duration(stepsDuration * float64(time.Second)))
		}

		emb := embRes.Resolve(ctx, *cpuModel, effectiveDuration)
		embodiedG := emb.GCO2eqForRun
		embSource := emb.Source
		if *embodiedOverride >= 0 {
			embodiedG = *embodiedOverride
			embSource = "user-override"
		}

		// 3. SCI.
		op, total, sciVal := sci.Compute(totalKWh, gridR.ValueGCO2eqPerKWh, embodiedG, *rValue)

		// 4. Distinct run.id per (repo, pipeline) so the ingester (idempotent
		// upsert on id) doesn't overwrite previous tuples.
		runIDForJob := baseRunID
		if len(jobs) > 1 {
			runIDForJob = fmt.Sprintf("%s-%s-%s", baseRunID, slugRepo(j.repo), slugRepo(j.workflowName))
		}

		platform := *platform
		if j.platform != "" {
			platform = j.platform
		}

		r := model.Report{
			Schema:         model.SchemaURL,
			SpecVersion:    model.SpecVersion,
			SCISpecVersion: model.SCISpecVersion,
			Run: model.Run{
				ID:              runIDForJob,
				Platform:        platform,
				Repository:      j.repo,
				Workflow:        j.workflowName,
				Ref:             *ref,
				CommitSha:       strings.ToLower(*commit),
				StartedAt:       startT,
				EndedAt:         jobEnd,
				DurationSeconds: jobEnd.Sub(startT).Seconds(),
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
		// Always populate metadata when the multi-repo or per-pipeline path is
		// used, so downstream tools can group runs.
		if *team != "" || *costCtr != "" || j.pipelinePath != "" || len(repos) > 1 {
			if r.Metadata == nil {
				r.Metadata = &model.Metadata{}
			}
			r.Metadata.Team = *team
			r.Metadata.CostCenter = *costCtr
			if r.Metadata.Labels == nil {
				r.Metadata.Labels = map[string]string{}
			}
			if j.pipelinePath != "" {
				r.Metadata.Labels["pipeline_path"] = j.pipelinePath
				r.Metadata.Labels["pipeline_source"] = "ci-detect"
			}
			if len(repos) > 1 {
				r.Metadata.Labels["repositories"] = strings.Join(repos, ",")
			}
		}

		// 5. Emit. Multi-pipeline output paths include both the repo *and* the
		// pipeline slug to avoid collisions when one repo has several workflows.
		r.Suggestions = suggest.For(&r)
		buf, err := json.MarshalIndent(r, "", "  ")
		check(err)
		outPath := resolveOutPathV2(*out, j.repo, j.workflowName, idx, len(jobs))
		if outPath == "-" {
			_, _ = os.Stdout.Write(buf)
			_, _ = os.Stdout.Write([]byte("\n"))
		} else {
			check(os.WriteFile(outPath, buf, 0o644))
			fmt.Fprintf(os.Stderr, "wrote %s (repo=%s, workflow=%s, SCI=%.3f gCO2eq, E=%.6f kWh, embodied=%.3f gCO2eq, I=%.0f gCO2eq/kWh, source=%s)\n",
				outPath, j.repo, j.workflowName, r.SCI.Value, r.Energy.TotalKWh, r.Carbon.EmbodiedGCO2eq, r.Carbon.GridIntensity.ValueGCO2eqPerKWh, r.Carbon.GridIntensity.Source)
		}

		// 6. OTLP export (best-effort).
		if *otlpURL != "" {
			exp := otlp.New(*otlpURL)
			if *otlpAuth != "" {
				parts := strings.SplitN(*otlpAuth, ":", 2)
				if len(parts) == 2 {
					exp.Headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
				}
			}
			if err := exp.Export(ctx, &r); err != nil {
				fmt.Fprintf(os.Stderr, "warning: OTLP export failed for %s/%s: %v\n", j.repo, j.workflowName, err)
			} else {
				fmt.Fprintf(os.Stderr, "pushed OTLP metrics for %s/%s to %s/v1/metrics\n", j.repo, j.workflowName, *otlpURL)
			}
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

// splitRepos parses a comma-separated repository list and returns the unique,
// non-empty, trimmed entries. Always returns at least one element.
func splitRepos(s string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, r := range strings.Split(s, ",") {
		r = strings.TrimSpace(r)
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	if len(out) == 0 {
		return []string{"local/repo"}
	}
	return out
}

// slugRepo turns "org/app name:v2" into "org_app_name_v2" — used to build a
// distinct run.id per repo and to suffix output filenames.
func slugRepo(repo string) string {
	return strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(strings.TrimSpace(repo))
}

// resolveOutPath returns the destination path for one report in a multi-repo
// run. Substitutes the literal token "{repo}" anywhere in the template; if the
// template contains no placeholder and there are several repos, the slugified
// repo name is inserted before the file extension to avoid overwriting.
func resolveOutPath(template, repo string, idx, total int) string {
	if template == "-" {
		return "-"
	}
	slug := slugRepo(repo)
	if strings.Contains(template, "{repo}") {
		return strings.ReplaceAll(template, "{repo}", slug)
	}
	if total <= 1 {
		return template
	}
	// Insert "-<slug>" before the extension. e.g. energy-report.json → energy-report-orgapp.json
	dot := strings.LastIndex(template, ".")
	if dot < 0 || dot < strings.LastIndex(template, "/") {
		return fmt.Sprintf("%s-%s", template, slug)
	}
	return template[:dot] + "-" + slug + template[dot:]
}
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

// multiFlag implements flag.Value to accept --repo-path multiple times.
// Each entry must be of the form "repo/slug=/abs/or/relative/path".
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(s string) error { *m = append(*m, s); return nil }
func (m multiFlag) parseMap() map[string]string {
	out := map[string]string{}
	for _, e := range m {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			fmt.Fprintf(os.Stderr, "warning: ignoring malformed --repo-path %q (expected slug=path)\n", e)
			continue
		}
		out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return out
}

// pipelineToSteps converts a cidetect.Pipeline into the stepSample shape
// consumed by the aggregator's energy loop. Source is preserved so the
// emitted report carries source="ci-detect-heuristic" on every step.
func pipelineToSteps(p cidetect.Pipeline) []stepSample {
	out := make([]stepSample, 0, len(p.Steps))
	for _, s := range p.Steps {
		out = append(out, stepSample{
			Name:            s.Name,
			DurationSeconds: s.DurationSeconds,
			CPUUtilPct:      s.CPUUtilPct,
			Source:          s.Source,
		})
	}
	return out
}

// resolveOutPathV2 picks an output filename per (repo, workflow) tuple. It
// supports the {repo} and {workflow} placeholders; falls back to inserting
// "-<repo>-<workflow>" before the extension when no placeholder is present
// and there is more than one tuple to emit.
func resolveOutPathV2(template, repo, workflow string, idx, total int) string {
	if template == "-" {
		return "-"
	}
	repoSlug := slugRepo(repo)
	wfSlug := slugRepo(workflow)
	if strings.Contains(template, "{repo}") || strings.Contains(template, "{workflow}") {
		s := strings.ReplaceAll(template, "{repo}", repoSlug)
		s = strings.ReplaceAll(s, "{workflow}", wfSlug)
		return s
	}
	if total <= 1 {
		return template
	}
	dot := strings.LastIndex(template, ".")
	if dot < 0 || dot < strings.LastIndex(template, "/") {
		return fmt.Sprintf("%s-%s-%s", template, repoSlug, wfSlug)
	}
	return template[:dot] + "-" + repoSlug + "-" + wfSlug + template[dot:]
}

