SHELL := /bin/sh

BINARY := wh-cli
PKG := ./...
SQLITE_TAGS := sqlite_omit_load_extension

.PHONY: build run test test-e2e lint ci openapi pre-commit migrate-new clean

build:
	go build -tags "$(SQLITE_TAGS)" -o bin/$(BINARY) ./cmd/wh-cli

run:
	go run -tags "$(SQLITE_TAGS)" ./cmd/wh-cli daemon --listen 127.0.0.1:7777

test:
	go test -tags "$(SQLITE_TAGS)" $(PKG)

test-e2e:
	FIXTURE_QR=1 go test -tags "$(SQLITE_TAGS)" ./...

lint:
	gofumpt -w=false .
	golangci-lint run

ci: lint test build

openapi:
	oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml

pre-commit:
	pre-commit run --all-files

migrate-new:
	@test -n "$(name)" || (echo "usage: make migrate-new name=<name>" && exit 4)
	migrate create -ext sql -dir migrations -seq "$(name)"

clean:
	rm -rf bin
