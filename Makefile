.PHONY: build run test lint docker-build docker-run clean

BINARY := claude-code-otel-exporter
IMAGE  := claude-code-otel-exporter

SOURCE_DIR         ?= $(HOME)/.claude/projects
STATE_FILE_PATH    ?= /tmp/otel-state.json
COLLECTOR_ENDPOINT ?= localhost:4317
COLLECTOR_INSECURE ?= true

build:
	go build -o $(BINARY) ./cmd/exporter

run: build
	SOURCE_DIR=$(SOURCE_DIR) \
	STATE_FILE_PATH=$(STATE_FILE_PATH) \
	COLLECTOR_ENDPOINT=$(COLLECTOR_ENDPOINT) \
	COLLECTOR_INSECURE=$(COLLECTOR_INSECURE) \
	./$(BINARY)

test:
	go test ./...

lint:
	go vet ./...

docker-build:
	docker build -t $(IMAGE) .

docker-run: docker-build
	docker run --rm \
		-e SOURCE_DIR=$(SOURCE_DIR) \
		-e STATE_FILE_PATH=/data/otel-state.json \
		-e COLLECTOR_ENDPOINT=$(COLLECTOR_ENDPOINT) \
		-e COLLECTOR_INSECURE=$(COLLECTOR_INSECURE) \
		-v $(SOURCE_DIR):$(SOURCE_DIR):ro \
		-v /tmp/otel-data:/data \
		$(IMAGE)

clean:
	rm -f $(BINARY)
