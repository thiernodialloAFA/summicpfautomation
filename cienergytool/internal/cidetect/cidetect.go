// Package cidetect scans a repository for CI/build definitions (GitHub
// Actions, GitLab CI, Azure Pipelines, Jenkins, Tekton) and turns each
// detected pipeline into a list of measurable steps with heuristic
// (durationSeconds, cpuUtilPct) so the cienergy aggregator can produce
// a *distinct* energy/carbon report per pipeline file — instead of the
// historical "monorepo mode" that copied the same numbers onto every repo.
//
// Heuristics are intentionally conservative; the goal is to differentiate
// repos that have e.g. 3 jobs × 8 steps from repos that just run a one-line
// lint, *not* to pretend we measured the actual runtime. Every emitted step
// records source = "ci-detect-heuristic" so downstream tools can flag it.
package cidetect

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Step is one measurable build step extracted from a pipeline file.
type Step struct {
	Name            string  `json:"name"`
	DurationSeconds float64 `json:"durationSeconds"`
	CPUUtilPct      float64 `json:"cpuUtilPct"`
	Kind            string  `json:"kind"`             // checkout|setup|build|test|lint|docker|deploy|other
	Source          string  `json:"source,omitempty"` // always "ci-detect-heuristic"
}

// Pipeline is one detected CI definition file (one workflow / one job-list).
type Pipeline struct {
	Path     string // absolute path of the YAML file
	RelPath  string // path relative to the repo root
	Platform string // github-actions | gitlab-ci | azure-devops | jenkins | tekton
	Name     string // human-friendly name (workflow name or filename)
	Steps    []Step
}

// TotalDurationSec returns the sum of all step durations.
func (p Pipeline) TotalDurationSec() float64 {
	t := 0.0
	for _, s := range p.Steps {
		t += s.DurationSeconds
	}
	return t
}

// Detect scans repoPath for CI files and returns one Pipeline per file.
// Files that fail to parse are skipped silently (the caller can re-scan
// with Verbose=true on a *Detector to surface them).
func Detect(repoPath string) ([]Pipeline, error) {
	return (&Detector{}).Scan(repoPath)
}

// Detector exposes Scan with optional knobs.
type Detector struct {
	// MaxFiles caps the number of YAML files inspected per repo. 0 = no limit.
	MaxFiles int
	// Verbose reports parse errors via Errors.
	Verbose bool
	Errors  []error
}

