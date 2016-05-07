package shardmaster


import "raft"
import "labrpc"
import "sync"
import "encoding/gob"
import "log"
import "fmt"
import "sort"

type ShardMaster struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	debug	int
	applyCh chan raft.ApplyMsg

	// Your data here.
	waitingForRaft_queue 	map[int]RPCResp
	lastCommitTable 		map[int64]CommitStruct
	shutdownChan 			chan int

	configs []Config // indexed by config num
}

type CommitStruct struct {
	RequestID   int64
	ReturnValue Config
}

type RPCResp struct {
	resp_chan chan RPCReturnInfo
	Op        Op
}

type RPCReturnInfo struct {
	success bool
	value   Config
}

// Note: If add to this structure, update compareOp as well. 
type Op struct {
	// Your data here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	CommandType OpType
	Leave_GIDs			[]int
	Join_Servers 		map[int][]string
	Move_GID			int
	Move_Shard			int
	Query_Num 			int //desired configuration number
	ClientID    		int64
	RequestID   		int64
}

// Human readable operation
type OpType int

const (
	Join   	OpType = 1
	Leave   OpType = 2
	Move 	OpType = 3
	Query 	OpType = 4
)


func (sm *ShardMaster) Join(args *JoinArgs, reply *JoinReply) {
	// Your code here.
	sm.DPrintf2("Action: Received Join Request, JoinArgs => %+v", args)

	// Convert GetArgs into  Op Struct
	thisOp := Op{
		CommandType: 	Join,
		Join_Servers: 	args.Servers,
		ClientID:    	args.ClientID,
		RequestID: 		args.RequestID}

	reply_gen := sm.handleMutationRequest(thisOp, "JOIN")

	*reply = JoinReply(reply_gen.(GenericReply))

}

func (sm *ShardMaster) Leave(args *LeaveArgs, reply *LeaveReply) {
	// Your code here.
	sm.DPrintf2("Action: Received Leave Request, JoinArgs => %+v", args)

	// 1) Convert GetArgs into  Op Struct
	thisOp := Op{
		CommandType: Leave,
		Leave_GIDs:	 args.GIDs,
		ClientID:    args.ClientID,
		RequestID:   args.RequestID}

	reply_gen := sm.handleMutationRequest(thisOp, "LEAVE")

	*reply = LeaveReply(reply_gen.(GenericReply))
	
}

func (sm *ShardMaster) Move(args *MoveArgs, reply *MoveReply) {
	// Your code here.
	sm.DPrintf2("Action: Received Move Request, MoveArgs => %+v", args)

	// 1) Convert GetArgs into  Op Struct
	thisOp := Op{
		CommandType: Move,
		Move_Shard:	args.Shard,
		Move_GID:	args.GID,
		ClientID:   args.ClientID,
		RequestID:  args.RequestID}

	reply_gen := sm.handleMutationRequest(thisOp, "MOVE")

	*reply = MoveReply(reply_gen.(GenericReply))
}

