IMG ?= harbor.noderings.com/nrings/prometheus-virtfusion-exporter
TAG ?= dev
PLATFORMS ?= linux/amd64,linux/arm64

.PHONY: help
help:
	@echo "Targets:"
	@echo "  build        Build binary"
	@echo "  test         Run tests"
	@echo "  test-race    Run tests with -race"
	@echo "  lint         Run linter"
	@echo "  fmt          Format code"
	@echo "  tidy         Tidy dependencies"
	@echo "  clean        Clean build artifacts"
	@echo "  docker-build Build multi-arch image"
	@echo "  docker-push  Build and push multi-arch image"

.PHONY: build
build:
	@echo "Building prometheus-virtfusion-exporter"
	@CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags "-s -w" -o prometheus-virtfusion-exporter ./cmd/exporter
	@echo "Built successfully"

.PHONY: test
test:
	@echo "Running tests"
	@go test -v ./...

.PHONY: test-race
test-race:
	@echo "Running tests with -race"
	@go test -race -count=1 ./...

.PHONY: lint
lint:
	@golangci-lint run ./...

.PHONY: fmt
fmt:
	@go fmt ./...

.PHONY: tidy
tidy:
	@go mod tidy

.PHONY: clean
clean:
	@rm -f prometheus-virtfusion-exporter

.PHONY: docker-buildx-setup
docker-buildx-setup:
	@docker buildx version >/dev/null 2>&1 || (echo "Error: docker buildx not available" && exit 1)
	@if ! docker buildx inspect multiarch-builder >/dev/null 2>&1; then \
		docker buildx create --name multiarch-builder --driver docker-container --use || true; \
		docker buildx inspect --bootstrap; \
	fi

.PHONY: docker-build
docker-build: docker-buildx-setup
	docker buildx build \
		--platform $(PLATFORMS) \
		-t $(IMG):$(TAG) \
		.

.PHONY: docker-push
docker-push: docker-buildx-setup
	docker buildx build \
		--platform $(PLATFORMS) \
		-t $(IMG):$(TAG) \
		--push \
		.
