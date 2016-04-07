#!/usr/bin/env bash

set -x

# add whatever tests you like here
TESTS=("TestSnapshotRPC" "TestSnapshotRecover" "TestSnapshotRecoverManyClients" "TestSnapshotUnreliable" "TestSnapshotUnreliableRecover")
#TESTS=("TestPersistPartition")

mkdir -p Test
for test in "${TESTS[@]}"
do
	mkdir -p Test/${test}
  	for i in {52..52}
  	do
    	go test -run=${test}  2>&1 | tee ./Test/${test}/${i}.log
  	done
done