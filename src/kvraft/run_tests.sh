#!/usr/bin/env bash

set -x

# add whatever tests you like here
 #TESTS=("TestBasic" "TestConcurrent" "TestUnreliable" "TestUnreliableOneKey" "TestOnePartition" "TestManyPartitionsOneClient" "TestManyPartitionsManyClients" "TestPersistOneClient" "TestPersistConcurrent" "TestPersistConcurrentUnreliable" "TestPersistPartition" "TestPersistPartitionUnreliable" "TestSnapshotRPC" "TestSnapshotRecover" "TestSnapshotRecoverManyClients" "TestSnapshotUnreliable" "TestSnapshotUnreliableRecover")
TESTS=("TestSnapshotUnreliable")

mkdir -p Test
for test in "${TESTS[@]}"
do
	mkdir -p Test/${test}
  	for i in {1..1}
  	do
    	go test -run=${test}  2>&1 | tee ./Test/${test}/${i}.log
  	done
done