package partition

import (
	"crypto/md5"
	"fmt"
	"sort"
)

// ConsistentHash implements a consistent hash ring for distributed partitioning
// Uses virtual nodes for better load balancing
type ConsistentHash struct {
	ring          map[uint64]int // hash -> node ID
	sortedHashes  []uint64       // sorted list of hashes for binary search
	virtualNodes  int            // virtual nodes per physical node
	physicalNodes map[int]bool   // set of physical node IDs
}

// NewConsistentHash creates a new consistent hash ring
// virtualNodesPerPhysical: number of virtual copies per physical node (typically 150-200 for good distribution)
func NewConsistentHash(virtualNodesPerPhysical int) *ConsistentHash {
	if virtualNodesPerPhysical <= 0 {
		virtualNodesPerPhysical = 160 // DynamoDB default
	}
	return &ConsistentHash{
		ring:          make(map[uint64]int),
		sortedHashes:  make([]uint64, 0),
		virtualNodes:  virtualNodesPerPhysical,
		physicalNodes: make(map[int]bool),
	}
}

// AddNode adds a new physical node to the ring with virtual replicas
func (ch *ConsistentHash) AddNode(nodeID int) {
	if ch.physicalNodes[nodeID] {
		return // Already exists
	}

	ch.physicalNodes[nodeID] = true

	// Add virtual nodes
	for i := 0; i < ch.virtualNodes; i++ {
		hash := ch.hashKey(fmt.Sprintf("node-%d-%d", nodeID, i))
		ch.ring[hash] = nodeID
		ch.sortedHashes = append(ch.sortedHashes, hash)
	}

	// Keep hashes sorted for efficient lookup
	sort.Slice(ch.sortedHashes, func(i, j int) bool {
		return ch.sortedHashes[i] < ch.sortedHashes[j]
	})
}

// RemoveNode removes a physical node from the ring
func (ch *ConsistentHash) RemoveNode(nodeID int) {
	if !ch.physicalNodes[nodeID] {
		return // Doesn't exist
	}

	delete(ch.physicalNodes, nodeID)

	// Remove all virtual nodes
	for i := 0; i < ch.virtualNodes; i++ {
		hash := ch.hashKey(fmt.Sprintf("node-%d-%d", nodeID, i))
		delete(ch.ring, hash)
	}

	// Rebuild sorted hashes
	ch.sortedHashes = make([]uint64, 0)
	for hash := range ch.ring {
		ch.sortedHashes = append(ch.sortedHashes, hash)
	}
	sort.Slice(ch.sortedHashes, func(i, j int) bool {
		return ch.sortedHashes[i] < ch.sortedHashes[j]
	})
}

// GetNode returns the primary node responsible for the given key
func (ch *ConsistentHash) GetNode(key string) int {
	if len(ch.ring) == 0 {
		return -1
	}

	hash := ch.hashKey(key)

	// Binary search for the first hash >= our key hash
	idx := sort.Search(len(ch.sortedHashes), func(i int) bool {
		return ch.sortedHashes[i] >= hash
	})

	// Wrap around if necessary
	if idx == len(ch.sortedHashes) {
		idx = 0
	}

	return ch.ring[ch.sortedHashes[idx]]
}

// GetNodes returns the primary node and replica nodes (next N nodes on the ring)
// replicaCount: number of replicas (including primary, so total copies = replicaCount)
func (ch *ConsistentHash) GetNodes(key string, replicaCount int) []int {
	if len(ch.ring) == 0 {
		return []int{}
	}

	if replicaCount > len(ch.physicalNodes) {
		replicaCount = len(ch.physicalNodes) // Can't have more replicas than nodes
	}

	hash := ch.hashKey(key)

	// Binary search for starting position
	idx := sort.Search(len(ch.sortedHashes), func(i int) bool {
		return ch.sortedHashes[i] >= hash
	})

	if idx == len(ch.sortedHashes) {
		idx = 0
	}

	// Collect unique physical nodes as we traverse the ring
	result := make([]int, 0, replicaCount)
	seen := make(map[int]bool)

	for i := 0; i < len(ch.sortedHashes) && len(result) < replicaCount; i++ {
		currentIdx := (idx + i) % len(ch.sortedHashes)
		nodeID := ch.ring[ch.sortedHashes[currentIdx]]

		if !seen[nodeID] {
			result = append(result, nodeID)
			seen[nodeID] = true
		}
	}

	return result
}

// hashKey generates a consistent hash for a key using MD5
func (ch *ConsistentHash) hashKey(key string) uint64 {
	hash := md5.Sum([]byte(key))
	// Convert first 8 bytes to uint64
	var result uint64
	for i := 0; i < 8; i++ {
		result = (result << 8) | uint64(hash[i])
	}
	return result
}

// GetRingInfo returns debug information about the ring
func (ch *ConsistentHash) GetRingInfo() map[string]interface{} {
	return map[string]interface{}{
		"total_hashes":     len(ch.sortedHashes),
		"physical_nodes":   len(ch.physicalNodes),
		"virtual_per_node": ch.virtualNodes,
		"node_ids":         ch.GetAllNodes(),
	}
}

// GetAllNodes returns all physical node IDs
func (ch *ConsistentHash) GetAllNodes() []int {
	var nodes []int
	for nodeID := range ch.physicalNodes {
		nodes = append(nodes, nodeID)
	}
	sort.Ints(nodes)
	return nodes
}
