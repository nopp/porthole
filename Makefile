.PHONY: test build demo docker

test:
	go test ./...

build:
	CGO_ENABLED=0 go build -trimpath -o bin/porthole ./cmd/porthole

demo: build
	./bin/porthole serve --manifests testdata/demo --addr :8080

docker:
	docker build -t porthole:dev .
