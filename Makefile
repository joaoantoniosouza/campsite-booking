.PHONY: build run test

build:
	go build ./...

run:
	go run ./cmd/server

test:
	go test ./...
