.PHONY: test lint build

test:
	go test ./...

lint:
	golangci-lint run ./...

build:
	go build -o bin/gographify ./cmd/gographify
