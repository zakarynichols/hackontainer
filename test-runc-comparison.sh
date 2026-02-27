#!/bin/bash
set -e

CONTAINER="myrunc"
BUNDLE="test-bundles/busybox"

echo "=== Cleaning up any existing container ==="
sudo ./runc/runc delete ${CONTAINER} 2>/dev/null || true

echo "=== Creating container with runc ==="
sudo ./runc/runc create --bundle ${BUNDLE} ${CONTAINER}

echo "=== Starting container with runc ==="
sudo ./runc/runc start ${CONTAINER}

echo "=== Initial state (should be running) ==="
sudo ./runc/runc state ${CONTAINER}

echo "=== Killing container with SIGKILL ==="
sudo ./runc/runc kill ${CONTAINER} SIGKILL

echo "=== State after kill (should be stopped) ==="
sudo ./runc/runc state ${CONTAINER}

echo "=== Deleting container ==="
sudo ./runc/runc delete ${CONTAINER}

echo "=== Test complete ==="
