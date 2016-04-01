package raftkv

import (
	"encoding/gob"
	"labrpc"
	"log"
	"raft"
	"sync"
	"bytes"
)

const debug_break = "---------------------------------------------------------------------------------------"

// Human readable
type OpType int

const (
	Get    OpType = 1
	Put    OpType = 2
	Append OpType = 3
)

// Command structure (passed on to Raft)
// Note: Need to include ClientID and RequestID since any KV Server who recieves a Op from appplyCh needs to update their commitTable.
type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	CommandType OpType
	Key         string
	Value       string
	ClientID    int64
	RequestID   int64
}

type CommitStruct struct {
	RequestID   int64
	ReturnValue string
}

type RPCReturnInfo struct {
	success bool
	value   string
}

type RPCResp struct {
	resp_chan chan RPCReturnInfo
	Op        Op
}

type RaftKV struct {
	mu    sync.Mutex
	me    int
	rf    *raft.Raft
	persister *raft.Persister
	debug int

	applyCh              chan raft.ApplyMsg
	shutdownChan         chan int
	waitingForRaft_queue map[int]RPCResp

	maxraftstate int // snapshot if log grows this big

	// Your definitions here.
	// Include Key/Value data structure
	kvMap           map[string]string
	lastCommitTable map[int64]CommitStruct
}

// Handle GET Request RPCs.
// Note: Need to reply to the RPC by the end of the function.
func (kv *RaftKV) Get(args *GetArgs, reply *GetReply) {

	kv.mu.Lock()

	// 1) Convert GetArgs into  Op Struct
	thisOp := Op{
		CommandType: Get,
		Key:         args.Key,
		ClientID:    args.ClientID,
		RequestID:   args.RequestID}

	// Determine if RequestID has already been committed, or is the next request to commit.
	inCommitTable, returnValue := kv.checkCommitTable_beforeRaft(thisOp)

	// Return RPC with correct value: If the client is known, and this server already processed the RPC, send the return value to the client.
	// Note: Even if Server is not leader, respond with the information client needs for this specific request.
	if inCommitTable {

		kv.DPrintf1("KVServer%d, Action: REPEAT REQUEST. GET ALREADY APPLIED. Respond to client.   \n", kv.me)
		// Note: At this point, it's unkown if this server is the leader.
		// However, if the server has the right information for the client, send it.
		reply.WrongLeader = false
		reply.Value = returnValue // Return the value already committed.
		reply.Err = OK

		// Unlock on any reply
		kv.mu.Unlock()

		// Process the next valid RPC Request with Start(): If 1) client isn't known or 2) the request follows the request already committed.
	} else if !inCommitTable {

		// Unlock before Start (so Start can be in parallel)
		kv.mu.Unlock()

		// 2) Send Op Struct to Raft (using kv.rf.Start())
		index, _, isLeader := kv.rf.Start(thisOp)

		kv.mu.Lock()

		// 3) Handle Response: If Raft is not leader, return appropriate reply to client
		if !isLeader {

			kv.DPrintf2("KVServer%d, Action: Rejected GET. KVServer%d not Leader.  \n", kv.me, kv.me)

			reply.WrongLeader = true
			reply.Value = "" // Return value from key. Returns "" if key doesn't exist
			reply.Err = OK

			// Unlock on any reply
			kv.mu.Unlock()

			// 4) Handle Response: If Raft is leader, wait until raft committed Op Struct to log
		} else if isLeader {

			kv.DPrintf1("%s \n KVServer%d, Action:  KVServer%d is Leader. Sent GET Request to Raft. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v\n", debug_break, kv.me, kv.me, index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue)

			resp_chan := kv.updateRPCTable(thisOp, index)

			// Unlock before response Channel. Since response channel blocks, need to allow other threads to run.
			kv.mu.Unlock()

			// Wait until: 1) Raft indicates that RPC is successfully committed, 2) this server discovers it's not the leader, 3) failure
			rpcReturnInfo, open := <-resp_chan

			// Note: Locking possible here since every RPC created should only receive a single write on the Resp channel.
			// Once it receives the response, just wait until scheduled to run.
			// Error if we receive two writes on the same channel.
			kv.mu.Lock()
			// Successful commit indicated by Raft: Respond to Client
			if rpcReturnInfo.success && open {
				kv.DPrintf1("KVServer%d, Action: GET APPLIED. Respond to client. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n %s \n \n", kv.me, index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue, debug_break)
				reply.WrongLeader = false
				reply.Value = rpcReturnInfo.value // Return value from key. Returns "" if key doesn't exist
				reply.Err = OK

				// Commit Failed: If this server discovers it's not longer the leader, then tell the client to find the real leader,
				// and retry with the request there.
			} else if !open || !rpcReturnInfo.success {
				kv.DPrintf1("KVServer%d, Action: GET ABORTED. Respond to client. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n %s \n \n", kv.me, index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue, debug_break)
				reply.WrongLeader = true
				reply.Err = OK

			}
			kv.mu.Unlock()

		}

	}
}

