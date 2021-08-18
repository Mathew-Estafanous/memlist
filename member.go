package memlist

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
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

type handler func(a ackResp, from net.Addr)

type Member struct {
	sequenceNum uint32
	conf        *Config
	transport   Transport

	ackMu       sync.Mutex
	ackHandlers map[uint32]handler

	nodeMap  map[string]*node
	numNodes uint32

	shutdownCh chan struct{}
	logger     *log.Logger
}

func Create(conf *Config) (*Member, error) {
	transport := conf.Transport
	if transport == nil {
		t, err := NewNetTransport(conf.BindAddr, conf.BindPort)
		if err != nil {
			return nil, err
		}
		transport = t
	}

	l := log.New(os.Stdout, fmt.Sprintf("[%v]", conf.Name), log.LstdFlags)
	mem := &Member{
		conf:       conf,
		transport:  transport,
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
		m.handlePing(dec, from)
	case indirectPing:
		m.handleIndirectPing(dec, from)
	case ack:
		m.handleAck(dec, from)
	default:
		m.logger.Printf("[ERROR] Invalid message type (%v) is not available. %s", logFromAddr(from))
		return
	}
}

func (m *Member) handlePing(dec *gob.Decoder, from net.Addr) {
	var p pingReq
	if err := dec.Decode(&p); err != nil {
		log.Printf("[ERROR] Failed to decode byte slice into PingReq. %v", err)
		return
	}

	if p.Node != m.conf.Name {
		m.logger.Println("[WARNING] Received an unexpected ping for node. %s ")
		return
	}

	ackR := ackResp{SeqNo: p.SeqNo}
	b := encodeResponse(ack, &ackR)
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
}

func (m *Member) handleIndirectPing(dec *gob.Decoder, from net.Addr) {
	var ind indirectPingReq
	if err := dec.Decode(&ind); err != nil {
		log.Printf("[ERROR] Failed to decode byte slice into IndirectPingReq. %v", err)
		return
	}

	p := &pingReq{
		SeqNo:    m.nextSeqNum(),
		Node:     ind.Node,
		FromAddr: m.conf.BindAddr,
		FromPort: m.conf.BindPort,
	}

	// Setup an ack handler to route the ack response back to node that sent the indirect ping request.
	ackRespHandler := func(a ackResp, from net.Addr) {
		if a.SeqNo != p.SeqNo {
			log.Printf("[WARNING] Received an ack response with seq. number (%v) when %v is wanted", a.SeqNo, p.SeqNo)
			return
		}
		ackR := ackResp{SeqNo: ind.SeqNo}
		b := encodeResponse(ack, &ackR)

		var addr string
		if ind.FromPort > 0 && ind.FromAddr != "" {
			addr = net.JoinHostPort(ind.FromAddr, strconv.Itoa(int(ind.FromPort)))
		} else {
			addr = from.String()
		}

		if err := m.transport.SendTo(b, addr); err != nil {
			m.logger.Printf("[ERROR] Encountered an issue when sending ack response. %v", err)
		}
	}
	// Add the handler so that it gets called
	m.addAckHandler(ackRespHandler, p.SeqNo, m.conf.ProbeTimeout)

	b := encodeResponse(ping, &p)
	var addr string
	addr = net.JoinHostPort(ind.NodeAddr, strconv.Itoa(int(ind.NodePort)))
	if err := m.transport.SendTo(b, addr); err != nil {
		m.logger.Printf("[ERROR] Encountered an issue when sending ping. %v", err)
	}
}

func (m *Member) handleAck(dec *gob.Decoder, from net.Addr) {
	var a ackResp
	if err := dec.Decode(&a); err != nil {
		log.Printf("[ERROR] Failed to decode byte slice into AckResponse. %v", err)
		return
	}

	m.ackMu.Lock()
	ah, ok := m.ackHandlers[a.SeqNo]
	delete(m.ackHandlers, a.SeqNo)
	if !ok {
		log.Printf("[WARNING] Couldn't find a ah for sequence number %v", a.SeqNo)
		return
	}
	ah(a, from)
	m.ackMu.Unlock()
}

// addAckHandler is used to attach the provided handler with a specific ack response. When an
// ack message with the same sequence number is received, the handler will be called.
func (m *Member) addAckHandler(h handler, seqNo uint32, timeout time.Duration) {
	m.ackMu.Lock()
	defer m.ackMu.Unlock()
	m.ackHandlers[seqNo] = h

	// Delete ack handler after specific timeout to prevent growth in map.
	time.AfterFunc(timeout, func() {
		m.ackMu.Lock()
		defer m.ackMu.Unlock()
		delete(m.ackHandlers, seqNo)
	})
}

func encodeResponse(tp messageType, e interface{}) []byte {
	buf := bytes.NewBuffer([]byte{uint8(tp)})
	enc := gob.NewEncoder(buf)
	enc.Encode(e)
	return buf.Bytes()
}

func logFromAddr(from net.Addr) string {
	return fmt.Sprintf("(from = %s)", from.String())
}
