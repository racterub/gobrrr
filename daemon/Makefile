.PHONY: test build

test:
	go test -race ./...

build:
	CGO_ENABLED=0 go build -o gobrrr ./cmd/gobrrr/
