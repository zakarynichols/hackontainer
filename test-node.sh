#!/bin/bash
set -e

CONTAINER="mynode"
BUNDLE="test-bundles/node"

echo "=== Cleaning up previous bundle ==="
rm -rf ${BUNDLE}

echo "=== Creating fresh bundle directory ==="
mkdir -p ${BUNDLE}/rootfs

echo "=== Cleaning up previous container state ==="
sudo rm -rf /run/hackontainer/${CONTAINER}

echo "=== Pulling Node.js image and extracting rootfs ==="
docker create --name node-container node:latest
docker export node-container | tar -xf - -C ${BUNDLE}/rootfs
docker rm node-container

echo "=== Generating OCI config.json ==="
cd ${BUNDLE}
runc spec
cd -

echo "=== Running container ==="
sudo ./hackontainer run --bundle ${BUNDLE} ${CONTAINER}
