.PHONY: build test fmt clean sample dashboard-open ingester-image stack-up stack-down seed-samples

GO ?= go
BIN := bin

build:
	@mkdir -p $(BIN)
	$(GO) build -trimpath -ldflags="-s -w" -o $(BIN)/cienergy-aggregator ./cmd/aggregator
	$(GO) build -trimpath -ldflags="-s -w" -o $(BIN)/cienergy-ingester   ./cmd/ingester
	$(GO) build -trimpath -ldflags="-s -w" -o $(BIN)/cienergy-gpu-probe  ./cmd/gpu-probe

ingester-image:
	docker build -f cmd/ingester/Dockerfile -t cienergy/ingester:dev .

stack-up:
	cd dashboard/grafana && docker compose up -d --build

stack-down:
	cd dashboard/grafana && docker compose down -v

# Ingest the 5 bundled sample reports into the running stack.
seed-samples:
	@for f in dashboard/embedded/sample-reports/run-*.json; do \
		echo "POST $$f"; \
		curl -fsS -X POST http://localhost:8080/v1/runs \
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

