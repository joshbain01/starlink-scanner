PROTO_DIR  := proto
GEN_DIR    := proto/device
GRPCURL    := $(shell which grpcurl 2>/dev/null || echo $$HOME/go/bin/grpcurl)
DISH       := 192.168.100.1:9200

-include .athena/Makefile.inc

.PHONY: proto deps build web-build up verify-dish refresh-schema

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
