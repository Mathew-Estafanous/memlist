# Memlist Development Guidelines

This document provides essential information for developers working on the Memlist project.

## Build/Configuration Instructions

### Prerequisites
- Go 1.21 or later

### Building the Project
The project uses standard Go build tools. To build the project:

```bash
go build ./...
```

### Configuration
The `Config` struct in `config.go` provides various configuration options for a Memlist node:

- **Name**: A unique name for the node in the cluster
- **BindAddr/BindPort**: Network address and port for the node to bind to
- **MetaData**: Optional field for storing relevant information about the node
- **PingInterval/PingTimeout**: Controls frequency and timeout of health checks
- **PiggyBackLimit**: Maximum number of gossip events to piggyback on messages
- **IndirectChecks**: Number of nodes to contact for indirect pings
- **Transport**: Optional custom transport implementation
- **TCPTimeout**: Timeout for TCP connection attempts
- **EventListener**: Optional listener for events

For local development, use `DefaultLocalConfig()` as a starting point:

```go
conf := memlist.DefaultLocalConfig()
conf.Name = "my-node"
conf.BindPort = 8080
```

## Testing Information

### Running Tests
To run all tests:

```bash
go test ./...
```

To run specific tests:

```bash
go test -run TestName
```

To run tests with verbose output:

```bash
go test -v ./...
```

### Writing Tests
The project uses the standard Go testing package along with the `github.com/stretchr/testify` package for assertions.

Test files should:
- Be named `*_test.go`
- Be in the same package as the code they test
- Use descriptive test function names prefixed with `Test`
- Include multiple test cases to cover different scenarios
- Clean up resources with defer statements

Example test structure:

```go
func TestSomething(t *testing.T) {
    // Setup
    resource, err := setupResource()
    require.NoError(t, err)
    defer resource.Cleanup()
    
    // Test case
    result := functionUnderTest()
    assert.Equal(t, expectedValue, result)
}
```

For network-related tests, use local addresses and ports, and include timeouts to prevent tests from hanging:

```go
select {
case <-receivedChannel:
    // Test passed
case <-time.After(500 * time.Millisecond):
    t.Fatalf("Did not receive expected response in time")
}
```

## Additional Development Information

### Project Structure
- **member.go**: Core implementation of the membership list and gossip protocol
- **config.go**: Configuration options for the membership list
- **transport.go**: Network transport layer implementation
- **gossip.go**: Implementation of the gossip protocol
- **util.go**: Utility functions used throughout the project

### Code Style
- The project follows standard Go code style and conventions
- Error handling is done by returning errors rather than using panics
- Concurrency is managed using mutexes and channels
- Network operations include timeouts to prevent hanging
- Resources are properly cleaned up using defer statements

### Membership Protocol
The project implements a membership protocol using a gossip-based approach:
- Nodes periodically ping other nodes to check their health
- If a direct ping fails, an indirect ping is attempted through other nodes
- Information about node status is disseminated through gossip messages
- Nodes maintain a list of other nodes in the cluster with their current status

### Example Usage
See `example/main.go` for a simple example of how to use the library:

```go
conf := memlist.DefaultLocalConfig()
conf.Name = "my-node"
conf.BindPort = 8080

mem, err := memlist.Create(conf)
if err != nil {
    log.Fatalln(err)
}

// Join an existing cluster
if err := mem.Join("existing-node-address"); err != nil {
    log.Fatalln(err)
}
```
