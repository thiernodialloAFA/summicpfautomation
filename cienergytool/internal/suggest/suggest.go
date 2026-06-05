// Package suggest derives actionable improvement suggestions from a cienergy
// Report. Each rule inspects the report's own measurements/heuristics and
// returns at most one Suggestion. Estimated savings are upper bounds, never
// promises — they are intended to help operators prioritise, not to claim
// gains that have not been realised.
//
// All rules are pure functions over *model.Report so they can be unit-tested
// in isolation and reused from the CLI, the dashboard, and (later) a PR-comment
// bot.
package suggest

import (
	"fmt"
	"math"
	"strings"

	"github.com/axa-oss/cienergytool/internal/model"
)

// For runs every rule against r and returns the resulting suggestions
// sorted by severity then by estimated savings.
func For(r *model.Report) []model.Suggestion {
	rules := []func(*model.Report) *model.Suggestion{
		ruleEnableDependencyCache,
		ruleEnableDockerLayerCache,
		ruleShardLongTests,
		ruleSwitchToARM,
		ruleCleanerGridZone,
		ruleRightSizeRunner,
		rulePathFiltersForScans,
		ruleAvoidRedundantBuilds,
		ruleArtifactBloat,
		ruleCacheHitMissingSavings,
	}
	var out []model.Suggestion
	for _, rule := range rules {
		if s := rule(r); s != nil {
			out = append(out, *s)
		}
	}
	severityRank := map[string]int{"critical": 0, "major": 1, "minor": 2, "info": 3}
	// sort: severity asc, then savings desc
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			a, b := out[j-1], out[j]
			ra, rb := severityRank[a.Severity], severityRank[b.Severity]
			if ra > rb || (ra == rb && b.EstimatedSavingGCO2eq > a.EstimatedSavingGCO2eq) {
				out[j-1], out[j] = b, a
			}
		}
	}
	return out
}

// ---- helpers --------------------------------------------------------------

func intensity(r *model.Report) float64 { return r.Carbon.GridIntensity.ValueGCO2eqPerKWh }

func gCO2(kwh, gPerKWh float64) float64 { return kwh * gPerKWh }

func stepsMatching(r *model.Report, fn func(model.Step) bool) []model.Step {
	var out []model.Step
	for _, s := range r.Energy.ByStep {
		if fn(s) {
			out = append(out, s)
		}
	}
	return out
}

func stepKindFromName(name string) string {
	low := strings.ToLower(name)
	switch {
	case strings.Contains(low, "setup-") || strings.Contains(low, "setup "):
		return "setup"
	case strings.Contains(low, "checkout"):
		return "checkout"
	case strings.Contains(low, "docker") && (strings.Contains(low, "build") || strings.Contains(low, "buildx") || strings.Contains(low, "push")):
		return "docker"
	case (strings.Contains(low, "podman") || strings.Contains(low, "buildah") || strings.Contains(low, "kaniko")) && (strings.Contains(low, "build") || strings.Contains(low, "push")):
		return "podman"
	case strings.Contains(low, " test") || strings.HasSuffix(low, "test") || strings.Contains(low, "junit") || strings.Contains(low, "pytest"):
		return "test"
	case strings.Contains(low, "build") || strings.Contains(low, "package") || strings.Contains(low, "mvn ") || strings.Contains(low, "gradle"):
		return "build"
	case strings.Contains(low, "trivy") || strings.Contains(low, "snyk") || strings.Contains(low, "codeql") || strings.Contains(low, "semgrep"):
		return "security-scan"
	case strings.Contains(low, "lint") || strings.Contains(low, "spectral") || strings.Contains(low, "eslint"):
		return "lint"
	case strings.Contains(low, "deploy") || strings.Contains(low, "publish") || strings.Contains(low, "release") || strings.Contains(low, "pages"):
		return "deploy"
	case strings.Contains(low, "artifact"):
		return "artifact"
	}
	return ""
}

