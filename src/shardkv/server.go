package shardkv


import "shardmaster"
import "labrpc"
import "raft"
import "sync"
import "encoding/gob"
import "time"

// Human readable
type OpType int

const (
	Get    				OpType = 1
	Put    				OpType = 2
	Append 				OpType = 3
	Configuration 		OpType = 4
	ShardTransfer 		OpType = 5
	TransferSuccess 	OpType = 6
)

type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	CommandType 		OpType
	Key         		string
	Value       		string
	ClientID    		int64
	RequestID   		int64
	Config 				shardmaster.Config
	Shards 				map[string]string
	LastCommitTable  	map[int64]CommitStruct
}

type CommitStruct struct {
	RequestID   int64
	ReturnValue string
	Key 		string
}

type RPCReturnInfo struct {
	success bool
	value   string
	error 	Err
}

type RPCResp struct {
	resp_chan chan RPCReturnInfo
	Op        Op
}

type TransitionState struct {
	InTransition			bool
	// Tracks groups from which we needs keys from.
	GroupsToReceiveFrom		map[int]bool
	// Transition to this configuration. 
	FutureConfig 			shardmaster.Config
}


type ShardPackage struct {
	GidToSendTo 			int
	TransferKeys 			map[string]string
	TransferCommitTable 	map[int64]CommitStruct
	FutureConfig  			shardmaster.Config
}

type TransferRPCs struct {
	active 			bool
	gid_sendTo 		int
	futureNum		int
}

type ShardKV struct {
	mu           sync.Mutex
	me           int
	rf           *raft.Raft
	applyCh      chan raft.ApplyMsg
	make_end     func(string) *labrpc.ClientEnd
	gid          int
	masters      []*labrpc.ClientEnd
	maxraftstate int // snapshot if log grows this big

	// Your definitions here.
	persister 				*raft.Persister
	debug 					int
	shutdownChan         	chan int
	waitingForRaft_queue 	map[int]RPCResp
	kvMap           		map[string]string
	lastCommitTable 		map[int64]CommitStruct
	mck 					*shardmaster.Clerk
	committedConfig   		shardmaster.Config
	currentLeader 			map[int]int
	transitionState			TransitionState
	shardTransferStorage 	[]ShardPackage
	// Track which RPCs to transfer shards are active. 
	activeTransferRPCs		[]TransferRPCs
}


