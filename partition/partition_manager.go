package partition

import (
	"fmt"
	"sync"
)

// PartitionManager handles data partitioning and replica assignment
// It determines which nodes should store which keys based on consistent hashing
type PartitionManager struct {
	hashRing          *ConsistentHash
	replicationFactor int  // Number of replicas (including primary)
	enabled           bool // Enable/disable partitioning
	mutex             sync.RWMutex
}

// NewPartitionManager creates a new partition manager
// enabled: whether to use partitioning (false = full replication to all nodes)
// replicationFactor: number of replicas (e.g., 3 means primary + 2 replicas)
func NewPartitionManager(enabled bool, replicationFactor int) *PartitionManager {
	if replicationFactor < 1 {
		replicationFactor = 3 // DynamoDB default
	}

	return &PartitionManager{
		hashRing:          NewConsistentHash(160),
		replicationFactor: replicationFactor,
		enabled:           enabled,
	}
}

// AddNode adds a node to the partition ring (only when partitioning is enabled)
func (pm *PartitionManager) AddNode(nodeID int) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if pm.enabled {
		pm.hashRing.AddNode(nodeID)
	}
}

// RemoveNode removes a node from the partition ring
func (pm *PartitionManager) RemoveNode(nodeID int) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if pm.enabled {
		pm.hashRing.RemoveNode(nodeID)
	}
}

// GetPartitionOwners returns the list of nodes that should store a key
// Returns:
//   - Empty list if partitioning disabled (implies all nodes)
//   - List of 1 to replicationFactor nodes if partitioning enabled
func (pm *PartitionManager) GetPartitionOwners(key string) []int {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	if !pm.enabled {
		return []int{} // Empty list means replicate to all
	}

	return pm.hashRing.GetNodes(key, pm.replicationFactor)
}

// IsOwner checks if the given node owns a partition for this key
func (pm *PartitionManager) IsOwner(nodeID int, key string) bool {
	owners := pm.GetPartitionOwners(key)

	// If no partitioning, everyone is an owner
	if len(owners) == 0 {
		return true
	}

	for _, owner := range owners {
		if owner == nodeID {
			return true
		}
	}
	return false
}

// IsPrimaryOwner checks if the given node is the primary (first) owner
func (pm *PartitionManager) IsPrimaryOwner(nodeID int, key string) bool {
	owners := pm.GetPartitionOwners(key)

	// If no partitioning, node 1 is primary (arbitrary choice for quorum reads)
	if len(owners) == 0 {
		return nodeID == 1
	}

	return len(owners) > 0 && owners[0] == nodeID
}

// GetAllNodes returns all nodes in the partition ring
func (pm *PartitionManager) GetAllNodes() []int {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	return pm.hashRing.GetAllNodes()
}

// SetEnabled enables or disables partitioning
func (pm *PartitionManager) SetEnabled(enabled bool) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.enabled = enabled
}

// IsEnabled returns whether partitioning is currently enabled
func (pm *PartitionManager) IsEnabled() bool {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	return pm.enabled
}

// SetReplicationFactor changes the replication factor
func (pm *PartitionManager) SetReplicationFactor(factor int) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if factor < 1 {
		factor = 1
	}
	pm.replicationFactor = factor
}

// GetReplicationFactor returns the current replication factor
func (pm *PartitionManager) GetReplicationFactor() int {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	return pm.replicationFactor
}

// GetPartitionStats returns statistics about the current partitioning
func (pm *PartitionManager) GetPartitionStats() map[string]interface{} {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	stats := map[string]interface{}{
		"enabled":            pm.enabled,
		"replication_factor": pm.replicationFactor,
	}

	if pm.enabled {
		stats["ring_info"] = pm.hashRing.GetRingInfo()
	}

	return stats
}

// GetPartitionDistribution returns which keys are on which nodes (for debugging)
// testKeys: sample keys to check distribution
func (pm *PartitionManager) GetPartitionDistribution(testKeys []string) map[int]int {
	distribution := make(map[int]int)

	for _, key := range testKeys {
		owners := pm.GetPartitionOwners(key)
		if len(owners) > 0 {
			for _, owner := range owners {
				distribution[owner]++
			}
		}
	}

	return distribution
}

// DescribePartition returns human-readable description of where a key is stored
func (pm *PartitionManager) DescribePartition(key string) string {
	owners := pm.GetPartitionOwners(key)

	pm.mutex.RLock()
	enabled := pm.enabled
	pm.mutex.RUnlock()

	if !enabled {
		return fmt.Sprintf("Key '%s': Full replication to all nodes (partitioning disabled)", key)
	}

	if len(owners) == 0 {
		return fmt.Sprintf("Key '%s': No nodes available (empty cluster)", key)
	}

	ownerStr := ""
	for i, nodeID := range owners {
		if i == 0 {
			ownerStr += fmt.Sprintf("Node %d (primary)", nodeID)
		} else {
			ownerStr += fmt.Sprintf(", Node %d (replica)", nodeID)
		}
	}

	return fmt.Sprintf("Key '%s': Stored on %s", key, ownerStr)
}
