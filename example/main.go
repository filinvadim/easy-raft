package main

import (
	"context"
	eraft "github.com/filinvadim/easy-raft"
	"github.com/hashicorp/go-hclog"
	"log"
	"sync"
	"time"
)

type VoterAdder interface {
	AddVoter(addr string)
	ID() string
}

func main() {
	var (
		node1, node2, node3 VoterAdder
		err                 error
		wg                  = new(sync.WaitGroup)
	)

	wg.Add(3)

	go func() {
		defer wg.Done()

		node1, err = eraft.NewEasyRaft(
			context.Background(),
			"localhost:4011",
			hclog.New(&hclog.LoggerOptions{Level: hclog.Debug, Name: "1"}),
			nil, nil, nil,
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
			hclog.New(&hclog.LoggerOptions{Level: hclog.Debug, Name: "2"}),
			nil, nil, nil,
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
			hclog.New(&hclog.LoggerOptions{Level: hclog.Debug, Name: "3"}),
			nil, nil, nil,
		)
		if err != nil {
			log.Fatal(err)
		}
	}()

	wg.Wait()

	for _, outer := range []VoterAdder{node1, node2, node3} {
		for _, inner := range []VoterAdder{node1, node2, node3} {
			outer.AddVoter(inner.ID())
		}
	}
	time.Sleep(1 * time.Minute)
}
