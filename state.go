package easy_raft

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/hashicorp/raft"
	"github.com/vmihailenco/msgpack/v5"
	"io"
	"sync"
)

type KVState map[string]string

type fsm struct {
	state     *KVState
	prevState KVState

	mux *sync.Mutex

	validators []ConsensusValidatorFunc
}

type ConsensusValidatorFunc func(k, v string) error

func newFSM(validators ...ConsensusValidatorFunc) *fsm {
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
		if r := recover(); r != nil {
			*fsm.state = fsm.prevState
			result = errors.New("fsm apply panic: rollback")
		}
	}()

	if rlog.Type != raft.LogCommand {
		return nil
	}

	var newState = make(KVState, 1)
	if err := msgpack.Unmarshal(rlog.Data, &newState); err != nil {
		return fmt.Errorf("failed to decode log: %w", err)
	}

	for _, validator := range fsm.validators {
		for k, v := range newState {
			if err := validator(k, v); err != nil {
				return err
			}
		}
	}

	fsm.prevState = make(KVState, len(*fsm.state))
	for k, v := range *fsm.state {
		fsm.prevState[k] = v
	}

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
	err := msgpack.NewEncoder(buf).Encode(fsm.state)
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

	err = msgpack.NewDecoder(reader).Decode(fsm.state)
	if err != nil {
		return err
	}

	fsm.prevState = make(map[string]string, len(*fsm.state))
	return nil
}

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

func (snap *fsmSnapshot) Release() {}