// Handler: RPC sender receives returned RPC once function completed.
func (kv *RaftKV) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	// Your code here.

	kv.mu.Lock()

	// 1) Convert PutAppendArgs into  Op Struct. Include ClientId and RequestID so Servers can update their commitTable
	thisOp := Op{
		CommandType: kv.stringToOpType(args.Op),
		Key:         args.Key,
		Value:       args.Value,
		ClientID:    args.ClientID,
		RequestID:   args.RequestID}

	// Determine if RequestID has already been committed, or is the next request to commit.
	inCommitTable, _ := kv.checkCommitTable_beforeRaft(thisOp)

	// Return RPC with correct value: If the client is known, and this server already processed the RPC, send the return value to the client.
	// Note: Even if Server is not leader, respond with the information client needs for this specific request.
	if inCommitTable {

		kv.DPrintf1("KVServer%d, Action: REPEAT REQUEST. PUTAPPEND ALREADY APPLIED. Respond to client.   \n", kv.me)
		// Note: At this point, it's unkown if this server is the leader.
		// However, if the server has the right information for the client, send it.
		reply.WrongLeader = false
		reply.Err = OK

		// Unlock on any reply
		kv.mu.Unlock()

		// Process the next valid RPC Request with Start(): If 1) client isn't known or 2) the request follows the request already committed.
	} else if !inCommitTable {

		// Unlock before Start (so Start can be in parallel)
		kv.mu.Unlock()

		// 2) Send Op Struct to Raft (using kv.rf.Start())
		index, _, isLeader := kv.rf.Start(thisOp)

		kv.mu.Lock()

		// 3) Handle Response: If Raft is not leader, return appropriate reply to client
		if !isLeader {

			kv.DPrintf2("KVServer%d, Action: Rejected PutAppend. KVServer%d not Leader.  \n", kv.me, kv.me)
			reply.WrongLeader = true
			reply.Err = OK

			// Unlock on any reply
			kv.mu.Unlock()

			// 4) Handle Response: If Raft is leader, wait until raft committed Op Struct to log
		} else if isLeader {
			kv.DPrintf1("%s \n KVServer%d, Action:  KVServer%d is Leader. Sent PUTAPPEND Request to Raft. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v\n", debug_break, kv.me, kv.me, index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue)

			resp_chan := kv.updateRPCTable(thisOp, index)

			// Unlock before response Channel. Since response channel blocks, need to allow other threads to run.
			kv.mu.Unlock()

			// Wait until: 1) Raft indicates that RPC is successfully committed, 2) this server discovers it's not the leader, 3) failure
			rpcReturnInfo, open := <-resp_chan

			// Note: Locking not possible here since every RPC created should only receive a single write on the Resp channel.
			// Once it receives the response, just wait until scheduled to run.
			// Error if we receive two writes on the same channel.
			kv.mu.Lock()
			// Successful commit indicated by Raft: Respond to Client
			if open && rpcReturnInfo.success {

				kv.DPrintf1("KVServer%d, Action: PUTAPPEND APPLIED. Respond to client. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n %s \n \n", kv.me, index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue, debug_break)
				reply.WrongLeader = false
				reply.Err = OK

				// Commit Failed: If this server discovers it's no longer the leader, then tell the client to find the real leader,
				// and retry with the request there.
			} else if !open || !rpcReturnInfo.success {

				kv.DPrintf1("KVServer%d, Action: PUTAPPEND ABORTED. Respond to client. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n %s \n \n", kv.me, index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue, debug_break)
				reply.WrongLeader = true
				reply.Err = OK

			}

			kv.mu.Unlock()
		}
	}
}

