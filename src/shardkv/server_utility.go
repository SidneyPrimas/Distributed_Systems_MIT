package shardkv

import (
	"encoding/gob"
	"log"
	"bytes"
	"fmt"
	"labrpc"
	"time"
	"shardmaster"
)

const debug_break = "---------------------------------------------------------------------------------------"

//********** Helper Functions **********//
// Determine if we need to take a snapshot. Handle the process of obtaining a snapshot. 
// Note: Take  snapshot after we execute command to ensure that the snapshot includes the last committed message in map.  
// Note: Cannot lock communication channels with Raft (not from KVServer to Raft)
func (kv *ShardKV) manageSnapshots(lastIncludedIndex int, lastIncludedTerm int) {

	// Don't snapshot if maxraftstate == -1. 
	if (kv.maxraftstate<=-1) {
		return
	}

	// Determine if we need to take a snapshot 
	// If size of stored raft state in bytes>= maxraftstate, take snapshot.
	currentRaftSize := kv.persister.RaftStateSize()
	if (currentRaftSize >= kv.maxraftstate)  {
		kv.DPrintf1("Action: Create Snapsthot.  \n kv.transitionState => %+v, \n kv.committedConfig => %+v \n kv.shardTransferStorage => %+v \n kv.activeTransferRPCs => %+v \n", kv.transitionState, kv.committedConfig, kv.shardTransferStorage, kv.activeTransferRPCs)

		// Error checking: Ensure that kv.transitionState and committedConfig Configurations have the expected Num. 
		if (kv.transitionState.FutureConfig.Num < kv.committedConfig.Num) {
			kv.DError("Mismatching Num in transitionSate and committedConfig. kv.transitionState => %+v, \n kv.committedConfig => %+v", kv.transitionState, kv.committedConfig)
		}

		// Error Checking
		if (kv.transitionState.FutureConfig.Num == kv.committedConfig.Num && kv.transitionState.InTransition) {
			kv.DError("Action (wrie): In transition between configurations while committedConfig is already updated to the new one. ")
		}

		// Marshal data into snapshot buffer
		w := new(bytes.Buffer)
	 	e := gob.NewEncoder(w)
	 	e.Encode(lastIncludedIndex)
	 	e.Encode(lastIncludedTerm)
	 	e.Encode(kv.kvMap)
	 	e.Encode(kv.lastCommitTable)
	 	e.Encode(kv.committedConfig)
	 	e.Encode(kv.transitionState)
	 	e.Encode(kv.shardTransferStorage)
	 	data_snapshot := w.Bytes()

	 	// Send data to raft to 1) persist data and 2) truncate log. 
	 	// Note: Go routine does not keep the lock. 
		go kv.rf.TruncateLogs(lastIncludedIndex, lastIncludedTerm, data_snapshot)

	}

}

// Load the data from the last stored snapshot. 
func (kv *ShardKV) readPersistSnapshot(data []byte) {

	 r := bytes.NewBuffer(data)
	 d := gob.NewDecoder(r)

	 // Todo
	 kv.committedConfig = shardmaster.Config{}
	 kv.transitionState = TransitionState{}

	 // Discard lastIncludedIndex and lastIncludedTerm
	 var lastIncludedIndex int
	 var lastIncludedTerm int
	 d.Decode(&lastIncludedIndex)
	 d.Decode(&lastIncludedTerm)
	 d.Decode(&kv.kvMap)
	 d.Decode(&kv.lastCommitTable)
	 d.Decode(&kv.committedConfig)
	 d.Decode(&kv.transitionState)
	 d.Decode(&kv.shardTransferStorage)


	// Since received new shardTransferStorage, need to re-initialize activeTransferRPCs to 1) correct length, and 2) values.
	kv.activeTransferRPCs = make([]TransferRPCs, len(kv.shardTransferStorage))
	for key, shardPackage := range(kv.shardTransferStorage) {

		newTransferRPCs := TransferRPCs{}
		newTransferRPCs.active = false
		newTransferRPCs.gid_sendTo = shardPackage.GidToSendTo
		newTransferRPCs.futureNum = shardPackage.FutureConfig.Num

		kv.activeTransferRPCs[key] = newTransferRPCs

	}

	// Error checking: Ensure that kv.transitionState and committedConfig Configurations have the expected Num. 
	if (kv.transitionState.FutureConfig.Num < kv.committedConfig.Num) {
		kv.DError("Mismatching Num in transitionSate and committedConfig.  kv.transitionState => %+v, \n kv.committedConfig => %+v", kv.transitionState, kv.committedConfig)
	}

	// Error Checking
	if (kv.transitionState.FutureConfig.Num == kv.committedConfig.Num && kv.transitionState.InTransition) {
		kv.DError("Action (read): In transition between configurations while committedConfig is already updated to the new one. ")
	}

	// Error Checking
	if (len(kv.transitionState.GroupsToReceiveFrom) > 0 && !kv.transitionState.InTransition) {
		kv.DError("Action (read): Indicat that not in transition when still have Shards to Receive. ")
	}


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
		kv.DError("Error checkCommitTable: Based on CommitTable, new RequestID is too old. This can happen if RPC very delayed (since client RPC timeout has been changed). \n")
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

	return new_rpcResp_struct.resp_chan

}

