#!/usr/bin/env bash

# add whatever tests you like here
TESTS=("TestBasic" "TestConcurrent" "TestUnreliable" "TestUnreliableOneKey" "TestOnePartition" "TestManyPartitionsOneClient" "TestManyPartitionsManyClients" "TestPersistOneClient" "TestPersistConcurrent" "TestPersistConcurrentUnreliable" "TestPersistPartition" "TestPersistPartitionUnreliable" "TestSnapshotRPC" "TestSnapshotRecover" "TestSnapshotRecoverManyClients" "TestSnapshotUnreliable" "TestSnapshotUnreliableRecover")
#TESTS=("TestPersistPartition")

mkdir -p Test
for test in "${TESTS[@]}"
do
	mkdir -p Test/${test}
  	for i in {1..50}
  	do
    	#go test -run=${test}  2>&1 | tee ./Test/${test}/${i}.log
    	go test -run=${test} &> ./Test/${test}/${i}.log

    	if grep -q FAIL ./Test/${test}/${i}.log; then
    		echo " "
    		echo "FAIL IN FILE: ./Test/${test}/${i}.log"
    		echo " "
    	fi

  	done
done