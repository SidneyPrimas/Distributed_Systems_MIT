#!/usr/bin/env bash

set -x

# add whatever tests you like here
 TESTS=("TestInitialElection" "TestReElection" "TestBasicAgree" "TestFailAgree" "TestFailNoAgree" "TestConcurrentStarts" "TestRejoin" "TestBackup" "TestCount" "TestPersist1" "TestPersist2" "TestPersist3" "TestFigure8" "TestUnreliableAgree" "TestFigure8Unreliable" "TestReliableChurn" "TestUnreliableChurn")
# TESTS=("TestFigure8" "TestUnreliableAgree" "TestFigure8Unreliable" "TestReliableChurn" "TestUnreliableChurn")

# TESTS=("TestFigure8Unreliable")

mkdir -p Test
for test in "${TESTS[@]}"
do
  for i in {1..60}
  do
    go test -run=${test} >> ./Test/${test}.txt
  done
done