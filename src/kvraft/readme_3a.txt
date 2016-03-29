******************* Lab Implementation Notes l*******************

Question: 
+ Is there any reason I need to lock my Client? I don't think so. 
+ Why can't we Lock Start() and Kill() for rf? 
+ For go routines that are awaiting a reply from the RPC (but the RPC has already timedout by my code), the go routine will block for ever. Do I need to garbage collect this go routine?
+ What does cover mean as a testing parameter? 

Potential Optimizations: 
+ Have KVServer keep track of leader and send that to the client (ti improve how quickly the client finds the correct server)
+ Close open channels after the RPC has returned. Currently, we are just garbage collecting the channels. 

Possible Issue: 

Future To Do: 
+ Thinking about loop on line 237 in Raft: Can we get infinite looping. 

******************* Description of Protocol: Lab 3a *******************
Note: 
+ Sever discovers not a leader
++ Rejects future requests from clients. 
++ Leaves existins RPCs in RPCQue. Only returns RPCs to clients if: 1) the client times out since the server takes too long, 
2) the server receives a raft index for another raft command that conflicts with an index in the RPCQue, 3) successful return of data, or 4) the server dies. 
+ Finding Correct Operation: To find the right RPC, you need to match the index and the operation. The operation contains requestId and ClientID as well. If you just use the index, you might get an operation with the same index, but from a different term (thus probably being a different operation.)
+ Two client requests into Raft: You can enter two clients requests into Raft (for example) when you 1) have enter a RequestID, 2) the RPC timeouts, and 3) you send the same RequestID to the same server who still thinks they are the leader. Before, they were partitioned, and weren't the leader. But, then when the partition is fixed, they become the leader, and so process the same RequestID twice. On the applyCH, just make sure you don't execute the same requestID twice. Check the commitTable
+ Returning to a client that has timedout: If you return to a client that has timedout, you will write onto the RPC_returned channel. But, there will be no code to read this channel, so the go routine will be blocked for ever. Due to this, it's safe to 1) return data from a commitTable even when server is not the leader and 2) return data from applyCh if the RPCque still has a link to the client (the client will only still be listeing to this channel if it has not sent out another RPC. If the client is not listening, then the go routine will just stall, and the client will never get this information. )
++ Returning data to client when not leader: Any time there is a commit, RAFT guarantees taht there will be a commit on all servers eventually. So, if there is a commit, we can return that data to the client (either through the commitTable or directly from the applyCh). 

Tables: 
+ CommitTable: The commitTable is only updated on the applyCh receive (only if we are 100% sure it's been committed.). When we receive an operation from raft or from the client, we chekc the commitTable. If it has already been committed before, then we submit it directly from the commitTable. This saves us time (since we don't have to put operations through raft twice) and it makes sure that we dont commit the same operation twice (on the output of raft). 
+ RPC Que: Evert time we receive a RPC, we add a structure into the RPC quue. This structure includes a channel (stored at the raft index) that allows us to return the RPC to the client (by finishing the RPC function). If the client has already moved on when we reply, the go routine on the client side will get our response, and do nothing with it. 
++ We remove old entries from old RPCs if we recieve a new RPC at the same index. If we receive a new RPC at the same raft index (raft gives us the index), then we know that the request from the RPC is not being completed properly by raft (meaing that raft is now at a new term, and the old submission will not be completed.) Essentially, if we get the same index back, it implies that the old index is invalid, and so we have to return the RPC and it needs to resubmit with the system that is now working. We check if the RPC in RPC que is outdated whenever we receive an index, which is both when we call Start() and when we receive a committed command on applyCh. Thus, we only artifically unlock RPCs when we know that they are outdated since their index has been given back to us by a more up to date raft. 