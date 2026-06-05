// Package ecoci implements the CPU-utilisation energy model used by
// green-coding-solutions/eco-ci-energy-estimation when hardware counters
// (RAPL / Kepler / NVML) are not available.
//
//	kWh = (TDP_W * utilPct/100 * durationSeconds) / 3600 / 1000
//
// Source: https://github.com/green-coding-solutions/eco-ci-energy-estimation
package ecoci

// EstimateKWh returns the energy in kWh for a step of given duration,
// given the CPU TDP in watts and the average CPU utilisation in percent.
func EstimateKWh(tdpWatts, utilPct, durationSeconds float64) float64 {
	if tdpWatts <= 0 || durationSeconds <= 0 {
		return 0
	}
	if utilPct < 0 {
		utilPct = 0
	}
	if utilPct > 100 {
		utilPct = 100
	}
	watts := tdpWatts * utilPct / 100.0
	wh := watts * (durationSeconds / 3600.0)
	return wh / 1000.0
}

