.PHONY: up down build test test-race test-concurrency vet fmt clean help

help:
	@echo "Available commands:"
	@echo "  make up               - Build and start full stack in Docker Compose"
	@echo "  make down             - Stop all containers and remove volumes"
	@echo "  make test             - Run all unit and integration tests"
	@echo "  make test-race        - Run tests with Go race detector (-race)"
	@echo "  make test-concurrency - Run real multi-threaded concurrency scenarios"
	@echo "  make vet              - Run go vet static analysis"
	@echo "  make fmt              - Format all Go source files"

up:
	docker compose up --build

down:
	docker compose down -v

test:
	go test -v ./...

test-race:
	go test -race -v ./...

test-concurrency:
	go test -v -race -run TestTwoSimultaneousBetsDispute ./tests/integration/...
	go test -v -race -run TestFiftySimultaneousIdenticalBets ./tests/integration/...

test-load:
	go test -v -run TestLoadBenchmark ./tests/integration/...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

clean:
	rm -rf bin/
