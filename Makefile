VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X main.version=$(VERSION)

.PHONY: build test vet install cross clean

build:
	go build -ldflags "$(LDFLAGS)" -o wow-mcp ./cmd/wow-mcp

test:
	go test ./...

vet:
	go vet ./...

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/wow-mcp

# Binaries for the machines WoW actually runs on.
cross:
	mkdir -p dist
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/wow-mcp-windows-amd64.exe ./cmd/wow-mcp
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/wow-mcp-darwin-arm64 ./cmd/wow-mcp
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/wow-mcp-darwin-amd64 ./cmd/wow-mcp
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/wow-mcp-linux-amd64 ./cmd/wow-mcp

clean:
	rm -rf wow-mcp wow-mcp.exe dist
