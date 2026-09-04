.PHONY: build test test-race verify verify-offline clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/skuggsja ./cmd/skuggsja

test:
	go test ./... -count=1

test-race:
	go test ./... -race -count=1

verify: test test-race build

verify-offline:
	./scripts/verify-runtime-offline.sh

clean:
	go clean
	$(RM) bin/skuggsja coverage.out
