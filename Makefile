.PHONY: up down test build logs test-integration run dev

up:
	docker-compose up -d

down:
	docker-compose down

test:
	go test ./... -v -race

build:
	go build ./cmd/gateway

logs:
	docker-compose logs -f gateway

test-integration:
	go test -tags integration -v ./...

run:
	go run ./cmd/gateway

dev:
	docker-compose up -d && go run ./cmd/gateway