func (sm *ShardMaster) Query(args *QueryArgs, reply *QueryReply) {
	// Your code here.
	sm.DPrintf2("Action: Received Query Request, QueryArgs => %+v", args)

	// 1) Convert GetArgs into  Op Struct
	thisOp := Op{
		CommandType: 	Query,
		Query_Num: 		args.Num,
		ClientID:    	args.ClientID,
		RequestID:   	args.RequestID}

	sm.mu.Lock()


	// Determine if RequestID has already been committed, or is the next request to commit.
	inCommitTable, returnValue := sm.checkCommitTable_beforeRaft(thisOp)

	// Return RPC with correct value: If the client is known, and this server already processed the RPC, send the return value to the client.
	// Note: Even if Server is not leader, respond with the information client needs for this specific request.
	if inCommitTable {

		sm.DPrintf2("Action: REPEAT REQUEST. QUERY ALREADY APPLIED. Respond to client.\n")
		// Note: At this point, it's unkown if this server is the leader.
		// However, if the server has the right information for the client, send it.
		reply.WrongLeader = false
		reply.Config = returnValue // Return the value already committed.
		reply.Err = OK

		// Unlock on any reply
		sm.mu.Unlock()

		// Process the next valid RPC Request with Start(): If 1) client isn't known or 2) the request follows the request already committed.
	} else if !inCommitTable {

		// Unlock before Start (so Start can be in parallel)
		sm.mu.Unlock()

		// 2) Send Op Struct to Raft (using kv.rf.Start())
		index, _, isLeader := sm.rf.Start(thisOp)

		sm.mu.Lock()

		// 3) Handle Response: If Raft is not leader, return appropriate reply to client
		if !isLeader {

			sm.DPrintf2("Action: Rejected Query. masterServer%d not Leader.  \n", sm.me)

			reply.WrongLeader = true
			reply.Config = Config{}
			reply.Err = OK

			// Unlock on any reply
			sm.mu.Unlock()

			// 4) Handle Response: If Raft is leader, wait until raft committed Op Struct to log
		} else if isLeader {

			sm.DPrintf1("Action:  masterServer%d is Leader. Sent QUERY Request to Raft. Index => %d, Configuration => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n", sm.me, index, sm.configs, thisOp, sm.lastCommitTable, sm.waitingForRaft_queue)

			resp_chan := sm.updateRPCTable(thisOp, index)

			// Unlock before response Channel. Since response channel blocks, need to allow other threads to run.
			sm.mu.Unlock()

			// Wait until: 1) Raft indicates that RPC is successfully committed, 2) this server discovers it's not the leader, 3) failure
			rpcReturnInfo, open := <-resp_chan

			// Note: Locking possible here since every RPC created should only receive a single write on the Resp channel.
			// Once it receives the response, just wait until scheduled to run.
			// Error if we receive two writes on the same channel.
			sm.mu.Lock()
			// Successful commit indicated by Raft: Respond to Client
			if rpcReturnInfo.success && open {
				sm.DPrintf1("Action: QUERY APPLIED. Respond to client. Index => %d, Configuration => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n \n", index, sm.configs, thisOp, sm.lastCommitTable, sm.waitingForRaft_queue)
				reply.WrongLeader = false
				reply.Config = rpcReturnInfo.value // Return value from key. Returns "" if key doesn't exist
				reply.Err = OK

				// Commit Failed: If this server discovers it's not longer the leader, then tell the client to find the real leader,
				// and retry with the request there.
			} else if !open || !rpcReturnInfo.success {
				sm.DPrintf1("Action: QUERY ABORTED. Respond to client. Index => %d, Configuration => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n \n", index, sm.configs, thisOp, sm.lastCommitTable, sm.waitingForRaft_queue)
				reply.WrongLeader = true
				reply.Err = OK

			}
			sm.mu.Unlock()
		}
	}


	sm.DPrintf1("Action: Received Query Request, QueryArgs => %+v, QueryReply => %+v \n" , args, reply)

}


//
// the tester calls Kill() when a ShardMaster instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (sm *ShardMaster) Kill() {
	sm.rf.Kill()
	// Your code here, if desired.

	// Note: While the serve is being killed, should not handle any other incoming RPCs, etc.
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.DPrintf1("Action: Server%d Dies \n", sm.me)

	// Kill all open go routines when this server quites.
	close(sm.shutdownChan)

	// Since this server will no longer be leader on resstart, return all outstanding PutAppend and Get RPCs.
	// Allows respective client to find another leader.
	sm.killAllRPCs()

	// Turn off debuggin output
	sm.debug = -1
}

// needed by shardkv tester
func (sm *ShardMaster) Raft() *raft.Raft {
	return sm.rf
}

