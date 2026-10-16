#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
backend_dir="$repo_dir/backend"

if [[ -n "${GO_BIN:-}" ]]; then
  go_bin="$GO_BIN"
elif command -v go >/dev/null 2>&1; then
  go_bin="$(command -v go)"
else
  echo "Go 1.27 is required; set GO_BIN to the Go 1.27 binary." >&2
  exit 1
fi

go_version="$("$go_bin" version)"
if [[ "$go_version" != *"go1.27"* ]]; then
  echo "Go 1.27 is required, found: $go_version" >&2
  exit 1
fi

cd "$backend_dir"
GOPROXY="${GOPROXY:-off}" "$go_bin" test -tags=unit \
  ./internal/service ./internal/server/middleware ./internal/repository \
  ./internal/server/routes ./internal/handler ./migrations \
  -run 'Auto|ExecutionPreflight|ResolveCandidates|GetKeyRoutingState|Playground|RequireGroupAssignment|APIKeyAuth|KeyBilling|UsageLog|FixedKeySettlement' -count=1
GOPROXY="${GOPROXY:-off}" "$go_bin" test ./cmd/server -run '^$'

if [[ -n "${PGLITE_MODULE:-}" ]]; then
  node "$repo_dir/tools/verify-auto-routing-migration.mjs"
fi
