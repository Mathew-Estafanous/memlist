# Memlist
[![Go Report Card](https://goreportcard.com/badge/github.com/Mathew-Estafanous/memlist)](https://goreportcard.com/report/github.com/Mathew-Estafanous/memlist)
[![GoDoc](https://godoc.org/github.com/Mathew-Estafanous/memlist?status.svg)](https://pkg.go.dev/github.com/Mathew-Estafanous/memlist)
---
Memlist is a peer-to-peer cluster membership package that uses the [SWIM protocol](https://www.cs.cornell.edu/projects/Quicksilver/public_pdfs/SWIM.pdf),
to detect member failures and communicate them through a gossip-style method. This package can 
be used as a platform to manage membership within a distributed system. Enabling things such as 
service discovery and data consistency among the entire cluster. 

Memlist is a weakly-consistent protocol, meaning data will eventually become consistent among the 
entire cluster. The speed at which it becomes consistent largely depends on the chosen configuration.
Node failures are detected though a periodic "ping-ack" communication and then disseminated across the
cluster, in the case of a new gossip event.

## Usage
Memlist is very simple to use and only takes a few lines to get up and running.
```go
// You can use the default local config or create your own configuration.
conf := memlist.DefaultLocalConfig()
mem, err := memlist.Create(conf)
if err != nil {
	log.Fatal("Failed to create member: ", err)
}

// Join the cluster with the address of one of the members of that cluster.
if err := mem.Join(":7990"); err != nil {
	log.Fatal("Failed to join another cluster: ", err)
}

// Get information about other nodes in the cluster.
for node := range mem.AllNodes() {
	log.Println(node)
}

// Now the member will keep track of the cluster in the background, enabling 
// you to go ahead and do other needed operations.
// 
// A Listener can be used to receive events when a change in membership occurs. 
// This must be implemented by the client and injected into the Config before 
// creating a member. 
```

## In Action
Watch as memlist sucessfully identifies and forms a cluster with three nodes and automatically detects a failure in the THIRD
node and successfully spreading this information accross the rest of the cluster.
https://user-images.githubusercontent.com/56979977/138743693-974c21ca-6a98-44bc-869d-2e2f461ba880.mov



## Connect & Contact
**Email** - mathewestafanous13@gmail.com

**Website** - https://mathewestafanous.com

**GitHub** - https://github.com/Mathew-Estafanous
