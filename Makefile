PROTO_DIR  := proto
GEN_DIR    := proto/device
GRPCURL    := $(shell which grpcurl 2>/dev/null || echo $$HOME/go/bin/grpcurl)
DISH       := 192.168.100.1:9200

-include .athena/Makefile.inc

.PHONY: sync-start bootstrap bootstrap-build bootstrap-check cleanup proto deps build web-build up verify-dish refresh-schema

# One-command update + environment start prep.
# - pulls latest origin/master (ff-only)
# - bootstraps and builds binaries
# - initializes DB schema
# - configures observer location (interactive if missing)
#
# Optional non-interactive location override:
#   make sync-start LAT=47.6062 LON=-122.3321
sync-start:
	git pull --ff-only origin master
	@PIDS="$$(ps -ef | grep -E '[/]bin/pp-starlink daemon|[p]p-starlink daemon' | awk '{print $$2}')"; \
	if [ -n "$$PIDS" ]; then \
		echo "Stopping existing pp-starlink daemon process(es): $$PIDS"; \
		kill $$PIDS; \
	fi
	$(MAKE) bootstrap BOOTSTRAP_BUILD=1
	./bin/pp-starlink init
	@echo "Location setup:"; \
	if [ -n "$(LAT)" ] && [ -n "$(LON)" ]; then \
		./bin/pp-starlink set-location --lat "$(LAT)" --lon "$(LON)"; \
	else \
		echo "Paste coordinates as 'lat, lon' (example: 32.853359101557814, -97.25425341002087)"; \
		printf "Coordinates: "; read -r COORDS_IN; \
		LAT_IN="$$(printf '%s' "$$COORDS_IN" | cut -d',' -f1 | tr -d ' ')"; \
		LON_IN="$$(printf '%s' "$$COORDS_IN" | cut -d',' -f2 | tr -d ' ')"; \
		if [ -z "$$LAT_IN" ] || [ -z "$$LON_IN" ]; then \
			echo "Could not parse 'lat, lon' input; entering manual mode."; \
			printf "Latitude: "; read -r LAT_IN; \
			printf "Longitude: "; read -r LON_IN; \
		fi; \
		./bin/pp-starlink set-location --lat "$$LAT_IN" --lon "$$LON_IN"; \
	fi
	@echo "sync-start complete."
	@echo "  Go daemon:    ./bin/pp-starlink daemon"
	@echo "  Live status:  ./bin/pp-starlink status --json"
	@echo "  RCA pipeline: venv/bin/pp-starlink analyze && venv/bin/pp-starlink report"
	@echo "  AI bundle:    venv/bin/pp-starlink report --ai-bundle"

# One-command local bootstrap for developers and agents.
# Creates/updates venv and installs lockfile deps.
# Set BOOTSTRAP_BUILD=1 to also attempt binary build.
bootstrap:
	@if [ ! -x venv/bin/python ]; then python3.13 -m venv venv; fi
	venv/bin/pip install -r requirements.lock -r requirements-dev.lock
	venv/bin/pip install -e . --no-deps
	mkdir -p cmd/pp-starlink/web/dist
	@if [ ! -f cmd/pp-starlink/web/dist/index.html ]; then \
		echo '<!doctype html><html><body><h1>pp-starlink</h1><p>UI not built. Run make web-build.</p></body></html>' > cmd/pp-starlink/web/dist/index.html; \
	fi
	@if [ "$(BOOTSTRAP_BUILD)" = "1" ]; then \
		$(MAKE) bootstrap-build; \
	fi
	@if [ "$(BOOTSTRAP_BUILD)" = "1" ]; then \
		echo "Bootstrap complete: venv ready and binaries built."; \
	else \
		echo "Bootstrap complete: venv ready. Set BOOTSTRAP_BUILD=1 to also build binaries."; \
	fi

# Strict build target for local binaries.
bootstrap-build:
	go mod download
	go mod tidy
	go build -o bin/pp-starlink ./cmd/pp-starlink
	go build -o bin/e2e ./cmd/e2e
	@if [ "$(BOOTSTRAP_WEB)" = "1" ]; then $(MAKE) web-build; fi
	@echo "Build complete: bin/pp-starlink and bin/e2e"

# Optional stricter bootstrap that also validates the Athena baseline suite.
bootstrap-check: bootstrap
	@if [ -x venv/bin/athena ]; then \
		venv/bin/athena test tests/athena; \
	else \
		echo "Athena CLI is not installed in venv. Install it, then run: venv/bin/athena test tests/athena"; \
		exit 1; \
	fi

# One-command cleanup for local generated artifacts.
# Safe for source files: restores only tracked __pycache__ artifacts and
# removes common local build outputs.
cleanup:
	rm -rf bin
	rm -rf web/node_modules
	rm -rf .athena/output
	find . -type d -name "__pycache__" -prune -exec rm -rf {} +
	@echo "Cleanup complete."
	@echo "Cleanup complete: removed local artifacts and restored tracked cache files."

# Pull live descriptor from dish to confirm field numbers still match:
#   make verify-dish
verify-dish:
	grpcurl -plaintext 192.168.100.1:9200 describe SpaceX.API.Device.DishGetStatus
	grpcurl -plaintext 192.168.100.1:9200 describe SpaceX.API.Device.DishAlerts

# Generate Go stubs from proto/device.proto
proto:
	mkdir -p $(GEN_DIR)
	protoc \
		--go_out=. --go_opt=module=pp-starlink \
		--go-grpc_out=. --go-grpc_opt=module=pp-starlink \
		$(PROTO_DIR)/device.proto

deps:
	go mod download
	go mod tidy

web-build:
	cd web && npm install --prefer-offline && npm run build

build: deps web-build
	go build -o bin/pp-starlink ./cmd/pp-starlink
	go build -o bin/e2e ./cmd/e2e

up: web-build
	docker compose up --build

# Capture live gRPC schema snapshot from dish.
# Run after a firmware update to detect field-number drift.
refresh-schema:
	@echo "# Starlink Dish gRPC Schema" > schema/dish_schema.txt
	@echo "# Generated: $$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> schema/dish_schema.txt
	@echo "" >> schema/dish_schema.txt
	@for msg in \
		SpaceX.API.Device.Request \
		SpaceX.API.Device.Response \
		SpaceX.API.Device.DishGetStatusResponse \
		SpaceX.API.Device.DishGetHistoryResponse \
		SpaceX.API.Device.DishGetContextResponse \
		SpaceX.API.Device.DishAlerts \
		SpaceX.API.Device.DishOutage \
		SpaceX.API.Device.AlignmentStats \
		SpaceX.API.Device.DeviceInfo \
		SpaceX.API.Device.DeviceState \
		SpaceX.API.Device.DishObstructionStats \
		SpaceX.API.Device.EventLog \
		SpaceX.API.Device.UXEvent \
		SpaceX.API.Device.EventReason \
		SpaceX.API.Device.EventSeverity \
		"SpaceX.API.Telemetron.Public.Integrations.RateLimitReason" \
		"SpaceX.API.Satellites.Network.UtDisablementCode"; do \
		echo "## $$msg" >> schema/dish_schema.txt; \
		$(GRPCURL) -plaintext $(DISH) describe "$$msg" >> schema/dish_schema.txt 2>&1; \
		echo "" >> schema/dish_schema.txt; \
	done
	@echo "schema/dish_schema.txt updated"

# Required tools:
#   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
#   apt install protobuf-compiler grpcurl traceroute iputils-ping
