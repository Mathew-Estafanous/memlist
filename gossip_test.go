package memlist

import (
	"github.com/google/btree"
	"testing"
)

func newGossipEventQueue() *GossipEventQueue {
	return &GossipEventQueue{
		numNodes: func() int {
			return 3
		},
		bt: btree.New(2),
	}
}

func TestGossipEventQueue_Queue(t *testing.T) {
	gossips := [2]Gossip{{gt: join, node: Node{Name: "One"}},
		{gt: join, node: Node{Name: "Two"}}}
	eventQue := newGossipEventQueue()
	for _, g := range gossips {
		eventQue.Queue(g)
	}

	queue := eventQue.orderedView()
	for i := range queue {
		otherName := queue[i].gossip.node.Name
		if otherName != gossips[i].node.Name {
			t.Fatalf("Gossip %v is not equal to %v", queue[0].gossip, gossips[0])
		}
	}

	invalidate := Gossip{gt: dead, node: Node{Name: "Two"}}
	eventQue.Queue(invalidate)

	expected := [2]Gossip{{gt: dead, node: Node{Name: "Two"}},
		{gt: join, node: Node{Name: "One"}}}
	queue = eventQue.orderedView()
	if len(queue) != len(expected) {
		t.Fatalf("Queue length %v does not match expected length %v.", len(queue), len(expected))
	}

	for i := range queue {
		otherName := queue[i].gossip.node.Name
		if otherName != gossips[i].node.Name {
			t.Fatalf("Gossip %v is not equal to %v", queue[0].gossip, gossips[0])
		}
	}
}
