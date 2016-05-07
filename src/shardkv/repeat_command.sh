#!/usr/bin/env bash

#Note: > means that errors (or log print statments) will be printed to the terminal.
#Note: &> means that errors (or log print statments) will be printed to the txt file, and not the terminal.
#mkdir -p Custom_test
#for i in {1..10}
#do
#  go test -test.v  2>&1 | tee -a ./Custom_Test/TestCheckSummary${i}.log
#done

#!/usr/bin/env bash
# Adju
# add whatever tests you like here
TESTS=("TestUnreliable1" "TestUnreliable2")

mkdir -p Custom_Test
for test in "${TESTS[@]}"
do
  	for i in {1..10}
  	do
    go test -run=${test} 2>&1 | tee -a ./Custom_Test/Summary_${test}.log
    	#go test -run=${test} &> ./Test/${test}/${i}.log

    	if grep -q FAIL ./Custom_Test/Summary_${test}.log; then
    		echo " "
    		echo "FAIL IN FILE: ./Custom_Test/Summary_${test}.log"
    		echo " "
    	fi

  	done
done