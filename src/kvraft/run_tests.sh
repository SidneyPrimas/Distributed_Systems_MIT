#!/usr/bin/env bash

set -x

# add whatever tests you like here
TESTS=("TestPersistPartition" "TestSnapshotRPC" "TestSnapshotRecover" "TestSnapshotRecoverManyClients" "TestSnapshotUnreliable" "TestSnapshotUnreliableRecover")
#TESTS=("TestSnapshotUnreliable")

mkdir -p Test
for test in "${TESTS[@]}"
do
	mkdir -p Test/${test}
  	for i in {6..15}
  	do
    	go test -run=${test}  2>&1 | tee -a ./Test/${test}/summary.log
  	done
done