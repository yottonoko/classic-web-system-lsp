set shell := ["bash", "-uc"]

binary := "asp-lsp-go"
go_package := "./cmd/asp-lsp-go"
ldflags := "-s -w -buildid="

_build os arch:
  @set -eu; \
    ext=""; \
    if [ "{{os}}" = "windows" ]; then ext=".exe"; fi; \
    out="bin/{{os}}-{{arch}}/{{binary}}${ext}"; \
    mkdir -p "$(dirname "$out")"; \
    CGO_ENABLED=0 GOOS={{os}} GOARCH={{arch}} \
      go build -trimpath -buildvcs=false -ldflags="{{ldflags}}" -o "$out" {{go_package}}; \
    echo "$out"

mac arch="arm64":
  just _build darwin {{arch}}

windows arch="amd64":
  just _build windows {{arch}}

linux arch="amd64":
  just _build linux {{arch}}

cross-build: mac windows linux

test-race:
  go test -race ./...
