package main

import (
	"code.cloudfoundry.org/lager"
	"github.com/monostream/helmi/pkg/broker"
	"github.com/monostream/helmi/pkg/catalog"
	"log"
	"os"
)

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}

	return fallback
}

func main() {
	logger := lager.NewLogger("helmi")
	logger.RegisterSink(lager.NewWriterSink(os.Stdout, lager.DEBUG))
	logger.RegisterSink(lager.NewWriterSink(os.Stderr, lager.ERROR))

	catalogSource := getEnv("CATALOG_SOURCE", "./catalog")
	c, err := catalog.Parse(catalogSource)
	if err != nil {
		log.Fatal(err)
	}

	addr := ":" + getEnv("PORT", "5000")
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
