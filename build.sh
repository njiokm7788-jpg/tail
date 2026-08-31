#!/bin/bash
set -e

# Build tailscale-gateway plugin for Linux (Docker target)
# Output: dist/tailscale-gateway.so

echo "Building tailscale-gateway.so for linux/amd64..."

CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
  go build -buildmode=c-shared \
  -ldflags="-s -w" \
  -o dist/tailscale-gateway.so \
  .

echo "Built: dist/tailscale-gateway.so"
ls -lh dist/tailscale-gateway.so
