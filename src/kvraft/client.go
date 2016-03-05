package raftkv

import "labrpc"
import mrand "math/rand" 
import crand "crypto/rand" 
import "math/big"
import "time"


type Clerk struct {
	servers []*labrpc.ClientEnd
	// You will have to modify this struct.
	currentLeader 	int
	clientID		int64
	currentRPCNum	int64
}

func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := crand.Int(crand.Reader, max)
	x := bigx.Int64()
	return x
}

func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	ck := new(Clerk)
	ck.servers = servers
	// You'll have to add code here.
	ck.currentLeader = -1
	ck.clientID = nrand()
	ck.currentRPCNum = 0


	return ck
}

//
// fetch the current value for a key.
// returns "" if the key does not exist.
// keeps trying forever in the face of all other errors.
//
// you can send an RPC with code like this:
// ok := ck.servers[i].Call("RaftKV.Get", args, &reply)
//
func (ck *Clerk) Get(key string) string {

	// Initialize default return value
	var getOutput string = ""

	// You will have to modify this function.
	// 1) Build the args and reply structure. 
	args := GetArgs{
		Key: key, 
		ClientID: ck.clientID, 
		RequestID: ck.currentRPCNum}


	var rpcSuccess bool = false
	// Keep sending this PutAppend Request until it's successful 
	for (!rpcSuccess) {

		// Select server to send request to. 
		var testServer int
		if (ck.currentLeader == -1) {
			testServer =  ck.getRandomServer()
		} else {	
			testServer = ck.currentLeader
		}

		var reply GetReply
		RPC_returned := make(chan bool)
		// Go function used to timeout RPC call. 
		go func(args *GetArgs, reply *GetReply) {

			// Loop around servers until find successful server. 
			ok := ck.servers[testServer].Call("RaftKV.Get", args, reply)
			RPC_returned <- ok
			
		}(&args, &reply)
		// Creates artificial timeout for RPC call
		select {
		case <- time.After(time.Second * 7):
			//Send out another RPC. 
		case ok := <-RPC_returned:
			// Only process reply if server responded. 
			if (ok) {
				// Succes: Command successfuly committed. We 
				if (!reply.WrongLeader) {

					// Exit RPC Sending loop, and safe the current leader. 
					ck.currentLeader = testServer
					rpcSuccess = true

					// Return Value: If the key didn't exists, the KVServer already sends "". 
					getOutput = reply.Value


				// Wrong Leader: find the correct leader
				} else if (reply.WrongLeader) {
					ck.currentLeader = -1
					rpcSuccess = false
				}

			}
		}
	}

	// Final Step: RPC Completed so increment the RPC count by 1. 
	ck.currentRPCNum = ck.currentRPCNum + 1
	return getOutput
}

//
// shared by Put and Append.
//
// you can send an RPC with code like this:
// ok := ck.servers[i].Call("RaftKV.PutAppend", args, &reply)
//
func (ck *Clerk) PutAppend(key string, value string, op string) {
	// You will have to modify this function.

	// 1) Build the args and reply structure. 
	args := PutAppendArgs{
		Key: key, 
		Value: value, 
		Op: op, 
		ClientID: ck.clientID, 
		RequestID: ck.currentRPCNum}


	var rpcSuccess bool = false
	// Keep sending this PutAppend Request until it's successful 
	for (!rpcSuccess) {

		// Select server to send request to. 
		var testServer int
		if (ck.currentLeader == -1) {
			testServer =  ck.getRandomServer()
		} else {	
			testServer = ck.currentLeader
		}

		var reply PutAppendReply
		RPC_returned := make(chan bool)
		// Go function used to timeout RPC call. 
		go func(args *PutAppendArgs, reply *PutAppendReply) {

			// Loop around servers until find successful server. 
			ok := ck.servers[testServer].Call("RaftKV.PutAppend", args, reply)
			RPC_returned <- ok
			
		}(&args, &reply)
		// Creates artificial timeout for RPC call
		select {
		case <- time.After(time.Second * 7):
			//Send out another RPC. 
		case ok := <-RPC_returned:
			// Success: Command successfuly committed. 
			// Note: Will only return from server with correct leader if committed. 
			// Exit RPC Sending loop, and safe the current leader. 
			if (ok && !reply.WrongLeader) {
				ck.currentLeader = testServer
				rpcSuccess = true
			// Wrong Leader
			} else if (ok && reply.WrongLeader) {
				ck.currentLeader = -1
				rpcSuccess = false
			}
		}
	}

	// Final Step: RPC Completed so increment the RPC count by 1. 
	ck.currentRPCNum = ck.currentRPCNum + 1 
	
}

func (ck *Clerk) Put(key string, value string) {
	ck.PutAppend(key, value, "Put")
}
func (ck *Clerk) Append(key string, value string) {
	ck.PutAppend(key, value, "Append")
}


//********** KV Client FUNCTIONS (non-RPC) **********//

func (ck *Clerk) getRandomServer() (testServer int) {

	randSource := mrand.NewSource(time.Now().UnixNano())
    r := mrand.New(randSource)
	testServer = r.Int()  % (len(ck.servers))

	return testServer

}