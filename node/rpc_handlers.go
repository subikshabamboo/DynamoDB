package node

import (
	"fmt"
	"net/rpc"
	"strings"
)

// ===== Basic Storage Operations =====

// PutArgs arguments for Put operation
type PutArgs struct {
	Key       string
	Value     []byte
	Timestamp int64
	IsReplica bool // If true, don't replicate further (prevents infinite loop)
}

// PutReply response for Put operation
type PutReply struct {
	Success    bool
	Message    string
	Replicated bool
}

// GetArgs arguments for Get operation
type GetArgs struct {
	Key string
}

// GetReply response for Get operation
type GetReply struct {
	Value     []byte
	Found     bool
	Timestamp int64
	Message   string
}

// DeleteArgs arguments for Delete operation
type DeleteArgs struct {
	Key       string
	Timestamp int64
	IsReplica bool // If true, this is a propagated deletion
}

// DeleteReply response for Delete operation
type DeleteReply struct {
	Success bool
	Message string
}

// Put stores a key-value pair and replicates to other nodes based on partitioning
func (n *Node) Put(args *PutArgs, reply *PutReply) error {
	n.UpdateClock(args.Timestamp)

	n.DataMutex.Lock()
	n.DataStore[args.Key] = StoredItem{
		Value:     args.Value,
		Timestamp: args.Timestamp,
	}
	n.DataMutex.Unlock()

	reply.Success = true

	if args.IsReplica {
		reply.Message = fmt.Sprintf("Replicated key '%s' on Node %d", args.Key, n.ID)
		fmt.Printf("[Node %d] REPLICATED: key='%s', size=%d bytes\n",
			n.ID, args.Key, len(args.Value))
	} else {
		// Replicate to partition-assigned nodes
		fmt.Printf("[Node %d] PUT: key='%s', size=%d bytes, timestamp=%d\n",
			n.ID, args.Key, len(args.Value), args.Timestamp)

		replicatedTo := n.replicateToPartitionNodes(args.Key, args.Value, args.Timestamp)
		reply.Replicated = true
		reply.Message = fmt.Sprintf("Stored key '%s' on Node %d, replicated to: %s",
			args.Key, n.ID, strings.Join(replicatedTo, ", "))
	}

	return nil
}

// replicateToPartitionNodes sends data to partition-assigned replica nodes
// Respects the partitioning configuration - if disabled, replicates to all other nodes
func (n *Node) replicateToPartitionNodes(key string, value []byte, timestamp int64) []string {
	results := make([]string, 0)

	// Determine which nodes should store this key
	var targetNodes []int

	if n.PartitionMgr.IsEnabled() {
		// Partitioning enabled: use consistent hashing to determine replicas
		partitionOwners := n.PartitionMgr.GetPartitionOwners(key)

		// Filter out current node (we already stored it)
		for _, nodeID := range partitionOwners {
			if nodeID != n.ID {
				targetNodes = append(targetNodes, nodeID)
			}
		}

		fmt.Printf("[Node %d] PARTITION-AWARE REPLICATION: key='%s' to nodes %v\n",
			n.ID, key, partitionOwners)
	} else {
		// Full replication: replicate to all other nodes
		targetNodes = n.GetOtherNodes()
		fmt.Printf("[Node %d] FULL REPLICATION: key='%s' to all other nodes\n",
			n.ID, key)
	}

	// Send replication to target nodes
	for _, nodeID := range targetNodes {
		address := n.GetNodeAddress(nodeID)
		client, err := rpc.Dial("tcp", address)
		if err != nil {
			fmt.Printf("[Node %d] Cannot replicate to Node %d: %v\n", n.ID, nodeID, err)
			results = append(results, fmt.Sprintf("Node %d (failed)", nodeID))
			continue
		}

		putArgs := &PutArgs{
			Key:       key,
			Value:     value,
			Timestamp: timestamp,
			IsReplica: true,
		}
		var putReply PutReply
		err = client.Call("Node.Put", putArgs, &putReply)
		client.Close()

		if err != nil {
			fmt.Printf("[Node %d] Replication to Node %d failed: %v\n", n.ID, nodeID, err)
			results = append(results, fmt.Sprintf("Node %d (failed)", nodeID))
		} else {
			fmt.Printf("[Node %d] Replicated key '%s' to Node %d\n", n.ID, key, nodeID)
			results = append(results, fmt.Sprintf("Node %d (ok)", nodeID))
		}
	}

	return results
}

