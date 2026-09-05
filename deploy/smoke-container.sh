#!/usr/bin/env bash
# Exercise the shipped image with both its default user and Unraid's user.
# Requires Docker, curl, and jq. Creates only disposable test containers/volumes.
set -euo pipefail

image=${1:-slopchan:test}
prefix="slopchan-smoke-$(date +%s)-$$"
token="container-smoke-$(openssl rand -hex 16)"
container=""
volume=""

cleanup() {
  if [[ -n "$container" ]]; then docker rm -f "$container" >/dev/null 2>&1 || true; fi
  if [[ -n "$volume" ]]; then docker volume rm "$volume" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT

for run_user in 10001:10001 99:100; do
  container="$prefix-${run_user%:*}"
  volume="$container-data"
  docker volume create "$volume" >/dev/null
  mount="type=volume,src=$volume,dst=/data"
  if [[ "$run_user" == 99:100 ]]; then
    # Emulate an empty Unraid appdata directory prepared with install -d.
    # No image directory is copied into this volume, just as with a bind mount.
    docker run --rm --mount "$mount" alpine:3.23 \
      install -d -m 0750 -o 99 -g 100 /data
    mount="$mount,volume-nocopy"
  fi

  start() {
    docker run -d --name "$container" --user "$run_user" \
      --read-only --cap-drop=ALL --security-opt=no-new-privileges:true \
      --stop-timeout=40 --mount "$mount" \
      -e "SLOPCHAN_TOKENS=$token" -p 127.0.0.1::8080 "$image" >/dev/null
    address="http://$(docker port "$container" 8080/tcp)"
    for ((attempt = 0; attempt < 60; attempt++)); do
      if curl -fsS "$address/api/threads" >/dev/null 2>&1; then return; fi
      sleep 1
    done
    docker logs "$container" >&2
    echo "Container did not become ready" >&2
    return 1
  }

  start
  status=$(curl -sS -o /dev/null -w '%{http_code}' \
    -H 'Content-Type: application/json' -d '{"text":"unauthorized"}' "$address/api/threads")
  [[ "$status" == 401 ]]

  # A one-pixel GIF exercises image validation and durable file writes.
  post=$(printf 'R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==' | openssl base64 -d -A | \
    curl -fsS -H "Authorization: Bearer $token" \
      -F 'text=container persistence check' -F 'image=@-;filename=smoke.gif;type=image/gif' \
      "$address/api/threads")
  post_id=$(jq -er '.post.id' <<< "$post")
  image_path=$(jq -er '.post.image.url' <<< "$post")
  before=$(curl -fsS "$address$image_path" | openssl dgst -sha256)

  # Recreate rather than just restart: all surviving data must be in /data.
  docker stop "$container" >/dev/null
  [[ $(docker inspect -f '{{.State.ExitCode}}' "$container") == 0 ]]
  docker rm "$container" >/dev/null
  start
  curl -fsS "$address/api/posts/$post_id" | jq -e '.post.text == "container persistence check"' >/dev/null
  [[ $(curl -fsS "$address$image_path" | openssl dgst -sha256) == "$before" ]]
  curl -fsS "$address/api/search?q=persistence" | jq -e '.posts | length == 1' >/dev/null
  docker exec "$container" /slopchan remove "$post_id" >/dev/null
  [[ $(curl -sS -o /dev/null -w '%{http_code}' "$address$image_path") == 404 ]]

  docker stop "$container" >/dev/null
  [[ $(docker inspect -f '{{.State.ExitCode}}' "$container") == 0 ]]
  docker rm "$container" >/dev/null
  container=""
  docker volume rm "$volume" >/dev/null
  volume=""
  echo "Container smoke test passed as $run_user"
done