func (kv *ShardKV) Get(args *GetArgs, reply *GetReply) {
	// Your code here.
	kv.mu.Lock()
	kv.Locking(true)

	// Transition Check: If server is in transition, automatically reject incoming RPCs (they will just clog the Raft log)
	if (kv.transitionState.InTransition) {
		reply.WrongLeader = true
		reply.Err = OK
		kv.mu.Unlock()
		kv.Locking(false)
		return
	}

	// Shard Check: Check if group owns the incoming key. 
	thisShard := key2shard(args.Key)
	if (kv.committedConfig.Shards[thisShard] != kv.gid) {
		reply.WrongLeader = true
		reply.Err = ErrWrongGroup
		kv.mu.Unlock()
		kv.Locking(false)
		return
	}

	// Convert GetArgs into  Op Struct
	thisOp := Op{
		CommandType: Get,
		Key:         args.Key,
		ClientID:    args.ClientID,
		RequestID:   args.RequestID}


	// Determine if RequestID has already been committed, or is the next request to commit.
	inCommitTable, returnValue := kv.checkCommitTable(thisOp)

	// Return RPC with correct value: If the client is known, and this server already processed the RPC, send the return value to the client.
	// Note: Even if Server is not leader, respond with the information client needs for this specific request.
	if inCommitTable {

		kv.DPrintf1("Action: REPEAT REQUEST. GET ALREADY APPLIED. Respond to client.   \n")
		// Note: At this point, it's unkown if this server is the leader.
		// However, if the server has the right information for the client, send it.
		reply.WrongLeader = false
		reply.Value = returnValue // Return the value already committed.
		reply.Err = OK
		kv.mu.Unlock()
		kv.Locking(false)
		return

		// Process the next valid RPC Request with Start(): If 1) client isn't known or 2) the request follows the request already committed.
	} else if !inCommitTable {

		// Unlock before Start (so Start can be in parallel)
		kv.mu.Unlock()
		kv.Locking(false)

		// 2) Send Op Struct to Raft (using kv.rf.Start())
		index, _, isLeader := kv.rf.Start(thisOp)

		kv.mu.Lock()
		kv.Locking(true)

		// 3) Handle Response: If Raft is not leader, return appropriate reply to client
		if !isLeader {

			kv.DPrintf2("Action: Rejected GET. KVServer%d not Leader.  \n", kv.me)

			reply.WrongLeader = true
			reply.Value = "" // Return value from key. Returns "" if key doesn't exist
			reply.Err = OK
			kv.mu.Unlock()
			kv.Locking(false)
			return

			// 4) Handle Response: If Raft is leader, wait until raft committed Op Struct to log
		} else if isLeader {

			kv.DPrintf1("%s \n Action:  KVServer%d is Leader. Sent GET Request to Raft. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v\n", debug_break, kv.me, index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue)

			resp_chan := kv.updateRPCTable(thisOp, index)

			// Unlock before response Channel. Since response channel blocks, need to allow other threads to run.
			kv.mu.Unlock()
			kv.Locking(false)

			// Wait until: 1) Raft indicates that RPC is successfully committed, 2) this server discovers it's not the leader, 3) failure
			rpcReturnInfo, open := <-resp_chan

			// Note: Locking possible here since every RPC created should only receive a single write on the Resp channel.
			// Once it receives the response, just wait until scheduled to run.
			// Error if we receive two writes on the same channel.
			kv.mu.Lock()
			kv.Locking(true)
			// Operation successfully executed. 
			if rpcReturnInfo.success && open && rpcReturnInfo.error == OK {
				kv.DPrintf1("Action: GET APPLIED. Respond to client. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n %s \n \n", index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue, debug_break)
				reply.WrongLeader = false
				reply.Value = rpcReturnInfo.value // Return value from key. Returns "" if key doesn't exist
				reply.Err = OK
				kv.mu.Unlock()
				kv.Locking(false)
				return

			// Op Failed to execute properly
			} else if !open || !rpcReturnInfo.success {

				// 1) If this server discovers it's not longer the leader, then tell the client to find the real leader. 
				if (!open || rpcReturnInfo.error == OK) {
					kv.DPrintf1("Action: GET ABORTED since no longer leader. Respond to client. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n %s \n \n", index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue, debug_break)
					reply.WrongLeader = true
					reply.Err = rpcReturnInfo.error
					kv.mu.Unlock()
					kv.Locking(false)
					return
				// 2) If this server discovers it doesn't own the key, tell client to find correct group. 
				} else if (rpcReturnInfo.error == ErrWrongGroup) {
					kv.DPrintf1("Action: GET ABORTED since ErrWrongGroup. Respond to client. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n %s \n \n", index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue, debug_break)
					reply.WrongLeader = false
					reply.Err = rpcReturnInfo.error
					kv.mu.Unlock()
					kv.Locking(false)
					return
				}

			// RPC Channel closed: Server no longer is leader. 
			} 
		}
	}

	//Error Checking
	kv.DError("Error in Get RPC. Should never return at end of function. ")

}

