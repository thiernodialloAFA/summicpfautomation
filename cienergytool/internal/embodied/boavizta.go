// Package embodied resolves embodied carbon (M term of the SCI formula)
// for the runner hardware, in gCO2eq, amortised over a configurable lifetime.
//
// Primary source: Boavizta API v1 (https://doc.api.boavizta.org/).
// Fallback:       a CCF-derived static default (~250 kgCO2eq per server,
//                 4-year lifetime, prorated by step duration).
package embodied

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultServerGWPkg is a conservative server-class baseline used when
// Boavizta returns nothing for an unknown SKU. Source: Cloud Carbon Footprint
// methodology — https://www.cloudcarbonfootprint.org/docs/embodied-emissions/.
const DefaultServerGWPkg = 250.0 // kgCO2eq, full server

// DefaultLifetimeYears matches Boavizta's default amortisation horizon.
const DefaultLifetimeYears = 4.0

// Result captures the resolved value and its provenance.
type Result struct {
	GCO2eqForRun float64 `json:"gCO2eqForRun"`
	Source       string  `json:"source"` // "boavizta" | "ccf-static" | "user-override"
	HardwareGWPkg float64 `json:"hardwareGWPkg"`
	LifetimeYears float64 `json:"lifetimeYears"`
}

// Resolver fetches embodied carbon with an offline fallback and tiny LRU cache.
type Resolver struct {
	HTTPClient    *http.Client
	BaseURL       string  // override Boavizta endpoint
	LifetimeYears float64
	cache         sync.Map // key=cpuModel → cachedEntry
}

type cachedEntry struct {
	hardwareGWPkg float64
	at            time.Time
}

func New() *Resolver {
	return &Resolver{
		HTTPClient:    &http.Client{Timeout: 6 * time.Second},
		BaseURL:       "https://api.boavizta.org/v1",
		LifetimeYears: DefaultLifetimeYears,
	}
}

// Resolve returns embodied carbon for one run on the given CPU, prorated
// by run duration. Never returns an error — falls back to CCF static value
// so the agent always produces a number.
func (r *Resolver) Resolve(ctx context.Context, cpuModel string, runDurationSeconds float64) Result {
	hw, src := r.lookupHardware(ctx, cpuModel)
	lifetimeSeconds := r.LifetimeYears * 365.25 * 24 * 3600
	share := runDurationSeconds / lifetimeSeconds
	if share < 0 {
		share = 0
	}
	gco2eqRun := hw * 1000.0 * share // kgCO2eq → gCO2eq
	return Result{
		GCO2eqForRun:  gco2eqRun,
		Source:        src,
		HardwareGWPkg: hw,
		LifetimeYears: r.LifetimeYears,
	}
}

func (r *Resolver) lookupHardware(ctx context.Context, cpuModel string) (float64, string) {
	key := strings.TrimSpace(strings.ToLower(cpuModel))
	if v, ok := r.cache.Load(key); ok {
		e := v.(cachedEntry)
		if time.Since(e.at) < 24*time.Hour {
			return e.hardwareGWPkg, "boavizta"
		}
	}
	if v, src, ok := r.fromBoavizta(ctx, cpuModel); ok {
		r.cache.Store(key, cachedEntry{hardwareGWPkg: v, at: time.Now()})
		return v, src
	}
	return DefaultServerGWPkg, "ccf-static"
}

// boaviztaCPURequest matches POST /v1/component/cpu body.
type boaviztaCPURequest struct {
	Name string `json:"name"`
}

// boaviztaImpactsResponse — relevant subset of the API response.
type boaviztaImpactsResponse struct {
	Impacts struct {
		GWP struct {
			Embedded struct {
				Value float64 `json:"value"`
				Unit  string  `json:"unit"`
			} `json:"embedded"`
		} `json:"gwp"`
	} `json:"impacts"`
}

func (r *Resolver) fromBoavizta(ctx context.Context, cpuModel string) (float64, string, bool) {
	if r.BaseURL == "" || cpuModel == "" {
		return 0, "", false
	}
	body, _ := json.Marshal(boaviztaCPURequest{Name: cpuModel})
	url := r.BaseURL + "/component/cpu?verbose=false&criteria=gwp"
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return 0, "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, "", false
	}
	var out boaviztaImpactsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, "", false
	}
	v := out.Impacts.GWP.Embedded.Value
	if v <= 0 {
		return 0, "", false
	}
	// Boavizta returns kgCO2eq for the CPU alone. Scale up to "full server"
	// using a rough 3× factor (CPU ≈ 1/3 of server embodied, per Boavizta's
	// own server vs CPU benchmarks). Conservative default.
	return v * 3.0, "boavizta", true
}

// String for debug.
func (e Result) String() string {
	return fmt.Sprintf("embodied=%.3f gCO2eq (source=%s, hw=%.1f kgCO2eq, lifetime=%.1fy)",
		e.GCO2eqForRun, e.Source, e.HardwareGWPkg, e.LifetimeYears)
}

