# 🔬 Detailed Explanation: Consistent Hashing & Partitioning

## Complete Technical Guide with Code Walkthrough

---

## **PART 1: CONSISTENT HASHING FUNDAMENTALS**

### **The Problem We're Solving**

Imagine you have a simple list of servers:
```
Servers: [Node 1, Node 2, Node 3]
```

**Old way (Simple modulo):**
```go
// Simple hash: just use modulo
server_id = hash(key) % num_servers

// Problem: If one server dies and you have 2 servers left:
server_id = hash(key) % 2  // COMPLETELY DIFFERENT!
// All keys rehash to wrong servers!
```

**Example:**
```
key="user:100"
Old: hash("user:100") % 3 = 42 % 3 = 0 → Server 1
New: hash("user:100") % 2 = 42 % 2 = 0 → Server 1 (lucky!)

key="user:200" 
Old: hash("user:200") % 3 = 95 % 3 = 2 → Server 3
New: hash("user:200") % 2 = 95 % 2 = 1 → Server 2 (WRONG!)
```

**This causes: MASSIVE DATA MOVEMENT** 😱

---

## **SOLUTION: Consistent Hashing with Hash Ring**

### **Core Concept: The Ring**

Imagine arranging servers in a **circular ring** instead of a line:

```
                    0°
                    │
              Node 1 │
             /       \
        320°          40°
       /               \
    Node 3           Node 2
       \              /
        240°        120°
             \     /
              Ring
```

**Key insight:**
- Hash all servers → positions on the ring (0 to 360°)
- Hash all keys → positions on the ring (0 to 360°)
- Key owns by the first server **clockwise** from the key

### **Visual Example:**

```
                  0°
                  │
          Node 1 (45°)
         /        
      320°        45°      Key A (20°)
     /              \       ↓
  Node 3         Key B (80°)→ Node 2 (120°)
 (280°)          
    \            Key C (200°)
     \     150°       /
      Node 2        /
          \        /
           Ring

Key A (20°) → next server clockwise → Node 1 ✓
Key B (80°) → next server clockwise → Node 2 ✓
Key C (200°) → next server clockwise → Node 3 ✓
```

---

## **PROBLEM WITH SIMPLE RING: Uneven Distribution**

If we have only 3 servers, we might get:

```
Node 1: 10°
Node 2: 200°
Node 3: 350°

Ring section sizes:
Node 1 owns: 10° to 200° = 190° (52% of keys!)
Node 2 owns: 200° to 350° = 150° (42% of keys)
Node 3 owns: 350° to 10° = 20° (6% of keys) ← STARVED!
```

---

## **SOLUTION: Virtual Nodes**

**Idea:** Instead of 1 position per server, create 160 virtual copies per server!

```
Node 1 virtual positions: 1, 5, 12, 18, 23, 31, 45, 67, 89, ...
Node 2 virtual positions: 2, 8, 14, 25, 39, 52, 61, 78, 95, ...
Node 3 virtual positions: 3, 11, 19, 28, 41, 59, 71, 88, 103, ...

Result: Ring now has 3 × 160 = 480 positions
Distribution becomes nearly perfect!
```

**Visualization:**
```
              0°
              │
    N1 N1 N1  │   N3
  N2  (45°)   │   N2 N3
 /      N2    \  /    \
N1      N3    N1 N2   N3
 \      N1    / \    /
  N3     N2  /   N3 /
    \(200°) /     N1
     \    /     /
      \  /   N2(120°)
       \/    
     Ring with Virtual Nodes
     (Distribution is PERFECT!)
```

---

## **THE ALGORITHM STEP BY STEP**

### **Step 1: Hash Function (MD5)**

```
Input: "user:123"
       ↓
MD5("user:123") = 6f8df64b944672c4... (hexadecimal)
       ↓
Convert to number = 0x6f8df64b...
       ↓
Output: A 64-bit unsigned integer
```

**Why MD5?**
- ✅ Deterministic (same input = same output always)
- ✅ Uniform distribution (keys spread evenly)
- ✅ Fast computation
- ⚠️ Not cryptographically secure (but we don't care, we're not securing)

---

### **Step 2: Position on Ring**

```go
hash_value = 6f8df64b...  // A huge number
ring_size = 2^64           // Max possible value
position = hash_value % ring_size
```

This gives a position from 0 to 2^64 on our imaginary ring.

---

### **Step 3: Find Owner (Server Lookup)**

