#!/usr/bin/env bash

for run in {1..3}
do
  go test -test.v >> ./Test/all_tests.txt
done