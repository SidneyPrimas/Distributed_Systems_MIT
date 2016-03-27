#!/usr/bin/env bash

set -x

# add whatever tests you like here
 # TESTS=("TestBasic" "TestConcurrent" "TestUnreliable" "TestUnreliableOneKey" "TestOnePartition" "TestManyPartitionsOneClient" "TestManyPartitionsManyClients" "TestPersistOneClient" "TestPersistConcurrent" "TestPersistConcurrentUnreliable" "TestPersistPartition" "TestPersistPartitionUnreliable")
TESTS=("TestPersistPartition")

mkdir -p Test
for test in "${TESTS[@]}"
do
	mkdir -p Test/${test}
  	for i in {1..30}
  	do
    	go test -run=${test} &> ./Test/${test}/${i}.log
  	done
done