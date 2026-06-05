// Package grid resolves marginal grid carbon intensity (gCO2eq/kWh)
// from Electricity Maps when a token is available, otherwise from a
// bundled Ember monthly average dataset.
package grid

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Result captures the resolved value and its provenance.
type Result struct {
	ValueGCO2eqPerKWh float64   `json:"valueGCO2eqPerKWh"`
	Source            string    `json:"source"`
	Zone              string    `json:"zone"`
	Timestamp         time.Time `json:"timestamp"`
}

// Resolver fetches grid intensity, with an offline fallback.
type Resolver struct {
	HTTPClient *http.Client
	EMapsToken string // Electricity Maps API token (optional)
}

// New returns a Resolver with sensible defaults.
func New(token string) *Resolver {
	return &Resolver{
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
		EMapsToken: strings.TrimSpace(token),
	}
}

// Resolve returns the carbon intensity for an Electricity Maps zone (e.g. "FR", "US-VA").
// Order: electricitymaps (if token) → ember-monthly (always available).
func (r *Resolver) Resolve(ctx context.Context, zone string) (Result, error) {
	zone = strings.ToUpper(strings.TrimSpace(zone))
	if zone == "" {
		zone = "WORLD"
	}
	if r.EMapsToken != "" {
		if res, err := r.fromElectricityMaps(ctx, zone); err == nil {
			return res, nil
		}
	}
	return r.fromEmber(zone), nil
}

type emapsResp struct {
	CarbonIntensity float64   `json:"carbonIntensity"`
	DateTime        time.Time `json:"datetime"`
	Zone            string    `json:"zone"`
}

func (r *Resolver) fromElectricityMaps(ctx context.Context, zone string) (Result, error) {
	url := fmt.Sprintf("https://api.electricitymap.org/v3/carbon-intensity/latest?zone=%s", zone)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("auth-token", r.EMapsToken)
	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("electricitymaps: status %d", resp.StatusCode)
	}
	var em emapsResp
	if err := json.NewDecoder(resp.Body).Decode(&em); err != nil {
		return Result{}, err
	}
	return Result{
		ValueGCO2eqPerKWh: em.CarbonIntensity,
		Source:            "electricitymaps",
		Zone:              em.Zone,
		Timestamp:         em.DateTime,
	}, nil
}

// emberMonthlyAvg is a bundled, conservative monthly-average dataset
// derived from Ember's Global Electricity Review 2024 (gCO2eq/kWh).
// Source: https://ember-climate.org/data/
var emberMonthlyAvg = map[string]float64{
	"WORLD":  481,
	"EU":     242,
	"FR":     56,
	"DE":     381,
	"UK":     207,
	"GB":     207,
	"ES":     150,
	"IT":     268,
	"PL":     635,
	"US":     369,
	"US-VA":  391, // us-east-1
	"US-OR":  136, // us-west-2
	"US-IA":  470,
	"US-CA":  237,
	"CA-QC":  29,
	"BR":     97,
	"IN":     713,
	"CN":     549,
	"JP":     494,
	"AU":     517,
	"ZA":     900,
}

func (r *Resolver) fromEmber(zone string) Result {
	v, ok := emberMonthlyAvg[zone]
	if !ok {
		v = emberMonthlyAvg["WORLD"]
		zone = "WORLD"
	}
	return Result{
		ValueGCO2eqPerKWh: v,
		Source:            "ember-monthly",
		Zone:              zone,
		Timestamp:         time.Now().UTC().Truncate(time.Hour),
	}
}

