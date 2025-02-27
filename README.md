# easy-raft

A simple wrapper for HashiCorp's Raft implementation in Go.

## Overview

`easy-raft` provides a straightforward interface to integrate Raft consensus into your Go applications, leveraging HashiCorp's robust Raft library.

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
	node, err := easyraft.NewNode(// Configuration parameters)
	if err != nil {
		// Handle error
	}
	defer node.Shutdown()

	node.Raft()
	// Your application logic here
}

```

For a complete example, refer to the example directory in the repository.
Contributing

Contributions are welcome! Please fork the repository and submit a pull request.

### License

This project is licensed under the Apache License.

