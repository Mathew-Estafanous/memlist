package memlist

import (
	"os"
	"time"
)

type Config struct {
	// A Name for the Node that is unique to the entire cluster.
	Name string

	// Configuration related to which address and Port the Node will
	// bind to and listen on.
	BindAddr string
	BindPort uint16

	// ProbeInterval is the time between attempts at random probes towards
	// another Node. Decreasing the interval will result in more frequent
	// checks at the cost of increased bandwidth usage.
	//
	// ProbeTimeout is the timeout a Node will wait for an ACK response from
	// any member before determining that the Node is potentially unhealthy.
	ProbeInterval time.Duration
	ProbeTimeout  time.Duration

	// IndirectChecks is the number of nodes that will be contacted in the case
	// that an indirect sendProbe is required. Increasing the number of checks will
	// also increase the chances of an indirect sendProbe succeeding. This is at the
	// expense of bandwidth usage.
	IndirectChecks int

	// Transport is an optional field for the client to define custom
	// communication among other nodes. If this field is left nil, Create will
	// by default use a NetTransport in the Member.
	Transport Transport
}

// DefaultLocalConfig returns a configuration that is set up for a local environment.
func DefaultLocalConfig() *Config {
	host, _ := os.Hostname()
	return &Config{
		Name:           host,
		BindAddr:       "",
		BindPort:       7990,
		ProbeTimeout:   200 * time.Millisecond,
		ProbeInterval:  1 * time.Second,
		IndirectChecks: 1,
	}
}
