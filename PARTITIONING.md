# Data Partitioning & Consistent Hashing

## Overview

This DynamoDB implementation now includes **optional DynamoDB-style data partitioning** using **Consistent Hashing**. This allows you to scale your system efficiently without replicating every key-value pair to all nodes.

## Key Features

✅ **Non-Invasive**: Existing code continues to work with full replication by default  
✅ **Toggle On/Off**: Enable/disable partitioning via `config.json` without code changes  
✅ **Consistent Hashing**: Keys are deterministically mapped to nodes using MD5-based hash ring  
✅ **Configurable Replication**: Set the number of replicas per key (e.g., 3 = primary + 2 backups)  
✅ **Backward Compatible**: Works alongside Bully election, Ricart-Agrawala mutex, and DAG deadlock detection  

---

## How It Works

### Without Partitioning (Default)
```
Every node stores every key:
Put(key) → replicated to Nodes 1, 2, 3
Get(key) → can be read from any node
Delete(key) → deleted from all nodes
```

### With Partitioning Enabled
```
Consistent hashing determines ownership:
Put(user:123) → primary on Node 1, replicas on Nodes 2, 3
Put(user:456) → primary on Node 3, replicas on Nodes 1, 2
Get(user:123) → read from Node 1 (or its replicas)
Delete(user:123) → deleted from Nodes 1, 2, 3
```

---

## Configuration

### Enable Partitioning

Edit `config.json`:

```json
{
    "nodes": [...],
    "heartbeat_interval_ms": 3000,
    "election_timeout_ms": 5000,
    "deadlock_timeout_ms": 5000,
    "partition": {
        "enabled": true,
        "replication_factor": 3
    }
}
```

**Configuration Options:**

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `partition.enabled` | bool | `false` | Enable/disable data partitioning |
| `partition.replication_factor` | int | `3` | Number of replicas per key (including primary) |

---

## API Usage

### Get Partition Information

Check which nodes store a given key:

**Client Command:**
```bash
partition-info <node_id> <key>
```

**Example:**
```bash
> partition-info 1 myfile
====================================
Partition Info for Key: 'myfile'
====================================
Partitioning Enabled: true
Replication Factor: 3
Primary Owner: Node 1
Replicas: Node 2, Node 3

Key 'myfile': Stored on Node 1 (primary), Node 2 (replica), Node 3 (replica)
====================================
```

### RPC Handler

**Go Code:**
```go
args := &node.PartitionInfoArgs{Key: "myfile"}
var reply node.PartitionInfoReply
err := client.Call("Node.GetPartitionInfo", args, &reply)

if err != nil {
    log.Fatal(err)
}

fmt.Printf("Primary: Node %d\n", reply.Primary)
fmt.Printf("Replicas: %v\n", reply.Replicas)
```

---

## Consistent Hashing Details

The system uses a **hash ring** with virtual nodes:

```
Virtual Nodes Per Physical Node: 160 (configurable)
Hash Function: MD5 (256-bit)
Distribution: Uniform across the ring
```

### How Keys Are Assigned:

1. **Hash Key**: `hash = MD5(key)`
2. **Find Position**: Binary search on the sorted hash ring
3. **Get Replicas**: Walk forward N positions to collect unique physical nodes
4. **Return**: [primary, replica1, replica2, ...]

**Example:**
```
Hash Ring (simplified):
Node 1 ---- Node 2 ---- Node 3 ---- [wrap around]

Key "user:123" hashes to position between Node 1 and Node 2
→ Primary: Node 2
→ Next available: Node 3
→ Next available: Node 1
→ Result: [2, 3, 1]
```

---

## Replication Behavior

### Put Operation
1. Store on primary node
2. Replicate to all replica nodes
3. Return when all replicas acknowledge

### Get Operation
1. Read from any owner node (primary or replica)
2. Return immediately on success
3. Can implement quorum reads for stronger consistency

### Delete Operation
1. Delete from primary and all replicas
2. Propagates asynchronously to replicas
3. Return on primary success

---

## Scaling Implications

### With Full Replication (Partitioning Disabled)
- **Disk Usage**: O(n × m) where n = nodes, m = keys
- **Storage per node**: 100% of total data
- **Write latency**: Distribution to all nodes
- **Read latency**: Low (any node works)
- **Max scalability**: ~10-20 nodes before replication overhead

### With Partitioning (Enabled, RF=3)
- **Disk Usage**: O(rf/n × m) where rf = replication factor
- **Storage per node**: ~30% of total data (3 replicas across 9+ nodes)
- **Write latency**: Distribution to 3 replicas only
- **Read latency**: Still very low (nearest replica)
- **Max scalability**: 100+ nodes easily

---

## Advanced: Modifying Partition Settings at Runtime

