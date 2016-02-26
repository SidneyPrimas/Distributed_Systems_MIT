package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import "sync"
import "labrpc"
import "time"
import "math/rand"
import "fmt"

// import "bytes"
// import "encoding/gob"



//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make().
//
type ApplyMsg struct {
	Index       int
	Command     interface{}
	UseSnapshot bool   // ignore for lab2; only used in lab3
	Snapshot    []byte // ignore for lab2; only used in lab3
}

// Create a simple system to store the raft state (that is human redable)
type RaftState int

const (
	Follower 		RaftState = 3
	Candidate 		RaftState = 2
	Leader 			RaftState = 1
)

func (state RaftState) String() string {
	s:=""
    if state&Follower == 3 {
    	s+="Follower"
   	}
   	if state&Candidate == 2 {
    	s+="Candidate"
   	}
   	if state&Leader == 1 {
    	s+="Leader"
   	}
   	return s
}



// Important: Since the first log has an index of 1, we can just use len to find the index
type RaftLog struct {
	Term 	int
	Command interface{}
}

//
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu        sync.Mutex
	peers     []*labrpc.ClientEnd
	persister *Persister
	me        int // index into peers[]

	// Your data here.
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	// Volatile internal Raft states (for all servers)
	commitIndex int 
	lastApplied int

	// Voltatile internal Raft state (only for leaders)
	// Note: nextIndex can only be ahead of leader index by 1 (which is when the logs are matching)
	nextIndex []int
	matchIndex []int

	// Persistant internal Raft states (for all servers)
	// currentTerm: Last term that server has seen. 
	currentTerm int
	// votedFor: candidateId that recieved vote in current term
	votedFor int
	log []RaftLog

	// Interrupt channel and timers
	electionTimer *time.Timer
	heartbeatTimer *time.Timer
	serviceClientChan chan int

	// Additional variables for each raft instance. 
	voteCount int
	majority int
	myState RaftState

}


// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool
	
	if (rf.myState == Leader) {
		isleader = true
	} else {
		isleader = false
	}

	term = rf.currentTerm

	return term, isleader
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here.
	// Example:
	// w := new(bytes.Buffer)
	// e := gob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	// Your code here.
	// Example:
	// r := bytes.NewBuffer(data)
	// d := gob.NewDecoder(r)
	// d.Decode(&rf.xxx)
	// d.Decode(&rf.yyy)
}


