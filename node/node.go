package node

import (
	"dynamo-go/partition"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// NodeConfig represents a single node's configuration
type NodeConfig struct {
	ID   int    `json:"id"`
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

// PartitionConfig represents partitioning configuration
type PartitionConfig struct {
	Enabled           bool `json:"enabled"`
	ReplicationFactor int  `json:"replication_factor"`
}

// Config represents the complete cluster configuration
type Config struct {
	Nodes             []NodeConfig    `json:"nodes"`
	HeartbeatInterval int             `json:"heartbeat_interval_ms"`
	ElectionTimeout   int             `json:"election_timeout_ms"`
	DeadlockTimeout   int             `json:"deadlock_timeout_ms"`
	Partition         PartitionConfig `json:"partition"`
}

// StoredItem represents data stored with timestamp
type StoredItem struct {
	Value     []byte
	Timestamp int64
}

// MutexManager interface for mutual exclusion (Ricart-Agrawala)
type MutexManager interface {
	ExecuteInCriticalSection(fn func()) bool
}

// DeadlockDetector interface for DAG-based deadlock detection
type DeadlockDetector interface {
	Lock(nodeID int, resource string) (granted bool, waitFor int)
	Unlock(nodeID int, resource string)
	DetectCycle() (hasCycle bool, cycle []int)
	Resolve(abortNodeID int)
	GetWaitForGraph() map[int][]int
	GetHeldResources() map[string]int
	GetLogs() []string
	ClearLogs()
	Reset()
}

// Node represents a single node in the distributed system
type Node struct {
	ID        int
	IP        string
	Port      int
	Config    *Config
	DataStore map[string]StoredItem
	DataMutex sync.RWMutex

	// Lamport Clock for logical timestamps
	LamportClock int64
	ClockMutex   sync.Mutex

	// Leader Election
	LeaderID    int
	IsLeader    bool
	LeaderMutex sync.RWMutex

	// Mutual Exclusion (Ricart-Agrawala)
	RequestingCS  bool
	RequestTime   int64
	ReplyCount    int
	DeferredQueue []DeferredRequest
	MutexLock     sync.Mutex
	CSChan        chan bool

	// Managers (set from main.go to avoid circular imports)
	MutexMgr     MutexManager
	DeadlockMgr  DeadlockDetector
	PartitionMgr *partition.PartitionManager
}

// DeferredRequest represents a deferred reply in Ricart-Agrawala
type DeferredRequest struct {
	NodeID    int
	Timestamp int64
}

// LoadConfig loads configuration from config.json
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	var config Config
	err = json.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %v", err)
	}

	return &config, nil
}

// NewNode creates a new node instance
func NewNode(id int, config *Config) *Node {
	var nodeConfig NodeConfig
	for _, nc := range config.Nodes {
		if nc.ID == id {
			nodeConfig = nc
			break
		}
	}

	// Create partition manager based on config
	partitionConfig := config.Partition
	if partitionConfig.ReplicationFactor == 0 {
		partitionConfig.ReplicationFactor = 3 // Default to 3
	}
	partitionMgr := partition.NewPartitionManager(
		partitionConfig.Enabled,
		partitionConfig.ReplicationFactor,
	)

	// Initialize all nodes in the partition ring
	for _, nc := range config.Nodes {
		partitionMgr.AddNode(nc.ID)
	}

	return &Node{
		ID:            id,
		IP:            nodeConfig.IP,
		Port:          nodeConfig.Port,
		Config:        config,
		DataStore:     make(map[string]StoredItem),
		LamportClock:  0,
		LeaderID:      -1,
		IsLeader:      false,
		RequestingCS:  false,
		DeferredQueue: make([]DeferredRequest, 0),
		CSChan:        make(chan bool, 1),
		PartitionMgr:  partitionMgr,
	}
}

// IncrementClock increments and returns the Lamport clock
func (n *Node) IncrementClock() int64 {
	n.ClockMutex.Lock()
	defer n.ClockMutex.Unlock()
	n.LamportClock++
	return n.LamportClock
}

// UpdateClock updates Lamport clock based on received timestamp
func (n *Node) UpdateClock(receivedTime int64) int64 {
	n.ClockMutex.Lock()
	defer n.ClockMutex.Unlock()
	if receivedTime > n.LamportClock {
		n.LamportClock = receivedTime
	}
	n.LamportClock++
	return n.LamportClock
}

// GetClock returns current Lamport clock value
func (n *Node) GetClock() int64 {
	n.ClockMutex.Lock()
	defer n.ClockMutex.Unlock()
	return n.LamportClock
}

// SetLeader sets the current leader
func (n *Node) SetLeader(leaderID int) {
	n.LeaderMutex.Lock()
	defer n.LeaderMutex.Unlock()
	n.LeaderID = leaderID
	n.IsLeader = (leaderID == n.ID)
	if n.IsLeader {
		fmt.Printf("[Node %d] I am now the LEADER\n", n.ID)
	} else {
		fmt.Printf("[Node %d] Leader is now Node %d\n", n.ID, leaderID)
	}
}

// GetLeader returns current leader ID
func (n *Node) GetLeader() int {
	n.LeaderMutex.RLock()
	defer n.LeaderMutex.RUnlock()
	return n.LeaderID
}

// AmILeader checks if this node is the leader
func (n *Node) AmILeader() bool {
	n.LeaderMutex.RLock()
	defer n.LeaderMutex.RUnlock()
	return n.IsLeader
}

// GetAddress returns the full address of this node
func (n *Node) GetAddress() string {
	return fmt.Sprintf("%s:%d", n.IP, n.Port)
}

// GetNodeAddress returns address for a specific node ID
func (n *Node) GetNodeAddress(nodeID int) string {
	for _, nc := range n.Config.Nodes {
		if nc.ID == nodeID {
			return fmt.Sprintf("%s:%d", nc.IP, nc.Port)
		}
	}
	return ""
}

// GetOtherNodes returns list of other node IDs
func (n *Node) GetOtherNodes() []int {
	var others []int
	for _, nc := range n.Config.Nodes {
		if nc.ID != n.ID {
			others = append(others, nc.ID)
		}
	}
	return others
}

// GetHigherNodes returns list of nodes with higher IDs
func (n *Node) GetHigherNodes() []int {
	var higher []int
	for _, nc := range n.Config.Nodes {
		if nc.ID > n.ID {
			higher = append(higher, nc.ID)
		}
	}
	return higher
}

// GetAllNodeIDs returns all node IDs in the cluster
func (n *Node) GetAllNodeIDs() []int {
	var ids []int
	for _, nc := range n.Config.Nodes {
		ids = append(ids, nc.ID)
	}
	return ids
}

// PrintStatus prints current node status
func (n *Node) PrintStatus() {
	n.LeaderMutex.RLock()
	leaderID := n.LeaderID
	isLeader := n.IsLeader
	n.LeaderMutex.RUnlock()

	n.DataMutex.RLock()
	dataCount := len(n.DataStore)
	n.DataMutex.RUnlock()

	fmt.Println("====================================")
	fmt.Printf("         NODE %d STATUS\n", n.ID)
	fmt.Println("====================================")
	fmt.Printf("Address: %s\n", n.GetAddress())
	if isLeader {
		fmt.Printf("Role: LEADER\n")
	} else {
		fmt.Printf("Role: FOLLOWER\n")
		fmt.Printf("Leader: Node %d\n", leaderID)
	}
	fmt.Printf("Lamport Clock: %d\n", n.GetClock())
	fmt.Printf("Stored Items: %d\n", dataCount)
	fmt.Println("====================================")
}
