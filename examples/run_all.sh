#!/bin/bash
# Run all ish example scripts and report results
# Scripts with "STATUS: FAILS" in their header are expected to fail.
ISH="./ish"
PASS=0
FAIL=0
XFAIL=0
TIMEOUT=10

for script in examples/*.ish; do
    name=$(basename "$script")
    expect_fail=$(head -5 "$script" | grep -c "STATUS: FAILS")
    output=$(timeout $TIMEOUT "$ISH" "$script" 2>&1)
    exit_code=$?

    if [ $exit_code -eq 0 ]; then
        if [ "$expect_fail" -gt 0 ]; then
            echo "XPASS $name (was expected to fail but passed!)"
            PASS=$((PASS + 1))
        else
            echo "PASS  $name"
            PASS=$((PASS + 1))
        fi
    elif [ $exit_code -eq 124 ]; then
        if [ "$expect_fail" -gt 0 ]; then
            echo "XFAIL $name (expected - hangs)"
            XFAIL=$((XFAIL + 1))
        else
            echo "HANG  $name (killed after ${TIMEOUT}s)"
            FAIL=$((FAIL + 1))
        fi
    else
        if [ "$expect_fail" -gt 0 ]; then
            echo "XFAIL $name (expected)"
            XFAIL=$((XFAIL + 1))
        else
            echo "FAIL  $name (exit $exit_code)"
            echo "$output" | head -3 | sed 's/^/      /'
            FAIL=$((FAIL + 1))
        fi
    fi
done

echo ""
echo "Results: $PASS passed, $FAIL failed, $XFAIL expected failures"