```
Servers on ring:
  Node 1 virtual-1: position 100
  Node 1 virtual-2: position 250
  Node 2 virtual-1: position 500
  Node 2 virtual-2: position 800
  Node 3 virtual-1: position 350
  Node 3 virtual-2: position 650

Sorted: [100, 250, 350, 500, 650, 800, ...]

Key hashes to: position 400
  ↓
Find first position >= 400 in sorted list
  ↓
Position 500 (Node 2's virtual-1)
  ↓
Key owner: Node 2 ✓
```

---

## **REPLICATION (Multiple Copies)**

### **Why We Need Replication**

```
Scenario: Node 2 dies
  Before: Keys on Node 2 are LOST! 💀
  After: Replica on Node 3 still has data ✓
```

### **How Replication Works**

**Configuration:**
```json
"replication_factor": 3
```

This means: **"Keep 3 copies of every key"**

**Algorithm:**
```
Key: "user:123"
  ↓
Find primary owner: Node 2 (position 500)
  ↓
Continue walking ring for next unique physical node
  ↓
Next position >= 500: 650 (Node 3)
  ↓
Next position >= 650: 800 (Node 2 again - SKIP, already have it)
  ↓
Next position >= 800: 100 (Node 1)
  ↓
Result: [Node 2, Node 3, Node 1]
  (3 unique nodes = replication_factor satisfied)
```

---

## **PART 2: CODE WALKTHROUGH**

Now let's look at what was implemented:

### **File 1: `partition/consistent_hash.go`**

#### **1. The ConsistentHash Struct**

```go
type ConsistentHash struct {
	ring           map[uint64]int // hash → node ID
	sortedHashes   []uint64       // sorted for binary search
	virtualNodes   int            // 160 per physical node
	physicalNodes  map[int]bool   // set of physical node IDs
}
```

**What each field does:**

| Field | Purpose | Example |
|-------|---------|---------|
| `ring` | Maps a hash value to its owning physical node | `{100: 1, 250: 1, 500: 2}` = "at position 100, it's Node 1's virtual copy" |
| `sortedHashes` | Sorted positions for O(log n) binary search | `[100, 250, 350, 500, ...]` |
| `virtualNodes` | How many copies each physical node has | 160 (more = better distribution) |
| `physicalNodes` | Which actual servers exist | `{1: true, 2: true, 3: true}` |

---

#### **2. Adding a Node (Constructor)**

```go
func (ch *ConsistentHash) AddNode(nodeID int) {
	if ch.physicalNodes[nodeID] {
		return // Already exists
	}

	ch.physicalNodes[nodeID] = true

	// Add virtual nodes
	for i := 0; i < ch.virtualNodes; i++ {
		hash := ch.hashKey(fmt.Sprintf("node-%d-%d", nodeID, i))
		//                                        ↑ physical   ↑ virtual copy #
		ch.ring[hash] = nodeID
		ch.sortedHashes = append(ch.sortedHashes, hash)
	}

	// Keep hashes sorted for binary search
	sort.Slice(ch.sortedHashes, func(i, j int) bool {
		return ch.sortedHashes[i] < ch.sortedHashes[j]
	})
}
```

**Example: Adding Node 1 with 3 virtual copies (simplified)**

```
Loop i=0: hash("node-1-0") = 100
           ch.ring[100] = 1
           sortedHashes = [100]

Loop i=1: hash("node-1-1") = 250
           ch.ring[250] = 1
           sortedHashes = [100, 250]

Loop i=2: hash("node-1-2") = 350
           ch.ring[350] = 1
           sortedHashes = [100, 250, 350]

Result: Node 1 has 3 positions on ring [100, 250, 350]
```

**After adding Node 2 and 3:**
```
Ring positions: [100, 120, 250, 300, 350, 420, 500, ...]
Owners:        [ 1 ,  2 ,  1 ,  3 ,  1 ,  2 ,  2 , ...]
```

---

#### **3. Hash Function**

```go
func (ch *ConsistentHash) hashKey(key string) uint64 {
	hash := md5.Sum([]byte(key))
	// hash is [16]byte from MD5

	// Convert first 8 bytes to uint64
	var result uint64
	for i := 0; i < 8; i++ {
		result = (result << 8) | uint64(hash[i])
	}
	return result
}
```

