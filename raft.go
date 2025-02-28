package easy_raft

import (
	"context"
	"fmt"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/raft"
	"net"
	"os"
	"sync"
	"time"
)

type (
	Logger        = hclog.Logger
	StableStore   = raft.StableStore
	SnapshotStore = raft.SnapshotStore
	LogStore      = raft.LogStore
	Transporter   = raft.Transport
	ServerID      = raft.ServerID
	Raft          = raft.Raft
)

type raftService struct {
	ctx       context.Context
	raft      *Raft
	transport Transporter
	raftID    ServerID
	syncMx    *sync.RWMutex
	log       Logger
}

// NewEasyRaftInMemory is not going to network - for testing purposes
func NewEasyRaftInMemory(
	ctx context.Context,
	addr string,
	logger Logger,
	validators ...ConsensusValidatorFunc,
) (*raftService, error) {
	inmemStore := raft.NewInmemStore()
	_, transport := raft.NewInmemTransport(raft.ServerAddress(addr))
	return NewEasyRaft(
		ctx,
		addr,
		logger,
		inmemStore,
		inmemStore,
		raft.NewInmemSnapshotStore(),
		transport,
		validators...,
	)
}

func NewEasyRaft(
	ctx context.Context,
	addr string,
	logger Logger,
	logStore LogStore,
	stableStore StableStore,
	snapshotStore SnapshotStore,
	transport Transporter,
	validators ...ConsensusValidatorFunc,
) (_ *raftService, err error) {
	r := &raftService{
		ctx:    ctx,
		syncMx: new(sync.RWMutex),
		log:    logger,
	}

	r.syncMx.Lock()
	defer r.syncMx.Unlock()

	var inMemStore *raft.InmemStore
	if logStore == nil {
		inMemStore = raft.NewInmemStore()
		logStore = inMemStore
	}
	if stableStore == nil {
		if inMemStore != nil {
			stableStore = inMemStore
		} else {
			stableStore = raft.NewInmemStore()
		}
	}
	if snapshotStore == nil {
		snapshotStore = raft.NewInmemSnapshotStore()
	}

	config := raft.DefaultConfig()
	config.HeartbeatTimeout = time.Second * 5
	config.ElectionTimeout = config.HeartbeatTimeout
	config.LeaderLeaseTimeout = config.HeartbeatTimeout
	config.CommitTimeout = time.Second * 30
	config.LocalID = raft.ServerID(addr)

	if logger != nil {
		config.Logger = logger
	}

	if err := raft.ValidateConfig(config); err != nil {
		return nil, err
	}
	r.raftID = config.LocalID

	if transport == nil {
		netAddr, err := net.ResolveTCPAddr("tcp", addr)
		if err != nil {
			return nil, err
		}
		transport, err = raft.NewTCPTransport(addr, netAddr, 100, time.Second*5, os.Stderr)
		if err != nil {
			return nil, err
		}
	}

	if err = r.forceBootstrap(
		config.LocalID,
		logStore,
		stableStore,
	); err != nil {
		return nil, err
	}

	r.raft, err = raft.NewRaft(
		config,
		newFSM(validators...),
		logStore,
		stableStore,
		snapshotStore,
		transport,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create raft node: %w", err)
	}

	wait := r.raft.GetConfiguration()
	if err := wait.Error(); err != nil {
		return nil, fmt.Errorf("raft configuration error: %v", err)
	}

	return r, r.sync()
}

func (r *raftService) forceBootstrap(
	addr raft.ServerID,
	logStore raft.LogStore,
	stableStore raft.StableStore,
) error {
	lastIndex, err := logStore.LastIndex()
	if err != nil {
		return fmt.Errorf("failed to read last log index: %v", err)
	}

	if lastIndex != 0 {
		return nil
	}
	r.log.Debug("bootstrapping a new cluster with server id:", addr)

	// force Raft to create single node cluster no matter what
	raftConf := raft.Configuration{}
	raftConf.Servers = append(raftConf.Servers, raft.Server{
		Suffrage: raft.Voter,
		ID:       addr,
		Address:  raft.ServerAddress(addr),
	})

	if err := stableStore.SetUint64([]byte("CurrentTerm"), 1); err != nil {
		return fmt.Errorf("failed to save current term: %v", err)
	}
	if err := logStore.StoreLog(&raft.Log{
		Type: raft.LogConfiguration, Index: 1, Term: 1,
		Data: raft.EncodeConfiguration(raftConf),
	}); err != nil {
		return fmt.Errorf("failed to store bootstrap log: %v", err)
	}

	return logStore.GetLog(1, &raft.Log{})
}

type raftSync struct {
	raft   *raft.Raft
	raftID raft.ServerID
	log    Logger
}

