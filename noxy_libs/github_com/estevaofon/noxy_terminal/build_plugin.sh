#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
go build -o noxy-plugin-terminal .
echo "Created noxy-plugin-terminal"