func (kv *ShardKV) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	// Your code here.
	kv.mu.Lock()
	kv.Locking(true)

	// Transition Check: If server is in transition, automatically reject incoming RPCs (they will just clog the Raft log)
	if (kv.transitionState.InTransition) {
		reply.WrongLeader = true
		reply.Err = OK
		kv.mu.Unlock()
		kv.Locking(false)
		return
	}

	// Shard Check: Check if group owns the incoming key. 
	thisShard := key2shard(args.Key)
	if (kv.committedConfig.Shards[thisShard] != kv.gid) {
		reply.WrongLeader = true
		reply.Err = ErrWrongGroup
		kv.mu.Unlock()
		kv.Locking(false)
		return
	}

	// 1) Convert PutAppendArgs into  Op Struct. Include ClientId and RequestID so Servers can update their commitTable
	thisOp := Op{
		CommandType: kv.stringToOpType(args.Op),
		Key:         		args.Key,
		Value:       		args.Value,
		ClientID:    		args.ClientID,
		RequestID:   		args.RequestID}

	// Determine if RequestID has already been committed, or is the next request to commit.
	inCommitTable, _ := kv.checkCommitTable(thisOp)

	// Return RPC with correct value: If the client is known, and this server already processed the RPC, send the return value to the client.
	// Note: Even if Server is not leader, respond with the information client needs for this specific request.
	if inCommitTable {

		kv.DPrintf1("Action: REPEAT REQUEST. PUTAPPEND ALREADY APPLIED. Respond to client.   \n")
		// Note: At this point, it's unkown if this server is the leader.
		// However, if the server has the right information for the client, send it.
		reply.WrongLeader = false
		reply.Err = OK
		kv.mu.Unlock()
		kv.Locking(false)
		return

	// Process the next valid RPC Request with Start(): If 1) client isn't known or 2) the request follows the request already committed.
	} else if !inCommitTable {

		// Unlock before Start (so Start can be in parallel)
		kv.mu.Unlock()
		kv.Locking(false)

		// 2) Send Op Struct to Raft (using kv.rf.Start())
		index, _, isLeader := kv.rf.Start(thisOp)

		kv.mu.Lock()
		kv.Locking(true)

		// 3) Handle Response: If Raft is not leader, return appropriate reply to client
		if !isLeader {

			kv.DPrintf2("Action: Rejected PutAppend. KVServer%d not Leader.  \n", kv.me)
			reply.WrongLeader = true
			reply.Err = OK
			kv.mu.Unlock()
			kv.Locking(false)
			return

			// Unlock on any reply
			kv.mu.Unlock()
			kv.Locking(false)

			// 4) Handle Response: If Raft is leader, wait until raft committed Op Struct to log
		} else if isLeader {
			kv.DPrintf1("%s \n Action:  KVServer%d is Leader. Sent PUTAPPEND Request to Raft. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v\n", debug_break, kv.me, index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue)

			resp_chan := kv.updateRPCTable(thisOp, index)

			// Unlock before response Channel. Since response channel blocks, need to allow other threads to run.
			kv.mu.Unlock()
			kv.Locking(false)

			// Wait until: 1) Raft indicates that RPC is successfully committed, 2) this server discovers it's not the leader, 3) failure
			rpcReturnInfo, open := <-resp_chan

			// Note: Locking not possible here since every RPC created should only receive a single write on the Resp channel.
			// Once it receives the response, just wait until scheduled to run.
			// Error if we receive two writes on the same channel.
			kv.mu.Lock()
			kv.Locking(true)
			// Successful commit indicated by Raft: Respond to Client
			if rpcReturnInfo.success && open && rpcReturnInfo.error == OK {

				kv.DPrintf1("Action: PUTAPPEND APPLIED. Respond to client. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n %s \n \n",index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue, debug_break)
				reply.WrongLeader = false
				reply.Err = OK
				kv.mu.Unlock()
				kv.Locking(false)
				return

			//OP Failed to Execute
			} else if !open || !rpcReturnInfo.success {

				// 1) If this server discovers it's not longer the leader, then tell the client to find the real leader. 
				if (!open || rpcReturnInfo.error == OK) {
					kv.DPrintf1("Action: PUTAPPEND ABORTED since no longer leader. Respond to client. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n %s \n \n", index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue, debug_break)
					reply.WrongLeader = true
					reply.Err = rpcReturnInfo.error
					kv.mu.Unlock()
					kv.Locking(false)
					return
				// 2) If this server discovers it doesn't own the key, tell client to find correct group. 
				} else if (rpcReturnInfo.error == ErrWrongGroup) {
					kv.DPrintf1("Action: PUTAPPEND ABORTED since ErrWrongGroup. Respond to client. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n %s \n \n", index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue, debug_break)
					reply.WrongLeader = false
					reply.Err = rpcReturnInfo.error
					kv.mu.Unlock()
					kv.Locking(false)
					return
				}
			}
		}
	}

	//Error Checking
	kv.DError("Error in PUTAPPEND RPC. Should never return at end of function. ")
}

// Receive keys from new shard. 
func (kv *ShardKV) AddShardKeys(args *AddShardsArgs, reply *AddShardsReply) {
	kv.mu.Lock()
	kv.Locking(true)
	kv.DPrintf1("Action: Receive AddShardKeys RPC REQUEST: kv.committedConfig => %+v, Args => %+v, kv.transitionState => %+v \n", kv.committedConfig, args, kv.transitionState )

	thisOp := Op{
	CommandType: 		ShardTransfer,
	Shards:      		args.ShardKeys,
	LastCommitTable: 	args.LastCommitTable,
	ClientID: 	 		args.ClientID, 
	// Num of FutureConfig
	RequestID: 	 args.RequestID}

	// Default Value
	reply.WrongLeader = true


	// Return RPC with success: if the clients configuration is ahead. 
	// This happens when the leader has already committed the configuration, but a follower sends an RPC as it's catching up. 
	// Note: Even if Server is not leader, we can respond (since we know the leader must have committed this.)
	if (kv.committedConfig.Num >= int(args.RequestID)) {

		kv.DPrintf1("Action: REPEAT REQUEST. Committed Configuration ahead of received configuration. Respond to client.   \n")
		// Note: At this point, it's unkown if this server is the leader.
		// However, if the server has the right information for the client, send it.
		reply.WrongLeader = false
		kv.mu.Unlock()
		kv.Locking(false)
		return


	
	} else {

		// Only put shards into Raft when: the group is transition to the correctin configuration number. 
		if (kv.transitionState.InTransition) && (kv.committedConfig.Num + 1 == int(args.RequestID)) {

			// Unlock before Start (so Start can be in parallel)
			kv.mu.Unlock()
			kv.Locking(false)

			// Send Op Struct to Raft (using kv.rf.Start())
			index, _, isLeader := kv.rf.Start(thisOp)
			kv.mu.Lock()
			kv.Locking(true)

			if !isLeader {
				kv.DPrintf2("Action: Rejected AddShardKeys. KVServer%d not Leader.  \n", kv.me)
				reply.WrongLeader = true
				kv.mu.Unlock()
				kv.Locking(false)
				return


			} else if isLeader {
				kv.DPrintf1("Action: KVServer%d is Leader. Sent SHARDTRANSFER REQUEST through Raft. Index => %d, Operation => %+v \n", kv.me, index, thisOp)
				

				resp_chan := kv.updateRPCTable(thisOp, index)

				// Unlock before response Channel. Since response channel blocks, need to allow other threads to run.
				kv.mu.Unlock()
				kv.Locking(false)

				// Wait until: 1) Raft indicates that RPC is successfully committed, 2) this server discovers it's not the leader, 3) failure
				rpcReturnInfo, open := <-resp_chan


				kv.mu.Lock()
				kv.Locking(true)
				// Successful commit indicated by Raft: Respond to Client
				if open && rpcReturnInfo.success {

					kv.DPrintf1("Action: SHARDTRANSFER APPLIED. Respond to client. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n %s \n \n",index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue, debug_break)
					reply.WrongLeader = false
					kv.mu.Unlock()
					kv.Locking(false)
					return

				// Commit Failed: If this server discovers it's no longer the leader, then tell the client to find the real leader,
				// and retry with the request there.
				} else if !open || !rpcReturnInfo.success {

					kv.DPrintf1("Action: SHARDTRANSFER ABORTED. Respond to client. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n %s \n \n", index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue, debug_break)
					reply.WrongLeader = true
					kv.mu.Unlock()
					kv.Locking(false)
					return

				}
			} 
		// Reject if not in correct transition period. 
		} else {
			kv.DPrintf2("Action: Rejected AddShardKeys because KVServer%d not in correct transition Period. kv.committedConfig => %+v, kv.transitionState => %+v  \n", kv.me, kv.committedConfig, kv.transitionState)
			reply.WrongLeader = true
			kv.mu.Unlock()
			kv.Locking(false)
			return
		}
	}

	// Catch errors
	kv.DError("Error in AddShardKeys. Should never return at end of function. ")

}

