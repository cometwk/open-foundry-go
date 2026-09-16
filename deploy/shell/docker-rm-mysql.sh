#!/usr/bin/env bash

# Remove all mysql containers, 
# 避免测试残留的容器没清理干净

set -euo pipefail

IMAGE="mysql:8.4.11"

containers=$(docker ps -a --filter "ancestor=${IMAGE}" --format '{{.ID}}')

if [[ -z "$containers" ]]; then
    echo "No containers found for image: ${IMAGE}"
    exit 0
fi

echo "Removing containers for image: ${IMAGE}"
echo "$containers"

docker rm -f $containers

echo "Done."