.PHONY: build lint test dev-up dev-down dev templates seed e2e-m0 e2e-m1 e2e-m2

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/forged-amd64 ./cmd/forged
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/forge-gateway-amd64 ./cmd/forge-gateway
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/forge-llmproxy-amd64 ./cmd/forge-llmproxy
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/forge-noded-amd64 ./cmd/forge-noded
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/forge-shim-amd64 ./cmd/forge-shim
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/forged-arm64 ./cmd/forged
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/forge-gateway-arm64 ./cmd/forge-gateway
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/forge-llmproxy-arm64 ./cmd/forge-llmproxy
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/forge-noded-arm64 ./cmd/forge-noded
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/forge-shim-arm64 ./cmd/forge-shim

lint:
	go vet ./...

test:
	go test ./...

dev-up:
	sudo docker compose -f deploy/dev/docker-compose.yml up -d

dev-down:
	sudo docker compose -f deploy/dev/docker-compose.yml down -v

dev:
	# Stub for dev
	echo "dev"

templates:
	# Stub for templates
	echo "templates"

seed:
	# Stub for seed
	echo "seed"

e2e-m0:
	bash e2e/e2e-m0.sh

e2e-m1:
	bash e2e/e2e-m1.sh

e2e-m2:
	bash e2e/e2e-m2.sh
