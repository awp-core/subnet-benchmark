.PHONY: benchmarkd test

.DEFAULT_GOAL := benchmarkd

test:
	go test -p 1 ./...

benchmarkd:
	go build -o bin/benchmarkd ./cmd/benchmarkd
