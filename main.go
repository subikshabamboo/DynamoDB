package main

import (
	"dynamo-go/deadlock"
	"dynamo-go/election"
	"dynamo-go/mutex"
	"dynamo-go/node"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"os/signal"
	"strconv"
	"syscall"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <node_id>")
		fmt.Println("Example: go run main.go 1")
		fmt.Println("")
		fmt.Println("Node IDs: 1, 2, or 3")
		os.Exit(1)
	}

	nodeID, err := strconv.Atoi(os.Args[1])
	if err != nil {
		fmt.Printf("Invalid node ID: %s\n", os.Args[1])
		os.Exit(1)
	}

	// Load configuration
	config, err := node.LoadConfig("config.json")
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Create node
	n := node.NewNode(nodeID, config)

	// Initialize Mutual Exclusion Manager (Ricart-Agrawala)
	mutexMgr := mutex.NewRicartAgrawala(n)
	n.MutexMgr = mutexMgr

	// Initialize Deadlock Detection Manager (DAG / Wait-For Graph)
	dagMgr := deadlock.NewDAGManager()
	n.DeadlockMgr = dagMgr

	// Register RPC handlers
	err = rpc.Register(n)
	if err != nil {
		fmt.Printf("Failed to register RPC: %v\n", err)
		os.Exit(1)
	}

	// Start listening
	address := n.GetAddress()
	listener, err := net.Listen("tcp", address)
	if err != nil {
		fmt.Printf("Failed to start listener on %s: %v\n", address, err)
		os.Exit(1)
	}

	fmt.Println("====================================")
	fmt.Printf("    DISTRIBUTED SYSTEM - NODE %d\n", n.ID)
	fmt.Println("====================================")
	fmt.Printf("Address: %s\n", address)
	fmt.Println("Algorithms:")
	fmt.Println("  1. Bully Algorithm (Leader Election)")
	fmt.Println("  2. Ricart-Agrawala (Mutual Exclusion)")
	fmt.Println("  3. DAG Wait-For Graph (Deadlock Detection)")
	fmt.Println("Status: RUNNING")
	fmt.Println("====================================")
	fmt.Println("")

	// Start accepting RPC connections
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				continue
			}
			go rpc.ServeConn(conn)
		}
	}()

	// Initialize election manager
	electionMgr := election.NewBullyElection(n)

	// Start heartbeat monitoring
	electionMgr.StartHeartbeatMonitor()

	// Trigger initial election
	go electionMgr.TriggerElectionOnStartup()

	fmt.Println("[Node", n.ID, "] Ready and waiting for connections...")
	fmt.Println("[Node", n.ID, "] Press Ctrl+C to stop")
	fmt.Println("")

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("")
	fmt.Printf("[Node %d] Shutting down...\n", n.ID)
	electionMgr.StopHeartbeatMonitor()
	listener.Close()
	fmt.Printf("[Node %d] Stopped\n", n.ID)
}
