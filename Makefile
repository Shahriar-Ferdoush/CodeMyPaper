# Leading v stripped to match GoReleaser's {{.Version}}, so local, release and
# image builds all print the same string.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo dev)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"
IMAGE ?= codemypaper
# Host dir mounted at /work/out; separate from out/ so runs don't mix.
DOCKER_OUT ?= docker_out

.DEFAULT_GOAL := build

.PHONY: build install test lint fmt fmt-check clean docker-build docker-run docker-shell

build:
	go build $(LDFLAGS) -o bin/codemypaper ./cmd/codemypaper

install:
	go install $(LDFLAGS) ./cmd/codemypaper

test:
	go test ./...

lint:
	go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run; else echo "golangci-lint not installed; skipping"; fi

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed on:"; echo "$$out"; exit 1; fi

clean:
	rm -rf bin

# Same VERSION as `build`, so image and binary report the same string.
docker-build:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

# make docker-run ARGS="run 2401.12345 --verbose"
# mkdir first: Docker would create a missing bind source root-owned on Linux.
docker-run:
	mkdir -p $(DOCKER_OUT)
	docker run --rm \
		-e GEMINI_API_KEY \
		-v $(CURDIR)/$(DOCKER_OUT):/work/out \
		$(IMAGE):latest $(ARGS)

# Shell in the image, same mount, for running commands by hand.
docker-shell:
	mkdir -p $(DOCKER_OUT)
	docker run --rm -it \
		-e GEMINI_API_KEY \
		-v $(CURDIR)/$(DOCKER_OUT):/work/out \
		--entrypoint bash \
		$(IMAGE):latest
