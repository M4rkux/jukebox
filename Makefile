.PHONY: run build tidy dev

# Load .env if it exists
ifneq (,$(wildcard ./.env))
  include .env
  export
endif

run:
	go run ./cmd/server/

build:
	go build -o bin/jukebox ./cmd/server/

tidy:
	go mod tidy

dev:
	@which air > /dev/null 2>&1 || go install github.com/cosmtrek/air@latest
	air
