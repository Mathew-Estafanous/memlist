package memlist

import (
	"bytes"
	"encoding/gob"
	"github.com/google/btree"
	"math"
	"sync"
)

type gossipType uint8

const (
	join gossipType = iota
	leave
	dead
)

const gossipLimit = 3

// Gossip represents a message that is sent to peers regarding changes
// in the state of the cluster and other nodes.
type Gossip struct {
	Gt gossipType

	// Node is the peer that the Gossip refers to.
	Node Node
}

func (g Gossip) invalidates(other Gossip) bool {
	return g.Node.Name == other.Node.Name
}

type GossipEvent struct {
	// The Gossip that this event relates to.
	Gossip Gossip
	// Transmit is the number of times this event has been transmitted.
	Transmit int
}

func (g *GossipEvent) Less(i btree.Item) bool {
	o := i.(*GossipEvent)
	if g.Transmit < o.Transmit {
		return true
	} else if g.Transmit > o.Transmit {
		return false
	}

	if g.Gossip.invalidates(o.Gossip) {
		return false
	}
	return g.Gossip.Node.Name < o.Gossip.Node.Name
}

type GossipEventQueue struct {
	numNodes func() int

	mu sync.Mutex
	// used as a queue that prioritize the newest events.
	bt *btree.BTree
}

func (q *GossipEventQueue) Queue(g Gossip) {
	ge := &GossipEvent{
		Gossip:   g,
		Transmit: 0,
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	var remove []*GossipEvent
	q.bt.Ascend(func(i btree.Item) bool {
		o := i.(*GossipEvent)
		if o.Gossip.invalidates(ge.Gossip) {
			remove = append(remove, o)
			return false
		}
		return true
	})

	for _, e := range remove {
		_ = q.bt.Delete(e)
	}
	_ = q.bt.ReplaceOrInsert(ge)
}

func (q *GossipEventQueue) orderedView() []*GossipEvent {
	gossipQueue := make([]*GossipEvent, 0)
	q.bt.Descend(func(i btree.Item) bool {
		o := i.(*GossipEvent)
		gossipQueue = append(gossipQueue, o)
		return true
	})
	return gossipQueue
}

func (q *GossipEventQueue) GetGossipEvents(limit int) ([]byte, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	gossips := make([]*GossipEvent, 0, limit)
	q.bt.Descend(func(i btree.Item) bool {
		ge := i.(*GossipEvent)
		gossips = append(gossips, ge)
		if len(gossips) >= limit {
			return false
		}
		return true
	})

	transmitLimit := calcTransmitLimit(q.numNodes())
	for _, g := range gossips {
		_ = q.bt.Delete(g)
		g.Transmit++
		if g.Transmit < transmitLimit {
			q.bt.ReplaceOrInsert(g)
		}
	}

	buf := bytes.NewBuffer([]byte{})
	enc := gob.NewEncoder(buf)
	if err := enc.Encode(gossips); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func calcTransmitLimit(totalNodes int) int {
	limit := math.Ceil(math.Log10(float64(totalNodes)))
	return int(limit)
}
