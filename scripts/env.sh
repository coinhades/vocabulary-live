#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH="$PWD/.tools/go/bin:$PWD/.tools/node-v24.14.0-linux-x64/bin:$PWD/.tools/redis-8.10.2/src:$PATH"
export GOCACHE="$PWD/.cache/go-build"
export GOPATH="$PWD/.cache/go"
export npm_config_cache="$PWD/.cache/npm"
export PLAYWRIGHT_BROWSERS_PATH="$PWD/.cache/browsers"
exec "$@"