// Handles interrupts from timers, and clients. 
func (rf *Raft) manageRaftInterrupts() {

	for {
		select {
		// Handles incoming service requests. Incoming requests must be handled in parallel
		case logIndex := <-rf.serviceClientChan:

			// Note: Only handle Client Requests when Leader. 
			// Note: This check might be necessary if the serviceClientChan is backlogged, and we switch from a leader to a follower state
			// without this serve failing. Not 100% necessary since we check for Leader again below.
			if (rf.myState == Leader) {
				fmt.Printf("%s, Server%d, Term%d, State: %s, Action: Leader Begins log consistency routtine \n", time.Now().Format(time.StampMilli), rf.me, rf.currentTerm, rf.myState.String())
				fmt.Printf("TheLog: %q \n", rf.log[logIndex-1])

				// Sidney: If server believes itself to be the leader and sends AppendEntries, turn-on/reset heartbeat timer. 
				rf.heartbeatTimer.Reset(time.Millisecond * 50)


				// Protocol: Make new log consistent by sending AppendEntry RPC to all servers. 
				for thisServer := 0; thisServer < len(rf.peers); thisServer++ {
					if (thisServer != rf.me) {

						// Protocol: Create a go routine for each server that doesn't complete until the server is
						// as up to date as to the logIndex of the log of the leader. 
						//Note: Go routine will have access to updated rf raft structure. 
						go func(server int) {	

							// Note: While loop executes until server's log is at least as up to date as the logIndex.
							// Note: Only can send these AppendEntries if server is the leader
							for (logIndex >= rf.nextIndex[server]) && (rf.myState == Leader) {

								// Setup outgoing arguments.
								// Protocol: These arguments should be re-initialized for each RPC call since rf might update in the meantime.
								// We want to replicate the leader log everywhere, so we can always send it when the follower is out of date. 
								var reply AppendEntriesReply
								start_index_sending := rf.nextIndex[server]
								final_index_sending := len(rf.log)

								args := AppendEntriesArgs{
									Term: rf.currentTerm, 
									LeaderId: rf.me, 
									// Index of log entry immediately preceding the new one
									PrevLogIndex: start_index_sending-1,  
									// Fine to update commit index since if follower replies successfully, the follower log will be as up to date
									// as leader, and thus can commit as much as the leader. 
									LeaderCommit: rf.commitIndex}

								// Handle situation where only single entry in log
								if (args.PrevLogIndex == 0) {
									args.PrevLogTerm = -1
								} else {
									args.PrevLogTerm = rf.log[args.PrevLogIndex-1].Term
								}


								// Protocol: The leader should send all logs from requested index, and upwards. 
								args.Entries = rf.log[start_index_sending-1:final_index_sending]
								msg_received := rf.sendAppendEntries(server, args, &reply)
								
								// Handle the reply. 
								// Note: If the msg isn't received by server, send another message. 
								if (msg_received) {

									if (reply.Success) {
										// The follower server is now up to date (both the logs and the commit)
										// Protocol: After successful AppendEntries, increase nextIndex for this server to one above the last index
										// sent by the last AppendEntries RPC request. 
										rf.nextIndex[server] =  final_index_sending + 1
										rf.matchIndex[server] = final_index_sending
									} else if (!reply.Success) {
										// Protocol: If fail because of log inconsistency, decrement next index by 1. 
										rf.nextIndex[server] = args.PrevLogIndex
									}

								}
							}
						}(thisServer)
					}
				}
			}

		// Sends hearbeats. 
		// A server only sends hearbeats if they believe to be leader. 
		case currentTime := <- rf.heartbeatTimer.C: 

			//Error Checking
			if (rf.myState != Leader) {
				fmt.Printf("Error: Server is trying to send out a hearbeat when it's not the leader. Should not be possible.\n")
			}
			fmt.Printf("%s, Server%d, Term%d, State: %s, Action: Send out heartbeat \n", currentTime.Format(time.StampMilli), rf.me, rf.currentTerm, rf.myState.String())

			// Sidney: If server believes itself to be the leader, turn on heartbeat timer. 
			rf.heartbeatTimer.Reset(time.Millisecond * 50)


			// Protocol: If server believes to be the leader, sends heartbeats to all peer servers. 
			// Setup outgoing arguments
			args := AppendEntriesArgs{
				Term: rf.currentTerm, 
				LeaderId: 1, 
				PrevLogTerm: 1, 
				PrevLogIndex: 1, 
				LeaderCommit: 1}

			for i := 0; i < len(rf.peers); i++ {
				if (i != rf.me) {
					var reply AppendEntriesReply

					go func(server int, args AppendEntriesArgs, reply AppendEntriesReply) {	
						rf.sendAppendEntries(server, args, &reply)
					}(i, args, reply)

				}
			}

		// Handles election timeout interrupt: Starts an election
		case currentTime := <- rf.electionTimer.C: 

			// Error Checking
			if (rf.myState == Leader) {
				fmt.Printf("Error: Sending vote request as a leader. \n")
			}

			//TODO: Figure out when to change to candidate state
			// Protocol: Since the election timeout elapsed, start an election. 
			// Protocol: To indicate that this server started an election, switch to candidate state, and reset the votes. 
			rf.myState = Candidate
			rf.voteCount = 0
			// Protocol: For each new election, increment the servers current term. Since it's a new term, reset votedFor
			rf.currentTerm += 1
			rf.votedFor = -1

			fmt.Printf("%s, Server%d, Term%d, State: %s, Action: Election Time Interrupt \n", currentTime.Format(time.StampMilli), rf.me, rf.currentTerm, rf.myState.String())

			// Protocol: Vote for yourself. 
			rf.tallyVotes(true)

			// Protocol: Reset election timer (in case we have split brain issues.)
			rf.electionTimer.Reset(getElectionTimeout())

			// Protocol: Send a RequestVote RPC to all peers. 
			//Setup outgoing arguments
			args := RequestVoteArgs{
				Term: rf.currentTerm,
				CandidateId: rf.me,
				LastLogIndex: len(rf.log)}

			// If tree to ensure correct first-time LastLogTerm initialization 
			if len(rf.log) == 0 {
				args.LastLogTerm = -1
			} else {
				args.LastLogTerm = rf.log[len(rf.log)-1].Term
			}

			for i := 0; i < len(rf.peers); i++ {
				//Send a RequestVote RPC to all Raft servers (accept our own)
				if (i != rf.me) {
					var reply RequestVoteReply
					// Important: Need to use anonymous function implementation so that: 
					// 1) We can run multiple requests in parallel
					// 2) We can capture the specific reply of each request seperately
					go func(server int, args RequestVoteArgs, reply RequestVoteReply) {
						rf.sendRequestVote(server, args, &reply)
					}(i, args, reply)
				}
				
			}
			

		default: 

		}
	}

}

