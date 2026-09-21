#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
helper="$repo_root/deploy/docker-update-helper.sh"
temp_dir=$(mktemp -d)
trap 'rm -rf "$temp_dir"' EXIT HUP INT TERM

fail() { printf 'docker update helper test failed: %s\n' "$1" >&2; exit 1; }

mkdir -p "$temp_dir/bin"
cat > "$temp_dir/bin/docker" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "$DOCKER_LOG"
if [ "$1" = compose ]; then
    shift
    compose_file=
    while [ "$#" -gt 0 ]; do
        case "$1" in
            -f) test -n "$compose_file" || compose_file=$2 ;;
            config)
                test "${MOCK_CONFIG_STATUS:-0}" -eq 0 || exit "$MOCK_CONFIG_STATUS"
                test "${2:-}" = --quiet && exit 0
                cat "$compose_file"
                exit 0
                ;;
            ps) printf '%s\n' mock-container; exit 0 ;;
            up) exit "${MOCK_UP_STATUS:-0}" ;;
        esac
        shift
    done
fi
if [ "$1" = inspect ]; then
    status=${MOCK_HEALTH_STATUS:-healthy}
    if [ -n "${MOCK_HEALTH_SEQUENCE:-}" ]; then
        count_file="$DOCKER_LOG.inspect-count"
        count=1
        if [ -f "$count_file" ]; then count=$(($(cat "$count_file") + 1)); fi
        printf '%s\n' "$count" > "$count_file"
        status=$(printf '%s\n' "$MOCK_HEALTH_SEQUENCE" | awk -F, -v field="$count" '{ print $field }')
    fi
    printf '%s\n' "$status"
fi
EOF
chmod +x "$temp_dir/bin/docker"

make_compose() {
    cat > "$temp_dir/docker-compose.yml" <<'EOF'
services:
  other:
    image: example/other:old
  sub2api:
    image: example/sub2api:old
    volumes:
      - ./data:/app/data
  postgres:
    image: postgres:16
EOF
}

run_helper() { DOCKER_LOG="$temp_dir/docker.log" PATH="$temp_dir/bin:$PATH" "$helper" "$@"; }

make_compose
run_helper stage ghcr.io/example/sub2api:new "$temp_dir/docker-compose.yml" sub2api
grep -Fqx '    image: ghcr.io/example/sub2api:new' "$temp_dir/docker-compose.yml" || fail 'stage did not replace target image'
grep -Fqx '    image: example/other:old' "$temp_dir/docker-compose.yml" || fail 'stage changed another service image'
test -f "$temp_dir/docker-compose.yml.update-backup" || fail 'stage did not create rollback backup'
test -f "$temp_dir/docker-compose.yml.update-stage" || fail 'stage did not create stage status'
test "$(stat -f '%Lp' "$temp_dir/docker-compose.yml.update-stage")" = 644 || fail 'stage status is not world-readable'
if grep -Fq ' up ' "$temp_dir/docker.log"; then fail 'stage changed running services'; fi
if run_helper stage ghcr.io/example/sub2api:other "$temp_dir/docker-compose.yml" sub2api; then fail 'repeated stage unexpectedly succeeded'; fi
grep -Fqx '    image: example/sub2api:old' "$temp_dir/docker-compose.yml.update-backup" || fail 'repeated stage replaced original backup'

UPDATE_DOCKER_COMPOSE_PROJECT_NAME=custom-project run_helper activate ghcr.io/example/sub2api:new "$temp_dir/docker-compose.yml" sub2api
grep -Fq -- '--project-directory '"$temp_dir" "$temp_dir/docker.log" || fail 'activate did not use compose directory'
grep -Fq -- '--project-name custom-project' "$temp_dir/docker.log" || fail 'activate did not preserve project name'
grep -Fq -- 'up -d --no-deps --force-recreate sub2api' "$temp_dir/docker.log" || fail 'activate did not recreate only target service'
test ! -e "$temp_dir/docker-compose.yml.update-backup" || fail 'activate did not clear backup'
test ! -e "$temp_dir/docker-compose.yml.update-stage" || fail 'activate did not clear stage status'