The `PartitionManager` supports dynamic configuration:

```go
// Enable partitioning
node.PartitionMgr.SetEnabled(true)

// Change replication factor
node.PartitionMgr.SetReplicationFactor(5)

// Check current settings
fmt.Printf("Enabled: %v\n", node.PartitionMgr.IsEnabled())
fmt.Printf("RF: %d\n", node.PartitionMgr.GetReplicationFactor())

// Get partition owners for a key
owners := node.PartitionMgr.GetPartitionOwners("mykey")
// → [1, 3, 2]
```

---

## Examples

### Example 1: Store and Retrieve Partitioned Data

```bash
# Start 3 nodes
node 1 &
node 2 &
node 3 &

# Enable partitioning in config.json

# Put a file
> put 1 invoice-001 testfiles/bigdata.txt
[Node 1] PUT: key='invoice-001', replicated to: Nodes 1, 2, 3

# Check where it's stored
> partition-info 1 invoice-001
Primary Owner: Node 1
Replicas: Node 2, Node 3

# Get from any replica
> get 2 invoice-001
Found key 'invoice-001' on Node 2
```

### Example 2: Observe Distribution

```bash
# Put multiple keys
> put 1 user:100 testfiles/data1.txt
> put 1 user:200 testfiles/data2.txt
> put 1 user:300 testfiles/data1.txt

# Check distribution
> partition-info 1 user:100
Primary Owner: Node 2

> partition-info 1 user:200
Primary Owner: Node 3

> partition-info 1 user:300
Primary Owner: Node 1

# Each key is on different nodes!
```

### Example 3: Replication Factor = 2

```json
{
    "partition": {
        "enabled": true,
        "replication_factor": 2
    }
}
```

```bash
> partition-info 1 mykey
Primary Owner: Node 1
Replicas: Node 3
# Only 2 copies total, less redundancy but more storage efficiency
```

---

## Interaction with Other Components

### Mutual Exclusion (Ricart-Agrawala)
- `mutex-put` respects partitioning
- Acquires critical section on primary node
- Replicates only to assigned replicas

### Deadlock Detection (DAG)
- Works independently of partitioning
- Lock tracking is per-node
- Wait-for graphs built locally

### Leader Election (Bully)
- Elects global leader regardless of partitioning
- Leader role is distribution-independent
- Still useful for coordination

---

## Testing Partitioning

### Compile
```bash
go build -o dynamo.exe ./node
go build -o client.exe ./client
```

### Run 3-Node Cluster with Partitioning

**config.json:**
```json
{
    "nodes": [
        {"id": 1, "ip": "127.0.0.1", "port": 8001},
        {"id": 2, "ip": "127.0.0.1", "port": 8002},
        {"id": 3, "ip": "127.0.0.1", "port": 8003}
    ],
    "partition": {
        "enabled": true,
        "replication_factor": 3
    }
}
```

**Start Nodes:**
```bash
go run main.go 1
go run main.go 2
go run main.go 3
```

**Run Client Tests:**
```bash
# Window 1: Node 1
# Window 2: Node 2
# Window 3: Node 3
# Window 4: Client
go run client/client.go

> put 1 test testfiles/test.txt
> partition-info 1 test
> get 2 test
> delete 1 test
```

---

## Troubleshooting

### Keys Not Found
- Check partition info: `partition-info <node> <key>`
- May not be replicated to the node you're querying from
- Use primary owner or any replica shown

### Uneven Distribution
- With RF=3 and 3 nodes, distribution is perfect
- With RF=3 and 9 nodes, each node holds ~33% of data
- Virtual nodes (160 per physical) ensure even distribution

### Partitioning Not Working
- Verify config.json has `"partition": {"enabled": true}`
- Check that all nodes are running the same config
- Restart nodes after config changes

---

## Performance Characteristics

| Operation | Without Partitioning | With Partitioning |
|-----------|--------------------|--------------------|
| Put | O(1) + replication to n nodes | O(1) + replication to rf nodes |
| Get | O(1) local read | O(1) local read |
| Delete | O(1) + deletion from n nodes | O(1) + deletion from rf nodes |
| Network Bandwidth | (n-1) × data_size | (rf-1) × data_size |

---

## References

- **Consistent Hashing**: https://en.wikipedia.org/wiki/Consistent_hashing
- **Hash Ring with Virtual Nodes**: Karger et al., "Consistent Hashing and Random Trees"
- **DynamoDB Architecture**: AWS official documentation

---

## Future Enhancements

- [ ] Dynamic node addition/removal (rebalancing)
- [ ] W/R quorum reads/writes
- [ ] Adaptive replication factor
- [ ] Partition-aware query routing
- [ ] Cross-partition transactions