//
// the tester calls Kill() when a RaftKV instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (kv *RaftKV) Kill() {

	kv.rf.Kill()

	// Note: While the serve is being killed, should not handle any other incoming RPCs, etc.
	kv.mu.Lock()
	defer kv.mu.Unlock()
	kv.DPrintf1("%s \n KVServer%d, Action: Dies \n", debug_break, kv.me)

	// Kill all open go routines when this server quites.
	close(kv.shutdownChan)

	// Since this server will no longer be leader on resstart, return all outstanding PutAppend and Get RPCs.
	// Allows respective client to find another leader.
	kv.killAllRPCs()

	// Turn off debuggin output
	kv.debug = -1

}

//
// servers[] contains the ports of the set of
// servers that will cooperate via Raft to
// form the fault-tolerant key/value service.
// me is the index of the current server in servers[].
// the k/v server should store snapshots with persister.SaveSnapshot(),
// and Raft should save its state (including log) with persister.SaveRaftState().
// the k/v server should snapshot when Raft's saved state exceeds maxraftstate bytes,
// in order to allow Raft to garbage-collect its log. if maxraftstate is -1,
// you don't need to snapshot.
//
func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *RaftKV {
	// call gob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	gob.Register(Op{})

	kv := new(RaftKV)
	kv.me = me
	kv.maxraftstate = maxraftstate
	kv.persister = persister
	kv.debug = 0

	// Your initialization code here.
	kv.mu = sync.Mutex{}

	// Note: This locking probably not necessary, but included for saftey
	kv.mu.Lock()
	defer kv.mu.Unlock()

	// Creates Queue to keep tract of outstand Client RPCs (while waiting for Raft)
	kv.waitingForRaft_queue = make(map[int]RPCResp)

	// Create Key/Value Map
	kv.kvMap = make(map[string]string)

	// Creates commitTable (to store last RPC committed for each Client)
	kv.lastCommitTable = make(map[int64]CommitStruct)

	// Makes a channel that recevies committed messages from Raft architecutre.
	kv.applyCh = make(chan raft.ApplyMsg)
	// Used to shutdown go routines when service is killed.
	kv.shutdownChan = make(chan int)

	// Load persisted snapshot (if it exists)
	// For failure recover, raft reads directly from persister. 
	rawSnapshotData := persister.ReadSnapshot()
	if (len(rawSnapshotData) > 0) {
		kv.readPersistSnapshot(rawSnapshotData)
	}

	// Initiates raft system (built as part of Lab2)
	kv.rf = raft.Make(servers, me, persister, kv.applyCh)

	kv.DPrintf1("%s \n KVServer%d, Action: New KV Server and Raft Instance Created. \n", debug_break, kv.me)

	go kv.processCommits()

	return kv
}

//********** KV Server FUNCTIONS (non-RPC) **********//

