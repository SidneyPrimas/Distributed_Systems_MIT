package shardkv

import (
	"encoding/gob"
	"log"
	"bytes"
	"fmt"
	"shardmaster"
)

const debug_break = "---------------------------------------------------------------------------------------"

//********** Helper Functions **********//
// Determine if we need to take a snapshot. Handle the process of obtaining a snapshot. 
func (kv *ShardKV) manageSnapshots(lastIncludedIndex int, lastIncludedTerm int) {

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
		go kv.rf.TruncateLogs(lastIncludedIndex, lastIncludedTerm, data_snapshot)

	}

}

// Load the data from the last stored snapshot. 
func (kv *ShardKV) readPersistSnapshot(data []byte) {

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

// Note: For checking the commitTable, we only need to ckeck 1) client and 2) requestID.
// Since we are looking for duplicates, we don't care about the index (or the raft variables).
// We care just about what has been done for this request for this client.
func (kv *ShardKV) checkCommitTable(thisCommand Op) (inCommitTable bool, returnValue string) {

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
		return inCommitTable, returnValue

		// Catch Errors: Received an old RequestID (behind the one already committed)
	} else if ok && (prevCommitted.RequestID > thisCommand.RequestID) {
		kv.DPrintf1("Error at KVServer%d: prevCommitted: %+v and thisCommand: %+v \n", kv.me, prevCommitted, thisCommand)
		kv.DError("Error checkCommitTable_beforeRaft: Based on CommitTable, new RequestID is too old. This can happen if RPC very delayed (since client RPC timeout has been changed). \n")
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
		return inCommitTable, returnValue

	}

	// Catch Errors
	kv.DError("Error at KVServer checkCommitTalbe. Default values should never be returned.")
	return inCommitTable, returnValue

}



// After inputting operation into Raft as leader
func (kv *ShardKV) updateRPCTable(thisOp Op, raftIndex int) chan RPCReturnInfo {

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
func (kv *ShardKV) handleOpenRPCs(raftIndex int, raftOp Op, valueToSend string) {

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

func (kv *ShardKV) killAllRPCs() {

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

// Compare Shards and Groups of two Configs. 
func compareShards(shard1 [shardmaster.NShards]int, shard2 [shardmaster.NShards]int) (bool) {

	if (len(shard1) != len(shard2)) {
		return false
	}

	// Shards only consider to be different if 1) gid different and 2) neither gid is Group#0. 
	for k, v := range(shard1) {
		if (shard2[k] != v) && (v != 0) && (shard2[k] != 0)  {
			return false
		}
	}

    return true
}

func compareOp(op1 Op, op2 Op) (sameOp bool) {

	if (op1.CommandType == op2.CommandType) && (op1.Key == op2.Key) && (op1.Value == op2.Value) && (op1.ClientID == op2.ClientID) && (op1.RequestID == op2.RequestID) {
		sameOp = true
	} else {
		sameOp = false
	}
	return sameOp
}

func (kv *ShardKV) stringToOpType(op_s string) (op_type OpType) {

	if op_s == "Configuration" {
		op_type = 4
	}else if op_s == "Append" {
		op_type = 3
	} else if op_s == "Put" {
		op_type = 2
	} else {
		kv.DError("Error: Client's Operation is neither 'Append' nor 'Put'. ")
		op_type = -1
	}

	return op_type
}

func (sm *ShardKV) DPrintf2(format string, a ...interface{}) (n int, err error) {
	if sm.debug >= 2 {
		custom_input := make([]interface{},2)
		custom_input[0] = sm.gid
		custom_input[1] = sm.me
		out_var := append(custom_input , a...)
		log.Printf("GID%d, KVServer%d, " + format + "\n", out_var...)
	}
	return
}

func (sm *ShardKV) DPrintf1(format string, a ...interface{}) (n int, err error) {
	if sm.debug >= 1 {
		custom_input := make([]interface{},2)
		custom_input[0] = sm.gid
		custom_input[1] = sm.me
		out_var := append(custom_input , a...)
		log.Printf("GID%d, KVServer%d, " + format + "\n", out_var...)
	}
	return
}

func (sm *ShardKV) DPrintf_now(format string, a ...interface{}) (n int, err error) {
	if sm.debug >= 0 {
		custom_input := make([]interface{},2)
		custom_input[0] = sm.gid
		custom_input[1] = sm.me
		out_var := append(custom_input , a...)
		log.Printf("GID%d, KVServer%d, " + format + "\n", out_var...)
	}
	return
}

func (sm *ShardKV) DError(format string, a ...interface{}) (n int, err error) {
	if sm.debug >= 0 {
		custom_input := make([]interface{},2)
		custom_input[0] = sm.gid
		custom_input[1] = sm.me
		out_var := append(custom_input , a...)
		panic_out := fmt.Sprintf("GID%d, KVServer%d, " + format + "\n", out_var...)
		panic(panic_out)
	}
	return
}