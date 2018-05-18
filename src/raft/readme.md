# Raft
Our goal is to build a fault-tolerant system. We accomplished this by replicating a server’s state across multiple servers (called replicated state machines). We then use Raft to keep the replicated servers in-synch. To keep the state machine’s in-synch, each state machine needs to execute the same operations in the same order. Raft is a consensus protocol that maintains a log of operations that will be applied to each server (state machine) in a defined order. Thus, Raft ensures that each state machine processes the same operations, producing the same results and thus arriving at the same state. In a successful implementation, it will appear to clients that they interact with a single, reliable state machine.  

I implemented the entire Raft service as described in the original [Raft paper](https://raft.github.io/raft.pdf). This includes: 
* Voting to elect a leader to manage concencus across the replicas 
* Receiving/sending heartbeats to monitor if a leader node fails 
* Safely replicating logs across state machines by ensuring that a majority of replicas have approved a log entry before committing it
* Persisting certain states on the disk
* etc

##### Including Snapshotting Functionality
I updated Raft to include snapshotting. In real world implementations, memory constraints limit the size of the Raft log. Snapshotting is a technique that captures the current state of the key-value service, and thus allows for Raft to delete any log entries prior to the snapshot. Also, using snapshots, we can bring failed and partitioned nodes back up-to-date more efficiently. 

Find raft lab directions [here](http://nil.csail.mit.edu/6.824/2016/labs/lab-raft.html).
Find snapshotting lab directions [here](http://nil.csail.mit.edu/6.824/2016/labs/lab-kvraft.html).