//
// servers[] contains the ports of the set of
// servers that will cooperate via Paxos to
// form the fault-tolerant shardmaster service.
// me is the index of the current server in servers[].
//
func StartServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister) *ShardMaster {
	sm := new(ShardMaster)
	sm.me = me
	sm.debug = -1

	sm.configs = make([]Config, 1)
	sm.configs[0].Groups = map[int][]string{}

	// Register all interface and exported variables. 
	gob.Register(Op{})
	gob.Register(Config{})
	gob.Register(JoinArgs{})
	gob.Register(JoinReply{})
	gob.Register(LeaveArgs{})
	gob.Register(LeaveReply{})
	gob.Register(MoveArgs{})
	gob.Register(MoveReply{})
	gob.Register(QueryArgs{})
	gob.Register(QueryReply{})
	gob.Register(GenericReply{})
	gob.Register(CommitStruct{})



	sm.applyCh = make(chan raft.ApplyMsg)
	sm.rf = raft.Make(servers, me, persister, sm.applyCh)

	// Your code here.
	sm.DPrintf1("Action: New Master Server Started." )

	// Create mutex for locking Master Server
	sm.mu = sync.Mutex{}
	// Note: This locking probably not necessary, but included for saftey
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Initialize the Shards to Group0 within Configuration 0. 
	for k := range(sm.configs[0].Shards) {
		sm.configs[0].Shards[k] = 0
	}

	// Creates Queue to keep track of outstanding Client RPCs (while waiting for Raft to process request)
	sm.waitingForRaft_queue = make(map[int]RPCResp)

	// Creates commitTable (to store last RPC committed for each Client)
	sm.lastCommitTable = make(map[int64]CommitStruct)

	// Used to shutdown go routines when service is killed.
	sm.shutdownChan = make(chan int)

	go sm.processCommits()

	return sm
}

//********** KV Server FUNCTIONS (non-RPC) **********//

func (sm *ShardMaster) processCommits() {

		// Go routine that loops until server is shutdown.
	for {
		
		select {
		case commitMsg := <-sm.applyCh:

			// Lock the entire code-set that handles returned Operations from applyCh
			sm.mu.Lock()

			sm.DPrintf2("State: State before Receiving OP on applyCh. Configuration => %+v, RPC_Queue => %+v, CommitTable %+v \n", sm.configs, sm.waitingForRaft_queue, sm.lastCommitTable)
			

			// Type Assert: Package the Command from ApplyMsg into an Op Struct
			thisCommand := commitMsg.Command.(Op)

				commitExists, returnValue := sm.checkCommitTable_afterRaft(thisCommand)

				// Raft Op is next request: Execute the commmand
				if !commitExists {

					// Execute Query Request
					if thisCommand.CommandType == Query {

						// If the requested configuration number is too large or is -1,
						// then set the requested configuration to the max possible. 
						if (thisCommand.Query_Num == -1) || (thisCommand.Query_Num > len(sm.configs)-1) {
							thisCommand.Query_Num = len(sm.configs)-1
						}

						sm.DPrintf2("Action: Processing Query from Raft. sm.configs => %+v \n", sm.configs)

						// Exectue operation:
						newValue := sm.configs[thisCommand.Query_Num]

						// Update the value to be returnd
						returnValue = newValue

					// Execute Join Request
					}  else if thisCommand.CommandType == Join {
						// Exectue operation:
						newConfig := Config{}
						prevConfigNum := sm.configs[len(sm.configs)-1].Num
						newConfig.Num = prevConfigNum + 1
						newConfig.Groups = map[int][]string{}

						// Copy mapping of previous Groups. 
						for gid, server_list := range sm.configs[prevConfigNum].Groups {

							newConfig.Groups[gid] = server_list
						}

						// Add new groups
						for gid, server_list := range(thisCommand.Join_Servers) {
							// Checking for possible error
							if (gid == -1) {
								sm.DError("GID of -1 received. We use GID of -1 to indicate invalid GID.")
							}
							if (gid == 0) {
								sm.DError("GID of 0 received. We use GID of 0 to indicate invalid GID.")
							}
							newConfig.Groups[gid] = server_list
						}

						sm.DPrintf2("State: State before shard re-distribution. Prev Shards => %+v, Prev Groups => %+v \n",  sm.configs[prevConfigNum].Shards, sm.configs[prevConfigNum].Groups)
						newConfig.Shards = sm.distributeShards(sm.configs[prevConfigNum].Shards, newConfig.Groups)
						sm.DPrintf2("State: State after shard distribution. New Shards => %+v, New Groups => %+v \n",  newConfig.Shards, newConfig.Groups)

						sm.configs = append(sm.configs, newConfig)

						// Set return value
						returnValue = Config{}

					// Execute Leave Request
					} else if thisCommand.CommandType == Leave {
						// Exectue operation:
						newConfig := Config{}
						prevConfigNum := sm.configs[len(sm.configs)-1].Num
						newConfig.Num = prevConfigNum + 1
						newConfig.Groups = map[int][]string{}

						// Copy mapping of previous Groups. 
						for gid, server_list := range sm.configs[prevConfigNum].Groups {
							newConfig.Groups[gid] = server_list
						}

						// Copy previous shard distribution into new shard distribution. 
						var tempShards [NShards]int
						copy(tempShards[0:NShards-1], sm.configs[prevConfigNum].Shards[0:NShards-1])

						// Remove Groups
						for _, leave_gid := range(thisCommand.Leave_GIDs) {
							delete(newConfig.Groups, leave_gid)

							// Set all removed GIDs to invalid group 0, so they can be re-distributed. 
							for k, shard_gid := range(tempShards) {
								if (shard_gid == leave_gid) {
									tempShards[k] = 0
								}
							}
						}

						sm.DPrintf2("State: State before shard re-distribution. Prev Shards => %+v, Prev Groups => %+v \n",  sm.configs[prevConfigNum].Shards, sm.configs[prevConfigNum].Groups)
						newConfig.Shards = sm.distributeShards(tempShards, newConfig.Groups)
						sm.DPrintf2("State: State after shard distribution. New Shards => %+v, New Groups => %+v \n",  newConfig.Shards, newConfig.Groups)

						sm.configs = append(sm.configs, newConfig)

						// Set return value
						returnValue = Config{}

					// Execute Move Request
					} else if thisCommand.CommandType == Move {
						// Exectue operation:
						newConfig := Config{}
						prevConfigNum := sm.configs[len(sm.configs)-1].Num
						newConfig.Num = prevConfigNum + 1
						newConfig.Groups = map[int][]string{}

						// Copy mapping of previous Groups. 
						for gid, server_list := range sm.configs[prevConfigNum].Groups {
							newConfig.Groups[gid] = server_list
						}

						// Copy previous shard distribution into new shard distribution. 
						var newShards [NShards]int
						copy(newShards[0:NShards-1], sm.configs[prevConfigNum].Shards[0:NShards-1])

						// Error Checking
						if thisCommand.Move_GID == 0 {
							sm.DError("Moved forced a Shard to Invalid GID #0.")
						}

						// Move Shards
						newShards[thisCommand.Move_Shard] = thisCommand.Move_GID

						// Update configs
						newConfig.Shards = newShards
						sm.configs = append(sm.configs, newConfig)

						// Set return value
						returnValue = Config{}
					}

					// Update commitTable
					sm.lastCommitTable[thisCommand.ClientID] = CommitStruct{RequestID: thisCommand.RequestID, ReturnValue: returnValue}
				}

				// If there is an outstanding RPC, return the appropriate value.
				sm.handleOpenRPCs(commitMsg.Index, thisCommand, returnValue)

			sm.DPrintf2("State: State after Receiving OP on applyCh. Configuration => %+v, RPC_Queue => %+v, CommitTable %+v \n",  sm.configs, sm.waitingForRaft_queue, sm.lastCommitTable)

			sm.mu.Unlock()

		case <-sm.shutdownChan:
			return

		}
	}

}