**Example:**
```
MD5("user:123") = [0x6f, 0x8d, 0xf6, 0x4b, 0x94, 0x46, 0x72, 0xc4, ...]

Converting to uint64:
  Start: result = 0
  i=0: result = (0 << 8) | 0x6f = 0x6f
  i=1: result = (0x6f << 8) | 0x8d = 0x6f8d
  i=2: result = (0x6f8d << 8) | 0xf6 = 0x6f8df6
  ... (8 times total)
  
  Final: result = 0x6f8df64b9446...
```

---

#### **4. Finding the Primary Owner**

```go
func (ch *ConsistentHash) GetNode(key string) int {
	if len(ch.ring) == 0 {
		return -1
	}

	hash := ch.hashKey(key)
	// hash = 0x6f8df64b... (some large number)

	// Binary search: find first position >= our hash
	idx := sort.Search(len(ch.sortedHashes), func(i int) bool {
		return ch.sortedHashes[i] >= hash
		//                       ↑ compare with ring positions
	})

	// Wrap around if necessary
	if idx == len(ch.sortedHashes) {
		idx = 0
	}

	// Return the physical node at this position
	return ch.ring[ch.sortedHashes[idx]]
}
```

**Example:**

```
Ring positions (sorted): [100, 250, 350, 500, 650, 800]
Owners:                  [ 1 ,  1 ,  3 ,  2 ,  3 ,  2 ]

Key "user:123" hashes to: 400

Binary search finds: first position >= 400 is 500
ch.ring[500] = 2

✓ Owner = Node 2
```

---

#### **5. Finding All Replicas (The Key Function!)**

```go
func (ch *ConsistentHash) GetNodes(key string, replicaCount int) []int {
	if len(ch.ring) == 0 {
		return []int{}
	}

	if replicaCount > len(ch.physicalNodes) {
		replicaCount = len(ch.physicalNodes)
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
	//      ↑ track which physical nodes we've already seen

	for i := 0; i < len(ch.sortedHashes) && len(result) < replicaCount; i++ {
		currentIdx := (idx + i) % len(ch.sortedHashes)
		//              ↑ wrap around the ring
		
		nodeID := ch.ring[ch.sortedHashes[currentIdx]]

		if !seen[nodeID] {
			result = append(result, nodeID)
			seen[nodeID] = true
			// ✓ add this physical node only once
		}
	}

	return result
}
```

**Detailed Example:**

```
Ring: positions [100, 120, 250, 300, 350, 420, 500, ...]
      owners    [ 1 ,  2 ,  1 ,  3 ,  1 ,  2 ,  2 , ...]

Key "user:456" hashes to: 200

Binary search finds: first position >= 200 is 250 (index 2)

LOOP through ring starting from index 2:
  i=0: currentIdx = (2+0) % 7 = 2 → position 250 → owner 1
       seen[1] = false? YES → add 1 to result
       result = [1], seen = {1: true}

  i=1: currentIdx = (2+1) % 7 = 3 → position 300 → owner 3
       seen[3] = false? YES → add 3 to result
       result = [1, 3], seen = {1: true, 3: true}

  i=2: currentIdx = (2+2) % 7 = 4 → position 350 → owner 1
       seen[1] = false? NO → skip (already have Node 1)

  i=3: currentIdx = (2+3) % 7 = 5 → position 420 → owner 2
       seen[2] = false? YES → add 2 to result
       result = [1, 3, 2], len(result) = 3

STOP (have 3 replicas)

✓ Final result: [1, 3, 2]
  Key "user:456" should be stored on:
    - Node 1 (primary)
    - Node 3 (replica)
    - Node 2 (replica)
```

---

### **File 2: `partition/partition_manager.go`**

This is the **management layer** above the hash ring:

```go
type PartitionManager struct {
	hashRing          *ConsistentHash
	replicationFactor int
	enabled           bool
	mutex             sync.RWMutex
}
```

#### **Key Methods:**

**1. Get Owners (What I Added to Your Put/Get/Delete)**

```go
func (pm *PartitionManager) GetPartitionOwners(key string) []int {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	if !pm.enabled {
		return []int{} // signals: replicate to all nodes
	}

	return pm.hashRing.GetNodes(key, pm.replicationFactor)
	//                    ↑ key  ↑ how many copies you want
}
```

**When you call `put("key1")`:**

```
pm.GetPartitionOwners("key1")
  ↓
hashRing.GetNodes("key1", 3)
  ↓
Returns: [3, 1, 2]  ← which nodes store key1
```

