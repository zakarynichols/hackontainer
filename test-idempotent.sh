#!/bin/bash
set -e

CONTAINER="mytest"
BUNDLE="test-bundles/busybox"

echo "=== Cleaning up previous bundle ==="
rm -rf ${BUNDLE}

echo "=== Creating fresh bundle directory ==="
mkdir -p ${BUNDLE}/rootfs

echo "=== Cleaning up previous container state ==="
sudo rm -rf /run/hackontainer/${CONTAINER}

echo "=== Creating fresh Busybox rootfs using docker ==="
docker create --name busybox-container busybox:latest >/dev/null 2>&1
docker export busybox-container | tar -xf - -C ${BUNDLE}/rootfs
docker rm busybox-container >/dev/null 2>&1

echo "=== Generating OCI config.json using runc spec ==="
cd ${BUNDLE}
runc spec
cd -

echo "=== Running container ==="
sudo ./hackontainer run --bundle ${BUNDLE} ${CONTAINER}
