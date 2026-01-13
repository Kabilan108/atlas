.PHONY: bin install deps clean test fmt check run

VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

bin/atlas: $(shell find . -name '*.go')
	CGO_ENABLED=0 go build $(LDFLAGS) -o bin/atlas ./cmd/atlas

build: bin/atlas

install:
	go install

deps:
	go mod tidy

clean:
	rm -f bin/atlas

test:
	CGO_ENABLED=0 go test -v -cover ./...

fmt:
	go fmt ./...

check:
	CGO_ENABLED=0 go vet ./...

run: build