// Send keys for shard to another group. 
func (kv *ShardKV) sendShardsToGroup(gid int, args AddShardsArgs, futureConfig shardmaster.Config) {

	kv.DPrintf1("Action: Sending keys to Group %d. args => %+v \n", gid, args)


	for {
		// If the gid exists in our current stored configuration. 
		if servers, ok := futureConfig.Groups[gid]; ok {
			// try each server for the shard.
			for si := 0; si < len(servers); si++ {

				kv.mu.Lock()
				kv.Locking(true)

				selectedServer := kv.findLeaderServer(si, gid)
				kv.DPrintf1("sendShardToGroup to server (%s) in gid%d", futureConfig.Groups[gid][selectedServer], gid)
				srv := kv.make_end(futureConfig.Groups[gid][selectedServer])

				kv.mu.Unlock()
				kv.Locking(false)
				
				var reply AddShardsReply
				ok := kv.sendRPC(srv, "ShardKV.AddShardKeys", &args, &reply)

				kv.mu.Lock()
				kv.Locking(true)

				// If Wrong Leader (reset stored leader)
				// If no response, reset leader 
				if !ok || (ok && reply.WrongLeader == true) {
					kv.currentLeader[gid] = -1
				}

				// Success: Shards have been applied. 
				if ok && reply.WrongLeader == false  {
					kv.DPrintf1("Action: Successfully SENT SHARD TO GROUP %d. Sent Args => %+v, Received Reply => %+v \n", gid, args, reply)

					//Update stored Leader
					kv.currentLeader[gid] = selectedServer

					// Check if "Successful Shard Transfer" already committed. 
					committed, packageLocation := kv.checkIfTransferCommitted(gid, futureConfig.Num)



					// If transferSuccess has not been committed, put it into raft. 
					// If it's already committed, just return out of the sending Shard function. 
					if !committed {


						// Indicate in activeTransferRPCs array that we returned from this RPC
						// Note: Only indicate 1) if not committed and 2) while still holding lock from above query. 
						kv.activeTransferRPCs[packageLocation].active = false


						thisOp := Op{}
						thisOp.CommandType = TransferSuccess
						// GID of the group we sent this to. 
						thisOp.ClientID = int64(gid)
						// Num of the future config the above GID is trying to move towards. 
						thisOp.RequestID = args.RequestID

						kv.mu.Unlock()
						kv.Locking(false)
						index, _, isLeader := kv.rf.Start(thisOp)
						kv.mu.Lock()
						kv.Locking(true)

						if (isLeader) {
							kv.DPrintf1("Action: KVServer%d is Leader. Sent TransferSuccess REQUEST through Raft. Index => %d, Operation => %+v \n", kv.me, index, thisOp)
						}


					} else if committed {
						kv.DPrintf1("Action: REPEAT REQUEST. TransferSuccess has alrady been removed from shardTransferStorage. kv.transitionState => %+v, kv.committedConfig => %+v, kv.shardTransferStorage => %+v \n", kv.transitionState, kv.committedConfig, kv.shardTransferStorage)
					}



					kv.mu.Unlock()
					kv.Locking(false)
					return

				}

				kv.mu.Unlock()
				kv.Locking(false)

			}
		}
		// Wait to allow other groups/servers catch up. 
		time.Sleep(100 * time.Millisecond)
	}

	// Error Checking
	kv.DError("Return from sendShardToGroup in ShardKV Server. Should never return from here.")
	return
}

