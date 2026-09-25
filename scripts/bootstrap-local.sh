#!/usr/bin/env bash
# Optional isolated WSL/Linux tools for this workstation. Not part of app startup.
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p .tools/downloads
cd .tools
curl -fSL --retry 2 https://go.dev/dl/go1.27.1.linux-amd64.tar.gz -o downloads/go.tar.gz
echo '63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445  downloads/go.tar.gz' | sha256sum -c -
tar -xzf downloads/go.tar.gz
curl -fSL --retry 2 https://nodejs.org/dist/v24.14.0/node-v24.14.0-linux-x64.tar.xz -o downloads/node.tar.xz
curl -fsSL https://nodejs.org/dist/v24.14.0/SHASUMS256.txt -o downloads/node-sha.txt
node_hash=$(awk '$2 == "node-v24.14.0-linux-x64.tar.xz" {print $1}' downloads/node-sha.txt)
echo "$node_hash  downloads/node.tar.xz" | sha256sum -c -
tar -xJf downloads/node.tar.xz
./go/bin/go version
./node-v24.14.0-linux-x64/bin/node --version
