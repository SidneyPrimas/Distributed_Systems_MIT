package raftkv

import (
	"encoding/gob"
	"labrpc"
	"log"
	"raft"
	"sync"
)

const debug_break = "---------------------------------------------------------------------------------------"
const Debug = 1

// Human readable 
type OpType int

const (
	Get 		OpType = 1
	Put 		OpType = 2
	Append 		OpType = 3
)



// Command structure (passed on to Raft)
// Note: Need to include ClientID and RequestID since any KV Server who recieves a Op from appplyCh needs to update their commitTable. 
type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	CommandType 		OpType
	Key 				string
	Value 				string
	ClientID			int64
	RequestID			int64

}

type CommitStruct struct {
	commitNum		int64
	returnValue		string
}


type RaftKV struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft


	applyCh 					chan raft.ApplyMsg
	shutdownChan				chan int
	waitingForPutAppend_queue	map[int]chan bool
	waitingForGet_queue 		map[int]chan string 

	maxraftstate int // snapshot if log grows this big

	// Your definitions here.
	// Include Key/Value data structure
	kvMap				map[string]string
	lastCommitTable		map[int64]CommitStruct
}

// Handle GET Request RPCs. 
// Note: Need to reply to the RPC by the end of the function. 
func (kv *RaftKV) Get(args *GetArgs, reply *GetReply) {

	// Get previous commit value for this client. 
	prevCommitted, ok := kv.lastCommitTable[args.ClientID]

	// Return RPC with correct value: If the client is known, and this server already processed the RPC, send the return value to the client.
	// Note: Even if Server is not leader, respond with the information client needs for this specific request. 
	if (ok && (prevCommitted.commitNum == args.RequestID)) {

		DPrintf1("%s \n KVServer%d, Action: REPEAT REQUEST. PUTAPPEND ALREADY APPLIED. Respond to client.   \n", debug_break, kv.me)
		// Note: At this point, it's unkown if this server is the leader. 
		// However, if the server has the right information for the client, send it. 
		reply.WrongLeader = false
		reply.Value = prevCommitted.returnValue // Return the value already committed. 
		reply.Err = OK

	// Catch Errors: Received an old RequestID (behind the one already committed)
	// Catch Errors: Or, received RequestID two ahead of what's been commited.  
	} else if ok && ((prevCommitted.commitNum > args.RequestID) || prevCommitted.commitNum+1 < args.RequestID) {
		DError("Error: Based on CommitTable, RequestID is too old or too new. \n")
		// Possible not an error since we don't know if this server is leader
		// ** Change to: If any other request number, reject the RPC

	// Process the next valid RPC Request with Start(): If 1) client isn't known or 2) the request follows the request already committed. 
	} else if  (!ok) || (prevCommitted.commitNum+1 == args.RequestID) {

		// 1) Convert GetArgs into  Op Struct
		thisOp := Op{
			CommandType: Get, 
			Key: args.Key,
			ClientID: args.ClientID, 
			RequestID: args.RequestID }

		// 2) Send Op Struct to Raft (using kv.rf.Start())
		index, _, isLeader := kv.rf.Start(thisOp)

		// 3) Handle Response: If Raft is not leader, return appropriate reply to client
		if (!isLeader) {
			DPrintf2("KVServer%d, Action: Rejected GET. KVServer%d not Leader.  \n", kv.me, kv.me)
			// If this server is no longer the leader, return all outstanding PutAppend and Get RPCs. 
			// Allows respective client to find another leader.
			kv.killAllRPCs()

			reply.WrongLeader = true
			reply.Value = "" // Return null value (since get request failed)
			reply.Err = OK

		// 4) Handle Response: If Raft is leader, wait until raft committed Op Struct to log
		} else if (isLeader) {

			// Build channel to flag this RPC to return once respective index is committed. 
			RPC_chan := make(chan string)
			DPrintf1("%s \n KVServer%d, Action:  KVSevrver%d is Leader. Sent GET Request to Raft.  Operation => %+v \n", debug_break, kv.me, kv.me, thisOp)

			// Add the channel to a queue of outsanding Client RPC calls (currently in Raft)
			kv.waitingForGet_queue[index] = RPC_chan

			// Wait until: 1) Raft indicates that RPC is successfully committed, 2) this server discovers it's not the leader, 3) failure
			rpcOutput_string, open := <- RPC_chan
			DPrintf2("KVServer%d, Action: GET RPC hears on RPC_chan.  \n", kv.me)
			

			// Successful commit indicated by Raft: Respond to Client
			if (open) {
				DPrintf1("%s \n KVServer%d, Action: GET APPLIED. Respond to client. \n", debug_break, kv.me)
				reply.WrongLeader = false
				reply.Value = rpcOutput_string // Return value from key. Returns "" if key doesn't exist
				// Don't set reply.Err = OK.

			// Commit Failed: If this server discovers it's not longer the leader, then tell the client to find the real leader, 
			// and retry with the request there. 
			} else if (!open) {
				DPrintf1("KVServer%d, Action: GET ABORTED. Respond to client.  \n", kv.me)
				reply.WrongLeader = true
				reply.Err = OK

			}

		}

	}
}

