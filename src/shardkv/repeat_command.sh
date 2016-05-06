#!/usr/bin/env bash

#Note: > means that errors (or log print statments) will be printed to the terminal.
#Note: &> means that errors (or log print statments) will be printed to the txt file, and not the terminal.

for i in {1..1}
do
  go test -race -run=TestUnreliable1 2>&1 | tee ./Custom_Test/TestUnreliable1_${i}.txt
done