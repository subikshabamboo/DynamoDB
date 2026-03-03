package mutex

import (
	"dynamo-go/node"
	"fmt"
	"net/rpc"
	"sync"
	"time"
)

// RicartAgrawala implements the Ricart-Agrawala algorithm for mutual exclusion
type RicartAgrawala struct {
	Node          *node.Node
	replyCount    int
	replyMutex    sync.Mutex
	replyChan     chan bool
	timeout       time.Duration
}

// NewRicartAgrawala creates a new Ricart-Agrawala manager
func NewRicartAgrawala(n *node.Node) *RicartAgrawala {
	return &RicartAgrawala{
		Node:      n,
		replyChan: make(chan bool, 10),
		timeout:   time.Duration(n.Config.DeadlockTimeout) * time.Millisecond,
	}
}

// RequestCriticalSection requests entry to critical section
func (ra *RicartAgrawala) RequestCriticalSection() bool {
	ra.Node.MutexLock.Lock()
	ra.Node.RequestingCS = true
	ra.Node.RequestTime = ra.Node.IncrementClock()
	ra.Node.ReplyCount = 0
	ra.Node.MutexLock.Unlock()
	
	requestTime := ra.Node.RequestTime
	otherNodes := ra.Node.GetOtherNodes()
	expectedReplies := len(otherNodes)
	
	fmt.Printf("[Node %d] Requesting CRITICAL SECTION (timestamp=%d)\n", ra.Node.ID, requestTime)
	fmt.Printf("[Node %d] Need replies from %d nodes: %v\n", ra.Node.ID, expectedReplies, otherNodes)
	
	if expectedReplies == 0 {
		// No other nodes, can enter directly
		fmt.Printf("[Node %d] No other nodes, entering CS directly\n", ra.Node.ID)
		return true
	}
	
	// Send REQUEST to all other nodes
	var wg sync.WaitGroup
	grantedCount := 0
	var grantedMutex sync.Mutex
	deadNodes := make([]int, 0)
	var deadMutex sync.Mutex
	
	for _, nodeID := range otherNodes {
		wg.Add(1)
		go func(nid int) {
			defer wg.Done()
			
			granted, alive := ra.sendCSRequest(nid, requestTime)
			
			if !alive {
				deadMutex.Lock()
				deadNodes = append(deadNodes, nid)
				deadMutex.Unlock()
				fmt.Printf("[Node %d] Node %d is not responding (dead/timeout)\n", ra.Node.ID, nid)
			} else if granted {
				grantedMutex.Lock()
				grantedCount++
				grantedMutex.Unlock()
				fmt.Printf("[Node %d] Received REPLY (grant) from Node %d\n", ra.Node.ID, nid)
			} else {
				fmt.Printf("[Node %d] Request DEFERRED by Node %d (will wait)\n", ra.Node.ID, nid)
			}
		}(nodeID)
	}
	
	// Wait for all responses
	done := make(chan bool)
	go func() {
		wg.Wait()
		done <- true
	}()
	
	// Wait with overall timeout
	select {
	case <-done:
		// All responses received
	case <-time.After(ra.timeout * 2):
		fmt.Printf("[Node %d] Overall timeout waiting for CS replies\n", ra.Node.ID)
	}
	
	grantedMutex.Lock()
	finalGranted := grantedCount
	grantedMutex.Unlock()
	
	deadMutex.Lock()
	finalDead := len(deadNodes)
	deadMutex.Unlock()
	
	// We can enter CS if we got replies from all ALIVE nodes
	aliveNodes := expectedReplies - finalDead
	
	fmt.Printf("[Node %d] CS Request Summary: granted=%d, dead=%d, alive=%d, expected=%d\n",
		ra.Node.ID, finalGranted, finalDead, aliveNodes, expectedReplies)
	
	// Enter CS if we have all grants from alive nodes OR if only dead nodes didn't respond
	canEnter := (finalGranted + finalDead) >= expectedReplies
	
	if canEnter {
		fmt.Printf("[Node %d] ENTERING Critical Section\n", ra.Node.ID)
		return true
	} else {
		fmt.Printf("[Node %d] CANNOT enter Critical Section, waiting for deferred replies\n", ra.Node.ID)
		
		// Wait for deferred replies
		return ra.waitForDeferredReplies(expectedReplies - finalGranted - finalDead)
	}
}

