# DynamoDB Distributed System Implementation

This project implements a **simplified DynamoDB-style distributed database from scratch** using GoRPC. It demonstrates core distributed systems concepts through a working 3-node cluster that handles storage, replication, leader election, mutual exclusion, and deadlock detection — all communicating over the network.

### What This Project Does

- 3 nodes (servers) run on 3 different machines (or 3 terminals for testing)
- Each node stores data in-memory (key-value pairs)
- Data is **automatically replicated** to all nodes when stored (default) or partitioned via consistent hashing (optional)
- One node is elected as **leader** using Bully Algorithm
- **Mutual Exclusion** ensures only one node writes at a time (Ricart-Agrawala)
- **Deadlock Detection** uses a Wait-For Graph (DAG) with DFS cycle detection
- All communication happens via **GoRPC** (Go's built-in RPC)
- **Optional: Data Partitioning** with consistent hashing for scalability

### Real-World Mapping

This system maps to a **Distributed Inventory System for E-Commerce**:

| Node | Role | Real-World Equivalent |
|------|------|----------------------|
| Node 1 | Storage Node | Mumbai Warehouse Server |
| Node 2 | Storage Node | Delhi Warehouse Server |
| Node 3 | Storage Node + Initial Leader | Bangalore Warehouse Server |

**Example scenarios:**
- Storing product inventory from Mumbai → automatically available in Delhi and Bangalore
- If Bangalore server (leader) crashes → Delhi automatically becomes coordinator
- Two warehouses updating same product stock → Ricart-Agrawala prevents conflicts
- Circular resource dependencies between warehouses → DAG detects and resolves deadlock

---

## Tech Stack

| Component | Technology | Why |
|-----------|-----------|-----|
| Language | Go (Golang) | Excellent concurrency with goroutines |
| Communication | GoRPC | Go's built-in RPC package (teacher's requirement) |
| Storage | In-memory (Go maps) | No external DB needed, simple for demo |
| Config | JSON file | Easy to update IPs before demo |

---

## Project Structure

```
dynamo-go/
├── config.json              # Node IPs and ports (update before demo)
├── main.go                  # Entry point: starts node, initializes ALL managers
├── go.mod                   # Go module definition
│
├── node/
│   ├── node.go              # Node struct, Lamport clock, interfaces
│   │                          - NodeConfig, Config (from JSON)
│   │                          - StoredItem (key-value with timestamp)
│   │                          - MutexManager interface
│   │                          - DeadlockDetector interface
│   │                          - Lamport clock: Increment, Update, Get
│   │                          - Leader helpers: SetLeader, GetLeader, AmILeader
│   │                          - Address helpers: GetNodeAddress, GetOtherNodes
│   │
│   └── rpc_handlers.go      # ALL RPC methods that nodes expose
│                               - Put/Get/Delete (with partition-aware replication)
│                               - GetPartitionInfo: query partition ownership
│                               - Election/Coordinator/Heartbeat (Bully)
│                               - RequestCS/ReleaseCS (Ricart-Agrawala)
│                               - DAGLock/DAGUnlock/DAGDetect/DAGResolve
│                               - MutexPut (combines mutex + put + replicate)
│                               - Status/ListKeys
│
├── partition/
│   ├── consistent_hash.go    # Consistent hashing for data partitioning
│   │                          - Hash ring with virtual nodes
│   │                          - GetNode(): single placement
│   │                          - GetNodes(): replica placement
│   │
│   └── partition_manager.go  # Partition assignment management
│                               - Enable/disable partitioning
│                               - GetPartitionOwners(): which nodes own a key
│                               - IsOwner/IsPrimaryOwner(): query ownership
│                               - SetReplicationFactor(): replica count
│
├── election/
│   └── bully.go             # Bully Algorithm implementation
│                               - StartElection(): sends ELECTION to higher nodes
│                               - becomeLeader(): announces COORDINATOR to all
│                               - StartHeartbeatMonitor(): checks leader every 3s
│                               - checkLeaderAlive(): heartbeat with timeout
│                               - TriggerElectionOnStartup(): initial election
│
├── mutex/
│   └── ricart_agrawala.go   # Ricart-Agrawala mutual exclusion
│                               - RequestCriticalSection(): gets grants from all
│                               - ReleaseCriticalSection(): sends deferred replies
│                               - ExecuteInCriticalSection(): wraps request/do/release
│                               - Handles dead nodes (timeout = assumed granted)
│                               - Deferred queue for concurrent requests
│
├── deadlock/
│   └── dag.go               # DAG Wait-For Graph deadlock detection
│                               - Lock(): grants resource or adds wait edge
│                               - Unlock(): releases resource, clears edges
│                               - DetectCycle(): DFS traversal to find cycles
│                               - Resolve(): aborts transaction to break deadlock
│                               - GetWaitForGraph(): returns edge list
│
├── client/
│   └── client.go            # Interactive CLI client
│                               - put/get/delete: basic storage operations
│                               - partition-info: query partition ownership
│                               - status/list: cluster monitoring
│                               - mutex-put: Ricart-Agrawala protected write
│                               - deadlock-test: full DAG demo scenario
│
└── testfiles/
    ├── test.txt             # "Hello, this is a test file..." (for put/get demo)
    ├── data1.txt            # "DATA FROM LAPTOP 1..." (for mutex demo)
    ├── data2.txt            # "DATA FROM LAPTOP 2..." (for mutex demo)
    └── bigdata.txt          # DAG explanation text (for deadlock demo)
```

