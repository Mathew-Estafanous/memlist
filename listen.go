package memlist

// Listener is implemented by the client as a way to hook into the membership
// and gossip layer of the Member. Providing a way to listen for important
// events. This listener must be thread-safe.
type Listener interface {
	// OnMembershipChange is called when there is a change in the state of a
	// peer or if a peer has joined/left the cluster.
	OnMembershipChange(peer Node)
}

// emptyListener is a stub struct that is used in the case that there is no
// provided Listener. This listener does nothing with each event.
type emptyListener struct{}

func (e *emptyListener) OnMembershipChange(_ Node) {}
