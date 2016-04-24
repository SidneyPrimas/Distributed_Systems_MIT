package shardmaster

//
// Shardmaster clerk.
//

import "labrpc"
import "time"
import "crypto/rand"
import "math/big"
import "fmt"
import "log"

type Clerk struct {
	servers []*labrpc.ClientEnd
	// Your data here.
	currentLeader int
	clientID      int64
	currentRPCNum int64
	debug         int
}

func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x
}

func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	ck := new(Clerk)
	ck.servers = servers
	// Your code here.

	ck.currentLeader = -1
	ck.clientID = nrand()
	ck.currentRPCNum = 0
	ck.debug = 2

	return ck
}

func (ck *Clerk) Query(num int) Config {
	args := &QueryArgs{}
	// Your code here.
	args.Num = num
	args.ClientID = ck.clientID
	args.RequestID = ck.currentRPCNum

	ck.DPrintf1("Action: New Query sent. Args => %+v  \n", args)


	// Loop across servers until request is fieled. 
	for {
		//If leader not known, try each known server.
		for server := range ck.servers {

			selectedServer := ck.findLeaderServer(server)

			var reply QueryReply
			ok := ck.sendRPC(selectedServer, "ShardMaster.Query", args, &reply)

			if ok && reply.WrongLeader == false {

				// Update the leader
				ck.currentLeader = selectedServer
				ck.DPrintf1("Action: Query completed. Sent Args => %+v, Received Reply => %+v \n", args, reply)
				// Final Step: RPC Completed so increment the RPC count by 1.
				ck.currentRPCNum = ck.currentRPCNum + 1
				return reply.Config
			} else {
				ck.currentLeader = -1
			}
		}
		//If leader not found after cycling through all servers, probalby within an election.  
		time.Sleep(100 * time.Millisecond)
	}
}

func (ck *Clerk) Join(servers map[int][]string) {
	args := &JoinArgs{}
	// Your code here.
	args.Servers = servers
	args.ClientID = ck.clientID
	args.RequestID = ck.currentRPCNum


	ck.DPrintf1("Action: New Join sent. Args => %+v  \n", args)


	// Loop across servers until request is fieled. 
	for {
		//If leader not known, try each known server.
		for server := range ck.servers {

			selectedServer := ck.findLeaderServer(server)

			var reply JoinReply
			ok := ck.sendRPC(selectedServer, "ShardMaster.Join", args, &reply)

			if ok && reply.WrongLeader == false {

				// Update the leader
				ck.currentLeader = selectedServer
				ck.DPrintf1("Action: Join completed. Sent Args => %+v, Received Reply => %+v \n", args, reply)

				// Final Step: RPC Completed so increment the RPC count by 1.
				ck.currentRPCNum = ck.currentRPCNum + 1
				return
			} else {
				ck.currentLeader = -1
			}
		}
		//If leader not found after cycling through all servers, probalby within an election.  
		time.Sleep(100 * time.Millisecond)
	}
}

func (ck *Clerk) Leave(gids []int) {
	args := &LeaveArgs{}
	// Your code here.
	args.GIDs = gids
	args.ClientID = ck.clientID
	args.RequestID = ck.currentRPCNum


	ck.DPrintf1("Action: New Leave sent. Args => %+v  \n", args)


	// Loop across servers until request is fieled. 
	for {
		//If leader not known, try each known server. Otherwise, try known leader.
		for server := range ck.servers {

			selectedServer := ck.findLeaderServer(server)

			var reply LeaveReply
			ok := ck.sendRPC(selectedServer, "ShardMaster.Leave", args, &reply)

			if ok && reply.WrongLeader == false {

				// Update the leader
				ck.currentLeader = selectedServer
				ck.DPrintf1("Action: Leave completed. Sent Args => %+v, Received Reply => %+v \n", args, reply)

				// Final Step: RPC Completed so increment the RPC count by 1.
				ck.currentRPCNum = ck.currentRPCNum + 1
				return
			} else {
				ck.currentLeader = -1
			}
		}
		//If leader not found after cycling through all servers, probalby within an election.  
		time.Sleep(100 * time.Millisecond)
	}
}

func (ck *Clerk) Move(shard int, gid int) {
	args := &MoveArgs{}
	// Your code here.
	args.Shard = shard
	args.GID = gid
	args.ClientID = ck.clientID
	args.RequestID = ck.currentRPCNum


	ck.DPrintf1("Action: New Move sent. Args => %+v  \n", args)


	// Loop across servers until request is fieled. 
	for {
		//If leader not known, try each known server. Otherwise, try known leader.
		for server := range ck.servers {

			selectedServer := ck.findLeaderServer(server)

			var reply MoveReply
			ok := ck.sendRPC(selectedServer, "ShardMaster.Move", args, &reply)

			if ok && reply.WrongLeader == false {

				// Update the leader
				ck.currentLeader = selectedServer
				ck.DPrintf1("Action: Move completed. Sent Args => %+v, Received Reply => %+v \n", args, reply)

				// Final Step: RPC Completed so increment the RPC count by 1.
				ck.currentRPCNum = ck.currentRPCNum + 1
				return
			} else {
				ck.currentLeader = -1
			}
		}
		//If leader not found after cycling through all servers, probalby within an election.  
		time.Sleep(100 * time.Millisecond)
	}
}

//********** HELPER FUNCTIONS **********//

// Select server to send request to.
func (ck *Clerk) findLeaderServer(srv int) (selectedServer int) {
	if ck.currentLeader == -1 {
		selectedServer = srv
	} else {
		selectedServer = ck.currentLeader
	}

	return selectedServer
}

// Send out an RPC (with timeout implemented)
func (ck *Clerk) sendRPC(server int, function string, goArgs interface{}, goReply interface{}) (ok_out bool){

	RPC_returned := make(chan bool)
	go func() {
		ok := ck.servers[server].Call(function, goArgs, goReply)

		RPC_returned <- ok
	}()

	//Allows for RPC Timeout
	ok_out = false
	select {
	case <-time.After(time.Millisecond * 300):
	  	ok_out = false
	case ok_out = <-RPC_returned:
	}

	return ok_out
}

//********** UTILITY FUNCTIONS **********//
func (ck *Clerk) DPrintf2(format string, a ...interface{}) (n int, err error) {
	if ck.debug >= 2 {
		custom_input := make([]interface{},1)
		custom_input[0] = ck.clientID
		out_var := append(custom_input , a...)
		log.Printf("masterServer%d, " + format + "\n", out_var...)
	}
	return
}

func (ck *Clerk) DPrintf1(format string, a ...interface{}) (n int, err error) {
	if ck.debug >= 1 {
		custom_input := make([]interface{},1)
		custom_input[0] = ck.clientID
		out_var := append(custom_input , a...)
		log.Printf("masterServer%d, " + format + "\n", out_var...)
	}
	return
}

func (ck *Clerk) DPrintf_now(format string, a ...interface{}) (n int, err error) {
	if ck.debug >= 0 {
		custom_input := make([]interface{},1)
		custom_input[0] = ck.clientID
		out_var := append(custom_input , a...)
		log.Printf("masterServer%d, " + format + "\n", out_var...)
	}
	return
}

func (ck *Clerk) DError(format string, a ...interface{}) (n int, err error) {
	if ck.debug >= 0 {
		custom_input := make([]interface{},1)
		custom_input[0] = ck.clientID
		out_var := append(custom_input , a...)
		panic_out := fmt.Sprintf("masterServer%d, " + format + "\n", out_var...)
		panic(panic_out)
	}
	return
}