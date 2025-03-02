# Hashicorp Raft: Plug-n-Play

## What is Raft?

Raft is a distributed consensus algorithm designed to be understandable and implementable. 
It ensures that multiple nodes in a distributed system agree on a log of transactions, even in 
the presence of failures. Unlike Paxos, which is often considered complex and difficult to implement,
Raft is structured around leader election, log replication, and safety guarantees.
Key Features of Raft:

1. Leader Election – nodes elect a leader that manages log replication.
2. Log Replication – the leader ensures that all followers apply the same sequence of log entries.
3. Commit and Safety – Raft guarantees that once an entry is committed, it remains committed.
4. Membership Changes – Raft allows nodes to join and leave the cluster dynamically.

## hashicorp/raft – The Go Implementation

[github.com/hashicorp/raft](https://github.com/hashicorp/raft) is a production-ready Go implementation of the Raft protocol. 
It is used in HashiCorp's tools like Consul and Nomad to maintain consistent state across nodes.
Core Components of hashicorp/raft:

The raft package is structured around key components:
    `Raft` - the main struct managing consensus and state transitions;
    `FSM (Finite State Machine)` - application logic for handling committed logs;
    `LogStore` - persistent storage for logs;
    `StableStore` - persistent storage for Raft metadata (e.g., term, voted leader);
    `Transport` - handles communication between nodes;
    `SnapshotStore` - saves snapshots of the FSM to reduce log size.

## Breakdown of hashicorp/raft APIs

### Log Storage

The LogStore stores the Raft log entries, ensuring persistent, ordered application of commands. 
The most common implementation are:
- [raft-boltdb](https://github.com/hashicorp/raft-boltdb)

```go
logStore, err := raftboltdb.NewBoltStore("raft-log.bolt")
if err != nil {
    log.Fatal(err)
}
```

- [raft-leveldb](https://github.com/tidwall/raft-leveldb)

```go
logStore, err := raftleveldb.NewLevelDBStore("raft-log.leveldb", 0)
if err != nil {
    log.Fatal(err)
}
```

- [raft-badgerdb](https://github.com/BBVA/raft-badger)

```go
logStore, err := raftbadger.NewBadgerStore("raft-log.badger")
if err != nil {
    log.Fatal(err)
}
```

### State Machine (FSM)

The Finite State Machine (FSM) applies committed log entries to an application's state.

```go
type FSM struct{}

func (f *FSM) Apply(log *raft.Log) interface{} {
    log.Println("Applying log:", string(log.Data))
    return nil
}

func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
    return &Snapshot{}, nil
}

func (f *FSM) Restore(rc io.ReadCloser) error {
    return nil
}
```

### Transport Layer

Communication between nodes uses a transport abstraction. The two main implementations are:

```
raft.NewTCPTransport(...) – Production use
raft.NewInmemTransport(...) – Testing
```

```go
transport, err := raft.NewTCPTransport("127.0.0.1:8080", nil, 3, time.Second, os.Stderr)
```

### Cluster Membership Management

Nodes can join or leave the cluster dynamically. Here's an example:

```go
raftNode.AddVoter(raft.ServerID("node-2"), "127.0.0.1:8081", 0, 0)
raftNode.RemoveServer(raft.ServerID("node-3"), 0, 0)
```

## How Raft Works Internally in HashiCorp's Implementation

1. Leader Election:
- nodes start in the Follower state.
- if no leader is detected, a Candidate state is initiated.
- candidates request votes from other nodes.
- a majority vote elects a Leader.

2. Log Replication:
- clients send commands to the Leader.
- the Leader appends the command to its log.
- the command is replicated to followers.
- if a majority acknowledges, the log entry is committed.

3. Cluster Membership Changes:
- new nodes start as non-voting members.
- the Leader updates the cluster configuration.
- when committed, the node becomes active.

## Setting Up a Raft Node in Go

A Raft node is initialized using the raft.NewRaft(...) function. Below is a minimal setup:

```go
package main

import (
    "log"
    "os"
    "time"

	"github.com/hashicorp/raft"
	"github.com/hashicorp/raft-boltdb"
)

func main() {
    // Create configuration
    config := raft.DefaultConfig()
    config.LocalID = raft.ServerID("node-1")

	// Setup Raft storage (logs, metadata, snapshots)
	logStore, _ := raftboltdb.NewBoltStore("raft-log.bolt")
	stableStore, _ := raftboltdb.NewBoltStore("raft-stable.bolt")
	snapshots, _ := raft.NewFileSnapshotStore("snapshots", 3, os.Stderr)

	// Transport layer (communication between nodes)
	transport, _ := raft.NewTCPTransport("127.0.0.1:8081", nil, 3, time.Second, os.Stderr)

	// Initialize the Raft node
	raftNode, err := raft.NewRaft(config, &FSM{}, logStore, stableStore, snapshots, transport)
	if err != nil {
		log.Fatal(err)
	}

	// setup a cluster
	configuration := raft.Configuration{
		Servers: []raft.Server{
			{ID: config.LocalID, Address: transport.LocalAddr()},
			{ID: raft.ServerID("node-2"), Address: "127.0.0.1:8082"},
			{ID: raft.ServerID("node-3"), Address: "127.0.0.1:8083"},
		},
	}
	raftNode.BootstrapCluster(configuration)
}
```

Now theoretical part is done. Looks simple, huh? Let's start out practice.

## Caveats
Inexperienced users may run into various issues when integrating HashiCorp’s Raft library 
into their distributed systems. Here are some of the most common pitfalls and how to avoid them:

### Misunderstanding Cluster Bootstrapping

`BootstrapCluster` does many implicit operations (not really SOLID function) which are incomprehensible
by inexperienced user. For example:

- you can't bootstrap cluster twice if you set up persistent storage. `BootstrapCluster` stores
  information about all cluster nodes and their initial states. Second call of this function will
  return error:
  `ErrCantBootstrap = errors.New("bootstrap only works on new clusters")`
  So it's quite challenging to set up cluster only once. There are several options what you can do:
    1. Call `BootstrapCluster` and ignore the error. Simplest solution but potentially you can miss
      some other important errors.
    2. Set up some distributed lock (like Redis one) which will prevent other nodes to call
       `BootstrapCluster` function.
    3. Completely rewrite `BootstrapCluster` function using your own custom logic.
  
- you must use persistent storage at least for `stable store` and `snapshot store`. Otherwise, your
  Raft node will lose its stability and won't be able to sync with other nodes.
- node must have unique `ServerID` across all Raft cluster and consistent address also.
- other cluster nodes may not be ready to be part of a cluster due to network conditions or
  just because of start up lag. That will lead of impossibility to elect leader and cluster
  will become stalled.

Here's my personal solution to avoid `BootstrapCluster` function unpredictive behavior:

```go
func ForceBootstrap(addr raft.ServerID, logStore raft.LogStore, stableStore raft.StableStore) error {
	lastIndex, err := logStore.LastIndex()
	if err != nil {
		return fmt.Errorf("failed to read last log index: %v", err)
	}

	if lastIndex != 0 {
		// if last index is not zero it means that there's already set up and active Raft cluster
		// so skip
		return nil
	}

	// force Raft to create single node cluster no matter what;
	// single node will elect itself as a leader immediately;
	// after connection to other nodes cluster reelects leader again
	raftConf := raft.Configuration{}
	raftConf.Servers = append(raftConf.Servers, raft.Server{
		Suffrage: raft.Voter,
		ID:       addr,
		Address:  raft.ServerAddress(addr),
	})

	// set up initial values for log store and stable store
	if err := stableStore.SetUint64([]byte("CurrentTerm"), 1); err != nil {
		return fmt.Errorf("failed to save current term: %v", err)
	}
	if err := logStore.StoreLog(&raft.Log{
		Type: raft.LogConfiguration, Index: 1, Term: 1,
		Data: raft.EncodeConfiguration(raftConf),
	}); err != nil {
		return fmt.Errorf("failed to store bootstrap log: %v", err)
	}

	// ensure that cluster last index now is not zero
	return logStore.GetLog(1, &raft.Log{})
}

```

### Misunderstanding Cluster Readiness

One of the most common pitfalls when working with Raft is misunderstanding when a cluster 
is actually ready to serve requests. If nodes aren’t properly initialized, users might experience 
unexpected failures, stale reads, or unresponsive services.
Cluster may appear "Up" but its actually isn’t ready and here are symptoms of it:
- cluster starts, but requests fail or get stuck;
- leader election occurs, but logs don’t replicate;
- clients receive stale data or not the leader errors;
- cluster stays in the follower state forever;
- leader election never succeeds, even after multiple retries;
- clients query a follower node and get stale data;
- leader has committed new entries, but followers lag.

The solution of above problems requires understanding how Raft manages nodes roles. 
You need to be sure prior that Raft has majority of nodes (quorum) to be alive to make decisions.
And it's absolutely necessary to check cluster quorum before serving requests and check if there is
a leader elected.
Also don't forget that nodes state takes time to be synchronized so you won't be able to apply any
new state until sync is done.

Here is how it might look in a code:

```go
    raftNode, _ := raft.NewRaft(config, &FSM{}, logStore, stableStore, snapshots, transport)
    ticker := time.NewTicker(time.Second)
    defer ticker.Stop()

	// wait for leader election
loop:
    for {
        select {
        case <-ticker.C:
            if addr, id := raftNode.LeaderWithID(); addr != "" {
            // leader found!
            break loop
        }
        case <-ctx.Done():
            return ctx.Err()
        }
    }

	// after leader election let's wait own node promotion to a voter
loop2:
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
            wait := raftNode.GetConfiguration()
			if err := wait.Error(); err != nil {
                return err
            }
        
        if isVoter(id, wait.Configuration()) {
			// node is voter!
            break loop2
        }
        }
    }

	// wait state to be sync. Applied index must be equal to last index
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
            lastAppliedIndex := r.raft.AppliedIndex()
            lastIndex := r.raft.LastIndex()
            
			
            if lastAppliedIndex == lastIndex {
                return nil
            }
        }
    }
```

### Misunderstanding FSM (Finite State Machine) and State Data Types
When implementing Raft with `hashicorp/raft`, a common mistake is misunderstanding 
how the `FSM (Finite State Machine)` works and what data types can or should be stored. 
This often leads to data corruption, crashes, or performance issues. Here are the most common
caveats:
- logs cannot be replayed because non-serializable types (e.g., maps with interfaces) were used;
- FSM only applies logs on the leader; followers replicate logs but don’t execute them;
- FSM doesn't validate your data in any way. You must create custom FSM for that;
- FSM is an in-memory application state that is rebuilt from Raft logs. It is not automatically 
  persistent. To persist state across restarts, enable snapshots so Raft can restore FSM efficiently.

After some research i've found out that the most suitable data type for FSM is a `map[string]string`.
Having unique keys for you data is a leading to good state consistency. Of course, it's regular
Golang map so its methods must be wrapped to mutex to avoid potential race condition because
`hashicorp/raft` may call `FSM` methods asynchronously.
Also, if you need nodes to have consensus about validity of something like transaction, for example,
you need `FSM` to have validation functionality.

Here's how basic implementation of `FSM` that consider of above notes might look like:

```go
type (
	KVState map[string]string // THE 'STATE'!

	ConsensusValidatorFunc func(k, v string) error
)

type fsm struct {
    mux *sync.Mutex // mutex hat
    state     *KVState // link to a curren state
	prevState KVState // previous state store for proper disaster recovery
	
	validators []ConsensusValidatorFunc // custom validators
}


func newFSM(validators ...ConsensusValidatorFunc) *fsm {
    // initializing state to avoid nil state errors
	// 'genesis' value here is only for observability and not required
	state := KVState{"genesis": "genesis"}
	return &fsm{
		state:      &state,
		prevState:  KVState{},
		mux:        new(sync.Mutex),
		validators: validators,
	}
}

// Apply is invoked by Raft once a log entry is commited. Do not use directly.
func (fsm *fsm) Apply(rlog *raft.Log) (result interface{}) {
	fsm.mux.Lock()
	defer fsm.mux.Unlock()
	defer func() {
		// attempt to save state after panic
		if r := recover(); r != nil {
			*fsm.state = fsm.prevState
			result = errors.New("fsm apply panic: rollback")
		}
	}()
	var newState = make(KVState, 1)
	if err := json.Unmarshal(rlog.Data, &newState); err != nil {
		return fmt.Errorf("failed to decode log: %w", err)
	}

	for _, validator := range fsm.validators {
		// apply validator on each key/value
		for k, v := range newState {
			if err := validator(k, v); err != nil {
				return err
			}
		}
		// applied validators doesn't stop method Apply from storing data
	}
	
	// save previous state
	fsm.prevState = make(KVState, len(*fsm.state))
	for k, v := range *fsm.state {
		fsm.prevState[k] = v
	}
	// save new state
	for k, v := range newState {
		(*fsm.state)[k] = v
	}
	return fsm.state
}

// Snapshot encodes the current state so that we can save a snapshot.
func (fsm *fsm) Snapshot() (raft.FSMSnapshot, error) {
	fsm.mux.Lock()
	defer fsm.mux.Unlock()

	buf := new(bytes.Buffer)
	err := json.NewEncoder(buf).Encode(fsm.state)
	if err != nil {
		return nil, err
	}

	return &fsmSnapshot{state: buf}, nil
}

// Restore takes a snapshot and sets the current state from it.
func (fsm *fsm) Restore(reader io.ReadCloser) (err error) {
	defer func() {
		err = reader.Close()
	}()

	fsm.mux.Lock()
	defer fsm.mux.Unlock()

	err = json.NewDecoder(reader).Decode(fsm.state)
	if err != nil {
		return err
	}

	// here previous state might be handy
	fsm.prevState = make(map[string]string, len(*fsm.state))
	return nil
}

// just a buffer for snapshot data
type fsmSnapshot struct {
	state *bytes.Buffer
}

// Persist writes the snapshot (a serialized state) to a raft.SnapshotSink.
func (snap *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	_, err := io.Copy(sink, snap.state)
	if err != nil {
		_ = sink.Cancel()
		return err
	}
	return sink.Close()
}

func (snap *fsmSnapshot) Release() {
	// interface stub
}
```

Full simple wrapper for HashiCorp's Raft implementation in Go that helps to avoid all above issue
you can find here: https://github.com/filinvadim/easy-raft

## Useful links
- Hashicorp Raft Golang implementation - https://github.com/hashicorp/raft
- Raft paper - https://raft.github.io/raft.pdf
- Raft Wikipedia - https://en.wikipedia.org/wiki/Raft_(algorithm)
