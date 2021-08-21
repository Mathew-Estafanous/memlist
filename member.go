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

// Node represents a single node within the cluster and their
// state within the cluster.
type Node struct {
	Name  string
	Addr  net.IP
	Port  uint16
	State StateType
}

type handler func(a ackResp, from net.Addr)

type Member struct {
	name          string
	requestNumber uint32

	conf      *Config
	transport Transport

	ackMu       sync.Mutex
	ackHandlers map[uint32]handler

	nodeMu   sync.Mutex
	nodeMap  map[string]*Node
	numNodes uint32

	shutdownCh chan struct{}
	hasStopped bool
	logger     *log.Logger
}

// Create a new member using the given configuration file. The member
// will start listening for packets that are sent from the Transport.
//
// The Member has not joined any cluster at this point. To join a cluster
// of nodes, look into using Join.
//
// Upon creating a member with the Config, the data must NOT be altered
// and is assumed to remain unchanged throughout the life of the Member.
func Create(conf *Config) (*Member, error) {
	transport := conf.Transport
	if transport == nil {
		t, err := NewNetTransport(conf.BindAddr, conf.BindPort)
		if err != nil {
			// TODO: wrap errors so that the API doesn't expose internal errors.
			return nil, err
		}
		transport = t
	}

	l := log.New(os.Stdout, fmt.Sprintf("[%v]", conf.Name), log.LstdFlags)
	m := &Member{
		conf:       conf,
		transport:  transport,
		nodeMap:    make(map[string]*Node),
		shutdownCh: make(chan struct{}),
		logger:     l,
	}

	go m.packetListen()
	go m.streamListen()
	return m, nil
}

// Join will attempt to join the member into a cluster of nodes by connecting
// to the Node at the given address. An error will be return if the member
// failed to join a cluster.
func (m *Member) Join(addr string) error {
	if m.conf.TCPTimeout == 0 {
		m.conf.TCPTimeout = 10 * time.Second
	}
	conn, err := m.transport.DialAndConnect(addr, m.conf.TCPTimeout)
	if err != nil {
		m.logger.Printf("[ERROR] Failed to connect to host address: %v", err)
		return fmt.Errorf("failed to connect to %s: %v", addr, err)
	}

	if _, err = conn.Write([]byte(byte(joinSync))); err != nil {
		m.logger.Printf("[ERROR] Failed to send sync message to the host address: %v", err)
		return fmt.Errorf("failed to join cluster: %v", err)
	}

	var b []byte
	if _, err = conn.Read(b); err != nil {
		m.logger.Printf("[ERROR] Couldn't receive data from host: %v", err)
		return fmt.Errorf("failed to join cluster: %v", err)
	}

	var peerState map[string]*Node
	dec := gob.NewDecoder(bytes.NewReader(b[1:]))
	if err = dec.Decode(peerState); err != nil {
		m.logger.Printf("[ERROR] Failed to decode received message: %v", err)
		return fmt.Errorf("failed to sync with peer: %v", err)
	}

	m.nodeMu.Lock()
	m.nodeMap = peerState
	m.nodeMu.Unlock()
	go m.runSchedule()
	return nil
}

// AllNodes will return every known Alive node at the time.
func (m *Member) AllNodes() []Node {
	m.nodeMu.Lock()
	defer m.nodeMu.Unlock()
	var nodes []Node
	for _, n := range m.nodeMap {
		if n.State == Alive {
			nodes = append(nodes, *n)
		}
	}
	return nodes
}

// Shutdown will stop all background processes such as responded to received
// packets. No message will be sent regarding leaving the cluster and as a
// result the member will eventually be considered 'dead'.
//
// This method should only be called once. If called more than once, a non-nil
// error will be returned.
//
// If you want your member to notify other members about leaving the cluster
// look into using Leave instead.
func (m *Member) Shutdown() error {
	if m.hasStopped {
		return fmt.Errorf("member has already been shutdown")
	}

	if err := m.transport.Shutdown(); err != nil {
		return err
	}
	close(m.shutdownCh)
	return nil
}

// Leave will safely stop all running processes and will notify other nodes
// that it will be leaving the cluster. This is a blocking operation until
// the member has successfully left the cluster.
func (m *Member) Leave() error {
	return nil
}

