package memlist

import (
	"bytes"
	"encoding/gob"
	"github.com/google/btree"
	"testing"
)

func newGossipEventQueue() *gossipEventQueue {
	return &gossipEventQueue{
		numNodes: func() int {
			return 3
		},
		bt: btree.New(2),
	}
}

func TestGossipEventQueue_Queue(t *testing.T) {
	gossips := [3]Gossip{
		{Gt: join, Node: Node{Name: "Two"}},
		{Gt: dead, Node: Node{Name: "Three"}},
		{Gt: join, Node: Node{Name: "One"}}}
	eventQue := newGossipEventQueue()
	for _, g := range gossips {
		eventQue.queue(g)
	}

	queue := eventQue.orderedView()
	for i := range queue {
		otherName := queue[i].Gossip.Node.Name
		if otherName != gossips[i].Node.Name {
			t.Fatalf("Gossip %v is not equal to %v", queue[0].Gossip, gossips[0])
		}
	}

	invalidate := Gossip{Gt: dead, Node: Node{Name: "One"}}
	eventQue.queue(invalidate)

	expected := [3]Gossip{
		{Gt: join, Node: Node{Name: "Two"}},
		{Gt: dead, Node: Node{Name: "Three"}},
		{Gt: dead, Node: Node{Name: "One"}}}
	queue = eventQue.orderedView()
	if len(queue) != len(expected) {
		t.Fatalf("queue length %v does not match expected length %v.", len(queue), len(expected))
	}

	for i := range queue {
		otherName := queue[i].Gossip.Node.Name
		if otherName != expected[i].Node.Name {
			t.Fatalf("Gossip %v is not equal to %v", queue[i].Gossip, expected[i])
		}
	}
}

func TestGossipEventQueue_GetGossipEvents(t *testing.T) {
	gossips := [3]Gossip{
		{Gt: join, Node: Node{Name: "Two"}},
		{Gt: dead, Node: Node{Name: "Three"}},
		{Gt: join, Node: Node{Name: "One"}}}
	eventQue := newGossipEventQueue()
	for _, g := range gossips {
		eventQue.queue(g)
	}

	resultBuf, err := eventQue.getGossipEvents(2)
	if err != nil {
		t.Fatal(err)
	}

	expected := []*gossipEvent{
		{Transmit: 1, Gossip: gossips[0]},
		{Transmit: 1, Gossip: gossips[1]},
	}
	expectedBuf := bytes.NewBuffer([]byte{})
	enc := gob.NewEncoder(expectedBuf)
	if err = enc.Encode(expected); err != nil {
		t.Error(err)
	}

	if !bytes.Equal(resultBuf, expectedBuf.Bytes()) {
		t.Fatal("Result byte slice does not match expected.")
	}

	result := eventQue.orderedView()
	expected = []*gossipEvent{
		{Transmit: 1, Gossip: gossips[0]},
		{Transmit: 1, Gossip: gossips[1]},
		{Transmit: 0, Gossip: gossips[2]},
	}
	for i, res := range result {
		expect := expected[i]
		if expect.Transmit != res.Transmit {
			t.Fatalf("Result event %d had transmit of %d instead of %d", i, res.Transmit, expect.Transmit)
		}
		if expect.Gossip != res.Gossip {
			t.Fatalf("Result event %d had %v instead of %v", i, res.Gossip, expect.Gossip)
		}
	}
}
