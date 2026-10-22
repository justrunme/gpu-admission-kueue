export PATH := /usr/local/go/bin:$(HOME)/bin:$(PATH)

.PHONY: help tidy fmt vet test corpus evidence build image demo e2e clean

help:
	@echo "Targets: tidy fmt vet test corpus evidence build image demo e2e"

tidy:
	go mod tidy

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

vet:
	go vet ./...

test:
	go test ./... -count=1

corpus: test
	go run ./cmd/corpus
	go run ./cmd/evidencegen

evidence: corpus

build:
	CGO_ENABLED=0 go build -o bin/controller ./cmd/controller

image:
	docker build -t gpu-admission-controller:e2e .

demo:
	bash demo/setup.sh

e2e:
	bash test/e2e/run.sh

clean:
	rm -rf bin/
	kind delete cluster --name gpu-admission || true