---

## Algorithms Implemented (3 Algorithms)

### Algorithm 1: Bully Algorithm (Leader Election)

**File:** `election/bully.go` (277 lines)

**What it does:** Automatically elects a leader among the nodes. The node with the highest ID becomes leader.

**How it works:**
1. When a node starts, it triggers an election after 2 seconds
2. It sends `ELECTION` message to all nodes with **higher IDs**
3. If any higher node responds → that node takes over the election
4. If **no higher node responds** → this node becomes leader
5. New leader sends `COORDINATOR` message to ALL other nodes
6. Leader sends **heartbeat** every 3 seconds to all nodes
7. If a follower doesn't receive heartbeat within 5 seconds → starts new election

**Why DynamoDB needs this:** DynamoDB has coordinator nodes that route requests. If a coordinator fails, another must take over automatically.

**Key code flow:**
```
StartElection() → sendElectionMessage() to higher nodes
  ↓ no response
becomeLeader() → sendCoordinatorMessage() to all nodes
  ↓
StartHeartbeatMonitor() → sendHeartbeats() every 3 seconds
  ↓ leader dies
checkLeaderAlive() fails → StartElection() again
```

---

### Algorithm 2: Ricart-Agrawala (Mutual Exclusion)

**File:** `mutex/ricart_agrawala.go` (255 lines)

**What it does:** Ensures that only ONE node can write data at a time, preventing conflicts when multiple nodes try to update the same key.

**How it works:**
1. Node wants to write → calls `RequestCriticalSection()`
2. Sends `CS_REQUEST` with **Lamport timestamp** to ALL other nodes
3. Each receiving node decides:
   - If **not requesting CS** → immediately grants (reply = true)
   - If **also requesting CS** → compares timestamps:
     - Requester has **lower timestamp** (older request) → grants immediately
     - Requester has **higher timestamp** (newer request) → **defers** reply (adds to queue)
     - **Same timestamp** → lower Node ID wins (tiebreaker)
4. Once ALL alive nodes grant → node **enters Critical Section**
5. Node writes data + replicates inside CS
6. Node **releases CS** → sends all deferred replies from queue

**Why DynamoDB needs this:** Multiple clients writing to same key simultaneously would cause data corruption. Mutual exclusion ensures ordering.

