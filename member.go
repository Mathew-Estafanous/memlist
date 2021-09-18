package memlist

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"fmt"
	"github.com/google/btree"
	"io"
	"log"
	"math/rand"
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
)

const packetDelim = '$'

// Node represents a single node within the cluster and their
// state within the cluster.
type Node struct {
	Name  string
	Addr  string
	Port  uint16
	State StateType
}

func (n *Node) String() string {
	return fmt.Sprintf("[%s | %s]", n.Name, n.Addr)
}

type handler func(a ackResp, from net.Addr)

type Member struct {
	requestNumber uint32

	conf       *Config
	transport  Transport
	listener   Listener
	eventQueue *GossipEventQueue

	ackMu       sync.Mutex
	ackHandlers map[uint32]handler

	nodeMu     sync.Mutex
	nodeMap    map[string]*Node
	aliveNodes uint32
	probeList  []string
	probeIdx   int

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

	if conf.EventListener == nil {
		conf.EventListener = &emptyListener{}
	}

	l := log.New(os.Stdout, fmt.Sprintf("[%v] ", conf.Name), log.LstdFlags)
	m := &Member{
		conf:        conf,
		transport:   transport,
		listener:    conf.EventListener,
		nodeMap:     make(map[string]*Node),
		probeList:   make([]string, 0),
		ackHandlers: make(map[uint32]handler),
		shutdownCh:  make(chan struct{}),
		logger:      l,
	}

	m.eventQueue = &GossipEventQueue{
		numNodes: m.TotalNodes,
		bt:       btree.New(2),
	}

	go m.packetListen()
	go m.streamListen()
	go m.runSchedule()
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

	n := Node{
		Name:  m.conf.Name,
		Addr:  m.conf.BindAddr,
		Port:  m.conf.BindPort,
		State: Alive,
	}
	if _, err = conn.Write(encodeMessage(joinSync, &n)); err != nil {
		m.logger.Printf("[ERROR] Failed to send sync message to the host address: %v", err)
		return fmt.Errorf("failed to join cluster: %v", err)
	}

	var peerState map[string]Node
	dec := gob.NewDecoder(conn)
	if err = dec.Decode(&peerState); err != nil {
		m.logger.Printf("[ERROR] Failed to decode received message: %v", err)
		return fmt.Errorf("failed to sync with peer: %v", err)
	}

	for _, v := range peerState {
		n := v
		m.addNewNode(&n)
	}

	m.hasStopped = false
	return nil
}

// AllNodes will return every known alive node at the time.
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

// TotalNodes returns the total number of known peers that are alive.
func (m *Member) TotalNodes() int {
	m.nodeMu.Lock()
	m.nodeMu.Unlock()
	return int(m.aliveNodes)
}

// Shutdown will stop all background processes such as responding to received
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

	close(m.shutdownCh)
	if err := m.transport.Shutdown(); err != nil {
		return err
	}
	m.hasStopped = true
	return nil
}

// Leave will safely stop all running processes and will notify other nodes
// that it will be leaving the cluster. This is a blocking operation until
// the member has successfully left the cluster or the timeout has been reached.
func (m *Member) Leave(timeout time.Duration) error {
	if m.hasStopped {
		return fmt.Errorf("member has already stopped")
	}

	leaveGossip := Gossip{
		Gt: leave,
		Node: Node{
			Name:  m.conf.Name,
			Addr:  m.conf.BindAddr,
			Port:  m.conf.BindPort,
			State: Alive,
		},
	}
	broadcast := make(chan struct{})
	m.eventQueue.QueueWithBroadcast(leaveGossip, broadcast)
	select {
	case <-broadcast:
	case <-time.After(timeout):
		return fmt.Errorf("timed out while waiting for leave broadcast")
	}
	close(m.shutdownCh)
	m.logger.Println("[CHANGE] Node has successfully left the cluster.")
	return nil
}

// nextReqNum increments the request number in a thread safe manner, returning
// the resulting number after incrementing.
func (m *Member) nextReqNum() uint32 {
	return atomic.AddUint32(&m.requestNumber, 1)
}

// packetListen listens for packets sent from the Transport layer and hands it
// down to be properly handled.
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

// streamListen listens for an attempted stream connection sent by the Transport,
// sending the given connection to be properly handled.
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
	reader := bufio.NewReader(bytes.NewReader(b))
	msgB, err := reader.ReadBytes(packetDelim)
	if err != nil {
		m.logger.Printf("[ERROR] Failed to read the message until ending delim (%v): %v", packetDelim, err)
		return
	}

	if len(b) <= 1 {
		m.logger.Println("[ERROR] Missing message type in payload.")
		return
	}

	dec := gob.NewDecoder(bytes.NewReader(msgB[1 : len(msgB)-1]))
	switch messageType(msgB[0]) {
	case ping:
		m.handlePing(dec, from)
		gossipB, err := io.ReadAll(reader)
		if err != nil && err != io.EOF {
			m.logger.Printf("[ERROR] Failed to read gossip bytes from packet: %v", err)
			return
		}
		m.handleGossips(gossipB)
	case indirectPing:
		m.handleIndirectPing(dec, from)
	case ack:
		m.handleAck(dec, from)
	default:
		m.logger.Printf("[ERROR] Invalid message type (%v) is not available.", msgB[0])
		return
	}

}

