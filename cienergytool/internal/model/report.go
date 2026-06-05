// Package model defines the canonical cienergy report types matching docs/schema/v1.json.
package model

import "time"

const (
	SpecVersion    = "1.0.0"
	SCISpecVersion = "ISO/IEC 21031:2024"
	SchemaURL      = "https://axa-oss.github.io/cienergy/schema/v1.json"
)

type Report struct {
	Schema         string    `json:"$schema"`
	SpecVersion    string    `json:"specVersion"`
	SCISpecVersion string    `json:"sciSpecVersion"`
	Run            Run       `json:"run"`
	Runner         Runner    `json:"runner"`
	Energy         Energy    `json:"energy"`
	Carbon         Carbon    `json:"carbon"`
	SCI            SCI       `json:"sci"`
	Cache          *Cache    `json:"cache,omitempty"`
	Metadata       *Metadata `json:"metadata,omitempty"`
}

type Run struct {
	ID              string    `json:"id"`
	Platform        string    `json:"platform"`
	Repository      string    `json:"repository"`
	Workflow        string    `json:"workflow"`
	Ref             string    `json:"ref,omitempty"`
	CommitSha       string    `json:"commitSha"`
	StartedAt       time.Time `json:"startedAt"`
	EndedAt         time.Time `json:"endedAt"`
	DurationSeconds float64   `json:"durationSeconds"`
}

type Runner struct {
	OS       string  `json:"os"`
	Arch     string  `json:"arch"`
	VCPU     int     `json:"vcpu"`
	RAMGiB   float64 `json:"ramGiB"`
	CPUModel string  `json:"cpuModel"`
	TDPWatts float64 `json:"tdpWatts"`
	Provider string  `json:"provider"`
	Region   string  `json:"region"`
	IsSpot   bool    `json:"isSpot,omitempty"`
}

type Step struct {
	Name            string  `json:"name"`
	KWh             float64 `json:"kWh"`
	DurationSeconds float64 `json:"durationSeconds"`
	Source          string  `json:"source"`
	CPUUtilPct      float64 `json:"cpuUtilPct,omitempty"`
	GPUKWh          float64 `json:"gpuKWh,omitempty"`
}

type Energy struct {
	TotalKWh float64 `json:"totalKWh"`
	ByStep   []Step  `json:"byStep"`
}

type GridIntensity struct {
	ValueGCO2eqPerKWh float64   `json:"valueGCO2eqPerKWh"`
	Source            string    `json:"source"`
	Zone              string    `json:"zone"`
	Timestamp         time.Time `json:"timestamp"`
}

type Carbon struct {
	OperationalGCO2eq float64       `json:"operationalGCO2eq"`
	EmbodiedGCO2eq    float64       `json:"embodiedGCO2eq"`
	TotalGCO2eq       float64       `json:"totalGCO2eq"`
	GridIntensity     GridIntensity `json:"gridIntensity"`
	EmbodiedSource    string        `json:"embodiedSource,omitempty"`
}

type SCI struct {
	Value          float64 `json:"value"`
	Unit           string  `json:"unit"`
	FunctionalUnit string  `json:"functionalUnit"`
	R              float64 `json:"R"`
}

type Cache struct {
	Hit                 bool    `json:"hit"`
	SavedKWhEstimate    float64 `json:"savedKWhEstimate,omitempty"`
	SavedGCO2eqEstimate float64 `json:"savedGCO2eqEstimate,omitempty"`
}

type Metadata struct {
	Team       string            `json:"team,omitempty"`
	CostCenter string            `json:"costCenter,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

