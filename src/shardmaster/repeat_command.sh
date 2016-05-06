#!/usr/bin/env bash

#Note: > means that errors (or log print statments) will be printed to the terminal.
#Note: &> means that errors (or log print statments) will be printed to the txt file, and not the terminal.
mkdir ./Custom_Test
for i in {1..10}
do
  go test -run=TestMulti &> ./Custom_Test/TestMulti${i}.txt
done