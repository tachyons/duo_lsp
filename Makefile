BINARY     := duo-lsp
CMD        := ./cmd/duo-lsp
BUILD_DIR  := .

GO         := go
GOFLAGS    ?=

.PHONY: all build run lint fmt tidy test test-verbose clean install help

all: build

## build: compile the binary
build:
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY) $(CMD)

## run: build and run the LSP server
run: build
	./$(BINARY) serve

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## fmt: format code with gofmt and goimports
fmt:
	gofmt -w .
	goimports -w .

## tidy: tidy and verify go modules
tidy:
	$(GO) mod tidy
	$(GO) mod verify

## test: run all tests
test:
	$(GO) test ./...

## test-verbose: run all tests with verbose output
test-verbose:
	$(GO) test -v ./...

## clean: remove build artifacts
clean:
	rm -f $(BUILD_DIR)/$(BINARY)

## install: install the binary to GOPATH/bin
install:
	$(GO) install $(GOFLAGS) $(CMD)

## help: print this help message
help:
	@grep -E '^##' $(MAKEFILE_LIST) | sed 's/## /  /'
