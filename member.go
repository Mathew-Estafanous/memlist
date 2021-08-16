package memlist

import (
	"fmt"
	"log"
	"net"
	"os"
)

type StateType int

const (
	Alive StateType = iota
	Dead
	Left
)

// node represents a single node within the cluster.
type node struct {
	name string
	addr net.IP
	port uint16
	state StateType
}

type Member struct {
	conf      *Config
	transport Transport

	nodeMap map[string]*node
	numNodes uint32

	shutdownCh chan struct{}
	logger *log.Logger
}

func Create(conf *Config) (*Member, error) {
	if conf.Transport == nil {
		// TODO: Create the default transport.
	}

	l := log.New(os.Stdout, fmt.Sprintf("[%v]", conf.Name), log.LstdFlags)
	mem := &Member{
		conf: conf,
		transport: conf.Transport,
		nodeMap: make(map[string]*node),
		shutdownCh: make(chan struct{}),
		logger: l,
	}

	go mem.packetListen()
	return mem, nil
}

func (m *Member) packetListen() {
	for {
		select {
		case p := <- m.transport.Packets():
			m.handlePacket(p.Buf, p.From)
		case <-m.shutdownCh:
			return
		}
	}
}

func (m *Member) handlePacket(buf []byte, from net.Addr) {

}