func (sm *ShardMaster) distributeShards(prevShards [NShards]int, newGroups map[int][]string) ([NShards]int) {
	minShardLevel := NShards/len(newGroups)
	maxShardLevel := NShards/len(newGroups) + 1

	// Copy over for saftey (although since prevShards passed in function, already should be a copy)
	var newShards [NShards]int
	copy(newShards[0:NShards-1], prevShards[0:NShards-1])

	for {
		minGID, minShards, maxGID, maxShards := sm.getShardDistribution(newShards, newGroups)

		if (minGID == -1) || (maxGID == -1) {
			sm.DError("No Min or Max Group detected during shard distribution.")
		}

		// Break when shards are correctly distributed
		if (minShards >= minShardLevel) && (maxShards <= maxShardLevel) {
			return newShards
		}

		// Switch a single maxGID with minGID
		for key, gid := range(newShards) {
			if (gid == maxGID) {
				newShards[key] = minGID
				break
			}
		}
	}

}

func (sm *ShardMaster) getShardDistribution(newShards [NShards]int, newGroups map[int][]string) (minGID int, minShards int, maxGID int, maxShards int) {
	countShardsPerGroup := make(map[int]int)

	// Initialize counting map
	for gid := range(newGroups) {
		countShardsPerGroup[gid] = 0
	}

	// Count shards assigned to each group
	for _, gid := range(newShards) {

		countShardsPerGroup[gid] = countShardsPerGroup[gid] + 1
	}


	// Important: Figure out the mapping of the hash
	// Important: Range is not determinsitics. 
	var all_gids []int
	for gid_key := range countShardsPerGroup {
	    all_gids = append(all_gids, gid_key)
	}
	sort.Ints(all_gids)

	//Find max shards assigned to a group
	maxShards = -1
	maxGID = -1
	for _, gid := range(all_gids) {
		count := countShardsPerGroup[gid]

		//Initialize variables (with first values in map)
		if (maxGID == -1) {
			maxGID = gid
			maxShards = count
		}

		// If any shard is assigned to the 0 group, re-distribute it immedately 
		if (gid == 0) {
			maxGID = gid
			// Ensures that we won't break the re-distribution loop
			maxShards = NShards + 1
			break
		}
		if(count > maxShards) {
			maxShards = count
			maxGID = gid
		}

	}

	minShards = NShards + 1
	minGID = -1
	// Edge case: Make sure we never assign a shard to Group 0. 
	for _, gid := range(all_gids) {
		count := countShardsPerGroup[gid]
		//Initialize variables (with first values in map)
		if (minGID == -1) && (gid != 0) {
			minGID = gid
			minShards = count
		}

		if (count < minShards) && (gid != 0) {
			minShards = count
			minGID = gid
		}
	}


	return minGID, minShards, maxGID, maxShards


}

