GOCACHE ?= $(CURDIR)/.cache/go-build

.PHONY: all test bench legacy install clean

all: test

test:
	GOCACHE=$(GOCACHE) go test ./...

bench:
	GOCACHE=$(GOCACHE) go test -bench . ./...

legacy:
	JS_BEAUTIFY_GO_LEGACY=1 GOCACHE=$(GOCACHE) go test ./internal/html ./internal/javascript ./internal/css -run TestLegacy -count=1

install:
	GOCACHE=$(GOCACHE) go install ./cmd/...

clean:
	rm -rf .cache