// Collects submitted votes, and determine election result. 
func (rf *Raft) tallyVotes(voteResult bool) {

	// A server only accepts votes if they believe they are a candidate. Important because might accumulate votes as
	// follower through delayed RPCs.
	if (rf.myState == Candidate) {
		//Count votes
		if (voteResult) {
	 		rf.voteCount += 1
	 	}

	 	// Decide election
	 	if (rf.voteCount >= rf.majority) {
	 		fmt.Printf("%s, Server%d, Term%d, State: %s, Action: Elected New Leader \n", time.Now().Format(time.StampMilli), rf.me, rf.currentTerm, rf.myState.String())
	 		// Protocol: Transition to leader state. 
	 		rf.myState = Leader
	 		rf.electionTimer.Stop()
	 		rf.heartbeatTimer.Reset(time.Millisecond * 50)
	 		// Initialize leader specific variables. 
	 		rf.nextIndex = make([]int, len(rf.peers))
	 		rf.matchIndex =  make([]int, len(rf.peers))
	 		for i := range(rf.nextIndex) {

	 			rf.nextIndex[i] = len(rf.log) + 1
	 			rf.matchIndex[i] = 0

	 		}
	 	}
 	}
}

//
func (rf *Raft) processAppendEntryRequest (args AppendEntriesArgs)	bool {
	if (args.PrevLogIndex == 1) {
		return true
	} else {
		return true
	}
}


//
// example RequestVote RPC arguments structure.
//
type RequestVoteArgs struct {
	// Your data here.
	Term 			int
	CandidateId 	int
	LastLogIndex	int
	LastLogTerm		int

}

//
// example RequestVote RPC reply structure.
//
type RequestVoteReply struct {
	// Your data here.
	Term 			int
	VoteGranted 	bool
}

//
// Function handles an incoming RPC call for Leader Election. 
func (rf *Raft) RequestVote(args RequestVoteArgs, reply *RequestVoteReply) {

	// Protocol: As always, if this server's term is lagging, update the term. 
	// Protocol: If the current system thinks they are a Leader or Candidate (but is in the wrong term), set them to Follower state. 
	if (args.Term > rf.currentTerm) {
		rf.currentTerm = args.Term
		rf.votedFor = -1

		if (rf.myState == Leader)  {
			//Transition from Leader to Follower: reset electionTimer
			rf.myState = Follower
			rf.electionTimer.Reset(getElectionTimeout())
			rf.heartbeatTimer.Stop()
		} else if (rf.myState == Candidate) {
			rf.myState = Follower
			rf.electionTimer.Reset(getElectionTimeout())
		}
	}

	// Protocol: Start protocl discussed in Figure 2 (receiver implementation)
	// Protocol: If the sender has a smaller term, reject the RPC immediately. 
	if (args.Term < rf.currentTerm) {
		reply.VoteGranted = false
	// Protocol: Determine if this server should vote for the candidate given that the candidate is in an equal or higher term. 
	// Only grant vote if: 1) candidate's log is at least as up-to-date as receiver's log and 2) this server hasn't voted for somebody else.
	// Setup guarantees that voter is in same term as candidate.
	} else {

		if (args.Term != rf.currentTerm) {
			fmt.Printf("Error: Server is voting, but is not in the same term as candidate.\n");
		}

		//Setup variables
		var allowedToVote bool = (rf.votedFor == -1) || (rf.votedFor == args.CandidateId)
		thisLastLogIndex := len(rf.log)

		// Setup variables: Handle case where log is not initialized. 
		var thisLastLogTerm int
		if (thisLastLogIndex == 0) {
			thisLastLogTerm = -1
		} else {
			thisLastLogTerm = rf.log[thisLastLogIndex-1].Term
		}

		// Determine if this server can vote for candidate: Vote when candidate has larger log term
		// Protocol: Reset the election timer when granting a vote. 
		if (allowedToVote) && (args.LastLogTerm > thisLastLogTerm) {
			reply.VoteGranted = true
			rf.votedFor = args.CandidateId
			rf.electionTimer.Reset(getElectionTimeout())
		// Determine if this server can vote for canddiate: When candidates's log term is equal, look at index
		// Protocol: Reset the election timer when granting a vote. 
		} else if (allowedToVote) && (args.LastLogTerm == thisLastLogTerm) && (args.LastLogIndex >= thisLastLogIndex) {
			reply.VoteGranted = true
			rf.votedFor = args.CandidateId
			rf.electionTimer.Reset(getElectionTimeout())
		// If the above statements are not met, don't vote for this candidate.  
		} else {
			reply.VoteGranted = false
		}
	}

	reply.Term = rf.currentTerm
	fmt.Printf("%s, Server%d, Term%d, State: %s, Action: Method RequestVote Prcoessed, Reply => (%+v) \n", time.Now().Format(time.StampMilli), rf.me, rf.currentTerm, rf.myState.String(), reply)
}



