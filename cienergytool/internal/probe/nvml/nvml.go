// Package nvml provides a GPU energy probe based on `nvidia-smi` polling.
//
// The probe shells out to `nvidia-smi --query-gpu=power.draw --format=csv,...`
// at a fixed interval, integrates the readings over time to compute kWh, and
// aggregates across all visible GPUs.
//
// CGO-free by design — works on any host where the NVIDIA driver ships
// nvidia-smi (Linux, Windows, WSL). Returns (0, "", false) if nvidia-smi is
// missing or fails, so the aggregator transparently falls back to the
// eco-ci CPU model.
package nvml

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Sample is one nvidia-smi reading.
type Sample struct {
	At       time.Time
	GPUWatts []float64 // one entry per visible GPU
}

// Probe periodically samples nvidia-smi.
type Probe struct {
	Interval time.Duration // default 2s
	bin      string
}

// New returns a Probe if nvidia-smi is available on PATH, else (_, false).
func New() (*Probe, bool) {
	bin, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return nil, false
	}
	return &Probe{Interval: 2 * time.Second, bin: bin}, true
}

// Sample returns one snapshot of GPU power.draw (watts) for all visible GPUs.
func (p *Probe) Sample(ctx context.Context) (Sample, error) {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, p.bin,
		"--query-gpu=power.draw", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return Sample{}, err
	}
	s := Sample{At: time.Now()}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "[N/A]" || line == "[Not Supported]" {
			continue
		}
		w, err := strconv.ParseFloat(line, 64)
		if err != nil {
			continue
		}
		s.GPUWatts = append(s.GPUWatts, w)
	}
	if len(s.GPUWatts) == 0 {
		return s, errors.New("no GPU power readings")
	}
	return s, nil
}

// IntegrateKWh trapezoidally integrates a series of samples and returns kWh.
// Sum across all GPUs at each sample point.
func IntegrateKWh(samples []Sample) float64 {
	if len(samples) < 2 {
		return 0
	}
	var wh float64
	for i := 1; i < len(samples); i++ {
		dt := samples[i].At.Sub(samples[i-1].At).Hours()
		if dt <= 0 {
			continue
		}
		w1 := sum(samples[i-1].GPUWatts)
		w2 := sum(samples[i].GPUWatts)
		wh += dt * (w1 + w2) / 2.0
	}
	return wh / 1000.0
}

func sum(xs []float64) float64 {
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s
}

// Run polls nvidia-smi every p.Interval until ctx is done, returning the
// integrated kWh and the number of samples actually collected. Errors during
// individual samples are ignored (transient driver hiccups are normal).
func (p *Probe) Run(ctx context.Context) (kwh float64, samples int) {
	interval := p.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var series []Sample
	if s, err := p.Sample(ctx); err == nil {
		series = append(series, s)
	}
	for {
		select {
		case <-ctx.Done():
			return IntegrateKWh(series), len(series)
		case <-ticker.C:
			if s, err := p.Sample(ctx); err == nil {
				series = append(series, s)
			}
		}
	}
}

