#!/bin/sh
set -eu

operation=${1:-}
image=${2:-}
compose_file=${3:-}
service_name=${4:-sub2api}

fail() {
    printf '%s\n' "$*" >&2
    exit 1
}

[ -n "$image" ] || fail 'update image is required'
[ -n "$compose_file" ] || fail 'absolute compose file path is required'
[ -n "$service_name" ] || fail 'compose service is required'
case "$compose_file" in /*) ;; *) fail 'compose file path must be absolute' ;; esac
[ -f "$compose_file" ] || fail "compose file does not exist: $compose_file"

compose_dir=$(CDPATH= cd -- "$(dirname -- "$compose_file")" && pwd)
compose_file="$compose_dir/$(basename -- "$compose_file")"
backup_file="${compose_file}.update-backup"
stage_file="${compose_file}.update-stage"
lock_dir="${compose_file}.update-lock"
tmp_file=
overlay_file=${UPDATE_DOCKER_COMPOSE_OVERLAY_FILE:-}

if [ -n "$overlay_file" ]; then
    case "$overlay_file" in /*) ;; *) fail 'compose overlay path must be absolute' ;; esac
    [ -f "$overlay_file" ] || fail "compose overlay does not exist: $overlay_file"
fi

compose() {
    project_name=${UPDATE_DOCKER_COMPOSE_PROJECT_NAME:-${COMPOSE_PROJECT_NAME:-}}
    if [ -n "$project_name" ]; then
        if [ -n "$overlay_file" ]; then
            docker compose --project-directory "$compose_dir" --project-name "$project_name" -f "$compose_file" -f "$overlay_file" "$@"
        else
            docker compose --project-directory "$compose_dir" --project-name "$project_name" -f "$compose_file" "$@"
        fi
    else
        if [ -n "$overlay_file" ]; then
            docker compose --project-directory "$compose_dir" -f "$compose_file" -f "$overlay_file" "$@"
        else
            docker compose --project-directory "$compose_dir" -f "$compose_file" "$@"
        fi
    fi
}

service_image() {
    awk -v service="$service_name" '
        BEGIN { in_services = 0; in_target = 0 }
        /^[[:space:]]*services:[[:space:]]*(#.*)?$/ { in_services = 1; next }
        {
            indent = match($0, /[^[:space:]]/) - 1
            if (in_services && indent == 2 && $0 ~ "^[[:space:]]*" service ":[[:space:]]*(#.*)?$") {
                in_target = 1
            } else if (in_services && indent == 2 && $0 ~ /^[[:space:]]*[A-Za-z0-9_.-]+:[[:space:]]*(#.*)?$/) {
                in_target = 0
            } else if (in_services && indent == 0 && $0 !~ /^[[:space:]]*#/ && $0 !~ /^[[:space:]]*$/) {
                in_services = 0; in_target = 0
            }
            if (in_target && $0 ~ /^[[:space:]]*image:[[:space:]]*/) {
                sub(/^[[:space:]]*image:[[:space:]]*/, "")
                sub(/[[:space:]]*(#.*)?$/, "")
                print
                exit
            }
        }
    '
}

replace_service_image() {
    output_file=$1
    awk -v service="$service_name" -v image="$image" '
        BEGIN { in_services = 0; in_target = 0; found_service = 0; replaced = 0 }
        /^[[:space:]]*services:[[:space:]]*(#.*)?$/ { in_services = 1; print; next }
        {
            indent = match($0, /[^[:space:]]/) - 1
            if (in_services && indent == 2 && $0 ~ "^[[:space:]]*" service ":[[:space:]]*(#.*)?$") {
                in_target = 1; found_service = 1
            } else if (in_services && indent == 2 && $0 ~ /^[[:space:]]*[A-Za-z0-9_.-]+:[[:space:]]*(#.*)?$/) {
                in_target = 0
            } else if (in_services && indent == 0 && $0 !~ /^[[:space:]]*#/ && $0 !~ /^[[:space:]]*$/) {
                in_services = 0; in_target = 0
            }
            if (in_target && !replaced && $0 ~ /^[[:space:]]*image:[[:space:]]*/) {
                sub(/image:[[:space:]]*.*/, "image: " image); replaced = 1
            }
            print
        }
        END { if (!found_service || !replaced) exit 1 }
    ' "$compose_file" > "$output_file"
}

wait_for_health() {
    timeout=${UPDATE_DOCKER_HEALTH_TIMEOUT_SECONDS:-120}
    case "$timeout" in ''|*[!0-9]*) fail 'UPDATE_DOCKER_HEALTH_TIMEOUT_SECONDS must be a non-negative integer' ;; esac
    elapsed=0
    while [ "$elapsed" -le "$timeout" ]; do
        container_id=$(compose ps -q "$service_name" 2>/dev/null || true)
        if [ -n "$container_id" ]; then
            status=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container_id" 2>/dev/null || true)
            case "$status" in healthy|running) return 0 ;; unhealthy|exited|dead) return 1 ;; esac
        fi
        sleep 1
        elapsed=$((elapsed + 1))
    done
    return 1
}

clear_stage() { rm -f "$backup_file" "$stage_file"; }

cleanup() {
    [ -z "$tmp_file" ] || rm -f "$tmp_file"
    rmdir "$lock_dir" 2>/dev/null || true
}

if ! mkdir "$lock_dir" 2>/dev/null; then
    fail 'another compose update is already in progress'
fi
trap cleanup EXIT HUP INT TERM

rollback() {
    [ -f "$backup_file" ] || return 1
    cp "$backup_file" "$compose_file"
    compose up -d --no-deps --force-recreate "$service_name"
    wait_for_health
}

case "$operation" in
    stage)
        [ ! -e "$backup_file" ] && [ ! -e "$stage_file" ] || fail 'a compose update is already staged'
        tmp_file="${compose_file}.update-tmp.$$"
        cp "$compose_file" "$backup_file"
        if ! replace_service_image "$tmp_file" || ! mv "$tmp_file" "$compose_file" || ! compose config --quiet || [ "$(compose config | service_image)" != "$image" ]; then
            cp "$backup_file" "$compose_file"
            clear_stage
            fail 'could not stage compose image update'
        fi
        printf '%s\n' "$image" > "$stage_file"
        chmod 0644 "$stage_file"
        tmp_file=
        ;;
    activate)
        [ -f "$backup_file" ] && [ -f "$stage_file" ] || fail 'no staged compose update exists'
        staged_image=$(cat "$stage_file")
        [ "$image" = "$staged_image" ] || fail 'requested image does not match staged image'
        [ "$(service_image < "$compose_file")" = "$image" ] || fail 'compose file does not match staged image'
        [ "$(compose config | service_image)" = "$image" ] || fail 'effective compose config does not match staged image'
        if ! compose up -d --no-deps --force-recreate "$service_name" || ! wait_for_health; then
            printf '%s\n' 'updated service did not become healthy; restoring previous image' >&2
            if ! rollback; then
                printf '%s\n' 'rollback did not become healthy; manual intervention is required' >&2
                exit 1
            fi
            clear_stage
            exit 1
        fi
        clear_stage
        ;;
    *)
        printf '%s\n' 'unsupported update helper operation' >&2
        exit 2
        ;;
esac
