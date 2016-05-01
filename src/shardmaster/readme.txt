High Level To Do: 
+ + Implement better logging: https://github.com/Sirupsen/logrus

Holistic To Do: 
+ When skipping entries since we are in a configuration transition, make sure to 1) respond to RPC indicating client to try again and 2) clean out the RPC table at that index. Ideally, the client knows that they have the correct leader and responds back to same server 
+Figure out how to initialize configuration on startup. Getting the latest configuration is *not* the right approach because the configuration needs to match the current state of the system. 
+ Figure out when to delete keys from maps. A thought: I can only delete it when it has bee successfully accepted by all groups that need it
+ To improve readability, break the following parts into seperate functions: handling configuration change OP. 
+ Figure out what to do when: Server's configuration is significantly behind. And, another server is attempting to send shards over. Should I put these shards into raft, or should I just reject the RPC. Then, should the server find another server within the group, and continue trying until the other server catches up. 
+ Important: Handle the possible deadlock when servers need to 1) receive a shard from a server and 2) send a shard to another server. 
+ Important: Figure out if it's necessary to snapshot during configuration changes. Currently, I don't snapshot when rejecting logs during a transition. 
+ Error Checking: Check if I ever add a key to a a shard the group doesn't own. 


High Priority To Do: 
+ When I am in transition mode, reject all incoming RPCs from entering the log. 
+ Before executing a command (put, get, append), make sure that it belongs in this group in the current configuration. 
+ Before entering a command into the log, make sure that it belongs in this group within the current configuration. 
++ Reject put/append requests if we are not responsible for a client key (since we are not responsible for a shard). Essentially, just check an incoming request against the last committed configuration. Should I reject the requests once I get it back from the applyCh? Or, should I reject it both upon first receipt and once I get it back from the applyCh. I definitely need to reject the request when I receive it on the applyCH if I am not responsible. The question is can I also reject it before applying it to the applyCh. It's probably more efficient if I do. In a few cases, the configuration change will already be in my applyCh to make me responsible, but the RPC has already been rejected. In this case, the Client will get the correct Group, and then try me again. So, in the rare case this happens, it wold still be correct. 
+ Possible Implementation: Convert to a scheme where each shard has it's own key/value manp and it's own commitTable. Thus, I can transfer around full shards. 
+ Possible Implementation: Identify entries of the commit table that are relevant to a specific shard. Only send these entries when transferring shards. 

Possible Issue: 
+ Does ShardMaster ever crash? If so, do we need to persist data on shardmaster. 
+ Currently, I am committing out of order requestIDs. That means that a client can send RequestID #1 to GroupA and RequestID #2 to GroupB. Thus, GroupA will never see RequestID#2. This should be fine: If the same requestID is sent twice, it will be caugt in the commitTable. If we send a higher requestID, we know that the client has received a response for the previous requestID. So, if we know it's an old request, we throw an error. 
++ Possible Solution: Create a request ID for each group. Deal with each group seperately. 
+ Currently, I don't process any configuration changes if I am moving a shard away from or toward Group #0. 

Questions: 
+ Should a group of servers have the same clientID for the purpose of other systems identifying them? I am assuming yes since a group of replicated servers should really function as a single server to the outside world. 
+ If you are not the leader, how do you handle configuration changes. Do you still need to send RPCs to transfer key/value maps? 

High Level Architecture Note: 
+ Note: A group is composed of multiple servers. Each group has it's own GID that it belongs to. We don't need to worry about changing the members of a group during operation. The groups are contained in Config.Groups[gid]. 
+ Note: Both kvshard-client and kvshard-clerk create a shardmaster Client. This client is used to talk to the shardmaster for configuration information. 
+ The mapping happens in the following sequence: key => shard (total of 10 shards) => each shard indicates a single group that handles that shard => each group contains multiple servers. 
++ So, shards represent a group of keys (where each key always goes to the same shards). These shards are then mapped to Groups of servers that keep track of those shards/keys. If more Groups of servers are added, the shards are re-distributed across the Groups to balance the system. 

Checking key in correct group: 
+ 

Configuration Update Notes: 
+ We can only update the local configuration if the configuration has been agreed upon by Raft. This ensures that that the configuration change will be processed in order within a single Group. 
+ We only can switch to a new configuration if: 1) we have received all the shards we need and 2) we successfully sent all the shards to the correct other group. 
+ Duplicate Detection (new reasoning): The reason we need duplicate detection is if: 1) a op was received on the applyCh, 2) we processed the request but the server didn't hear our response, and 3) we receive the same request again from the same server. In thise case, we should just check our committedEntry table and response to the server immediately (instead of having to put it through raft again).

Configuration Update Implementation:
+ When receive configuration change on applyCh, change to configuration change state. 
+ In configuration change state, do not process any new logs from applyCh except if they help complete the configuration change. 
+ Since we are only interested in completing the configuration change at this point, don't accept any requests from clients. And, reject any logs on the applyCh other than configuration changes. 
+ Outdated: Upon receiving a configuration change, start 2 go routines. One go routine to make sure that we receive all shards. And, another go routine to make sure that we can send all shards. 
+ To send shards, send the shard to the appropriate group. Make sure that the server in the group is 1) in the appropriate transition state, and 2) leader. If they are both, the server will eventually receive the keys on the applyCh. When it does, it should update it's state. 
+ To receive shards, the system needs to keep track of all the shards it should receive. When it receives a shard, on the applyCh, it should add this to the map, and then it should remove the shard from the list of shards it should receive. 
+ Once a group has both successfully sent, and successfully received all the shards it needs, then the group can move out of transition state, and continue processing new requests. 


Hints: 
+ Servers probably should query shardmaster to determine what keys to accept/reject or transfer to other servers. 
+ If configuration 16 includes a group, but configuration 17 omits the group, that does not mean that the group is disconnected or dead. Indeed, the group has to still be alive and reachable in order for the system as a whole to proceed to configuration 17. When all the groups have reached configuration 16, then they can all proceed to configuration 17 by moving the key/value pairs as required by the difference between configuration 16 and 17.

