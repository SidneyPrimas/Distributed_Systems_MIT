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
	Get    			OpType = 1
	Put    			OpType = 2
	Append 			OpType = 3
	Configuration 	OpType = 4
)

type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	CommandType OpType
	Key         string
	Value       string
	ClientID    int64
	RequestID   int64
	Config 		shardmaster.Config
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
}


func (kv *ShardKV) Get(args *GetArgs, reply *GetReply) {
	// Your code here.
	kv.mu.Lock()

	// 1) Convert GetArgs into  Op Struct
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

			kv.DPrintf2("Action: Rejected GET. KVServer%d not Leader.  \n", kv.me)

			reply.WrongLeader = true
			reply.Value = "" // Return value from key. Returns "" if key doesn't exist
			reply.Err = OK

			// Unlock on any reply
			kv.mu.Unlock()

			// 4) Handle Response: If Raft is leader, wait until raft committed Op Struct to log
		} else if isLeader {

			kv.DPrintf1("%s \n Action:  KVServer%d is Leader. Sent GET Request to Raft. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v\n", debug_break, kv.me, index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue)

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
				kv.DPrintf1("Action: GET APPLIED. Respond to client. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n %s \n \n", index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue, debug_break)
				reply.WrongLeader = false
				reply.Value = rpcReturnInfo.value // Return value from key. Returns "" if key doesn't exist
				reply.Err = OK

				// Commit Failed: If this server discovers it's not longer the leader, then tell the client to find the real leader,
				// and retry with the request there.
			} else if !open || !rpcReturnInfo.success {
				kv.DPrintf1("Action: GET ABORTED. Respond to client. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n %s \n \n", index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue, debug_break)
				reply.WrongLeader = true
				reply.Err = OK

			}
			kv.mu.Unlock()

		}

	}
}

func (kv *ShardKV) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
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
	inCommitTable, _ := kv.checkCommitTable(thisOp)

	// Return RPC with correct value: If the client is known, and this server already processed the RPC, send the return value to the client.
	// Note: Even if Server is not leader, respond with the information client needs for this specific request.
	if inCommitTable {

		kv.DPrintf1("Action: REPEAT REQUEST. PUTAPPEND ALREADY APPLIED. Respond to client.   \n")
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

			kv.DPrintf2("Action: Rejected PutAppend. KVServer%d not Leader.  \n", kv.me)
			reply.WrongLeader = true
			reply.Err = OK

			// Unlock on any reply
			kv.mu.Unlock()

			// 4) Handle Response: If Raft is leader, wait until raft committed Op Struct to log
		} else if isLeader {
			kv.DPrintf1("%s \n Action:  KVServer%d is Leader. Sent PUTAPPEND Request to Raft. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v\n", debug_break, kv.me, index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue)

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

				kv.DPrintf1("Action: PUTAPPEND APPLIED. Respond to client. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n %s \n \n",index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue, debug_break)
				reply.WrongLeader = false
				reply.Err = OK

				// Commit Failed: If this server discovers it's no longer the leader, then tell the client to find the real leader,
				// and retry with the request there.
			} else if !open || !rpcReturnInfo.success {

				kv.DPrintf1("Action: PUTAPPEND ABORTED. Respond to client. Index => %d, Map => %+v, Operation => %+v, CommitTable => %+v, RPC_Que => %+v \n %s \n \n", index, kv.kvMap, thisOp, kv.lastCommitTable, kv.waitingForRaft_queue, debug_break)
				reply.WrongLeader = true
				reply.Err = OK

			}

			kv.mu.Unlock()
		}
	}
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
	defer kv.mu.Unlock()
	kv.DPrintf1("%s \nAction: Dies \n", debug_break)

	// Kill all open go routines when this server quites.
	close(kv.shutdownChan)

	// Since this server will no longer be leader on resstart, return all outstanding PutAppend and Get RPCs.
	// Allows respective client to find another leader.
	kv.killAllRPCs()

	// Turn off debuggin output
	kv.debug = -1
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

	kv := new(ShardKV)
	kv.me = me
	kv.maxraftstate = maxraftstate
	kv.make_end = make_end
	kv.gid = gid
	kv.masters = masters

	// Your initialization code here.
	kv.persister = persister
	kv.debug = 1
	kv.mu = sync.Mutex{}

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

	// Use something like this to talk to the shardmaster:
	kv.mck = shardmaster.MakeClerk(kv.masters)
	// Get initial configuration Num. 
	kv.committedConfig = kv.mck.Query(-1)

	kv.DPrintf1("%s \n Action: New KV Server and Raft Instance Created. \n", debug_break)

	go kv.processCommits()

	go kv.checkConfiguration()

	return kv
}

