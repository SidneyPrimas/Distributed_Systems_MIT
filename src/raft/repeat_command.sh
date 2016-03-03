#!/usr/bin/env bash

mkdir -p TestReliableChurn
for i in {101..130}
do
  go test -run TestReliableChurn >> ./TestReliableChurn/TestReliableChurn_final${i}.txt
done