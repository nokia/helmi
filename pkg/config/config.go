package config

import (
	"os"
	"reflect"
)

type Config struct {
	RepositoryURLs string `env:"REPOSITORY_URLS" default:"{}"`
	HelmNamespace  string `env:"HELM_NAMESPACE"`
	IngressDomain  string `env:"INGRESS_DOMAIN"`

	Username  string `env:"USERNAME"`
	Password  string `env:"PASSWORD"`
	Port      string `env:"PORT" default:"5000"`

	CatalogURL             string `env:"CATALOG_URL" default:"./catalog"`
	CatalogUpdateInterval  string `env:"CATALOG_UPDATE_INTERVAL" default:"20s"`
}

// This loads environment variables or sets a default value based on the tag in the struct definition
func (c *Config) LoadConfig() {
	val := reflect.ValueOf(c).Elem()
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		envName := field.Tag.Get("env")
		defaultValue := field.Tag.Get("default")

		if envName == "" && defaultValue == "" {
			// resolvable fields must have the `env` or `default` struct tag
			continue
		}
		if value, ok := os.LookupEnv(envName); ok {
			// set the field value
			val.Field(i).SetString(value)
		} else if defaultValue != "" {
			// set a fallback value
			val.Field(i).SetString(defaultValue)
		}
	}
}