#!/usr/bin/env bash

mkdir -p Custom_test
for i in {1..1}
do
  go test -test.v >> ./Custom_test/TestCheckSummary.txt
done