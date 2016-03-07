#!/usr/bin/env bash

set -x

# add whatever tests you like here
 TESTS=("TestBasic" "TestConcurrent" "TestUnreliable" "TestUnreliableOneKey" "TestOnePartition" "TestManyPartitionsOneClient" "TestManyPartitionsManyClients" "TestPersistOneClient" "TestPersistConcurrent" "TestPersistConcurrentUnreliable" "TestPersistPartition" "TestPersistPartitionUnreliable")


mkdir -p Test
for test in "${TESTS[@]}"
do
  for i in {1..20}
  do
    go test -run=${test} >> ./Test/${test}.txt
  done
done