// Get retrieves a value by key
func (n *Node) Get(args *GetArgs, reply *GetReply) error {
	n.IncrementClock()

	n.DataMutex.RLock()
	defer n.DataMutex.RUnlock()

	item, found := n.DataStore[args.Key]
	reply.Found = found

	if found {
		reply.Value = item.Value
		reply.Timestamp = item.Timestamp
		reply.Message = fmt.Sprintf("Found key '%s' on Node %d", args.Key, n.ID)
		fmt.Printf("[Node %d] GET: key='%s', found=true, size=%d bytes\n",
			n.ID, args.Key, len(item.Value))
	} else {
		reply.Message = fmt.Sprintf("Key '%s' not found on Node %d", args.Key, n.ID)
		fmt.Printf("[Node %d] GET: key='%s', found=false\n", n.ID, args.Key)
	}

	return nil
}

// Delete removes a key-value pair and propagates deletion to replica nodes
func (n *Node) Delete(args *DeleteArgs, reply *DeleteReply) error {
	n.UpdateClock(args.Timestamp)

	n.DataMutex.Lock()
	_, found := n.DataStore[args.Key]
	if found {
		delete(n.DataStore, args.Key)
	}
	n.DataMutex.Unlock()

	reply.Success = found

	if args.IsReplica {
		reply.Message = fmt.Sprintf("Deleted key '%s' from Node %d (replica)", args.Key, n.ID)
		fmt.Printf("[Node %d] DELETED (replica): key='%s'\n", n.ID, args.Key)
	} else {
		if found {
			fmt.Printf("[Node %d] DELETE: key='%s', success=true, timestamp=%d\n", n.ID, args.Key, args.Timestamp)

			// Propagate deletion to partition-assigned replicas
			if n.PartitionMgr.IsEnabled() {
				partitionOwners := n.PartitionMgr.GetPartitionOwners(args.Key)
				for _, nodeID := range partitionOwners {
					if nodeID != n.ID {
						go n.propagateDelete(nodeID, args.Key, args.Timestamp)
					}
				}
			} else {
				// Full replication: propagate to all other nodes
				for _, nodeID := range n.GetOtherNodes() {
					go n.propagateDelete(nodeID, args.Key, args.Timestamp)
				}
			}

			reply.Message = fmt.Sprintf("Deleted key '%s' from Node %d", args.Key, n.ID)
		} else {
			reply.Message = fmt.Sprintf("Key '%s' not found on Node %d", args.Key, n.ID)
			fmt.Printf("[Node %d] DELETE: key='%s', not found\n", n.ID, args.Key)
		}
	}

	return nil
}

// propagateDelete sends deletion to a specific node
func (n *Node) propagateDelete(nodeID int, key string, timestamp int64) {
	address := n.GetNodeAddress(nodeID)
	client, err := rpc.Dial("tcp", address)
	if err != nil {
		fmt.Printf("[Node %d] Cannot propagate delete to Node %d: %v\n", n.ID, nodeID, err)
		return
	}
	defer client.Close()

	deleteArgs := &DeleteArgs{
		Key:       key,
		Timestamp: timestamp,
		IsReplica: true,
	}
	var deleteReply DeleteReply
	err = client.Call("Node.Delete", deleteArgs, &deleteReply)
	if err != nil {
		fmt.Printf("[Node %d] Propagate delete to Node %d failed: %v\n", n.ID, nodeID, err)
	} else {
		fmt.Printf("[Node %d] Propagated delete for key '%s' to Node %d\n", n.ID, key, nodeID)
	}
}

// ===== Leader Election (Bully Algorithm) =====

// ElectionArgs arguments for Election message
type ElectionArgs struct {
	CandidateID int
	Timestamp   int64
}

// ElectionReply response for Election message
type ElectionReply struct {
	Acknowledged bool
	NodeID       int
}

// CoordinatorArgs arguments for Coordinator announcement
type CoordinatorArgs struct {
	LeaderID  int
	Timestamp int64
}

// CoordinatorReply response for Coordinator announcement
type CoordinatorReply struct {
	Acknowledged bool
}

// HeartbeatArgs arguments for Heartbeat
type HeartbeatArgs struct {
	LeaderID  int
	Timestamp int64
}

// HeartbeatReply response for Heartbeat
type HeartbeatReply struct {
	Alive bool
}

// Election handles incoming election message (Bully Algorithm)
func (n *Node) Election(args *ElectionArgs, reply *ElectionReply) error {
	n.UpdateClock(args.Timestamp)

	fmt.Printf("[Node %d] Received ELECTION from Node %d\n", n.ID, args.CandidateID)

	reply.Acknowledged = true
	reply.NodeID = n.ID

	if n.ID > args.CandidateID {
		fmt.Printf("[Node %d] I have higher ID, will start my own election\n", n.ID)
	}

	return nil
}