func (kv *RaftKV) processCommits() {

	// Go routine that loops until server is shutdown.
	for {
		
		select {
		case commitMsg := <-kv.applyCh:

			// Lock the entire code-set that handles returned Operations from applyCh
			kv.mu.Lock()

			kv.DPrintf2("KVServer%d, State before Receiving OP on applyCh. Map => %+v, RPC_Queue => %+v, CommitTable %+v \n", kv.me, kv.kvMap, kv.waitingForRaft_queue, kv.lastCommitTable)

			if (commitMsg.UseSnapshot) {
				kv.readPersistSnapshot(commitMsg.Snapshot)
				kv.DPrintf2("KVServer%d, State Machine reset with snapshot. Map => %+v, RPC_Queue => %+v, CommitTable %+v \n", kv.me, kv.kvMap, kv.waitingForRaft_queue, kv.lastCommitTable)

				// Just apply the snapshot. This will skip all of the rest of the exectuion, and return to select. 
				kv.mu.Unlock()
				continue
			}
			

			// Type Assert: Package the Command from ApplyMsg into an Op Struct
			thisCommand := commitMsg.Command.(Op)

			// Execute Get Request
			if thisCommand.CommandType == Get {

				commitExists, returnValue := kv.checkCommitTable_afterRaft(thisCommand)

				// Raft Op is next request: Execute the commmand
				if !commitExists {

					// Exectue operation: get value
					newValue, ok := kv.kvMap[thisCommand.Key]

					// Update the value to be returnd
					returnValue = newValue

					// Handle casewhere key (from Get) doesn't exists.
					if !ok {
						newValue = ""
					} else if ok && newValue == "" {
						kv.DPrintf2("Assertion: Send back a null string value from a found key. Can get confused with ErrNoKey. \n")
					}

					// Update commitTable
					kv.lastCommitTable[thisCommand.ClientID] = CommitStruct{RequestID: thisCommand.RequestID, ReturnValue: returnValue}

				}

				// If there is an outstanding RPC, return the appropriate value.
				kv.handleOpenRPCs(commitMsg.Index, thisCommand, returnValue)

				// Execute Put Request
			} else if thisCommand.CommandType == Put {

				// Check commitTable
				commitExists, _ := kv.checkCommitTable_afterRaft(thisCommand)

				// Raft Op is next request: Execute the commmand
				if !commitExists {
					// Exectue operation: Replaces the value for a particular key.
					kv.kvMap[thisCommand.Key] = thisCommand.Value

					// Update commitTable. No returnValue since a put/append request
					kv.lastCommitTable[thisCommand.ClientID] = CommitStruct{RequestID: thisCommand.RequestID}
				}

				//Return RPC to Client with correct value.
				kv.handleOpenRPCs(commitMsg.Index, thisCommand, "")

				// Execute Append Request
			} else if thisCommand.CommandType == Append {

				// Check commitTable
				commitExists, _ := kv.checkCommitTable_afterRaft(thisCommand)

				// Raft Op is next request: Execute the commmand
				if !commitExists {
					// Exectue operation: Replaces the value for a particular key.
					kv.kvMap[thisCommand.Key] = kv.kvMap[thisCommand.Key] + thisCommand.Value

					// Update commitTable. No returnValue since a put/append request
					kv.lastCommitTable[thisCommand.ClientID] = CommitStruct{RequestID: thisCommand.RequestID}
				}

				//Return RPC to Client with correct value.
				kv.handleOpenRPCs(commitMsg.Index, thisCommand, "")

			} else {
				kv.DError("Error: Operation Recieved on applyCh is neither 'Append', 'Put' nor 'Get'. \n")
			}

			kv.DPrintf2("KVServer%d, State after Receiving OP on applyCh. Map => %+v, RPC_Queue => %+v, CommitTable %+v \n \n \n ", kv.me, kv.kvMap, kv.waitingForRaft_queue, kv.lastCommitTable)

			kv.mu.Unlock()

			// Determine if snapshots are needed, and handle the process of obtaining a snapshot. 
			// Note: Take  snapshot after we execute command to ensure that the snapshot includes the last committed message in map. 
			// Note: If kv.maxraftstate = -1, this indicates snapshotting is not being used. 
			// Note: Cannot lock communication channels with Raft (not from KVServer to Raft)
			if (kv.maxraftstate>-1) {
				kv.manageSnapshots(commitMsg.Index, commitMsg.Term)
			}


		case <-kv.shutdownChan:
			return

		}
	}
}

// Determine if we need to take a snapshot. Handle the process of obtaining a snapshot. 
func (kv *RaftKV) manageSnapshots(lastIncludedIndex int, lastIncludedTerm int) {

	// Determine if we need to take a snapshot 
	// If size of stored raft state in bytes>= maxraftstate, take snapshot.
	currentRaftSize := kv.persister.RaftStateSize()
	if (currentRaftSize >= kv.maxraftstate) {
		kv.mu.Lock()
		kv.DPrintf1("KVServer%d, Action: Create Snapsthot. Map => %+v, RPC_Queue => %+v, CommitTable %+v \n", kv.me, kv.kvMap, kv.waitingForRaft_queue, kv.lastCommitTable)

		// Marshal data into snapshot buffer
		w := new(bytes.Buffer)
	 	e := gob.NewEncoder(w)
	 	e.Encode(lastIncludedIndex)
	 	e.Encode(lastIncludedTerm)
	 	e.Encode(kv.kvMap)
	 	e.Encode(kv.lastCommitTable)
	 	data_snapshot := w.Bytes()

	 	// Note: Don't lock communication channes with raft from KVserver
	 	kv.mu.Unlock()
	 	// Send data to raft to 1) persist data and 2) truncate log. 
		kv.rf.TruncateLogs(lastIncludedIndex, lastIncludedTerm, data_snapshot)

	}

}

// Load the data from the last stored snapshot. 
func (kv *RaftKV) readPersistSnapshot(data []byte) {

	 r := bytes.NewBuffer(data)
	 d := gob.NewDecoder(r)

	 // DiscardlastIncludedIndex and lastIncludedTerm
	 var lastIncludedIndex int
	 var lastIncludedTerm int
	 d.Decode(&lastIncludedIndex)
	 d.Decode(&lastIncludedTerm)
	 d.Decode(&kv.kvMap)
	 d.Decode(&kv.lastCommitTable)

}