//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should probably
// pass &reply.
//
// returns true if labrpc says the RPC was delivered.
//
// Sidney: Function sends an outgoing RPC request with go. 
//
func (rf *Raft) sendRequestVote(server int, args RequestVoteArgs, reply *RequestVoteReply) bool {
	fmt.Printf("%s, Server%d, Term%d, State: %s, Action: Method sendRequestVote sent to Server%d, Request => (%+v) \n" , time.Now().Format(time.StampMilli), rf.me, rf.currentTerm, rf.myState.String(), server, args)
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)


	// Protocol: As always, if this server's term is lagging, update the term. 
	// Protocol: If the current system thinks they are a Leader or Candidate (but is in the wrong term), set them to Follower state. 
	if (reply.Term > rf.currentTerm) {
		rf.currentTerm = reply.Term
		rf.votedFor = -1

		if (rf.myState == Leader)  {
			//Transition from Leader to Follower: reset electionTimer
			rf.myState = Follower
			rf.electionTimer.Reset(getElectionTimeout())
			rf.heartbeatTimer.Stop()
		} else if (rf.myState == Candidate) {
			rf.myState = Follower
			rf.electionTimer.Reset(getElectionTimeout())
		}
	}

	// Document if the vote was or was not granted.
	rf.tallyVotes(reply.VoteGranted)
	return ok
}


//
//
type AppendEntriesArgs struct {
	// Your data here.
	Term 			int
	LeaderId 		int
	PrevLogIndex 	int
	PrevLogTerm 	int
	Entries 		[]RaftLog
	LeaderCommit 	int
}

//
//
type AppendEntriesReply struct {
	// Your data here.
	Term 			int
	Success 		bool

}

//
// Sidney: Function handles communication between Raft instances to synchronize on logs. 
// Sidney: Functions handles as an indication of a heartbeat
//
func (rf *Raft) AppendEntries(args AppendEntriesArgs, reply *AppendEntriesReply) {

	// If the request is from a stale leader (an older term), reject the RPC immediately. 
	if(args.Term < rf.currentTerm) {
		reply.Success = false
	// Handles the case where this server is in same or lower term. 
	} else {

		// Protocol: Recognize the leader in AppendEntries when sender is in larger or equal term
		rf.electionTimer.Reset(getElectionTimeout())

		// UPDATE THE TERM AND STATE OF THE SERVER

		// Protocol: As always, if this server's term is lagging, update the term. 
		// Protocol: If the current system thinks they are a Leader or Candidate (but is in the wrong term), set them to Follower state.
		// Protocol: In this case, recognize the leader by setting reseting the election timeout
		if (args.Term > rf.currentTerm) {
			rf.currentTerm = args.Term
			rf.votedFor = -1


			if (rf.myState == Leader)  {
				//Transition from Leader to Follower: reset electionTimer
				rf.myState = Follower
				rf.electionTimer.Reset(getElectionTimeout())
				rf.heartbeatTimer.Stop()
			} else if (rf.myState == Candidate) {
				rf.myState = Follower
				rf.electionTimer.Reset(getElectionTimeout())
			}


		// If they are in the same term, just recognize the leader. 
		} else if (args.Term == rf.currentTerm) {

			// Important Protocol: If this server is a Candidate, and it recieves append entries, then
			// this server knows that a leader has been elected, and it should become a follower. 
			if (rf.myState == Candidate) {
				rf.myState = Follower
				rf.electionTimer.Reset(getElectionTimeout())
			} else if (rf.myState == Leader) {
				fmt.Printf("Error: Two leaders have been selected in the same term. \n")
			}
		}

		// ONCE THE TERM/STATE ARE UPDATED, HANDLE THE APPEND ENTRIES REQUEST
		reply.Success = rf.processAppendEntryRequest(args)

	}

	reply.Term = rf.currentTerm
	fmt.Printf("%s, Server%d, Term%d, State: %s, Action: Method AppendEntries Prcoessed, Reply => (%+v) \n", time.Now().Format(time.StampMilli), rf.me, rf.currentTerm, rf.myState.String(), reply)

}

