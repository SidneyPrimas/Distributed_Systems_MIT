#!/usr/bin/env bash

# add whatever tests you like here
# TESTS=("TestStaticShards" "TestJoinLeave" "TestSnapshot" "TestMissChange" "TestConcurrent1" "TestConcurrent2" "TestUnreliable1")
#TESTS=("TestConcurrent1" "TestConcurrent2" "TestUnreliable1" "TestUnreliable2")
TESTS=("TestUnreliable1" "TestUnreliable2")

mkdir -p Test
for test in "${TESTS[@]}"
do
	mkdir -p Test/${test}
  	for i in {21..30}
  	do
    go test -run=${test}  2>&1 | tee ./Test/${test}/${i}.log
    	#go test -run=${test} &> ./Test/${test}/${i}.log

    	if grep -q FAIL ./Test/${test}/${i}.log; then
    		echo " "
    		echo "FAIL IN FILE: ./Test/${test}/${i}.log"
    		echo " "
    	fi

  	done
done