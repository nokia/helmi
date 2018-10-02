package main

import (
	"github.com/monostream/helmi/pkg/broker"
	"github.com/monostream/helmi/pkg/catalog"
	"github.com/monostream/helmi/pkg/helm"

	"code.cloudfoundry.org/lager"

	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
)

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}

	return fallback
}

func parseHelmReposFromJSON(helmReposJSON string) error {
	var helmRepos map[string]string
	err := json.Unmarshal([]byte(helmReposJSON), &helmRepos)
	if err != nil {
		return err
	}

	for repo, url := range helmRepos {
		err := helm.RepoAdd(repo, url)
		if err != nil {
			return errors.New(fmt.Sprintf("failed to update repository %s: %s", repo, err))
		}
	}

	return helm.RepoUpdate()
}

func main() {
	logger := lager.NewLogger("helmi")
	logger.RegisterSink(lager.NewWriterSink(os.Stdout, lager.DEBUG))
	logger.RegisterSink(lager.NewWriterSink(os.Stderr, lager.ERROR))

	catalogSource := getEnv("CATALOG_URL", "./catalog")
	c, err := catalog.Parse(catalogSource)
	if err != nil {
		log.Fatal(err)
	}

	// expects a JSON map in the form of "name":"http://url" pairs
	err = parseHelmReposFromJSON(getEnv("REPOSITORY_URLS", "{}"))
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
