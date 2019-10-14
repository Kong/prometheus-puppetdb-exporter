.PHONY: all unit e2e bin tests

bin:
	go build -ldflags="-X main.version=$(shell git describe) -X main.commitSha1=$(shell git rev-parse HEAD) -X main.buildDate=$(shell date -u +%Y%m%d)"

unit:
	go test -race $(shell go list ./... | grep -v e2e)

e2e:
	go test -race $(shell go list ./... | grep e2e)

vet:
	go vet ./...

tests: vet unit e2e

all: unit e2e bin