// Scan walks repoPath looking for known CI file patterns and parses them.
func (d *Detector) Scan(repoPath string) ([]Pipeline, error) {
	info, err := os.Stat(repoPath)
	if err != nil {
		return nil, fmt.Errorf("cidetect: stat %q: %w", repoPath, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("cidetect: %q is not a directory", repoPath)
	}

	var out []Pipeline
	count := 0

	add := func(p Pipeline) {
		if d.MaxFiles > 0 && count >= d.MaxFiles {
			return
		}
		out = append(out, p)
		count++
	}

	// 1. GitHub Actions: .github/workflows/*.{yml,yaml}
	ghDir := filepath.Join(repoPath, ".github", "workflows")
	if entries, err := os.ReadDir(ghDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext != ".yml" && ext != ".yaml" {
				continue
			}
			full := filepath.Join(ghDir, e.Name())
			p, err := parseGitHubWorkflow(full, repoPath)
			if err != nil {
				if d.Verbose {
					d.Errors = append(d.Errors, err)
				}
				continue
			}
			add(p)
		}
	}

	// 2. GitLab CI: .gitlab-ci.yml at repo root.
	if p, err := parseGitLabCI(filepath.Join(repoPath, ".gitlab-ci.yml"), repoPath); err == nil && p.Path != "" {
		add(p)
	}

	// 3. Azure Pipelines: azure-pipelines.yml + variants.
	for _, name := range []string{"azure-pipelines.yml", "azure-pipelines.yaml", ".azure-pipelines.yml"} {
		if p, err := parseAzurePipeline(filepath.Join(repoPath, name), repoPath); err == nil && p.Path != "" {
			add(p)
		}
	}

	// 4. Jenkinsfile (treated as a single opaque "build" step).
	for _, name := range []string{"Jenkinsfile", "jenkinsfile"} {
		full := filepath.Join(repoPath, name)
		if st, err := os.Stat(full); err == nil && !st.IsDir() {
			add(Pipeline{
				Path: full, RelPath: name, Platform: "jenkins", Name: name,
				Steps: defaultJenkinsSteps(),
			})
			break
		}
	}

	// 5. Tekton: tekton/*.yaml or .tekton/*.yaml — best-effort, treated as one task.
	for _, dir := range []string{"tekton", ".tekton"} {
		full := filepath.Join(repoPath, dir)
		if entries, err := os.ReadDir(full); err == nil {
			for _, e := range entries {
				ext := strings.ToLower(filepath.Ext(e.Name()))
				if e.IsDir() || (ext != ".yml" && ext != ".yaml") {
					continue
				}
				p := Pipeline{
					Path: filepath.Join(full, e.Name()), RelPath: filepath.Join(dir, e.Name()),
					Platform: "tekton", Name: e.Name(),
					Steps: defaultTektonSteps(),
				}
				add(p)
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, nil
}

// ---- GitHub Actions parser ------------------------------------------------

// ghWorkflow is the minimal subset of a GH Actions workflow YAML we care about.
type ghWorkflow struct {
	Name string                 `yaml:"name"`
	Jobs map[string]ghJobOrCall `yaml:"jobs"`
}
type ghJobOrCall struct {
	Name  string   `yaml:"name"`
	Uses  string   `yaml:"uses"` // reusable workflow call → modelled as one step
	Steps []ghStep `yaml:"steps"`
}
type ghStep struct {
	Name string `yaml:"name"`
	Uses string `yaml:"uses"`
	Run  string `yaml:"run"`
	If   string `yaml:"if"`
}

func parseGitHubWorkflow(path, repoRoot string) (Pipeline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Pipeline{}, err
	}
	var wf ghWorkflow
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		return Pipeline{}, fmt.Errorf("cidetect: %s: %w", path, err)
	}
	rel, _ := filepath.Rel(repoRoot, path)
	name := wf.Name
	if name == "" {
		name = filepath.Base(path)
	}

	var steps []Step
	// Iterate jobs in deterministic alphabetical order.
	jobIDs := make([]string, 0, len(wf.Jobs))
	for k := range wf.Jobs {
		jobIDs = append(jobIDs, k)
	}
	sort.Strings(jobIDs)
	for _, jid := range jobIDs {
		job := wf.Jobs[jid]
		jobLabel := job.Name
		if jobLabel == "" {
			jobLabel = jid
		}
		// Reusable workflow call: model as one "build" step.
		if job.Uses != "" && len(job.Steps) == 0 {
			steps = append(steps, classify(jobLabel+" (call "+job.Uses+")", "uses:"+job.Uses, ""))
			continue
		}
		for _, st := range job.Steps {
			label := st.Name
			if label == "" {
				if st.Uses != "" {
					label = jobLabel + " · " + st.Uses
				} else if st.Run != "" {
					label = jobLabel + " · run"
				} else {
					label = jobLabel
				}
			} else {
				label = jobLabel + " · " + label
			}
			steps = append(steps, classify(label, st.Uses, st.Run))
		}
	}
	return Pipeline{Path: path, RelPath: rel, Platform: "github-actions", Name: name, Steps: steps}, nil
}

// ---- GitLab CI parser -----------------------------------------------------

func parseGitLabCI(path, repoRoot string) (Pipeline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Pipeline{}, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return Pipeline{}, fmt.Errorf("cidetect: %s: %w", path, err)
	}
	rel, _ := filepath.Rel(repoRoot, path)
	var steps []Step
	// Reserved top-level keys to skip when iterating jobs.
	reserved := map[string]bool{
		"stages": true, "variables": true, "default": true, "include": true,
		"workflow": true, "image": true, "services": true, "before_script": true,
		"after_script": true, "cache": true, "pages": true, "types": true,
	}
	jobNames := make([]string, 0, len(doc))
	for k := range doc {
		if reserved[k] || strings.HasPrefix(k, ".") { // .hidden jobs are templates
			continue
		}
		jobNames = append(jobNames, k)
	}
	sort.Strings(jobNames)
	for _, jn := range jobNames {
		jobDef, ok := doc[jn].(map[string]any)
		if !ok {
			continue
		}
		// Combine before_script, script, after_script — each entry is one shell step.
		for _, key := range []string{"before_script", "script", "after_script"} {
			s, _ := jobDef[key].([]any)
			for i, line := range s {
				cmd, _ := line.(string)
				steps = append(steps, classify(fmt.Sprintf("%s · %s[%d]", jn, key, i), "", cmd))
			}
		}
	}
	if len(steps) == 0 {
		return Pipeline{}, fmt.Errorf("no jobs found in %s", path)
	}
	return Pipeline{Path: path, RelPath: rel, Platform: "gitlab-ci", Name: filepath.Base(path), Steps: steps}, nil
}

