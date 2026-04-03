BINARY_NAME := tusk
BUILD_DIR := bin
GO := go
GOFLAGS := -v

.PHONY: all build clean test test-race test-e2e vet lint run install

all: build

build:
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/tusk

clean:
	rm -rf $(BUILD_DIR)

test:
	$(GO) test $(GOFLAGS) ./...

test-race:
	$(GO) test $(GOFLAGS) -race ./...

test-e2e:
	$(GO) test $(GOFLAGS) ./tests/e2e/

vet:
	$(GO) vet ./...

lint:
	golangci-lint run ./...

run: build
	./$(BUILD_DIR)/$(BINARY_NAME)

install:
	$(GO) install $(GOFLAGS) ./cmd/tusk
