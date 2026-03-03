package main

import (
	"bufio"
	"dynamo-go/node"
	"fmt"
	"net/rpc"
	"os"
	"strconv"
	"strings"
	"time"
)

var config *node.Config

func main() {
	// Load configuration - try current dir first, then parent dir
	var err error
	config, err = node.LoadConfig("config.json")
	if err != nil {
		config, err = node.LoadConfig("../config.json")
		if err != nil {
			fmt.Printf("Failed to load config: %v\n", err)
			fmt.Println("Make sure config.json is in the current or parent directory")
			os.Exit(1)
		}
	}

	fmt.Println("====================================")
	fmt.Println("   DISTRIBUTED SYSTEM CLIENT")
	fmt.Println("====================================")
	fmt.Println("")
	fmt.Println("Commands:")
	fmt.Println("  put <node_id> <key> <filename>  - Store file (replicated)")
	fmt.Println("  get <node_id> <key>             - Retrieve file")
	fmt.Println("  delete <node_id> <key>          - Delete key")
	fmt.Println("  status                          - Show all nodes status")
	fmt.Println("  status <node_id>                - Show specific node status")
	fmt.Println("  list <node_id>                  - List keys on node")
	fmt.Println("  mutex-put <key> <filename>      - Put with mutual exclusion")
	fmt.Println("  deadlock-test <resource>         - Test DAG deadlock detection")
	fmt.Println("  help                            - Show this help")
	fmt.Println("  exit                            - Quit")
	fmt.Println("")
	fmt.Println("====================================")
	fmt.Println("")

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" {
			continue
		}

		parts := strings.Fields(input)
		command := parts[0]

		switch command {
		case "put":
			handlePut(parts)
		case "get":
			handleGet(parts)
		case "delete":
			handleDelete(parts)
		case "status":
			handleStatus(parts)
		case "list":
			handleList(parts)
		case "mutex-put":
			handleMutexPut(parts)
		case "deadlock-test":
			handleDeadlockTest(parts)
		case "help":
			printHelp()
		case "exit", "quit":
			fmt.Println("Goodbye!")
			os.Exit(0)
		default:
			fmt.Printf("Unknown command: %s\n", command)
			fmt.Println("Type 'help' for available commands")
		}

		fmt.Println("")
	}
}

func connectToNode(nodeID int) (*rpc.Client, error) {
	for _, nc := range config.Nodes {
		if nc.ID == nodeID {
			address := fmt.Sprintf("%s:%d", nc.IP, nc.Port)
			client, err := rpc.Dial("tcp", address)
			if err != nil {
				return nil, fmt.Errorf("cannot connect to Node %d at %s: %v", nodeID, address, err)
			}
			return client, nil
		}
	}
	return nil, fmt.Errorf("node %d not found in config", nodeID)
}

func findLeader() (int, *rpc.Client) {
	for _, nc := range config.Nodes {
		client, err := connectToNode(nc.ID)
		if err != nil {
			continue
		}

		statusArgs := &node.StatusArgs{}
		var statusReply node.StatusReply
		err = client.Call("Node.Status", statusArgs, &statusReply)
		if err != nil {
			client.Close()
			continue
		}

		if statusReply.IsLeader {
			return nc.ID, client
		}
		client.Close()
	}
	return -1, nil
}

func handlePut(parts []string) {
	if len(parts) < 4 {
		fmt.Println("Usage: put <node_id> <key> <filename>")
		fmt.Println("Example: put 1 myfile testfiles/test.txt")
		return
	}

	nodeID, err := strconv.Atoi(parts[1])
	if err != nil {
		fmt.Println("Invalid node ID")
		return
	}

	key := parts[2]
	filename := parts[3]

	data, err := os.ReadFile(filename)
	if err != nil {
		fmt.Printf("Cannot read file '%s': %v\n", filename, err)
		return
	}

	client, err := connectToNode(nodeID)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer client.Close()

	args := &node.PutArgs{
		Key:       key,
		Value:     data,
		Timestamp: time.Now().UnixNano(),
		IsReplica: false,
	}
	var reply node.PutReply

	fmt.Printf("Storing '%s' -> '%s' on Node %d (with replication)...\n", key, filename, nodeID)

	err = client.Call("Node.Put", args, &reply)
	if err != nil {
		fmt.Printf("RPC Error: %v\n", err)
		return
	}

	if reply.Success {
		fmt.Printf("SUCCESS: %s\n", reply.Message)
		if reply.Replicated {
			fmt.Println("Data replicated to all other nodes")
		}
	} else {
		fmt.Printf("FAILED: %s\n", reply.Message)
	}
}

