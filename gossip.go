package memlist

import (
	"bytes"
	"encoding/gob"
	"fmt"
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

	notify chan struct{}
}

// finished sends a notification message through the channel, signifying that
// the event has successfully disseminated.
func (g *GossipEvent) finished() {
	select {
	case g.notify <- struct{}{}:
	default:
	}
}

func (g *GossipEvent) String() string {
	return fmt.Sprintf("[%v: %v]", g.Gossip.Node.Name, g.Transmit)
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

// QueueWithBroadcast is similar to Queue; however it provides the ability to inject a broadcast
// channel that is notified when the gossip has successfully disseminated across the cluster.
func (q *GossipEventQueue) QueueWithBroadcast(g Gossip, broadcast chan struct{}) {
	q.queueEvent(g, broadcast)
}

// Queue will add the gossip to the queue of events that will be eventually disseminated
// across the cluster.
// If a notification when the gossip has finished disseminating, look into using QueueWithBroadcast.
func (q *GossipEventQueue) Queue(g Gossip) {
	q.queueEvent(g, make(chan struct{}))
}

func (q *GossipEventQueue) queueEvent(g Gossip, notify chan struct{}) {
	ge := &GossipEvent{
		Gossip:   g,
		Transmit: 0,
		notify:   notify,
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

	transmitLimit := q.calcTransmitLimit()
	for _, g := range gossips {
		_ = q.bt.Delete(g)
		g.Transmit++
		if g.Transmit <= transmitLimit {
			q.bt.ReplaceOrInsert(g)
		} else {
			g.finished()
		}
	}

	buf := bytes.NewBuffer([]byte{})
	enc := gob.NewEncoder(buf)
	if err := enc.Encode(gossips); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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

func (q *GossipEventQueue) calcTransmitLimit() int {
	limit := math.Ceil(math.Log10(float64(q.numNodes() + 1)))
	return int(limit)
}
