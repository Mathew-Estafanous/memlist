package memlist

import (
	"os"
	"time"
)

// Config provides different options that can be adjusted according to what is needed of the
// member. Some fields are required and should not be left empty when creating a new member.
type Config struct {
	// A Name for the Node that is unique to the entire cluster.
	Name string

	// Configuration related to which address and Port the Node will
	// bind to and listen on.
	BindAddr string
	BindPort uint16

	// PingInterval is the time between attempts at random pings towards
	// another Node. Decreasing the interval will result in more frequent
	// checks at the cost of increased bandwidth usage.
	PingInterval time.Duration
	// PingTimeout is the timeout a Node will wait for an ACK response from
	// any member before determining that the Node is potentially unhealthy.
	PingTimeout time.Duration

	// PiggyBackLimit is used as the maximum number of gossip events that are added
	// (piggyback) off of a Node's ping/ack messages.
	//
	// Consider a slider when determining the limit. The lower the limit the smaller
	// the messages and latency will decrease; however, gossip events will take
	// longer to disseminate. Opposite, when increasing the limit, messages will
	// be larger and latency increases while gossip will spread exponentially faster.
	//
	// DefaultLocalConfig chooses 4 as an arbitrary limit.
	PiggyBackLimit int

	// IndirectChecks is the number of nodes that will be contacted in the case
	// that an indirect sendPing is required. Increasing the number of checks will
	// also increase the chances of an indirect sendPing succeeding. This is at the
	// expense of bandwidth usage.
	IndirectChecks int

	// Transport is an optional field for the client to define custom
	// communication among other nodes. If this field is left nil, Create will
	// by default use a NetTransport in the Member.
	Transport Transport

	// TCPTimeout is the time in which a TCP connection will be attempted. If no
	// connection is made before reaching the timeout, then the attempt will fail.
	TCPTimeout time.Duration

	// EventListener can be used to inject the client's implementation of the Listener.
	// If nothing is injected, then Create will use a fake listener in its place.
	EventListener Listener
}

// DefaultLocalConfig returns a configuration that is set up for a local environment.
func DefaultLocalConfig() *Config {
	host, _ := os.Hostname()
	return &Config{
		Name:           host,
		BindAddr:       "127.0.0.1",
		BindPort:       7990,
		PingTimeout:    300 * time.Millisecond,
		PingInterval:   1 * time.Second,
		PiggyBackLimit: 4,
		IndirectChecks: 1,
		TCPTimeout:     15 * time.Second,
		EventListener:  &emptyListener{},
	}
}
