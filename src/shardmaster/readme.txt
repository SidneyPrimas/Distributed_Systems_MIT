To Do: 
+ Implement better logging: https://github.com/Sirupsen/logrus
+ When master dies, handle the situation when I assign the shards to Groups for the first time. Especially, be careful about Group 0 cases

To Do Next Time: 
+ Think through why configuration changes from Group 0 to Group X don't need to be procesed. 
+ Implement shard migration: 
+ Chan we have each shard in a different map. Thus, we can just transfer the maps the correspond to each shard when we need to exchange a shard. 
+ Reject put/append requests if we are not responsible for a client key (since we are not responsible for a shard). Essentially, just check an incoming request against the last committed configuration. Should I reject the requests once I get it back from the applyCh? Or, should I reject it both upon first receipt and once I get it back from the applyCh. I definitely need to reject the request when I receive it on the applyCH if I am not responsible. The question is can I also reject it before applying it to the applyCh. It's probably more efficient if I do. In a few cases, the configuration change will already be in my applyCh to make me responsible, but the RPC has already been rejected. In this case, the Client will get the correct Group, and then try me again. So, in the rare case this happens, it wold still be correct. 
+ In summary: 1) Make a map for each shard, 2) Create a for loop across the old and new shard config. 3) Send the corresponding map from the old shard to the new shard,

Possible Issue: 
+ Does ShardMaster ever crash? If so, do we need to persist data on shardmaster. 
+ Currently, I am committing out of order requestIDs. That means that a client can send RequestID #1 to GroupA and RequestID #2 to GroupB. Thus, GroupA will never see RequestID#2. This should be fine: If the same requestID is sent twice, it will be caugt in the commitTable. If we send a higher requestID, we know that the client has received a response for the previous requestID. So, if we know it's an old request, we throw an error. 
++ Solution: Create a request ID for each group. Deal with each group seperately. 


Notes: 
+ Note: A group is composed of multiple servers. Each group has it's own GID that it belongs to. We don't need to worry about changing the members of a group during operation. The groups are contained in Config.Groups[gid]. 
+ Note: Both kvshard-client and kvshard-clerk create a shardmaster Client. This client is used to talk to the shardmaster for configuration information. 
+ The mapping happens in the following sequence: key => shard (total of 10 shards) => each shard indicates a single group that handles that shard => each group contains multiple servers. 
++ So, shards represent a group of keys (where each key always goes to the same shards). These shards are then mapped to Groups of servers that keep track of those shards/keys. If more Groups of servers are added, the shards are re-distributed across the Groups to balance the system. 

Configuration Update Implementation: 
+ We can only update the local configuration if the configuration has been agreed upon by Raft. This ensures that that the configuration change will be processed in order within a single Group. 

Hints: 
+ Servers probably should query shardmaster to determine what keys to accept/reject or transfer to other servers. 
+ If configuration 16 includes a group, but configuration 17 omits the group, that does not mean that the group is disconnected or dead. Indeed, the group has to still be alive and reachable in order for the system as a whole to proceed to configuration 17. When all the groups have reached configuration 16, then they can all proceed to configuration 17 by moving the key/value pairs as required by the difference between configuration 16 and 17.

Questions: 
1) If you are not the leader, how do you handle configuration changes. 
2) Do I need anything like a ClientID or RequestID for a configuration changes? RequestID is held by Num. And, I assume there is a total of one client (the shard group). If the shard group sends you something, they have agreed on it. So, it acts like a single client. 
3) Big question: Can I create a different map for each shard. 