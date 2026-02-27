#!/bin/bash
set -e

CONTAINER="mykill"
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
rm -f ${BUNDLE}/config.json
cd ${BUNDLE}
runc spec
cd -

echo "=== Modifying config to use sleep 30 ==="
# Use jq to modify the args if available, otherwise use python
if command -v jq &> /dev/null; then
    jq '.process.args = ["sleep", "30"] | .process.terminal = false' ${BUNDLE}/config.json > ${BUNDLE}/config.json.tmp && mv ${BUNDLE}/config.json.tmp ${BUNDLE}/config.json
else
    # Fallback: use python to modify JSON
    python3 -c "
import json
with open('${BUNDLE}/config.json', 'r') as f:
    config = json.load(f)
config['process']['args'] = ['sleep', '30']
config['process']['terminal'] = False
with open('${BUNDLE}/config.json', 'w') as f:
    json.dump(config, f)
"
fi

echo "=== Creating container ==="
sudo ./hackontainer create --bundle ${BUNDLE} ${CONTAINER}

echo "=== Starting container ==="
sudo ./hackontainer start ${CONTAINER}

echo "=== Getting initial container state ==="
sudo ./hackontainer state ${CONTAINER}

echo "=== Waiting for container to be running ==="
sleep 1
sudo ./hackontainer state ${CONTAINER}

echo "=== Killing container ==="
sudo ./hackontainer kill ${CONTAINER}

echo "=== Checking container state immediately after kill ==="
sudo ./hackontainer state ${CONTAINER}

echo "=== Waiting for process to exit and reaper to update state ==="
sleep 2

echo "=== Checking container state after process exited ==="
sudo ./hackontainer state ${CONTAINER}

echo "=== Deleting container ==="
sudo ./hackontainer delete ${CONTAINER}