// After receiving operation from Raft
func (kv *ShardKV) handleOpenRPCs(raftIndex int, raftOp Op, valueToSend string, error Err) {

	//Find all open RPCs at this index. Return the appropriate value.
	for index, rpcResp_struct := range kv.waitingForRaft_queue {

		if index == raftIndex {
			sameOp := compareOp(rpcResp_struct.Op, raftOp)

			// Found the correct RPC at index
			if sameOp && error == OK {

				// Respond to Client with success
				rpcResp_struct.resp_chan <- RPCReturnInfo{success: true, value: valueToSend, error: OK}
				// Delete index
				delete(kv.waitingForRaft_queue, index)

			// Found different RPC at inex
			} else {

				// Respond to Client with failure
				// Note: Since we got the same index but a different Op, this means that the operation at the current index
				// is stale (submitted at an older term, or we this server thinks they are leader, but are not).
				rpcResp_struct.resp_chan <- RPCReturnInfo{success: false, value: "", error: error}
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

// Compare Shards and Groups of two Configs. 
func (kv *ShardKV) getGroupsAndShardsToExchange() (groupsToReceiveFrom map[int]bool, groupsToTransferTo map[int]bool, shardsToTransfer []int) {

	groupsToReceiveFrom = make(map[int]bool)
	groupsToTransferTo = make(map[int]bool)
	

	// Transfer Shard: Current server only sends shards when GID changes from kv.gid to another Gid. 
	//Important Note: If the currentShard_gid is 0, we don't need to send or receive anything (because 0 is an invalid state).
	for k, currentShard_gid := range(kv.committedConfig.Shards) {

		// Error Checking
		if (kv.transitionState.FutureConfig.Shards[k] == 0) {
			kv.DError("Moving towards an initialized shard with #0 as GID. Not taken account in implementation. ")
		}

		// Identify shards to be Transferred. 
		if (currentShard_gid == kv.gid) && (currentShard_gid != 0) {
			if (kv.transitionState.FutureConfig.Shards[k] != kv.gid) {
				// Map of Groups we need to send information to. 
				futureShard_gid := kv.transitionState.FutureConfig.Shards[k]
				groupsToTransferTo[futureShard_gid] = true
				// Index of specific shards that need to be transferred. 
				shardsToTransfer = append(shardsToTransfer, k)
			}
		}

		// Identify groups to receive from 
		if (kv.gid == kv.transitionState.FutureConfig.Shards[k]) && (currentShard_gid != 0) {
			if (currentShard_gid != kv.gid) {
				groupsToReceiveFrom[currentShard_gid] = true
			}
		}
	}

	return groupsToReceiveFrom, groupsToTransferTo, shardsToTransfer
}

func (kv *ShardKV) getKeysToTransfer(shardsToTransfer []int) (map[int]map[string]string) {

	transferMap := make(map[int]map[string]string)

	// Identify keys that needs to be transferred to new GID. 
	// Loop through all keys. 
	for mapKey, mapValue := range(kv.kvMap) {
		// Identify shard that owns key. 
		shardOfKey := key2shard(mapKey)

		// If the shard needs to be moved, store the correspond key/value to be transferred. 
		for _, transferShard := range(shardsToTransfer) {
			
			if (shardOfKey == transferShard) {
				gid_transferTo := kv.transitionState.FutureConfig.Shards[transferShard]

				// Make sure map exists
				if _, ok := transferMap[gid_transferTo]; ok {
					transferMap[gid_transferTo][mapKey] = mapValue
				// If the map doesn't exists
				} else {
					transferMap[gid_transferTo] = make(map[string]string)
					transferMap[gid_transferTo][mapKey] = mapValue
				}

			}

		}
	}

	return transferMap
}

func (kv *ShardKV) getCommitRowsToTransfer(shardsToTransfer []int) (map[int]map[int64]CommitStruct) {

	transferCommitTable := make(map[int]map[int64]CommitStruct)

	// Identify keys that needs to be transferred to new GID. 
	// Loop through all rows on commit table. 
	// Remember: Skip rows with keys that have null val
	for clientID, commitRow := range(kv.lastCommitTable) {

		// If the Key is the null value, we do not send the row. 
		// Skip this row. 
		if (commitRow.Key == "") {
			continue
		}

		// Identify shard that owns key. 
		shardOfKey := key2shard(commitRow.Key)

		// If the shard needs to be moved and the key isnt a null "", store the correspond row to be transferred. 
		for _, transferShard := range(shardsToTransfer) {
			
			if (shardOfKey == transferShard)  {
				gid_transferTo := kv.transitionState.FutureConfig.Shards[transferShard]

				// Make sure map exists
				if _, ok := transferCommitTable[gid_transferTo]; ok {
					transferCommitTable[gid_transferTo][clientID] = commitRow
				// If the map doesn't exists
				} else {
					transferCommitTable[gid_transferTo] = make(map[int64]CommitStruct)
					transferCommitTable[gid_transferTo][clientID] = commitRow
				}

			}

		}
	}

	return transferCommitTable
}


// Select server to send request to.
func (kv *ShardKV) findLeaderServer(si int, gid int) (selectedServer int) {

	selectedServer, ok := kv.currentLeader[gid]
	if (ok && selectedServer != -1){
		return selectedServer
	} else {
		selectedServer = si
		return selectedServer
	}
}

func (kv *ShardKV) transitionCompleteCheck() {

	if len(kv.transitionState.GroupsToReceiveFrom) == 0 {
		kv.transitionState.InTransition = false		
		kv.DPrintf1("Action: CONFIGURATION TRANSITION COMPLETED (all shards received). Previous Configuration => %+v, New Configuration => %+v, Transition Stats => %+v \n", kv.committedConfig, kv.transitionState.FutureConfig, kv.transitionState)		
		kv.committedConfig = kv.transitionState.FutureConfig
	} 
}


//********** UTILITY FUNCTIONS **********//
func (kv *ShardKV) checkIfTransferCommitted(gid_rpc int, configNum int) (committed bool, packageLocation int) {

	// Set default values
	committed = true
	packageLocation = -1

	for key, shardPackage := range(kv.shardTransferStorage) {
		if (shardPackage.GidToSendTo == gid_rpc) && (shardPackage.FutureConfig.Num == configNum) {

			// Error Checking: Ensure that activeTransferRPC has same entry as shardTransferStorage. 
			if (kv.activeTransferRPCs[key].gid_sendTo != shardPackage.GidToSendTo) || (kv.activeTransferRPCs[key].futureNum != shardPackage.FutureConfig.Num)  {
				kv.DError("Transfer arrays have mis-aligned entries: activeTransferRPC and shardTransferStorage. ")
			}

			committed = false
			packageLocation = key
			return committed, packageLocation
		}
	}

	return committed, packageLocation
}


// Send out an RPC (with timeout implemented)
func (kv *ShardKV) sendRPC(srv *labrpc.ClientEnd, function string, goArgs interface{}, goReply interface{}) (ok_out bool){

	RPC_returned := make(chan bool)
	go func() {
		ok := srv.Call(function, goArgs, goReply)

		RPC_returned <- ok
	}()

	//Allows for RPC Timeout
	ok_out = false
	select {
	case <-time.After(time.Millisecond * 250):
	  	ok_out = false
	case ok_out = <-RPC_returned:
	}

	return ok_out
}

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

func (kv *ShardKV) DPrintf2(format string, a ...interface{}) (n int, err error) {
	if kv.debug >= 2 {
		custom_input := make([]interface{},2)
		custom_input[0] = kv.gid
		custom_input[1] = kv.me
		out_var := append(custom_input , a...)
		log.Printf("GID%d, KVServer%d, " + format + "\n", out_var...)
	}
	return
}

func (kv *ShardKV) DPrintf1(format string, a ...interface{}) (n int, err error) {
	if kv.debug >= 1 {
		custom_input := make([]interface{},2)
		custom_input[0] = kv.gid
		custom_input[1] = kv.me
		out_var := append(custom_input , a...)
		log.Printf("GID%d, KVServer%d, " + format + "\n", out_var...)
	}
	return
}

func (kv *ShardKV) DPrintf_now(format string, a ...interface{}) (n int, err error) {
	if kv.debug >= 0 {
		custom_input := make([]interface{},2)
		custom_input[0] = kv.gid
		custom_input[1] = kv.me
		out_var := append(custom_input , a...)
		log.Printf("GID%d, KVServer%d, " + format + "\n", out_var...)
	}
	return
}

func (kv *ShardKV) DError(format string, a ...interface{}) (n int, err error) {
	if kv.debug >= 0 {
		custom_input := make([]interface{},2)
		custom_input[0] = kv.gid
		custom_input[1] = kv.me
		out_var := append(custom_input , a...)
		panic_out := fmt.Sprintf("GID%d, KVServer%d, " + format + "\n", out_var...)
		log.Fatalf(panic_out)
	}
	return
}

func (kv *ShardKV) Locking(locked bool) {
	if (locked) {
		log.Printf("GID%d, KVServer%d: Now Acquiring Lock \n", kv.gid, kv.me)
	} else if (!locked) {
		log.Printf("GID%d, KVServer%d: Now Releasing Lock \n", kv.gid, kv.me)
	}
}