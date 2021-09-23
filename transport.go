package memlist

import (
	"fmt"
	"log"
	"net"
	"time"
)

type messageType uint8

const (
	ping messageType = iota
	indirectPing
	ack
	joinSync
)

type pingReq struct {
	ReqNo uint32

	// Node is the Name of the intended recipient Node and is used as a
	// verification for the receiving Node.
	Node string

	// The address and Port of the Node that is sending the sendPing request.
	FromAddr string
	FromPort uint16
}

type indirectPingReq struct {
	ReqNo uint32

	// Node is the Name of the Node that the sendPing is targeted towards.
	Node     string
	NodeAddr string
	NodePort uint16

	// The address and Port of the Node that is sending the sendPing request.
	FromAddr string
	FromPort uint16
}

type ackResp struct {
	ReqNo uint32
}

// Packet represents the incoming packetCh and the peer's associated
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

	// DialAndConnect will create a connection to another peer allowing for a
	// direct two-way connection between both peers.
	DialAndConnect(addr string, timeout time.Duration) (net.Conn, error)

	// Packets returns a channel that is used to receive incoming packets
	// from other peers.
	Packets() <-chan *Packet

	// Stream returns a read only channel that is used to receive incoming
	// streamCh connections from other peers. A streamCh is usually sent during
	// attempts at syncing state between two peers.
	Stream() <-chan net.Conn

	// Shutdown allows for the transport to clean up all listeners safely.
	Shutdown() error
}

// NetTransport is the standard implementation of the Transport and should be enough for
// most use cases.
type NetTransport struct {
	udpCon *net.UDPConn
	tcpLsn *net.TCPListener

	packetCh chan *Packet
	streamCh chan net.Conn
	shutdown chan struct{}
}

// NewNetTransport will create and return a NetTransport that is properly setup with udp
// and tcp listeners.
func NewNetTransport(addr string, port uint16) (*NetTransport, error) {
	ok := true
	t := &NetTransport{
		packetCh: make(chan *Packet),
		streamCh: make(chan net.Conn),
		shutdown: make(chan struct{}),
	}
	defer func() {
		if !ok {
			t.Shutdown()
		}
	}()

	udpAddr := &net.UDPAddr{Port: int(port), IP: net.ParseIP(addr)}
	udpCon, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		ok = false
		return nil, fmt.Errorf("failed to start UDP connection on address %v Port %v: %v", addr, port, err)
	}
	t.udpCon = udpCon

	tcpAddr := &net.TCPAddr{Port: int(port), IP: net.ParseIP(addr)}
	tcpCon, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		ok = false
		return nil, err
	}
	t.tcpLsn = tcpCon

	go t.listenForPacket()
	go t.listenForStream()
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

func (n *NetTransport) DialAndConnect(addr string, timeout time.Duration) (net.Conn, error) {
	d := &net.Dialer{Timeout: timeout}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (n *NetTransport) Packets() <-chan *Packet {
	return n.packetCh
}

func (n *NetTransport) Stream() <-chan net.Conn {
	return n.streamCh
}

func (n *NetTransport) Shutdown() error {
	close(n.shutdown)
	close(n.packetCh)
	return n.udpCon.Close()
}

// listenForPacket will wait for a UDP packet sent to the connection and
// will format it into a Packet and forward it to the packet channel to be handled.
func (n *NetTransport) listenForPacket() {
	for {
		b := make([]byte, 65536)
		i, addr, err := n.udpCon.ReadFrom(b)
		if err != nil {
			select {
			case <-n.shutdown:
				return
			default:
				log.Printf("[ERROR] Failed to read received UDP packetCh: %v", err)
				continue
			}
		}

		if len(b) <= 1 {
			log.Printf("[ERROR] Byte packetCh is too short (%v), must be longer.", len(b))
			continue
		}

		n.packetCh <- &Packet{
			From: addr,
			Buf:  b[:i],
		}
	}
}

// listenForStream will listen for attempts of creating a TCP connection and if
// successful, will forward the new connection through towards the stream.
func (n *NetTransport) listenForStream() {
	for {
		conn, err := n.tcpLsn.AcceptTCP()
		if err != nil {
			select {
			case <-n.shutdown:
				return
			default:
				log.Printf("[ERROR] Failed to accept TCP connection: %v", err)
				continue
			}
		}

		n.streamCh <- conn
	}
}
