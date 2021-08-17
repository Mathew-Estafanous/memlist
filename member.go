package memlist

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"sync/atomic"
)

type StateType int

const (
	Alive StateType = iota
	Dead
	Left
)

// node represents a single node within the cluster.
type node struct {
	name  string
	addr  net.IP
	port  uint16
	state StateType
}

type Member struct {
	sequenceNum uint32
	conf        *Config
	transport   Transport

	nodeMap  map[string]*node
	numNodes uint32

	shutdownCh chan struct{}
	logger     *log.Logger
}

func Create(conf *Config) (*Member, error) {
	if conf.Transport == nil {
		// TODO: Create the default transport.
	}

	l := log.New(os.Stdout, fmt.Sprintf("[%v]", conf.Name), log.LstdFlags)
	mem := &Member{
		conf:       conf,
		transport:  conf.Transport,
		nodeMap:    make(map[string]*node),
		shutdownCh: make(chan struct{}),
		logger:     l,
	}

	go mem.packetListen()
	return mem, nil
}

func (m *Member) nextSeqNum() uint32 {
	return atomic.AddUint32(&m.sequenceNum, 1)
}

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

func (m *Member) packetListen() {
	for {
		select {
		case p := <-m.transport.Packets():
			m.handlePacket(p.Buf, p.From)
		case <-m.shutdownCh:
			return
		}
	}
}

func (m *Member) handlePacket(b []byte, from net.Addr) {
	if len(b) <= 1 {
		m.logger.Printf("[ERROR] Missing message type in payload. %s", logFromAddr(from))
		return
	}

	dec := gob.NewDecoder(bytes.NewReader(b[1:]))
	switch messageType(b[0]) {
	case ping:
		var p pingReq
		dec.Decode(&p)

		if p.Node != m.conf.Name {
			m.logger.Println("[WARNING] Received an unexpected ping for node. %s ")
			return
		}

		ackR := ackResp{SeqNo: p.SeqNo}
		b = encodeResponse(ack, &ackR)
		var addr string
		if p.FromPort > 0 && p.FromAddr != "" {
			addr = net.JoinHostPort(p.FromAddr, strconv.Itoa(int(p.FromPort)))
		} else {
			addr = from.String()
		}

		// send ack response to address with the payload.
		if err := m.transport.SendTo(b, addr); err != nil {
			m.logger.Printf("[ERROR] Encountered an issue when sending ack response. %v", err)
		}
	case indirectPing:
		var ind indirectPingReq
		dec.Decode(&ind)

		p := &pingReq{
			SeqNo:    m.nextSeqNum(),
			Node:     ind.Node,
			FromAddr: m.conf.BindAddr,
			FromPort: m.conf.BindPort,
		}

		b = encodeResponse(ping, &p)
		var addr string
		addr = net.JoinHostPort(ind.NodeAddr, strconv.Itoa(int(ind.NodePort)))
		if err := m.transport.SendTo(b, addr); err != nil {
			m.logger.Printf("[ERROR] Encountered an issue when sending ping. %v", err)
		}
	case ack:

	default:
		m.logger.Printf("[ERROR] Invalid message type (%v) is not available. %s", logFromAddr(from))
		return
	}
}

func encodeResponse(tp messageType, e interface{}) []byte {
	buf := bytes.NewBuffer([]byte{uint8(tp)})
	enc := gob.NewEncoder(buf)
	enc.Encode(e)
	return buf.Bytes()
}
