package main

import (
	"os"
	"github.com/monostream/helmi/pkg/broker"
	"code.cloudfoundry.org/lager"
	"github.com/monostream/helmi/pkg/catalog"
	"log"
)

func main() {
	logger := lager.NewLogger("helmi")
	logger.RegisterSink(lager.NewWriterSink(os.Stdout, lager.DEBUG))
	logger.RegisterSink(lager.NewWriterSink(os.Stderr, lager.ERROR))

	var catalog catalog.Catalog
	catalog.Parse("./catalog.yaml")

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
		Address: addr,
	}

	b := broker.NewBroker(catalog, config, logger)
	b.Run()
}