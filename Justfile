default:
    @just --list

fmt:
    golangci-lint fmt

test:
    go test -race ./...

lint:
    golangci-lint run ./...

build:
    go build -o bin/veer .

run:
    go run .
