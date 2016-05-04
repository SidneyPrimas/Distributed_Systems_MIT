#!/usr/bin/env bash

#Note: > means that errors (or log print statments) will be printed to the terminal.
#Note: &> means that errors (or log print statments) will be printed to the txt file, and not the terminal.
for i in {1..3}
do
  go test -run=TestUnreliable2 >> ./Custom_Test/TestCheckSummary.txt
done