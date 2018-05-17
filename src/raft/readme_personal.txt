******************* Lab Implementation Notes *******************

Possible To Do: 
+ Improve how I: 1) Switch between states (during a term switch) and 2) handle term/state checking during after an RPC exchange. Essentially, I repeate the same code in multiple locations instead of consolidating this into a function. 
+ Break out code into more modular re-usable functions: for example, demoteToFollower(), updateTerm(), firstTermElement(), isLogUpToDate(), becomeLeader() 
+ Handle transitions through a better method. Essentially, transitions should be placed in a function, so each transition can be triggered with a single line of code (to improve readability). 
+ Error check if any rf.log have out of index issues.  
+ Make all go routines into seperate functions, and make all the select statements into seperate functionss. This will improve readability. 

Possible Debug Issues: 
+ We try to handle setting the heartbeat intelligently: When you transition to leader, turn it on. And, if you are leader, keep on restarting it once it runs out. Another option is to use Tickers (can possibly use for election timer as well by just reinitializing the clock each time it runs out)
+ When server killed, make sure all functions stop running ASAP by adding: if rf.killed{return}. 


Optional Saftey: 
+ We include reply variable in checkCommitStatus. This is not explicity necessary since this function is contained in a mutual exclusion lock, and thus should not have a state change from the initial check. In fact, the entire checkCommitStatus has a few unnecessary checks. 


Questions: 
+ What is a good approach/strategy for using logs? (using actual logs, formatting of logs, showing color, etc)
+ I often see null characters when I print to a file. How do I fix this
+ Currently sending my full log. Will that cause problems later on? 

Notes: 
+ Finite Client Requests Handled (buffer Channel for incoming requests): Currently have limited amount of client requests I can process simultaneously. Drain the channel to make sure we are not backed-up. => This is not an issue. When a value is written on a channel, the code doesn't progress until we read from the channel. Thus, as long as there is active code looking to read from a channel, the channel will continiously be cleared. 


******************* Description of Protocol *******************

Elections: Implemented
+ Each server in one of three states: leader, candidate or follower. 
++ Whenever a server starts up, it's a follower. 
++ If a server doesn't get any communication for an election time_out period, it begins an election by 1) incrementing it's current term, 2) voting for itself, and 3) sending out a RPC request to vote. Before sending out RPCs, start a new election_timeout timer with a new randomized timeout (this is used to handle split vote situations)
++ Outcome of current server wins the election: Server wins if positive votes received from a) majority of servers and b) in the same term that the candidate is in. If a candidate wins, then a) it transitions to leader state and b) it then can send heartbeats to all the other servers to establish it's authority. Note, in a single term, only a single candidate can win. 
++ Outcome of another server wins the election: While being a candidate and waiting for sufficient votes, the server can transition to follower if: 1) it recieves an Append Entries from a server in a higher term (always the case) or 2) it recieve Append Entries from a server in the same term. Error check: If you a leader ever receives append entries from a server in the same term, then there are two leaders, and we have to throw a huge ERROR. 
++ Outcome of no server winning the election: A server times out with election timeout, and begins a new election by: 1) resetting voted for, 2) incrementing the term, 3) voting for itself, 4) sending out a RPC vote request series. 

Leaders: Implemented
++ Only leaders can send Append Entries RPC requests.
++ Leader election logic: The Leader must have all the entries in it's log that have been committed when it's elected. We ensure this by: since a leader must be elected by a majority, it is guaranteed that at least one of the this majority will have the last committed entry. If the leader has this last committed entry, it will recieve the vote. If the leader doesn't have this last committed entry, it won't. Since we don't know exactly what has been committed and what hasn't, we only grant a vote to somebody who is equally up-to-date or more up-to-date than the voter. This ensures that the leader will be ahead of the voter or at the same level. If the leader doesn't have the last committed entry (where a majority must have this last committed entry for it to be committed), then it cannot win the election. 

