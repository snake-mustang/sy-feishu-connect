BINARY := sy-feishu-codex

.PHONY: build test run

build:
	go build -o bin/$(BINARY) ./cmd/sy-feishu-codex

build-windows:
	go build -o bin/$(BINARY).exe ./cmd/sy-feishu-codex

test:
	go test ./...

run:
	go run ./cmd/sy-feishu-codex -config config.toml
