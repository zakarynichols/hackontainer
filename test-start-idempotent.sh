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

echo "=== Creating container ==="
sudo ./hackontainer create --bundle ${BUNDLE} ${CONTAINER}

echo "=== Starting container ==="
sudo ./hackontainer start ${CONTAINER}

echo "=== Getting initial container state ==="
sudo ./hackontainer state ${CONTAINER}

echo "=== Waiting for process to exit (sleep 3) ==="
sleep 3

echo "=== Checking if shell process is still running ==="
PID=$(sudo ./hackontainer state ${CONTAINER} | grep -o '"pid":[0-9]*' | head -1 | tr -d ' "pid:')
echo "Checking PID: ${PID}"
ps -p ${PID} 2>/dev/null && echo "Process ${PID} is RUNNING" || echo "Process ${PID} has EXITED"

echo "=== Checking container state after 3 seconds ==="
sudo ./hackontainer state ${CONTAINER}

echo "=== Deleting container ==="
sudo ./hackontainer delete ${CONTAINER}
