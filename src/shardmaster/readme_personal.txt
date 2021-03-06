High Level To Do: 
+ + Implement better logging: https://github.com/Sirupsen/logrus 

Holistic To Do: 
+ When skipping entries since we are in a configuration transition, make sure to 1) respond to RPC indicating client to try again and 2) clean out the RPC table at that index. Ideally, the client knows that they have the correct leader and responds back to same server 
+ Figure out when to delete keys from maps. A thought: I can only delete it when it has bee successfully accepted by all groups that need it
+ To improve readability, break the following parts into seperate functions: handling configuration change OP. 
+ Figure out what to do when: Server's configuration is significantly behind. And, another server is attempting to send shards over. Should I put these shards into raft, or should I just reject the RPC. Then, should the server find another server within the group, and continue trying until the other server catches up. 
+ Error Checking: Check if I ever add a key to a a shard the group doesn't own. Check when a new map comes in from raft log. 
+ Investigate go routines: Should I have goRoutines when I query the shardmaster? 
+ Do not enter Query configuration changes into raft. Essentially, these have no effect on any of the servers. They are ignored in every way accept to increase the commit Num. So, when receiving this as a new configuration, just reject it. In this case, don't even change the configuration Num (since in order to change a committed configuration we need to go through Raft). Instead, allow raft to accept configuration changes with a Num greater than the current configuration Num. 
+ Snapshotting during transition: Currently, we don't take any snapshots during the transition. The reason for this is that the snapshot assumes that the operation in question has been completed, and we no longer need to log for that operation. However, in the transition, that's not the case. Thus, if we decide we need to snapshot the transition, we also need to snapshot 1) the committedConfig and 2) the transitionState. The transitionState is especially important because it captures the changes to the state of the system during the transition. 
+ Think about: 1) Should I overwrite keys (without thinking) when I transfer shards and 2) should I delete keys after transferring shards. Also, how do I best error check this? 


High Priority To Do: 
+ Important: Handle the possible deadlock when servers need to 1) receive a shard from a server and 2) send a shard to another server. 
+ Possible Implementation: Convert to a scheme where each shard has it's own key/value map and it's own commitTable. Thus, I can transfer around full shards. 
+ Need to think about: I need to figure out if I should delete key/values from mape and rows from commitTable for transferred shards. 
+ Shard exchange: With rf.GetState(), I can make sure that only the leader sends shards to other groups. Positive:  The benefit is a situation where the follower somehow finds the other group's leader first. Then, the follower completes the transaction, and moves ahead of the leader.  Negative: The drawback is if the leader cannot contact the other shard, and the follower doesn't need to contact the other shard. Then, the follower can be ahead of the execution state of the leader, and process more logs. This might cause issues. A possible solution is to put the fact that the other group responded into Raft. Then, wait for the confirmation on Raft (put in by leader), before moving on. 
+ Currently, I am not snapshotting during configuration changes. Make sure this doesn't cause any issues.
++ Implementing snapshotting during configuration change: The configuration change is an operation that indicates that we go into a transition state. Store this transition state in a variable, and make sure to snapshot this variable. When the server comes back on, check if we are in transition state. If we are in transition state, call a worker to complete the transition state by send out RPCS. 

Completed To Dos: 
+ Completed: During snapshotting, make sure that we capture the committedConfig state. Then, make sure we load the correct committedConfig state during startup. 
+ Completed: When I am in transition mode, reject all incoming RPCs from entering the log. 
+ Completed: Before executing a command (put, get, append), make sure that it belongs in this group in the current configuration. 
+ Completed: Before entering a command into the log, make sure that it belongs in this group within the current configuration. 
+ Completed: Figure out if it's necessary to snapshot during configuration changes. Currently, I don't snapshot when rejecting logs during a transition. The solution to this is move the snapshot call into the locations where we handle each Op. 
+ Completed: Identify entries of the commit table that are relevant to a specific shard. Only send these entries when transferring shards
+ Completed: Currently, we add very old configuration changes to the top of the log. The reason for this is that when we are replaying a log, we still query mastershard with checkConfiguration. And, we add this to the top of the log. Make sure this case is handled correctly
+ Completed: Figure out how to initialize configuration on startup. Getting the latest configuration is *not* the right approach because the configuration needs to match the current state of the system. 

Possible Issue: 
+ Does ShardMaster ever crash? If so, do we need to persist data on shardmaster. 
+ Currently, I am committing out of order requestIDs. That means that a client can send RequestID #1 to GroupA and RequestID #2 to GroupB. Thus, GroupA will never see RequestID#2. This should be fine: If the same requestID is sent twice, it will be caugt in the commitTable. If we send a higher requestID, we know that the client has received a response for the previous requestID. So, if we know it's an old request, we throw an error. 
++ Possible Solution: Create a request ID for each group. Deal with each group seperately. 
+ Currently, I don't process any configuration changes if I am moving a shard away from or toward Group #0. 