**2. Check Ownership**

```go
func (pm *PartitionManager) IsOwner(nodeID int, key string) bool {
	owners := pm.GetPartitionOwners(key)
	//       ↑ [3, 1, 2] for key1

	if len(owners) == 0 {
		return true // No partitioning: all are owners
	}

	for _, owner := range owners {
		if owner == nodeID {
			return true ✓
		}
	}
	return false
}
```

Example: `IsOwner(1, "key1")`
```
owners = [3, 1, 2]
Is 1 in [3, 1, 2]? YES ✓
Return: true
```

---

## **PART 3: INTEGRATION INTO YOUR CODE**

### **File 3: `node/node.go` (Modified)**

I added:

```go
type PartitionConfig struct {
	Enabled           bool `json:"enabled"`
	ReplicationFactor int  `json:"replication_factor"`
}

type Config struct {
	// ... existing fields ...
	Partition PartitionConfig `json:"partition"`
}

type Node struct {
	// ... existing fields ...
	PartitionMgr *partition.PartitionManager
	//            ↑ new!
}
```

**In NewNode():**
```go
func NewNode(id int, config *Config) *Node {
	// ... setup ...

	// Create partition manager based on config
	partitionConfig := config.Partition
	if partitionConfig.ReplicationFactor == 0 {
		partitionConfig.ReplicationFactor = 3
	}
	partitionMgr := partition.NewPartitionManager(
		partitionConfig.Enabled,
		partitionConfig.ReplicationFactor,
	)

	// Initialize all nodes in the partition ring
	for _, nc := range config.Nodes {
		partitionMgr.AddNode(nc.ID)
		//             ↑ register each node
	}

	// ... rest of setup ...
	PartitionMgr: partitionMgr,
	//             ↑ attach to node
}
```

---

### **File 4: `node/rpc_handlers.go` (Modified)**

#### **Original Put (Full Replication):**

```go
func (n *Node) Put(args *PutArgs, reply *PutReply) error {
	// Store locally
	n.DataStore[args.Key] = StoredItem{Value: args.Value}
	
	if !args.IsReplica {
		// Replicate to ALL other nodes
		n.replicateToOthers(args.Key, args.Value)
	}
}
```

#### **New Put (Partition-Aware):**

```go
func (n *Node) Put(args *PutArgs, reply *PutReply) error {
	// Store locally
	n.DataStore[args.Key] = StoredItem{Value: args.Value}
	
	if !args.IsReplica {
		// Use PARTITION-AWARE replication
		n.replicateToPartitionNodes(args.Key, args.Value)
		//                            ↑ new function!
	}
}

func (n *Node) replicateToPartitionNodes(key string, value []byte) []string {
	results := make([]string, 0)

	var targetNodes []int

	if n.PartitionMgr.IsEnabled() {
		// Get nodes that should store this key
		partitionOwners := n.PartitionMgr.GetPartitionOwners(key)
		//                                  ↑ [3, 1, 2] for key1

		// Filter out current node (we already stored it locally)
		for _, nodeID := range partitionOwners {
			if nodeID != n.ID {
				targetNodes = append(targetNodes, nodeID)
			}
		}
		
		fmt.Printf("[Node %d] PARTITION-AWARE: key='%s' to nodes %v\n",
			n.ID, key, partitionOwners)
	} else {
		// Old way: replicate to all
		targetNodes = n.GetOtherNodes()
		fmt.Printf("[Node %d] FULL REPLICATION: key='%s'\n", n.ID, key)
	}

	// Send to each target node
	for _, nodeID := range targetNodes {
		address := n.GetNodeAddress(nodeID)
		client, _ := rpc.Dial("tcp", address)
		defer client.Close()

		putArgs := &PutArgs{Key: key, Value: value, IsReplica: true}
		var putReply PutReply
		client.Call("Node.Put", putArgs, &putReply)
	}

	return results
}
```

**Example Flow:**

```
User: put 1 key1 file.txt

Node 1 receives Put RPC:
  ↓
Store locally: DataStore["key1"] = file content ✓
  ↓
Check: args.IsReplica? NO (first put)
  ↓
Call replicateToPartitionNodes("key1", content)
  ↓
IsEnabled()? YES (partitioning on)
  ↓
GetPartitionOwners("key1") = [3, 1, 2]
  ↓
Filter out Node 1 (current): [3, 2]
  ↓
Replicate to Node 3: RPC("Node.Put", key1, ..., IsReplica=true)
Replicate to Node 2: RPC("Node.Put", key1, ..., IsReplica=true)
  ↓
Node 2 receives: "Put with IsReplica=true"
  ↓
Store locally: DataStore["key1"] = file content ✓
DON'T replicate further (IsReplica=true prevents infinite loop)
  ↓
Node 3 same...
  ↓
Result: DataStore["key1"] on Nodes [3, 1, 2] ✓
```

