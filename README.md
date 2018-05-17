# A Fault-Tolerant, Sharded Key-Value Storage Service

Over the course of 3 months, I built a fault-tolerant, sharded key-value system almost completely from scratch. 
The project can be split into three sub-systems: 
* **First**, I built a consensus service based on the Raft protocol that ensures distributed servers agree on a single result.  
* **Second**, I used my Raft library to build a key-value service replicated across multiple servers to ensure fault-tolerance. 
* **Third**, I expanded my key-value service to shard the keys across multiple replica groups, and allow for managing their configuration while the servers are live. 

To validate our implementations, we were provided with tests that simulated server failures, partitioned networks, unreliable networks, and many other situations + edge cases. Since each of the above services are inter-dependent, a bug in any service can cause failures in other services. That means I spent most of my time debugging by pouring over 100,000+ line debug logs, looking at deadlocks, livelocks, inconsistent logs, etc. 

I built the system as part of MIT’s 2016 Distributed System course ([6.824](http://nil.csail.mit.edu/6.824/2016/index.html)). The course is (in)famous for being one of (if not the most) demanding CS course at MIT. 

### Raft
Our goal is to build a fault-tolerant system. We accomplished this by replicating a server’s state across multiple servers (called replicated state machines). We then use Raft to keep the replicated servers in-synch. To keep the state machine’s in-synch, each state machine needs to execute the same operations in the same order. Raft is a consensus protocol that maintains a log of operations that will be applied to each server (state machine) in a defined order. Thus, Raft ensures that each state machine processes the same operations, producing the same results and thus arriving at the same state. In a successful implementation, it will appear to clients that they interact with a single, reliable state machine.  

I implemented the entire Raft service as described in the original [Raft paper](https://raft.github.io/raft.pdf). This includes: 
* Voting to elect a leader to manage concencus across the replicas 
* Receiving/sending heartbeats to monitor if a leader node fails 
* Safely replicating logs across state machines by ensuring that a majority of replicas have approved a log entry before committing it
* Persisting certain states on the disk
* etc

### Fault-Tolerant Key-Value Storage Service
We built a key-value storage service that replicates its state across multiple servers. We used our Raft library to maintain a consistent state across servers. In this implementation, we guarantee sequential consistency. This means that no matter which server the client interacts with, a get (read) command should observe the most recent put/append (write) command. 


The service is split into two functional parts:  
* **Client-Side API**: The client-side API allows clients to Put, Append and Get keys/values from the distributed storage service. We use RPCs (remote procedure calls) to communicate between client and server.  
* **Server-Side Infrastructure**:  On the server side, we built the infrastructure to triage incoming client requests, update the key-value store once Raft reaches consensus, and respond back to the correct client. This includes helping clients find the leader node, rejecting duplicate requests from clients (either already committed or just staged in the log), handling requests asynchronously from multiple clients, etc. 

#### Snapshot
I updated Raft to include snapshotting. In real world implementations, memory constraints limit the size of the Raft log. Snapshotting is a technique that captures the current state of the key-value service, and thus allows for Raft to delete any log entries prior to the snapshot. Also, using snapshots, we can bring failed and partitioned nodes back up-to-date more efficiently. 

### Shared Key-Value Service 
I expanded my key-value service to shard the keys across multiple replica groups, and built a service to manage the replica groups configuration while the servers are live. Specifically, the sharded key-value service allows for:
+ Load balancing requests across replica groups by moving shards between them. 
+ Adding and removing replica groups while servers are live (and automatically rebalancing the shards acrss replica groups)

The goal of sharding is to increase system through-put. Since each replica group handles only a subset of the keys, the entire system can handle more gets/puts simultaneously. 

To accomplish this, we needed to: 
* **Build a Shard Configuration Service (ShardMaster)**: The ShardMaster is a separate replica group tasked with maintaining fault-tolerant records on the configuration of the system. Specifically, the ShardMaster keeps track of the mapping from shards to replica groups. 
* **Update the Key-Value Storage servers**: We needed to update the key-value storage servers to 1) redirect requests to the correct replica group, 2) monitor the ShardMaster for configuration changes, 3) migrate shards using RPCs from one replica group to another during load balancing, etc. 
