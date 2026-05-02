.PHONY: build test run clean docker lint

BINARY_NAME=go-gradle-cache
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS=-ldflags "-w -s -X main.version=$(VERSION)"

build:
	cd app && go build $(LDFLAGS) -o ../$(BINARY_NAME) .

test:
	cd app && go test -race -cover ./...

run:
	cd app && go run $(LDFLAGS) . -storage=local -dir=../cache-data

lint:
	cd app && golangci-lint run ./...

clean:
	rm -f $(BINARY_NAME)
	rm -rf cache-data

docker:
	docker build -t $(BINARY_NAME):$(VERSION) .
