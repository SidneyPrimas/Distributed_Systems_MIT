#!/usr/bin/env bash

# add whatever tests you like here
TESTS=("TestBasic" "TestMulti")
#TESTS=("TestMulti")

mkdir -p Test
for test in "${TESTS[@]}"
do
	mkdir -p Test/${test}
  	for i in {1..20}
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