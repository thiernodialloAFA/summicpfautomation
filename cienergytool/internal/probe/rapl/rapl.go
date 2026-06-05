// Package rapl reads Intel/AMD Running Average Power Limit (RAPL) energy
// counters from the Linux powercap sysfs interface.
//
//   /sys/class/powercap/intel-rapl:<n>/energy_uj
//   /sys/class/powercap/intel-rapl:<n>/name
//
// Counters are monotonic free-running microjoule integrators that wrap when
// they hit max_energy_range_uj. Probe.Sample handles wrap-around by tracking
// the previous reading per domain.
//
// References:
//   - Intel "Running Average Power Limit Energy Reporting" — CVE-2020-8694 era doc
//     https://www.intel.com/content/www/us/en/developer/articles/technical/software-security-guidance/advisory-guidance/running-average-power-limit-energy-reporting.html
//   - kernel.org powercap sysfs ABI
//     https://www.kernel.org/doc/Documentation/power/powercap/powercap.txt
//   - Scaphandre (Hubblo) — production-grade RAPL exporter in Rust
//     https://github.com/hubblo-org/scaphandre
package rapl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const sysfsRoot = "/sys/class/powercap"

// Domain represents one RAPL domain (package, core, dram, psys, ...).
type Domain struct {
	Path        string // /sys/class/powercap/intel-rapl:0
	Name        string // "package-0", "core", "dram", ...
	MaxRangeUJ  uint64 // max_energy_range_uj for wrap detection
}

// Probe accumulates energy across all visible RAPL domains.
type Probe struct {
	mu        sync.Mutex
	domains   []Domain
	lastUJ    map[string]uint64 // path → last raw counter
	totalUJ   uint64            // accumulated, wrap-corrected
	startedAt time.Time
}

// Available reports whether RAPL is readable on this host.
// Returns (probe, true, nil) on success or (nil, false, nil) if RAPL is not
// available (most common cause: kernel ≥ 5.10 on cloud VMs hides the counters
// behind root permissions as a side-channel mitigation — CVE-2020-8694).
func Available() (*Probe, bool, error) {
	entries, err := os.ReadDir(sysfsRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var doms []Domain
	for _, e := range entries {
		// We only care about top-level intel-rapl / amd-rapl domains.
		if !strings.HasPrefix(e.Name(), "intel-rapl:") && !strings.HasPrefix(e.Name(), "amd-rapl:") {
			continue
		}
		p := filepath.Join(sysfsRoot, e.Name())
		nameBytes, err := os.ReadFile(filepath.Join(p, "name"))
		if err != nil {
			continue
		}
		maxBytes, _ := os.ReadFile(filepath.Join(p, "max_energy_range_uj"))
		maxUJ, _ := strconv.ParseUint(strings.TrimSpace(string(maxBytes)), 10, 64)
		// Probe-read once: if energy_uj is unreadable (EACCES), give up early
		// rather than fail later mid-build.
		if _, err := os.ReadFile(filepath.Join(p, "energy_uj")); err != nil {
			return nil, false, nil
		}
		doms = append(doms, Domain{
			Path: p, Name: strings.TrimSpace(string(nameBytes)), MaxRangeUJ: maxUJ,
		})
	}
	if len(doms) == 0 {
		return nil, false, nil
	}
	return &Probe{
		domains: doms,
		lastUJ:  make(map[string]uint64, len(doms)),
	}, true, nil
}

// Start records the initial counter values.
func (p *Probe) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, d := range p.domains {
		v, err := readUJ(d.Path)
		if err != nil {
			return fmt.Errorf("rapl start %s: %w", d.Name, err)
		}
		p.lastUJ[d.Path] = v
	}
	p.totalUJ = 0
	p.startedAt = time.Now()
	return nil
}

// Sample integrates the delta since the last Sample/Start, handling wrap.
// Returns the cumulative energy in kWh since Start.
func (p *Probe) Sample() (float64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, d := range p.domains {
		cur, err := readUJ(d.Path)
		if err != nil {
			return 0, err
		}
		prev := p.lastUJ[d.Path]
		var delta uint64
		if cur >= prev {
			delta = cur - prev
		} else if d.MaxRangeUJ > 0 {
			delta = (d.MaxRangeUJ - prev) + cur
		}
		p.totalUJ += delta
		p.lastUJ[d.Path] = cur
	}
	return ujToKWh(p.totalUJ), nil
}

// Stop returns the final cumulative energy (kWh) and duration since Start.
func (p *Probe) Stop() (kwh float64, duration time.Duration, err error) {
	kwh, err = p.Sample()
	duration = time.Since(p.startedAt)
	return
}

// Domains returns the list of detected RAPL domains for debug / reporting.
func (p *Probe) Domains() []Domain { return append([]Domain(nil), p.domains...) }

func readUJ(domainPath string) (uint64, error) {
	b, err := os.ReadFile(filepath.Join(domainPath, "energy_uj"))
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
}

func ujToKWh(uj uint64) float64 {
	// µJ → J → kWh    (1 kWh = 3.6e9 J = 3.6e15 µJ)
	return float64(uj) / 3.6e15
}