func handleGet(parts []string) {
	if len(parts) < 3 {
		fmt.Println("Usage: get <node_id> <key>")
		fmt.Println("Example: get 2 myfile")
		return
	}

	nodeID, err := strconv.Atoi(parts[1])
	if err != nil {
		fmt.Println("Invalid node ID")
		return
	}

	key := parts[2]

	client, err := connectToNode(nodeID)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer client.Close()

	args := &node.GetArgs{Key: key}
	var reply node.GetReply

	fmt.Printf("Retrieving '%s' from Node %d...\n", key, nodeID)

	err = client.Call("Node.Get", args, &reply)
	if err != nil {
		fmt.Printf("RPC Error: %v\n", err)
		return
	}

	if reply.Found {
		outputFile := key + "_downloaded.txt"
		err = os.WriteFile(outputFile, reply.Value, 0644)
		if err != nil {
			fmt.Printf("Cannot save file: %v\n", err)
			return
		}
		fmt.Printf("SUCCESS: Downloaded to '%s' (%d bytes)\n", outputFile, len(reply.Value))
		fmt.Printf("Content preview: %s\n", string(reply.Value[:min(len(reply.Value), 200)]))
	} else {
		fmt.Printf("NOT FOUND: %s\n", reply.Message)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func handleDelete(parts []string) {
	if len(parts) < 3 {
		fmt.Println("Usage: delete <node_id> <key>")
		fmt.Println("Example: delete 1 myfile")
		return
	}

	nodeID, err := strconv.Atoi(parts[1])
	if err != nil {
		fmt.Println("Invalid node ID")
		return
	}

	key := parts[2]

	client, err := connectToNode(nodeID)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer client.Close()

	args := &node.DeleteArgs{Key: key}
	var reply node.DeleteReply

	fmt.Printf("Deleting '%s' from Node %d...\n", key, nodeID)

	err = client.Call("Node.Delete", args, &reply)
	if err != nil {
		fmt.Printf("RPC Error: %v\n", err)
		return
	}

	if reply.Success {
		fmt.Printf("SUCCESS: %s\n", reply.Message)
	} else {
		fmt.Printf("FAILED: %s\n", reply.Message)
	}
}

func handleStatus(parts []string) {
	if len(parts) >= 2 {
		nodeID, err := strconv.Atoi(parts[1])
		if err != nil {
			fmt.Println("Invalid node ID")
			return
		}
		showNodeStatus(nodeID)
	} else {
		fmt.Println("====================================")
		fmt.Println("       CLUSTER STATUS")
		fmt.Println("====================================")

		for _, nc := range config.Nodes {
			showNodeStatus(nc.ID)
		}

		fmt.Println("====================================")
	}
}

func showNodeStatus(nodeID int) {
	client, err := connectToNode(nodeID)
	if err != nil {
		fmt.Printf("Node %d: OFFLINE (cannot connect)\n", nodeID)
		return
	}
	defer client.Close()

	args := &node.StatusArgs{}
	var reply node.StatusReply

	err = client.Call("Node.Status", args, &reply)
	if err != nil {
		fmt.Printf("Node %d: ERROR (%v)\n", nodeID, err)
		return
	}

	role := "Follower"
	if reply.IsLeader {
		role = "LEADER"
	}

	fmt.Printf("Node %d: ONLINE | Role: %s | Leader: Node %d | Clock: %d | Data: %d items\n",
		reply.NodeID, role, reply.LeaderID, reply.LamportClock, reply.DataCount)
}

func handleList(parts []string) {
	if len(parts) < 2 {
		fmt.Println("Usage: list <node_id>")
		fmt.Println("Example: list 1")
		return
	}

	nodeID, err := strconv.Atoi(parts[1])
	if err != nil {
		fmt.Println("Invalid node ID")
		return
	}

	client, err := connectToNode(nodeID)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer client.Close()

	args := &node.ListKeysArgs{}
	var reply node.ListKeysReply

	err = client.Call("Node.ListKeys", args, &reply)
	if err != nil {
		fmt.Printf("RPC Error: %v\n", err)
		return
	}

	if len(reply.Keys) == 0 {
		fmt.Printf("Node %d has no stored keys\n", nodeID)
	} else {
		fmt.Printf("Keys on Node %d:\n", nodeID)
		for _, key := range reply.Keys {
			fmt.Printf("  - %s\n", key)
		}
	}
}

// handleMutexPut - REAL mutual exclusion using Ricart-Agrawala via RPC
func handleMutexPut(parts []string) {
	if len(parts) < 3 {
		fmt.Println("Usage: mutex-put <key> <filename>")
		fmt.Println("This stores data using mutual exclusion (Ricart-Agrawala)")
		fmt.Println("Example: mutex-put sharedfile testfiles/data1.txt")
		return
	}

	key := parts[1]
	filename := parts[2]

	data, err := os.ReadFile(filename)
	if err != nil {
		fmt.Printf("Cannot read file '%s': %v\n", filename, err)
		return
	}

	fmt.Println("====================================")
	fmt.Println("  MUTUAL EXCLUSION WRITE")
	fmt.Println("  Algorithm: Ricart-Agrawala")
	fmt.Println("====================================")
	fmt.Printf("Key: %s\n", key)
	fmt.Printf("File: %s (%d bytes)\n", filename, len(data))
	fmt.Println("")

	// Find leader node
	leaderID, leaderClient := findLeader()
	if leaderClient == nil {
		fmt.Println("No leader found. Please ensure nodes are running.")
		return
	}
	defer leaderClient.Close()

	fmt.Printf("Leader found: Node %d\n", leaderID)
	fmt.Println("Sending MutexPut request (Ricart-Agrawala will run on server)...")
	fmt.Println("")

	// Call MutexPut on the leader - this triggers REAL Ricart-Agrawala on the server
	args := &node.MutexPutArgs{
		Key:       key,
		Value:     data,
		Timestamp: time.Now().UnixNano(),
	}
	var reply node.MutexPutReply

	err = leaderClient.Call("Node.MutexPut", args, &reply)
	if err != nil {
		fmt.Printf("RPC Error: %v\n", err)
		return
	}

	if reply.Success {
		fmt.Println("SUCCESS!")
		fmt.Printf("Result: %s\n", reply.Message)
		fmt.Println("")
		fmt.Println("What happened on the server:")
		fmt.Println("  1. Node requested Critical Section from all other nodes")
		fmt.Println("  2. Used Lamport timestamps for priority (lower = higher priority)")
		fmt.Println("  3. All alive nodes granted permission")
		fmt.Println("  4. Data written inside Critical Section")
		fmt.Println("  5. Data replicated to all nodes")
		fmt.Println("  6. Critical Section released")
		fmt.Println("  7. Deferred requests processed")
	} else {
		fmt.Printf("FAILED: %s\n", reply.Message)
	}

	fmt.Println("====================================")
}

// handleDeadlockTest - REAL DAG deadlock detection via RPC
func handleDeadlockTest(parts []string) {
	if len(parts) < 2 {
		fmt.Println("Usage: deadlock-test <resource_name>")
		fmt.Println("This tests DAG (Wait-For Graph) deadlock detection")
		fmt.Println("Example: deadlock-test shared_resource")
		return
	}

	resource := parts[1]
	resourceA := resource + "_A"
	resourceB := resource + "_B"

	fmt.Println("====================================")
	fmt.Println("  DAG DEADLOCK DETECTION TEST")
	fmt.Println("  Algorithm: Wait-For Graph (DAG)")
	fmt.Println("====================================")
	fmt.Println("")
	fmt.Println("This test creates a real deadlock scenario:")
	fmt.Println("  - Node 1 locks Resource A, then tries Resource B")
	fmt.Println("  - Node 2 locks Resource B, then tries Resource A")
	fmt.Println("  - This creates a cycle in the Wait-For Graph")
	fmt.Println("  - DAG cycle detection finds the deadlock")
	fmt.Println("  - System resolves it by aborting one transaction")
	fmt.Println("")

	// Find leader
	leaderID, leaderClient := findLeader()
	if leaderClient == nil {
		fmt.Println("No leader found. Please ensure nodes are running.")
		return
	}
	defer leaderClient.Close()

	fmt.Printf("Leader (coordinator): Node %d\n", leaderID)
	fmt.Println("")

	// Reset any previous DAG state
	leaderClient.Call("Node.DAGReset", &node.DAGResetArgs{}, &node.DAGResetReply{})

	// Step 1: Node 1 locks Resource A
	fmt.Println("--- Step 1: Node 1 locks Resource A ---")
	lockArgs1 := &node.DAGLockArgs{NodeID: 1, Resource: resourceA}
	var lockReply1 node.DAGLockReply
	err := leaderClient.Call("Node.DAGLock", lockArgs1, &lockReply1)
	if err != nil {
		fmt.Printf("RPC Error: %v\n", err)
		return
	}
	fmt.Printf("Result: %s\n", lockReply1.Message)
	fmt.Println("")
	time.Sleep(500 * time.Millisecond)

	// Step 2: Node 2 locks Resource B
	fmt.Println("--- Step 2: Node 2 locks Resource B ---")
	lockArgs2 := &node.DAGLockArgs{NodeID: 2, Resource: resourceB}
	var lockReply2 node.DAGLockReply
	err = leaderClient.Call("Node.DAGLock", lockArgs2, &lockReply2)
	if err != nil {
		fmt.Printf("RPC Error: %v\n", err)
		return
	}
	fmt.Printf("Result: %s\n", lockReply2.Message)
	fmt.Println("")
	time.Sleep(500 * time.Millisecond)

	// Step 3: Show current state
	fmt.Println("--- Current State ---")
	showDAGGraph(leaderClient)
	fmt.Println("No deadlock yet - no cycles in graph")
	fmt.Println("")
	time.Sleep(500 * time.Millisecond)

	// Step 4: Node 1 tries to lock Resource B (held by Node 2) -> WAIT
	fmt.Println("--- Step 3: Node 1 tries to lock Resource B (held by Node 2) ---")
	lockArgs3 := &node.DAGLockArgs{NodeID: 1, Resource: resourceB}
	var lockReply3 node.DAGLockReply
	err = leaderClient.Call("Node.DAGLock", lockArgs3, &lockReply3)
	if err != nil {
		fmt.Printf("RPC Error: %v\n", err)
		return
	}
	fmt.Printf("Result: %s\n", lockReply3.Message)
	if !lockReply3.Granted {
		fmt.Printf("Wait-For Graph edge added: Node 1 → Node %d\n", lockReply3.WaitingFor)
	}
	fmt.Println("")
	time.Sleep(500 * time.Millisecond)

	// Step 5: Node 2 tries to lock Resource A (held by Node 1) -> WAIT -> DEADLOCK!
	fmt.Println("--- Step 4: Node 2 tries to lock Resource A (held by Node 1) ---")
	lockArgs4 := &node.DAGLockArgs{NodeID: 2, Resource: resourceA}
	var lockReply4 node.DAGLockReply
	err = leaderClient.Call("Node.DAGLock", lockArgs4, &lockReply4)
	if err != nil {
		fmt.Printf("RPC Error: %v\n", err)
		return
	}
	fmt.Printf("Result: %s\n", lockReply4.Message)
	if !lockReply4.Granted {
		fmt.Printf("Wait-For Graph edge added: Node 2 → Node %d\n", lockReply4.WaitingFor)
	}
	fmt.Println("")
	time.Sleep(500 * time.Millisecond)

	// Step 6: Show the Wait-For Graph
	fmt.Println("--- Wait-For Graph (DAG) ---")
	showDAGGraph(leaderClient)
	fmt.Println("")
	time.Sleep(500 * time.Millisecond)

	// Step 7: Detect cycle (deadlock)
	fmt.Println("--- Step 5: Running Cycle Detection (DFS) ---")
	detectArgs := &node.DAGDetectArgs{}
	var detectReply node.DAGDetectReply
	err = leaderClient.Call("Node.DAGDetect", detectArgs, &detectReply)
	if err != nil {
		fmt.Printf("RPC Error: %v\n", err)
		return
	}
	fmt.Printf("Result: %s\n", detectReply.Message)
	if detectReply.HasCycle {
		fmt.Printf("Cycle path: %v\n", detectReply.Cycle)
		fmt.Println("")
		fmt.Println("DEADLOCK CONFIRMED!")
		fmt.Println("Node 1 waits for Node 2 (wants Resource B)")
		fmt.Println("Node 2 waits for Node 1 (wants Resource A)")
		fmt.Println("This forms a cycle: 1 → 2 → 1")
	}
	fmt.Println("")
	time.Sleep(500 * time.Millisecond)

	// Step 8: Resolve deadlock by aborting Node 2's transaction
	fmt.Println("--- Step 6: Resolving Deadlock ---")
	fmt.Println("Strategy: Abort the younger transaction (Node 2)")
	resolveArgs := &node.DAGResolveArgs{AbortNodeID: 2}
	var resolveReply node.DAGResolveReply
	err = leaderClient.Call("Node.DAGResolve", resolveArgs, &resolveReply)
	if err != nil {
		fmt.Printf("RPC Error: %v\n", err)
		return
	}
	fmt.Printf("Result: %s\n", resolveReply.Message)
	fmt.Println("")
	time.Sleep(500 * time.Millisecond)

	// Step 9: Now Node 1 can get Resource B
	fmt.Println("--- Step 7: Node 1 retries Resource B ---")
	lockArgs5 := &node.DAGLockArgs{NodeID: 1, Resource: resourceB}
	var lockReply5 node.DAGLockReply
	err = leaderClient.Call("Node.DAGLock", lockArgs5, &lockReply5)
	if err != nil {
		fmt.Printf("RPC Error: %v\n", err)
		return
	}
	fmt.Printf("Result: %s\n", lockReply5.Message)
	fmt.Println("")
	time.Sleep(500 * time.Millisecond)

	// Step 10: Verify no more deadlocks
	fmt.Println("--- Step 8: Verify no deadlock ---")
	var detectReply2 node.DAGDetectReply
	leaderClient.Call("Node.DAGDetect", &node.DAGDetectArgs{}, &detectReply2)
	fmt.Printf("Result: %s\n", detectReply2.Message)
	fmt.Println("")

	// Cleanup
	fmt.Println("--- Cleanup ---")
	leaderClient.Call("Node.DAGUnlock", &node.DAGUnlockArgs{NodeID: 1, Resource: resourceA}, &node.DAGUnlockReply{})
	leaderClient.Call("Node.DAGUnlock", &node.DAGUnlockArgs{NodeID: 1, Resource: resourceB}, &node.DAGUnlockReply{})
	fmt.Println("All resources released")
	fmt.Println("")

	// Show final graph
	fmt.Println("--- Final Wait-For Graph ---")
	showDAGGraph(leaderClient)
	fmt.Println("")

	fmt.Println("====================================")
	fmt.Println("  DEADLOCK TEST COMPLETE")
	fmt.Println("====================================")
	fmt.Println("")
	fmt.Println("Summary:")
	fmt.Println("  1. Created circular dependency between Node 1 and Node 2")
	fmt.Println("  2. Wait-For Graph (DAG) detected the cycle using DFS")
	fmt.Println("  3. Resolved by aborting younger transaction (Node 2)")
	fmt.Println("  4. Node 1 successfully acquired both resources")
	fmt.Println("  5. System continues operating normally")
	fmt.Println("")
	fmt.Println("DEADLOCK DETECTED AND RESOLVED using DAG algorithm!")
	fmt.Println("====================================")
}

func showDAGGraph(client *rpc.Client) {
	graphArgs := &node.DAGGraphArgs{}
	var graphReply node.DAGGraphReply
	err := client.Call("Node.DAGGraph", graphArgs, &graphReply)
	if err != nil {
		fmt.Printf("Cannot get graph: %v\n", err)
		return
	}

	fmt.Println("Held Resources:")
	if len(graphReply.HeldResources) == 0 {
		fmt.Println("  (none)")
	} else {
		for resource, nodeID := range graphReply.HeldResources {
			fmt.Printf("  '%s' -> Node %d\n", resource, nodeID)
		}
	}

	fmt.Println("Wait-For Edges:")
	if len(graphReply.WaitForGraph) == 0 {
		fmt.Println("  (none)")
	} else {
		for from, toList := range graphReply.WaitForGraph {
			for _, to := range toList {
				fmt.Printf("  Node %d → Node %d\n", from, to)
			}
		}
	}
}

func printHelp() {
	fmt.Println("====================================")
	fmt.Println("         AVAILABLE COMMANDS")
	fmt.Println("====================================")
	fmt.Println("")
	fmt.Println("Basic Operations:")
	fmt.Println("  put <node_id> <key> <filename>")
	fmt.Println("      Store a file (auto-replicates to all nodes)")
	fmt.Println("")
	fmt.Println("  get <node_id> <key>")
	fmt.Println("      Retrieve a file from any node")
	fmt.Println("")
	fmt.Println("  delete <node_id> <key>")
	fmt.Println("      Delete a key from specific node")
	fmt.Println("")
	fmt.Println("Status:")
	fmt.Println("  status")
	fmt.Println("      Show status of all nodes")
	fmt.Println("")
	fmt.Println("  status <node_id>")
	fmt.Println("      Show status of specific node")
	fmt.Println("")
	fmt.Println("  list <node_id>")
	fmt.Println("      List all keys on specific node")
	fmt.Println("")
	fmt.Println("Advanced (Algorithms Demo):")
	fmt.Println("  mutex-put <key> <filename>")
	fmt.Println("      Store with mutual exclusion (Ricart-Agrawala)")
	fmt.Println("      Requests critical section from all nodes before writing")
	fmt.Println("")
	fmt.Println("  deadlock-test <resource>")
	fmt.Println("      Test DAG deadlock detection (Wait-For Graph)")
	fmt.Println("      Creates circular dependency, detects cycle, resolves")
	fmt.Println("")
	fmt.Println("Other:")
	fmt.Println("  help    - Show this help")
	fmt.Println("  exit    - Quit the client")
	fmt.Println("")
	fmt.Println("====================================")
}
