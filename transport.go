package memlist

import (
	"fmt"
	"net"
)

type messageType uint8

const (
	ping messageType = iota
	indirectPing
	ack
)

func logFromAddr(from net.Addr) string {
	return fmt.Sprintf("(from = %s)", from.String())
}

// Packet represents the incoming packet and the peer's associated
// data including the message payload.
type Packet struct {
	// Buf is the raw content of the payload.
	Buf []byte

	// From exposes the peer's (sender) address.
	From net.Addr
}

// Transport is an interface designed to abstract away the communication
// details among the member nodes.
type Transport interface {
	// SendTo will forward the provided byte payload to the given address.
	// This message is expected to be done in a connectionless manner, meaning
	// a response is not guaranteed when the method returns.
	SendTo(b []byte, addr string) error

	// Packets returns a channel that is used to receive incoming packets
	// from other peers.
	Packets() chan *Packet

	// Shutdown allows for the transport to clean up all listeners safely.
	Shutdown() error
}
