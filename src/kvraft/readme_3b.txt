******************* Lab Implementation Notes l*******************
Change to Raft: 
+ Updating Log: Previously, when a log wasn't up to date (and we needed to request more log entries fromt the leader), the leader sent over the entire new log upon it's next request. This has been changed to sending over the log entries starting from the first log entry in the conflicting term. 
+ Generic Term/State checking of Incoming RPC: When RPCs return to sendRequestVote and sendAppendEntries, we first check if the server is in the correct term. If it isn't, then we update the term, and put it into follower mode. For these changes to the server's states, we do not need to check if the server is a candidate/leader since this change needs to be done independent of this. For any changes specific to the server being a candidate/leader, we always need to check the term and the server state. 

To Do: 


******************* Description of Protocol: Lab 3b *******************
Note: 
+ K/V Server stores the snapshot with persister.SaveSnapshot(). KVServer snapshots the Raft's saved state when it exceeds maxraftstate bytes (specifically, when the GOB has more than maxraftstate bytes). 
+ When a follower receives a InstallSnapshot RPC from the leader, it has essentially received a new snapshot. Use the applyMsg for Raft to tell KVRaft  to intstall the snapshot it has just received snapshot. To do this, set UseSnapshot true, and include the full snapshot in Snapshot[]. When the KVServer receives a snapshot through applyMsg, the KVServer should update it's state immediately. 
+ KVServer tell it's Raft library to take a snapshot when the state size (the bytes of the logs persisted) grows too large. 
+ Send entire server state in an RPC (don't break it up into chunks)
+ Recommendation: When the leader wants to send a Install Snapshot RPC, the k/v server should hand the snapshot directly to raft (instead of raft reading it). Not sure why, but probably to make sure that read/writing is not happening at same time?  
+ Use Persister.RaftStateSize() to find out when the raft logs are too large (or the Raft GOB is too large).


+ When restarting (after a crash), the KVServer needs to use persister.ReadSnapshot() to to restore the state of the server from it's last saved snapshot. 

High Level: 
+ Managing Snapshots: Use maxraftstate to determine when the raft GOB is too large (has too many bytes). Check if the GOB is too large every time the KVServer receives a message from the applyCh. When the KVServer receives a message from the apply channel, we know the raft has committed a log entry (which means that we can technically take a snapshot). Also, we know that the operation will be executed by the server, and so it only then makes sense to take a new snapshot. Note: Snapshots can only be take from committed entries. 
++ If the GOB is too large, initiate the snapshot process. The snapshot will be taken by the kv server. And, once it has been taken, the kv server should tell raft so that it can discar old entires. If the GOB is below maxraftstate bytes, then move forward as usual. 
+ InstallSnapshot RPC: When the leader discovers it no longer has log data requested by a follower, then the leader must install a snapshot to the follower instead. The leader sends the snapshot to the follower in a single RPC. The follower handles the snapshot based on the Figure13 instructions, and installs the actual snapshot by sending it through to the KVServer through the applyCh. 