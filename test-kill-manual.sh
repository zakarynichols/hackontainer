#!/bin/bash
set -e

CONTAINER="mymanual"
BUNDLE="test-bundles/busybox"

echo "=== Cleaning up any existing container ==="
sudo ./hackontainer delete ${CONTAINER} 2>/dev/null || true

echo "=== Creating container ==="
sudo ./hackontainer create --bundle ${BUNDLE} ${CONTAINER}

echo "=== Starting container ==="
sudo ./hackontainer start ${CONTAINER}

echo "=== Initial state (should be running) ==="
sudo ./hackontainer state ${CONTAINER}

echo "=== Killing container with SIGKILL ==="
sudo ./hackontainer kill ${CONTAINER} SIGKILL

echo "=== State after kill (should be stopped) ==="
sudo ./hackontainer state ${CONTAINER}

echo "=== Deleting container ==="
sudo ./hackontainer delete ${CONTAINER}

echo "=== Test complete ==="
