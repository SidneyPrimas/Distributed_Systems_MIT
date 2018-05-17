# Sharded Key-Value Service: 
I expanded my key-value service to shard the keys across multiple replica groups, and built a service to manage the replica groups configuration while the servers are live. Specifically, the sharded key-value service allows for:
+ Load balancing requests across replica groups by moving shards between them. 
+ Adding and removing replica groups while servers are live (and automatically rebalancing the shards acrss replica groups)

The goal of sharding is to increase system through-put. Since each replica group handles only a subset of the keys, the entire system can handle more gets/puts simultaneously. 

This directory contains scripts for the update key-value storage servers that supports sharding. We needed to update the key-value storage servers to 1) redirect requests to the correct replica group, 2) monitor the ShardMaster for configuration changes, 3) migrate shards using RPCs from one replica group to another during load balancing, etc. Find code at [src/shardkv](https://github.

Find lab directions [here](http://nil.csail.mit.edu/6.824/2016/labs/lab-shard.html).