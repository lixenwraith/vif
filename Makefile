.PHONY: generate build run clean

generate:
	go generate ./manifest/...

build: generate
	go build -o bin/vi-fighter ./cmd/vi-fighter

run: build
	./bin/vi-fighter

clean:
	rm -rf bin/
