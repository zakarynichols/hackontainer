#!/bin/bash
set -e

CONTAINER="mytest"
BUNDLE="test-bundles/busybox"

echo "=== Cleaning up container state ==="
sudo rm -rf /run/hackontainer/${CONTAINER}

echo "=== Cleaning up bundle ==="
rm -rf ${BUNDLE}

echo "=== Cleanup complete ==="
