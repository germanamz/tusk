BINARY_NAME := tusk
BUILD_DIR := bin
GO := go
GOFLAGS := -v
CGO_ENABLED := 1

.PHONY: all build clean test test-race test-e2e vet lint run install

all: build

build:
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/tusk

clean:
	rm -rf $(BUILD_DIR)

test:
	CGO_ENABLED=$(CGO_ENABLED) $(GO) test $(GOFLAGS) ./...

test-race:
	CGO_ENABLED=$(CGO_ENABLED) $(GO) test $(GOFLAGS) -race ./...

test-e2e:
	CGO_ENABLED=$(CGO_ENABLED) $(GO) test $(GOFLAGS) ./tests/e2e/

vet:
	$(GO) vet ./...

lint:
	golangci-lint run ./...

run: build
	./$(BUILD_DIR)/$(BINARY_NAME)

install:
	CGO_ENABLED=$(CGO_ENABLED) $(GO) install $(GOFLAGS) ./cmd/tusk