func (r *raftService) sync() error {
	if r.raftID == "" {
		panic("raft id not initialized")
	}

	leaderCtx, leaderCancel := context.WithTimeout(r.ctx, time.Minute)
	defer leaderCancel()

	cs := raftSync{
		raft:   r.raft,
		raftID: r.raftID,
		log:    r.log,
	}

	r.log.Debug("waiting for leader...")
	leaderID, err := cs.waitForLeader(leaderCtx)
	if err != nil {
		return fmt.Errorf("waiting for leader: %w", err)
	}

	if string(r.raftID) == leaderID {
		r.log.Debug("node is a leader!")
	} else {
		r.log.Debug("current leader:", leaderID)
	}

	r.log.Debug("waiting until we are promoted to a voter...")
	voterCtx, voterCancel := context.WithTimeout(r.ctx, time.Minute)
	defer voterCancel()

	err = cs.waitForVoter(voterCtx)
	if err != nil {
		return fmt.Errorf("waiting to become a voter: %w", err)
	}
	r.log.Debug("node received voter status")

	updatesCtx, updatesCancel := context.WithTimeout(r.ctx, time.Minute*5)
	defer updatesCancel()

	err = cs.waitForUpdates(updatesCtx)
	if err != nil {
		return fmt.Errorf("waiting for consensus updates: %w", err)
	}
	r.log.Debug("sync complete")
	return nil
}

func (r *raftSync) waitForLeader(ctx context.Context) (string, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if addr, id := r.raft.LeaderWithID(); addr != "" {
				return string(id), nil
			}
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func (r *raftSync) waitForVoter(ctx context.Context) error {
	ticker := time.NewTicker(time.Second / 2)
	defer ticker.Stop()

	id := r.raftID
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			wait := r.raft.GetConfiguration()
			if err := wait.Error(); err != nil {
				return err
			}

			if isVoter(id, wait.Configuration()) {
				return nil
			}
			r.log.Debug("not voter yet:", id)
		}
	}
}

func (r *raftSync) waitForUpdates(ctx context.Context) error {
	r.log.Debug("raft state is catching up to the latest known version. Please wait...")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			lastAppliedIndex := r.raft.AppliedIndex()
			lastIndex := r.raft.LastIndex()
			r.log.Debug("current raft index: ", lastAppliedIndex, "/", lastIndex)
			if lastAppliedIndex == lastIndex {
				return nil
			}
		}
	}
}

func isVoter(srvID raft.ServerID, cfg raft.Configuration) bool {
	for _, server := range cfg.Servers {
		if server.ID == srvID && server.Suffrage == raft.Voter {
			return true
		}
	}
	return false
}

func (r *raftService) ID() ServerID {
	r.waitSync()

	return r.raftID
}

func (r *raftService) AddVoter(addr ServerID) {
	if r == nil {
		return
	}
	if addr == "" {
		return
	}

	r.waitSync()

	if r.raft == nil {
		return
	}

	if _, leaderId := r.raft.LeaderWithID(); r.raftID != leaderId {
		return
	}
	r.log.Debug("adding new voter:", addr)

	configFuture := r.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		r.log.Error("failed to get raft configuration:", err)
		return
	}
	prevIndex := configFuture.Index()

	wait := r.raft.RemoveServer(
		addr, prevIndex, 30*time.Second,
	)
	if err := wait.Error(); err != nil {
		r.log.Warn("failed to remove raft server:", wait.Error())
	}

	wait = r.raft.AddVoter(
		addr, raft.ServerAddress(addr), prevIndex, 30*time.Second,
	)
	if wait.Error() != nil {
		r.log.Error("failed to add voted:", wait.Error())
	}
	return
}

func (r *raftService) RemoveVoter(addr ServerID) {
	if r == nil {
		return
	}
	if addr == "" {
		return
	}

	r.waitSync()

	if r.raft == nil {
		return
	}

	if _, leaderId := r.raft.LeaderWithID(); r.raftID != leaderId {
		return
	}
	r.log.Debug("removing voter:", addr)

	configFuture := r.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		r.log.Error("failed to get raft configuration:", err)
		return
	}
	prevIndex := configFuture.Index()

	wait := r.raft.RemoveServer(
		addr, prevIndex, 30*time.Second,
	)
	if err := wait.Error(); err != nil {
		r.log.Warn("failed to remove raft server:", wait.Error())
	}
	return
}

func (r *raftService) Raft() *Raft {
	r.waitSync()

	return r.raft
}

func (r *raftService) waitSync() {
	r.syncMx.RLock()
	r.syncMx.RUnlock()
}

func (r *raftService) Shutdown() {
	if r == nil || r.raft == nil {
		return
	}

	wait := r.raft.Shutdown()
	if wait != nil && wait.Error() != nil {
		r.log.Error("failed to shutdown raft:", wait.Error())
	}
	r.log.Debug("raft node shut down")
	r.raft = nil
}
