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
import "log"

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
	Term 		int
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
	applyCh chan ApplyMsg

	majority int
	myState RaftState
	debug int
	heartbeat_len time.Duration

	// Snapshot Related Variavles
	lastIncludedIndex 	int
	lastIncludedTerm	int
	snapshotData 		[]byte


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
	 e.Encode(rf.lastIncludedIndex)
	 e.Encode(rf.lastIncludedTerm)
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
	 d.Decode(&rf.lastIncludedIndex)
	 d.Decode(&rf.lastIncludedTerm)
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

						// Protocol: Create a go routine for a server that doesn't complete until that server is
						//  up-to-date as to the logIndex of the log of the leader. 
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
									lastIncludedIndex_temp := rf.lastIncludedIndex
									rf.mu.Unlock()

									// Critical: These state checks (to make sure the servers state has not changed) need to be made 
									// here since 1) we just recieved the lock (so other threads could be running in between), 
									// and 2) we will use this data to make permenant state changes to our system. 
									if ((logIndex >= myNextIndex_temp) && (myState_temp == Leader) && (msg_received)  && (rf.currentTerm == temp_term)) {

										// If nextIndex doesn't exists in Log, send a snapshot. 
										// Otherwise, send the log entries directly. 
										if (myNextIndex_temp <= lastIncludedIndex_temp) {

											msg_received, temp_term = rf.updateFollowerState(server)
										} else {
											msg_received, temp_term = rf.updateFollowerLogs(server)
										}

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

			//Make sure still leader.
			if (rf.myState == Leader) {
			
				rf.dPrintf1("Server%d, Term%d, State: %s, Action: Send out heartbeat, log: not included \n", rf.me, rf.currentTerm, rf.stateToString())
				

				// If server believes itself to be the leader, turn on heartbeat timer. 
				rf.heartbeatTimer.Reset(time.Millisecond * rf.heartbeat_len)

				// Protocol: If server believes to be the leader, sends heartbeats to all peer servers. 
				// Note: The heartbeat will try to update the follower. Heartbeat sends a single update, and does not iterate until follower is updated. 
				// The goal is just to assert control as leader. 
				for thisServer := 0; thisServer < len(rf.peers); thisServer++ {
					if (thisServer != rf.me) {

						// Note: Go routine will have access to updated rf raft structure. 
						go func(server int) {

							rf.mu.Lock()
							myNextIndex_temp := rf.nextIndex[server]
							lastIncludedIndex_temp := rf.lastIncludedIndex
							rf.mu.Unlock()

							// If nextIndex doesn't exists in Log, send a snapshot. 
							// Otherwise, send the log entries directly. 
							if (myNextIndex_temp <= lastIncludedIndex_temp) {
								rf.updateFollowerState(server)
							} else {
								rf.updateFollowerLogs(server)
							}

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
							if (voteGranted && msg_received && (rf.myState == Candidate) && (rf.currentTerm == returnedTerm)) {
								voteCount = voteCount +1
								rf.dPrintf1("Server%d, Term%d, State: %s ,Action: Now have a total of %d votes. \n",rf.me, rf.currentTerm, rf.stateToString(), voteCount)
							}

							// Decide election
						 	if ((voteCount >= rf.majority) && (rf.myState == Candidate)){
						 		rf.dPrintf1("%s \n Server%d, Term%d, State: %s, Action: Elected New Leader, votes: %d\n", debug_break, rf.me, rf.currentTerm, rf.stateToString(), voteCount)
						 		// Protocol: Transition to leader state. 
						 		rf.myState = Leader
						 		rf.electionTimer.Stop()
						 		rf.heartbeatTimer.Reset(time.Millisecond * rf.heartbeat_len)

						 		// Initialize leader specific variables. 
						 		rf.nextIndex = make([]int, len(rf.peers))
						 		rf.matchIndex =  make([]int, len(rf.peers))
						 		for i := range(rf.nextIndex) {

						 			rf.nextIndex[i] = rf.realLogLength() + 1
						 			// Need to initialize to 0 (not rf.lastIncludedIndex) because we don't know which other servers 
						 			// have applied up to this point on the snapshot. Other servers might be lagging. 

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
		LastLogIndex: rf.realLogLength()}

	// If tree to ensure correct first-time LastLogTerm initialization 
	if rf.realLogLength() == 0 {
		args.LastLogTerm = -1
	} else {
		args.LastLogTerm = rf.getLogEntry(rf.realLogLength()-1).Term
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

	rf.dPrintf1("Server%d, Term%d, State: %s, Action: Append RPC Request, args => %+v, rf.Log => %+v, rf.lastApllied => %d, rf.commitIndex => %d  \n", rf.me, rf.currentTerm, rf.stateToString(),  args, rf.log, rf.lastApplied, rf.commitIndex)

	// Default reply for ConflictIndex. 
	reply.ConflictingIndex = 1 // Except if we explicitly set ConflictIndex (due to false replySuccess), it won't be used by Leader. 



	// Protocol (2A): Determine if the previous entry has the same term and index. If it doesn't, return false. 
	// If Leader's PrevLogIndex is greater than highest index in Follower's log, the index cannot be the same. 
	if  (args.PrevLogIndex > rf.realLogLength()) {
		reply.Success = false

		// If the log is empty, set the ConlictIndex to the first available log entry. 
		// Otherwise, set it to the last log entry.
		if (len(rf.log) == 0) {
			reply.ConflictingIndex = rf.lastIncludedIndex + 1
		} else {
			reply.ConflictingIndex = rf.realLogLength()
		}

		// Protocol: Immediately reply false if the PrevLogIndex doesn't exist in the log of this folloer. 
		return 
		
	}

	// Handle out of index edge cases: out of range of log[i]
	// myPrevLogTerm is the term of the log at args.prevIndex for this follower (this is what we are comparing to the leader)
	// Note: Due to return statement, can only get to this part of code if len(rf.log) >= args.PrevLogIndex.
	myPrevLogTerm := rf.getConsistencyTerm(args.PrevLogIndex-1)


	// Protocol (2B): If the Follower doesn't have the same term (as PrevLogTerm) at the same index (as PrevLogIndex), then they don't match.
	if (myPrevLogTerm != args.PrevLogTerm) {
		reply.Success = false

		reply.ConflictingIndex = rf.getFirstIndexInTerm(args.PrevLogIndex, myPrevLogTerm)

		// Protocol: Immediately reply false if the PrevLogIndex doesn't exist in the log of this folloer. 
		return
	}

	// Protocol (3-5): APPEND Entry is a succuess. The PrevLogIndex and PrevLogTerm are equal in the follower's Log. 
	// Protocol: In steps 3-5, update this server with the correct logs and commitIndex. 
	// Note: Due to return statements above, only get to this part of code if AppendEntry is a success.
	reply.Success = true

	// Protocol (3): If index/term are different in sent log (rf.Entries), remove that index and all other 
	// log entries after that index from follower.
	// Note: Need to do this since these Append Entries RPCs might come out of order, 
	// and we don't want to undo anything that's already written to a log. 
	// entriesIDifferent: Array index at which entry should be copied over to rf.log. 
	// entriesIDifferent: If they aren't different, nothing should be copied over. 
	var entriesIDifferent int = len(args.Entries)
	for i,v := range(args.Entries) {
		// Handle case where the next rf.log doesn't exist. Since the entry doesn't exist, we know it conflicts. 
		// Note: if (max index of rf.log) is <= (the index we plan to append), then the index doesn't exist in rf.log. 
		if (rf.realLogLength()-1 < args.PrevLogIndex+i) {

			entriesIDifferent = i

			break
		// Handle the case where the next rf.log entry exists but isn't up to date 
		// Protocol: "If an existing entry conflicts with a new one, delete the existing entry". 
		} else if(v.Term != rf.getLogEntry(args.PrevLogIndex+i).Term) {

			// Identify the conflict
			entriesIDifferent = i
			// Delete the entry at logIDifferent, and all that follow
			logIDifferent := args.PrevLogIndex+i
			// Note: If we remove an entry that has been committed, throw an error. 
			// Note: We remove anything at or above logIDifferent, so throw error when of commitIndex_i is greater or equal. 
			if (logIDifferent <= rf.commitIndex-1) {
				rf.error("Error: Deleting Logs already committed.  \n", );
			}
			rf.log = rf.log[:rf.getSnapIndex(logIDifferent)]
			rf.persist()
			break
		}
	}

	// Protocol (4): Append any new entries not already in the log. 
	rf.log = append(rf.log, args.Entries[entriesIDifferent:]...)
	rf.persist()
	

	// Protocol (5): Update on the entries that have been comitted. At this point, rf.log should be up to date. 
	if (args.LeaderCommit > rf.commitIndex) {
		// Protocol: Use the minimum between leaderCommit and current Log's max index since 
		// there is no way to commit entries that are not in log. 
		rf.commitIndex = int(math.Min(float64(args.LeaderCommit), float64(rf.realLogLength())))
		if (rf.commitIndex != args.LeaderCommit) {
			rf.error("Error: Claimed to have updated the full log of a follower, but didn't \n")
		}
	}

	return
}


func (rf *Raft) getFirstIndexInTerm(initialIndex int, conflictingTerm int) (firstIndex int) {

	if (conflictingTerm == -1) {
		rf.error("Error in getFirstIndexInTerm: When the follower's Term at args.PrevLogIndex is -1 (meaning a non-existent element in the log), the AppedEntries func should always be successful. \n")
	}

	// From the point in the log where the terms conflict (initialIndex), decrease the index until 
	// 1) the begginging of the log (even after snapshotted) or 2) the term changes to a lower term.
	firstIndex = initialIndex
	for firstIndex > rf.lastIncludedIndex &&
			conflictingTerm <= rf.getLogEntry(firstIndex-1).Term  {
		firstIndex--
	}

	// We want the last index of a term. 
	firstIndex++

	if (rf.indexNotInLog(firstIndex)) {
		rf.error("Error in getFirstIndexInTerm: The logs can only be roled back to the last log entry after truncation.");
	}

	return firstIndex
}

// Protocol: Used to send AppendEntries request to each server until the followr server updates. 
// Protocol: Also used for hearbeats
func (rf *Raft) updateFollowerLogs(server int)  (msg_received bool, returnedTerm int)  {

	//Lock_updateFollowerLogs_beginning
	rf.mu.Lock()
	rf.dPrintf1("Server%d, Term%d, State: %s, Action: Leader send AppendEntry RPC  to Server%d. matchIndex => %v, nextIndex => %v \n" ,rf.me, rf.currentTerm, rf.stateToString(), server, rf.matchIndex, rf.nextIndex)

	// Setup outgoing arguments.
	// Protocol: These arguments should be re-initialized for each RPC call since rf might update in the meantime.
	// We want to replicate the leader log everywhere, so we can always send it when the follower is out of date. 
	start_index_sending := rf.nextIndex[server]
	final_index_sending := rf.realLogLength()

	args := AppendEntriesArgs{
		Term: rf.currentTerm, 
		LeaderId: rf.me, 
		// Index of log entry immediately preceding the new one
		PrevLogIndex: start_index_sending-1,  
		// Fine to update commit index since if follower replies successfully, the follower log will be as up to date
		// as leader, and thus can commit as much as the leader. 
		LeaderCommit: rf.commitIndex}

	// Get PrevLogTerm (even when log truncated or Term is -1)
	args.PrevLogTerm = rf.getConsistencyTerm(args.PrevLogIndex-1)


	// Protocol: The leader should send all logs from requested index, and upwards. 
	// Important: Make sure to copy arrays to a new array. Otherwise, we just send the pointer, and then can get race conditions.
	entries_temp := rf.log[rf.getSnapIndex(start_index_sending)-1 : rf.getSnapIndex(final_index_sending)] 
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
			// Note: MatchIndex should monitonically increase. If it doesn't (due to a reordered RPC), dont' make changes.
			if (rf.matchIndex[server] <= final_index_sending) {
				rf.nextIndex[server] =  final_index_sending + 1
				rf.matchIndex[server] = final_index_sending

			// Error checking. matchIndex can only increase. 
			} else if (rf.matchIndex[server] > final_index_sending) {
				rf.dPrintf2("Server%d, Term%d, State: %s, Action: Ignored updating matchIndex for Server%d. Received old matchIndex matchIndex => %v, final_index_sending => %d \n", rf.me, rf.currentTerm, rf.stateToString(), server, rf.matchIndex, final_index_sending)
			}

			rf.dPrintf1("Server%d, Term%d, State: %s, Action: Update Match Index matchIndex => %v \n", rf.me, rf.currentTerm, rf.stateToString(), rf.matchIndex)


			rf.checkCommitStatus(&reply)

		} else if (!reply.Success) {
			
			rf.dPrintf1("Server%d, Term%d, State: %s, Action: Optimization Update, ConflictingIndex => %v, rf.log => %v \n", rf.me, rf.currentTerm, rf.stateToString(), reply.ConflictingIndex, rf.log)

			rf.dPrintf2("Server%d, Action: Update rf.nextIndex. Old nextIndex => %v", rf.me, rf.nextIndex)
			rf.nextIndex[server] = reply.ConflictingIndex
			rf.dPrintf2("Server%d, Action: Update rf.nextIndex. New nextIndex => %v", rf.me, rf.nextIndex)
			
		}

	}
	//Return if the server is still responding. 
	returnedTerm = reply.Term
	return msg_received, returnedTerm
}

// Protocol: Update follower with snapshot. 
func (rf *Raft) updateFollowerState(server int)  (msg_received bool, returnedTerm int)  {
	
	// SETUP SENDSNAPSHOT VARIABLES
	rf.mu.Lock()
	rf.dPrintf1("Server%d, Term%d, State: %s, Action: Leader send Snapshot RPC to Server%d \n" ,rf.me, rf.currentTerm, rf.stateToString(), server)

	args := InstallSnapshotArgs{
		Term: rf.currentTerm, 
		LeaderId: rf.me, 
		LastIncludedIndex: rf.lastIncludedIndex,  
		LastIncludedTerm: rf.lastIncludedTerm, 
		// Send the complete snapshot (with map, lastIncludedIndex and lastIncludedTerm)
		SnapshotData:  rf.persister.ReadSnapshot()}

		// Implement optimistic approach to snapshotting. If we send a snapshot, we assume the follower takes it on. 
		// If the follower rejects the Snapshot, we just role back nextIndex via AppendEntries RPC per the usual protocol. 
		rf.nextIndex[server] =  rf.realLogLength() + 1

	rf.mu.Unlock()

	// SEND SNAPASHOT RPC
	var reply InstallSnapshotReply
	msg_received = rf.SendSnapshot(server, args, &reply)

	// Note: We only update matchIndex through protocol in AppendEntries (safter and simpler)


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
				msgOut.Term = rf.getLogEntry(rf.lastApplied-1).Term
				msgOut.Command = rf.getLogEntry(rf.lastApplied-1).Command
				applyCh <- msgOut
				rf.dPrintf1("Server%d, Term%d, State: %s, Action: Successful Commit up to lastApplied of %d, msgSent => %v, rf.log => %+v \n", rf.me, rf.currentTerm, rf.stateToString(), rf.lastApplied, msgOut, rf.log)	
			}
			rf.mu.Unlock()

			time.Sleep(time.Millisecond * rf.heartbeat_len)
		}
	}
}

// Everytime a follower returns successfully from a Append Entries Routine, check if leader can commit additional log entries. 
func (rf *Raft) checkCommitStatus(reply *AppendEntriesReply) {

	// Protocol: Only the leader can decide when it's safe to apply a command to the state machine. 
	// Note: Technically, we do not need to check that term at this point since the checkCommitStatus function
	// is enclosed in a mutual exclusion lock. So, the term should not have been able to change. 
	if (rf.myState == Leader) && (rf.currentTerm == reply.Term) {

		var tempCommitIndex int = 0

		//Protocol to determine new commitIndex

		// Protocol: Ensure that the N > commitIndex
		// Iterate across each item in log above commitIndex
		for i_log := rf.commitIndex; i_log < rf.realLogLength(); i_log++ {
			var count int = 0
			// Protocol: Ensure that log[N].term == currentTerm
			if(rf.getLogEntry(i_log).Term  == rf.currentTerm){
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
			rf.error("Error: Voting related issue. This should not to be possible. \n");
		}

		//Setup variables
		var allowedToVote bool = (rf.votedFor == -1) || (rf.votedFor == args.CandidateId)
		thisLastLogIndex := rf.realLogLength()

		// Setup variables: Handle case where log is not initialized. 
		var thisLastLogTerm int
		if (thisLastLogIndex == 0) {
			thisLastLogTerm = -1
		} else {
			thisLastLogTerm = rf.getLogEntry(thisLastLogIndex-1).Term
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
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
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
	case <-time.After(time.Millisecond * 100):
	  	ok = false
	case ok = <-RPC_returned:
	
		 // Needed to maintain appropriate concurrency 
		rf.mu.Lock()
	  	defer rf.mu.Unlock()


		// Note: We don't need to check the server state or term before entering this if statement. This term/state checking 
		// code block needs to be done when every RPC is exchanged. 
		if(ok) {
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
	ConflictingIndex	int
	Success 			bool

}

//
// Function handles communication between Raft instances to synchronize on logs. 
// Functions handles as an indication of a heartbeat
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
		// Protocol: In this case, recognize the leader by  reseting the election timeout
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
	case <-time.After(time.Millisecond * 100):
	  	ok = false
	case ok = <-RPC_returned:
		
		// Needed to maintain appropriate concurrency 
		rf.mu.Lock()
	  	defer rf.mu.Unlock()
	  	

		if(ok) {
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


type InstallSnapshotArgs struct {
	Term 				int
	LeaderId			int
	LastIncludedIndex 	int
	LastIncludedTerm 	int
	SnapshotData		[]byte
}


type InstallSnapshotReply struct {
	Term 				int
	Applied 			bool
}


func (rf *Raft) HandleSnapshot(args InstallSnapshotArgs, reply *InstallSnapshotReply) {

	// Needed to maintain appropriate concurrency 
	rf.mu.Lock()
  	defer rf.mu.Unlock()
  	rf.dPrintf2("Server%d, Term%d, State: %s, Action: Follower Receives Snapshot RPC. args => %v rf.log => %v ",rf.me, rf.currentTerm, rf.stateToString(),args, rf.log )
			

	// Protocol: If this server's term is lagging, update the term. 
	// Protocol: If the current system thinks they are a Leader or Candidate (but is in the wrong term), set them to Follower state.
	// Protocol: In this case, recognize the leader by  reseting the election timeout
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
	}

	// Set default return values. 
	reply.Term = rf.currentTerm
	reply.Applied = false

	// Procol 1: Reply immediately if term < currentTerm
	// Note: The server sending the RPC is not the real Leader, so inform it. 
	if (args.Term < rf.currentTerm) {
		return
	}

	// Unpack the snapshot
	// ToDo: Put in function
	var newLastIncludedIndex	int
	var newLastIncludedTerm		int
	r := bytes.NewBuffer(args.SnapshotData)
 	d := gob.NewDecoder(r)
	d.Decode(&newLastIncludedIndex)
	d.Decode(&newLastIncludedTerm)


	// Protocol 5: Discard outdated snapshots. If the snapshot is ahead of this one, reject the current RPC. 
	// Note: This is possibleif RPCs are re-ordered. 
	if (rf.lastIncludedIndex >= newLastIncludedIndex) {
		// Note: If this occurs, how does the leader who receives the RPC handle the case.  
		// Only can happen if follower recieves a very delayed RPC (and has processed a previous sendSnapshot RPC in the meantime.)
		// Note: Equal sign included in if statement because might discard future logs. 
		rf.error("Error: Leader sent RPC to follower. But, follower already had a snapshot that is more advanced. rf.lastIncludedIndex => %d, newLastIncludedIndex => %d", rf.lastIncludedIndex, newLastIncludedIndex)
		return
	}



	// Protocol 6: "If existing log entry has same index and term as snapshot's last included entry, retain
	// log entries following it and reply."
	// Note: Check if rf.log has log entry at the new snapshot's lastIncludedIndex
	// Adjustment to protocol: If the log exists, truncate log and apply snapshot to KVServer. 
	if ( (newLastIncludedIndex > rf.lastIncludedIndex) && (newLastIncludedIndex <= rf.realLogLength())) {
		// Note: Check if the terms match for lastIncludedTerm and entry in rf.log at the newLastIncludedIndex
		if (newLastIncludedTerm == rf.getLogEntry(newLastIncludedIndex-1).Term) {

			rf.dPrintf_now("Warning: Handling edge case described in Step 6.")

			// Note: Make sure that we are not roling back map with snapshot. If follower applied more logs than snapshot, do 
			// not install the snapshot. Use commitIndex since don't want to role back commitIndex either. 
			// if (rf.commitIndex <= newLastIncludedIndex) {

			// 	// At this point, we know that the snapshot will be applied to the state machine. 
			// 	reply.Applied = true

			// 	// Protocol 5: Save Snapshot File (save in KVServer)
			// 	rf.persister.SaveSnapshot(args.SnapshotData)

			// 	// If match, only retain log entries following it, and reply. 
			// 	rf.dPrintf2("Server%d, Term%d, State: %s, Action: Truncate Logs. newLastIncludedIndex => %d, rf.lastIncludedIndex => %d, rf.log => %v ",rf.me, rf.currentTerm, rf.stateToString(), newLastIncludedIndex, rf.lastIncludedIndex, rf.log )
			// 	trunc_snapIndex_start := newLastIncludedIndex - rf.lastIncludedIndex
			// 	trunc_snapIndex_end := len(rf.log)
			// 	rf.log = rf.log[trunc_snapIndex_start:trunc_snapIndex_end]
			// 	// After truncating log, update snapshot variables in rf structure
			// 	rf.lastIncludedIndex =  newLastIncludedIndex
			// 	rf.lastIncludedTerm = newLastIncludedTerm
			// 	rf.persist()


			// 	// Update KVServer with snapshot (done synchronously)
			// 	rf.dPrintf1("Server%d, Term%d, State: %s, Action: Send Snapshot to KVServer (truncated log)\n", rf.me, rf.currentTerm, rf.stateToString())
			// 	rf.sendSnapshotToServer(args.SnapshotData)

			// 	// Error checking for debugging
			// 	// Note: We can never role back lastApplied. Once a log is applied, that's final. 
			// 	// We should also never role back rf.comitINdex
			// 	if (rf.lastApplied > rf.lastIncludedIndex) || (rf.commitIndex > rf.lastIncludedIndex){
			// 		rf.error("Error in HandleSnapshot: We roled back lastApplied.")
			// 	}

			// 	//After updating state machine, update applied and committed variables. 
			// 	rf.lastApplied = newLastIncludedIndex
			//	rf.commitIndex = newLastIncludedIndex
		
			// 	return

			// }	

			// If the snapshot is in the log but the appliedEntries are ahead of the lastIncludedIndex of the snapshot, 
			// then we canno apply snapshot. 
			return
		}
	}

	// At this point, we know that the snapshot will be applied to the state machine. 
	reply.Applied = true

	// Protocol 5: Save Snapshot File (save in KVServer)
	rf.persister.SaveSnapshot(args.SnapshotData)

	// Protocol 7: "Discard Entire Log"
	// If match, only retain log entries following it, and reply. 
	rf.log = make([]RaftLog, 0)
	rf.lastIncludedIndex = newLastIncludedIndex
	rf.lastIncludedTerm = newLastIncludedTerm
	rf.persist()


	// Protocol 8: "Reset state machine using snapshot contents"
	// Update Raft snapshot parameters (after updaing KVServer)
	rf.dPrintf1("Server%d, Term%d, State: %s, Action: Send Snapshot to KVServer (removed log)", rf.me, rf.currentTerm, rf.stateToString())
	rf.sendSnapshotToServer(args.SnapshotData)

	// Error checking for debugging
	// Note: We can never role back lastApplied. Once a log is applied, that's final. 
	// We should also never role back rf.comitINdex
	if (rf.lastApplied > rf.lastIncludedIndex) || (rf.commitIndex > rf.lastIncludedIndex){
		rf.error("Error in HandleSnapshot: We roled back lastApplied.")
	}

	// After updating state machine, update applied and committed variables.
	rf.lastApplied = newLastIncludedIndex
	rf.commitIndex = newLastIncludedIndex

}

func (rf *Raft) SendSnapshot(server int, args InstallSnapshotArgs, reply *InstallSnapshotReply) bool {

	myState_temp :=rf.getLockedState()
	RPC_returned := make(chan bool)
	if (myState_temp == Leader) {
		
		go func(goArgs InstallSnapshotArgs, goReply *InstallSnapshotReply,output chan bool) {
			ok := rf.peers[server].Call("Raft.HandleSnapshot", goArgs, goReply)
			output <- ok
		}(args, reply, RPC_returned)
	}

	//Allows for RPC Timeout
	var ok bool = false
	select {
	case <-time.After(time.Millisecond * 100):
	  	ok = false
	case ok = <-RPC_returned:
		
		// Needed to maintain appropriate concurrency 
		rf.mu.Lock()
	  	defer rf.mu.Unlock()
	  	
	  	// Use for debugging purposes
	  	if (!ok || !reply.Applied || (reply.Term > rf.currentTerm)) {
	  		rf.dPrintf_now("Warning: Sent snapshot to Follower. But, follower did not apply the snapshot. ")
	  	}

		if(ok) {
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
// may fail or lose an election.
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
		rf.matchIndex[rf.me] = rf.realLogLength()

		index = rf.realLogLength()
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
	rf.mu.Lock()
	defer rf.mu.Unlock()

	rf.dPrintf1("Server%d, Term%d, State: %s, Action: SERVER Dies \n %s \n" ,  rf.me, rf.currentTerm, rf.stateToString(), debug_break)
	close(rf.shutdownChan)
	// Note: Don't need to close this channel. The channel will be garbage collected when it's no longer used. 
	// Closing a channel is a control signal, and so we don't need to close this channel. 
	// Note: With the locking implemented, we technically can close the channel. Since this code will run synchronously with any thread
	// that checks serviceClientChan. And, once this function is finished running, the test is successful. 
	// close(rf.serviceClientChan)

	rf.heartbeatTimer.Stop()
	rf.electionTimer.Stop()

	rf.debug = -1
}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
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
	rf.applyCh = applyCh
	rf.debug = 2
	// Needed to maintain appropriate concurrency 
	// Note: This case probably not necessary, but included for saftey 
	rf.mu.Lock()
  	defer rf.mu.Unlock()



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
	rf.serviceClientChan = make(chan int, 512)

	rf.shutdownChan = make(chan int)



	// INITIALIZE PERSISTANT STATES //
	if (rf.persister.RaftStateSize() > 0) {
		rf.readPersist(persister.ReadRaftState())
		rf.dPrintf1("Server%d, Term%d, State: %s, Action: Initialize Server from Raft Persister. rf.log => %v, currentTerm => %d, votedFor => %d \n" ,  rf.me, rf.currentTerm, rf.stateToString(), rf.log, rf.currentTerm, rf.votedFor)
	} else {
	// initialize to base state if nothing stored in persistant memory
		// Protocol: Initialize current term to 0 on first boot
		rf.currentTerm = 0
		rf.votedFor = -1
	}


	// Initialize Snapshot States
	// Note: For failure recover, raft can directly from persister (raft and kvserver are currnetly operating synchronously). 
	rawSnapshotData := rf.persister.ReadSnapshot()
	if (len(rawSnapshotData) > 0) {

		// Decode rawSnapshotData
		// ToDo: Put in seperate function
		r := bytes.NewBuffer(rawSnapshotData)
	 	d := gob.NewDecoder(r)

	 	var lastIncludedIndex_check int
		d.Decode(&lastIncludedIndex_check)
		d.Decode(&rf.lastIncludedTerm)

		if(lastIncludedIndex_check != rf.lastIncludedIndex) {
			rf.error("Persistance Problem (conflicing persistance states)")
		}

		// Initialize to rf.lastIncludedIndex since we know this has been commited by snapshot. 
		rf.commitIndex = rf.lastIncludedIndex
		rf.lastApplied = rf.lastIncludedIndex

		rf.dPrintf1("Server%d, Term%d, State: %s, Action: Initialize Server from Snapshot. rf.lastIncludedIndex => %d\n" ,  rf.me, rf.currentTerm, rf.stateToString(), rf.lastIncludedIndex)

	} else {
		// Note: Don't initialize rf.lastIncludedTerm. Leads to a panics if used before first snapshot. 
		rf.lastIncludedIndex = 0
		rf.lastIncludedTerm = -1
		rf.persist()
		rf.commitIndex = 0
		rf.lastApplied = 0

	}


	// Go live
	rf.dPrintf1("Server%d, Term%d, State: %s, Action: SERVER Created \n %s \n" , rf.me, rf.currentTerm, rf.stateToString(), debug_break)
	go rf.manageRaftInterrupts()
	go rf.commitLogEntries(applyCh)


	return rf
}

// After snapshot taken, truncates the logs. 
func (rf *Raft) TruncateLogs(lastIncludedIndex int, lastIncludedTerm int, data_snapshot []byte) {

	rf.mu.Lock()
	defer rf.mu.Unlock()

	rf.dPrintf1("Server%d, Term%d, State: %s, Action: Truncate Logs. lastIncludedIndex => %d, lastIncludedTerm => %d \n" , rf.me, rf.currentTerm, rf.stateToString(), lastIncludedIndex, lastIncludedTerm)
	
	// Store the snapshot
	rf.persister.SaveSnapshot(data_snapshot)

	// Truncate all logs before lastIncludedIndex. Reassign log slice to rf.logs. Only keep log entries that are not part of the snapshot.
	rf.dPrintf2("Server%d: Before Truncation. rf.log => %+v, Raft Size => %d \n", rf.me, rf.log, rf.persister.RaftStateSize())
	trunc_snapIndex_start := lastIncludedIndex - rf.lastIncludedIndex
	trunc_snapIndex_end := len(rf.log)
	rf.log = rf.log[trunc_snapIndex_start:trunc_snapIndex_end]
	rf.persist()
	rf.dPrintf2("Server%d: After Truncation. rf.log => %+v, Raft Size => %d \n", rf.me, rf.log, rf.persister.RaftStateSize())

	// After truncating log, update snapshot variables in rf structure
	rf.lastIncludedIndex =  lastIncludedIndex
	rf.lastIncludedTerm = lastIncludedTerm
	rf.persist()

}

func (rf *Raft) sendSnapshotToServer(snapshot_data []byte) {
	msgOut := ApplyMsg{}
	msgOut.UseSnapshot = true
	msgOut.Snapshot = snapshot_data
	rf.dPrintf1("Server%d, msg on ApplyCh => %+v", rf.me, msgOut)
	rf.applyCh <- msgOut
}

// Function to 
func (rf *Raft) getLogEntry(i int) RaftLog {
	snap_i :=i-rf.lastIncludedIndex
	realIndex := i +1

	if (rf.indexNotInLog(realIndex)) {
		rf.error("Server%d: getLogEntry: Index is not included in Log. realIndex => %d, rf.lastIncludedIndex => %d ", rf.me, realIndex, rf.lastIncludedIndex)
	}

	return rf.log[snap_i]
}

func (rf *Raft) getSnapIndex(realIndex int) int {

	if (rf.indexNotInLog(realIndex)) {
		rf.dPrintf_now("Warning in getSnapIndex: Index might not be included in Log. realIndex => %d, rf.lastIncludedIndex => %d ", realIndex, rf.lastIncludedIndex)
	}
	return realIndex - rf.lastIncludedIndex
}


func (rf *Raft) indexNotInLog(realIndex int) bool {
	return (realIndex <= rf.lastIncludedIndex)
}

func (rf *Raft) realLogLength() int {
	return len(rf.log) + rf.lastIncludedIndex
}

func (rf *Raft) getConsistencyTerm(i int) int {
	realIndex := i + 1
	

	if (realIndex == 0) {
		return -1 
	} else if (realIndex == rf.lastIncludedIndex) {
		// Error checking
		if (rf.lastIncludedTerm == -1) {
			rf.error("Error in getConsistencyTerm: Should never be called when Term is -1.")
		}

		return rf.lastIncludedTerm
	} else {
		return rf.getLogEntry(i).Term
	}


}


//********** UTILITY FUNCTIONS **********//

// Returns a new election timeout duration between 150ms and 300ms
func getElectionTimeout() time.Duration {

	randSource := rand.NewSource(time.Now().UnixNano())
    r := rand.New(randSource)
	// Create random number between 150 and 300
	seedTime := (r.Float32() * float32(60)) + float32(150)
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
		log.Fatalf(format, a...)
	}
	return
}

func (rf *Raft) dPrintf1(format string, a ...interface{}) (n int, err error) {
	if rf.debug >= 1 {
		log.Printf(format + "\n", a...)
	}
	return
}

func (rf *Raft) dPrintf2(format string, a ...interface{}) (n int, err error) {
	if rf.debug >= 2 {
		log.Printf(format + "\n", a...)
	}
	return
}

func (rf *Raft) dPrintf_now(format string, a ...interface{}) (n int, err error) {
	if rf.debug >= 0 {
		log.Printf(format + "\n", a...)
	}
	return
}
