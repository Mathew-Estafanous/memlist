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
	dead
)

// Gossip represents a message that is sent to peers regarding changes
// in the state of the cluster and other nodes.
type Gossip struct {
	gt gossipType

	// node is the peer that the gossip refers to.
	node Node
}

func (g Gossip) invalidates(other Gossip) bool {
	return g.node.Name == other.node.Name
}

type GossipEvent struct {
	// The Gossip that this event relates to.
	gossip Gossip
	// transmit is the number of times this event has been transmitted.
	transmit int
}

func (g *GossipEvent) Less(i btree.Item) bool {
	o := i.(*GossipEvent)
	if g.transmit <= o.transmit {
		return true
	}
	return false
}

type GossipEventQueue struct {
	numNodes  func() int

	mu sync.Mutex
	// used as a queue that prioritize the newest events.
	bt *btree.BTree
}

func (q *GossipEventQueue) addGossip(g Gossip) {
	ge := &GossipEvent{
		gossip: g,
		transmit: 0,
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	override := make([]*GossipEvent, 0)
	q.bt.Ascend(func(i btree.Item) bool {
		o := i.(*GossipEvent)
		if o.gossip.invalidates(ge.gossip) {
			override = append(override, o)
			return false
		}
		return true
	})

	for _, e := range override {
		_ = q.bt.Delete(e)
	}
	_ = q.bt.ReplaceOrInsert(ge)
}

func (q *GossipEventQueue) getGossips(limit int) ([]byte, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	gossips := make([]*GossipEvent, 0, limit)
	q.bt.Ascend(func(i btree.Item) bool {
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
		g.transmit++
		if g.transmit < transmitLimit {
			q.bt.ReplaceOrInsert(g)
		}
	}

	var b []byte
	enc := gob.NewEncoder(bytes.NewBuffer(b))
	if err := enc.Encode(gossips); err != nil {
		return nil, err
	}
	return b, nil
}

func calcTransmitLimit(totalNodes int) int {
	limit := math.Ceil(math.Log10(float64(totalNodes)))
	return int(limit)
}
