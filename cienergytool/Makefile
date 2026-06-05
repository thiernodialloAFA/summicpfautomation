.PHONY: build test fmt clean sample dashboard-open ingester-image stack-up stack-down seed-samples otlp-demo

GO ?= go
BIN := bin

build:
	@mkdir -p $(BIN)
	$(GO) build -trimpath -ldflags="-s -w" -o $(BIN)/cienergy-aggregator ./cmd/aggregator
	$(GO) build -trimpath -ldflags="-s -w" -o $(BIN)/cienergy-ingester   ./cmd/ingester
	$(GO) build -trimpath -ldflags="-s -w" -o $(BIN)/cienergy-gpu-probe  ./cmd/gpu-probe
	$(GO) build -trimpath -ldflags="-s -w" -o $(BIN)/cienergy-cidetect   ./cmd/cidetect

ingester-image:
	podman build -f cmd/ingester/Dockerfile -t cienergy/ingester:dev .

stack-up:
	cd dashboard/grafana && podman compose up -d --build

stack-down:
	cd dashboard/grafana && podman compose down -v

# Ingest the 5 bundled sample reports into the running stack.
seed-samples:
	@for f in dashboard/embedded/sample-reports/run-*.json; do \
		echo "POST $$f"; \
		curl -fsS -X POST http://localhost:8085/v1/runs \
		  -H 'Content-Type: application/json' \
		  --data @"$$f" >/dev/null && echo "  ok"; \
	done

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...

clean:
	rm -rf $(BIN)

# Generate a sample report from the bundled JSONL and copy it into the embedded dashboard.
sample: build
	$(BIN)/cienergy-aggregator \
	  --start "2026-06-04T08:12:33Z" \
	  --end   "2026-06-04T08:19:07Z" \
	  --repo  axa/demo-app \
	  --workflow ci.yml \
	  --commit a1b2c3d4e5f60718293a4b5c6d7e8f9012345678 \
	  --region US-VA \
	  --steps-file examples/samples/steps.jsonl \
	  --team claims-platform \
	  --cost-center AXA-FR-IT-042 \
	  --out dashboard/embedded/sample-reports/run-local.json
	@echo "→ open dashboard/embedded/index.html"

dashboard-open:
	@command -v open >/dev/null && open dashboard/embedded/index.html || xdg-open dashboard/embedded/index.html

# End-to-end OTLP smoke test: build, run the aggregator against the bundled
# JSONL, and push the resulting report as OpenTelemetry metrics to the local
# collector started by `make stack-up` (otel-collector → Prometheus → Grafana).
OTLP_ENDPOINT ?= http://localhost:4318
otlp-demo: build
	@echo "→ pushing OTLP metrics to $(OTLP_ENDPOINT)"
	$(BIN)/cienergy-aggregator \
	  --start "2026-06-04T08:12:33Z" \
	  --end   "2026-06-04T08:19:07Z" \
	  --repo  axa/demo-app \
	  --workflow ci.yml \
	  --commit a1b2c3d4e5f60718293a4b5c6d7e8f9012345678 \
	  --region FR \
	  --steps-file examples/samples/steps.jsonl \
	  --team claims-platform \
	  --cost-center AXA-FR-IT-042 \
	  --otlp-endpoint $(OTLP_ENDPOINT) \
	  --out /tmp/cienergy-otlp-demo.json
	@echo "→ open http://localhost:3000/d/cienergy-otlp"

