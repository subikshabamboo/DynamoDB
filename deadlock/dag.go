package deadlock

import (
	"fmt"
	"sync"
)

// DAGManager implements Wait-For Graph (DAG) based deadlock detection
// The coordinator/leader maintains a global Wait-For Graph
// Edges represent "Node X is waiting for Node Y to release a resource"
// Cycles in the graph indicate deadlocks
type DAGManager struct {
	mu              sync.Mutex
	heldResources   map[string]int    // resource -> nodeID holding it
	waitingFor      map[int][]int     // nodeID -> []nodeIDs it's waiting for
	waitingResource map[int]string    // nodeID -> resource it's waiting for
	logs            []string          // operation logs for demo output
}

// NewDAGManager creates a new DAG-based deadlock manager
func NewDAGManager() *DAGManager {
	return &DAGManager{
		heldResources:   make(map[string]int),
		waitingFor:      make(map[int][]int),
		waitingResource: make(map[int]string),
		logs:            make([]string, 0),
	}
}

// Lock attempts to lock a resource for a given node
// Returns: granted (bool), waitingForNodeID (int)
func (dm *DAGManager) Lock(nodeID int, resource string) (bool, int) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	holder, held := dm.heldResources[resource]

	if !held {
		// Resource is free - grant it
		dm.heldResources[resource] = nodeID
		msg := fmt.Sprintf("Resource '%s' GRANTED to Node %d (resource was free)", resource, nodeID)
		dm.logs = append(dm.logs, msg)
		fmt.Println("[DAG]", msg)
		return true, 0
	}

	if holder == nodeID {
		// Already holding this resource
		msg := fmt.Sprintf("Resource '%s' already held by Node %d", resource, nodeID)
		dm.logs = append(dm.logs, msg)
		fmt.Println("[DAG]", msg)
		return true, 0
	}

	// Resource is held by another node - add wait edge (nodeID -> holder)
	dm.waitingFor[nodeID] = append(dm.waitingFor[nodeID], holder)
	dm.waitingResource[nodeID] = resource

	msg := fmt.Sprintf("Resource '%s' held by Node %d. Node %d must WAIT. Edge added: %d → %d",
		resource, holder, nodeID, nodeID, holder)
	dm.logs = append(dm.logs, msg)
	fmt.Println("[DAG]", msg)

	return false, holder
}

// Unlock releases a resource held by a node
func (dm *DAGManager) Unlock(nodeID int, resource string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	holder, held := dm.heldResources[resource]
	if !held || holder != nodeID {
		return
	}

	delete(dm.heldResources, resource)

	msg := fmt.Sprintf("Resource '%s' RELEASED by Node %d", resource, nodeID)
	dm.logs = append(dm.logs, msg)
	fmt.Println("[DAG]", msg)

	// Remove wait edges for nodes that were waiting for this resource
	for waitingNode, waitRes := range dm.waitingResource {
		if waitRes == resource {
			delete(dm.waitingResource, waitingNode)
			delete(dm.waitingFor, waitingNode)
			msg := fmt.Sprintf("Cleared wait edges for Node %d (was waiting for '%s')", waitingNode, resource)
			dm.logs = append(dm.logs, msg)
			fmt.Println("[DAG]", msg)
		}
	}
}

// DetectCycle performs DFS on the Wait-For Graph to find cycles (deadlocks)
func (dm *DAGManager) DetectCycle() (bool, []int) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	visited := make(map[int]bool)
	recStack := make(map[int]bool)

	for node := range dm.waitingFor {
		if !visited[node] {
			if cycle := dm.dfs(node, visited, recStack, []int{}); len(cycle) > 0 {
				msg := fmt.Sprintf("DEADLOCK DETECTED! Cycle: %v", cycle)
				dm.logs = append(dm.logs, msg)
				fmt.Println("[DAG]", msg)
				return true, cycle
			}
		}
	}

	msg := "No deadlock detected (no cycles in Wait-For Graph)"
	dm.logs = append(dm.logs, msg)
	fmt.Println("[DAG]", msg)
	return false, nil
}

// dfs performs depth-first search to detect cycles
func (dm *DAGManager) dfs(node int, visited, recStack map[int]bool, path []int) []int {
	visited[node] = true
	recStack[node] = true
	path = append(path, node)

	for _, neighbor := range dm.waitingFor[node] {
		if !visited[neighbor] {
			if cycle := dm.dfs(neighbor, visited, recStack, path); len(cycle) > 0 {
				return cycle
			}
		} else if recStack[neighbor] {
			// Found a cycle - extract the cycle path
			cycle := append(path, neighbor)
			return cycle
		}
	}

	recStack[node] = false
	return nil
}

// Resolve aborts a node's transactions to break a deadlock
func (dm *DAGManager) Resolve(abortNodeID int) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	msg := fmt.Sprintf("RESOLVING deadlock: Aborting Node %d's transactions", abortNodeID)
	dm.logs = append(dm.logs, msg)
	fmt.Println("[DAG]", msg)

	// Release all resources held by the aborted node
	for resource, holder := range dm.heldResources {
		if holder == abortNodeID {
			delete(dm.heldResources, resource)
			msg := fmt.Sprintf("Released resource '%s' (was held by aborted Node %d)", resource, abortNodeID)
			dm.logs = append(dm.logs, msg)
			fmt.Println("[DAG]", msg)
		}
	}

	// Remove all wait edges FROM the aborted node
	delete(dm.waitingFor, abortNodeID)
	delete(dm.waitingResource, abortNodeID)

	// Remove all wait edges TO the aborted node from other nodes
	for node, waitList := range dm.waitingFor {
		newWaitList := []int{}
		for _, w := range waitList {
			if w != abortNodeID {
				newWaitList = append(newWaitList, w)
			}
		}
		if len(newWaitList) == 0 {
			delete(dm.waitingFor, node)
		} else {
			dm.waitingFor[node] = newWaitList
		}
	}

	msg = fmt.Sprintf("Node %d's transactions aborted, deadlock resolved", abortNodeID)
	dm.logs = append(dm.logs, msg)
	fmt.Println("[DAG]", msg)
}

// GetWaitForGraph returns a copy of the current Wait-For Graph
func (dm *DAGManager) GetWaitForGraph() map[int][]int {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	graph := make(map[int][]int)
	for k, v := range dm.waitingFor {
		edges := make([]int, len(v))
		copy(edges, v)
		graph[k] = edges
	}
	return graph
}

// GetHeldResources returns a copy of currently held resources
func (dm *DAGManager) GetHeldResources() map[string]int {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	resources := make(map[string]int)
	for k, v := range dm.heldResources {
		resources[k] = v
	}
	return resources
}

// GetLogs returns the operation logs
func (dm *DAGManager) GetLogs() []string {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	logs := make([]string, len(dm.logs))
	copy(logs, dm.logs)
	return logs
}

// ClearLogs clears the operation logs
func (dm *DAGManager) ClearLogs() {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.logs = make([]string, 0)
}

// Reset clears all state
func (dm *DAGManager) Reset() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.heldResources = make(map[string]int)
	dm.waitingFor = make(map[int][]int)
	dm.waitingResource = make(map[int]string)
	dm.logs = make([]string, 0)

	fmt.Println("[DAG] All state cleared")
}
