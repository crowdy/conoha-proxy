.PHONY: build test lint docker clean

GO ?= go
BIN := conoha-proxy

build:
	$(GO) build -o bin/$(BIN) ./cmd/conoha-proxy

test:
	$(GO) test -race -coverprofile=coverage.txt ./...

lint:
	golangci-lint run
	staticcheck ./...

e2e:
	$(GO) test -tags=e2e -timeout=5m ./test/e2e/...

docker:
	docker build -t ghcr.io/crowdy/conoha-proxy:dev .

clean:
	rm -rf bin/ dist/ coverage.txt
