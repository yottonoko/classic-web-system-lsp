#!/bin/bash
# Prepares Claude Code cloud sessions: installs pnpm workspace dependencies and
# builds bin/asp-lsp-go, which also fetches Go modules and warms the build cache.
set -euo pipefail

if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

cd "${CLAUDE_PROJECT_DIR:-$(cd "$(dirname "$0")/../.." && pwd)}"

# Hook stdout is added to the session context, so keep tool output on stderr.
{
  pnpm install

  # The first go command fetches the toolchain go.mod requires (GOTOOLCHAIN=auto) and the
  # modules the build needs. Avoid `go mod download`: in workspace mode it rewrites go.work.sum.
  pnpm run build:go

  if ! command -v just >/dev/null 2>&1; then
    apt-get install -y --no-install-recommends just \
      || { apt-get update && apt-get install -y --no-install-recommends just; } \
      || echo "warning: could not install just; justfile recipes are unavailable" >&2
  fi
} 1>&2

echo "Session setup complete: pnpm dependencies installed, $(go version | cut -d' ' -f3) ready, bin/asp-lsp-go built."