Term: Implemented. 
++ Term begins when a follower wants to become leader. So, it becomes a candidate. 
++ Multiple candidates can try to become leader in the same term. In this case: 1) 2 election_timeout ends, 2) 2 followers becomes candidates and both increment their term to the same higher term, 3) both candidates send out requests for votes, 4) all followers vote for only a single candidate in each term (so, their votedFor variable can only be set once per term), 5) see below for handling different outcomes

Exchanging Terms: Implemented 
+ Terms are exchanged whenever servers communicate. The below logic is relevant both for a received request and a reply. 
++ If any server receives a message or reply with a higher term, it updates it's term. This is done before the servers start interacting. This is true for any RPC reply or request. 
++ If a candidate or leader receives an RPC or reply with a larger term, it immediatley reverts to follower. 
++ If any server receivers an RPC requst with a stale term number, it rejects it (except for a vote)

Log Replication Overview:  Implemented
1) Leader receives client request. It appends the command to log as new entry. 
2) Leader sends parallel AppendEntries RPC requests to all other servers. If the other server doesn't respond, the leader tries to resend indefinitely. 
++ In the AppendEntries RPC, leader includes inex and term of entry that immediately precedes new entry. If the follower doesn't find an entry in it's log with the same index/term, then it rejects the new entries: it fails the consistency test. Here, remember to handle the initial state. 
++ Consistency test: The leader maintains a 'nextIndex' for each follower, which is the index of the next log the leader will send to the follower (here, the leader should send the log at the index, and all future log the leader has). nextIndex should initalized to the index just after the last log entry. For logs to be consistent, leader/follower must find the last log where the two entries agree (term/index), and then delete the entries after that point. 
++ Pass consistency test: The follower replies "successful" and its log is identical to the leader's log up to the new entries. The follower will 1) remove entries (if any) and 2) append new entries (if any). Must handle the case where there are none. Important: once a follower's logs are consistent, it will remain that way for the rest of the term. 
++ Fail th consistency test: The follower replies "failed" and the leader decrements nextIndex and retries the AppendEntries
++ Optimization: The follower returns 1) term of conflicting entry, and first index it stores for that term. 
3) When entries have been safely replicated, the leader applies the entry to its state machine.  Only then it returns the result to the client. 

Commit Entry: Implemented
+ Only leader decides when safe to apply log entry to state machine. Applied entry is called committed. 
1) Log committed once the leader that created the entry (*it must be the leader that created it*) has replicated it on the majority of servers. Once Leader committs entry, all previous entry in log of leader are also deemed committed. 
++ Important distinction: A leader cannot assume that when a majority of the servers have a log from a previous term, it's committed. It can only assume a log is committed when a majority of the servers have a log from this term. If they do, then the server knows everything is committed up to that point. The reason for this is if you assume logs in old terms are committed, then a server with a log in a later term might be able to be elected and overwrite those "committed logs". 
2) Once committed, the Leader updates its commitIndex field, and sends this out on all AppendEntries RPC. 
3) When a follower finds out through the commitIndex field, it applies up to that log in log order. 


Personal Description of Protocol: 
+ When to reset variables: 
++ votedFor: Whenever a server sees a change in term, immediately reset votedFor to -1. If they just changed term, they haven't voted for anybody int that term yet. 
++ voteCount: Whenever a server transitions to another election (by transitioning to canddiate), reset the voteCount to 0. We only care about resetting voteCount at the beginning of an election. 
++ Transition to Leader: Stop the election_timeout clock. Start the heartbeat clock. Initialize nextIndex and matchIndex. Possibly reinitialize clientRequestChan
++ Transition to Follower from Leader: Stop heartbeat. Start election Timer. 
++ Transition to Follower from Candidate:  Restart election timer (which regonizes that there is another leader)
++ Anytime change term: Check if you need to change state. 
++ Anytime get an appendEntries RPC from higher or equal term: recognize leader by resetting election timer
++ Anytime we change state to follower: recognize leader by resetting election timer
++ Reset election_timer: receive append_entry from current leader or vote for candidate
+ Servers retry RPCs if they don't receive responses in a timely manner. 