func sumKWhByKind(r *model.Report, kind string) float64 {
	t := 0.0
	for _, s := range r.Energy.ByStep {
		if stepKindFromName(s.Name) == kind {
			t += s.KWh
		}
	}
	return t
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// ---- rules ----------------------------------------------------------------

// R1: dependency cache absent → setup-* steps re-download every run.
func ruleEnableDependencyCache(r *model.Report) *model.Suggestion {
	if r.Cache != nil && r.Cache.Hit {
		return nil
	}
	setupKWh := sumKWhByKind(r, "setup")
	if setupKWh <= 0 {
		return nil
	}
	// Assume cache eliminates 70% of the setup energy on warm runs.
	saving := setupKWh * 0.7
	return &model.Suggestion{
		ID: "enable-dependency-cache", Severity: "major",
		Title:  "Enable language-level dependency cache",
		Detail: fmt.Sprintf("Setup steps consumed %.4f kWh and the run did not advertise a cache hit. Enable actions/cache (or the language-specific cache: maven, npm, pip, go-mod) to drop ~70%% of that on warm runs.", round6(setupKWh)),
		EstimatedSavingKWh:    round6(saving),
		EstimatedSavingGCO2eq: round3(gCO2(saving, intensity(r))),
		Reference:             "https://docs.github.com/actions/using-workflows/caching-dependencies-to-speed-up-workflows",
	}
}

// R2: podman build without buildx/cache mounts.
func ruleEnableDockerLayerCache(r *model.Report) *model.Suggestion {
	// Count both docker- and podman-classified steps so the rule stays correct
	// regardless of which OCI builder the pipeline uses.
	podman := sumKWhByKind(r, "docker") + sumKWhByKind(r, "podman")
	if podman <= 0 {
		return nil
	}
	// Look for any hint a layer cache is already wired.
	for _, s := range r.Energy.ByStep {
		low := strings.ToLower(s.Name)
		if strings.Contains(low, "buildx") || strings.Contains(low, "cache-from") || strings.Contains(low, "cache-mount") || strings.Contains(low, "gha cache") {
			return nil
		}
	}
	saving := podman * 0.6
	return &model.Suggestion{
		ID: "enable-docker-layer-cache", Severity: "major",
		Title:  "Enable podman BuildKit layer cache",
		Detail: fmt.Sprintf("podman build steps consumed %.4f kWh with no buildx cache hint. Switch to docker/build-push-action with cache-from/cache-to=type=gha to cut ~60%% on warm runs.", round6(podman)),
		EstimatedSavingKWh:    round6(saving),
		EstimatedSavingGCO2eq: round3(gCO2(saving, intensity(r))),
		Reference:             "https://docs.docker.com/build/cache/backends/gha/",
	}
}

// R3: a single test step over 5 minutes → shard it.
func ruleShardLongTests(r *model.Report) *model.Suggestion {
	var worst model.Step
	for _, s := range r.Energy.ByStep {
		if stepKindFromName(s.Name) != "test" {
			continue
		}
		if s.DurationSeconds > worst.DurationSeconds {
			worst = s
		}
	}
	if worst.DurationSeconds < 300 {
		return nil
	}
	saving := worst.KWh * 0.4
	return &model.Suggestion{
		ID: "shard-long-tests", Severity: "minor",
		Title:  "Shard long-running test step",
		Detail: fmt.Sprintf("Step %q ran for %.0fs (%.4f kWh). Splitting it across N parallel jobs typically removes ~40%% of its wall-time on hosted runners.", worst.Name, worst.DurationSeconds, worst.KWh),
		EstimatedSavingKWh:    round6(saving),
		EstimatedSavingGCO2eq: round3(gCO2(saving, intensity(r))),
		Reference:             "https://docs.github.com/actions/using-jobs/using-a-matrix-for-your-jobs",
	}
}

// R4: x86 runner → ARM saves ~25% energy on equivalent workloads.
func ruleSwitchToARM(r *model.Report) *model.Suggestion {
	arch := strings.ToLower(r.Runner.Arch)
	if arch == "arm64" || arch == "aarch64" {
		return nil
	}
	if r.Energy.TotalKWh < 0.005 {
		return nil // not worth raising for tiny builds
	}
	saving := r.Energy.TotalKWh * 0.25
	return &model.Suggestion{
		ID: "switch-to-arm-runners", Severity: "info",
		Title:  "Try ARM64 runners",
		Detail: fmt.Sprintf("Runner arch is %q. Hosted ARM64 runners draw ~25%% less power for equivalent CI work (Graviton/Ampere). Multi-arch images keep x86 compatibility.", r.Runner.Arch),
		EstimatedSavingKWh:    round6(saving),
		EstimatedSavingGCO2eq: round3(gCO2(saving, intensity(r))),
		Reference:             "https://github.blog/changelog/2024-06-03-actions-arm64-linux-and-windows-runners-now-available/",
	}
}

// R5: dirty grid → suggest moving runner to a cleaner zone.
func ruleCleanerGridZone(r *model.Report) *model.Suggestion {
	i := intensity(r)
	if i <= 300 {
		return nil
	}
	// Target a realistic clean European average ≈ 100 g/kWh.
	target := 100.0
	saving := gCO2(r.Energy.TotalKWh, i-target)
	if saving <= 0.001 {
		return nil
	}
	sev := "minor"
	if i > 500 {
		sev = "major"
	}
	return &model.Suggestion{
		ID: "move-to-cleaner-zone", Severity: sev,
		Title:  fmt.Sprintf("Move runner from %s (%.0f gCO₂eq/kWh) to a cleaner zone", r.Carbon.GridIntensity.Zone, i),
		Detail: fmt.Sprintf("Operational carbon scales linearly with grid intensity. Picking a zone closer to %.0f gCO₂eq/kWh (e.g. FR, NO, SE, QC) would reduce Scope 2 by ~%.1f%%.", target, (i-target)*100/i),
		EstimatedSavingGCO2eq: round3(saving),
		Reference:             "https://app.electricitymaps.com/map",
	}
}

// R6: oversized runner (high CPU TDP + low utilisation).
func ruleRightSizeRunner(r *model.Report) *model.Suggestion {
	if r.Runner.TDPWatts < 200 || len(r.Energy.ByStep) == 0 {
		return nil
	}
	// Average CPU utilisation across steps with a CPUUtilPct hint.
	var sum, n float64
	for _, s := range r.Energy.ByStep {
		if s.CPUUtilPct > 0 {
			sum += s.CPUUtilPct
			n++
		}
	}
	if n == 0 {
		return nil
	}
	avg := sum / n
	if avg >= 40 {
		return nil
	}
	saving := r.Energy.TotalKWh * 0.3
	return &model.Suggestion{
		ID: "right-size-runner", Severity: "minor",
		Title:  "Right-size the runner",
		Detail: fmt.Sprintf("Average CPU utilisation across %d steps is %.0f%% on a %.0f W TDP runner. A smaller runner (2 vCPU) would idle less; embodied carbon would also drop proportionally.", int(n), avg, r.Runner.TDPWatts),
		EstimatedSavingKWh:    round6(saving),
		EstimatedSavingGCO2eq: round3(gCO2(saving, intensity(r))),
		Reference:             "https://docs.github.com/actions/using-github-hosted-runners/about-larger-runners",
	}
}

// R7: many lint/security steps without path filters → wasted work on docs-only PRs.
func rulePathFiltersForScans(r *model.Report) *model.Suggestion {
	var scan float64
	count := 0
	for _, s := range r.Energy.ByStep {
		k := stepKindFromName(s.Name)
		if k == "security-scan" || k == "lint" {
			scan += s.KWh
			count++
		}
	}
	if count < 2 || scan <= 0 {
		return nil
	}
	// Conservative: assume 20% of PRs are docs-only / unrelated.
	saving := scan * 0.2
	return &model.Suggestion{
		ID: "add-paths-filter", Severity: "info",
		Title:  fmt.Sprintf("Skip scans on unrelated changes (%d scan/lint step(s))", count),
		Detail: fmt.Sprintf("Lint/security steps consumed %.4f kWh. Add an on.paths-ignore filter (or job-level if: contains(...)) to skip them when only docs/CHANGELOG/README change — ~20%% of PRs in most repos.", round6(scan)),
		EstimatedSavingKWh:    round6(saving),
		EstimatedSavingGCO2eq: round3(gCO2(saving, intensity(r))),
		Reference:             "https://docs.github.com/actions/using-workflows/workflow-syntax-for-github-actions#onpushpull_requestpull_request_targetpathspaths-ignore",
	}
}

// R8: several build steps that look redundant (build twice or build in N jobs).
func ruleAvoidRedundantBuilds(r *model.Report) *model.Suggestion {
	builds := stepsMatching(r, func(s model.Step) bool { return stepKindFromName(s.Name) == "build" })
	if len(builds) < 2 {
		return nil
	}
	var sum float64
	for _, s := range builds {
		sum += s.KWh
	}
	// Conservative: at least one rebuild could be replaced by artifact reuse.
	saving := sum * 0.4
	return &model.Suggestion{
		ID: "share-build-artifacts", Severity: "major",
		Title:  fmt.Sprintf("Share build artifacts across %d build step(s)", len(builds)),
		Detail: fmt.Sprintf("Detected %d build steps consuming %.4f kWh combined. Build once, upload as artifact, download in downstream jobs (test, scan, deploy) instead of rebuilding.", len(builds), round6(sum)),
		EstimatedSavingKWh:    round6(saving),
		EstimatedSavingGCO2eq: round3(gCO2(saving, intensity(r))),
		Reference:             "https://docs.github.com/actions/using-workflows/storing-workflow-data-as-artifacts",
	}
}

// R9: too many artifact upload/download steps.
func ruleArtifactBloat(r *model.Report) *model.Suggestion {
	n := 0
	for _, s := range r.Energy.ByStep {
		if stepKindFromName(s.Name) == "artifact" {
			n++
		}
	}
	if n < 5 {
		return nil
	}
	return &model.Suggestion{
		ID: "consolidate-artifacts", Severity: "info",
		Title:  fmt.Sprintf("Consolidate %d artifact upload/download steps", n),
		Detail: "Each upload/download crosses the network and re-encodes the payload. Group small files into a tarball and rely on a single upload-artifact step per job.",
		Reference: "https://github.com/actions/upload-artifact#v4-what-is-new",
	}
}

// R10: cache hit but no savings recorded → instrumentation gap, not a CI bug.
func ruleCacheHitMissingSavings(r *model.Report) *model.Suggestion {
	if r.Cache == nil || !r.Cache.Hit {
		return nil
	}
	if r.Cache.SavedKWhEstimate > 0 {
		return nil
	}
	return &model.Suggestion{
		ID: "record-cache-savings", Severity: "info",
		Title:  "Record counter-factual cache savings",
		Detail: "This run benefited from a cache hit but cienergy did not log the avoided kWh / gCO₂eq. Pass the cold-run baseline to --cache-saved-kwh so the dashboard can show the avoided emissions.",
	}
}

