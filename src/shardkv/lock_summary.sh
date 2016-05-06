#!/usr/bin/env bash

# Clear the terminal
clear

# Handle Input Variables
path_to_file=$1
gid=$2
server=$3

# Note: R search recersively through all files in folder (and subdirectories)
pass=$(grep -Rn --color "GID${gid}, KVServer${server}:" ${path_to_file})
echo "$pass"