**Key code flow:**
```
RequestCriticalSection()
  → sendCSRequest() to Node 1  → Granted ✓
  → sendCSRequest() to Node 2  → Granted ✓
  → All granted → ENTER CS
  → Write data + replicate
ReleaseCriticalSection()
  → Send deferred replies
  → Notify all: CS released
```

---

### Algorithm 3: DAG / Wait-For Graph (Deadlock Detection)

**File:** `deadlock/dag.go` (200+ lines)

**What it does:** Detects deadlocks (circular dependencies) when multiple nodes compete for the same resources, and resolves them automatically.

**How it works:**
1. The **leader node** maintains a global **Wait-For Graph** (directed graph)
2. Nodes in the graph = Node IDs
3. Edges in the graph = "Node X is waiting for Node Y to release a resource"
4. When a node requests a resource:
   - If **free** → grant it, add to held resources
   - If **held by another node** → add edge (requester → holder) to the graph
5. To detect deadlock: run **DFS (Depth-First Search)** on the graph
6. If DFS finds a **cycle** → that's a deadlock!
7. Resolution: **abort** one transaction in the cycle (the younger one)
8. Aborted node's resources are released, other nodes can proceed

**Why DynamoDB needs this:** In a distributed database, nodes may hold locks on resources and wait for each other, creating circular waits. Without detection, the system would freeze forever.

**Key code flow:**
```
Node 1: Lock(Resource_A) → Granted ✓
Node 2: Lock(Resource_B) → Granted ✓
Node 1: Lock(Resource_B) → Held by Node 2 → Edge: 1→2
Node 2: Lock(Resource_A) → Held by Node 1 → Edge: 2→1

Wait-For Graph:
  Node 1 → Node 2
  Node 2 → Node 1

DetectCycle() using DFS:
  Visit 1 → Visit 2 → Visit 1 (already in stack!)
  CYCLE FOUND: [1, 2, 1]

Resolve(Node 2):
  Release Node 2's resources
  Node 1 can now get Resource_B ✓
```

---

## How to Run (Local Testing — 1 Laptop, 4 Terminals)

### Step 1: Open 4 Terminals

In VS Code: Terminal → click the `+` button 3 times to get 4 terminal tabs.

In **each** terminal, navigate to the project:
```bash
cd path/to/dynamo-go
```

### Step 2: Start 3 Nodes (one per terminal)

**Terminal 1:**
```bash
go run main.go 1
```

**Terminal 2:**
```bash
go run main.go 2
```

**Terminal 3:**
```bash
go run main.go 3
```

**Wait 3-5 seconds.** You'll see leader election happen:
```
[Node 3] No higher nodes found, I am the LEADER
[Node 3] Announced leadership to Node 2
[Node 3] Announced leadership to Node 1
[Node 1] Leader is now Node 3
[Node 2] Leader is now Node 3
```

> **Note:** "Cannot reach Node X" messages when starting are NORMAL — they happen because not all nodes are running yet. Once all 3 are up, they find each other via heartbeats.

### Step 3: Start Client

**Terminal 4:**
```bash
go run ./client/client.go
```

You'll see the command menu. Now run the use case demos.

---

## Use Case Demos (5 Use Cases)

### Use Case 1: Distributed Storage with Replication

**What we demonstrate:** Store a file on one node, retrieve it from a different node.

**Command (Terminal 4):**
```
> put 1 myfile testfiles/test.txt
```

**Expected output:**
```
Storing 'myfile' -> 'testfiles/test.txt' on Node 1 (with replication)...
SUCCESS: Stored key 'myfile' on Node 1, replicated to: Node 2 (ok), Node 3 (ok)
Data replicated to all other nodes
```

**Now retrieve from Node 2:**
```
> get 2 myfile
```

**Expected output:**
```
Retrieving 'myfile' from Node 2...
SUCCESS: Downloaded to 'myfile_downloaded.txt' (182 bytes)
Content preview: Hello, this is a test file for the distributed system demo.
```

