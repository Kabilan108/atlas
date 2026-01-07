.PHONY: build install deps clean test fmt check run

VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

build/atlas: $(shell find . -name '*.go')
	CGO_ENABLED=0 go build $(LDFLAGS) -o build/atlas ./cmd/atlas

build: build/atlas

install:
	go install

deps:
	go mod tidy

clean:
	rm -f build/atlas

test:
	go test -v -cover ./...

fmt:
	go fmt ./...

check:
	go vet ./...

run: build
