package main

import (
	"context"
	eraft "github.com/filinvadim/easy-raft"
	"github.com/hashicorp/go-hclog"
	"log"
	"sync"
	"time"
)

type RaftVoter interface {
	AddVoter(addr eraft.ServerID)
	ID() eraft.ServerID
	Shutdown()
	Raft() *eraft.Raft
}

func main() {
	var (
		node1, node2, node3 RaftVoter
		err                 error
		wg                  = new(sync.WaitGroup)
	)

	defer func() {
		node1.Shutdown()
		node2.Shutdown()
		node3.Shutdown()
	}()

	wg.Add(3)

	go func() {
		defer wg.Done()
		node1, err = eraft.NewEasyRaft(
			context.Background(),
			"localhost:4011",
			hclog.New(&hclog.LoggerOptions{Level: hclog.Debug, Name: "node1"}),
			nil, nil, nil, nil,
		)
		if err != nil {
			log.Fatal(err)
		}
	}()

	go func() {
		defer wg.Done()
		node2, err = eraft.NewEasyRaft(
			context.Background(),
			"localhost:4012",
			hclog.New(&hclog.LoggerOptions{Level: hclog.Debug, Name: "node2"}),
			nil, nil, nil, nil,
		)
		if err != nil {
			log.Fatal(err)
		}
	}()

	go func() {
		defer wg.Done()

		node3, err = eraft.NewEasyRaft(
			context.Background(),
			"localhost:4013",
			hclog.New(&hclog.LoggerOptions{Level: hclog.Debug, Name: "node3"}),
			nil, nil, nil, nil,
		)
		if err != nil {
			log.Fatal(err)
		}
	}()

	wg.Wait()

	for _, outer := range []RaftVoter{node1, node2, node3} {
		for _, inner := range []RaftVoter{node1, node2, node3} {
			outer.AddVoter(inner.ID())
		}
	}

	// let's take first node as an example
	if _, leaderID := node1.Raft().LeaderWithID(); node1.ID() == leaderID {
		// node1 is leader! now it can apply new state
		wait := node1.Raft().Apply([]byte("some cmd"), time.Second*5)
		if wait.Error() != nil {
			log.Println("ERROR:", wait.Error())
		}
		log.Println(wait.Response())
	}

	select {}
}
