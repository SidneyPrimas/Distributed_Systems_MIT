******************* Lab Implementation Notes l*******************
Change to Raft: 
+ Updating Log: Previously, when a log wasn't up to date (and we needed to request more log entries fromt the leader), the leader sent over the entire new log upon it's next request. This has been changed to sending over the log entries starting from the first log entry in the conflicting term. 
+ Generic Term/State checking of Incoming RPC: When RPCs return to sendRequestVote and sendAppendEntries, we first check if the server is in the correct term. If it isn't, then we update the term, and put it into follower mode. For these changes to the server's states, we do not need to check if the server is a candidate/leader since this change needs to be done independent of this. For any changes specific to the server being a candidate/leader, we always need to check the term and the server state. 

To Do: 
+ Make sure to handlse the case where system crashes in between snapshot persistance and raft persistance. 
+ When we restore a snapshot, where to we store lastIncludedIndex and lastIncludedTerm
+ Make sure the truncated log can be garbage collected by GoLang.
+ Upon restart ater crash, reinitialize base on the last snapshot. Also, if the snapshot existed, reinitialize commitIndex and lastApplied to lastIncludedIndex. I probalby need a way to pass Snapshot parameters (as request by Raft) to Raft. 
+ With getSnapIndex, do we check out of range situation (through error). If we do, we need to handle the case where index is 0 seperately (since this will throw an error).
+ Put RPC Timeouts into a seperate function to allow for more functiona modularity (easier to read code). Call it ok := rf.processRPC(). 
+ Get rid of buffered serviceClientChan channel. Don't need it. 
+ Move IndexTranslation helper function into helper section. 
+ Turn sending heartbeat (the go function) into a stand alone function. 
+ Implement situation where failure between snapshot and raft persist. Use guide to raft for help. 
+ I need to include lastCommitTable in snapshot as well. 
+ Implement shorter rpc timeouts for client/server
+ IS IT OKAY TO PERSIST UNLCOK BERFORE WE PERSIST SNAPSTHO

Important: 
+ Possible Deadlock: Currently, we have a possible deadlock situation when: 1) a commit has been applied to the channel. When we are waiting for KVServer to execute teh commit, we don't lock the applyCh in case we need to create a snapshot (so we can call TruncateLogs), 2) So, the scheduler is free to process incoming RPCs, including an incoming snapshot. When the install Snapshot is handled, we grab the lock and then wait to apply the snapshot to the applyCh. Then, we have a deadlock. 
++ The solution: Create a seperate lock that makes sure that when we are waiting for a applyCh response we don't process installSnapshot 

Question: 
+ How do we access information requested by raft but stored in kvServer?
+ When passing an array from KVserver to Raft, do I need to copy it (look in snapshotData)s

Question about step 6: 
1) When I apply from SnapshotRPC, do I update nextIndex and matchIndex of master? If so, how do they know what to update to? 
Note: We need to update the master, since that's the whole point of sending the Install Snapshot RPC. When we delete the entire log, we obviously can update the master. When we trim the log, we know that lastIncludedIndex is in the log, so we can update match index and next index (?)
2) 

Careful and Possible Issues: 
+ When presisting (or maybe it was sending) the CommitTable, the persist will only work if the types start with a capital letter.
+ Possible indexing issue: When we remove incorrect entries from a follower (rf.log[:logIDifferent]), we convert directly from snapIndex to realIndex. We know this snapIndex exists because it cannot have been committed if it's being removed. 
+ Are we going to create a deadlock when we lock a function that interacts between raft and kvraft. 
+ Two options for getting a snapshot: 1) Whenever we take a snapshot in server, send all snapshot parameters to raft and keep a copy locally. 2) Whenever we need a snapshot, request if from the server. One approach might be better for race conditions. 
+ If a snapshot doesn't exists, should I initialize lastIncludedTerm to -1 or not at all (causeing an error if it's called. )
+ Do we update matchIndex and 


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
+ Take Snapshot:
++ Determine if we need to take a snapshot: Use maxraftstate to determine when the raft GOB is too large (has too many bytes). Check if the GOB is too large every time the KVServer receives a message from the applyCh. When the KVServer receives a message from the apply channel, we know that the operation has been executed by the server, and so it only then makes sense to take a new snapshot. 
++ Take snapshot that includes: 1) the state of the server machine 2) the last index included in the snapshot, and 3) the last term included in the snapshot. 
++ Indicate to raft to truncate it's log. Truncate the log up the last included snapshot index (or lastIncludedIndex). 

+ Rambling Notes of Take Snapshot:
++ Use maxraftstate to determine when the raft GOB is too large (has too many bytes). Check if the GOB is too large every time the KVServer receives a message from the applyCh. When the KVServer receives a message from the apply channel, we know the raft has committed a log entry (which means that we can technically take a snapshot). Also, we know that the operation will be executed by the server, and so it only then makes sense to take a new snapshot. Note: Snapshots can only be take from committed entries. 
++ If the GOB is too large, initiate the snapshot process. The snapshot will be taken by the kv server. And, once it has been taken, the kv server should tell raft so that it can discard old entires. 


InstallSnapshot: 
+ Only allow MatchIndex to be updated by Append Entries. nextIndex should be updated either 1) every time we send a snapshot or 2) every time we receive a snapshot successful snapshot. I decided to update nextIndex every time I send a snapshot. This is an optimistic approach to sending a snapshot that might be less efficient, but not harmful. If the snapshot wasn't successfully installed, then the nextIndex will be roled back through AppendEntries until another snapshot is sent. 
+ Protocol 6 for Snapshotting: If the lastIncludedTerm is included at the lastIncludedIndex, then thee was a reordering of the AppendEntries RPCs. Essentially, there was no snapshot necessary. 
++ Approach 1): If the commitIndex is before or equal to the lastIncludedIndex, apply a snapshot anyway, discarding the log at and before lastIncludedIndex. If you apply the log, update lastApplied, commitIndex and make sure to locally persist the snapshot. The reason we only apply if the commitIndex is lower is that otherwise we risk discarding a more advanced map state in replacing it with the older state in snapshot. 
++ Approach 2) Since the follower can be updated through append entries, reject the snapshot completely (don't save the snapshot, don't persist any snapshot states, and don't resest the state machine). A future appendEntry will update this follower correctly. The 2nd Approach is saver and the first approach is more efficient. 
+ Note: If you persist data on the disk for a snapshot, then you need to update the state machine (map)

Recovery: 
+ Make sure that you persist rf.lastIncluded each time it's updated. This is important in case you have a situation where a snapshot is persisted, and the system crashes before the raft state is persisted. In this case, when we recover, we need to make sure to truncate the log first based on the lastIndex in the snapshot vs. the lastIndex in teh raft persist.  

+ InstallSnapshot RPC: When the leader discovers it no longer has log data requested by a follower, then the leader must install a snapshot to the follower instead. The leader sends the snapshot to the follower in a single RPC. The follower handles the snapshot based on the Figure13 instructions, and installs the actual snapshot by sending it through to the KVServer through the applyCh. 


