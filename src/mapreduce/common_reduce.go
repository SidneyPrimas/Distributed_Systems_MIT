package mapreduce

import (
	"fmt"
//	"io/ioutil"
	"os"
	"encoding/json"
	"log"
)


// doReduce does the job of a reduce worker: it reads the intermediate
// key/value pairs (produced by the map phase) for this task, sorts the
// intermediate key/value pairs by key, calls the user-defined reduce function
// (reduceF) for each key, and writes the output to disk.
func doReduce(
	jobName string, // the name of the whole MapReduce job
	reduceTaskNumber int, // which reduce task this is
	nMap int, // the number of map tasks that were run ("M" in the paper)
	reduceF func(key string, values []string) string,
) {
	// TODO:
	// You will need to write this function.
	// You can find the intermediate file for this reduce task from map task number
	// m using reduceName(jobName, m, reduceTaskNumber).
	// Remember that you've encoded the values in the intermediate files, so you
	// will need to decode them. If you chose to use JSON, you can read out
	// multiple decoded values by creating a decoder, and then repeatedly calling
	// .Decode() on it until Decode() returns an error.
	//
	// You should write the reduced output in as JSON encoded KeyValue
	// objects to a file named mergeName(jobName, reduceTaskNumber). We require
	// you to use JSON here because that is what the merger than combines the
	// output from all the reduce tasks expects. There is nothing "special" about
	// JSON -- it is just the marshalling format we chose to use. It will look
	// something like this:
	//
	// enc := json.NewEncoder(mergeFile)
	// for key in ... {
	// 	enc.Encode(KeyValue{key, reduceF(...)})
	// }
	// file.Close()


	//Step 1: Identify input files for this reduce worker through the hash_mod_code
	//Step 2: For each input file, a) decode the JSON data and b) sort the KeyValue pairs into a map. 

	//Map that combines key/value paris from all files with the same hash_mod_code
	keyMap := make(map[string][]string)
	//Important: We get the file with the correct hash_mod_code for *each* map worker. 
	//Iterate across all relevant input files
	for i := 0; i<nMap; i++ {

		//Identify and Open file
		cFname := reduceName(jobName, i, reduceTaskNumber)
		fmt.Println(cFname)
		cFile, err := os.Open(cFname)
		if err != nil {
			log.Fatal("common_reduce.go error: ", err)
		}

		//Decode contents of file with JSON
		dec := json.NewDecoder(cFile)
		for {
			var kv KeyValue
			err = dec.Decode(&kv)
			if err != nil {
				break
			}

			//For each key, add the value to the 
			keyMap[kv.Key] = append(keyMap[kv.Key], kv.Value)
		}
		cFile.Close()
	}
	


	//Step 3: Creates the output file, and JSON object.
	OutFileName := mergeName(jobName, reduceTaskNumber)
	//Creates the files
	f, err := os.Create(OutFileName)
	if err != nil {
		log.Fatal("Error in doReduce: ", err);
	}
	defer f.Close()
	//Create JSON encodeer
	enc := json.NewEncoder(f)

	//Step 4: On each key within the map, implement the reduceF function, and write the resulting key/value to the output.
	for k,v := range(keyMap) {
		reduceOut := reduceF(k,v)
		enc.Encode(KeyValue{k, reduceOut})
		
	}

}
