To Do: 
+ Implement better logging: https://github.com/Sirupsen/logrus
+ When master dies, handle the situation when I assign the shards to Groups for the first time. Especially, be careful about Group 0 cases

Possible Issue: 
+ Does ShardMaster ever crash? If so, do we need to persist data on shardmaster. 


Notes: 
+ Note: A group is composed of multiple servers. Each group has it's own GID that it belongs to. We don't need to worry about changing the members of a group during operation. The groups are contained in Config.Groups[gid]. 
+ Note: Both kvshard-client and kvshard-clerk create a shardmaster Client. This client is used to talk to the shardmaster for configuration information. 
+ The mapping happens in the following sequence: key => shard (total of 10 shards) => each shard indicates a single group that handles that shard => each group contains multiple servers. 
++ So, shards represent a group of keys (where each key always goes to the same shards). These shards are then mapped to Groups of servers that keep track of those shards/keys. If more Groups of servers are added, the shards are re-distributed across the Groups to balance the system. 