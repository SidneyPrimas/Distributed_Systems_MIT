To Do: 
+ Implement better logging: https://github.com/Sirupsen/logrus
+ When master dies, handle the situation when I assign the shards to Groups for the first time. Especially, be careful about Group 0 cases

Possible Issue: 
+ Does ShardMaster ever crash? If so, do we need to persist data on shardmaster. 
+ Currently, I am committing out of order requestIDs. That means that a client can send RequestID #1 to GroupA and RequestID #2 to GroupB. Thus, GroupA will never see RequestID#2. This should be fine: If the same requestID is sent twice, it will be caugt in the commitTable. If we send a higher requestID, we know that the client has received a response for the previous requestID. So, if we know it's an old request, we throw an error. 
++ Solution: Create a request ID for each group. Deal with each group seperately. 


Notes: 
+ Note: A group is composed of multiple servers. Each group has it's own GID that it belongs to. We don't need to worry about changing the members of a group during operation. The groups are contained in Config.Groups[gid]. 
+ Note: Both kvshard-client and kvshard-clerk create a shardmaster Client. This client is used to talk to the shardmaster for configuration information. 
+ The mapping happens in the following sequence: key => shard (total of 10 shards) => each shard indicates a single group that handles that shard => each group contains multiple servers. 
++ So, shards represent a group of keys (where each key always goes to the same shards). These shards are then mapped to Groups of servers that keep track of those shards/keys. If more Groups of servers are added, the shards are re-distributed across the Groups to balance the system. 

Hints: 
+ Servers probably should query shardmaster to determine what keys to accept/reject or transfer to other servers. 
+ If configuration 16 includes a group, but configuration 17 omits the group, that does not mean that the group is disconnected or dead. Indeed, the group has to still be alive and reachable in order for the system as a whole to proceed to configuration 17. When all the groups have reached configuration 16, then they can all proceed to configuration 17 by moving the key/value pairs as required by the difference between configuration 16 and 17.