---

#### **Delete Propagation**

```go
func (n *Node) Delete(args *DeleteArgs, reply *DeleteReply) error {
	// Delete locally
	delete(n.DataStore, args.Key)

	if !args.IsReplica {
		// Propagate to replicas
		if n.PartitionMgr.IsEnabled() {
			partitionOwners := n.PartitionMgr.GetPartitionOwners(args.Key)
			for _, nodeID := range partitionOwners {
				if nodeID != n.ID {
					go n.propagateDelete(nodeID, args.Key)
					//  ↑ send async
				}
			}
		} else {
			// Full replication: propagate to all
			for _, nodeID := range n.GetOtherNodes() {
				go n.propagateDelete(nodeID, args.Key)
			}
		}
	}
}

func (n *Node) propagateDelete(nodeID int, key string) {
	address := n.GetNodeAddress(nodeID)
	client, _ := rpc.Dial("tcp", address)
	defer client.Close()

	deleteArgs := &DeleteArgs{Key: key, IsReplica: true}
	var deleteReply DeleteReply
	client.Call("Node.Delete", deleteArgs, &deleteReply)
}
```

---

#### **Query Partition Info**

```go
func (n *Node) GetPartitionInfo(args *PartitionInfoArgs, reply *PartitionInfoReply) error {
	reply.Key = args.Key
	reply.IsPartitioned = n.PartitionMgr.IsEnabled()
	reply.ReplicationFactor = n.PartitionMgr.GetReplicationFactor()

	owners := n.PartitionMgr.GetPartitionOwners(args.Key)
	//        ↑ calls: hashRing.GetNodes(key, RF)
	reply.Owners = owners

	if len(owners) > 0 {
		reply.Primary = owners[0]
		reply.Replicas = owners[1:]
		reply.Message = n.PartitionMgr.DescribePartition(args.Key)
		//              ↑ "Stored on Node X (primary), Y, Z (replicas)"
	}

	return nil
}
```

---

## **PART 4: CONFIG.JSON**

```json
{
    "nodes": [
        {"id": 1, "ip": "127.0.0.1", "port": 8001},
        {"id": 2, "ip": "127.0.0.1", "port": 8002},
        {"id": 3, "ip": "127.0.0.1", "port": 8003}
    ],
    "heartbeat_interval_ms": 3000,
    "election_timeout_ms": 5000,
    "deadlock_timeout_ms": 5000,
    "partition": {
        "enabled": false,        // ← Toggle partitioning on/off
        "replication_factor": 3  // ← How many copies
    }
}
```

When enabled=true:
- Each Put calculates owners via consistent hashing
- Each key stored on exactly `replication_factor` nodes
- Each Delete propagates to all replicas

When enabled=false:
- Ignores partitioning logic
- Replicates to ALL nodes (backward compatible)

---

## **COMPLETE EXAMPLE WALKTHROUGH**

Let's trace through `put 1 key1 test.txt` with partitioning ENABLED:

