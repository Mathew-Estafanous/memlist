package memlist

import (
	"bytes"
	"encoding/gob"
	"github.com/google/btree"
	"reflect"
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
	gossips := [3]Gossip{
		{Gt: join, Node: Node{Name: "Two"}},
		{Gt: dead, Node: Node{Name: "Three"}},
		{Gt: join, Node: Node{Name: "One"}}}
	eventQue := newGossipEventQueue()
	for _, g := range gossips {
		eventQue.Queue(g, 0)
	}

	queue := eventQue.orderedView()
	for i := range queue {
		otherName := queue[i].Gossip.Node.Name
		if otherName != gossips[i].Node.Name {
			t.Fatalf("Gossip %v is not equal to %v", queue[0].Gossip, gossips[0])
		}
	}

	invalidate := Gossip{Gt: dead, Node: Node{Name: "One"}}
	eventQue.Queue(invalidate, 0)

	expected := [3]Gossip{
		{Gt: join, Node: Node{Name: "Two"}},
		{Gt: dead, Node: Node{Name: "Three"}},
		{Gt: dead, Node: Node{Name: "One"}}}
	queue = eventQue.orderedView()
	if len(queue) != len(expected) {
		t.Fatalf("Queue length %v does not match expected length %v.", len(queue), len(expected))
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
		eventQue.Queue(g, 0)
	}

	resultBuf, err := eventQue.GetGossipEvents(2)
	if err != nil {
		t.Fatal(err)
	}

	expected := []*GossipEvent{
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
	expected = []*GossipEvent{{Transmit: 0, Gossip: gossips[2]}}
	if !reflect.DeepEqual(result, expected) {
		t.Fatal("Expected remaining events do not match the result.")
	}
}