// ---- Azure Pipelines parser -----------------------------------------------

type azPipeline struct {
	Name   string   `yaml:"name"`
	Steps  []azStep `yaml:"steps"`
	Stages []struct {
		Stage string `yaml:"stage"`
		Jobs  []struct {
			Job   string   `yaml:"job"`
			Steps []azStep `yaml:"steps"`
		} `yaml:"jobs"`
	} `yaml:"stages"`
	Jobs []struct {
		Job   string   `yaml:"job"`
		Steps []azStep `yaml:"steps"`
	} `yaml:"jobs"`
}
type azStep struct {
	Task        string `yaml:"task"`
	Script      string `yaml:"script"`
	Bash        string `yaml:"bash"`
	PowerShell  string `yaml:"powershell"`
	DisplayName string `yaml:"displayName"`
}

func (s azStep) cmd() string { // collapse all body forms
	for _, c := range []string{s.Script, s.Bash, s.PowerShell} {
		if c != "" {
			return c
		}
	}
	return ""
}

func parseAzurePipeline(path, repoRoot string) (Pipeline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Pipeline{}, err
	}
	var pl azPipeline
	if err := yaml.Unmarshal(raw, &pl); err != nil {
		return Pipeline{}, fmt.Errorf("cidetect: %s: %w", path, err)
	}
	rel, _ := filepath.Rel(repoRoot, path)
	var steps []Step
	emit := func(prefix string, ss []azStep) {
		for _, st := range ss {
			label := st.DisplayName
			if label == "" {
				if st.Task != "" {
					label = st.Task
				} else {
					label = "step"
				}
			}
			if prefix != "" {
				label = prefix + " · " + label
			}
			steps = append(steps, classify(label, "task:"+st.Task, st.cmd()))
		}
	}
	emit("", pl.Steps)
	for _, j := range pl.Jobs {
		emit(j.Job, j.Steps)
	}
	for _, st := range pl.Stages {
		for _, j := range st.Jobs {
			emit(st.Stage+"/"+j.Job, j.Steps)
		}
	}
	if len(steps) == 0 {
		return Pipeline{}, fmt.Errorf("no steps found in %s", path)
	}
	name := pl.Name
	if name == "" {
		name = filepath.Base(path)
	}
	return Pipeline{Path: path, RelPath: rel, Platform: "azure-devops", Name: name, Steps: steps}, nil
}

// ---- Defaults for opaque platforms ----------------------------------------