func (sm *ShardMaster) handleMutationRequest(thisOp Op, requestType string) (interface{}) {

	var reply GenericReply

	sm.mu.Lock()
	// Determine if RequestID has already been committed, or is the next request to commit.
	inCommitTable, _ := sm.checkCommitTable_beforeRaft(thisOp)

	// Return RPC with correct value: If the client is known, and this server already processed the RPC, send the return value to the client.
	// Note: Even if Server is not leader, respond with the information client needs for this specific request.
	if inCommitTable {

		sm.DPrintf2("Action: REPEAT REQUEST. %s ALREADY APPLIED. Respond to client.\n", requestType)
		// Note: At this point, it's unkown if this server is the leader.
		// However, if the server has the right information for the client, send it.
		reply.WrongLeader = false
		reply.Err = OK

		// Unlock on any reply
		sm.mu.Unlock()

		// Process the next valid RPC Request with Start(): If 1) client isn't known or 2) the request follows the request already committed.
	} else if !inCommitTable {

		// Unlock before Start (so Start can be in parallel)
		sm.mu.Unlock()

		// 2) Send Op Struct to Raft (using kv.rf.Start())
		index, _, isLeader := sm.rf.Start(thisOp)

		sm.mu.Lock()

		// 3) Handle Response: If Raft is not leader, return appropriate reply to client
		if !isLeader {

			sm.DPrintf2("Action: Rejected %s. masterServer%d not Leader.  \n", requestType, sm.me)

			reply.WrongLeader = true
			reply.Err = OK

			// Unlock on any reply
			sm.mu.Unlock()

			// 4) Handle Response: If Raft is leader, wait until raft committed Op Struct to log
		} else if isLeader {

			sm.DPrintf1("Action: masterServer%d is Leader. Sent %s Request to Raft. Index => %d, Configuration => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n", sm.me, requestType, index, sm.configs, thisOp, sm.lastCommitTable, sm.waitingForRaft_queue)

			resp_chan := sm.updateRPCTable(thisOp, index)

			// Unlock before response Channel. Since response channel blocks, need to allow other threads to run.
			sm.mu.Unlock()

			// Wait until: 1) Raft indicates that RPC is successfully committed, 2) this server discovers it's not the leader, 3) failure
			rpcReturnInfo, open := <-resp_chan

			// Note: Locking possible here since every RPC created should only receive a single write on the Resp channel.
			// Once it receives the response, just wait until scheduled to run.
			// Error if we receive two writes on the same channel.
			sm.mu.Lock()
			// Successful commit indicated by Raft: Respond to Client
			if rpcReturnInfo.success && open {
				sm.DPrintf1("Action: %s APPLIED. Respond to client. Index => %d, Configuration => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n \n",requestType, index, sm.configs, thisOp, sm.lastCommitTable, sm.waitingForRaft_queue)
				reply.WrongLeader = false
				reply.Err = OK

				// Commit Failed: If this server discovers it's not longer the leader, then tell the client to find the real leader,
				// and retry with the request there.
			} else if !open || !rpcReturnInfo.success {
				sm.DPrintf1("Action: %s ABORTED. Respond to client. Index => %d, Configuration => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n \n",requestType, index, sm.configs, thisOp, sm.lastCommitTable, sm.waitingForRaft_queue)
				reply.WrongLeader = true
				reply.Err = OK

			}
			sm.mu.Unlock()
		}
	}

	return reply
}

