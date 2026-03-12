.PHONY: run build test help

# Default config file (without extension)
CONFIG ?= config.local

help:
	@echo "Available commands:"
	@echo "  make run    - Run server locally (uses config.local.yaml by default)"
	@echo "  make build  - Build server binary"
	@echo "  make test   - Run quick tests"

run:
	@if [ ! -f $(CONFIG).yaml ]; then \
		echo "Error: $(CONFIG).yaml not found!"; \
		echo "Please check if config.local.yaml exists or specify another config via CONFIG=..."; \
		exit 1; \
	fi
	go run ./cmd/server -config $(CONFIG)

build:
	go build -o server ./cmd/server

test:
	./scripts/test-quick.sh
