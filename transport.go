package memlist

import (
	"fmt"
	"log"
	"net"
)

type messageType uint8

const (
	ping messageType = iota
	indirectPing
	ack
)

type pingReq struct {
	SeqNo uint32

	// Node is the name of the intended recipient node and is used as a
	// verification for the receiving node.
	Node string

	// The address and port of the node that is sending the ping request.
	FromAddr string
	FromPort uint16
}

type indirectPingReq struct {
	SeqNo uint32

	// Node is the name of the node that the ping is targeted towards.
	Node     string
	NodeAddr string
	NodePort uint16

	// The address and port of the node that is sending the ping request.
	FromAddr string
	FromPort uint16
}

type ackResp struct {
	SeqNo uint32
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
	Packets() <-chan *Packet

	// Shutdown allows for the transport to clean up all listeners safely.
	Shutdown() error
}

type NetTransport struct {
	udpCon   *net.UDPConn
	packet   chan *Packet
	shutdown chan struct{}
}

func NewNetTransport(addr string, port uint16) (*NetTransport, error) {
	udpAddr := &net.UDPAddr{
		Port: int(port),
		IP:   net.ParseIP(addr),
	}

	var ok bool
	t := &NetTransport{
		packet:   make(chan *Packet),
		shutdown: make(chan struct{}),
	}
	defer func() {
		if !ok {
			t.Shutdown()
		}
	}()

	udpCon, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to start UDP connection on address %v port %v: %v", addr, port, err)
	}
	t.udpCon = udpCon

	go t.listenForPacket()
	return t, nil
}

func (n *NetTransport) SendTo(b []byte, addr string) error {
	add, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}

	if _, err = n.udpCon.WriteTo(b, add); err != nil {
		return err
	}
	return nil
}

func (n *NetTransport) Packets() <-chan *Packet {
	return n.packet
}

func (n *NetTransport) Shutdown() error {
	close(n.shutdown)
	close(n.packet)
	return n.udpCon.Close()
}

func (n *NetTransport) listenForPacket() {
	for {
		var b []byte
		_, addr, err := n.udpCon.ReadFromUDP(b)
		if err != nil {
			select {
			case <-n.shutdown:
				break
			default:
				log.Printf("[ERROR] Failed to read received UDP packet: %v", err)
				continue
			}
		}

		if len(b) <= 1 {
			log.Printf("[ERROR] Byte packet is too short (%v), must be longer.", len(b))
			continue
		}

		n.packet <- &Packet{
			From: addr,
			Buf:  b,
		}
	}
}
