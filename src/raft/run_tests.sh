#!/usr/bin/env bash

# set -x

# add whatever tests you like here
 TESTS=("TestInitialElection" "TestReElection" "TestBasicAgree" "TestFailAgree" "TestFailNoAgree" "TestConcurrentStarts" "TestRejoin" "TestBackup" "TestCount" "TestPersist1" "TestPersist2" "TestPersist3" "TestFigure8" "TestUnreliableAgree" "TestFigure8Unreliable" "TestReliableChurn" "TestUnreliableChurn")
#TESTS=("TestReliableChurn" "TestUnreliableChurn")

mkdir -p Test
for test in "${TESTS[@]}"
do
  mkdir -p Test/${test}
  for i in {1..50}
  do
    go test -run=${test} &> ./Test/${test}/${i}.log

    if grep -q FAIL ./Test/${test}/${i}.log; then
    	echo " "
    	echo "FAIL IN FILE: ./Test/${test}/${i}.log"
    	echo " "
    fi

  done
done