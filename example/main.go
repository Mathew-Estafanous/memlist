package main

import (
	"github.com/Mathew-Estafanous/memlist"
	"log"
	"os"
	"strconv"
)

// [exe] <BindPort> <Host Name> <Address* (of another node in the cluster)>
func main() {
	conf := memlist.DefaultLocalConfig()
	conf.Name = os.Args[2]
	port, err := strconv.Atoi(os.Args[1])
	if err != nil {
		log.Fatalln(err)
	}
	conf.BindPort = uint16(port)

	mem, err := memlist.Create(conf)
	if err != nil {
		log.Fatalln(err)
	}

	if len(os.Args) >= 4 {
		if err := mem.Join(":"+os.Args[3], ""); err != nil {
			log.Fatalln(err)
		}
	}

	stop := make(chan bool)
	<-stop
}