// Coordinator handles coordinator announcement
func (n *Node) Coordinator(args *CoordinatorArgs, reply *CoordinatorReply) error {
	n.UpdateClock(args.Timestamp)

	fmt.Printf("[Node %d] Received COORDINATOR: Node %d is the new leader\n", n.ID, args.LeaderID)

	n.SetLeader(args.LeaderID)
	reply.Acknowledged = true

	return nil
}

// Heartbeat handles heartbeat from leader
func (n *Node) Heartbeat(args *HeartbeatArgs, reply *HeartbeatReply) error {
	n.UpdateClock(args.Timestamp)
	reply.Alive = true
	return nil
}

// ===== Mutual Exclusion (Ricart-Agrawala Algorithm) =====

// CSRequestArgs arguments for Critical Section request
type CSRequestArgs struct {
	NodeID    int
	Timestamp int64
}

// CSRequestReply response for Critical Section request
type CSRequestReply struct {
	Granted bool
}

// CSReleaseArgs arguments for Critical Section release notification
type CSReleaseArgs struct {
	NodeID    int
	Timestamp int64
}

// CSReleaseReply response for Critical Section release
type CSReleaseReply struct {
	Acknowledged bool
}

// RequestCS handles incoming critical section request (Ricart-Agrawala)
func (n *Node) RequestCS(args *CSRequestArgs, reply *CSRequestReply) error {
	n.UpdateClock(args.Timestamp)

	fmt.Printf("[Node %d] Received CS REQUEST from Node %d (timestamp=%d)\n",
		n.ID, args.NodeID, args.Timestamp)

	n.MutexLock.Lock()
	defer n.MutexLock.Unlock()

	shouldDefer := false

	if n.RequestingCS {
		if args.Timestamp > n.RequestTime {
			shouldDefer = true
			fmt.Printf("[Node %d] My request (timestamp=%d) is older, deferring Node %d's request\n",
				n.ID, n.RequestTime, args.NodeID)
		} else if args.Timestamp == n.RequestTime {
			if args.NodeID > n.ID {
				shouldDefer = true
				fmt.Printf("[Node %d] Same timestamp, my ID is lower, deferring Node %d's request\n",
					n.ID, args.NodeID)
			}
		}
	}

	if shouldDefer {
		n.DeferredQueue = append(n.DeferredQueue, DeferredRequest{
			NodeID:    args.NodeID,
			Timestamp: args.Timestamp,
		})
		reply.Granted = false
		fmt.Printf("[Node %d] Deferred reply to Node %d\n", n.ID, args.NodeID)
	} else {
		reply.Granted = true
		fmt.Printf("[Node %d] Granted CS to Node %d immediately\n", n.ID, args.NodeID)
	}

	return nil
}

// ReleaseCS handles notification that a node has released CS
func (n *Node) ReleaseCS(args *CSReleaseArgs, reply *CSReleaseReply) error {
	n.UpdateClock(args.Timestamp)

	fmt.Printf("[Node %d] Node %d released Critical Section\n", n.ID, args.NodeID)
	reply.Acknowledged = true

	return nil
}

// ===== Mutual Exclusion Put (uses Ricart-Agrawala) =====

// MutexPutArgs arguments for mutex-protected put
type MutexPutArgs struct {
	Key       string
	Value     []byte
	Timestamp int64
}

// MutexPutReply response for mutex-protected put
type MutexPutReply struct {
	Success bool
	Message string
}

// MutexPut stores data with mutual exclusion using Ricart-Agrawala
func (n *Node) MutexPut(args *MutexPutArgs, reply *MutexPutReply) error {
	n.UpdateClock(args.Timestamp)

	fmt.Printf("[Node %d] ===== MUTEX-PUT START for key '%s' =====\n", n.ID, args.Key)

	if n.MutexMgr == nil {
		reply.Success = false
		reply.Message = "Mutex manager not initialized"
		return nil
	}

	fmt.Printf("[Node %d] Requesting critical section via Ricart-Agrawala...\n", n.ID)

	success := n.MutexMgr.ExecuteInCriticalSection(func() {
		// Inside critical section - store locally
		n.DataMutex.Lock()
		n.DataStore[args.Key] = StoredItem{Value: args.Value, Timestamp: args.Timestamp}
		n.DataMutex.Unlock()
		fmt.Printf("[Node %d] MUTEX-PUT: Stored key '%s' locally inside Critical Section\n", n.ID, args.Key)

		// Replicate to other nodes (still inside CS for consistency)
		n.replicateToPartitionNodes(args.Key, args.Value, args.Timestamp)
	})

	if success {
		reply.Success = true
		reply.Message = fmt.Sprintf("Key '%s' stored with mutual exclusion (Ricart-Agrawala) and replicated", args.Key)
		fmt.Printf("[Node %d] ===== MUTEX-PUT COMPLETE for key '%s' =====\n", n.ID, args.Key)
	} else {
		reply.Success = false
		reply.Message = "Failed to acquire critical section"
		fmt.Printf("[Node %d] ===== MUTEX-PUT FAILED for key '%s' =====\n", n.ID, args.Key)
	}

	return nil
}