Possible Optimizations: 
+ When a server that is in transition state receives a request, tell the client to take a break (timeout) before sending another request. This will make sure the client doesn't send endless requests when the server is in an extended transtion. The drawbak is if a single server is stuck in transition, but the rest of the gorup is making progress, then we would waste time. 
+ Use rf.GetState() in order to check if server is leader. This is valuable if 1) you only want the leader to print output and 2) you check if your leader before using Query. 

Questions: 
+ Should a group of servers have the same clientID for the purpose of other systems identifying them? I am assuming yes since a group of replicated servers should really function as a single server to the outside world. 
+ If you are not the leader, how do you handle configuration changes. Do you still need to send RPCs to transfer key/value maps? 

High Level Architecture Note: 
+ Note: A group is composed of multiple servers. Each group has it's own GID that it belongs to. We don't need to worry about changing the members of a group during operation. The groups are contained in Config.Groups[gid]. 
+ Note: Both kvshard-client and kvshard-clerk create a shardmaster Client. This client is used to talk to the shardmaster for configuration information. 
+ The mapping happens in the following sequence: key => shard (total of 10 shards) => each shard indicates a single group that handles that shard => each group contains multiple servers. 
++ So, shards represent a group of keys (where each key always goes to the same shards). These shards are then mapped to Groups of servers that keep track of those shards/keys. If more Groups of servers are added, the shards are re-distributed across the Groups to balance the system. 

Checking key in correct group: 
+ At a high level, all groups stall at each join/leave configuration change until all groups have caught up, and are in agreement about the current configuration. The configuration changes in between are just query configurations, and have no impact on the actual distribution of shards. So, whenever the distribution of shards changes, we are guaranteed that all groups synch towards the same distribution. 
+ So, when a server receives an operation, all it has to do is make sure that 1) it's not in transition and 2) it owns the shard of the key when it's in a stable configuration. If the key is not part of the group, it just indicates this to the client, who then updates. 
+ After failure, when catching up across persisted log entries, we might receive a request from a client that has a config far in the future. The easiest approach is just to reject until the servers have caught up. (think about if this is correct)
+ Implementation Question: When it is determined that a client has an old configuration number, should we sent it the current configuration number we are on. This way, the client can send keys to the appropriate groups, and make updates (that will be correct in the future). Or, do we just allow the client to advance to the highest configuration, and wait until the servers catch up. 
+ Implementation Question: If the servers died and are catching up on configuration numbers, should the clients be given the current configuration number, or just wait until the servers have caught up. 
++ Possible soltion: The easier implementation is to just stall the client. The client knows it's 1) in the most advanced configuration and 2) the servers will eventually catch up to this configuration. So, this might slow the client down (and decrease performance), but the server will eventually catch-up and the client will be able to add the request.
+ For AddShardKeys RPC: Here, we can never request data from the wrong group (since configuration tells us where to request dataf from). And, we can only handle these in the transition period. So, no additional checking is needed.  
+ Thoughts on rejecting put/append requests if we are not responsible for a client key (since we are not responsible for a shard). Essentially, just check an incoming request against the last committed configuration. We definitely need to reject the request when I receive it on the applyCH if I am not responsible. The question is can I also reject it before applying it to the applyCh. It's probably more efficient if I do to not clog up the raft log. In a few cases, the configuration change will already be in my applyCh to make me responsible, but the RPC has already been rejected. In this case, the Client will get the correct Group, and then try me again. So, in the rare case this happens, it wold still be correct. 

Configuration Update Notes: 
+ We can only update the local configuration if the configuration has been agreed upon by Raft. This ensures that that the configuration change will be processed in order within a single Group. 
+ We only can switch to a new configuration if: 1) we have received all the shards we need and 2) we successfully sent all the shards to the correct other group. 
+ Duplicate Detection (new reasoning): The reason we need duplicate detection is if: 1) a op was received on the applyCh, 2) we processed the request but the server didn't hear our response, and 3) we receive the same request again from the same server. In thise case, we should just check our committedEntry table and response to the server immediately (instead of having to put it through raft again).
+ Ensuring configuration changes are in order: 
++ Don't need an RPC table since I never need to re-apply to an asynch RPC. The system knows it's successful when the configuration increases. 
++ Don't need to add this to the commitTable. The reason for this is that the committedConfig functions as the commit table. We always know what the last committed config was, so we know if the current config is a duplicate, or the next config. Essentially, the committedConfig functions as it's own row in a commit table. 
+ Transferring Commit Tables: When transferring commit tables, I only transfer the keys that need to be exchanged. The reason for this is that the goal is to move around shards, and only keys belong to shards (not configuraation and shard changes). 

