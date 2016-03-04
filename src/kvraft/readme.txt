Potential Optimizations: 
+ Should the kvServer (not just the Raft) keep track if it's a leader. 

Question: 
+ Is the index itself a unique identifier of a Operation? Or, do we need the term as well? 
+ Do I need to kill all the RPCs waiting for responses from Raft? 
+ What happens if I enter two of the same client requests into Raft?
+ Will we only have one index for every Start call. Is it every possible that we get a 2nd index that is the same? 

ToDo: 
+ If we find out that server is not leader, we need to clean out the waitForRaft to return the RPCs to the client (indicating that this server is no longer the leader)
+ Handle no key with Get
+ In Kill, make sure to close all the RPCs from the clietns that are waiting for a response.
+ When executing a command, don't execute it if it's the last command that was executed. 
+ Include random number for Op (check this to make sure command isn't committed twice). Check in KVServer or raft? 

Note: 
+ Returning RPCs to client with notLeader: 
1) If the server knows it's not the leader before submitting to raft, just return false. 
2) If the server discovers it's not the leader (after already having submitted client requests to Raft), kill those RPCs and tell the client to find the leader. My current understanding: the major reason for this is that it's safer for the client to send a request from the current leader and receive a response from the current leader. Also, if the 'wrong' leader took a command into it's log, it will most likely be over-written by a new leader. 
3) If the server crashes, it should ideally indicate to the client that it will be a follower. 

Clients: 
+ Clients only send one request at a time. 


Next: 
1) Implement processCommits (Make a map, commit to map, respond to appropriate client through Channel)
2) Implement Client 