#!/usr/bin/env bash

# Clear the terminal
clear

# Handle Input Variables
path_to_folder=$1

# Note: R search recersively through all files in folder (and subdirectories)

# Find files that don't have PASS in them
echo "************* FAIL *************"
fail=$(grep -Rn --color -L 'PASS' ./${path_to_folder}/)
echo "$fail"

echo "************* PASS *************"
pass=$(grep -Rn --color 'PASS' ./${path_to_folder}/)
echo "$pass"

echo "************* Summary *************"
total_fail=$(echo "$fail" | grep -c --color 'FAIL')
echo "Total Fail: $total_fail"
total_pass=$(echo "$pass" | grep -c --color 'PASS')
echo "Total Pass: $total_pass"