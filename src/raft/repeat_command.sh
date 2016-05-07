#!/usr/bin/env bash

#Note: > means that errors (or log print statments) will be printed to the terminal.
#Note: &> means that errors (or log print statments) will be printed to the txt file, and not the terminal.
mkdir -p Custom_test
for i in {1..10}
do
  go test -test.v  2>&1 | tee -a ./Custom_Test/TestCheckSummary${i}.log
done