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
	rm -rf atlas-linux-amd64
	rm -f atlas-linux-amd64.tar.gz

test:
	go test -v -cover ./...

fmt:
	go fmt ./...

run: build
