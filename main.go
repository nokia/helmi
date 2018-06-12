package main

import (
	"code.cloudfoundry.org/lager"
	"github.com/monostream/helmi/pkg/broker"
	"github.com/monostream/helmi/pkg/catalog"
	"log"
	"os"
)

func main() {
	logger := lager.NewLogger("helmi")
	logger.RegisterSink(lager.NewWriterSink(os.Stdout, lager.DEBUG))
	logger.RegisterSink(lager.NewWriterSink(os.Stderr, lager.ERROR))

	c, err := catalog.ParseDir("./catalog")
	if err != nil {
		log.Fatal(err)
	}

	addr := ":5000"
	if port, ok := os.LookupEnv("PORT"); ok {
		addr = ":" + port
	}

	user := os.Getenv("USERNAME")
	pass := os.Getenv("PASSWORD")

	if user == "" || pass == "" {
		log.Println("Username and/or password not specified, authentication will be disabled!")
	}

	config := broker.Config{
		Username: user,
		Password: pass,
		Address:  addr,
	}

	b := broker.NewBroker(c, config, logger)
	b.Run()
}
