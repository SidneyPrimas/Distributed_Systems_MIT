package mapreduce

import "fmt"
//import "sync"

// schedule starts and waits for all tasks in the given phase (Map or Reduce).
func (mr *Master) schedule(phase jobPhase) {
	var ntasks int
	var nios int // number of inputs (for reduce) or outputs (for map)
	switch phase {
	case mapPhase:
		ntasks = len(mr.files)
		nios = mr.nReduce
	case reducePhase:
		ntasks = mr.nReduce
		nios = len(mr.files)
	}

	fmt.Printf("Schedule: %v %v tasks (%d I/Os)\n", ntasks, phase, nios)

	// All ntasks tasks have to be scheduled on workers, and only once all of
	// them have been completed successfully should the function return.
	// Remember that workers may fail, and that any given worker may finish
	// multiple tasks.
	
	var current_task int = 0
	failChannel := make(chan *DoTaskArgs)

	for current_task < ntasks {

	
		select {
		//Send a task to a worker
		case idle_worker := <-mr.registerChannel:

			select {
			case failTaskArgs := <- failChannel:
				go func(myArgs *DoTaskArgs, myWorker string) {

					var reply ShutdownReply
					ok := call(myWorker, "Worker.DoTask", myArgs , &reply)
					if ok == false {
						fmt.Printf("RPS Failure\n")

						failChannel <- myArgs

					} else {
						mr.registerChannel <- myWorker
					}
				}(failTaskArgs, idle_worker)




			default:
				args := new(DoTaskArgs)
				args.JobName = mr.jobName
				args.File = mr.files[current_task]
				args.Phase = phase
				args.TaskNumber = current_task
				args.NumOtherPhase = nios

				current_task++
				go func(myArgs *DoTaskArgs, myWorker string) {
					


					var reply ShutdownReply
					ok := call(myWorker, "Worker.DoTask", myArgs , &reply)
					if ok == false {
						fmt.Printf("RPS Failure\n")
						failChannel <- myArgs


					} else {
						mr.registerChannel <- myWorker
					}


				}(args, idle_worker)
			}
			
		default :

		}
	}



	fmt.Printf("Schedule: %v phase done\n", phase)



}
