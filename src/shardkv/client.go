package shardkv

//
// client code to talk to a sharded key/value service.
//
// the client first talks to the shardmaster to find out
// the assignment of shards (keys) to groups, and then
// talks to the group that holds the key's shard.
//

import "labrpc"
import "crypto/rand"
import "math/big"
import "shardmaster"
import "time"
import "fmt"
import "log"
import mrand "math/rand"

//
// which shard is a key in?
// please use this function,
// and please do not change it.
//
// Maps a key (which is a string) to a shard. 
func key2shard(key string) int {
	shard := 0
	if len(key) > 0 {
		shard = int(key[0])
	}
	shard %= shardmaster.NShards
	return shard
}

func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x
}

type Clerk struct {
	sm       *shardmaster.Clerk
	config   shardmaster.Config
	make_end func(string) *labrpc.ClientEnd
	// You will have to modify this struct.
	currentLeader map[int]int
	clientID      int64
	currentRPCNum int64
	debug         int
}

//
// the tester calls MakeClerk.
//
// masters[] is needed to call shardmaster.MakeClerk().
//
// make_end(servername) turns a server name from a
// Config.Groups[gid][i] into a labrpc.ClientEnd on which you can
// send RPCs.
//
func MakeClerk(masters []*labrpc.ClientEnd, make_end func(string) *labrpc.ClientEnd) *Clerk {
	ck := new(Clerk)
	//Creates a shardmaster linked to this client. Masters includes all other shardmasters. 
	ck.sm = shardmaster.MakeClerk(masters)
	ck.make_end = make_end
	// You'll have to add code here.

	ck.currentLeader = make(map[int]int)
	ck.clientID = nrand()
	ck.currentRPCNum = 0
	ck.debug = 0

	return ck
}

//
// fetch the current value for a key.
// returns "" if the key does not exist.
// keeps trying forever in the face of all other errors.
// You will have to modify this function.
//
func (ck *Clerk) Get(key string) string {
	args := GetArgs{}
	args.Key = key
	args.ClientID = ck.clientID
	args.RequestID = ck.currentRPCNum


	for  {
		shard := key2shard(key)
		gid := ck.config.Shards[shard]
		// If the gid exists in our current stored configuration. 
		if servers, ok := ck.config.Groups[gid]; ok {
			// try each server for the shard.

				selectedServer := ck.getRandomServer(gid)
				srv := ck.make_end(servers[selectedServer])
				
				var reply GetReply
				RPC_returned := make(chan bool)
				go func() {
					ok := srv.Call("ShardKV.Get", &args, &reply)

					RPC_returned <- ok
				}()

				//Allows for RPC Timeout
				ok := false
				select {
				case <-time.After(time.Millisecond * 250):
				  	ok = false
				case ok = <-RPC_returned:
				}

				// Wrong Leader (reset stored leader)
				if !ok || (ok && reply.WrongLeader == true) {
					ck.currentLeader[gid] = -1
				}

				// Correct Leader
				if ok && reply.WrongLeader == false  {
					//Update stored Leader
					ck.currentLeader[gid] = selectedServer

					// Handle successful reply
					if (reply.Err == OK || reply.Err == ErrNoKey) {
						ck.DPrintf1("Action: Get completed. Sent Args => %+v, Received Reply => %+v \n", args, reply)
						// RPC Completed so increment the RPC count by 1.
						ck.currentRPCNum = ck.currentRPCNum + 1


						return reply.Value
					}
				}
				// Handle situation where wrong group.
				if ok && (reply.Err == ErrWrongGroup) {
					// ask master for the latest configuration.
					ck.config = ck.sm.Query(-1)
					time.Sleep(100 * time.Millisecond)
				}

		} else {
			// ask master for the latest configuration.
			ck.config = ck.sm.Query(-1)
			time.Sleep(100 * time.Millisecond)
		}
	}

	ck.DError("Return from Get in ShardKV Client. Should never return from here.")
	return ""
}