```
┌─────────────────────────────────────────────────────────────────┐
│ CLIENT: put 1 key1 testfiles/test.txt                          │
└─────────────────────────────────────────────────────────────────┘
                           ↓
                        Read file
                           ↓
         RPC Call: Node 1 → Put(key="key1", value=content)

┌─────────────────────────────────────────────────────────────────┐
│ NODE 1 Receives Put RPC                                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│ 1. Store locally:                                               │
│    n.DataStore["key1"] = StoredItem{Value: content, ...}       │
│    ✓ key1 now on Node 1                                         │
│                                                                 │
│ 2. Check IsReplica? NO (this is the primary request)            │
│    → Call replicateToPartitionNodes("key1", content)           │
│                                                                 │
│ 3. In replicateToPartitionNodes:                                │
│    - IsEnabled()? YES (partitioning is on)                      │
│    - GetPartitionOwners("key1"):                                │
│        • hash("key1") = 0x4a5e...                              │
│        • Binary search: find first ring position >= 0x4a5e...  │
│        • Positions sorted: [0x1000, 0x2000, 0x3000, 0x4500]   │
│        • First >= 0x4a5e: 0x4500 (Node 3's virtual copy)       │
│        • Continue walking: 0x1000 (Node 1), 0x2000 (Node 2)    │
│        • Result: [3, 1, 2] (3 unique physical nodes)           │
│                                                                 │
│ 4. Filter out current node:                                    │
│    owners = [3, 1, 2]                                           │
│    Filter n.ID=1: targetNodes = [3, 2]                         │
│                                                                 │
│ 5. Replicate to Node 3:                                        │
│    RPC to Node 3: Put(key="key1", IsReplica=true)              │
│                                                                 │
│ 6. Replicate to Node 2:                                        │
│    RPC to Node 2: Put(key="key1", IsReplica=true)              │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────────┐
│ NODE 3 Receives Put RPC (IsReplica=true)                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│ 1. Store locally:                                               │
│    n.DataStore["key1"] = StoredItem{Value: content, ...}       │
│    ✓ key1 now on Node 3                                         │
│                                                                 │
│ 2. Check IsReplica? YES                                         │
│    → Skip replication (don't propagate further)                 │
│    ✓ Done!                                                      │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────────┐
│ NODE 2 Receives Put RPC (IsReplica=true)                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│ 1. Store locally:                                               │
│    n.DataStore["key1"] = StoredItem{Value: content, ...}       │
│    ✓ key1 now on Node 2                                         │
│                                                                 │
│ 2. Check IsReplica? YES                                         │
│    → Skip replication                                           │
│    ✓ Done!                                                      │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
                           ↓
              ✓ RESULT: key1 stored on [3, 1, 2]
              ✓ Partition working perfectly!
```

---

## **SUMMARY TABLE: What Was Built**

| Component | File | What It Does |
|-----------|------|-------------|
| **Hash Function** | `consistent_hash.go` | Converts any key to a position (0-2^64) using MD5 |
| **Ring Structure** | `consistent_hash.go` | Stores servers at positions with 160 virtual copies each |
| **Primary Owner** | `GetNode()` | Binary search to find which server owns a key |
| **Replica Assignment** | `GetNodes()` | Walk ring to find RF unique servers that store the key |
| **Manager Layer** | `partition_manager.go` | Wrapper that enables/disables partitioning + toggles RF |
| **Node Integration** | `node.go` | Attaches PartitionManager to each Node |
| **Put Logic** | `rpc_handlers.go` | Calls `GetPartitionOwners()` instead of replicating to all |
| **Delete Logic** | `rpc_handlers.go` | Propagates only to partition owners |
| **Query API** | `rpc_handlers.go` | `GetPartitionInfo()` RPC lets client see ownership |
| **Config** | `config.json` | `partition.enabled` and `replication_factor` settings |

---

## **Key Insights**

1. **Consistent Hashing != Modulo Hash**
   - ✅ Adding/removing servers minimizes data movement
   - ❌ Simple modulo changes everything when server count changes

2. **Virtual Nodes = Fair Distribution**
   - 160 copies per server → nearly perfect load balance
   - Prevents "hot spotting" (one server getting all the keys)

3. **Toggle Without Code Changes**
   - `enabled: false` → Full replication (backward compatible)
   - `enabled: true` → Partitioned (optimal for large clusters)

4. **Replication Factor is Flexible**
   - RF=1: No redundancy (risky)
   - RF=2: One backup (balanced)
   - RF=3: Two backups (AWS standard)
   - Can change in config.json without code edits

---

## **Performance Characteristics**

| Operation | Without Partitioning | With Partitioning |
|-----------|--------------------|--------------------|
| Put | O(1) + replication to n nodes | O(1) + replication to rf nodes |
| Get | O(1) local read | O(1) local read |
| Delete | O(1) + deletion from n nodes | O(1) + deletion from rf nodes |
| Network Bandwidth | (n-1) × data_size | (rf-1) × data_size |

---

## **References**

- **Consistent Hashing**: https://en.wikipedia.org/wiki/Consistent_hashing
- **Hash Ring with Virtual Nodes**: Karger et al., "Consistent Hashing and Random Trees"
- **DynamoDB Architecture**: AWS official documentation

---

**END OF DETAILED EXPLANATION**

This document contains complete technical details of the consistent hashing and partitioning implementation.
