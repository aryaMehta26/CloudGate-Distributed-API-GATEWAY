# ── CloudGate Makefile ────────────────────────────────────────────────────────
# Common developer tasks. Run `make help` to see all targets.
# ─────────────────────────────────────────────────────────────────────────────

BINARY_GATEWAY  := bin/cloudgate-gateway
BINARY_ADMIN    := bin/cloudgate-admin
MODULE          := github.com/aryaMehta26/cloudgate
GO              := go
GOFLAGS         := -trimpath
LDFLAGS         := -s -w
GOLANGCI_LINT   := golangci-lint

.PHONY: all build run-gateway run-admin test test-race bench lint fmt vet tidy \
        deps-up deps-down proto proto-check clean help

## all: Build all binaries (default)
all: build

## build: Compile gateway and admin binaries
build:
	@mkdir -p bin
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY_GATEWAY) ./cmd/gateway
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY_ADMIN)   ./cmd/admin
	@echo "Build complete: $(BINARY_GATEWAY)  $(BINARY_ADMIN)"

## run-gateway: Start the gateway (requires deps to be up)
run-gateway:
	$(GO) run ./cmd/gateway/...

## run-admin: Start the gRPC admin service
run-admin:
	$(GO) run ./cmd/admin/...

## test: Run all unit tests
test:
	$(GO) test ./... -v -count=1

## test-race: Run tests with race detector
test-race:
	$(GO) test ./... -race -count=1

## bench: Run all benchmarks
bench:
	$(GO) test ./... -bench=. -benchmem -run='^$$'

## cover: Run tests with HTML coverage report
cover:
	$(GO) test ./... -coverprofile=coverage.out
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## lint: Run golangci-lint
lint:
	$(GOLANGCI_LINT) run ./...

## fmt: Format all Go files
fmt:
	$(GO) fmt ./...
	gofmt -s -w .

## vet: Run go vet
vet:
	$(GO) vet ./...

## tidy: Tidy go.mod and go.sum
tidy:
	$(GO) mod tidy

## deps-up: Start local dependencies (Redis, Cassandra, Prometheus, Grafana)
deps-up:
	docker compose up -d
	@echo "Waiting for Cassandra to be ready (this takes ~60s on first start)..."
	@until docker exec cloudgate-cassandra cqlsh -e "describe cluster" > /dev/null 2>&1; do \
		sleep 5; echo "  still waiting..."; \
	done
	@echo "All dependencies are up."

## deps-down: Stop local dependencies
deps-down:
	docker compose down

## proto: Regenerate gRPC code from .proto files (requires protoc + plugins)
proto:
	protoc \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/admin/admin.proto

## proto-check: Fail if the committed gRPC code differs from what protoc generates
proto-check: proto
	git diff --exit-code -- proto/

## check: Run fmt, vet, lint, and race tests in one shot (use before every PR)
check: fmt vet lint test-race

## bench-save: Save current benchmark as baseline for comparison
bench-save:
	$(GO) test ./... -bench=. -benchmem -run='^$$' | tee bench-baseline.txt
	@echo "Baseline saved to bench-baseline.txt"

## bench-compare: Compare current benchmarks against baseline
bench-compare:
	$(GO) test ./... -bench=. -benchmem -run='^$$' | tee bench-current.txt
	@if command -v benchcmp > /dev/null; then \
		benchcmp bench-baseline.txt bench-current.txt; \
	else \
		echo "Install benchcmp: go install golang.org/x/tools/cmd/benchcmp@latest"; \
	fi

## install-tools: Install all required development tools
install-tools:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.59.1
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install golang.org/x/tools/cmd/benchcmp@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.1
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.4.0
	@echo "All tools installed."

## clean: Remove build artifacts
clean:
	rm -rf bin coverage.out coverage.html bench-baseline.txt bench-current.txt

## help: Print this help message
help:
	@grep -E '^## ' Makefile | sed 's/## //' | column -t -s ':'
