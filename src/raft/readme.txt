ToDo Notes: 
+ All changes to server state need to happen synchronously (either through channel or mutex)
++ When I switch between states, clearly show which variables I need to reset
++ Do I need to mutex (or channel) the act of switching between states. 
+ To Do: When I transition between states, handle correctly initializing variables. 
+ To Do: Implement error check to make sure => Leaders never over-write existing entries in their logs. 

Possible Option: 
+ Handle all client interrupts and timer interrupts within a selection function. 
+ Handle all variable change with mutex: 
++ For reads, use mutex to read the variable. 

Question: 
+ Can incoming RPC requests be processed in parallel? 
+ Do all items in the rf structure need to be locked when reading/writing for race conditions? 
+ Are there any situations when we are reading/wrting to raft structure where we don't need a mutex: for example, what if I just read/write without writing it back to the variable. 
+ Are we essentially just mutexing entire functions? So, the only time we don't have a lock on those variables is when we are communcating?
+ Do I need to handle transitions differently? 


Remember Notes: 
+ If a follower is behind, you need to send it log data through heartbeats. Heartbeats should send reall data, and not just empty data. 
+ Important: Might have to implement your own RPC timeout (since the inherent one is too long???)
+ Terminology: The word "hear" is when a server "handles a request" and when a server gets back a reply from a request sent out. 
+ Put a buffer channel in Start(): They send many back, to back start commands (that I think I need to handle in order). This will hopefully solve that problem. 
+ Need to put things into ApplyChannel!!!! The behavior of leader and follower commit is the same. 

Description of Protocol: 

Elections: Implemented
+ Each server in one of three states: leader, candidate or follower. 
++ Whenever a server starts up, it's a follower. 
++ If a server doesn't get any communication for an election time_out period, it begins an election by 1) incrementing it's current term, 2) voting for itself, and 3) sending out a RPC request to vote. Before sending out RPCs, start a new election_timeout timer with a new randomized timeout (this is used to handle split vote situations)
++ Outcome of current server wins the election: Server wins if positive votes received from a) majority of servers and b) in the same term that the candidate is in. If a candidate wins, then a) it transitions to leader state and b) it then can send heartbeats to all the other servers to establish it's authority. Note, in a single term, only a single candidate can win. 
++ Outcome of another server wins the election: While being a candidate and waiting for sufficient votes, the server can transition to follower if: 1) it recieves an Append Entries from a server in a higher term (always the case) or 2) it recieve Append Entries from a server in the same term. Error check: If you a leader ever receives append entries from a server in the same term, then there are two leaders, and we have to throw a huge ERROR. 
++Outcome of no server winning the election: A server times out with election timeout, and begins a new election by: 1) resetting voted for, 2) incrementing the term, 3) voting for itself, 4) sending out a RPC vote request series. 

+ Leaders: 
++ Only leaders can send Append Entries RPC requests.
++ Leader election logic: The Leader must have all the entries in it's log that have been committed when it's elected. We ensure this by: since a leader must be elected by a majority, it is guaranteed that at least one of the this majority will have the last committed entry. If the leader has this last committed entry, it will recieve the vote. If the leader doesn't have this last committed entry, it won't. Since we don't know exactly what has been committed and what hasn't, we only grant a vote to somebody who is equally up-to-date or more up-to-date than the voter. This ensures that the leader will be ahead of the voter or at the same level. If the leader doesn't have the last committed entry (where a majority must have this last committed entry for it to be committed), then it cannot win the election. 

+ Term: Implemented. 
++ Term begins when a follower wants to become leader. So, it becomes a candidate. 
++ Multiple candidates can try to become leader in the same term. In this case: 1) 2 election_timeout ends, 2) 2 followers becomes candidates and both increment their term to the same higher term, 3) both candidates send out requests for votes, 4) all followers vote for only a single candidate in each term (so, their votedFor variable can only be set once per term), 5) see below for handling different outcomes

Exchanging Terms: Implemented 
+ Terms are exchanged whenever servers communicate. The below logic is relevant both for a received request and a reply. 
++ If any server receives a message or reply with a higher term, it updates it's term. This is done before the servers start interacting. This is true for any RPC reply or request. 
++ If a candidate or leader receives an RPC or reply with a larger term, it immediatley reverts to follower. 
++ If any server receivers an RPC requst with a stale term number, it rejects it (except for a vote)

+ Servers retry RPCs if they don't receive responses in a timely manner. 


Personal Description of Protocol: 
+ When to reset variables: 
++ votedFor: Whenever a server sees a change in term, immediately reset votedFor to -1. If they just changed term, they haven't voted for anybody int that term yet. 
++ voteCount: Whenever a server transitions to another election (by transitioning to canddiate), reset the voteCount to 0. We only care about resetting voteCount at the beginning of an election. 
++ Transition to Leader: Stop the election_timeout clock. Start the heartbeat clock. 
++ Transition to Follower from Leader: Stop heartbeat. Start election Timer. 
++ Transition to Follower from Candidate:  Restart election timer (which regonizes that there is another leader)
++ Anytime change term: Check if you need to change state. 
++ Anytime get an appendEntries RPC from higher or equal term: recognize leader by resetting election timer
++ Anytime we change state to follower: recognize leader by resetting election timer


Possible Debug Issues: 
+ We try to handle setting the heartbeat intelligently: When you transition to leader, turn it on. And, if you are leader, keep on restarting it once it runs out. Another option is to use Tickers (can possibly use for election timer as well by just reinitializing the clock each time it runs out)