**What this proves:** Data stored on Node 1 (Mumbai) is automatically replicated and available on Node 2 (Delhi). Distributed storage works.

---

### Use Case 2: Leader Election (Bully Algorithm)

**What we demonstrate:** When the leader dies, a new leader is automatically elected.

**Step 1 — Check current status:**
```
> status
```
```
Node 1: ONLINE | Role: Follower | Leader: Node 3 | Data: 1 items
Node 2: ONLINE | Role: Follower | Leader: Node 3 | Data: 1 items
Node 3: ONLINE | Role: LEADER   | Leader: Node 3 | Data: 1 items
```

**Step 2 — Kill the leader:** Go to Terminal 3, press **Ctrl+C**

**Step 3 — Watch Terminals 1 and 2** (wait 5-10 seconds):
```
[Node 1] Leader not responding, starting election
[Node 1] Starting ELECTION (Bully Algorithm)
[Node 2] No higher nodes found, I am the LEADER
[Node 2] Announced leadership to Node 1
```

**Step 4 — Check status again:**
```
> status
```
```
Node 1: ONLINE | Role: Follower | Leader: Node 2
Node 2: ONLINE | Role: LEADER   | Leader: Node 2
Node 3: OFFLINE (cannot connect)
```

**What this proves:** System detected leader failure via heartbeat timeout. Bully Algorithm elected Node 2 (highest alive ID) as new leader. No manual intervention needed.

**Restart Node 3** for remaining demos: In Terminal 3, run `go run main.go 3` again.

---

### Use Case 3: Mutual Exclusion (Ricart-Agrawala)

**What we demonstrate:** Writes are protected by mutual exclusion — only one node can write at a time.

**Command:**
```
> mutex-put sharedfile testfiles/data1.txt
```

**Client output:**
```
====================================
  MUTUAL EXCLUSION WRITE
  Algorithm: Ricart-Agrawala
====================================
Key: sharedfile
File: testfiles/data1.txt (241 bytes)

Leader found: Node 3
Sending MutexPut request (Ricart-Agrawala will run on server)...

SUCCESS!
Result: Key 'sharedfile' stored with mutual exclusion (Ricart-Agrawala) and replicated to all nodes
```

**Node 3 terminal shows the real algorithm running:**
```
[Node 3] ===== MUTEX-PUT START for key 'sharedfile' =====
[Node 3] Requesting critical section via Ricart-Agrawala...
[Node 3] Requesting CRITICAL SECTION (timestamp=25)
[Node 3] Need replies from 2 nodes: [1 2]
[Node 3] Received REPLY (grant) from Node 1
[Node 3] Received REPLY (grant) from Node 2
[Node 3] CS Request Summary: granted=2, dead=0, alive=2, expected=2
[Node 3] ENTERING Critical Section
[Node 3] MUTEX-PUT: Stored key 'sharedfile' locally inside Critical Section
[Node 3] Replicated key 'sharedfile' to Node 1
[Node 3] Replicated key 'sharedfile' to Node 2
[Node 3] RELEASING Critical Section
[Node 3] ===== MUTEX-PUT COMPLETE for key 'sharedfile' =====
```

**Node 1 and Node 2 terminals show:**
```
[Node 1] Received CS REQUEST from Node 3 (timestamp=25)
[Node 1] Granted CS to Node 3 immediately
[Node 1] REPLICATED: key='sharedfile', size=241 bytes
[Node 1] Node 3 released Critical Section
```

**What this proves:** Node 3 requested permission from ALL other nodes. Both granted (their Lamport timestamps were compared). Only then did Node 3 write data inside the critical section. This ensures no two nodes write simultaneously.

---

### Use Case 4: Deadlock Detection (DAG / Wait-For Graph)

**What we demonstrate:** The system detects and resolves deadlocks using a Wait-For Graph with DFS cycle detection.

**Command:**
```
> deadlock-test inventory
```

