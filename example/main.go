package main

import (
	"github.com/Mathew-Estafanous/memlist"
	"log"
	"time"
)

func main() {
	conf1 := memlist.DefaultLocalConfig()
	conf1.Name = "ONE"
	_, err := memlist.Create(conf1)
	if err != nil {
		log.Fatalln(err)
	}

	conf2 := memlist.DefaultLocalConfig()
	conf2.Name = "TWO"
	conf2.BindPort = 7970
	mem2, err := memlist.Create(conf2)
	if err != nil {
		log.Fatalln(err)
	}
	if err = mem2.Join(":7990"); err != nil {
		log.Fatalln(err)
	}

	conf3 := memlist.DefaultLocalConfig()
	conf3.Name = "THREE"
	conf3.BindPort = 7950
	mem3, err := memlist.Create(conf3)
	if err != nil {
		log.Fatalln(err)
	}
	if err = mem3.Join(":7990"); err != nil {
		log.Fatalln(err)
	}

	stop := make(chan bool)
	time.Sleep(3 * time.Second)
	log.Println("Shutting down Member 2")
	if err = mem2.Shutdown(); err != nil {
		log.Fatalln(err)
	}
	<- stop
}