//
// Sidney: Function sends an outgoing RPC request from master to append entries to the logs of the other Raft instances. 
// Sidney: Only the leader sends this RPC
//
// returns true if labrpc says the RPC was delivered.
//
func (rf *Raft) sendAppendEntries(server int, args AppendEntriesArgs, reply *AppendEntriesReply) bool {
	fmt.Printf("%s, Server%d, Term%d, State: %s, Action: Method sendAppendEntries sent to Server%d, Request => (%+v) \n" , time.Now().Format(time.StampMilli), rf.me, rf.currentTerm, rf.myState.String(), server, args)
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)

	// Protocol: As always, if this server's term is lagging, update the term. 
	// Protocol: If the current system thinks they are a Leader or Candidate (but is in the wrong term), set them to Follower state. 
	if (reply.Term > rf.currentTerm) {
		rf.currentTerm = reply.Term
		rf.votedFor = -1

		if (rf.myState == Leader)  {
			//Transition from Leader to Follower: reset electionTimer
			rf.myState = Follower
			rf.electionTimer.Reset(getElectionTimeout())
			rf.heartbeatTimer.Stop()
		} else if (rf.myState == Candidate) {
			rf.myState = Follower
			rf.electionTimer.Reset(getElectionTimeout())
		}
	}

	return ok
}




//
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {

	// Initialize variables
	var index int
	var term int
	var isLeader bool
	
	// Not Leader: Reject the client request. 
	if (rf.myState != Leader) {
		index = -1
		term = -1
		isLeader = false
	// If Leader: Process the client request. 
	// Protocol: Append request to log as new entry. 
	} else {

		newLog := RaftLog{
			Term: rf.currentTerm, 
			Command: command}

		rf.log = append(rf.log, newLog)

		index = len(rf.log)
		term = rf.currentTerm
		isLeader = true

		// Protocol: Initiate the replication process (asynchronous)
		// Note: We use a buffered channel to try to 1) keep the client requests ordered and 2) ensure that
		// the Start() function can return immediately (not causing time-out issues)
		// Note: Pass the index of the log entry (allowed since while this server is leader, it will never change it's own log entries)
		rf.serviceClientChan <- index


	}


	return index, term, isLeader
}

//
// the tester calls Kill() when a Raft instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (rf *Raft) Kill() {
	// Your code here, if desired.
}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	
	// INITIALIZE VOLATILE STATES //
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Initialize volatile states (variables described by Figure2)
	rf.commitIndex = 0
	rf.lastApplied = 0

	//Initialize raft states (variables created for supplemental use)
	rf.voteCount = 0
	//Determine votes needed for a majority. (Use implicit truncation of integers in divsion to get correct result)
	rf.majority = 1 + len(rf.peers)/2
	// Protocol: Initialize all new servers (initializes for the first time or after crash) in a follower state. 
	rf.myState = Follower

	//TIMERS and CHANNELS//
	//Create election timeout timer
	//TODO: Do I need to close this timer?
	rf.electionTimer = time.NewTimer(getElectionTimeout())
	//Create heartbeat timer. Make sure it's stopped. 
	rf.heartbeatTimer = time.NewTimer(time.Millisecond * 50)
	rf.heartbeatTimer.Stop()
	//Create channel to synchronize log entries by handling incoming client requests. 
	//Channel is buffered so that we can handle/order 256 client requests simultaneously. 
	//TODO: Implement technique that doesn't limit how many client requests we can handle simultaneously. 
	rf.serviceClientChan = make(chan int, 256)


	// INITIALIZE PERSISTANT STATES //
	if (persister.RaftStateSize() > 0) {
		rf.readPersist(persister.ReadRaftState())
	} else {
	// initialize to base state if nothing stored in persistant memory
		// Protocol: Initialize current term to 0 on first boot
		rf.currentTerm = 0
		rf.votedFor = -1
	}


	go rf.manageRaftInterrupts()


	return rf
}

// SUPPORTING FUNCTIONS

// Returns a new election timeout duration between 150ms and 300ms
func getElectionTimeout() time.Duration {

	randSource := rand.NewSource(time.Now().UnixNano())
    r := rand.New(randSource)
	// Create random number between 150 and 300
	seedTime := (r.Float32() * float32(150)) + float32(150)
	newElectionTimeout := time.Duration(seedTime) * time.Millisecond
	return newElectionTimeout

}
