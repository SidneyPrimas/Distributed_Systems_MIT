#!/usr/bin/env bash

mkdir -p Custom_test
for i in {25..50}
do
  go test -test.v  2>&1 | tee -a ./Custom_Test/TestCheckSummary${i}.log
done