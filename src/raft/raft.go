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
import "math"

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
				fmt.Printf("TheLog: %v \n", rf.log)

				// Sidney: If server believes itself to be the leader and sends AppendEntries, turn-on/reset heartbeat timer. 
				rf.heartbeatTimer.Reset(time.Millisecond * 50)


				// Protocol: Make new log consistent by sending AppendEntry RPC to all servers. 
				for thisServer := 0; thisServer < len(rf.peers); thisServer++ {
					if (thisServer != rf.me) {
						// Note: Implement Go routine to call all AppendEntries requests seperately. 
						go func(server int) {
							// Note: While loop executes until server's log is at least as up to date as the logIndex.
							// Note: Only can send these AppendEntries if server is the leader
							for (logIndex >= rf.nextIndex[server]) && (rf.myState == Leader) {


								// Protocol: Create a go routine for a server that doesn't complete until that server is
								//  up-to-date as to the logIndex of the log of the leader. 
								// Note: Go routine will have access to updated rf raft structure. 
								rf.updateFollowerLogs(server)

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
			fmt.Printf("TheLog: %v \n", rf.log)

			// Sidney: If server believes itself to be the leader, turn on heartbeat timer. 
			rf.heartbeatTimer.Reset(time.Millisecond * 50)

			// Protocol: If server believes to be the leader, sends heartbeats to all peer servers. 
			// Note: The heartbeat will try to update the follower. Heartbeat sends a single update, and does not iterate until follower is updated. 
			// The goal is just to assert control as leader. 
			for thisServer := 0; thisServer < len(rf.peers); thisServer++ {
				if (thisServer != rf.me) {

					// Note: Go routine will have access to updated rf raft structure. 
					go rf.updateFollowerLogs(thisServer)
				}
			}


		// Handles election timeout interrupt: Starts an election
		case currentTime := <- rf.electionTimer.C: 

			// Note: I need to check that server is not the leader in case of the following order of operations => electionTimer timeout
			// to change to Leader to handle this election timerINterrupt
			if (rf.myState != Leader) {
			

				// Protocol: Since the election timeout elapsed, start an election. 
				// Protocol: To indicate that this server started an election, switch to candidate state, and reset the votes. 
				rf.myState = Candidate
				// Protocol: For each new election, increment the servers current term.
				rf.currentTerm += 1
				// Vote for yourself
				rf.votedFor = rf.me
				var voteCount_mu = &sync.Mutex{}
				var voteCount int = 1

				fmt.Printf("%s, Server%d, Term%d, State: %s, Action: Election Time Interrupt \n", currentTime.Format(time.StampMilli), rf.me, rf.currentTerm, rf.myState.String())


				// Protocol: Reset election timer (in case we have split brain issues.)
				rf.electionTimer.Reset(getElectionTimeout())

				// Protocol: Send a RequestVote RPC to all peers. 
				for thisServer := 0; thisServer < len(rf.peers); thisServer++ {
					//Send a RequestVote RPC to all Raft servers (accept our own)
					if (thisServer != rf.me) {
						// Important: Need to use anonymous function implementation so that: We can run multiple requests in parallel
						go func(server int) {

							// Document if the vote was or was not granted.
							voteGranted := rf.getVotes(server)

							var voteCount_temp int
							if (voteGranted) {
								voteCount_mu.Lock()
								voteCount = voteCount +1
								voteCount_temp = voteCount
								voteCount_mu.Unlock()

							}

							// Decide election
						 	if (voteCount_temp >= rf.majority) {
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
						}(thisServer)
					}
					
				}
			}

		default: 

		}
	}

}

// Collects submitted votes, and determine election result. 
func (rf *Raft) getVotes(server int) bool  {

	//Return Variable: defaults to false
	var myvoteGranted bool = false

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

	var reply RequestVoteReply
	msg_received := rf.sendRequestVote(server, args, &reply)

	// If we don't get a message back, then we cannot trust the data in reply. 
	if (msg_received) {
		// A server only accepts votes if they believe they are a candidate. Important because might accumulate votes as
		// follower through delayed RPCs.
		if (rf.myState == Candidate) {
			//Count votes
			if (reply.VoteGranted) {
		 		myvoteGranted = true
		 	} else {
		 		myvoteGranted = false
		 	}
	 	}
 	}

 	return myvoteGranted
}

// Logic to update the log of the follower
func (rf *Raft) processAppendEntryRequest(args AppendEntriesArgs) (success bool) {
	fmt.Printf("%s, Server%d, Term%d, State: %s, Action: processAppendEntryRequest, Log => %v \n", time.Now().Format(time.StampMilli), rf.me, rf.currentTerm, rf.myState.String(),  rf.log)
	fmt.Printf("Entries in AppendEntries RPC in processAppendEntryRequest: %v \n", args.Entries)

	// Handle case where we reached the end of the log.
	var myPrevLogTerm int
	if (args.PrevLogIndex == 0) {
		myPrevLogTerm = -1
	} else {
		myPrevLogTerm = rf.log[args.PrevLogIndex-1].Term
	}


	// Protocol: Determine if the previous entry has the same term an index
	// Protocol: If it doesn't, return false. 
	if (myPrevLogTerm != args.PrevLogTerm) {
		success = false
	// Protocol: If the previous log entry has the same term/index, update this server's log
	} else {

		// Protocol: Update this server with the correct logs and commitIndex. 
		success = true

		// Protocol: If index/term are different in sent log (rf.Entries), remove that index and all other 
		// log entries after that index from follower.
		// Note: Need to do this since these Append Entries RPCs might come out of order, 
		// and we don't want to undo anything that's commited. 
		// entriesIDifferent: Array index at which entry should be copied over to rf.log
		var entriesIDifferent int = 0
		for i,v := range(args.Entries) {
			// Handle case where the next rf.log doesn't exist. 
			// Note: if (max index of rf.log) is <= (the index we plan to append), then the index doesn't exist in rf.log. 
			if (len(rf.log)-1 < args.PrevLogIndex+i) {

				entriesIDifferent = i

				break
			// Handle the case where the next rf.log exists but doesn't math 
			} else if(v.Term != rf.log[args.PrevLogIndex+i].Term) {
				entriesIDifferent = i
				logIDifferent := args.PrevLogIndex+i
				rf.log = rf.log[:logIDifferent+1]
				break
			}
		}


		// Protocol: Append any new entries not already in the log. 
		fmt.Printf("Before Update of rf.log processAppendEntryRequest: %v \n", rf.log)
		rf.log = append(rf.log, args.Entries[entriesIDifferent:]...)
		fmt.Printf("After Update of rf.log processAppendEntryRequest: %v \n", rf.log)

		// Protocol: Update on the entries that have been comitted. At this point, rf.log should be up to date. 
		if (args.LeaderCommit > rf.commitIndex) {
			// Protocol: Use the minimum between leaderCommit and current Log's max index since 
			// there is no way to commit entires that are not in log. 
			rf.commitIndex = int(math.Min(float64(args.LeaderCommit), float64(len(rf.log))))
			if (rf.commitIndex != args.LeaderCommit) {
				fmt.Printf("Error: Claimed to have updated the full log of a follower, but didn't \n")
			}
		}

	}
	return
}

// Protocol: Used to send AppendEntries request to each server until the followr server updates. 
// Protocol: Also used for hearbeats
func (rf *Raft) updateFollowerLogs(server int) {
	fmt.Printf("%s, Server%d, Term%d, State: %s, Action: updateFollowerLogs \n", time.Now().Format(time.StampMilli), rf.me, rf.currentTerm, rf.myState.String())
	fmt.Printf("TheLog in updateFollowerLogs: %v \n", rf.log)

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

			rf.checkCommitStatus()

		} else if (!reply.Success) {
			// Protocol: If fail because of log inconsistency, decrement next index by 1. 
			rf.nextIndex[server] = args.PrevLogIndex
		}

	}
}

