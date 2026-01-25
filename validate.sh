#!/bin/bash

# Simple OCI Runtime Validation
# Run validation tests against runc and your runtime

set -e

echo "=== OCI Runtime Validation ==="

# Clone if needed
if [ ! -d "runtime-tools" ]; then
    echo "Cloning runtime-tools..."
    git clone https://github.com/opencontainers/runtime-tools.git
fi

if [ ! -d "runc" ]; then
    echo "Cloning runc..."
    git clone https://github.com/opencontainers/runc.git
fi

# Build if needed
if [ ! -f "runc/runc" ]; then
    echo "Building runc..."
    cd runc && make && cd ..
fi

if [ ! -f "runtime-tools/validate" ]; then
    echo "Building validation tools..."
    cd runtime-tools && make validation-executables && cd ..
fi

if [ ! -f "hackontainer" ]; then
    echo "Building hackontainer..."
    go build -o hackontainer ./cmd/hackontainer
fi

echo "Running validation tests..."

# Test each runtime individually
cd runtime-tools

# Test runc
echo "Testing runc..."
runc_results=$(mktemp)
make runtimetest > /dev/null 2>&1
TEST_LIST="$(make print-validation-tests 2>/dev/null)"
for test in $TEST_LIST; do
    RUNTIME=../runc/runc timeout 30 $test 2>/dev/null | grep -E "(ok [0-9]|not ok [0-9])" || true
done > $runc_results
runc_total=$(wc -l < $runc_results)
runc_passed=$(grep -c '^ok' $runc_results || echo "0")

# Test hackontainer
echo "Testing hackontainer..."
hackontainer_results=$(mktemp)
for test in $TEST_LIST; do
    RUNTIME=../hackontainer timeout 30 $test 2>/dev/null | grep -E "(ok [0-9]|not ok [0-9])" || true
done > $hackontainer_results
hackontainer_total=$(wc -l < $hackontainer_results)
hackontainer_passed=$(grep -c '^ok' $hackontainer_results || echo "0")

# Cleanup
rm -f $runc_results $hackontainer_results

cd ..

# Calculate results
runc_rate=$(echo "scale=1; $runc_passed * 100 / $runc_total" | bc -l)
hackontainer_rate=$(echo "scale=1; $hackontainer_passed * 100 / $hackontainer_total" | bc -l)
relative=$(echo "scale=1; $hackontainer_rate * 100 / $runc_rate" | bc -l)

echo ""
echo "=== RESULTS ==="
echo "runc:        $runc_passed/$runc_total ($runc_rate%)"
echo "hackontainer: $hackontainer_passed/$hackontainer_total ($hackontainer_rate%)"
echo "Performance:  $relative% of runc"