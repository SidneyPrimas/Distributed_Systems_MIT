# Fault-Tolerant Key-Value Storage Service
We built a key-value storage service that replicates its state across multiple servers. We used our Raft library to maintain a consistent state across servers. In this implementation, we guarantee sequential consistency. This means that no matter which server the client interacts with, a get (read) command should observe the most recent put/append (write) command. 


The service is split into two functional parts:  
* **Client-Side API**: The client-side API allows clients to Put, Append and Get keys/values from the distributed storage service. We use RPCs (remote procedure calls) to communicate between client and server.  
* **Server-Side Infrastructure**:  On the server side, we built the infrastructure to triage incoming client requests, update the key-value store once Raft reaches consensus, and respond back to the correct client. This includes helping clients find the leader node, rejecting duplicate requests from clients (either already committed or just staged in the log), handling requests asynchronously from multiple clients, etc. 

Find lab directions [here](http://nil.csail.mit.edu/6.824/2016/labs/lab-kvraft.html).