**Full output (step by step):**
```
====================================
  DAG DEADLOCK DETECTION TEST
  Algorithm: Wait-For Graph (DAG)
====================================

--- Step 1: Node 1 locks Resource A ---
Result: Resource 'inventory_A' granted to Node 1

--- Step 2: Node 2 locks Resource B ---
Result: Resource 'inventory_B' granted to Node 2

--- Current State ---
Held Resources:
  'inventory_A' -> Node 1
  'inventory_B' -> Node 2
Wait-For Edges:
  (none)
No deadlock yet - no cycles in graph

--- Step 3: Node 1 tries to lock Resource B (held by Node 2) ---
Result: Node 1 waiting for Node 2 to release 'inventory_B'
Wait-For Graph edge added: Node 1 → Node 2

--- Step 4: Node 2 tries to lock Resource A (held by Node 1) ---
Result: Node 2 waiting for Node 1 to release 'inventory_A'
Wait-For Graph edge added: Node 2 → Node 1

--- Wait-For Graph (DAG) ---
Held Resources:
  'inventory_A' -> Node 1
  'inventory_B' -> Node 2
Wait-For Edges:
  Node 1 → Node 2
  Node 2 → Node 1

--- Step 5: Running Cycle Detection (DFS) ---
Result: DEADLOCK DETECTED! Cycle: [1 2 1]
DEADLOCK CONFIRMED!
Node 1 waits for Node 2 (wants Resource B)
Node 2 waits for Node 1 (wants Resource A)
This forms a cycle: 1 → 2 → 1

--- Step 6: Resolving Deadlock ---
Strategy: Abort the younger transaction (Node 2)
Result: Deadlock resolved by aborting Node 2

--- Step 7: Node 1 retries Resource B ---
Result: Resource 'inventory_B' granted to Node 1

--- Step 8: Verify no deadlock ---
Result: No deadlock detected

DEADLOCK DETECTED AND RESOLVED using DAG algorithm!
```

**Node 3 (leader) terminal shows:**
```
[DAG] Resource 'inventory_A' GRANTED to Node 1 (resource was free)
[DAG] Resource 'inventory_B' GRANTED to Node 2 (resource was free)
[DAG] Resource 'inventory_B' held by Node 2. Node 1 must WAIT. Edge added: 1 → 2
[DAG] Resource 'inventory_A' held by Node 1. Node 2 must WAIT. Edge added: 2 → 1
[DAG] DEADLOCK DETECTED! Cycle: [1 2 1]
[DAG] RESOLVING deadlock: Aborting Node 2's transactions
[DAG] Released resource 'inventory_B' (was held by aborted Node 2)
[DAG] Node 2's transactions aborted, deadlock resolved
[DAG] Resource 'inventory_B' GRANTED to Node 1 (resource was free)
[DAG] No deadlock detected (no cycles in Wait-For Graph)
```

**What this proves:** Created a circular resource dependency. The Wait-For Graph tracked all wait edges. DFS found the cycle `[1→2→1]`. Resolved by aborting one transaction. System continued normally without freezing.

---
### Use Case 5: Quorum Read with Read Repair

1. Store a value on any node, e.g. `put 1 myfile testfiles/test.txt`.
2. Force one replica to become stale by writing a different value directly (or by editing its in-memory data in code).
3. Run `quorum-get myfile` from the client. The client will:
   - Contact two nodes in the cluster.
   - Compare their Lamport timestamps and pick the newest value.
   - If one node has an older or missing value, it will automatically repair that node by issuing a `Put`.
4. The freshest data is saved locally (`myfile_downloaded.txt`) and both nodes are now consistent.

This demonstrates DynamoDB’s R/W/N model with **R=2** (read from two replicas) and simple **read repair** built on top of the existing `Get`/`Put` RPCs.

---

## Running on 3 Laptops (Demo Day Setup)

### Requirements
- 3 laptops with **Go installed** (`go version` should work)
- All connected to the **SAME network**
- The project folder copied to ALL 3 laptops

