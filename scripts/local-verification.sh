#!/usr/bin/env bash
# Workstation-only harness. All child processes and data belong to this workspace.
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH="$PWD/.tools/go/bin:$PWD/.tools/node-v24.14.0-linux-x64/bin:$PWD/.tools/redis-8.10.2/src:$PATH"
export GOCACHE="$PWD/.cache/go-build" GOPATH="$PWD/.cache/go"
export REDIS_URL=redis://127.0.0.1:16379/0
export GOMAXPROCS=2 GOFLAGS=-p=2
mkdir -p .cache/redis dist docs/evidence
redis-server --bind 127.0.0.1 --port 16379 --appendonly yes --appendfsync everysec --maxmemory-policy noeviction --dir "$PWD/.cache/redis" > .cache/redis/redis-harness.log 2>&1 &
redis_pid=$!
trap 'kill "$redis_pid" 2>/dev/null || true' EXIT
for attempt in $(seq 1 100); do
  if (echo PING > /dev/tcp/127.0.0.1/16379) >/dev/null 2>&1; then break; fi
  if ! kill -0 "$redis_pid" 2>/dev/null; then cat .cache/redis/redis-harness.log; exit 1; fi
  sleep 0.05
done
echo 'Isolated Redis port is accepting connections.'
cd backend
gofmt -w .
go vet -tags integration ./...
go test -race -count=1 -timeout=60s -tags integration ./... > ../docs/evidence/go-integration.txt 2>&1
cat ../docs/evidence/go-integration.txt
echo 'Building Windows server (bounded compiler parallelism).'
GOOS=windows CGO_ENABLED=0 go build -trimpath -o ../dist/server.exe ./cmd/server
echo 'Building Linux server.'
go build -trimpath -o ../dist/server ./cmd/server
echo 'Go checks and Windows/Linux builds complete; Redis remains attached for browser checks.'
wait "$redis_pid"
