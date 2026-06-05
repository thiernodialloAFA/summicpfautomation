// Package otlp implements a minimal OTLP/HTTP-JSON exporter for cienergy
// reports. It pushes a small set of Gauge metrics aligned with the
// OpenTelemetry semantic conventions for sustainability (draft).
//
// Endpoint:   POST {endpoint}/v1/metrics
// Encoding:   application/json (OTLP/HTTP-JSON, OTel spec § 5)
// References:
//   - OTLP/HTTP spec: https://opentelemetry.io/docs/specs/otlp/#otlphttp
//   - OTel Sustainability semantic conventions (draft):
//     https://github.com/open-telemetry/semantic-conventions/issues/1129
//
// Why JSON and not protobuf? OTLP/HTTP-JSON keeps cienergy CGO-free and dep-free.
// Most collectors (otelcol, Grafana Alloy, Honeycomb, Datadog OTLP, New Relic OTLP)
// accept both encodings on the same /v1/metrics endpoint.
package otlp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/axa-oss/cienergytool/internal/model"
)

// Exporter pushes cienergy reports as OTLP gauges.
type Exporter struct {
	Endpoint   string
	HTTPClient *http.Client
	Headers    map[string]string // e.g. {"Authorization": "Bearer ..."}
}

// New returns an Exporter ready to use. endpoint is the OTLP/HTTP base URL
// (no trailing slash). The exporter appends "/v1/metrics".
func New(endpoint string) *Exporter {
	return &Exporter{
		Endpoint:   endpoint,
		HTTPClient: &http.Client{Timeout: 6 * time.Second},
		Headers:    map[string]string{},
	}
}

// Export converts a Report into an OTLP MetricsData payload and POSTs it.
func (e *Exporter) Export(ctx context.Context, r *model.Report) error {
	if e.Endpoint == "" {
		return nil
	}
	tsNano := strconv.FormatInt(r.Run.EndedAt.UTC().UnixNano(), 10)

	resourceAttrs := []otAttr{
		{Key: "service.name", Value: otValue{StringValue: "cienergy"}},
		{Key: "service.namespace", Value: otValue{StringValue: r.Run.Repository}},
		{Key: "ci.platform", Value: otValue{StringValue: r.Run.Platform}},
		{Key: "ci.workflow", Value: otValue{StringValue: r.Run.Workflow}},
		{Key: "ci.run.id", Value: otValue{StringValue: r.Run.ID}},
		{Key: "ci.commit.sha", Value: otValue{StringValue: r.Run.CommitSha}},
		{Key: "host.arch", Value: otValue{StringValue: r.Runner.Arch}},
		{Key: "host.cpu.model.name", Value: otValue{StringValue: r.Runner.CPUModel}},
		{Key: "cloud.region", Value: otValue{StringValue: r.Runner.Region}},
		{Key: "sustainability.grid.zone", Value: otValue{StringValue: r.Carbon.GridIntensity.Zone}},
		{Key: "sustainability.grid.source", Value: otValue{StringValue: r.Carbon.GridIntensity.Source}},
		{Key: "sustainability.sci.functional_unit", Value: otValue{StringValue: r.SCI.FunctionalUnit}},
	}
	if r.Metadata != nil {
		if r.Metadata.Team != "" {
			resourceAttrs = append(resourceAttrs, otAttr{Key: "team", Value: otValue{StringValue: r.Metadata.Team}})
		}
		if r.Metadata.CostCenter != "" {
			resourceAttrs = append(resourceAttrs, otAttr{Key: "cost_center", Value: otValue{StringValue: r.Metadata.CostCenter}})
		}
	}

	gauge := func(name, unit, desc string, v float64) otMetric {
		return otMetric{
			Name: name, Unit: unit, Description: desc,
			Gauge: &otGauge{
				DataPoints: []otNumberDataPoint{{
					AsDouble:     v,
					TimeUnixNano: tsNano,
				}},
			},
		}
	}

	payload := otMetricsData{
		ResourceMetrics: []otResourceMetrics{{
			Resource: otResource{Attributes: resourceAttrs},
			ScopeMetrics: []otScopeMetrics{{
				Scope: otScope{Name: "cienergy", Version: model.SpecVersion},
				Metrics: []otMetric{
					gauge("cienergy.energy.kwh", "kWh", "Total energy consumed by the run", r.Energy.TotalKWh),
					gauge("cienergy.carbon.operational.gco2eq", "gCO2eq", "Operational carbon (Scope 2, location-based)", r.Carbon.OperationalGCO2eq),
					gauge("cienergy.carbon.embodied.gco2eq", "gCO2eq", "Embodied carbon (Scope 3 cat. 1, amortised)", r.Carbon.EmbodiedGCO2eq),
					gauge("cienergy.carbon.total.gco2eq", "gCO2eq", "Total carbon = operational + embodied", r.Carbon.TotalGCO2eq),
					gauge("cienergy.grid.intensity.gco2eq_per_kwh", "gCO2eq/kWh", "Grid carbon intensity at runner location at run time", r.Carbon.GridIntensity.ValueGCO2eqPerKWh),
					gauge("cienergy.sci.value", "gCO2eq", "Software Carbon Intensity per functional unit (ISO/IEC 21031:2024)", r.SCI.Value),
					gauge("cienergy.run.duration.seconds", "s", "Wall-clock duration of the run", r.Run.DurationSeconds),
				},
			}},
		}},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.Endpoint+"/v1/metrics", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range e.Headers {
		req.Header.Set(k, v)
	}
	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("OTLP exporter: status %d", resp.StatusCode)
	}
	return nil
}

// ---- minimal OTLP JSON types (subset of opentelemetry-proto) --------------

type otMetricsData struct {
	ResourceMetrics []otResourceMetrics `json:"resourceMetrics"`
}
type otResourceMetrics struct {
	Resource     otResource       `json:"resource"`
	ScopeMetrics []otScopeMetrics `json:"scopeMetrics"`
}
type otResource struct {
	Attributes []otAttr `json:"attributes"`
}
type otScopeMetrics struct {
	Scope   otScope    `json:"scope"`
	Metrics []otMetric `json:"metrics"`
}
type otScope struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}
type otMetric struct {
	Name        string   `json:"name"`
	Unit        string   `json:"unit,omitempty"`
	Description string   `json:"description,omitempty"`
	Gauge       *otGauge `json:"gauge,omitempty"`
}
type otGauge struct {
	DataPoints []otNumberDataPoint `json:"dataPoints"`
}
type otNumberDataPoint struct {
	TimeUnixNano string  `json:"timeUnixNano"`
	AsDouble     float64 `json:"asDouble"`
}
type otAttr struct {
	Key   string  `json:"key"`
	Value otValue `json:"value"`
}
type otValue struct {
	StringValue string `json:"stringValue,omitempty"`
}

