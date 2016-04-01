#!/usr/bin/env bash

mkdir -p Custom_test
for i in {6..10}
do
  go test -test.v  2>&1 | tee -a ./Custom_Test/TestCheckSummary${i}.log
done