//
// shared by Put and Append.
// You will have to modify this function.
//
func (ck *Clerk) PutAppend(key string, value string, op string) {
	args := PutAppendArgs{}
	args.Key = key
	args.Value = value
	args.Op = op
	args.ClientID = ck.clientID
	args.RequestID = ck.currentRPCNum


	for {
		shard := key2shard(key)
		gid := ck.config.Shards[shard]
		// If the gid exists in our current stored configuration. 
		if servers, ok := ck.config.Groups[gid]; ok {
			// try each server for the shard.
			
				selectedServer := ck.getRandomServer(gid)
				srv := ck.make_end(servers[selectedServer])
				
				var reply PutAppendReply
				RPC_returned := make(chan bool)
				go func() {
					ok := srv.Call("ShardKV.PutAppend", &args, &reply)

					RPC_returned <- ok
				}()

				//Allows for RPC Timeout
				ok := false
				select {
				case <-time.After(time.Millisecond * 250):
				  	ok = false
				case ok = <-RPC_returned:
				}

				// Wrong Leader (reset stored leader)
				if !ok || (ok && reply.WrongLeader == true) {
					ck.currentLeader[gid] = -1
				}

				// Correct Leader
				if ok && reply.WrongLeader == false  {
					//Update stored Leader
					ck.currentLeader[gid] = selectedServer

					// Handle successful reply
					if (reply.Err == OK) {
						ck.DPrintf1("Action: PutAppend completed. Sent Args => %+v, Received Reply => %+v \n", args, reply)
						// RPC Completed so increment the RPC count by 1.
						ck.currentRPCNum = ck.currentRPCNum + 1

						return
					}
				}
				if ok && reply.Err == ErrWrongGroup {
					// ask master for the latest configuration.
					ck.config = ck.sm.Query(-1)
					time.Sleep(100 * time.Millisecond)
				}

		} else {
			// ask master for the latest configuration.
			ck.config = ck.sm.Query(-1)
			time.Sleep(100 * time.Millisecond)
		}
	}

	ck.DError("Return from PUTAPPEND in ShardKV Client. Should never return from here.")
}

func (ck *Clerk) Put(key string, value string) {
	ck.PutAppend(key, value, "Put")
}
func (ck *Clerk) Append(key string, value string) {
	ck.PutAppend(key, value, "Append")
}

//********** HELPER FUNCTIONS **********//

func (ck *Clerk) getRandomServer(gid int) (testServer int) {


	selectedServer, ok := ck.currentLeader[gid]
	if (ok && selectedServer != -1){
		return selectedServer
	} else {
		randSource := mrand.NewSource(time.Now().UnixNano())
		r := mrand.New(randSource)
		selectedServer = r.Int() % (len(ck.config.Groups[gid]))

		return selectedServer
	}

}


//********** UTILITY FUNCTIONS **********//

func (ck *Clerk) DPrintf2(format string, a ...interface{}) (n int, err error) {
	if ck.debug >= 2 {
		custom_input := make([]interface{},1)
		custom_input[0] = ck.clientID
		out_var := append(custom_input , a...)
		log.Printf("KVClient%d, " + format + "\n", out_var...)
	}
	return
}

func (ck *Clerk) DPrintf1(format string, a ...interface{}) (n int, err error) {
	if ck.debug >= 1 {
		custom_input := make([]interface{},1)
		custom_input[0] = ck.clientID
		out_var := append(custom_input , a...)
		log.Printf("KVClient%d, " + format + "\n", out_var...)
	}
	return
}

func (ck *Clerk) DPrintf_now(format string, a ...interface{}) (n int, err error) {
	if ck.debug >= 0 {
		custom_input := make([]interface{},1)
		custom_input[0] = ck.clientID
		out_var := append(custom_input , a...)
		log.Printf("KVClient%d, " + format + "\n", out_var...)
	}
	return
}

func (ck *Clerk) DError(format string, a ...interface{}) (n int, err error) {
	if ck.debug >= 0 {
		custom_input := make([]interface{},1)
		custom_input[0] = ck.clientID
		out_var := append(custom_input , a...)
		panic_out := fmt.Sprintf("KVClient%d, " + format + "\n", out_var...)
		panic(panic_out)
	}
	return
}