make_compose
mkdir "$temp_dir/docker-compose.yml.update-lock"
if run_helper stage ghcr.io/example/sub2api:new "$temp_dir/docker-compose.yml" sub2api; then fail 'concurrent helper unexpectedly acquired lock'; fi
test -d "$temp_dir/docker-compose.yml.update-lock" || fail 'rejected helper removed active lock'
rmdir "$temp_dir/docker-compose.yml.update-lock"

make_compose
run_helper stage ghcr.io/example/sub2api:new "$temp_dir/docker-compose.yml" sub2api
: > "$temp_dir/docker.log"
if run_helper activate ghcr.io/example/sub2api:other "$temp_dir/docker-compose.yml" sub2api; then fail 'mismatched image unexpectedly activated'; fi
grep -Fq -- 'up -d' "$temp_dir/docker.log" && fail 'mismatched image changed running service'
run_helper activate ghcr.io/example/sub2api:new "$temp_dir/docker-compose.yml" sub2api

make_compose
run_helper stage ghcr.io/example/sub2api:new "$temp_dir/docker-compose.yml" sub2api
sed -i.bak 's#ghcr.io/example/sub2api:new#ghcr.io/example/sub2api:other#' "$temp_dir/docker-compose.yml"
rm -f "$temp_dir/docker-compose.yml.bak"
: > "$temp_dir/docker.log"
if run_helper activate ghcr.io/example/sub2api:new "$temp_dir/docker-compose.yml" sub2api; then fail 'changed compose unexpectedly activated'; fi
grep -Fq -- 'up -d' "$temp_dir/docker.log" && fail 'changed compose modified running service'
test -e "$temp_dir/docker-compose.yml.update-backup" || fail 'changed compose removed backup'
test -e "$temp_dir/docker-compose.yml.update-stage" || fail 'changed compose removed stage status'
rm -f "$temp_dir/docker-compose.yml.update-backup" "$temp_dir/docker-compose.yml.update-stage"

make_compose
run_helper stage ghcr.io/example/sub2api:new "$temp_dir/docker-compose.yml" sub2api
rm -f "$temp_dir/docker.log.inspect-count"
if MOCK_HEALTH_SEQUENCE=unhealthy,healthy run_helper activate ghcr.io/example/sub2api:new "$temp_dir/docker-compose.yml" sub2api; then fail 'unhealthy update unexpectedly succeeded'; fi
grep -Fqx '    image: example/sub2api:old' "$temp_dir/docker-compose.yml" || fail 'failed activation did not restore image'
test ! -e "$temp_dir/docker-compose.yml.update-backup" || fail 'failed activation did not clear backup'
test ! -e "$temp_dir/docker-compose.yml.update-stage" || fail 'failed activation did not clear stage status'

make_compose
run_helper stage ghcr.io/example/sub2api:new "$temp_dir/docker-compose.yml" sub2api
rm -f "$temp_dir/docker.log.inspect-count"
if MOCK_HEALTH_SEQUENCE=unhealthy,unhealthy run_helper activate ghcr.io/example/sub2api:new "$temp_dir/docker-compose.yml" sub2api; then fail 'failed rollback unexpectedly succeeded'; fi
test -e "$temp_dir/docker-compose.yml.update-backup" || fail 'failed rollback removed backup'
test -e "$temp_dir/docker-compose.yml.update-stage" || fail 'failed rollback removed stage status'
rm -f "$temp_dir/docker-compose.yml.update-backup" "$temp_dir/docker-compose.yml.update-stage" "$temp_dir/docker.log.inspect-count"

make_compose
if MOCK_CONFIG_STATUS=1 run_helper stage ghcr.io/example/sub2api:new "$temp_dir/docker-compose.yml" sub2api; then fail 'invalid staged compose unexpectedly succeeded'; fi
grep -Fqx '    image: example/sub2api:old' "$temp_dir/docker-compose.yml" || fail 'invalid stage did not restore image'
test ! -e "$temp_dir/docker-compose.yml.update-backup" || fail 'invalid stage did not clear backup'
test ! -e "$temp_dir/docker-compose.yml.update-stage" || fail 'invalid stage did not clear stage status'

printf 'docker update helper test passed\n'