//
// the tester calls Kill() when a ShardKV instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (kv *ShardKV) Kill() {
	kv.rf.Kill()
	// Your code here, if desired.

	// Note: While the serve is being killed, should not handle any other incoming RPCs, etc.
	kv.mu.Lock()
	kv.Locking(true)

	kv.DPrintf1("%s \nAction: Dies \n", debug_break)

	// Kill all open go routines when this server quites.
	close(kv.shutdownChan)

	// Since this server will no longer be leader on resstart, return all outstanding PutAppend and Get RPCs.
	// Allows respective client to find another leader.
	kv.killAllRPCs()

	// Turn off debuggin output
	kv.debug = -1

	kv.mu.Unlock()
	kv.Locking(false)
}


//
// servers[] contains the ports of the servers in this group.
//
// me is the index of the current server in servers[].
//
// the k/v server should store snapshots with
// persister.SaveSnapshot(), and Raft should save its state (including
// log) with persister.SaveRaftState().
//
// the k/v server should snapshot when Raft's saved state exceeds
// maxraftstate bytes, in order to allow Raft to garbage-collect its
// log. if maxraftstate is -1, you don't need to snapshot.
//
// gid is this group's GID, for interacting with the shardmaster.
//
// pass masters[] to shardmaster.MakeClerk() so you can send
// RPCs to the shardmaster.
//
// make_end(servername) turns a server name from a
// Config.Groups[gid][i] into a labrpc.ClientEnd on which you can
// send RPCs. You'll need this to send RPCs to other groups.
//
// look at client.go for examples of how to use masters[]
// and make_end() to send RPCs to the group owning a specific shard.
//
// StartServer() must return quickly, so it should start goroutines
// for any long-running work.
//
func StartServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int, gid int, masters []*labrpc.ClientEnd, make_end func(string) *labrpc.ClientEnd) *ShardKV {
	// call gob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	gob.Register(Op{})
	gob.Register(TransitionState{})
	gob.Register(ShardPackage{})
	gob.Register(CommitStruct{})
	gob.Register(shardmaster.Config{})


	kv := new(ShardKV)
	kv.me = me
	kv.maxraftstate = maxraftstate
	kv.make_end = make_end
	kv.gid = gid
	kv.masters = masters

	// Your initialization code here.
	kv.persister = persister
	kv.debug = 2
	kv.mu = sync.Mutex{}

	kv.mu.Lock()
	kv.Locking(true)

	// Saves current leaders of other GIDs
	kv.currentLeader = make(map[int]int)

	// Records transition state from one configuratino to another
	kv.transitionState = TransitionState{}
	kv.transitionState.InTransition = false

	// Records shards that need to be sent. 
	kv.shardTransferStorage = make([]ShardPackage, 0)
	kv.activeTransferRPCs = make([]TransferRPCs, 0)

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

	// Initiates raft system (built as part of Lab2)
	kv.rf = raft.Make(servers, me, persister, kv.applyCh)

	// Use something like this to talk to the shardmaster:
	kv.mck = shardmaster.MakeClerk(kv.masters)
	// Get initial configuration Num. 
	kv.committedConfig = shardmaster.Config{}

	// Load persisted snapshot (if it exists)
	// For failure recover, raft reads directly from persister. 
	rawSnapshotData := persister.ReadSnapshot()
	if (len(rawSnapshotData) > 0) {
		kv.readPersistSnapshot(rawSnapshotData)
	}

	kv.DPrintf1("%s \n Action: New KV Server and Raft Instance Created. \n", debug_break)

	go kv.processCommits()

	go kv.checkConfiguration()

	go kv.sendShardPackages()

	kv.mu.Unlock()
	kv.Locking(false)
	return kv
}

//********** Helper Functions **********//

