#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_dir"

if [[ -n "$(git status --porcelain)" ]]; then
  echo "Working tree is not clean. Commit or stash changes before syncing." >&2
  exit 1
fi

if ! git remote get-url upstream >/dev/null 2>&1; then
  git remote add upstream https://github.com/Wei-Shaw/sub2api.git
fi

current_branch="$(git branch --show-current)"
if [[ "$current_branch" != "custom/community-qrcode" ]]; then
  echo "Run this script from the custom/community-qrcode branch." >&2
  exit 1
fi

git fetch --prune upstream main
before="$(git rev-parse --short HEAD)"
upstream_commit="$(git rev-parse --short upstream/main)"

if git merge-base --is-ancestor upstream/main HEAD; then
  echo "Already synchronized with upstream/main ($upstream_commit)."
else
  backup_tag="backup/community-qrcode-${before}-$(date +%Y%m%d%H%M%S)"
  git tag "$backup_tag"
  echo "Created rollback tag: $backup_tag"
  git rebase upstream/main
fi

./tools/verify-community.sh

image_tag="sub2api-community:${upstream_commit}-$(git rev-parse --short HEAD)"
if command -v docker >/dev/null 2>&1; then
  docker buildx build --platform linux/amd64 --load \
    --build-arg VERSION="community-${upstream_commit}" \
    --build-arg COMMIT="$(git rev-parse HEAD)" \
    -t "$image_tag" .
  echo "Built image: $image_tag"
else
  echo "Docker is unavailable; source synchronization and verification completed."
  echo "Build later with: docker buildx build --platform linux/amd64 --load -t $image_tag ."
fi

echo "Production was not restarted."
