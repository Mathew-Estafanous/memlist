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

type gossipEvent struct {
	// The Gossip that this event relates to.
	Gossip Gossip
	// Transmit is the number of times this event has been transmitted.
	Transmit int

	// notify channel will receive a message when the event has properly disseminated.
	notify chan struct{}
}

// finished sends a notification message through the channel, signifying that
// the event has successfully disseminated.
func (g *gossipEvent) finished() {
	select {
	case g.notify <- struct{}{}:
	default:
	}
}

func (g *gossipEvent) String() string {
	return fmt.Sprintf("[%v: %v]", g.Gossip.Node.Name, g.Transmit)
}

func (g *gossipEvent) Less(i btree.Item) bool {
	o := i.(*gossipEvent)
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

type gossipEventQueue struct {
	numNodes func() int

	mu sync.Mutex
	// used as a queue that prioritize the newest events.
	bt *btree.BTree
}

// queueWithBroadcast is similar to queue; however it provides the ability to inject a broadcast
// channel that is notified when the gossip has successfully disseminated across the cluster.
func (q *gossipEventQueue) queueWithBroadcast(g Gossip, broadcast chan struct{}) {
	q.queueEvent(g, broadcast)
}

// queue will add the gossip to the queue of events that will be eventually disseminated
// across the cluster.
// If a notification when the gossip has finished disseminating, look into using queueWithBroadcast.
func (q *gossipEventQueue) queue(g Gossip) {
	q.queueEvent(g, make(chan struct{}))
}

func (q *gossipEventQueue) queueEvent(g Gossip, notify chan struct{}) {
	ge := &gossipEvent{
		Gossip:   g,
		Transmit: 0,
		notify:   notify,
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	var remove []*gossipEvent
	q.bt.Ascend(func(i btree.Item) bool {
		o := i.(*gossipEvent)
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

func (q *gossipEventQueue) getGossipEvents(limit int) ([]byte, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	gossips := make([]*gossipEvent, 0, limit)
	q.bt.Descend(func(i btree.Item) bool {
		ge := i.(*gossipEvent)
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

func (q *gossipEventQueue) orderedView() []*gossipEvent {
	gossipQueue := make([]*gossipEvent, 0)
	q.bt.Descend(func(i btree.Item) bool {
		o := i.(*gossipEvent)
		gossipQueue = append(gossipQueue, o)
		return true
	})
	return gossipQueue
}

// calcTransmitLimit will calculate transmit total using Log(N + 1) with N
// being the number of known nodes.
func (q *gossipEventQueue) calcTransmitLimit() int {
	limit := math.Ceil(math.Log10(float64(q.numNodes() + 1)))
	return int(limit)
}