func defaultJenkinsSteps() []Step {
	return []Step{
		{Name: "checkout", DurationSeconds: 3, CPUUtilPct: 15, Kind: "checkout", Source: "ci-detect-heuristic"},
		{Name: "build", DurationSeconds: 240, CPUUtilPct: 75, Kind: "build", Source: "ci-detect-heuristic"},
		{Name: "test", DurationSeconds: 150, CPUUtilPct: 65, Kind: "test", Source: "ci-detect-heuristic"},
	}
}
func defaultTektonSteps() []Step {
	return []Step{
		{Name: "git-clone", DurationSeconds: 4, CPUUtilPct: 15, Kind: "checkout", Source: "ci-detect-heuristic"},
		{Name: "build", DurationSeconds: 180, CPUUtilPct: 70, Kind: "build", Source: "ci-detect-heuristic"},
		{Name: "test", DurationSeconds: 120, CPUUtilPct: 60, Kind: "test", Source: "ci-detect-heuristic"},
	}
}

// ---- Heuristic classifier -------------------------------------------------

// classify maps a (label, action-ref, shell-script) triple to a measurable
// Step. Heuristics are deliberately public so users can audit the numbers.
func classify(label, uses, run string) Step {
	low := strings.ToLower(label + " " + uses + " " + run)

	// Order matters — most specific first.
	switch {
	case contains(low, "actions/checkout", "git clone", "git-clone", "checkout@"):
		return mk(label, 3, 15, "checkout")
	case contains(low, "setup-java", "setup-node", "setup-python", "setup-go", "setup-dotnet", "setup-ruby", "setup-buildx", "setup-qemu", "use-java", "usenode", "usepython"):
		return mk(label, 12, 30, "setup")
	case contains(low, "cache@", "actions/cache", "save-cache", "restore-cache"):
		return mk(label, 5, 20, "cache")
	case contains(low, "upload-artifact", "download-artifact", "upload-pages-artifact", "deploy-pages", "publish-pages"):
		return mk(label, 8, 25, "artifact")
	case contains(low, "podman build", "podman buildx", "docker/build-push-action", "buildx build", "kaniko"):
		return mk(label, 75, 60, "docker")
	case contains(low, "trivy", "snyk", "codeql", "semgrep", "sonarqube", "sonarcloud", "owasp", "grype", "scorecard"):
		return mk(label, 60, 50, "security-scan")
	case contains(low, " test ", " test", "pytest", "jest", "mocha", "junit", "go test", "cargo test", "mvn test", "gradle test", "npm test", "yarn test"):
		return mk(label, 130, 65, "test")
	case contains(low, "mvn ", "maven ", "gradle ", "sbt ", "bazel ", "make ", "go build", "cargo build", "npm run build", "yarn build", "pnpm build", "tsc ", "webpack", "vite build", "next build", "package "):
		return mk(label, 195, 75, "build")
	case contains(low, "lint", "spectral", "eslint", "ruff", "flake8", "rubocop", "checkstyle", "spotless"):
		return mk(label, 25, 35, "lint")
	case contains(low, "deploy", "publish", "release", "helm upgrade", "kubectl apply", "terraform apply"):
		return mk(label, 45, 35, "deploy")
	case contains(low, "github-script", "actions/github-script", "issues.createcomment", "rest.issues"):
		return mk(label, 6, 20, "comment")
	case uses != "" || run == "":
		// Unknown reusable action — assume a small composite.
		return mk(label, 20, 35, "action")
	default:
		// Plain shell line we couldn't classify. Use length as a weak proxy.
		dur := 10.0
		if n := len(run); n > 200 {
			dur = 30
		} else if n > 60 {
			dur = 18
		}
		return mk(label, dur, 30, "shell")
	}
}

func mk(name string, dur, cpu float64, kind string) Step {
	return Step{Name: name, DurationSeconds: dur, CPUUtilPct: cpu, Kind: kind, Source: "ci-detect-heuristic"}
}

func contains(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

