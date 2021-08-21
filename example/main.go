package main

import (
	"github.com/Mathew-Estafanous/memlist"
	"log"
)

func main() {
	conf1 := memlist.DefaultLocalConfig()
	conf1.Name = "MEMBER ONE"
	mem1, err := memlist.Create(conf1)
	if err != nil {
		log.Fatalln(err)
	}

	conf2 := memlist.DefaultLocalConfig()
	conf2.Name = "MEMBER TWO"
	conf2.BindPort = 7970
	mem2, err := memlist.Create(conf2)
	if err != nil {
		log.Fatalln(err)
	}

	if err = mem2.Join(":7990"); err != nil {
		log.Fatalln(err)
	}

	stop := make(chan bool)
	log.Printf("Member 1: %v", mem1.AllNodes())
	log.Printf("Member 2: %v", mem2.AllNodes())
	<- stop
}