// ===== DAG Deadlock Detection (Wait-For Graph) =====

// DAGLockArgs arguments for locking a resource
type DAGLockArgs struct {
	NodeID   int
	Resource string
}

// DAGLockReply response for lock request
type DAGLockReply struct {
	Granted    bool
	WaitingFor int
	Message    string
}

// DAGUnlockArgs arguments for unlocking a resource
type DAGUnlockArgs struct {
	NodeID   int
	Resource string
}

// DAGUnlockReply response for unlock
type DAGUnlockReply struct {
	Acknowledged bool
	Message      string
}

// DAGDetectArgs arguments for cycle detection
type DAGDetectArgs struct{}

// DAGDetectReply response for cycle detection
type DAGDetectReply struct {
	HasCycle bool
	Cycle    []int
	Message  string
}

// DAGGraphArgs arguments for getting the graph
type DAGGraphArgs struct{}

// DAGGraphReply response with the wait-for graph
type DAGGraphReply struct {
	WaitForGraph  map[int][]int
	HeldResources map[string]int
	Message       string
}

// DAGResolveArgs arguments for resolving deadlock
type DAGResolveArgs struct {
	AbortNodeID int
}

// DAGResolveReply response for resolve
type DAGResolveReply struct {
	Success bool
	Message string
}

// DAGResetArgs arguments for resetting DAG state
type DAGResetArgs struct{}

// DAGResetReply response for reset
type DAGResetReply struct {
	Acknowledged bool
}

// DAGLogsArgs arguments for getting logs
type DAGLogsArgs struct{}

// DAGLogsReply response with logs
type DAGLogsReply struct {
	Logs []string
}

// DAGLock handles resource lock request (only works on leader)
func (n *Node) DAGLock(args *DAGLockArgs, reply *DAGLockReply) error {
	n.IncrementClock()

	if n.DeadlockMgr == nil {
		reply.Message = "Deadlock manager not initialized"
		return nil
	}

	fmt.Printf("[Node %d] DAG-LOCK request: Node %d wants resource '%s'\n",
		n.ID, args.NodeID, args.Resource)

	granted, waitFor := n.DeadlockMgr.Lock(args.NodeID, args.Resource)
	reply.Granted = granted
	reply.WaitingFor = waitFor

	if granted {
		reply.Message = fmt.Sprintf("Resource '%s' granted to Node %d", args.Resource, args.NodeID)
	} else {
		reply.Message = fmt.Sprintf("Node %d waiting for Node %d to release '%s'",
			args.NodeID, waitFor, args.Resource)
	}

	return nil
}

// DAGUnlock handles resource unlock request
func (n *Node) DAGUnlock(args *DAGUnlockArgs, reply *DAGUnlockReply) error {
	n.IncrementClock()

	if n.DeadlockMgr == nil {
		reply.Message = "Deadlock manager not initialized"
		return nil
	}

	fmt.Printf("[Node %d] DAG-UNLOCK: Node %d releasing resource '%s'\n",
		n.ID, args.NodeID, args.Resource)

	n.DeadlockMgr.Unlock(args.NodeID, args.Resource)
	reply.Acknowledged = true
	reply.Message = fmt.Sprintf("Resource '%s' released by Node %d", args.Resource, args.NodeID)

	return nil
}

// DAGDetect runs cycle detection on the Wait-For Graph
func (n *Node) DAGDetect(args *DAGDetectArgs, reply *DAGDetectReply) error {
	if n.DeadlockMgr == nil {
		reply.Message = "Deadlock manager not initialized"
		return nil
	}

	fmt.Printf("[Node %d] Running DAG cycle detection...\n", n.ID)

	hasCycle, cycle := n.DeadlockMgr.DetectCycle()
	reply.HasCycle = hasCycle
	reply.Cycle = cycle

	if hasCycle {
		reply.Message = fmt.Sprintf("DEADLOCK DETECTED! Cycle: %v", cycle)
	} else {
		reply.Message = "No deadlock detected"
	}

	return nil
}

