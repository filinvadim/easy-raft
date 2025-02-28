# easy-raft

A simple wrapper for HashiCorp's Raft implementation in Go.

## Overview

`easy-raft` provides a straightforward interface to integrate Raft consensus into your Go applications, leveraging HashiCorp's robust Raft library.

## Ratio

It is not easy to use HashiCorp's Raft library without knowing many details related to Raft consensus
algorithm spec. This wrapper is taking away any such overhead providing simple Raft node setup
and access to it interface.

## Features

- Simplified setup and management of Raft nodes
- Easy integration into existing Go projects
- Example usage provided in the `example` directory

## Getting Started

### Prerequisites

- Go 1.21 or later

### Installation

```sh
go get github.com/filinvadim/easy-raft
```

### Usage

```go
package main

import (
	"github.com/filinvadim/easy-raft"
)

func main() {
    // Initialize and start your Raft node
    node, err := easyraft.NewEasyRaft(
        ctx,
        "localhost:6969",
        logger,
        logStore,
        stableStore,
        snapshotStore,
        transport,
        ) // Set your own configuration parameters 
    
    // Handle error
    
    defer node.Shutdown()
    
    raft := node.Raft()
    raft.Apply([]byte("some cmd"), time.Second*5) 	// Your application logic here
}

```

For a complete example, refer to the example directory in the repository.

### Contributing

Contributions are welcome! Please fork the repository and submit a pull request.

### License

This project is licensed under the Apache License.