// Handler: RPC sender receives returned RPC once function completed. 
func (kv *RaftKV) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	// Your code here.

	// Get previous commit value for this client. 
	prevCommitted, ok := kv.lastCommitTable[args.ClientID]

	// Return RPC with correct value: If the client is known, and this server already processed the RPC, send the return value to the client.
	// Note: Even if Server is not leader, respond with the information client needs for this specific request. 
	if (ok && (prevCommitted.commitNum == args.RequestID)) {

		DPrintf1("%s \n KVServer%d, Action: REPEAT REQUEST. PUTAPPEND ALREADY APPLIED. Respond to client.   \n", debug_break, kv.me)
		// Note: At this point, it's unkown if this server is the leader. 
		// However, if the server has the right information for the client, send it. 
		reply.WrongLeader = false
		reply.Err = OK

	// Catch Errors: Received an old RequestID (behind the one already committed)
	// Catch Errors: Or, received RequestID two ahead of what's been commited.  
	} else if ok && ((prevCommitted.commitNum > args.RequestID) || prevCommitted.commitNum+1 < args.RequestID) {
		DError("Error: Based on CommitTable, RequestID is too old or too new. \n")
		// Possible not an error since we don't know if this server is leader
		// ** Change to: If any other request number, reject the RPC

	// Process the next valid RPC Request with Start(): If 1) client isn't known or 2) the request follows the request already committed. 
	} else if  (!ok) || (prevCommitted.commitNum+1 == args.RequestID) {

		// 1) Convert PutAppendArgs into  Op Struct. Include ClientId and RequestID so Servers can update their commitTable
		thisOp := Op{
			CommandType: stringToOpType(args.Op), 
			Key: args.Key, 
			Value: args.Value,
			ClientID: args.ClientID, 
			RequestID: args.RequestID}

		// 2) Send Op Struct to Raft (using kv.rf.Start())
			index, _, isLeader := kv.rf.Start(thisOp)

		// 3) Handle Response: If Raft is not leader, return appropriate reply to client
		if (!isLeader) {
			DPrintf2("KVServer%d, Action: Rejected PutAppend. KVServer%d not Leader.  \n",kv.me, kv.me)
			// If this server is no longer the leader, return all outstanding PutAppend and Get RPCs. 
			// Allows respective client to find another leader.
			kv.killAllRPCs()

			reply.WrongLeader = true
			reply.Err = OK

		// 4) Handle Response: If Raft is leader, wait until raft committed Op Struct to log
		} else if (isLeader) {
			DPrintf1("%s \n KVServer%d, Action:  KVSevrver%d is Leader. Sent PUTAPPEND Request to Raft.  Operation => %+v \n", debug_break, kv.me, kv.me, thisOp)

			// Build channel to flag this RPC to return once respective index is committed. 
			RPC_chan := make(chan bool)

			// Add the channel to a queue of outsanding Client RPC calls (currently in Raft)
			kv.waitingForPutAppend_queue[index] = RPC_chan

			// Wait until: 1) Raft indicates that RPC is successfully committed, 2) this server discovers it's not the leader, 3) failure
			rpcOutput := <- RPC_chan
			DPrintf2("KVServer%d, Action: PUTAPPEND RPC hears on Raft completed it's Request.  \n", kv.me)
			

			// Successful commit indicated by Raft: Respond to Client
			if (rpcOutput) {
				DPrintf1("%s \n KVServer%d, Action: PUTAPPEND APPLIED. Respond to client.   \n", debug_break, kv.me)
				reply.WrongLeader = false
				reply.Err = OK

			// Commit Failed: If this server discovers it's no longer the leader, then tell the client to find the real leader, 
			// and retry with the request there. 
			} else if (!rpcOutput) {
				DPrintf1("KVServer%d, Action: PUTAPPEND ABORTED. Respond to client.  \n", kv.me)
				reply.WrongLeader = true
				reply.Err = OK
			}
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
	
	// Kill all open go routines when this server quites. 
	close(kv.shutdownChan)

	// Since this server will no longer be leader on resstart, return all outstanding PutAppend and Get RPCs. 
	// Allows respective client to find another leader.
	kv.killAllRPCs()
	DPrintf1("%s \n KVServer%d, Action: Dies \n", debug_break, kv.me)


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

	// Your initialization code here.

	// Creates Queues to keep tract of outstand Client RPCs (while waiting for Raft)
	kv.waitingForPutAppend_queue = make(map[int]chan bool)
	kv.waitingForGet_queue = make(map[int]chan string)

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


	DPrintf1("%s \n KVServer%d, Action: New KV Server and Raft Instance Created. \n", debug_break, kv.me)

	go kv.processCommits()


	return kv
}

//********** KV Server FUNCTIONS (non-RPC) **********//

func (kv *RaftKV) processCommits() {

	// Go routine that loops until server is shutdown. 
	for {

		select {
		case commitMsg := <-  kv.applyCh:
			DPrintf1("KVServer%d, Action: Operation Received on applyCh, Operation => %+v \n", kv.me, commitMsg)

			// Type Assert: Package the Command from ApplyMsg into an Op Struct
			thisCommand := commitMsg.Command.(Op)


			// Execute Get Request
			if (thisCommand.CommandType == Get) {

				// Exectue operation: get value
				value, ok := kv.kvMap[thisCommand.Key]

				// Handle casewhere key (from Get) doesn't exists. 
				if (!ok) {
					value = ""
				} else if (ok && value == "") {
					DPrintf2("Assertion: Send back a null string value from a found key. Can get confused with ErrNoKey. \n")
				}

				// If key exists: Find the correct RPC, and tell that RPC to return the value. 
				// If key doesn't exist: Tell RPC to return Error. 
				kv.returnGetRPC(commitMsg.Index, value)
			

			// Execute Put Request
			} else if (thisCommand.CommandType == Put) {
				// Exectue operation: Replaces the value for a particular key. 
				kv.kvMap[thisCommand.Key] = thisCommand.Value

				//Return RPC to Client with correct value. 
				kv.returnPutAppendRPC(commitMsg.Index)
				

			// Execute Append Request
			} else if (thisCommand.CommandType == Append) {
				// Exectue operation: Appens value to a particaular key
				kv.kvMap[thisCommand.Key] = kv.kvMap[thisCommand.Key] + thisCommand.Value

				//Return RPC to Client with correct value. 
				kv.returnPutAppendRPC(commitMsg.Index)



			} else {
				DError("Error: Operation Recieved on applyCh is neither 'Append' nor 'Put'. \n")
			}

			DPrintf1("KVServer%d, Map after Successful Commit => %+v \n", kv.me, kv.kvMap)

		case <- kv.shutdownChan :
			return

		}
	}
}

func (kv *RaftKV) returnGetRPC(indexOfCommit int, valueToSend string) {

	//Find all open RPC that submitted this operation. Return the appropriate value. 
	for index, RPC_chan := range(kv.waitingForGet_queue) {

		if (index == indexOfCommit) {

			// If the key exists: send back the given value. 
			// If the key doesn't exist: Send back a nill character of ""
			RPC_chan <- valueToSend
			delete(kv.waitingForGet_queue, index)
			close(RPC_chan)
		}
	}

}

func (kv *RaftKV) returnPutAppendRPC(indexOfCommit int) {

	//Find all open RPC that submitted this operation. Return the appropriate value. 
	for index, RPC_chan := range(kv.waitingForPutAppend_queue) {

		if (index == indexOfCommit) {
			RPC_chan <- true
			delete(kv.waitingForPutAppend_queue, index)
			close(RPC_chan)
		} 
	}

}



func (kv *RaftKV) killAllRPCs() {
	for index, RPC_chan := range(kv.waitingForPutAppend_queue) {
		// Send false on every channel so every outstanding RPC can return, indicating client to find another leader. 
		RPC_chan <- false 
		delete(kv.waitingForPutAppend_queue, index)
		close(RPC_chan)
	}

	for index, RPC_chan := range(kv.waitingForGet_queue) {
		delete(kv.waitingForGet_queue, index)
		// Send false on every channel so every outstanding RPC can return, indicating client to find another leader. 
		close(RPC_chan)
	}

}



//********** UTILITY FUNCTIONS **********//
func (operation OpType) opToString() string {

	s:=""
    if operation == 1 {
    	s+="Get"
   	} else if operation == 2 {
    	s+="Put"
   	} else if operation == 3 {
    	s+="Append"
   	}
   	return s
}

func stringToOpType(op_s string) (op_type OpType) {

	if (op_s == "Append") {
		op_type = 3
	} else if (op_s == "Put") {
		op_type = 2
	} else {
		DError("Error: Client's Operation is neither 'Append' nor 'Put'. ")
		op_type = -1
	}

	return op_type
}


func DPrintf2(format string, a ...interface{}) (n int, err error) {
	if Debug >= 2 {
		log.Printf(format, a...)
	}
	return
}

func DPrintf1(format string, a ...interface{}) (n int, err error) {
	if Debug >= 1 {
		log.Printf(format, a...)
	}
	return
}

func DError(format string, a ...interface{}) (n int, err error) {
	if Debug >= 0 {
		log.Fatalf(format, a...)
	}
	return
}