func (m *Member) handlePing(dec *gob.Decoder, from net.Addr) {
	var p pingReq
	if err := dec.Decode(&p); err != nil {
		log.Printf("[ERROR] Failed to decode byte slice from (%v) into PingReq. %v", from.String(), err)
		return
	}

	if p.Node != m.conf.Name {
		m.logger.Printf("[WARNING] Received an unexpected sendProbe for Node: %s", p.Node)
		return
	}

	ackR := ackResp{ReqNo: p.ReqNo}
	b := encodeMessage(ack, &ackR)
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

func (m *Member) handleIndirectPing(dec *gob.Decoder, _ net.Addr) {
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

	// Set up an ack handler to route the ack response back to Node that sent the indirect sendProbe request.
	ackRespHandler := func(a ackResp, from net.Addr) {
		if a.ReqNo != p.ReqNo {
			log.Printf("[WARNING] Received an ack response with seq. number (%v) when %v is wanted", a.ReqNo, p.ReqNo)
			return
		}
		ackR := ackResp{ReqNo: ind.ReqNo}
		b := encodeMessage(ack, &ackR)

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

	b := encodeMessage(ping, &p)
	b = m.piggyBackGossip(b)
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
	m.nodeMu.Lock()
	if m.aliveNodes == 0 {
		m.logger.Println("[INFO] There are no known alive nodes to send a probe to.")
		return
	}

	// get the next peer to send probe to, depending on the list. Ensuring every peer is
	// sent a probe periodically, guaranteeing that all peers will be reached at some point.
	if m.probeIdx >= len(m.probeList) {
		m.probeIdx = 0
	}
	name := m.probeList[m.probeIdx]
	sendNode := m.nodeMap[name]
	m.probeIdx++
	m.nodeMu.Unlock()

	// Make ping/probe request and send it to the selected Node.
	p := &pingReq{
		ReqNo:    m.nextReqNum(),
		Node:     sendNode.Name,
		FromPort: m.conf.BindPort,
		FromAddr: m.conf.BindAddr,
	}
	b := encodeMessage(ping, p)
	b = m.piggyBackGossip(b)

	addr := net.JoinHostPort(sendNode.Addr, strconv.Itoa(int(sendNode.Port)))
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
		m.logger.Printf("[INFO] Sending indirect probe to %v.", sendNode.Name)
		m.sendIndirectProbe(sendNode)
	case <-responded:
		sendNode.State = Alive
		return
	case <-m.shutdownCh:
		return
	}
}

func (m *Member) sendIndirectProbe(send *Node) {
	var nodes []*Node
	m.nodeMu.Lock()
	if m.aliveNodes <= 1 {
		m.logger.Println("[INFO] There aren't enough nodes to send indirect probes to.")
		return
	}

	// randomly select other nodes to ask for indirect probes
	for k, v := range m.nodeMap {
		if k == send.Name && v.State != Dead {
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
			NodeAddr: send.Addr,
			NodePort: send.Port,
			FromPort: m.conf.BindPort,
			FromAddr: m.conf.BindAddr,
		}
		b := encodeMessage(indirectPing, indPing)
		addr := net.JoinHostPort(n.Addr, strconv.Itoa(int(n.Port)))
		if err := m.transport.SendTo(b, addr); err != nil {
			m.logger.Printf("[ERROR] Failed to send indirect probe to Node %v: %v", n.Name, err)
		}

		h := func(a ackResp, from net.Addr) {
			if a.ReqNo != indPing.ReqNo {
				m.logger.Printf("[WARNING] Received wrong ack response with seq. number (%v) instead of %v", a.ReqNo, indPing.ReqNo)
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
		m.logger.Printf("[CHANGE] Node %v has failed to respond and is now considered Dead.", send.Name)
		m.setDeadNode(send)
		deadGossip := Gossip{
			Gt:   dead,
			Node: *send,
		}
		m.eventQueue.Queue(deadGossip)
	case <-m.shutdownCh:
		return
	}
}

// handleConn will use the provided connection and do the appropriate operations
// depending on the message type.
func (m *Member) handleConn(conn net.Conn) {
	msgT := make([]byte, 1)
	if _, err := io.ReadAtLeast(conn, msgT, 1); err != nil {
		m.logger.Printf("[ERROR] Failed to read from connection: %v", err)
		return
	}

	switch messageType(msgT[0]) {
	case joinSync:
		dec := gob.NewDecoder(conn)
		joiningPeer := &Node{}
		if err := dec.Decode(joiningPeer); err != nil {
			m.logger.Printf("[ERROR] Failed to decode message from joining peer: %v", err)
			return
		}

		m.nodeMu.Lock()
		// Add self in node map since the peer will need to add this member as well as all others.
		m.nodeMap[m.conf.Name] = &Node{
			Name: m.conf.Name,
			Addr: m.conf.BindAddr,
			Port: m.conf.BindPort,
		}
		b := encodeMessage(joinSync, m.nodeMap)
		// remove self from the map, since we now created the message.
		delete(m.nodeMap, m.conf.Name)
		if _, err := conn.Write(b[1:]); err != nil {
			m.logger.Printf("[ERROR] Failed to send response with current state: %v", err)
			return
		}
		m.nodeMu.Unlock()

		// add the new peer that has joined the cluster to own map.
		m.addNewNode(joiningPeer)

		// create a new gossip that then is added to the event queue to be disseminated.
		gossip := Gossip{
			Gt:   join,
			Node: *joiningPeer,
		}
		m.eventQueue.Queue(gossip)
		m.logger.Printf("[CHANGE] Node Joined: %v", m.probeList)
	default:
		m.logger.Printf("[ERROR] Received message type %v which is not a valid option.", msgT[0])
		return
	}
}

// addNewNode will add the node to the map and return true as long as there are no
// duplicates found. Otherwise, the result will be false.
func (m *Member) addNewNode(n *Node) bool {
	m.nodeMu.Lock()
	defer m.nodeMu.Unlock()
	if _, ok := m.nodeMap[n.Name]; ok {
		return false
	}

	m.nodeMap[n.Name] = n
	m.aliveNodes++

	if len(m.probeList) == 0 {
		m.probeList = insert(m.probeList, 0, n.Name)
	} else {
		// randomly insert new node into probe list.
		m.probeList = insert(m.probeList, rand.Intn(len(m.probeList)), n.Name)
	}
	return true
}

// setDeadNode will remove the node from the probeList as long as it is found. If
// no matching node is found, then the result will be false. Otherwise, the response
// will be true.
func (m *Member) setDeadNode(n *Node) bool {
	m.nodeMu.Lock()
	defer m.nodeMu.Unlock()
	n.State = Dead
	// node should no longer be part of the probe list since it is considered dead.
	for i, v := range m.probeList {
		if v == n.Name {
			m.probeList = remove(m.probeList, i)
			m.aliveNodes--
			return true
		}
	}
	return false
}

// removeNode will remove the node entirely, as if the node is no-longer part of the
// cluster. If the node was successfully removed then the result is true.
func (m *Member) removeNode(n *Node) bool {
	m.nodeMu.Lock()
	defer m.nodeMu.Unlock()
	for i, v := range m.probeList {
		if v == n.Name {
			m.probeList = remove(m.probeList, i)
			m.aliveNodes--
		}
	}

	if _, ok := m.nodeMap[n.Name]; ok {
		delete(m.nodeMap, n.Name)
		return true
	}
	return false
}

func (m *Member) handleGossips(b []byte) {
	gossipEvents := make([]*GossipEvent, 0)
	dec := gob.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&gossipEvents); err != nil {
		m.logger.Printf("[ERROR] Failed to parse gossip events: %v", err)
		return
	}

	// handle the gossip event depending on the type of event it is.
	for _, g := range gossipEvents {
		if g.Gossip.Node.Name == m.conf.Name {
			// TODO: Handle gossips that refer to self.
			continue
		}

		var success bool
		switch g.Gossip.Gt {
		case join:
			success = m.addNewNode(&g.Gossip.Node)
		case leave:
			success = m.removeNode(&g.Gossip.Node)
			if success {
				m.logger.Printf("[CHANGE] Node %v has left the cluster.", g.Gossip.Node.Name)
			}
		case dead:
			success = m.setDeadNode(&g.Gossip.Node)
			if success {
				m.logger.Printf("[WARNING] Node %v has failed and is considered dead.", g.Gossip.Node.Name)
			}
		}

		if success {
			m.eventQueue.Queue(g.Gossip)
		}
	}
}

// piggyBackGossip will append a byte slice containing data regarding the
// gossip events in the queue and return the resulting complete slice.
func (m *Member) piggyBackGossip(b []byte) []byte {
	buff, err := m.eventQueue.GetGossipEvents(gossipLimit)
	if err != nil {
		m.logger.Printf("[WARNING] Failed to get byte-slice representation of gossip: %v", err)
	} else {
		b = append(b, buff...)
	}
	return b
}

func remove(s []string, i int) []string {
	s[i] = s[len(s)-1]
	return s[:len(s)-1]
}

func insert(a []string, index int, value string) []string {
	if len(a) == index { // nil or empty slice or after last element
		return append(a, value)
	}
	a = append(a[:index+1], a[index:]...) // index < len(a)
	a[index] = value
	return a
}

// encodeMessage will encode the provided type into a byte slice and appends the
// message type to the front of the byte slice.
func encodeMessage(tp messageType, e interface{}) []byte {
	buf := bytes.NewBuffer([]byte{uint8(tp)})
	enc := gob.NewEncoder(buf)
	enc.Encode(e)
	buf.Write([]byte{packetDelim})
	return buf.Bytes()
}