func (kv *ShardKV) sendShardPackages() {

	for {
		//Check if Leader
		_, isLeader := kv.rf.GetState()

		kv.mu.Lock()
		kv.Locking(true)

		// Ideally only the leader sends RPCs
		// Note: Once an RPC routine is activated, it will continue until completion. 
		// So, if leader state changes, follower might be first to reach GID (not an issue).
		if (isLeader) {
			// Create an RPC go routine if one hasn't already been created. 
			for k, shardPackage := range(kv.shardTransferStorage) {
				
				// Only create RPC Transfer Routine if one hasn't been created. 
				if (!kv.activeTransferRPCs[k].active) {

					// Error Checking: Ensure that activeTransferRPC has same entry values as shardTransferStorage. 
					if (kv.activeTransferRPCs[k].gid_sendTo != shardPackage.GidToSendTo) || (kv.activeTransferRPCs[k].futureNum != shardPackage.FutureConfig.Num)  {
						kv.DError("Transfer arrays have mis-aligned entries: activeTransferRPC and shardTransferStorage. ")
					}

					args := AddShardsArgs{}
					args.ShardKeys = shardPackage.TransferKeys
					args.LastCommitTable = shardPackage.TransferCommitTable
					args.ClientID 	= int64(kv.gid)
					// Create random requestID to ensure we return to the correct
					args.RequestID = int64(shardPackage.FutureConfig.Num)


					go kv.sendShardsToGroup(shardPackage.GidToSendTo, args, shardPackage.FutureConfig)



				}
			}
		}
		kv.mu.Unlock()
		kv.Locking(false)
		// Allow other processes to operate 
		time.Sleep(100 * time.Millisecond)
	}
}

// Detects and sends new configurations through Raft
func (kv *ShardKV) checkConfiguration() {

	// Go routine that loops until server is shutdown.
	for {
		
		select {
		// Garbage collection. 
		case <-kv.shutdownChan:
			return
		case <-time.After(time.Millisecond * 100):

			kv.mu.Lock()
			kv.Locking(true)
			// Transition Check: If server is in transition, don't clog up the Raft log. 
			if (kv.transitionState.InTransition) {
				kv.DPrintf2("Skip checkConfiguration function since in transition. \n TRANSITION STATE: => %+v, \n SHARD TRANSFER STORAGE => %+v, ", kv.transitionState, kv.shardTransferStorage )
				kv.mu.Unlock()
				kv.Locking(false)
				continue
			}

			nextConfigNum := kv.committedConfig.Num+1


			kv.mu.Unlock()
			kv.Locking(false)
			// Check if a higher configuration exists (configuration that is one higher).
			// Query should not be in lock (so we can handle other processes in parallel. )
			configTemp := kv.mck.Query(nextConfigNum)
			kv.mu.Lock()
			kv.Locking(true)

			// If there is an updated configuration, submit it through Raft. 
			if (configTemp.Num == nextConfigNum) {

				thisOp := Op{
					CommandType: Configuration,
					Config:      configTemp}


				// Unlock before Start (so Start can be in parallel)
				kv.mu.Unlock()
				kv.Locking(false)
				// Send Op Struct to Raft (using kv.rf.Start())
				index, _, isLeader := kv.rf.Start(thisOp)
				kv.mu.Lock()
				kv.Locking(true)

				// For Logging
				if isLeader {

					kv.DPrintf1("%s \n Action: KVServer%d is Leader. Sent CONFIGURATION CHANGE Request to Raft. Index => %d, Operation => %+v \n", debug_break, kv.me, index, thisOp)
				
				} 
			}

			kv.mu.Unlock()
			kv.Locking(false)
		}
	}
}

