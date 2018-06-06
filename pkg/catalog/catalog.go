package catalog

import (
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
	"path/filepath"
	"strings"
	"text/template"

	"fmt"
	"os"

	"bytes"
	"github.com/Masterminds/sprig"
	"github.com/monostream/helmi/pkg/helm"
	"github.com/monostream/helmi/pkg/kubectl"
	"github.com/satori/go.uuid"
	"strconv"
)

type Catalog struct {
	Services map[string]Service
}

type Service struct {
	Id          string `yaml:"_id"`
	Name        string `yaml:"_name"`
	Description string `yaml:"description"`

	Chart        string `yaml:"chart"`
	ChartVersion string `yaml:"chart-version"`

	Plans []Plan `yaml:"plans"`

	valuesTemplate      *template.Template
	credentialsTemplate *template.Template
}

type Plan struct {
	Id          string `yaml:"_id"`
	Name        string `yaml:"_name"`
	Description string `yaml:"description"`

	Chart        string            `yaml:"chart"`
	ChartVersion string            `yaml:"chart-version"`
	ChartValues  map[string]string `yaml:"chart-values"`

	UserCredentials map[string]interface{} `yaml:"user-credentials"`
}

// Parses all `.yaml` and `.yml` files in the specified path as service definitions
func ParseDir(dir string) (Catalog, error) {
	c := Catalog{
		Services: make(map[string]Service),
	}

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("unable to read catalog file: %q: %s", path, err)
			return nil
		}

		ext := filepath.Ext(path)
		if info.IsDir() || (ext != ".yml" && ext != ".yaml") {
			return nil
		}

		input, ioErr := ioutil.ReadFile(path)
		if ioErr != nil {
			return ioErr
		}

		return c.parseServiceDefinition(input, path)
	})

	if err != nil {
		return c, err
	}

	if len(c.Services) == 0 {
		err = fmt.Errorf("no services found in catalog directory: %s", dir)
		return c, err
	}

	return c, nil
}

func (c *Catalog) parseServiceDefinition(input []byte, file string) error {
	// we have three documents: service, chart-values, user-credentials
	documents := bytes.Split(input, []byte("\n---"))
	if n := len(documents); n != 3 {
		return fmt.Errorf("service file %s: must contain 3 yaml document parts, found %d", file, n)
	}

	var s struct{ Service }
	err := yaml.Unmarshal(documents[0], &s)
	if err != nil {
		return fmt.Errorf("failed to parse service definition: %s: %s", file, err)
	}

	fMap := templateFuncMap()
	valuesTemplate, valuesErr := template.New("values").Funcs(fMap).Parse(string(documents[1]))
	if valuesErr != nil {
		return fmt.Errorf("failed to parse values template: %s: %s", file, valuesErr)
	}

	credentialsTemplate, credentialsErr := template.New("credentials").Funcs(fMap).Parse(string(documents[2]))
	if credentialsErr != nil {
		return fmt.Errorf("failed to parse credentials template: %s: %s", file, credentialsErr)
	}

	s.valuesTemplate = valuesTemplate
	s.credentialsTemplate = credentialsTemplate

	c.Services[s.Id] = s.Service
	return nil
}

func (c *Catalog) Service(id string) *Service {
	if val, ok := c.Services[id]; ok {
		return &val
	}
	return nil
}

func (s *Service) Plan(id string) *Plan {
	for _, p := range s.Plans {
		if strings.EqualFold(p.Id, id) {
			return &p
		}
	}
	return nil
}

func templateFuncMap() template.FuncMap {
	f := sprig.TxtFuncMap()

	randomUuid := func() string {
		s := uuid.NewV4().String()
		s = strings.Replace(s, "-", "", -1)
		return s
	}

	f["generateUsername"] = randomUuid
	f["generatePassword"] = randomUuid

	return f
}

type chartValueVars struct {
	*Service
	*Plan
}

func (s *Service) ChartValues(p *Plan) (map[string]string, error) {
	b := new(bytes.Buffer)
	data := chartValueVars{s, p}
	err := s.valuesTemplate.Execute(b, data)
	if err != nil {
		return nil, err
	}

	var v struct {
		ChartValues map[string]string `yaml:"chart-values"`
	}

	err = yaml.Unmarshal(b.Bytes(), &v)
	if err != nil {
		return nil, err
	}

	if v.ChartValues == nil {
		v.ChartValues = make(map[string]string)
	}

	for key, value := range p.ChartValues {
		v.ChartValues[key] = value
	}

	return v.ChartValues, nil
}

type credentialVars struct {
	Service *Service
	Plan    *Plan
	Values  valueVars
	Release releaseVars
	Cluster clusterVars
	Secrets map[string]string
}

type valueVars map[string]interface{}

type releaseVars struct {
	Name      string
	Namespace string
}

type clusterVars struct {
	Address    string
	Hostname   string
	helmStatus helm.Status
}

func (c clusterVars) Port(port ...int) string {
	for clusterPort, nodePort := range c.helmStatus.NodePorts {
		if len(port) == 0 || port[0] == clusterPort {
			return strconv.Itoa(nodePort)
		}
	}

	for clusterPort, nodePort := range c.helmStatus.ClusterPorts {
		if len(port) == 0 || port[0] == clusterPort {
			return strconv.Itoa(nodePort)
		}
	}

	if len(port) > 0 {
		return strconv.Itoa(port[0])
	}

	return ""
}

func extractAddress(kubernetesNodes []kubectl.Node) string {
	// return dns name if set as environment variable
	if value, ok := os.LookupEnv("DOMAIN"); ok {
		return value
	}

	for _, node := range kubernetesNodes {
		if len(node.ExternalIP) > 0 {
			return node.ExternalIP
		}
	}

	for _, node := range kubernetesNodes {
		if len(node.InternalIP) > 0 {
			return node.InternalIP
		}
	}

	return ""
}

func extractHostname(kubernetesNodes []kubectl.Node) string {
	for _, node := range kubernetesNodes {
		if len(node.Hostname) > 0 {
			return node.Hostname
		}
	}

	return ""
}

func (s *Service) UserCredentials(plan *Plan, kubernetesNodes []kubectl.Node, helmStatus helm.Status, values map[string]interface{}) (map[string]interface{}, error) {

	// quick poc to retrieve secrets
	secrets, err := kubectl.GetSecret("secret-client-" + helmStatus.Name + "-datagrid", helmStatus.Namespace)
	if err != nil {
		secrets = make(map[string]string)
	}

	env := credentialVars{
		Service: s,
		Plan:    plan,
		Values:  values,
		Release: releaseVars{
			Name:      helmStatus.Name,
			Namespace: helmStatus.Namespace,
		},
		Cluster: clusterVars{
			Address:    extractAddress(kubernetesNodes),
			Hostname:   extractHostname(kubernetesNodes),
			helmStatus: helmStatus,
		},
		Secrets: secrets,
	}

	b := new(bytes.Buffer)
	err = s.credentialsTemplate.Execute(b, env)
	if err != nil {
		return nil, err
	}

	var v struct {
		UserCredentials map[string]interface{} `yaml:"user-credentials"`
	}
	err = yaml.Unmarshal(b.Bytes(), &v)
	if err != nil {
		return nil, err
	}

	return v.UserCredentials, nil
}
