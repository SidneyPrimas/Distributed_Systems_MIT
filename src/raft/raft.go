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
import "math"
import "fmt"

import "bytes"
import "encoding/gob"

const debug_break = "---------------------------------------------------------------------------------------"

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

func (rf *Raft) stateToString() string {
	theState := rf.myState

	s:=""
    if theState == 3 {
    	s+="Follower"
   	}
   	if theState == 2 {
    	s+="Candidate"
   	}
   	if theState == 1 {
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
	shutdownChan chan int

	majority int
	myState RaftState
	debug int
	heartbeat_len time.Duration

}


// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	 // Needed to maintain appropriate concurrency 
	rf.mu.Lock()
  	defer rf.mu.Unlock()

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

	 w := new(bytes.Buffer)
	 e := gob.NewEncoder(w)
	 e.Encode(rf.currentTerm)
	 e.Encode(rf.votedFor)
	 e.Encode(rf.log)
	 data := w.Bytes()
	 rf.persister.SaveRaftState(data)
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {

	 r := bytes.NewBuffer(data)
	 d := gob.NewDecoder(r)
	 d.Decode(&rf.currentTerm)
	 d.Decode(&rf.votedFor)
	 d.Decode(&rf.log)
}


// Handles interrupts from timers, and clients. 
func (rf *Raft) manageRaftInterrupts() {

	for {
		select {
		// Handles incoming service requests. Incoming requests must be handled in parallel
		case logIndex := <-rf.serviceClientChan:

			 //Lock Handling serviceClientChan
			rf.mu.Lock()
  			
			// Note: Only handle Client Requests when Leader. 
			// Note: This check might be necessary if the serviceClientChan is backlogged, and we switch from a leader to a follower state
			// without this serve failing. Not 100% necessary since we check for Leader again below.
			if (rf.myState == Leader) {
				
				rf.dPrintf1("%s, Server%d, Term%d, State: %s, Action: Leader Begins RPC  consistency routine \n", time.Now().Format(time.StampMilli), rf.me, rf.currentTerm, rf.stateToString())


				// Sidney: If server believes itself to be the leader and sends AppendEntries, turn-on/reset heartbeat timer. 
				rf.heartbeatTimer.Reset(time.Millisecond * rf.heartbeat_len)


				// Protocol: Make new log consistent by sending AppendEntry RPC to all servers. 
				for thisServer := 0; thisServer < len(rf.peers); thisServer++ {
					if (thisServer != rf.me) {
						// Note: Implement Go routine to call all AppendEntries requests seperately. 
						// Note: Since this is an infinite loop, make sure to close this when the other server doesn't respond. 
						go func(server int) {
							// Note: While loop executes until server's log is at least as up to date as the logIndex.
							// Note: Only can send these AppendEntries if server is the leader

						

							var loop bool = true
							// To begin, we assume that the server is functioning. If the server doesn't respond, exit the Go Routine
							var msg_received bool = true
							// To begin, we set temp_term to our current term. On future iterations, we need to make sure that the 
							// received term is in the right term before we act on the data. 
							var temp_term int = rf.currentTerm
							for  loop {

								select {
								case <- rf.shutdownChan:
									return
								default:
									rf.mu.Lock()
									myState_temp := rf.myState
									myNextIndex_temp := rf.nextIndex[server]
									rf.mu.Unlock()

									// Critical: These state checks (to make sure the servers state has not changed) need to be made 
									// here since 1) we just recieved the lock (so other threads could be running in between), 
									// and 2) we will use this data to make permenant state changes to our system. 
									if ((logIndex >= myNextIndex_temp) && (myState_temp == Leader) && (msg_received)  && (rf.currentTerm == temp_term)) {

									// Protocol: Create a go routine for a server that doesn't complete until that server is
									//  up-to-date as to the logIndex of the log of the leader. 
									// Note: Go routine will have access to updated rf raft structure. 
									msg_received, temp_term = rf.updateFollowerLogs(server)


									// When the if statement isn't satisfied, exit the while loop
									} else {
										loop = false
									}
								}

							}
							
						}(thisServer)
					}
				}
			}

			//Unlock Handling serviceClientChan
			rf.mu.Unlock()

		// Sends hearbeats. 
		// A server only sends hearbeats if they believe to be leader. 
		case   <- rf.heartbeatTimer.C: 
			//Lock_select_hearbeat
			rf.mu.Lock()

			//Error Checking
			if (rf.myState == Leader) {
			
				rf.dPrintf1("Server%d, Term%d, State: %s, Action: Send out heartbeat, log: not included \n", rf.me, rf.currentTerm, rf.stateToString())
				

				// Sidney: If server believes itself to be the leader, turn on heartbeat timer. 
				rf.heartbeatTimer.Reset(time.Millisecond * rf.heartbeat_len)

				// Protocol: If server believes to be the leader, sends heartbeats to all peer servers. 
				// Note: The heartbeat will try to update the follower. Heartbeat sends a single update, and does not iterate until follower is updated. 
				// The goal is just to assert control as leader. 
				for thisServer := 0; thisServer < len(rf.peers); thisServer++ {
					if (thisServer != rf.me) {

						// Note: Go routine will have access to updated rf raft structure. 

						go func(server int) {
							rf.updateFollowerLogs(server)
						}(thisServer)

						
					}
				}
			}
			//Lock_select_hearbeat
			rf.mu.Unlock()

		// Handles election timeout interrupt: Starts an election
		case <- rf.electionTimer.C: 

			//Lock_select_election_timer
			rf.mu.Lock()

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
				rf.persist()
				// Note: the voteCount_mu lock is technically not needed (since the rf.mu should block racy cahnges to voteCount)
				// We include it for extra saftey. 
				var voteCount_mu = &sync.Mutex{}
				var voteCount int = 1

				rf.dPrintf1("Server%d, Term%d, State: %s, Action: Election Time Interrupt. Vote count at %d. \n", rf.me, rf.currentTerm, rf.stateToString(), voteCount)


				// Protocol: Reset election timer (in case we have split brain issues.)
				rf.electionTimer.Reset(getElectionTimeout())

				// Protocol: Send a RequestVote RPC to all peers. 
				for thisServer := 0; thisServer < len(rf.peers); thisServer++ {
					//Send a RequestVote RPC to all Raft servers (accept our own)
					if (thisServer != rf.me) {
						// Important: Need to use anonymous function implementation so that: We can run multiple requests in parallel
						go func(server int) {
							
							
							
							// Document if the vote was or was not granted.
							voteGranted, msg_received, returnedTerm := rf.getVotes(server)
							//Lock_select_votes
							rf.mu.Lock()
							defer rf.mu.Unlock()

							// Critical: These state checks (to make sure the servers state has not changed) need to be made 
							// here since 1) we just recieved the lock (so other threads could be running in between), 
							// and 2) we will use this data to make permenant state changes to our system. 
							var voteCount_temp int
							if (voteGranted && msg_received && (rf.myState == Candidate) && (rf.currentTerm == returnedTerm)) {
								voteCount_mu.Lock()
								voteCount = voteCount +1
								voteCount_temp = voteCount
								rf.dPrintf1("Server%d, Term%d, State: %s ,Action: Now have a total of %d votes. \n",rf.me, rf.currentTerm, rf.stateToString(), voteCount)
								voteCount_mu.Unlock()	

							}

							// Decide election
						 	if ((voteCount_temp >= rf.majority) && (rf.myState == Candidate)){
						 		rf.dPrintf1("%s \n Server%d, Term%d, State: %s, Action: Elected New Leader, votes: %d\n", debug_break, rf.me, rf.currentTerm, rf.stateToString(), voteCount_temp)
						 		// Protocol: Transition to leader state. 
						 		rf.myState = Leader
						 		rf.electionTimer.Stop()
						 		rf.heartbeatTimer.Reset(time.Millisecond * rf.heartbeat_len)

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

			//Lock_select_election_timer
			rf.mu.Unlock()
		case <-rf.shutdownChan:
  			//quit: Handle leqcky goRoutines
  			return

		}
	}

}

// Collects submitted votes, and determine election result. 
func (rf *Raft) getVotes(server int)  (myvoteGranted bool, msg_received bool, returnedTerm int)  {

	//Lock_getVotes
	rf.mu.Lock()

	//Return Variable: defaults to false
	myvoteGranted  = false

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

	//Lock_getVotes
	rf.mu.Unlock()


	var reply RequestVoteReply
	msg_received = rf.sendRequestVote(server, args, &reply)
	//Deferred lock for concurrency
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// If we don't get a message back, then we cannot trust the data in reply. 
	if (msg_received && (reply.Term == rf.currentTerm)) {
		// A server only accepts votes if they believe they are a candidate. Important because might accumulate votes as
		// follower through delayed RPCs.
		if (rf.myState == Candidate) {
			//Count votes
			myvoteGranted = reply.VoteGranted

	 	}
 	}


 	returnedTerm = reply.Term

 	return myvoteGranted, msg_received, returnedTerm
}

// Logic to update the log of the follower
func (rf *Raft) processAppendEntryRequest(args AppendEntriesArgs, reply *AppendEntriesReply)  {

	rf.dPrintf1("%s, Server%d, Term%d, State: %s, Action: Append RPC Request, args => %v, rf.Log => %v , Entries => %v \n", time.Now().Format(time.StampMilli), rf.me, rf.currentTerm, rf.stateToString(),  args, rf.log, args.Entries)



	// Handle out of index edge cases: out of range of log[i]
	// myPrevLogTerm is the term of the log at args.prevIndex (this is what we are comparing to the leader)
	var myPrevLogTerm int
	// out_of_index: Indicates that rf.log (the followers log) doesn't have an index at prevIndex from Leader. 
	var out_of_index bool = false
	// Handles the case where the leader sends over the entire log. In this case, server sends Entries starting at Index1. 
	// Thus, the prevIndex is at 0. At 0, we need to create an artifically -1 Term. 
	// In this case, the append is guaranteed ot be a lucces
	if (args.PrevLogIndex == 0) {
		myPrevLogTerm = -1
	//Handles edge case when follower log is so far behind, and doesn't have an entry
	} else if (len(rf.log) < args.PrevLogIndex) {
		out_of_index = true
	} else {
		myPrevLogTerm = rf.log[args.PrevLogIndex-1].Term
	}


	// Conflicting Term: The conflict Term is the term at prevIndex that the leader sent (it's the log we are comparing).
	// Protocol: Determine if the previous entry has the same term an index
	// Protocol: If it doesn't, return false. 
	if (out_of_index) {
		reply.Success = false

		//Handle the case where rf.log has no entries. 
		if len(rf.log) == 0 {
			reply.ConflictingTerm = -1
		} else {
			reply.ConflictingTerm = rf.log[len(rf.log)-1].Term
		}
		
	} else if (myPrevLogTerm != args.PrevLogTerm) {
		reply.ConflictingTerm = myPrevLogTerm
		reply.Success = false
	// Protocol: If the previous log entry has the same term/index, update this server's log
	} else {

		// Protocol: Update this server with the correct logs and commitIndex. 
		reply.Success = true

		// Protocol: If index/term are different in sent log (rf.Entries), remove that index and all other 
		// log entries after that index from follower.
		// Note: Need to do this since these Append Entries RPCs might come out of order, 
		// and we don't want to undo anything that's already written to a log. 
		// entriesIDifferent: Array index at which entry should be copied over to rf.log. 
		// entriesIDifferent: If they aren't different, nothing should be copied over. 
		var entriesIDifferent int = len(args.Entries)
		for i,v := range(args.Entries) {
			// Handle case where the next rf.log doesn't exist. Since the entry doesn't exist, we know it conflicts. 
			// Note: if (max index of rf.log) is <= (the index we plan to append), then the index doesn't exist in rf.log. 
			if (len(rf.log)-1 < args.PrevLogIndex+i) {

				entriesIDifferent = i

				break
			// Handle the case where the next rf.log entry exists but isn't up to date 
			// Protocol: "If an existing entry conflicts with a new one, delete the existing entry". 
			} else if(v.Term != rf.log[args.PrevLogIndex+i].Term) {

				// Identify the conflict
				entriesIDifferent = i
				// Delete the entry at logIDifferent, and all that follow
				logIDifferent := args.PrevLogIndex+i
				// Note: If we remove an entry that has been committed, throw an error. 
				// Note: We remove anything at or above logIDifferent, so throw error when of commitIndex_i is greater or equal. 
				if (logIDifferent <= rf.commitIndex-1) {
					rf.error("Error: Deleting Logs already committed.  \n", );
				}
				rf.log = rf.log[:logIDifferent]
				break
			}
		}

		// Protocol: Append any new entries not already in the log. 
		rf.log = append(rf.log, args.Entries[entriesIDifferent:]...)
		rf.persist()
		

		// Protocol: Update on the entries that have been comitted. At this point, rf.log should be up to date. 
		if (args.LeaderCommit > rf.commitIndex) {
			// Protocol: Use the minimum between leaderCommit and current Log's max index since 
			// there is no way to commit entries that are not in log. 
			rf.commitIndex = int(math.Min(float64(args.LeaderCommit), float64(len(rf.log))))
			if (rf.commitIndex != args.LeaderCommit) {
				rf.error("Error: Claimed to have updated the full log of a follower, but didn't \n")
			}
		}

	}
	return
}

// Protocol: Used to send AppendEntries request to each server until the followr server updates. 
// Protocol: Also used for hearbeats
func (rf *Raft) updateFollowerLogs(server int)  (msg_received bool, returnedTerm int)  {

	//Lock_updateFollowerLogs_beginning
	rf.mu.Lock()

	// Setup outgoing arguments.
	// Protocol: These arguments should be re-initialized for each RPC call since rf might update in the meantime.
	// We want to replicate the leader log everywhere, so we can always send it when the follower is out of date. 
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
	// Important: Make sure to copy arrays to a new array. Otherwise, we just send the pointer, and then can get race conditions.
	entries_temp := rf.log[start_index_sending-1:final_index_sending] 
	entriesToSend := make([]RaftLog, len(entries_temp))
	copy(entriesToSend, entries_temp)
	args.Entries = entriesToSend


	//Lock_updateFollowerLogs_beginning
	rf.mu.Unlock()


	var reply AppendEntriesReply
	msg_received = rf.sendAppendEntries(server, args, &reply)

	//Deferred lock for concurrency issues
	rf.mu.Lock()
	defer rf.mu.Unlock()
	
	// Critical: These state checks (to make sure the servers state has not changed) need to be made 
	// here since 1) we just recieved the lock (so other threads could be running in between), 
	// and 2) we will use this data to make permenant state changes to our system. 
	if (msg_received && (rf.myState == Leader) && (rf.currentTerm == reply.Term)) {


		if (reply.Success) {
			
			// The follower server is now up to date (both the logs and the commit)
			// Protocol: After successful AppendEntries, increase nextIndex for this server to one above the last index
			// sent by the last AppendEntries RPC request. 
			rf.nextIndex[server] =  final_index_sending + 1
			rf.matchIndex[server] = final_index_sending

			rf.dPrintf1("Server%d, Term%d, State: %s, Action: Update Match Index matchIndex => %v \n", rf.me, rf.currentTerm, rf.stateToString(), rf.matchIndex)


			rf.checkCommitStatus(&reply)

		} else if (!reply.Success) {
			// Protocol: If fail because of log inconsistency, decrement next index by 1. 
			rf.dPrintf1("ConflictingTerm: %d \n", reply.ConflictingTerm)

			// Default of myNextIndex: Since the AppendLog wasn't successful, in the non-optimized case, I now send the preveiou log. 
			// In the optimized case, I send the first log with Term of ConflictingTerm
			var myNextIndex int = args.PrevLogIndex
			
			// Since we are sending args.PrevLogIndex, we should iterate up to that log entry but not more. 
			for i := 0; i <= args.PrevLogIndex-1; i++ {

				//Traverse through Leaders log and find first Log entry with the ConflictingTerm
				if(rf.log[i].Term == reply.ConflictingTerm) {
					// i is the indice of when they match, which coresponds to an index of i+1
					// This is the Index I want to send, so I set it to myNextIndex
					myNextIndex = i + 1
					break
				}
			}
			// Method to increase speed even faster by sending all logs. 
			myNextIndex = 1
			rf.nextIndex[server] = myNextIndex
			
		}

	}
	//Return if the server is still responding. 
	returnedTerm = reply.Term
	return msg_received, returnedTerm
}

// Go routine that runs in background committing entries when possible. 
// For optimal efficiency, this should be blocked 
func (rf *Raft) commitLogEntries(applyCh chan ApplyMsg) {

	for  {

		select {
		case <-rf.shutdownChan:
  			// Gargabe Collection
  				return
		default:
			// Needed to maintain appropriate concurrency 
			rf.mu.Lock()
			for(rf.commitIndex > rf.lastApplied) {

				rf.lastApplied = rf.lastApplied +1

				msgOut := ApplyMsg{}
				msgOut.Index = rf.lastApplied
				msgOut.Command = rf.log[rf.lastApplied-1].Command
				applyCh <- msgOut
				rf.dPrintf1("Server%d, Term%d, State: %s, Action: Successful Commit up to lastApplied of %d, msgSent => %v \n", rf.me, rf.currentTerm, rf.stateToString(), rf.commitIndex, msgOut)
			}
			rf.mu.Unlock()

			time.Sleep(time.Millisecond * rf.heartbeat_len)
		}
	}
}

// Everytime a follower returns successfully from a Append Entries Routine, check if leader can commit additional log entries. 
//
func (rf *Raft) checkCommitStatus(reply *AppendEntriesReply) {

	// Protocol: Only the leader can decide when it's safe to apply a command to the state machine. 
	// Note: Technically, we do not need to check that term at this point since the checkCommitStatus function
	// is enclosed in a mutual exclusion lock. So, the term should not have been able to change. 
	if (rf.myState == Leader) && (rf.currentTerm == reply.Term) {

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
		if (tempCommitIndex != 0) && (rf.myState == Leader) {
			rf.commitIndex = tempCommitIndex
			rf.dPrintf1("%s, Server%d, Term%d, State: %s, Action: Update commitIndex for Leader to %d, log => %v \n", time.Now().Format(time.StampMilli), rf.me, rf.currentTerm, rf.stateToString(), rf.commitIndex, rf.log)
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
	 // Needed to maintain appropriate concurrency 
	rf.mu.Lock()
  	defer rf.mu.Unlock()

	// Protocol: As always, if this server's term is lagging, update the term. 
	// Protocol: If the current system thinks they are a Leader or Candidate (but is in the wrong term), set them to Follower state. 
	if (args.Term > rf.currentTerm) {
		rf.currentTerm = args.Term
		rf.votedFor = -1
		rf.persist()

		if (rf.myState == Leader)  {
			//Transition from Leader to Follower: reset electionTimer
			rf.dPrintf1("Server%d Stop Being a Leader from RequestVote Method \n %s \n", rf.me, debug_break)
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
		rf.dPrintf1("Server%d, Term%d, Action: Server denies vote to Server%d \n",rf.me, rf.currentTerm, args.CandidateId)
		reply.VoteGranted = false
	// Protocol: Determine if this server should vote for the candidate given that the candidate is in an equal or higher term. 
	// Only grant vote if: 1) candidate's log is at least as up-to-date as receiver's log and 2) this server hasn't voted for somebody else.
	// Setup guarantees that voter is in same term as candidate.
	} else {

		if (args.Term != rf.currentTerm) {
			rf.error("Error: Server is voting, but is not in the same term as candidate.\n");
		}

		if (rf.votedFor == args.CandidateId) {
			rf.error("Error: Voting related issue. This seems not to be possible. \n");
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
			rf.dPrintf1("Server%d, Term%d, Action: Server votes for Server%d \n",rf.me, rf.currentTerm, args.CandidateId)
			reply.VoteGranted = true
			rf.votedFor = args.CandidateId
			rf.persist()
			rf.electionTimer.Reset(getElectionTimeout())
		// Determine if this server can vote for canddiate: When candidates's log term is equal, look at index
		// Protocol: Reset the election timer when granting a vote. 
		} else if (allowedToVote) && (args.LastLogTerm == thisLastLogTerm) && (args.LastLogIndex >= thisLastLogIndex) {
			rf.dPrintf1("Server%d, Term%d, Action: Server votes for Server%d \n",rf.me, rf.currentTerm, args.CandidateId)
			reply.VoteGranted = true
			rf.votedFor = args.CandidateId
			rf.persist()
			rf.electionTimer.Reset(getElectionTimeout())
		// If the above statements are not met, don't vote for this candidate.  
		} else {
			rf.dPrintf1("Server%d, Term%d, Action: Server denies vote to Server%d \n",rf.me, rf.currentTerm, args.CandidateId)
			reply.VoteGranted = false
		}
	}

	reply.Term = rf.currentTerm
	//rf.dPrintf1("%s, Server%d, Term%d, State: %s, Action: Method RequestVote Prcoessed, Reply => (%+v) \n", time.Now().Format(time.StampMilli), rf.me, rf.currentTerm, rf.stateToString(), reply)
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
	//rf.mu.Lock()
	//rf.dPrintf1("%s, Server%d, Term%d, State: %s, Action: Method sendRequestVote sent to Server%d, Request => (%+v) \n" , time.Now().Format(time.StampMilli), rf.me, rf.currentTerm, rf.stateToString(), server, args)
	//rf.mu.Unlock()
	



	RPC_returned := make(chan bool)
	myState_temp :=rf.getLockedState()
	if (myState_temp == Candidate) {

		go func(goArgs RequestVoteArgs, goReply *RequestVoteReply,output chan bool) {
			ok := rf.peers[server].Call("Raft.RequestVote", goArgs, goReply)
			output <- ok
		}(args, reply, RPC_returned)

	}

	//Allows for RPC Timeout
	var ok bool = false
	select {
	case <-time.After(time.Millisecond * 50):
	  	ok = false
	case ok = <-RPC_returned:
	
		 // Needed to maintain appropriate concurrency 
		rf.mu.Lock()
	  	defer rf.mu.Unlock()


		// Critical: These state checks (to make sure the servers state has not changed) need to be made 
		// here since 1) we just recieved the lock (so other threads could be running in between), 
		// and 2) we will use this data to make permenant state changes to our system. 
		if(ok && (rf.myState == Candidate) && (rf.currentTerm == reply.Term)) {
			// Protocol: As always, if this server's term is lagging, update the term. 
			// Protocol: If the current system thinks they are a Leader or Candidate (but is in the wrong term), set them to Follower state. 
			if (reply.Term > rf.currentTerm) {
				rf.currentTerm = reply.Term
				rf.votedFor = -1
				rf.persist()

				if (rf.myState == Leader)  {
					//Transition from Leader to Follower: reset electionTimer
					rf.myState = Follower
					rf.dPrintf1("Server%d Stop Being a Leader from sendRequestVote Method \n %s \n", rf.me, debug_break)
					rf.electionTimer.Reset(getElectionTimeout())
					rf.heartbeatTimer.Stop()
				} else if (rf.myState == Candidate) {
					rf.myState = Follower
					rf.electionTimer.Reset(getElectionTimeout())
				}
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
	Term 				int
	ConflictingTerm		int
	Success 			bool

}

//
// Sidney: Function handles communication between Raft instances to synchronize on logs. 
// Sidney: Functions handles as an indication of a heartbeat
//
func (rf *Raft) AppendEntries(args AppendEntriesArgs, reply *AppendEntriesReply) {
	// Needed to maintain appropriate concurrency 
	rf.mu.Lock()
  	defer rf.mu.Unlock()

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
			rf.persist()


			if (rf.myState == Leader)  {
				//Transition from Leader to Follower: reset electionTimer
				rf.myState = Follower
				rf.dPrintf1("Server%d Stop Being a Leader from AppendEntries Method \n %s \n", rf.me, debug_break)
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
				rf.error("Error: Two leaders have been selected in the same term. \n")
			}
		}

		// ONCE THE TERM/STATE ARE UPDATED, HANDLE THE APPEND ENTRIES REQUEST
		rf.processAppendEntryRequest(args, reply)

	}

	reply.Term = rf.currentTerm

}

//
// Sidney: Function sends an outgoing RPC request from master to append entries to the logs of the other Raft instances. 
// Sidney: Only the leader sends this RPC
//
// returns true if labrpc says the RPC was delivered.
//
func (rf *Raft) sendAppendEntries(server int, args AppendEntriesArgs, reply *AppendEntriesReply) bool {

	// ToDo: Remove comment since this comment might not be the real state of the system when we actually send the RPC. 
	rf.mu.Lock()
	rf.dPrintf1("Server%d, Term%d, State: %s, Action: Leader send AppendEntry RPC  to Server%d \n" ,rf.me, rf.currentTerm, rf.stateToString(), server)
	rf.mu.Unlock()




	myState_temp :=rf.getLockedState()
	RPC_returned := make(chan bool)
	if (myState_temp == Leader) {
		
		go func(goArgs AppendEntriesArgs, goReply *AppendEntriesReply,output chan bool) {
			ok := rf.peers[server].Call("Raft.AppendEntries", goArgs, goReply)
			output <- ok
		}(args, reply, RPC_returned)
	}

	//Allows for RPC Timeout
	var ok bool = false
	select {
	case <-time.After(time.Millisecond * 50):
	  	ok = false
	case ok = <-RPC_returned:
		
		// Needed to maintain appropriate concurrency 
		rf.mu.Lock()
	  	defer rf.mu.Unlock()
	  	

		if(ok && (rf.myState == Leader) && (rf.currentTerm == reply.Term)) {
			// Protocol: As always, if this server's term is lagging, update the term. 
			// Protocol: If the current system thinks they are a Leader or Candidate (but is in the wrong term), set them to Follower state. 
			if (reply.Term > rf.currentTerm) {
				rf.currentTerm = reply.Term
				rf.votedFor = -1
				rf.persist()

				if (rf.myState == Leader)  {
					rf.dPrintf1("Server%d Stop Being a Leader from sendAppendEntries method\n %s \n", rf.me, debug_break)
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
	// Needed to maintain appropriate concurrency 
	rf.mu.Lock()
  	defer rf.mu.Unlock()

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

		rf.dPrintf1("%s \n Server%d, Term%d, State: %s, Action: LEADER RECEIVED NEW START(), New Log Entry => (%v) \n" ,  debug_break, rf.me, rf.currentTerm, rf.stateToString(), newLog)


		rf.log = append(rf.log, newLog)
		rf.persist()
		rf.matchIndex[rf.me] = len(rf.log)

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
	rf.dPrintf1("Server%d, Term%d, State: %s, Action: SERVER Dies \n %s \n" ,  rf.me, rf.currentTerm, rf.stateToString(), debug_break)
	close(rf.shutdownChan)
	close(rf.serviceClientChan)
	rf.heartbeatTimer.Stop()
	rf.electionTimer.Stop()

	rf.debug = -1
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
	rf.mu = sync.Mutex{}
	rf.debug = -1
	// Needed to maintain appropriate concurrency 
	// Note: This case probably not necessary, but included for saftey 
	rf.mu.Lock()
  	defer rf.mu.Unlock()

	// Initialize volatile states (variables described by Figure2)
	rf.commitIndex = 0
	rf.lastApplied = 0

	rf.heartbeat_len = 50
	//Determine votes needed for a majority. (Use implicit truncation of integers in divsion to get correct result)
	rf.majority = 1 + len(rf.peers)/2
	// Protocol: Initialize all new servers (initializes for the first time or after crash) in a follower state. 
	rf.myState = Follower

	//TIMERS and CHANNELS//
	//Create election timeout timer
	rf.electionTimer = time.NewTimer(getElectionTimeout())
	//Create heartbeat timer. Make sure it's stopped. 
	rf.heartbeatTimer = time.NewTimer(time.Millisecond * rf.heartbeat_len)
	rf.heartbeatTimer.Stop()
	//Create channel to synchronize log entries by handling incoming client requests. 
	//Channel is buffered so that we can handle/order 256 client requests simultaneously. 
	//TODO: Implement technique that doesn't limit how many client requests we can handle simultaneously. 
	rf.serviceClientChan = make(chan int, 512)

	rf.shutdownChan = make(chan int)



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
	seedTime := (r.Float32() * float32(100)) + float32(150)
	newElectionTimeout := time.Duration(seedTime) * time.Millisecond
	return newElectionTimeout

}

func (rf *Raft)getLockedState() (raftState_temp RaftState) {
	
	rf.mu.Lock()
	raftState_temp = rf.myState
	rf.mu.Unlock()

	return raftState_temp
}

func (rf *Raft) error(format string, a ...interface{}) (n int, err error) {
	if rf.debug >= 0 {
		fmt.Printf(format, a...)
	}
	return
}

func (rf *Raft) dPrintf1(format string, a ...interface{}) (n int, err error) {
	if rf.debug >= 1 {
		fmt.Printf(format, a...)
	}
	return
}

func (rf *Raft) dPrintf2(format string, a ...interface{}) (n int, err error) {
	if rf.debug >= 2 {
		fmt.Printf(format, a...)
	}
	return
}