func (kv *ShardKV) processCommits() {

	// Go routine that loops until server is shutdown.
	for {
		
		select {
		case commitMsg := <-kv.applyCh:    


			// Lock the entire code-set that handles returned Operations from applyCh
			kv.mu.Lock()
			kv.Locking(true)

			kv.DPrintf2("State before Receiving OP on applyCh. commitMsg => %+v, \n CommitTable %+v \n kv.transitionState \n kv.shardTransferStorage \n kv.committedConfig \n", commitMsg, kv.kvMap, kv.lastCommitTable, kv.transitionState, kv.shardTransferStorage,  kv.committedConfig)

			// HANDLE SNAPSHOT
			if (commitMsg.UseSnapshot) {
				kv.readPersistSnapshot(commitMsg.Snapshot)
				kv.DPrintf2("State Machine reset with snapshot. \n\n kv.transitionState => %+v, \n \n kv.committedConfig => %+v \n  \n", kv.transitionState, kv.committedConfig )

				// Just apply the snapshot. This will skip all of the rest of the exectuion, and return to select. 
				kv.mu.Unlock()
				kv.Locking(false)
				continue
			}
			

			// Type Assert: Package the Command from ApplyMsg into an Op Struct
			thisCommand := commitMsg.Command.(Op)


			// Skip operations related to kvMap during configuration transitions 
			if (kv.transitionState.InTransition) && 
				(thisCommand.CommandType == Configuration || thisCommand.CommandType == Get || 
				thisCommand.CommandType == Put || thisCommand.CommandType == Append) {

				kv.DPrintf1("Action: While in-transition state, skipped OP on applyCh. thisCommand => %+v", thisCommand)
				// Delete operation from RPC_Que (since we will not execute it)
				delete(kv.waitingForRaft_queue, commitMsg.Index)
				kv.manageSnapshots(commitMsg.Index, commitMsg.Term)
				kv.mu.Unlock()
				kv.Locking(false)
				continue
			}

			// Shard Check: Check if group owns the incoming key (for Get, Put and Append)
			if (thisCommand.CommandType == Get || thisCommand.CommandType == Put || thisCommand.CommandType == Append) {
		 		
				thisShard := key2shard(thisCommand.Key)
				if (kv.committedConfig.Shards[thisShard] != kv.gid) {
					kv.DPrintf1("Action: Rejected operation since key is not part of this GID. thisCommand => %+v", thisCommand)
					kv.handleOpenRPCs(commitMsg.Index, thisCommand, "", ErrWrongGroup)
					kv.manageSnapshots(commitMsg.Index, commitMsg.Term)
					kv.mu.Unlock()
					kv.Locking(false)
					continue
				}
			}


			// HANDLE ALL OPs 
			if thisCommand.CommandType == Configuration {
				kv.DPrintf1("Received CONFIGURATION OP on ApplyCh. Operation => %+v , commitMsg => %+v, kv.committedConfig => %+v, kv.transitionState => %+v \n", thisCommand, commitMsg, kv.committedConfig, kv.transitionState )
				
				// Before executing, make sure this is the next configuration change that we expect.
				// Similar functionality to the committable. 
				if (kv.committedConfig.Num + 1 == thisCommand.Config.Num) {
					kv.DPrintf2("Received NEXT configuration change. Process the change. \n")


					// Execute Configuration Operation: Update transition state
					kv.transitionState.InTransition = true
					kv.transitionState.FutureConfig = thisCommand.Config

					// Get array of groups to exchange and array of shards that need to be transferred. 
					// Changes from Group0 are not considered shard changes (since no keys need to be moved). 
					groupsToReceiveFrom, groupsToTransferTo, shardsToTransfer := kv.getGroupsAndShardsToExchange()

					// Update transition state
					kv.transitionState.GroupsToReceiveFrom = groupsToReceiveFrom

					// For each gid we transfer to, a key/value map is created. 
					// transferMap is indexed by gid of group we need to send it to. 
					transferMap := kv.getKeysToTransfer(shardsToTransfer)
					transferCommitTable := kv.getCommitRowsToTransfer(shardsToTransfer)


					// Add new ShardPackage to shardsToSend array. 
					// Even if no keys/rows need to be transferred, still need to send an RPC to other group. 
					for gid := range(groupsToTransferTo) {

						newShardPackage := ShardPackage{}
						newShardPackage.GidToSendTo = gid 
						newShardPackage.TransferKeys = transferMap[gid]
						newShardPackage.TransferCommitTable = transferCommitTable[gid]
						newShardPackage.FutureConfig = kv.transitionState.FutureConfig

						newTransferRPCs := TransferRPCs{}
						newTransferRPCs.active = false
						newTransferRPCs.gid_sendTo = gid
						newTransferRPCs.futureNum = kv.transitionState.FutureConfig.Num


						kv.shardTransferStorage = append(kv.shardTransferStorage, newShardPackage)
						kv.activeTransferRPCs = append(kv.activeTransferRPCs, newTransferRPCs)

					}
						

					// In case there were no shards to exchange, perform "transition complete" check 
					kv.transitionCompleteCheck()


				// Note on old configurations: We can receive old configurations through the raft log when the server dies, and needs
				// to reply the raft log. IN this case, the checkConfiguration function still fires, attempting to get the next configuration
				// Since the system doesn't know that we are just replaying the pre-recorded logs, we add this very delayed configuration change into raft. 
				// This will be received by the applyCh, and rejected here. 
				} else if (kv.committedConfig.Num >= thisCommand.Config.Num) {
					kv.DPrintf2("Received REPEAT configuration change. Reject it. \n")

				} else {
					kv.DError("Received a configuration change that's more than one index newer than the committedConfig. Should not be possible. \n\n kv.transitionState => %+v, \n \n kv.committedConfig => %+v \n thisCommand => %+v \n", kv.transitionState, kv.committedConfig, thisCommand)
				}

				kv.manageSnapshots(commitMsg.Index, commitMsg.Term)

			// Execute ShardTransfer Request
			} else if (thisCommand.CommandType == ShardTransfer) {
				// Similar functionality to the commit table. 
				if (kv.transitionState.InTransition) && (int(thisCommand.RequestID) == kv.transitionState.FutureConfig.Num) {
					kv.DPrintf1("Action: Received and processing SHARD TRANSFER OP on ApplyCh. commitMsg => %+v, kv.committedConfig => %+v, kv.transitionState => %+v \n", commitMsg, kv.committedConfig, kv.transitionState)
					


					// Add all the shards to the current map
					for key, value := range(thisCommand.Shards) {
						// Exectue operation: Replaces the value for a particular key.
						kv.kvMap[key] = value
					}

					// Update each relevant row of the Commit Table
					for clientID_shard, row := range (thisCommand.LastCommitTable) {
						kv.lastCommitTable[clientID_shard] = row
					}
					
					// Delete gid from transition state. 
					delete(kv.transitionState.GroupsToReceiveFrom, int(thisCommand.ClientID))

					kv.DPrintf2("Action: Executed SHARD TRANSFER OP on ApplyCh. kv.committedConfig => %+v, Operation => %+v, kv.transitionState => %+v \n", kv.committedConfig, thisCommand, kv.transitionState )

					// Check if transition complete. 
					kv.transitionCompleteCheck()


					//Return RPC to Client with correct value.
					kv.handleOpenRPCs(commitMsg.Index, thisCommand, "", OK)
					

				} else {
					//Todo
					kv.DPrintf1("Reject ShardTransfer!!! \n")
					delete(kv.waitingForRaft_queue, commitMsg.Index)
				}

				kv.manageSnapshots(commitMsg.Index, commitMsg.Term)

			} else if thisCommand.CommandType == TransferSuccess {
				// Check if this successful transfer has already been executed. 
				// ClientID => ID of the client shards send to, RequestID => Num of future config 
				committed, packageLocation := kv.checkIfTransferCommitted(int(thisCommand.ClientID), int(thisCommand.RequestID))

				// If not committed, remove corresponding entries from shardTransferStorage and activeTransferRPCs table.
				if (!committed) {

					// Delete the element from the slice
					kv.shardTransferStorage = append(kv.shardTransferStorage[:packageLocation], kv.shardTransferStorage[packageLocation+1:]...)
					kv.activeTransferRPCs = append(kv.activeTransferRPCs[:packageLocation], kv.activeTransferRPCs[packageLocation+1:]...)

					kv.DPrintf2("Action: Executed TRANSFER SUCCCESS OP on ApplyCh. kv.committedConfig => %+v, Operation => %+v, kv.shardTransferStorage => %+v \n", kv.committedConfig, thisCommand, kv.shardTransferStorage )


				}

				kv.manageSnapshots(commitMsg.Index, commitMsg.Term)

			} else if thisCommand.CommandType == Get {

				commitExists, returnValue := kv.checkCommitTable(thisCommand)

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
					kv.lastCommitTable[thisCommand.ClientID] = CommitStruct{RequestID: thisCommand.RequestID, ReturnValue: returnValue, Key: thisCommand.Key}

				}

				// If there is an outstanding RPC, return the appropriate value.
				kv.handleOpenRPCs(commitMsg.Index, thisCommand, returnValue, OK)
				kv.manageSnapshots(commitMsg.Index, commitMsg.Term)

				// Execute Put Request
			} else if thisCommand.CommandType == Put {

				// Check commitTable
				commitExists, _ := kv.checkCommitTable(thisCommand)

				// Raft Op is next request: Execute the commmand
				if !commitExists {
					// Exectue operation: Replaces the value for a particular key.
					kv.kvMap[thisCommand.Key] = thisCommand.Value

					// Update commitTable. No returnValue since a put/append request
					kv.lastCommitTable[thisCommand.ClientID] = CommitStruct{RequestID: thisCommand.RequestID, Key: thisCommand.Key}
				}

				//Return RPC to Client with correct value.
				kv.handleOpenRPCs(commitMsg.Index, thisCommand, "", OK)
				kv.manageSnapshots(commitMsg.Index, commitMsg.Term)

				// Execute Append Request
			} else if thisCommand.CommandType == Append {

				// Check commitTable
				commitExists, _ := kv.checkCommitTable(thisCommand)

				// Raft Op is next request: Execute the commmand
				if !commitExists {
					// Exectue operation: Replaces the value for a particular key.
					kv.kvMap[thisCommand.Key] = kv.kvMap[thisCommand.Key] + thisCommand.Value

					// Update commitTable. No returnValue since a put/append request
					kv.lastCommitTable[thisCommand.ClientID] = CommitStruct{RequestID: thisCommand.RequestID, Key: thisCommand.Key}
				}

				//Return RPC to Client with correct value.
				kv.handleOpenRPCs(commitMsg.Index, thisCommand, "", OK)
				kv.manageSnapshots(commitMsg.Index, commitMsg.Term)

			} else {
				kv.DError("Error: Operation Recieved on applyCh is neither 'Append', 'Put' nor 'Get'. \n")
			}

			kv.DPrintf2("State after Receiving OP on applyCh. Map => %+v, RPC_Queue => %+v, CommitTable %+v \n \n \n ",kv.kvMap, kv.waitingForRaft_queue, kv.lastCommitTable)

			kv.mu.Unlock()
			kv.Locking(false)

		case <-kv.shutdownChan:
			return

		}
	}
}
