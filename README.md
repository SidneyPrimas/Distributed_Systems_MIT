# A Fault-Tolerant, Sharded Key-Value Storage Service

Over the course of 3 months, I built a fault-tolerant, sharded key-value system almost completely from scratch. 
The project can be split into the below three subsystems. Click on the link to navigate to the readme of each subsystem. 
1. [**Raft:**](https://github.com/SidneyPrimas/Sharded_Key-Value_System/tree/master/src/raft) I built a consensus service based on the [Raft protocol](https://raft.github.io/raft.pdf) that ensures distributed servers agree on a single result.  
2. [**Fault-Tolerant Key-Value Storage:**](https://github.com/SidneyPrimas/Sharded_Key-Value_System/tree/master/src/kvraft) I used my Raft library to build a key-value service replicated across multiple servers to ensure fault-tolerance. 
3. [**Sharded Key-Value Storage:**](https://github.com/SidneyPrimas/Sharded_Key-Value_System/tree/master/src/shardkv) I expanded my key-value service to shard the keys across multiple replica groups, and allow for managing their configuration while the servers are live. 

To validate our implementations, we were provided with tests that simulated server failures, partitioned networks, unreliable networks, and many other situations + edge cases. Since each of the above services are inter-dependent, a bug in any service can cause failures in other services. That means I spent most of my time debugging by pouring over 100,000+ line debug logs, looking at deadlocks, livelocks, inconsistent logs, etc. 

I built the system as part of MIT’s 2016 Distributed System course ([6.824](http://nil.csail.mit.edu/6.824/2016/index.html)). The course is (in)famous for being one of (if not the most) demanding CS course at MIT.  

A huge thank you to our incredible professors [Robert Morris](https://en.wikipedia.org/wiki/Robert_Tappan_Morris) and [Frans Kaashoek](https://en.wikipedia.org/wiki/Frans_Kaashoek). They represent everything that is good about academia. 

## Setup and Testing 
First, install [Go](https://golang.org/) (I used v1.5). Then, get setup with the following commands:
```
$ git clone https://github.com/SidneyPrimas/Sharded_Key-Value_System.git
$ cd Distributed_Systems_MIT
$ export GOPATH=$(pwd)
$ cd src/shardkv # for additional testing, shardkv can be replaced with other subsystem folders
$ go test -test.v
```

For the most end-to-end tests, navigate to src/shardkv. To test different subsystems, navigate to the directory of the subsystem. Possible options include: 
```
$ cd src/raft
$ cd src/kvraft
$ cd src/shardmaster
$ cd src/shardkv
```
