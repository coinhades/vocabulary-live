#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p .tools/downloads
curl -fSL --retry 2 https://download.redis.io/releases/redis-8.10.2.tar.gz -o .tools/downloads/redis.tar.gz
tar -xzf .tools/downloads/redis.tar.gz -C .tools
make -C .tools/redis-8.10.2/src -j4 BUILD_TLS=no MALLOC=libc OPTIMIZATION=-O2 REDIS_CFLAGS= REDIS_LDFLAGS= redis-server redis-cli > .tools/redis-build.log 2>&1
.tools/redis-8.10.2/src/redis-server --version