### Step 1: Connect to Same Network

**Best option:** One person creates a **mobile hotspot** from their phone. All 3 laptops connect to it. (More reliable than school WiFi)

**Alternative:** All connect to the same WiFi network.

### Step 2: Find Each Laptop's IP

On **each** laptop, open a terminal:

**Windows:**
```bash
ipconfig
```
Look for **IPv4 Address** under the WiFi adapter section. Example: `192.168.43.101`

**Mac:**
```bash
ifconfig en0
```

### Step 3: Update config.json

Edit `config.json` on **ALL 3 laptops** with the actual IPs:
```json
{
    "nodes": [
        {"id": 1, "ip": "192.168.43.101", "port": 8001},
        {"id": 2, "ip": "192.168.43.102", "port": 8002},
        {"id": 3, "ip": "192.168.43.103", "port": 8003}
    ],
    "heartbeat_interval_ms": 3000,
    "election_timeout_ms": 5000,
    "deadlock_timeout_ms": 5000
}
```

> **IMPORTANT:** The `config.json` must be **IDENTICAL** on all 3 laptops.

### Step 4: Allow Through Firewall

When you first run the program, Windows will show a firewall popup. Click **"Allow access"**.

If it still doesn't work, temporarily disable firewall:
```bash
netsh advfirewall set allprofiles state off
```

### Step 5: Start Nodes

- **Laptop 1 (Person A):** `go run main.go 1`
- **Laptop 2 (Person B):** `go run main.go 2`
- **Laptop 3 (Person C):** `go run main.go 3`

### Step 6: Run Client

On **any** laptop (e.g., Laptop 1 in a second terminal):
```bash
go run ./client/client.go
```

Then run the same demo commands: `put`, `get`, `status`, `mutex-put`, `deadlock-test`.

### Step 7: Kill Node Demo

Press **Ctrl+C** on the laptop whose node you want to kill. Watch other laptops' terminals for election messages.

---

## Configuration Reference

```json
{
    "nodes": [
        {"id": 1, "ip": "127.0.0.1", "port": 8001},
        {"id": 2, "ip": "127.0.0.1", "port": 8002},
        {"id": 3, "ip": "127.0.0.1", "port": 8003}
    ],
    "heartbeat_interval_ms": 3000,
    "election_timeout_ms": 5000,
    "deadlock_timeout_ms": 5000
}
```

| Field | Description |
|-------|-------------|
| `nodes.id` | Unique node identifier (1, 2, or 3) |
| `nodes.ip` | IP address (`127.0.0.1` for local, actual IP for multi-laptop) |
| `nodes.port` | TCP port for RPC communication |
| `heartbeat_interval_ms` | How often leader sends heartbeat (3 seconds) |
| `election_timeout_ms` | How long to wait before declaring leader dead (5 seconds) |
| `deadlock_timeout_ms` | Timeout for deadlock-related operations (5 seconds) |
| `partition.enabled` | Enable data partitioning with consistent hashing (default: false) |
| `partition.replication_factor` | Number of replicas per key (default: 3) |

---

## Data Partitioning (Optional)

By default, this system uses **full replication** (every node stores every key). For scalability, you can enable **DynamoDB-style data partitioning** using consistent hashing.

**Key Benefits:**
- Store only your partition's data, not all data
- Scales to hundreds of nodes efficiently
- Still maintains configurable replication for fault tolerance
- Backward compatible (toggle on/off in config)

**Enable in config.json:**
```json
{
    "partition": {
        "enabled": true,
        "replication_factor": 3
    }
}
```

**Check partition ownership:**
```bash
> partition-info <node_id> <key>
Primary Owner: Node 1
Replicas: Node 2, Node 3
```

**See [PARTITIONING.md](PARTITIONING.md) for detailed documentation:**
- How consistent hashing works
- Configuration options
- Scaling considerations
- API examples
- Performance characteristics

---