Configuration Update Implementation:
+ When receive configuration change on applyCh, change to configuration change state. 
+ In configuration change state, do not process any new logs from applyCh except if they help complete the configuration change. 
+ Since we are only interested in completing the configuration change at this point, don't accept any requests from clients. And, reject any logs on the applyCh other than configuration changes. 
+ Outdated: Upon receiving a configuration change, start 2 go routines. One go routine to make sure that we receive all shards. And, another go routine to make sure that we can send all shards. 
+ To send shards, send the shard to the appropriate group. Make sure that the server in the group is 1) in the appropriate transition state, and 2) leader. If they are both, the server will eventually receive the keys on the applyCh. When it does, it should update it's state. 
+ To receive shards, the system needs to keep track of all the shards it should receive. When it receives a shard, on the applyCh, it should add this to the map, and then it should remove the shard from the list of shards it should receive. 
+ Once a group has both successfully sent, and successfully received all the shards it needs, then the group can move out of transition state, and continue processing new requests. 

Sending Shards Brainstorm: 
+ To send shards: A configuration change indicates that we are changing into a transition state (not thave we have successfully changed into a new configuration). Currently, we are not snapshotting at this log, and thus don't have to persist the transition state in the snapshot. Once we have executed this operation, we should have a worker that uses the transitionState to send out RPCs. This is not part of the execution, but needs to happen before the transition state is complete. Then, the applyCh loop continues receiving RPCs, but rejects all RPCs until we get the shard log. 
++ The problem: What if we receive all shards, but haven't finished sending the shards. Then, we still will be in transition state, and we will reject any and all logs until we have successfully sent the RPC. This is an issue because 1) different servers in a group will be in different states and 2) when we re-play we won't replay deterministically. 
++ The solution: We transition out of the state as soon as we have all the shards. The problem here is what if we get a new configuration change that indicates that we receive the shard that we sent away. And, we get the shard back. Then, this transition state will be overwritten. 
++ The other issues is that we are still snapshotting. So, when we restart, we have to make sure that we stored the transition state and we have to make sure that we re-run the transition worker. 
++ Possible solution: We might have to creat a transition state that includes a different transition state for each shard that we need to send to. 


Updated Approach to Sending Shards: 
+ Upon receiving configuration change, change into transition state. To chage into transition state: 1) store the shards that need to be received, 2) indicate that in transition state, and 3) add the shards that need to be sent to an array. 
+ Remain in transition state until all shards received. When in transition state, reject all logs until receive all the shards. To move out of transition state, check if all the shards have been received. 
+ Add all the shards to be sent to an array. Each element in the array needs to include: 1) key/value of all the shards we need to send to a group, 2) the group we need to send to, and 3) any relevent configuration information. 
+ Create a worker loop that runs in the background. This should be created upon startup. The worker loop does the following: 
++ Check if the server is the leader. 
++ Create a go routine for each item on the transfer list. We only need to create each go routine once. The solution to accomplish this is create a seperate array of the same lengths as ShardsToTransfer. This arrays keeps track if this server has spawned a go routine for the specific entry in the array. Upon re-start, this will be initialized to 0, and so we will spawn all of these RPCs as we should. 
++ The go routine essentially is RPC handler that continually attempts to send the shards to the pre-defined group. We do not leave this accept with a success. 
++ If the go routine receives a success, then put the response into Raft. Raft will make sure that the server is leader. 
++ Once we receive this log from raft, 1) remove the entry from ShardsToTransfer, 2) remove the indicator of if the RPC has been sent, 3) and make sure to persist.
+ Note: Once we spawn an RPC they will always be running. So, even when the server is no longer leader, we will still try to send shards. This is not a problem since the other group can receive the shards from anybody. Only the leader will put the response into Raft. 

To Do (Update Sending Shards):
+ Why do I need to set kv.committedConfig and kv.transitionState before getting snapshot data? Fix this. 
+ When rejecting logs during the configuration transition, I should respond to the client of those logs indicating that they should resend them. 
+ Move manageSnapshots call to the end of the function. I currently don't have it at the end of the function to be thoguhtful about when not to take a snapshot.
+ Possible Issues: Try servers randomly (instead of in order) when sending RPCs
+ Possible Issues: Snapshotting implementation
+ Debugging Note: Since we are failing during unreliable, the issue is related to unordered RPCs or other network issues. 

Thoughts
It seems like I have a dead lock issue. With a deadlock issue I should make sure that 1) I am replying to RPCs (even an RPC times out, that server probably has an issue) and 2) log around locks and see who hasn't released a lock. 


Hints: 
+ Servers probably should query shardmaster to determine what keys to accept/reject or transfer to other servers. 
+ If configuration 16 includes a group, but configuration 17 omits the group, that does not mean that the group is disconnected or dead. Indeed, the group has to still be alive and reachable in order for the system as a whole to proceed to configuration 17. When all the groups have reached configuration 16, then they can all proceed to configuration 17 by moving the key/value pairs as required by the difference between configuration 16 and 17.