func (sm *ShardMaster) checkCommitTable_beforeRaft(thisCommand Op) (inCommitTable bool, returnValue Config) {

	// Set default return values
	inCommitTable = false
	returnValue = Config{}

	// Get previous commit value for this client.
	prevCommitted, ok := sm.lastCommitTable[thisCommand.ClientID]

	// Return RPC with correct value: If the client is known, and this server already processed the RPC, send the return value to the client.
	// Note: Even if Server is not leader, respond with the information client needs for this specific request.
	if ok && (prevCommitted.RequestID == thisCommand.RequestID) {

		// Note: At this point, it's unkown if this server is the leader.
		// However, if the server has the right information for the client, send it.
		inCommitTable = true
		returnValue = prevCommitted.ReturnValue

		// Catch Errors: Received an old RequestID (behind the one already committed)
	} else if ok && (prevCommitted.RequestID > thisCommand.RequestID) {
		sm.DPrintf1("Error: prevCommitted: %+v and thisCommand: %+v \n", prevCommitted, thisCommand)
		sm.DError("Error checkCommitTable_beforeRaft: Based on CommitTable, new RequestID is too old. This can happen if RPC very delayed. \n")
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
		returnValue = Config{}
	}
	return inCommitTable, returnValue
}

// Note: For checking the commitTable, we only need to ckeck 1) client and 2) requestID.
// Since we are looking for duplicates, we don't care about the index (or the raft variables).
// We care just about what has been done for this request for this client.
func (sm *ShardMaster) checkCommitTable_afterRaft(thisCommand Op) (commitExists bool, returnValue Config) {

	// Set default return values
	commitExists = false
	returnValue = Config{}

	// Check commitTable Status
	prevCommitted, ok := sm.lastCommitTable[thisCommand.ClientID]

	// Operation Previously Committed: Don't re-apply, and reply to RPC if necessary
	// Check if: 1) exists in table and 2) if same RequestID
	if ok && (prevCommitted.RequestID == thisCommand.RequestID) {
		commitExists = true
		returnValue = prevCommitted.ReturnValue

		// Out of Order RPCs: Throw Error
	} else if ok && ((prevCommitted.RequestID > thisCommand.RequestID) || prevCommitted.RequestID+1 < thisCommand.RequestID) {
		sm.DError("Error checkCommitTable_afterRaft: Out of order commit reached the commitTable. \n")
		// Right error since correctness means we cannot have out of order commits.

		// Operation never committed: Execute operation, and reply.
		// Enter if: 1) Client never committed before or 2) the requestID is the next expected id
	} else if (!ok) || (prevCommitted.RequestID+1 == thisCommand.RequestID) {
		commitExists = false
		returnValue = Config{}

	}

	return commitExists, returnValue

}


