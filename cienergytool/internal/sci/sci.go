// Package sci implements the Software Carbon Intensity formula
// from ISO/IEC 21031:2024: SCI = ((E * I) + M) / R.
package sci

// Compute returns operational, total and SCI given:
//   - energyKWh: E in kWh
//   - gridGCO2eqPerKWh: I in gCO2eq/kWh
//   - embodiedGCO2eq: M already amortised for the run
//   - r: functional unit count (must be > 0)
func Compute(energyKWh, gridGCO2eqPerKWh, embodiedGCO2eq, r float64) (operational, total, sci float64) {
	if r <= 0 {
		r = 1
	}
	operational = energyKWh * gridGCO2eqPerKWh
	total = operational + embodiedGCO2eq
	sci = total / r
	return
}

