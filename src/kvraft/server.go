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
type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	CommandType 		OpType
	Key 				string
	Value 				string

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
	kvMap			map[string]string
}


func (kv *RaftKV) Get(args *GetArgs, reply *GetReply) {
	DPrintf1("KVServer%d, Action: GET REQUEST FROM CLIENT.  \n", kv.me)
}

// Handler: RPC sender receives returned RPC once function completed. 
func (kv *RaftKV) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	// Your code here.

	// 1) Convert PutAppendArgs into  Op Struct
	thisOp := Op{
		CommandType: stringToOpType(args.Op), 
		Key: args.Key, 
		Value: args.Value}
	// 2) Send Op Struct to Raft (using kv.rf.Start())
		index, _, isLeader := kv.rf.Start(thisOp)
	// 3) Handle Response: If Raft is not leader, return appropriate reply to client
	if (!isLeader) {
		DPrintf1("KVServer%d, Action: Rejected PutAppend. KVServer%d not Leader.  \n",kv.me, kv.me)
		// If this server is no longer the leader, return all outstanding PutAppend and Get RPCs. 
		// Allows respective client to find another leader.
		kv.killAllRPCs()

		reply.WrongLeader = true
		reply.Err = OK

	// 4) Handle Response: If Raft is leader, wait until raft committed Op Struct to log
	} else if (isLeader) {
		DPrintf1("%s \n KVServer%d, Action: Received PutAppend Request. \n", debug_break, kv.me, kv.me)

		// Build channel to flag this RPC to return once respective index is committed. 
		RPC_chan := make(chan bool)

		// Add the channel to a queue of outsanding Client RPC calls (currently in Raft)
		kv.waitingForPutAppend_queue[index] = RPC_chan

		// Wait until: 1) Raft indicates that RPC is successfully committed, 2) this server discovers it's not the leader, 3) failure
		rpcOutput := <- RPC_chan
		DPrintf1("%s \n KVServer%d, Action: Raft and KVServer commited PutAppend Request.  \n", debug_break, kv.me, kv.me)

		// Successful commit indicated by Raft: Respond to Client
		if (rpcOutput) {
			DPrintf1("%s \n KVServer%d, Action: Respond to Client. PutAppend request successfully comitted.  \n", debug_break, kv.me)
			reply.WrongLeader = false
			reply.Err = OK

		// Commit Failed: If this server discovers it's not longer the leader, then tell the client to find the real leader, 
		// and retry with the request there. 
		} else if (!rpcOutput) {
			DPrintf1("%s \n KVServer%d, Action: Respond to Client. PutAppend request failed after being submitted to Raft.  \n", debug_break, kv.me)
			reply.WrongLeader = true
			reply.Err = OK

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
			DPrintf2("KVServer%d, Action: Operation Received on applyCh, Operation => %v \n", kv.me, commitMsg)

			// Type Assert: Package the Command from ApplyMsg into an Op Struct
			thisCommand := commitMsg.Command.(Op)

			// Execute Get Request
			if (thisCommand.CommandType == Get) {

				// Exectue operation: get value
				value, ok := kv.kvMap[thisCommand.Key]
				// If key exists: Find the correct RPC, and tell that RPC to return the value. 
				// If key doesn't exist: Tell RPC to return Error. 
				kv.returnGetRPC(commitMsg.Index, value, ok)
			

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


		case <- kv.shutdownChan :
			return

		}
	}
}

func (kv *RaftKV) returnGetRPC(indexOfCommit int, valueToSend string, success bool) {

	//Find all open RPC that submitted this operation. Return the appropriate value. 
	for index, RPC_chan := range(kv.waitingForGet_queue) {

		if (index == indexOfCommit) {

			// Key was found in map. Return the value. 
			if(success) {
				RPC_chan <- valueToSend
				delete(kv.waitingForGet_queue, index)
				close(RPC_chan)

			// Key wasn't in Map. Just close the channel. 
			} else if (!success) {
				delete(kv.waitingForGet_queue, index)
				close(RPC_chan)
			}
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