package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"code.cloudfoundry.org/lager"
	"github.com/monostream/helmi/pkg/broker"
	"github.com/monostream/helmi/pkg/catalog"
	"github.com/monostream/helmi/pkg/helm"
)

func main() {
	logger := lager.NewLogger("helmi")
	logger.RegisterSink(lager.NewWriterSink(os.Stdout, lager.DEBUG))
	logger.RegisterSink(lager.NewWriterSink(os.Stderr, lager.ERROR))

	// expects a JSON map in the form of "name":"http://url" pairs
	err := parseHelmReposFromJSON(getEnv("REPOSITORY_URLS", "{}"))

	if err != nil {
		log.Fatal(err)
	}

	catalogSource := getEnv("CATALOG_URL", "./catalog")
	c, err := catalog.New(catalogSource)

	if err != nil {
		log.Fatal("Failed to parse catalog. Did you set CATALOG_URL correctly? Error:", err)
	}

	err = verifyChartVersions(c)

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
			return fmt.Errorf("failed to update repository %s: %s", repo, err)
		}
	}

	return helm.RepoUpdate()
}

func verifyChartVersions(catalog *catalog.Catalog) error {
	charts, err := helm.ListCharts()

	if err != nil {
		return err
	}

	for _, service := range catalog.Services() {
		for _, plan := range service.Plans {
			chartName := service.Chart
			chartVersion := service.ChartVersion

			if len(plan.Chart) > 0 {
				chartName = plan.Chart
			}

			if len(plan.ChartVersion) > 0 {
				chartVersion = plan.ChartVersion
			}

			if chart, ok := charts[chartName]; ok {
				if !strings.EqualFold(chart.ChartVersion, chartVersion) {
					fmt.Println(fmt.Sprintf("Outdated Chart %v: %v <> %v", chartName, chartVersion, chart.ChartVersion))
				}
			} else {
				fmt.Println(fmt.Sprintf("Missing Chart %v", chartName))
			}
		}
	}

	return nil
}