// After inputting operation into Raft as leader
func (sm *ShardMaster) updateRPCTable(thisOp Op, raftIndex int) chan RPCReturnInfo {

	// Build RPCResp structure
	// Build channel to flag this RPC to return once respective index is committed.
	new_rpcResp_struct := RPCResp{
		resp_chan: make(chan RPCReturnInfo),
		Op:        thisOp}

	// Quary the queue to determine if RPC already in table.
	old_rpcResp_struct, ok := sm.waitingForRaft_queue[raftIndex]

	// If the index is already in queue
	if ok {

		sameOp := compareOp(new_rpcResp_struct.Op, old_rpcResp_struct.Op)

		// Operations Different: Return the old RPC Request. Enter the new RPC request into table.
		if !sameOp {

			// Return old RPC Request with failure
			// Note: Since we got the same index but a different Op, this means that the operation at the current index
			// is stale (submitted at an older term).
			old_rpcResp_struct.resp_chan <- RPCReturnInfo{success: false, value: Config{}}
			//Enter new RPC request into table
			sm.waitingForRaft_queue[raftIndex] = new_rpcResp_struct

			// Same index and same operation: Only possible when same server submits the same request twice, but in different terms and
			// with the old request somehow deleted from the log during the term switch.
		} else {
			// Possible when: Server submits an Operation, and then crashes. The operation is then
			sm.DPrintf_now("Warning: Server recieved the same operation with the same index assigned by raft. This is possible, but unlikely. \n")
			// The only way the same client can send a new response is if the other RPC returned. So, we replaced the old request
			// We know it's the same client since we compare the operation (which includes the client).
			sm.waitingForRaft_queue[raftIndex] = new_rpcResp_struct
		}

		// If index doesn't exist: just add new RPCResp_struct
	} else {
		sm.waitingForRaft_queue[raftIndex] = new_rpcResp_struct
	}

	sm.DPrintf2("RPCTable  before raft: %+v \n", sm.waitingForRaft_queue)
	return new_rpcResp_struct.resp_chan

}

func (sm *ShardMaster) handleOpenRPCs(raftIndex int, raftOp Op, valueToSend Config) {

	//Find all open RPCs at this index. Return the appropriate value.
	for index, rpcResp_struct := range sm.waitingForRaft_queue {

		if index == raftIndex {
			sameOp := compareOp(rpcResp_struct.Op, raftOp)

			// Found the correct RPC at index
			if sameOp {

				// Respond to Client with success
				rpcResp_struct.resp_chan <- RPCReturnInfo{success: true, value: valueToSend}
				// Delete index
				delete(sm.waitingForRaft_queue, index)

				// Found different RPC at inex
			} else {

				// Respond to Client with failure
				// Note: Since we got the same index but a different Op, this means that the operation at the current index
				// is stale (submitted at an older term, or we this server thinks they are leader, but are not).
				rpcResp_struct.resp_chan <- RPCReturnInfo{success: false, value: Config{}}
				delete(sm.waitingForRaft_queue, index)

			}
		}
	}
}

func (sm *ShardMaster) killAllRPCs() {

	for index, rpcResp_struct := range sm.waitingForRaft_queue {
		// Send false on every channel so every outstanding RPC can return, indicating client to find another leader.
		sm.DPrintf2("Kill Index: %d \n", index)
		rpcResp_struct.resp_chan <- RPCReturnInfo{success: false, value: Config{}}
		delete(sm.waitingForRaft_queue, index)

	}

}


//********** UTILITY FUNCTIONS **********//
func compareOp(op1 Op, op2 Op) (sameOp bool) {

	if (op1.CommandType == op2.CommandType) && (op1.ClientID == op2.ClientID) && (op1.RequestID == op2.RequestID) {
		sameOp = true
	} else {
		sameOp = false
	}
	return sameOp
}

func (sm *ShardMaster) DPrintf2(format string, a ...interface{}) (n int, err error) {
	if sm.debug >= 2 {
		custom_input := make([]interface{},1)
		custom_input[0] = sm.me
		out_var := append(custom_input , a...)
		log.Printf("masterServer%d, " + format + "\n", out_var...)
	}
	return
}

func (sm *ShardMaster) DPrintf1(format string, a ...interface{}) (n int, err error) {
	if sm.debug >= 1 {
		custom_input := make([]interface{},1)
		custom_input[0] = sm.me
		out_var := append(custom_input , a...)
		log.Printf("masterServer%d, " + format + "\n", out_var...)
	}
	return
}

func (sm *ShardMaster) DPrintf_now(format string, a ...interface{}) (n int, err error) {
	if sm.debug >= 0{
		custom_input := make([]interface{},1)
		custom_input[0] = sm.me
		out_var := append(custom_input , a...)
		log.Printf("masterServer%d, " + format + "\n", out_var...)
	}
	return
}

func (sm *ShardMaster) DError(format string, a ...interface{}) (n int, err error) {
	if sm.debug >= 0 {
		custom_input := make([]interface{},1)
		custom_input[0] = sm.me
		out_var := append(custom_input , a...)
		panic_out := fmt.Sprintf("masterServer%d, " + format + "\n", out_var...)
		panic(panic_out)
	}
	return
}
