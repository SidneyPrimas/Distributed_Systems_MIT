# Sharded Key-Value Service: ShardMaster Code
I expanded my key-value service to shard the keys across multiple replica groups, and built a service to manage the replica groups configuration while the servers are live. Specifically, the sharded key-value service allows for:
+ Load balancing requests across replica groups by moving shards between them. 
+ Adding and removing replica groups while servers are live (and automatically rebalancing the shards acrss replica groups)

The goal of sharding is to increase system through-put. Since each replica group handles only a subset of the keys, the entire system can handle more gets/puts simultaneously. 

This directory contains scripts for the Shard Configuration Service (ShardMaster). The ShardMaster is a separate replica group tasked with maintaining fault-tolerant records on the configuration of the system. Specifically, the ShardMaster keeps track of the mapping from shards to replica groups. Find code at [src/shardmaster](https://github.com/SidneyPrimas/Distributed_Systems_MIT/tree/master/src/shardmaster).

Find lab directions [here](http://nil.csail.mit.edu/6.824/2016/labs/lab-shard.html).