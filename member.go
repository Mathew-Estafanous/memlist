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

type node struct {
	name string
	addr net.IP
	port uint16
	state StateType
}

type Member struct {
	conf   *Config
	nodeMap map[string]*node

	shutdownCh chan struct{}
	logger *log.Logger
}

func Create(conf *Config) (*Member, error) {
	l := log.New(os.Stdout, fmt.Sprintf("[%v]", conf.Name), log.LstdFlags)
	return &Member{
		conf: conf,
		nodeMap: make(map[string]*node),
		shutdownCh: make(chan struct{}),
		logger: l,
	}, nil
}
