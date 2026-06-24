.PHONY: build test run clean docker lint

BINARY_NAME=go-http-cache-server
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS=-ldflags "-w -s -X main.version=$(VERSION)"

build:
	go build $(LDFLAGS) -o $(BINARY_NAME) ./app

test:
	go test -race -cover ./app/...

run:
	go run $(LDFLAGS) ./app -storage=local -dir=./cache-data

lint:
	golangci-lint run ./app/...

clean:
	rm -f $(BINARY_NAME)
	rm -rf cache-data

docker:
	docker build -t $(BINARY_NAME):$(VERSION) -t $(BINARY_NAME):latest .
	docker push $(BINARY_NAME):$(VERSION)
	docker push $(BINARY_NAME):latest