// Go routine that runs in background committing entries when possible. 
// For optimal efficiency, this should be blocked 
func (rf *Raft) commitLogEntries(applyCh chan ApplyMsg) {

	for  {
		for(rf.commitIndex > rf.lastApplied) {

			rf.lastApplied = rf.lastApplied +1

			msgOut := ApplyMsg{}
			msgOut.Index = rf.lastApplied
			msgOut.Command = rf.log[rf.lastApplied-1].Command
			applyCh <- msgOut
			fmt.Printf("%s, Server%d, Term%d, State: %s, Action: Successful Commit up to lastApplied of %d, log => %v \n", time.Now().Format(time.StampMilli), rf.me, rf.currentTerm, rf.myState.String(), rf.commitIndex, rf.log)
		}

		time.Sleep(time.Millisecond * 100)
	}
}

// Everytime a follower returns successfully from a Append Entries Routine, check if leader can commit additional log entries. 
func (rf *Raft) checkCommitStatus() {

	// Protocol: Only the leader can decide when it's safe to apply a command to the state machine. 
	if (rf.myState == Leader) {

		var tempCommitIndex int = 0

		//Protocol to determine new commitIndex

		// Protocol: Ensure that the N > commitIndex
		// Iterate across each item in log above commitIndex
		for i_log := rf.commitIndex; i_log < len(rf.log); i_log++ {
			var count int = 0
			// Protocol: Ensure that log[N].term == currentTerm
			if(rf.log[i_log].Term == rf.currentTerm){
				// Protocol: Majority of matchIndex[] >= N (i_log+1)
				for _, v_match := range(rf.matchIndex){
					if (v_match >= i_log+1) {
						count++
					}
				}
				// If current commitIndex meets majority standard, then update the tempCommitIndex
				if (count >= rf.majority) {
					tempCommitIndex = i_log + 1
				}
			}

		}

		// Update rf.commitIndex if it has changed from 0. 
		if (tempCommitIndex != 0) {
			rf.commitIndex = tempCommitIndex
			fmt.Printf("%s, Server%d, Term%d, State: %s, Action: Update commitIndex to %d, log => %v \n", time.Now().Format(time.StampMilli), rf.me, rf.currentTerm, rf.myState.String(), rf.commitIndex, rf.log)
		}
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
	// Only can process data in reply if the RPC was delivered
	if(ok) {
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
	}

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

	if(ok) {
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

		fmt.Printf("%s, Server%d, Term%d, State: %s, Action: LEADER RECEIVED NEW START(), New Log Entry => (%v) \n" , time.Now().Format(time.StampMilli), rf.me, rf.currentTerm, rf.myState.String(), newLog)

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

	go rf.commitLogEntries(applyCh)


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