func (kv *RaftKV) checkCommitTable_beforeRaft(thisCommand Op) (inCommitTable bool, returnValue string) {

	// Set default return values
	inCommitTable = false
	returnValue = ""

	// Get previous commit value for this client.
	prevCommitted, ok := kv.lastCommitTable[thisCommand.ClientID]

	// Return RPC with correct value: If the client is known, and this server already processed the RPC, send the return value to the client.
	// Note: Even if Server is not leader, respond with the information client needs for this specific request.
	if ok && (prevCommitted.RequestID == thisCommand.RequestID) {

		// Note: At this point, it's unkown if this server is the leader.
		// However, if the server has the right information for the client, send it.
		inCommitTable = true
		returnValue = prevCommitted.ReturnValue

		// Catch Errors: Received an old RequestID (behind the one already committed)
	} else if ok && (prevCommitted.RequestID > thisCommand.RequestID) {
		kv.DPrintf1("Error at KVServer%d: prevCommitted: %+v and thisCommand: %+v \n", kv.me, prevCommitted, thisCommand)
		kv.DError("Error checkCommitTable_beforeRaft: Based on CommitTable, new RequestID is too old. This can happen if RPC very delayed. \n")
		// If this happens, just reply to the RPC with wrongLeader (or do nothing). The client won't even be listening for this RPC anymore

		// Process the next valid RPC Request with Start(): If 1) client isn't known or 2) the request is larger than the currently committed request.
		// The reason we can put any larger RequestID  into Raft is:
		// 1) It's the client's job to make sure that it only sends 1 request at a time (until it gets a response).
		// If the client sends a larger RequestID, it has received responses for everything before it, so we should put it into Raft.
		// 2) The only reason the client has a higher RequestID than the leader is a) if the server thinks they are the leader,
		// but are not or b) the server just became leader, and needs to commit a log in it's own term. If it cannot get a new log
		// then it cannot move forward.
	} else if (!ok) || (prevCommitted.RequestID < thisCommand.RequestID) {
		inCommitTable = false
		returnValue = ""

	}

	return inCommitTable, returnValue

}

// Note: For checking the commitTable, we only need to ckeck 1) client and 2) requestID.
// Since we are looking for duplicates, we don't care about the index (or the raft variables).
// We care just about what has been done for this request for this client.
func (kv *RaftKV) checkCommitTable_afterRaft(thisCommand Op) (commitExists bool, returnValue string) {

	// Set default return values
	commitExists = false
	returnValue = ""

	// Check commitTable Status
	prevCommitted, ok := kv.lastCommitTable[thisCommand.ClientID]

	// Operation Previously Committed: Don't re-apply, and reply to RPC if necessary
	// Check if: 1) exists in table and 2) if same RequestID
	if ok && (prevCommitted.RequestID == thisCommand.RequestID) {
		commitExists = true
		returnValue = prevCommitted.ReturnValue

		// Out of Order RPCs: Throw Error
	} else if ok && ((prevCommitted.RequestID > thisCommand.RequestID) || prevCommitted.RequestID+1 < thisCommand.RequestID) {
		kv.DError("Error checkCommitTable_afterRaft: Out of order commit reached the commitTable. \n")
		// Right error since correctness means we cannot have out of order commits.

		// Operation never committed: Execute operation, and reply.
		// Enter if: 1) Client never committed before or 2) the requestID is the next expected id
	} else if (!ok) || (prevCommitted.RequestID+1 == thisCommand.RequestID) {
		commitExists = false
		returnValue = ""

	}

	return commitExists, returnValue

}

