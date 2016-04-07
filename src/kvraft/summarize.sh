#!/usr/bin/env bash

# Clear the terminal
clear

# Handle Input Variables
path_to_folder=$1

# Note: R search recersively through all files in folder (and subdirectories)

echo "************* PASS *************"
grep -Rn --color 'PASS' ./${path_to_folder}/

echo "************* FAIL *************"
grep -Rn --color FAIL ./${path_to_folder}/

echo "************* Summary *************"
total_fail=$(grep -R 'FAIL' ./$path_to_folder/ | grep -c --color 'FAIL')
echo "Total Fail: $total_fail"
total_pass=$(grep -R 'PASS' ./$path_to_folder/ | grep -c --color 'PASS')
echo "Total Pass: $total_pass"
total_ok=$(grep -R 'ok  ' ./$path_to_folder/ | grep -c --color 'ok  ')
echo "Total Ok: $total_ok"