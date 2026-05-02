.PHONY: build test run clean docker lint

BINARY_NAME=gradle-cache-server
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS=-ldflags "-w -s -X main.version=$(VERSION)"

build:
	go build $(LDFLAGS) -o $(BINARY_NAME) ./app

test:
	go test -race -cover ./...

run:
	go run $(LDFLAGS) ./app -storage=local -dir=./cache-data

lint:
	golangci-lint run ./...

clean:
	rm -f $(BINARY_NAME)
	rm -rf cache-data

docker:
	docker build -t $(BINARY_NAME):$(VERSION) .
