.PHONY: build run test docker-build clean

BINARY := claude-code-otel-exporter
IMAGE  := claude-code-otel-exporter

SOURCE_DIR         ?= $(HOME)/.claude/projects
STATE_FILE_PATH    ?= /tmp/otel-state.json
COLLECTOR_ENDPOINT ?= localhost:4317

build:
	go build -o $(BINARY) ./cmd/exporter

run: build
	SOURCE_DIR=$(SOURCE_DIR) \
	STATE_FILE_PATH=$(STATE_FILE_PATH) \
	COLLECTOR_ENDPOINT=$(COLLECTOR_ENDPOINT) \
	./$(BINARY)

test:
	go test ./...

docker-build:
	docker build -t $(IMAGE) .

clean:
	rm -f $(BINARY)