// DAGGraph returns the current Wait-For Graph
func (n *Node) DAGGraph(args *DAGGraphArgs, reply *DAGGraphReply) error {
	if n.DeadlockMgr == nil {
		reply.Message = "Deadlock manager not initialized"
		return nil
	}

	reply.WaitForGraph = n.DeadlockMgr.GetWaitForGraph()
	reply.HeldResources = n.DeadlockMgr.GetHeldResources()
	reply.Message = "Wait-For Graph retrieved"

	return nil
}

// DAGResolve resolves a deadlock by aborting a node's transactions
func (n *Node) DAGResolve(args *DAGResolveArgs, reply *DAGResolveReply) error {
	if n.DeadlockMgr == nil {
		reply.Message = "Deadlock manager not initialized"
		return nil
	}

	fmt.Printf("[Node %d] RESOLVING deadlock: aborting Node %d\n", n.ID, args.AbortNodeID)

	n.DeadlockMgr.Resolve(args.AbortNodeID)
	reply.Success = true
	reply.Message = fmt.Sprintf("Deadlock resolved by aborting Node %d", args.AbortNodeID)

	return nil
}

// DAGReset resets the DAG state
func (n *Node) DAGReset(args *DAGResetArgs, reply *DAGResetReply) error {
	if n.DeadlockMgr == nil {
		return nil
	}

	n.DeadlockMgr.Reset()
	reply.Acknowledged = true
	return nil
}

// DAGLogs returns DAG operation logs
func (n *Node) DAGLogs(args *DAGLogsArgs, reply *DAGLogsReply) error {
	if n.DeadlockMgr == nil {
		return nil
	}

	reply.Logs = n.DeadlockMgr.GetLogs()
	return nil
}

// ===== Status Operations =====

// StatusArgs arguments for status request
type StatusArgs struct{}

// StatusReply response with node status
type StatusReply struct {
	NodeID       int
	IsLeader     bool
	LeaderID     int
	LamportClock int64
	DataCount    int
	IsAlive      bool
}

// Status returns current node status
func (n *Node) Status(args *StatusArgs, reply *StatusReply) error {
	n.LeaderMutex.RLock()
	reply.NodeID = n.ID
	reply.IsLeader = n.IsLeader
	reply.LeaderID = n.LeaderID
	n.LeaderMutex.RUnlock()

	reply.LamportClock = n.GetClock()

	n.DataMutex.RLock()
	reply.DataCount = len(n.DataStore)
	n.DataMutex.RUnlock()

	reply.IsAlive = true

	return nil
}

// ListKeys returns all keys stored on this node
type ListKeysArgs struct{}

type ListKeysReply struct {
	Keys []string
}

func (n *Node) ListKeys(args *ListKeysArgs, reply *ListKeysReply) error {
	n.DataMutex.RLock()
	defer n.DataMutex.RUnlock()

	reply.Keys = make([]string, 0, len(n.DataStore))
	for key := range n.DataStore {
		reply.Keys = append(reply.Keys, key)
	}

	return nil
}

// ===== Partition Management =====

// PartitionInfoArgs arguments for partition info request
type PartitionInfoArgs struct {
	Key string
}

// PartitionInfoReply response with partition ownership information
type PartitionInfoReply struct {
	Key               string
	Owners            []int // List of node IDs that own this key
	Primary           int   // Primary owner (first in the list)
	Replicas          []int // Replica nodes (rest of the list)
	IsPartitioned     bool  // Whether partitioning is enabled
	ReplicationFactor int   // Current replication factor
	Message           string
}

// GetPartitionInfo returns which nodes are responsible for a given key
func (n *Node) GetPartitionInfo(args *PartitionInfoArgs, reply *PartitionInfoReply) error {
	reply.Key = args.Key
	reply.IsPartitioned = n.PartitionMgr.IsEnabled()
	reply.ReplicationFactor = n.PartitionMgr.GetReplicationFactor()

	owners := n.PartitionMgr.GetPartitionOwners(args.Key)
	reply.Owners = owners

	if len(owners) > 0 {
		reply.Primary = owners[0]
		if len(owners) > 1 {
			reply.Replicas = owners[1:]
		}
		reply.Message = n.PartitionMgr.DescribePartition(args.Key)
	} else {
		reply.Message = "All nodes store this key (no partitioning)"
	}

	fmt.Printf("[Node %d] GetPartitionInfo: key='%s', owners=%v\n", n.ID, args.Key, owners)

	return nil
}