//********** Helper Functions **********//
func (kv *ShardKV) checkConfiguration() {

	// Go routine that loops until server is shutdown.
	for {
		
		select {
		// Garbage collection. 
		case <-kv.shutdownChan:
			return
		case <-time.After(time.Millisecond * 100):
			//Check if a higher configuration exists (configuration that is one higher).
			configTemp := kv.mck.Query(kv.committedConfig.Num+1)
			kv.mu.Lock()

			// If there is an updated configuration, submit it through Raft. 
			if (configTemp.Num == kv.committedConfig.Num+1) {

				thisOp := Op{
					CommandType: Configuration,
					Config:      configTemp}


				// Unlock before Start (so Start can be in parallel)
				kv.mu.Unlock()
				// Send Op Struct to Raft (using kv.rf.Start())
				index, _, isLeader := kv.rf.Start(thisOp)
				kv.mu.Lock()

				// For Logging
				if isLeader {

					kv.DPrintf1("%s \n Action: KVServer%d is Leader. Sent CONFIGURATION CHANGE Request to Raft. Index => %d, Operation => %+v \n", debug_break, kv.me, index, thisOp)
				
				} 
			}

			kv.mu.Unlock()
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

			kv.DPrintf2("State before Receiving OP on applyCh. Map => %+v, RPC_Queue => %+v, CommitTable %+v \n", kv.kvMap, kv.waitingForRaft_queue, kv.lastCommitTable)

			if (commitMsg.UseSnapshot) {
				kv.readPersistSnapshot(commitMsg.Snapshot)
				kv.DPrintf2("State Machine reset with snapshot. Map => %+v, RPC_Queue => %+v, CommitTable %+v \n", kv.kvMap, kv.waitingForRaft_queue, kv.lastCommitTable)

				// Just apply the snapshot. This will skip all of the rest of the exectuion, and return to select. 
				kv.mu.Unlock()
				continue
			}
			

			// Type Assert: Package the Command from ApplyMsg into an Op Struct
			thisCommand := commitMsg.Command.(Op)

			if thisCommand.CommandType == Configuration {
				kv.DPrintf1("Received CONFIGURATION OP on ApplyCh. kv.committedConfig => %+v, Operation => %+v \n", kv.committedConfig, thisCommand)
				
				// 1) Make sure this is the next configuration change that we expect.
				// 2) Execute configuration change appropriately.  
				if (kv.committedConfig.Num + 1 == thisCommand.Config.Num) {
					kv.DPrintf2("Received NEXT configuration change. Process the change. \n")

					noShardChange := compareShards(kv.committedConfig.Shards, thisCommand.Config.Shards)

					// Update committedConfig when there is no shard change.
					if (noShardChange) {
						kv.committedConfig = thisCommand.Config

					// Configuration change detected. Migrate shards appropriately. 
					} else {
						kv.DError("v.committedConfig => %+v, Operation => %+v \n", kv.committedConfig, thisCommand)
					}

				} else if (kv.committedConfig.Num == thisCommand.Config.Num) {
					kv.DPrintf2("Received REPEAT configuration change. Process the change. \n")

				} else {
					kv.DError("Received a configuration change that's very old or very new through Raft. Should not be possible. \n")
				}


			// Execute Get Request
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
					kv.lastCommitTable[thisCommand.ClientID] = CommitStruct{RequestID: thisCommand.RequestID, ReturnValue: returnValue}

				}

				// If there is an outstanding RPC, return the appropriate value.
				kv.handleOpenRPCs(commitMsg.Index, thisCommand, returnValue)

				// Execute Put Request
			} else if thisCommand.CommandType == Put {

				// Check commitTable
				commitExists, _ := kv.checkCommitTable(thisCommand)

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
				commitExists, _ := kv.checkCommitTable(thisCommand)

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

			kv.DPrintf2("State after Receiving OP on applyCh. Map => %+v, RPC_Queue => %+v, CommitTable %+v \n \n \n ",kv.kvMap, kv.waitingForRaft_queue, kv.lastCommitTable)

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
