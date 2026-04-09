.PHONY: build build-dev run run-dev test test-dev clean

build:
	go build -o bin/server .

build-dev:
	go build -tags dev -o bin/server-dev .

run: build
	./bin/server

run-dev: build-dev
	./bin/server-dev

test:
	go test ./...

test-dev:
	go test -tags dev ./...

clean:
	rm -rf bin/
