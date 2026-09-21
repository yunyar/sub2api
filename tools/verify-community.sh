#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_dir"

if [[ -n "${PNPM_BIN:-}" ]]; then
  pnpm_cmd=("$PNPM_BIN")
elif command -v pnpm >/dev/null 2>&1; then
  pnpm_cmd=(pnpm)
elif command -v corepack >/dev/null 2>&1; then
  pnpm_cmd=(corepack pnpm)
else
  echo "pnpm or corepack is required for frontend verification." >&2
  exit 1
fi

"${pnpm_cmd[@]}" --dir frontend run check:i18n
"${pnpm_cmd[@]}" --dir frontend run typecheck
"${pnpm_cmd[@]}" --dir frontend exec vitest run \
  src/api/__tests__/communityQRCodes.spec.ts \
  src/api/__tests__/playground.spec.ts \
  src/api/__tests__/url.spec.ts \
  src/utils/__tests__/playgroundId.spec.ts \
  src/utils/__tests__/playgroundIntent.spec.ts \
  src/utils/__tests__/playgroundModel.spec.ts \
  src/views/user/__tests__/PlaygroundView.spec.ts \
  src/components/playground/__tests__/WorkflowPlayground.spec.ts

if [[ -n "${GO_BIN:-}" ]]; then
  go_bin="$GO_BIN"
elif command -v go >/dev/null 2>&1; then
  go_bin="$(command -v go)"
else
  echo "Go is unavailable; set GO_BIN to a Go 1.27 binary." >&2
  exit 1
fi

(cd backend && "$go_bin" test ./internal/server/routes -run Playground)
(cd backend && "$go_bin" test -tags unit ./internal/service -run 'CommunityQRCodes|ResolvePlaygroundKey')
(cd backend && "$go_bin" test -tags unit ./internal/service -run 'ImageRateAlways|ImageCountOverridesChannelTokenPricing|ImageMultiplierIgnores|ChannelImageBillingUsesImageCount|ListPlazaGroups_GroupImagePrice|BatchImagePublicService_Submit')