// After inputting operation into Raft as leader
func (kv *RaftKV) updateRPCTable(thisOp Op, raftIndex int) chan RPCReturnInfo {

	// Build RPCResp structure
	// Build channel to flag this RPC to return once respective index is committed.
	new_rpcResp_struct := RPCResp{
		resp_chan: make(chan RPCReturnInfo),
		Op:        thisOp}

	// Quary the queue to determine if RPC already in tablle.
	old_rpcResp_struct, ok := kv.waitingForRaft_queue[raftIndex]

	// If the index is already in queue
	if ok {

		sameOp := compareOp(new_rpcResp_struct.Op, old_rpcResp_struct.Op)

		// Operations Different: Return the old RPC Request. Enter the new RPC request into table.
		if !sameOp {

			// Return old RPC Request with failure
			// Note: Since we got the same index but a different Op, this means that the operation at the current index
			// is stale (submitted at an older term).
			old_rpcResp_struct.resp_chan <- RPCReturnInfo{success: false, value: ""}
			//Enter new RPC request into table
			kv.waitingForRaft_queue[raftIndex] = new_rpcResp_struct

			// Same index and same operation: Only possible when same server submits the same request twice, but in different terms and
			// with the old request somehow deleted from the log during the term switch.
		} else {
			// Possible when: Server submits an Operation, and then crashes. The operation is then
			kv.DPrintf1("Possible Error: Server recieved the same operation with the same index assigned by raft. This is possible, but unlikely. \n")
			// The only way the same client can send a new response is if the other RPC returned. So, we replaced the old request
			// We know it's the same client since we compare the operation (which includes the client).
			kv.waitingForRaft_queue[raftIndex] = new_rpcResp_struct
		}

		// If index doesn't exist: just add new RPCResp_struct
	} else {
		kv.waitingForRaft_queue[raftIndex] = new_rpcResp_struct
	}

	kv.DPrintf2("RPCTable  before raft: %v \n", kv.waitingForRaft_queue)
	return new_rpcResp_struct.resp_chan

}

// After receiving operation from Raft
func (kv *RaftKV) handleOpenRPCs(raftIndex int, raftOp Op, valueToSend string) {

	//Find all open RPCs at this index. Return the appropriate value.
	for index, rpcResp_struct := range kv.waitingForRaft_queue {

		if index == raftIndex {
			sameOp := compareOp(rpcResp_struct.Op, raftOp)

			// Found the correct RPC at index
			if sameOp {

				// Respond to Client with success
				rpcResp_struct.resp_chan <- RPCReturnInfo{success: true, value: valueToSend}
				// Delete index
				delete(kv.waitingForRaft_queue, index)

				// Found different RPC at inex
			} else {

				// Respond to Client with failure
				// Note: Since we got the same index but a different Op, this means that the operation at the current index
				// is stale (submitted at an older term, or we this server thinks they are leader, but are not).
				rpcResp_struct.resp_chan <- RPCReturnInfo{success: false, value: ""}
				delete(kv.waitingForRaft_queue, index)

			}
		}
	}
}

func (kv *RaftKV) killAllRPCs() {

	for index, rpcResp_struct := range kv.waitingForRaft_queue {
		// Send false on every channel so every outstanding RPC can return, indicating client to find another leader.
		kv.DPrintf2("Kill Index: %d \n", index)
		rpcResp_struct.resp_chan <- RPCReturnInfo{success: false, value: ""}
		delete(kv.waitingForRaft_queue, index)

	}

}

//********** UTILITY FUNCTIONS **********//
func (operation OpType) opToString() string {

	s := ""
	if operation == 1 {
		s += "Get"
	} else if operation == 2 {
		s += "Put"
	} else if operation == 3 {
		s += "Append"
	}
	return s
}

func compareOp(op1 Op, op2 Op) (sameOp bool) {

	if (op1.CommandType == op2.CommandType) && (op1.Key == op2.Key) && (op1.Value == op2.Value) && (op1.ClientID == op2.ClientID) && (op1.RequestID == op2.RequestID) {
		sameOp = true
	} else {
		sameOp = false
	}
	return sameOp
}

func (kv *RaftKV) stringToOpType(op_s string) (op_type OpType) {

	if op_s == "Append" {
		op_type = 3
	} else if op_s == "Put" {
		op_type = 2
	} else {
		kv.DError("Error: Client's Operation is neither 'Append' nor 'Put'. ")
		op_type = -1
	}

	return op_type
}

func (kv *RaftKV) DPrintf2(format string, a ...interface{}) (n int, err error) {
	if kv.debug >= 2 {
		log.Printf(format + "\n", a...)
	}
	return
}

func (kv *RaftKV) DPrintf1(format string, a ...interface{}) (n int, err error) {
	if kv.debug >= 1 {
		log.Printf(format + "\n", a...)
	}
	return
}

func (kv *RaftKV) DError(format string, a ...interface{}) (n int, err error) {
	if kv.debug >= 0 {
		log.Fatalf(format, a...)
	}
	return
}