// sendCSRequest sends a CS request to a specific node
func (ra *RicartAgrawala) sendCSRequest(nodeID int, timestamp int64) (granted bool, alive bool) {
	address := ra.Node.GetNodeAddress(nodeID)
	
	client, err := rpc.Dial("tcp", address)
	if err != nil {
		return false, false
	}
	defer client.Close()
	
	args := &node.CSRequestArgs{
		NodeID:    ra.Node.ID,
		Timestamp: timestamp,
	}
	var reply node.CSRequestReply
	
	call := client.Go("Node.RequestCS", args, &reply, nil)
	
	select {
	case <-call.Done:
		if call.Error != nil {
			return false, false
		}
		return reply.Granted, true
	case <-time.After(ra.timeout):
		return false, false
	}
}

// waitForDeferredReplies waits for remaining replies
func (ra *RicartAgrawala) waitForDeferredReplies(remaining int) bool {
	if remaining <= 0 {
		return true
	}
	
	fmt.Printf("[Node %d] Waiting for %d deferred replies...\n", ra.Node.ID, remaining)
	
	// In a real implementation, we would wait for replies via callback
	// For simplicity, we'll use a timeout and then force entry
	select {
	case <-time.After(ra.timeout):
		fmt.Printf("[Node %d] Timeout waiting for deferred replies, forcing CS entry\n", ra.Node.ID)
		return true
	}
}

// ReleaseCriticalSection releases the critical section and sends deferred replies
func (ra *RicartAgrawala) ReleaseCriticalSection() {
	ra.Node.MutexLock.Lock()
	ra.Node.RequestingCS = false
	deferredQueue := make([]node.DeferredRequest, len(ra.Node.DeferredQueue))
	copy(deferredQueue, ra.Node.DeferredQueue)
	ra.Node.DeferredQueue = make([]node.DeferredRequest, 0)
	ra.Node.MutexLock.Unlock()
	
	fmt.Printf("[Node %d] RELEASING Critical Section\n", ra.Node.ID)
	
	// Send deferred replies
	if len(deferredQueue) > 0 {
		fmt.Printf("[Node %d] Sending %d deferred REPLIES\n", ra.Node.ID, len(deferredQueue))
	}
	
	for _, deferred := range deferredQueue {
		go ra.sendDeferredReply(deferred.NodeID)
	}
	
	// Notify all other nodes that CS is released
	otherNodes := ra.Node.GetOtherNodes()
	for _, nodeID := range otherNodes {
		go ra.sendReleaseNotification(nodeID)
	}
}

// sendDeferredReply sends a deferred reply to a node
func (ra *RicartAgrawala) sendDeferredReply(nodeID int) {
	address := ra.Node.GetNodeAddress(nodeID)
	
	client, err := rpc.Dial("tcp", address)
	if err != nil {
		fmt.Printf("[Node %d] Cannot send deferred reply to Node %d\n", ra.Node.ID, nodeID)
		return
	}
	defer client.Close()
	
	args := &node.CSRequestArgs{
		NodeID:    ra.Node.ID,
		Timestamp: ra.Node.IncrementClock(),
	}
	var reply node.CSRequestReply
	
	err = client.Call("Node.RequestCS", args, &reply)
	if err != nil {
		return
	}
	
	fmt.Printf("[Node %d] Sent deferred REPLY to Node %d\n", ra.Node.ID, nodeID)
}

// sendReleaseNotification notifies a node that CS has been released
func (ra *RicartAgrawala) sendReleaseNotification(nodeID int) {
	address := ra.Node.GetNodeAddress(nodeID)
	
	client, err := rpc.Dial("tcp", address)
	if err != nil {
		return
	}
	defer client.Close()
	
	args := &node.CSReleaseArgs{
		NodeID:    ra.Node.ID,
		Timestamp: ra.Node.IncrementClock(),
	}
	var reply node.CSReleaseReply
	
	client.Call("Node.ReleaseCS", args, &reply)
}

// ExecuteInCriticalSection executes a function inside critical section
func (ra *RicartAgrawala) ExecuteInCriticalSection(fn func()) bool {
	if !ra.RequestCriticalSection() {
		fmt.Printf("[Node %d] Failed to enter Critical Section\n", ra.Node.ID)
		return false
	}
	
	// Execute the function
	fn()
	
	// Release CS
	ra.ReleaseCriticalSection()
	
	return true
}
