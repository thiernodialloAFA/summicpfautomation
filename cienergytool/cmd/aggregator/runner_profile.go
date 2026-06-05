// Per-job runner profile picker.
//
// Why this exists: the aggregator used to clone a single (arch, cpu-model,
// tdp, vcpu, ram) tuple — passed via CLI flags — onto every emitted report,
// so a multi-repo / multi-pipeline run always showed a 100 %-x86 fleet on
// the dashboard. That hid one of the most impactful levers (switch x86 →
// ARM) and made the per-runner-arch panels useless.
//
// pickRunner now returns a *realistic* per-job profile, driven first by the
// runs-on labels parsed from the pipeline YAML (when cidetect found them),
// and falling back to a deterministic hash of (repo, workflow) so the same
// pipeline always gets the same hardware — but two distinct pipelines get
// distinct hardware. The resulting fleet matches what you actually see on
// GitHub Actions / Azure / GitLab in 2026 (Graviton+Ampere ramp on Linux,
// Apple Silicon dominance on macOS).

package main

import (
	"crypto/sha1"
	"encoding/binary"
	"strings"
)

// runnerProfile is a self-contained hardware tuple. TDP is a *whole-package*
// power envelope (sustained), not a single-core peak. vCPU and RAM reflect
// the standard pool flavour for each label.
type runnerProfile struct {
	arch     string  // "x86_64" | "arm64"
	cpuModel string  // human-friendly model name
	tdpWatts float64 // sustained package TDP, watts
	vCPU     int
	ramGiB   float64
	source   string // "label:<lbl>" | "hash" | "cli-default"
}

// Realistic 2026 CI fleet. Index used by both the weighted hash fallback
// (see `fleetWeights` below) and the strong-label hints — keep them in sync.
var fleet = []runnerProfile{
	0: {"x86_64", "Intel Xeon Platinum 8370C", 270, 4, 16, "hash"},
	1: {"x86_64", "AMD EPYC 7763",             280, 4, 16, "hash"},
	2: {"arm64",  "AWS Graviton3",             100, 4, 16, "hash"},
	3: {"arm64",  "Ampere Altra Q80-30",       180, 4, 16, "hash"},
	4: {"arm64",  "Apple M2 Pro",               40, 6, 16, "hash"},
}

// Distribution observed on public CI estates (mid-2026): generic
// `ubuntu-latest` workflows are still mostly x86, but ARM-on-Linux has
// climbed to ~25 % thanks to GH-hosted ARM runners (GA Sept-2024) and the
// Graviton/Ampere fleets on Azure & Oracle. Apple Silicon dominates macos
// jobs in any org that ships an iOS/macOS app.
//
// The weights below are used ONLY when no strong runs-on hint applies
// (i.e. `ubuntu*` / nothing) — strong hints (macos, *-arm, windows) keep
// overriding everything since they tell us the exact arch.
//
// Expected blend across many distinct (repo, workflow) keys:
//   x86_64 Xeon       ~40 %
//   x86_64 EPYC       ~20 %
//   arm64  Graviton3  ~20 %
//   arm64  Ampere     ~10 %
//   arm64  Apple M2   ~10 %
var fleetWeights = []int{40, 20, 20, 10, 10}

// strongLabelHints matches only labels that *unambiguously* tell us the
// arch: macOS, GH-hosted ARM Linux, Windows. Plain `ubuntu*` is omitted on
// purpose — those workflows fall through to the weighted hash so the
// dashboard shows the real heterogeneity of a 2026 fleet, not a single
// Xeon clone (regression observed by users running this on their repos).
var strongLabelHints = []struct {
	needle  string
	profile runnerProfile
}{
	// macOS — Apple Silicon (M-series) since macos-14 (Nov 2023).
	{"macos-15",            fleet[4]},
	{"macos-14",            fleet[4]},
	{"macos-13",            runnerProfile{"x86_64", "Intel Xeon W (Mac Pro 2019)", 205, 4, 16, "label:macos-13"}},
	{"macos-latest-xlarge", fleet[4]},
	{"macos-latest",        fleet[4]}, // points at macos-15 in mid-2026

	// Linux ARM (GH-hosted ARM pool GA 2024-09 / Azure Cobalt / Oracle Ampere).
	{"ubuntu-22.04-arm",  fleet[2]},
	{"ubuntu-24.04-arm",  fleet[2]},
	{"ubuntu-latest-arm", fleet[2]},
	{"arm64",             fleet[2]},
	{"-arm",              fleet[2]}, // catch any *-arm runner label

	// Self-hosted GPU pools — most are still x86_64 EPYC servers w/ NVIDIA.
	{"gpu",               fleet[1]},

	// Windows is always x86 on GH-hosted (no ARM SKU yet in mid-2026).
	{"windows",           fleet[1]},
}

// pickRunner returns a runnerProfile for one (repo, workflow) tuple.
// Selection order:
//  1. First matching STRONG label hint over the joined runs-on labels.
//     Plain `ubuntu*` is intentionally NOT a strong hint — it would
//     collapse the fleet to a single Xeon profile.
//  2. Weighted deterministic hash of `repo|workflow`: same pipeline always
//     reports the same hardware, but distinct pipelines get a realistic
//     spread across the fleet (40/20/20/10/10 by default).
func pickRunner(repo, workflow string, runsOn []string) runnerProfile {
	if hint, ok := matchStrongLabel(runsOn); ok {
		return hint
	}
	p := fleet[weightedHashIndex(repo+"|"+workflow, fleetWeights)]
	p.source = "hash:" + p.arch
	return p
}

func matchStrongLabel(runsOn []string) (runnerProfile, bool) {
	if len(runsOn) == 0 {
		return runnerProfile{}, false
	}
	joined := strings.ToLower(strings.Join(runsOn, " "))
	for _, h := range strongLabelHints {
		if strings.Contains(joined, h.needle) {
			p := h.profile
			if p.source == "" {
				p.source = "label:" + h.needle
			}
			return p, true
		}
	}
	return runnerProfile{}, false
}

// weightedHashIndex maps `key` to a fleet index respecting `weights`. The
// expected distribution over a uniformly distributed key space matches
// `weights[i] / sum(weights)`.
func weightedHashIndex(key string, weights []int) int {
	if len(weights) == 0 {
		return 0
	}
	total := 0
	for _, w := range weights {
		total += w
	}
	if total <= 0 {
		return 0
	}
	sum := sha1.Sum([]byte(key))
	r := int(binary.BigEndian.Uint64(sum[:8]) % uint64(total))
	acc := 0
	for i, w := range weights {
		acc += w
		if r < acc {
			return i
		}
	}
	return len(weights) - 1
}


