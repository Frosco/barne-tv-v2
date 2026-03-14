#!/bin/bash
# Deploy barne-tv-v2 to the production server.
set -euo pipefail

SERVER="root@refsnes-barnetv.no"
DEPLOY_DIR="/opt/barne-tv"

echo "==> Cross-compiling for linux/amd64"
GOOS=linux GOARCH=amd64 go build -o barne-tv-v2-linux .

echo "==> Stopping service"
ssh "$SERVER" "systemctl stop barne-tv"

echo "==> Uploading binary and assets"
scp barne-tv-v2-linux "$SERVER:$DEPLOY_DIR/barne-tv-v2"
scp -r templates/ "$SERVER:$DEPLOY_DIR/templates/"
scp -r static/ "$SERVER:$DEPLOY_DIR/static/"

echo "==> Setting ownership and starting service"
ssh "$SERVER" "chown -R barnetv:barnetv $DEPLOY_DIR && systemctl start barne-tv"

echo "==> Cleaning up local build artifact"
rm barne-tv-v2-linux

echo "Deploy complete. Check: https://refsnes-barnetv.no"