func (m *Member) nextReqNum() uint32 {
	return atomic.AddUint32(&m.requestNumber, 1)
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

func (m *Member) streamListen() {
	for {
		select {
		case conn := <-m.transport.Stream():
			m.handleConn(conn)
		case <-m.shutdownCh:
			return
		}
	}
}

func (m *Member) handlePacket(b []byte, from net.Addr) {
	if len(b) <= 1 {
		m.logger.Println("[ERROR] Missing message type in payload.")
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
		m.logger.Println("[ERROR] Invalid message type (%v) is not available.")
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
		m.logger.Println("[WARNING] Received an unexpected sendProbe for Node. %s ")
		return
	}

	ackR := ackResp{ReqNo: p.ReqNo}
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
		ReqNo:    m.nextReqNum(),
		Node:     ind.Node,
		FromAddr: m.conf.BindAddr,
		FromPort: m.conf.BindPort,
	}

	// Setup an ack handler to route the ack response back to Node that sent the indirect sendProbe request.
	ackRespHandler := func(a ackResp, from net.Addr) {
		if a.ReqNo != p.ReqNo {
			log.Printf("[WARNING] Received an ack response with seq. number (%v) when %v is wanted", a.ReqNo, p.ReqNo)
			return
		}
		ackR := ackResp{ReqNo: ind.ReqNo}
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
	m.addAckHandler(ackRespHandler, p.ReqNo, m.conf.ProbeTimeout)

	b := encodeResponse(ping, &p)
	var addr string
	addr = net.JoinHostPort(ind.NodeAddr, strconv.Itoa(int(ind.NodePort)))
	if err := m.transport.SendTo(b, addr); err != nil {
		m.logger.Printf("[ERROR] Encountered an issue when sending sendProbe. %v", err)
	}
}

func (m *Member) handleAck(dec *gob.Decoder, from net.Addr) {
	var a ackResp
	if err := dec.Decode(&a); err != nil {
		log.Printf("[ERROR] Failed to decode byte slice into AckResponse. %v", err)
		return
	}

	m.ackMu.Lock()
	ah, ok := m.ackHandlers[a.ReqNo]
	delete(m.ackHandlers, a.ReqNo)
	if !ok {
		log.Printf("[WARNING] Couldn't find a ah for sequence number %v", a.ReqNo)
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

func (m *Member) runSchedule() {
	for {
		select {
		case <-time.After(m.conf.ProbeInterval):
			m.sendProbe()
		case <-m.shutdownCh:
			return
		}
	}
}

func (m *Member) sendProbe() {
	if m.numNodes == 0 {
		return
	}

	// arbitrarily select a peer Node from the map.
	var key string
	m.nodeMu.Lock()
	for k := range m.nodeMap {
		key = k
		break
	}
	sendNode := m.nodeMap[key]
	m.nodeMu.Unlock()

	// Make ping/probe request and send it to the selected Node.
	p := &pingReq{
		ReqNo:    m.nextReqNum(),
		Node:     sendNode.Name,
		FromPort: m.conf.BindPort,
		FromAddr: m.conf.BindAddr,
	}
	b := encodeResponse(ping, p)
	addr := net.JoinHostPort(sendNode.Addr.String(), strconv.Itoa(int(sendNode.Port)))
	if err := m.transport.SendTo(b, addr); err != nil {
		log.Printf("[ERROR] Failed to send initial ping to Node %v: %v", sendNode.Name, err)
	}

	// Add handler that closes response channel if an "ack" is returned in time.
	responded := make(chan bool)
	h := func(a ackResp, _ net.Addr) {
		if a.ReqNo != p.ReqNo {
			log.Printf("[WARNING] Received wrong ack response with seq. number (%v) instead of %v", a.ReqNo, p.ReqNo)
			return
		}
		close(responded)
	}
	m.addAckHandler(h, p.ReqNo, m.conf.ProbeTimeout)

	// Wait for "ack" response until timeout. In which we switch making an indirect probe.
	select {
	case <-time.After(m.conf.ProbeTimeout):
		m.sendIndirectProbe(sendNode)
	case <-responded:
		sendNode.State = Alive
		return
	}
}

func (m *Member) sendIndirectProbe(send *Node) {
	var nodes []*Node
	m.nodeMu.Lock()
	// randomly select other nodes to ask for indirect probes
	for k, v := range m.nodeMap {
		if k == send.Name {
			continue
		}

		nodes = append(nodes, v)
		if len(nodes) >= m.conf.IndirectChecks {
			break
		}
	}
	m.nodeMu.Unlock()

	responded := make(chan bool)
	for _, n := range nodes {
		indPing := &indirectPingReq{
			ReqNo:    m.nextReqNum(),
			Node:     send.Name,
			NodeAddr: send.Addr.String(),
			NodePort: send.Port,
			FromPort: m.conf.BindPort,
			FromAddr: m.conf.BindAddr,
		}
		b := encodeResponse(indirectPing, indPing)
		addr := net.JoinHostPort(m.conf.BindAddr, strconv.Itoa(int(m.conf.BindPort)))
		if err := m.transport.SendTo(b, addr); err != nil {
			log.Printf("[ERROR] Failed to send initial ping to Node %v: %v", n.Name, err)
		}

		h := func(a ackResp, from net.Addr) {
			if a.ReqNo != indPing.ReqNo {
				log.Printf("[WARNING] Received wrong ack response with seq. number (%v) instead of %v", a.ReqNo, indPing.ReqNo)
				return
			}
			responded <- true
		}
		m.addAckHandler(h, indPing.ReqNo, m.conf.ProbeInterval)
	}

	select {
	case <-responded:
		return
	case <-time.After(m.conf.ProbeInterval - m.conf.ProbeTimeout):
		log.Printf("[CHANGE] Node %v has failed to respond and is now considered Dead.", send.Name)
		// TODO: Remove node from internal member list into a different list of dead members.
		send.State = Dead
	}
}

func (m *Member) handleConn(conn net.Conn) {
	var b []byte
	if _, err := conn.Read(b); err != nil {
		m.logger.Println("[INFO] ", err)
		return
	}

	if len(b) < 1 {
		m.logger.Printf("[ERROR] Byte message is too short (%v), it must include message type", len(b))
		return
	}

	switch messageType(b[0]) {
	case joinSync:
		dec := gob.NewDecoder(bytes.NewReader(b[1:]))
		var joiningPeer Node
		if err := dec.Decode(&joiningPeer); err != nil {
			m.logger.Printf("[ERROR] Failed to decode message from joining peer: %v", err)
			return
		}

		b := encodeResponse(joinSync, m.nodeMap)
		if _, err := conn.Write(b); err != nil {
			m.logger.Printf("[ERROR] Failed to send response with current state: %v", err)
			return
		}

		m.nodeMu.Lock()
		m.nodeMap[joiningPeer.Name] = &joiningPeer
		m.nodeMu.Unlock()
	default:
		m.logger.Printf("[ERROR] Received message type %v which is not a valid option.", b[0])
		return
	}
}

func encodeResponse(tp messageType, e interface{}) []byte {
	buf := bytes.NewBuffer([]byte{uint8(tp)})
	enc := gob.NewEncoder(buf)
	enc.Encode(e)
	return buf.Bytes()
}
