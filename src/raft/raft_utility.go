package raft

import "time"
import "math/rand"
import "log"

//********** HELPER FUNCTIONS **********//

// Distribute new log entry to followers.  
func (rf *Raft) sendNewLog(server int, newLogIndex int) {

	// Lock entire AppendEntry and Snapshot routine (except for RPC)
	rf.mu.Lock()
	defer rf.mu.Unlock()

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
			
			// Critical: These state checks (to make sure the servers state has not changed) need to be made 
			// here since 1) we just recieved the lock (so other threads could be running in between), 
			// and 2) we will use this data to make permenant state changes to our system. 
			if ((newLogIndex >= rf.nextIndex[server]) && (rf.myState == Leader) && (msg_received)  && (rf.currentTerm == temp_term)) {

				// If nextIndex doesn't exists in Log, send a snapshot. 
				// Otherwise, send the log entries directly. 
				if (rf.nextIndex[server] <= rf.lastIncludedIndex) {

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
}


// Send heartbeat to followers. 
func (rf *Raft) sendNewHeartbeat(server int) {

	// Lock entire heartbeat routine (except for RPC)
	rf.mu.Lock()
	defer rf.mu.Unlock()
	

	// If nextIndex doesn't exists in Log, send a snapshot. 
	// Otherwise, send the log entries directly. 
	if (rf.nextIndex[server] <= rf.lastIncludedIndex) {
		rf.updateFollowerState(server)
	} else {
		rf.updateFollowerLogs(server)
	}

}

// Request vote from followers. 
// Important: voteCount needs to be a pointer (to make sure it's not just a copy)
func (rf *Raft) sendNewVote(server int, voteCount *int) {
	//Lock entire Vote logic (except actually sending the)
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// Document if the vote was or was not granted.
	voteGranted, msg_received, returnedTerm := rf.getVotes(server)

	// Critical: These state checks (to make sure the servers state has not changed) need to be made 
	// here since 1) we just recieved the lock (so other threads could be running in between), 
	// and 2) we will use this data to make permenant state changes to our system. 
	if (voteGranted && msg_received && (rf.myState == Candidate) && (rf.currentTerm == returnedTerm)) {
		*voteCount = *voteCount +1
		rf.dPrintf1("Server%d, Term%d, State: %s ,Action: Now have a total of %d votes. \n",rf.me, rf.currentTerm, rf.stateToString(), *voteCount)
	}

	// Decide election
 	if ((*voteCount >= rf.majority) && (rf.myState == Candidate)){
 		rf.dPrintf1("%s \n Server%d, Term%d, State: %s, Action: Elected New Leader, votes: %d\n", debug_break, rf.me, rf.currentTerm, rf.stateToString(), *voteCount)
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
}

// After snapshot taken, truncates the logs. 
func (rf *Raft) TruncateLogs(lastIncludedIndex int, lastIncludedTerm int, data_snapshot []byte) {

	rf.mu.Lock()
	defer rf.mu.Unlock()

	// Note: The TruncateLog function is called asynchronously within KVRaft
	// So, the TruncateLog can be processed after additional operations have been applied or can be processed out of order. 
	// If we receive an out of date TruncateLog function, discard it. 
	if (rf.lastApplied > lastIncludedIndex) || (lastIncludedIndex < rf.lastIncludedIndex) {
		return
	}

	rf.dPrintf1("Server%d, Term%d, State: %s, Action: Truncate Logs. lastIncludedIndex => %d, lastIncludedTerm => %d, rf.lastIncludedIndex => %d \n" , rf.me, rf.currentTerm, rf.stateToString(), lastIncludedIndex, lastIncludedTerm, rf.lastIncludedIndex)
	
	// Store the snapshot
	rf.persister.SaveSnapshot(data_snapshot)

	// Truncate all logs before lastIncludedIndex. Reassign log slice to rf.logs. Only keep log entries that are not part of the snapshot.
	rf.dPrintf2("Server%d: Before Truncation. rf.log => %+v, Raft Size => %d \n", rf.me, rf.log, rf.persister.RaftStateSize())
	trunc_snapIndex_start := lastIncludedIndex - rf.lastIncludedIndex
	trunc_snapIndex_end := len(rf.log)
	rf.log = rf.log[trunc_snapIndex_start:trunc_snapIndex_end]
	rf.dPrintf2("Server%d: After Truncation. rf.log => %+v, Raft Size => %d \n", rf.me, rf.log, rf.persister.RaftStateSize())

	if (lastIncludedIndex < rf.lastApplied) || (lastIncludedIndex-1 > rf.lastApplied) {
		rf.error("Error: lastIncludedIndex => %d, rf.lastApplied => %d ", lastIncludedIndex, rf.lastApplied)
	}
	

	// After truncating log, update snapshot variables in rf structure
	rf.lastIncludedIndex =  lastIncludedIndex
	rf.lastIncludedTerm = lastIncludedTerm
	// Also persist log changes above
	rf.persist()

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

func (rf *Raft) sendSnapshotToServer(snapshot_data []byte) {
	msgOut := ApplyMsg{}
	msgOut.UseSnapshot = true
	msgOut.Snapshot = snapshot_data
	rf.dPrintf1("Server%d, msg on ApplyCh => %+v", rf.me, msgOut)
	rf.applyCh <- msgOut
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

// Send out an RPC (with timeout implemented)
func (rf *Raft) sendRPC(server int, function string, goArgs interface{}, goReply interface{}) (ok_out bool){

	RPC_returned := make(chan bool)
	go func() {
		ok := rf.peers[server].Call(function, goArgs, goReply)
		RPC_returned <- ok
	}()

	//Allows for RPC Timeout
	ok_out = false
	select {
	case <-time.After(time.Millisecond * 100):
	  	ok_out = false
	case ok_out = <-RPC_returned:
	}

	return ok_out
}


//********** UTILITY FUNCTIONS **********//
func (rf *Raft) getLogEntry(i int) RaftLog {
	snap_i :=i-rf.lastIncludedIndex
	realIndex := i +1

	if (rf.indexNotInLog(realIndex)) {
		rf.dPrintf_now("Error in getLogEntry: Index is not included in Log. Server%d realIndex => %d, rf.lastIncludedIndex => %d, len(log) ", rf.me, realIndex, rf.lastIncludedIndex, len(rf.log))
		panic("Error in getLogEntry")
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

// Returns a new election timeout duration between 150ms and 300ms
func getElectionTimeout() time.Duration {

	randSource := rand.NewSource(time.Now().UnixNano())
    r := rand.New(randSource)
	// Create random number between 150 and 300
	seedTime := (r.Float32() * float32(60)) + float32(150)
	newElectionTimeout := time.Duration(seedTime) * time.Millisecond
	return newElectionTimeout

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


//********** DEBUGGING FUNCTIONS **********//
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
	if rf.debug >= 1 {
		log.Printf(format + "\n", a...)
	}
	return
}