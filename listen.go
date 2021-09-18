package memlist

// Listener is implemented by the client as a way to hook into the membership
// and gossip layer of the Member. Providing a way to listen for important
// events. This listener must be thread-safe.
type Listener interface {
	// OnPeerChange is called when there is a change in the state of a
	// peer or if a new peer has joined the cluster.
	OnPeerChange(peer Node)
}
